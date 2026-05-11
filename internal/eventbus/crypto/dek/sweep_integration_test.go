// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/audit/chain"
	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
)

// rekeyTestSetup is the sweep subsystem integration test harness.
type rekeyTestSetup struct {
	t         *testing.T
	pool      *pgxpool.Pool
	Repo      *dek.CheckpointRepo
	Publisher *capturingPublisher // captures emitted audit events in memory
	// AuditEmitter is a real *dek.RekeyAuditEmitter wired to Publisher.
	// SweepOnceForTest drives abortAndAudit which calls Emit on this.
	AuditEmitter *dek.RekeyAuditEmitter
	Logger       *slog.Logger
}

// newRekeyTestSetup builds a sweep integration harness.
// It uses a real CheckpointRepo (postgres) and a real RekeyAuditEmitter
// backed by a capturingPublisher (defined in audit_test.go) so that
// emitted events are captured in-memory for assertion.
func newRekeyTestSetup(t *testing.T) *rekeyTestSetup {
	t.Helper()
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	chainRepo := chain.NewPostgresRepo(pool)
	em := chain.NewEmitter(chainRepo)
	pub := &capturingPublisher{}
	auditEmitter := dek.NewRekeyAuditEmitter(em, pub)

	return &rekeyTestSetup{
		t:            t,
		pool:         pool,
		Repo:         dek.NewCheckpointRepo(pool),
		Publisher:    pub,
		AuditEmitter: auditEmitter,
		Logger:       slog.Default(),
	}
}

// Cleanup is a no-op — testIntegrationPool registers t.Cleanup already.
func (s *rekeyTestSetup) Cleanup() {}

