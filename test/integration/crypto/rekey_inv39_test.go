// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

// rekey_inv39_test.go — E2E specs for INV-CRYPTO-22: hot→cold-tier fallback when
// the hot DEK is destroyed after a completed rekey.
//
// Verifies:
//   - E2E_INV39_ColdFallback: after rekey completes and destroys the old DEK,
//     FallbackResolver.Resolve returns TierColdFallback when the event is
//     found in the cold tier (Phase 3 re-encrypted it with the new DEK).
//   - Double-miss path: when the cold-tier row is deleted, Resolve returns
//     ErrMetadataOnly.
//
// Implementation: seeds one XChaCha20-encrypted events_audit row under the
// initial DEK (so Phase 3 will re-encrypt it), runs a full rekey via the
// admin UDS (which destroys the old DEK at Phase 6), then exercises
// FallbackResolver directly — the same component that the history dispatcher
// wires in production.
//
// Part of holomush-jxo8.7 (bead jxo8.7.39, merged T45+T46+T47).
package crypto_test

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/protobuf/proto"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/codec"
	"github.com/holomush/holomush/internal/eventbus/crypto/aad"
	"github.com/holomush/holomush/internal/eventbus/history/source"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/pgnanos"
	eventbusv1 "github.com/holomush/holomush/pkg/proto/holomush/eventbus/v1"
)

// directColdLookup implements source.ColdTierLookup against events_audit
// via a direct pgxpool query. Used by the INV-CRYPTO-22 E2E test to avoid a
// dependency on history.newPostgresColdTier (which is unexported).
type directColdLookup struct {
	pool *pgxpool.Pool
}

func (d *directColdLookup) LookupByID(ctx context.Context, id eventbus.EventID) (eventbus.Envelope, bool, error) {
	var (
		idBytes    []byte
		subject    string
		evType     string
		envelopeB  []byte
		codecName  string
		dekRef     *int64
		dekVersion *uint32
		// events_audit.timestamp is BIGINT-ns post-gfo6 (INV-STORE-1);
		// scan target must be pgnanos.Time. ts is unused downstream
		// (env is built from envelope bytes), so this is a position
		// placeholder for Scan's column-count alignment.
		ts pgnanos.Time
	)
	err := d.pool.QueryRow(
		ctx,
		`SELECT id, subject, type, envelope, codec, dek_ref, dek_version, timestamp
		   FROM events_audit
		  WHERE id = $1`,
		id[:],
	).Scan(&idBytes, &subject, &evType, &envelopeB, &codecName, &dekRef, &dekVersion, &ts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return eventbus.Envelope{}, false, nil
		}
		return eventbus.Envelope{}, false, err //nolint:wrapcheck // direct error from pgx; callers wrap as needed
	}
	var keyID codec.KeyID
	if dekRef != nil {
		keyID = codec.KeyID(*dekRef) //nolint:gosec // G115: dek_ref is a BIGSERIAL PK; always non-negative
	}
	var keyVersion uint32
	if dekVersion != nil {
		keyVersion = *dekVersion
	}
	env := eventbus.NewEnvelopeFromColdRow(eventbus.ColdRow{
		EventID:    id,
		Payload:    envelopeB,
		Codec:      codecName,
		KeyID:      keyID,
		KeyVersion: keyVersion,
	})
	return env, true, nil
}

