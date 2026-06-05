// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/pgnanos"
)

// rekeyTestSetup is the sweep subsystem integration test harness.
type rekeyTestSetup struct {
	pool         *pgxpool.Pool
	Repo         *dek.CheckpointRepo
	Publisher    *capturingPublisher // captures emitted audit events in memory
	AuditEmitter *dek.RekeyAuditEmitter
	Logger       *slog.Logger
}

// newRekeyTestSetup builds a sweep integration harness.
// It uses a real CheckpointRepo (postgres) and a real RekeyAuditEmitter
// backed by a capturingPublisher (defined in audit_test.go) so that
// emitted events are captured in-memory for assertion.
func newRekeyTestSetup() *rekeyTestSetup {
	pool := testIntegrationPool(suiteT)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	chainRepo := chain.NewPostgresRepo(pool)
	em := chain.NewEmitter(chainRepo)
	pub := &capturingPublisher{}
	auditEmitter := dek.NewRekeyAuditEmitter(em, pub)

	return &rekeyTestSetup{
		pool:         pool,
		Repo:         dek.NewCheckpointRepo(pool),
		Publisher:    pub,
		AuditEmitter: auditEmitter,
		Logger:       slog.Default(),
	}
}

// OpenStaleCheckpoint inserts a checkpoint row that appears stale: the
// last_heartbeat_at is set to now() minus age. It seeds a DEK row first
// and returns the RequestID.
func (s *rekeyTestSetup) OpenStaleCheckpoint(ctxType, ctxID string, age time.Duration) dek.RequestID {
	// Seed a DEK row to satisfy the FK constraint on crypto_rekey_checkpoints.
	const dekID int64 = 999
	_, _ = s.pool.Exec(context.Background(),
		`INSERT INTO crypto_keys (id, context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants, created_at)
         VALUES ($1, $2, $3, 1, '\x00', 'test', 'test', '[]'::jsonb, $4)
         ON CONFLICT (id) DO NOTHING`,
		dekID, ctxType, ctxID, pgnanos.From(time.Now()))

	rid := dek.RequestID(idgen.New())
	_, err := s.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, last_heartbeat_at)
        VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT - $8::BIGINT)
    `, rid[:], ctxType, ctxID,
		make([]byte, 32), make([]byte, 32),
		"01PLAYER", dekID,
		age.Nanoseconds())
	Expect(err).NotTo(HaveOccurred())
	return rid
}

// LoadEventsAuditBySubject returns all published audit events captured by
// the in-memory publisher whose Subject matches the given subject string.
func (s *rekeyTestSetup) LoadEventsAuditBySubject(subject string) []capturedPublish {
	var out []capturedPublish
	for _, p := range s.Publisher.published {
		if p.Subject == subject {
			out = append(out, p)
		}
	}
	return out
}

var _ = Describe("Sweep_TTLExpiryEmitsAudit (INV-CRYPTO-105)", func() {
	It("aborts a TTL-expired checkpoint and emits a chained rekey audit event with aborted_reason=ttl_expired", func() {
		setup := newRekeyTestSetup()

		// Insert a checkpoint whose last_heartbeat_at is 30h old (> 24h TTL).
		rid := setup.OpenStaleCheckpoint("scene", "01ABC", 30*time.Hour)

		sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
			Repo:         setup.Repo,
			AuditEmitter: setup.AuditEmitter,
			Logger:       setup.Logger,
			TTL:          24 * time.Hour,
			Interval:     1 * time.Hour, // unused — test calls SweepOnceForTest directly
		})
		Expect(sub.SweepOnceForTest(context.Background())).To(Succeed())

		// The checkpoint MUST be aborted with reason "ttl_expired".
		ckpt, err := setup.Repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusAborted),
			"INV-CRYPTO-105: TTL-expired checkpoint MUST be marked aborted by sweep")
		Expect(ckpt.AbortedReason).NotTo(BeNil())
		Expect(*ckpt.AbortedReason).To(Equal("ttl_expired"),
			"INV-CRYPTO-105: aborted_reason MUST be ttl_expired")

		// INV-CRYPTO-105: chained audit event MUST be emitted for the aborted context.
		events := setup.LoadEventsAuditBySubject("events.g1.system.rekey.scene.01ABC")
		Expect(len(events)).To(BeNumerically(">=", 1),
			"INV-CRYPTO-105: sweep MUST emit a chained audit event for each aborted checkpoint")

		// Verify the emitted payload contains the expected context and reason.
		var payload dek.RekeyAuditPayload
		Expect(json.Unmarshal(events[0].Payload, &payload)).To(Succeed())
		Expect(payload.Context.Type).To(Equal("scene"))
		Expect(payload.Context.ID).To(Equal("01ABC"))
		Expect(payload.Justification).To(ContainSubstring("ttl_expired"),
			"INV-CRYPTO-105: audit payload Justification MUST reference ttl_expired")
	})
})

var _ = Describe("Sweep_Lifecycle_StartStop", func() {
	It("starts and stops cleanly including the background tick goroutine", func() {
		setup := newRekeyTestSetup()

		sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
			Repo:         setup.Repo,
			AuditEmitter: setup.AuditEmitter,
			Logger:       setup.Logger,
			TTL:          24 * time.Hour,
			Interval:     100 * time.Millisecond, // fast interval for lifecycle test
		})

		ctx := context.Background()
		Expect(sub.Start(ctx)).To(Succeed())
		// Stop must return quickly and cleanly (goroutine exits on cancel).
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		Expect(sub.Stop(stopCtx)).To(Succeed())
	})
})

var _ = Describe("Sweep_TickScanAbortsExpired", func() {
	It("background tick loop aborts expired checkpoints (not just boot-time sweep)", func() {
		setup := newRekeyTestSetup()

		rid := setup.OpenStaleCheckpoint("scene", "01TK", 30*time.Hour)

		sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
			Repo:         setup.Repo,
			AuditEmitter: setup.AuditEmitter,
			Logger:       setup.Logger,
			TTL:          24 * time.Hour,
			Interval:     50 * time.Millisecond, // fast tick for test
		})

		ctx := context.Background()
		Expect(sub.Start(ctx)).To(Succeed())
		DeferCleanup(func() {
			stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_ = sub.Stop(stopCtx)
		})

		// Boot-time sweep should have fired; the checkpoint is already aborted.
		ckpt, err := setup.Repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusAborted),
			"tick scan MUST abort expired checkpoints")
	})
})

var _ = Describe("Sweep_IdempotentReScan", func() {
	It("second SweepOnce on an already-aborted checkpoint is a no-op and does not emit duplicate audit events", func() {
		setup := newRekeyTestSetup()

		rid := setup.OpenStaleCheckpoint("scene", "01IDP", 30*time.Hour)

		sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
			Repo:         setup.Repo,
			AuditEmitter: setup.AuditEmitter,
			Logger:       setup.Logger,
			TTL:          24 * time.Hour,
			Interval:     1 * time.Hour,
		})

		// First sweep: aborts + emits audit.
		Expect(sub.SweepOnceForTest(context.Background())).To(Succeed())
		ckpt, err := setup.Repo.Get(context.Background(), rid)
		Expect(err).NotTo(HaveOccurred())
		Expect(ckpt.Status).To(Equal(dek.CheckpointStatusAborted))

		// Second sweep: row is now terminal (aborted) so ListExpired must
		// exclude it (status NOT IN ('complete','aborted')). The scan should
		// be a no-op and return nil.
		Expect(sub.SweepOnceForTest(context.Background())).To(Succeed(),
			"second sweep on already-aborted checkpoint MUST be a no-op")

		// No additional audit events should have been emitted.
		events := setup.LoadEventsAuditBySubject("events.g1.system.rekey.scene.01IDP")
		Expect(events).To(HaveLen(1),
			"idempotent re-scan MUST NOT emit duplicate audit events")
	})
})