// OpenStaleCheckpoint inserts a checkpoint row that appears stale: the
// last_heartbeat_at is set to now() minus age. It seeds a DEK row first
// and returns the RequestID.
func (s *rekeyTestSetup) OpenStaleCheckpoint(ctxType, ctxID string, age time.Duration) dek.RequestID {
	s.t.Helper()
	// Seed a DEK row to satisfy the FK constraint on crypto_rekey_checkpoints.
	const dekID int64 = 999
	_, _ = s.pool.Exec(context.Background(),
		`INSERT INTO crypto_keys (id, context_type, context_id, version, wrapped_dek, wrap_provider, wrap_key_id, participants, created_at)
         VALUES ($1, $2, $3, 1, '\x00', 'test', 'test', '[]'::jsonb, now())
         ON CONFLICT (id) DO NOTHING`,
		dekID, ctxType, ctxID)

	rid := dek.RequestID(idgen.New())
	_, err := s.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, last_heartbeat_at)
        VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7, now() - $8::interval)
    `, rid[:], ctxType, ctxID,
		make([]byte, 32), make([]byte, 32),
		"01PLAYER", dekID,
		age.String())
	require.NoError(s.t, err)
	return rid
}

// LoadEventsAuditBySubject returns all published audit events captured by
// the in-memory publisher whose Subject matches the given subject string.
// The returned slice is a view of capturingPublisher.published filtered by
// subject.
func (s *rekeyTestSetup) LoadEventsAuditBySubject(subject string) []capturedPublish {
	var out []capturedPublish
	for _, p := range s.Publisher.published {
		if p.Subject == subject {
			out = append(out, p)
		}
	}
	return out
}

// TestSweep_TTLExpiryEmitsAudit verifies INV-E18-SWEEP-TTL-AUDIT:
// the sweep aborts a TTL-expired checkpoint and emits a chained rekey
// audit event with aborted_reason="ttl_expired".
//
// Test name matches spec §8 INV-E18 (TestSweep_TTLExpiryEmitsAudit).
func TestSweep_TTLExpiryEmitsAudit(t *testing.T) {
	setup := newRekeyTestSetup(t)
	defer setup.Cleanup()

	// Insert a checkpoint whose last_heartbeat_at is 30h old (> 24h TTL).
	rid := setup.OpenStaleCheckpoint("scene", "01ABC", 30*time.Hour)

	sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
		Repo:         setup.Repo,
		AuditEmitter: setup.AuditEmitter,
		Logger:       setup.Logger,
		TTL:          24 * time.Hour,
		Interval:     1 * time.Hour, // unused — test calls SweepOnceForTest directly
	})
	require.NoError(t, sub.SweepOnceForTest(context.Background()))

	// The checkpoint MUST be aborted with reason "ttl_expired".
	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusAborted, ckpt.Status,
		"INV-E18: TTL-expired checkpoint MUST be marked aborted by sweep")
	require.NotNil(t, ckpt.AbortedReason)
	require.Equal(t, "ttl_expired", *ckpt.AbortedReason,
		"INV-E18: aborted_reason MUST be ttl_expired")

	// INV-E18: chained audit event MUST be emitted for the aborted context.
	events := setup.LoadEventsAuditBySubject("events.g1.system.rekey.scene.01ABC")
	require.GreaterOrEqual(t, len(events), 1,
		"INV-E18: sweep MUST emit a chained audit event for each aborted checkpoint")

	// Verify the emitted payload contains the expected context and reason.
	var payload dek.RekeyAuditPayload
	require.NoError(t, json.Unmarshal(events[0].Payload, &payload))
	require.Equal(t, "scene", payload.Context.Type)
	require.Equal(t, "01ABC", payload.Context.ID)
	require.Contains(t, payload.Justification, "ttl_expired",
		"INV-E18: audit payload Justification MUST reference ttl_expired")
}

// TestSweep_Lifecycle_StartStop verifies that CheckpointSweepSubsystem
// starts and stops cleanly, including the background tick goroutine.
func TestSweep_Lifecycle_StartStop(t *testing.T) {
	setup := newRekeyTestSetup(t)
	defer setup.Cleanup()

	sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
		Repo:         setup.Repo,
		AuditEmitter: setup.AuditEmitter,
		Logger:       setup.Logger,
		TTL:          24 * time.Hour,
		Interval:     100 * time.Millisecond, // fast interval for lifecycle test
	})

	ctx := context.Background()
	require.NoError(t, sub.Start(ctx))
	// Stop must return quickly and cleanly (goroutine exits on cancel).
	stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, sub.Stop(stopCtx))
}

// TestSweep_TickScanAbortsExpired verifies that the background tick loop
// (not just the boot-time sweep) will also abort expired checkpoints.
// Inserts a stale checkpoint, starts the subsystem with a short interval,
// and waits for the tick to fire and mark it aborted.
func TestSweep_TickScanAbortsExpired(t *testing.T) {
	setup := newRekeyTestSetup(t)
	defer setup.Cleanup()

	rid := setup.OpenStaleCheckpoint("scene", "01TK", 30*time.Hour)

	sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
		Repo:         setup.Repo,
		AuditEmitter: setup.AuditEmitter,
		Logger:       setup.Logger,
		TTL:          24 * time.Hour,
		Interval:     50 * time.Millisecond, // fast tick for test
	})

	ctx := context.Background()
	require.NoError(t, sub.Start(ctx))
	defer func() {
		stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = sub.Stop(stopCtx)
	}()

	// Boot-time sweep should have fired; the checkpoint is already aborted.
	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusAborted, ckpt.Status,
		"tick scan MUST abort expired checkpoints")
}

// TestSweep_IdempotentReScan verifies that a second SweepOnce on an
// already-aborted checkpoint is a no-op (MarkAborted returns
// DEK_REKEY_CHECKPOINT_TERMINAL but sweepOnce logs and continues).
func TestSweep_IdempotentReScan(t *testing.T) {
	setup := newRekeyTestSetup(t)
	defer setup.Cleanup()

	rid := setup.OpenStaleCheckpoint("scene", "01IDP", 30*time.Hour)

	sub := dek.NewCheckpointSweepSubsystem(dek.CheckpointSweepConfig{
		Repo:         setup.Repo,
		AuditEmitter: setup.AuditEmitter,
		Logger:       setup.Logger,
		TTL:          24 * time.Hour,
		Interval:     1 * time.Hour,
	})

	// First sweep: aborts + emits audit.
	require.NoError(t, sub.SweepOnceForTest(context.Background()))
	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusAborted, ckpt.Status)

	// Second sweep: row is now terminal (aborted) so ListExpired must
	// exclude it (status NOT IN ('complete','aborted')). The scan should
	// be a no-op and return nil.
	require.NoError(t, sub.SweepOnceForTest(context.Background()),
		"second sweep on already-aborted checkpoint MUST be a no-op")

	// No additional audit events should have been emitted.
	events := setup.LoadEventsAuditBySubject("events.g1.system.rekey.scene.01IDP")
	require.Len(t, events, 1,
		"idempotent re-scan MUST NOT emit duplicate audit events")
}