// insertEncryptedAuditRow seeds one events_audit row encrypted under the
// given DEK (dekID, dekVersion, key). Returns the ULID assigned to the row.
// The subject is "events.g1.scene.01ABC.ic" — same as the harness fixture,
// but with the actual DEK columns set so Phase 3 will re-encrypt it.
func insertEncryptedAuditRow(
	ctx context.Context,
	pool *pgxpool.Pool,
	dekID int64,
	dekVersion uint32,
	key codec.Key,
	plaintext []byte,
) (ulid.ULID, error) {
	id := idgen.New()

	envelope := &eventbusv1.Event{
		Id:      id[:],
		Subject: "events.g1.scene.01ABC.sensitive",
		Type:    "test.sensitive",
		Actor:   &eventbusv1.Actor{Kind: eventbusv1.ActorKind_ACTOR_KIND_SYSTEM},
	}

	aadBytes, err := aad.Build(envelope, string(codec.NameXChaCha20v1), uint64(dekID), dekVersion) //nolint:gosec // G115: dekID is a BIGSERIAL PK
	if err != nil {
		return ulid.ULID{}, err
	}

	c := codec.NewXChaCha20Poly1305v1()
	ciphertext, err := c.Encode(ctx, plaintext, key, aadBytes)
	if err != nil {
		return ulid.ULID{}, err
	}

	envelope.Payload = ciphertext
	envelopeBytes, err := proto.MarshalOptions{Deterministic: true}.Marshal(envelope)
	if err != nil {
		return ulid.ULID{}, err
	}

	// timestamp is BIGINT-ns post-gfo6 (INV-STORE-1). event_ms (000052 partition
	// key) is derived from the now-based ULID id — lands in the current partition.
	// event_ms/timestamp are NOT AAD inputs (aad.Build binds the envelope proto).
	eventMS := int64(id.Time()) * int64(time.Millisecond)
	_, execErr := pool.Exec(ctx, `
		INSERT INTO events_audit
		  (id, subject, type, timestamp, actor_kind, envelope, schema_ver,
		   codec, js_seq, rendering, dek_ref, dek_version, event_ms)
		VALUES ($1, 'events.g1.scene.01ABC.sensitive', 'test.sensitive',
		        (EXTRACT(EPOCH FROM now()) * 1e9)::BIGINT,
		        'system', $2, 1, $3, $4, '{}'::jsonb, $5, $6, $7)
	`, id[:], envelopeBytes, string(codec.NameXChaCha20v1),
		int64(time.Now().UnixNano()),
		dekID, int32(dekVersion), //nolint:gosec // G115: dekVersion fits in int32 for column storage
		eventMS)
	return id, execErr
}

