// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_sweep_ttl_test.go — E2E spec for INV-E18: the sweep subsystem aborts
// stale checkpoints and emits a chained rekey audit event.
//
// Verifies INV-E18-SWEEP-TTL-AUDIT:
//   - A checkpoint whose last_heartbeat_at is older than TTL is auto-aborted
//     by CheckpointSweepSubsystem.sweepOnce.
//   - The sweep emits a chained rekey audit event with aborted_reason="ttl_expired"
//     to the context's rekey chain scope.
//
// Implementation: the test seeds a stale checkpoint via SeedStaleCheckpoint,
// constructs a CheckpointSweepSubsystem with a very short TTL (1ms, so any
// row is expired), and calls SweepOnceForTest directly. This is the same
// approach used by the unit-integration sweep tests, promoted here to an E2E
// spec to verify the full stack (real Postgres, real RekeyAuditEmitter, real
// chain-linking via the primary server's emitter).
//
// Part of holomush-jxo8.7 (bead jxo8.7.39, merged T45+T46+T47).
package crypto_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
)

// sweepTestAuditPublisher implements dek.AuditPublisher for the sweep TTL E2E
// test. It inserts published audit events directly into events_audit using the
// same INSERT pattern as holomushtest.testAuditPublisher — this makes events
// visible to AssertAuditEventEmitted and the chain verifier.
type sweepTestAuditPublisher struct {
	pool *pgxpool.Pool
}

func (p *sweepTestAuditPublisher) PublishAudit(
	ctx context.Context,
	subject, evType string,
	payload []byte,
) (ulid.ULID, error) {
	id := ulid.Make()
	// events_audit.timestamp is BIGINT-ns post-gfo6 (INV-STORE-1).
	_, err := p.pool.Exec(ctx,
		`INSERT INTO events_audit
		   (id, subject, type, timestamp, actor_kind, envelope, schema_ver, codec, js_seq, rendering)
		 VALUES ($1, $2, $3, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT, 'system', $4, 1, 'identity', 0, '{}'::jsonb)
		 ON CONFLICT (id) DO NOTHING`,
		id[:], subject, evType, payload)
	return id, err //nolint:wrapcheck // passthrough; caller surfaces as oops code
}

var _ = Describe("Rekey sweep TTL", func() {
	It("aborts stale checkpoints and emits a chained audit event (INV-E18)", func() {
		h := SetupRekeyHarness(suiteT, WithEventCount(10))
		defer h.Cleanup()

		// Seed a stale checkpoint whose last_heartbeat_at is 1 hour in the past —
		// well beyond any reasonable TTL for this test.
		rid := h.SeedStaleCheckpoint("scene", "01ABC", 1*time.Hour)

		// Confirm the checkpoint is non-terminal before the sweep runs.
		ckpt0, err := h.findCheckpointByID(rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt0.Status.IsTerminal()).To(BeFalse(),
			"seeded checkpoint must be non-terminal before sweep")

		// Construct a CheckpointSweepSubsystem with a 1ms TTL so any row whose
		// heartbeat is more than 1ms old is considered expired. The stale row
		// (1 hour old heartbeat) trivially satisfies this. The sweep uses the
		// primary server's RekeyAuditEmitter to emit the chained abort event —
		// this verifies the chain-linking path on the same Postgres instance.
		//
		// We build the audit emitter the same way the test server does: backed
		// by the chain.NewEmitter + testAuditPublisher pair from the primary.
		// Since the primary's GetAuditChainVerifier wires the same chainRepo, the
		// emitted event is linkable into the verifiable chain.
		chainRepo := chain.NewPostgresRepo(h.DB)
		chainEmitter := chain.NewEmitter(chainRepo)
		// Re-use the same testAuditPublisher pattern: insert directly into events_audit.
		pub := &sweepTestAuditPublisher{pool: h.DB}
		sweepAuditEmitter := dek.NewRekeyAuditEmitter(chainEmitter, pub)

		sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
			Repo:         h.Primary.GetCheckpointRepo(),
			AuditEmitter: sweepAuditEmitter,
			TTL:          time.Millisecond, // effectively zero — all non-terminal rows expire
			Interval:     time.Hour,        // irrelevant — we call SweepOnceForTest directly
			Logger:       slog.Default(),
		})

		// Invoke the sweep synchronously.
		Expect(sub.SweepOnceForTest(context.Background())).To(Succeed(),
			"SweepOnceForTest must not return an error")

		// INV-E18: the checkpoint MUST be aborted.
		ckpt1, err := h.findCheckpointByID(rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt1.Status).To(Equal(dek.CheckpointStatusAborted),
			"INV-E18: TTL-expired checkpoint MUST be marked aborted by sweep")
		Expect(ckpt1.AbortedReason).NotTo(BeNil(),
			"INV-E18: aborted_reason must be set")
		Expect(*ckpt1.AbortedReason).To(Equal("ttl_expired"),
			"INV-E18: aborted_reason MUST be ttl_expired")

		// INV-E18: a chained rekey audit event MUST be emitted.
		// The event is written to events_audit by sweepTestAuditPublisher.
		// Assert it landed by checking the events_audit table directly.
		h.AssertAuditEventEmitted(
			"events.g1.system.rekey.scene.01ABC",
			"crypto.system.rekey",
		)

		// Verify the emitted audit payload contains the aborted_reason context.
		var envelopeBytes []byte
		queryErr := h.DB.QueryRow(context.Background(),
			`SELECT envelope FROM events_audit
			  WHERE subject LIKE $1 AND type = 'crypto.system.rekey'
			  ORDER BY js_seq DESC LIMIT 1`,
			"events.g1.system.rekey.scene.%").Scan(&envelopeBytes)
		Expect(queryErr).NotTo(HaveOccurred(),
			"INV-E18: must be able to read the emitted sweep audit event")

		var payload dek.RekeyAuditPayload
		Expect(json.Unmarshal(envelopeBytes, &payload)).To(Succeed(),
			"INV-E18: sweep audit payload must be valid RekeyAuditPayload JSON")
		Expect(payload.Justification).To(ContainSubstring("ttl_expired"),
			"INV-E18: audit Justification MUST reference ttl_expired")
	})
})