var _ = Describe("INV-CRYPTO-22 cold-tier fallback", func() {
	It("E2E_INV39_ColdFallback: substitutes cold envelope when hot DEK is destroyed", func() {
		h := SetupRekeyHarness(suiteT, WithEventCount(5))
		defer h.Cleanup()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Obtain the initial DEK for the scene context so we can seed an
		// encrypted row that Phase 3 will re-encrypt during rekey.
		dekMgr := h.Primary.GetDEKManager()
		initialKey, err := dekMgr.GetOrCreate(ctx, h.SceneContext, nil)
		Expect(err).NotTo(HaveOccurred(), "GetOrCreate must succeed for scene context")
		Expect(initialKey.ID).NotTo(BeZero(), "initial DEK ID must be non-zero")

		// Seed one encrypted events_audit row under the initial DEK.
		// This row will be re-encrypted by Phase 3 during the rekey.
		const plaintext = "hello-inv39-fallback"
		eventID, err := insertEncryptedAuditRow(
			ctx, h.DB,
			int64(initialKey.ID), //nolint:gosec // G115: KeyID is codec.KeyID (uint64-based); cast safe for BIGSERIAL PK
			initialKey.Version,
			initialKey,
			[]byte(plaintext),
		)
		Expect(err).NotTo(HaveOccurred(), "seeding encrypted event must succeed")

		// Run the full rekey. Phase 3 re-encrypts our seeded row to new DEK.
		// Phase 6 destroys the old DEK (sets destroyed_at, evicts cache).
		_, rekeyErr := runRekeyViaUDS(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			"inv39 cold-fallback E2E test",
		)
		Expect(rekeyErr).NotTo(HaveOccurred(), "rekey must complete successfully")

		// At this point: old DEK is destroyed (destroyed_at set + cache evicted).
		// The events_audit row has been re-encrypted under the new DEK by Phase 3.
		// Resolving via the hot envelope (which references the OLD dek_id) MUST
		// fall back to the cold tier and return TierColdFallback.

		// Build a FallbackResolver using the harness's DEK manager and a direct
		// cold-tier lookup that queries events_audit by event ID.
		reg := prometheus.NewRegistry()
		metrics := source.NewMetrics(reg)
		coldLookup := &directColdLookup{pool: h.DB}
		fallback := source.NewFallbackResolver(dekMgr, coldLookup, metrics, slog.Default())

		// Build a hot envelope mimicking the JetStream message for the encrypted
		// event: event ID is the ULID we seeded, codec is XChaCha20, DEK fields
		// reference the OLD DEK (which is now destroyed).
		hotEnv := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
			EventID:    eventID,
			Codec:      codec.NameXChaCha20v1,
			KeyID:      initialKey.ID,
			KeyVersion: initialKey.Version,
		})

		// INV-CRYPTO-22: FallbackResolver MUST return TierColdFallback because the hot
		// DEK is destroyed but the cold-tier row (re-encrypted by Phase 3) exists
		// with the new DEK, which IS resolvable.
		resolved, resolveErr := fallback.Resolve(ctx, hotEnv)
		Expect(resolveErr).NotTo(HaveOccurred(),
			"INV-CRYPTO-22: FallbackResolver must not error when cold-tier row exists with resolvable DEK")
		Expect(resolved.SourceTier).To(Equal(source.TierColdFallback),
			"INV-CRYPTO-22: cold-tier fallback must be used when hot DEK is destroyed")
		Expect(resolved.Key.Bytes).NotTo(BeEmpty(),
			"INV-CRYPTO-22: resolved key bytes must be non-empty for cold-tier substitution")
	})

	It("delivers metadata_only on double miss", func() {
		h := SetupRekeyHarness(suiteT, WithEventCount(5))
		defer h.Cleanup()

		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Same setup: seed an encrypted row + run rekey.
		dekMgr := h.Primary.GetDEKManager()
		initialKey, err := dekMgr.GetOrCreate(ctx, h.SceneContext, nil)
		Expect(err).NotTo(HaveOccurred())

		const plaintext = "hello-inv39-double-miss"
		eventID, err := insertEncryptedAuditRow(
			ctx, h.DB,
			int64(initialKey.ID), //nolint:gosec // G115: KeyID is codec.KeyID (uint64-based); cast safe for BIGSERIAL PK
			initialKey.Version,
			initialKey,
			[]byte(plaintext),
		)
		Expect(err).NotTo(HaveOccurred())

		_, rekeyErr := runRekeyViaUDS(
			h,
			h.AdminPlayer.PlayerID,
			h.SceneContext.Type,
			h.SceneContext.ID,
			"inv39 double-miss E2E test",
		)
		Expect(rekeyErr).NotTo(HaveOccurred())

		// After rekey: delete the cold-tier row to simulate a double miss —
		// neither the hot DEK nor the cold-tier row is available.
		_, delErr := h.DB.Exec(ctx,
			`DELETE FROM events_audit WHERE id = $1`, eventID[:])
		Expect(delErr).NotTo(HaveOccurred(), "DELETE of cold-tier row must succeed")

		reg := prometheus.NewRegistry()
		metrics := source.NewMetrics(reg)
		coldLookup := &directColdLookup{pool: h.DB}
		fallback := source.NewFallbackResolver(dekMgr, coldLookup, metrics, slog.Default())

		hotEnv := eventbus.NewEnvelopeForTest(eventbus.EnvelopeFields{
			EventID:    eventID,
			Codec:      codec.NameXChaCha20v1,
			KeyID:      initialKey.ID,
			KeyVersion: initialKey.Version,
		})

		// INV-CRYPTO-22 double-miss: hot DEK destroyed + cold-tier row deleted →
		// FallbackResolver MUST return ErrMetadataOnly.
		_, resolveErr := fallback.Resolve(ctx, hotEnv)
		Expect(errors.Is(resolveErr, source.ErrMetadataOnly)).To(BeTrue(),
			"INV-CRYPTO-22 double-miss: ErrMetadataOnly must be returned when both tiers are unavailable")
	})
})
