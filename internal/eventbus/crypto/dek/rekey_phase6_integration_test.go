// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// phase6TestSetupImpl is the Phase 6 integration test harness.
// Builds on the Phase 5 harness shape: real pool, real Manager
// (which implements dek.DEKDestroyer via DestroyDEK + EvictCachedDEK),
// and a fake Phase5Coordinator so RunPhase5 can advance to phase5_invalidate.
type phase6TestSetupImpl struct {
	t        *testing.T
	pool     *pgxpool.Pool
	Orch     *dek.Orchestrator
	Repo     *dek.CheckpointRepo
	Manager  dek.Manager
	oldDEKID int64
	newDEKID int64
}

// newPhase6TestSetup builds a harness ready to drive RunPhase6.
func newPhase6TestSetup(t *testing.T) *phase6TestSetupImpl {
	t.Helper()
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	provider := newTestProvider(t)
	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})

	mgr, err := dek.NewManager(provider, store, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	require.NoError(t, err)

	ctxID := dek.ContextID{Type: "scene", ID: "01PH6"}
	_, err = mgr.GetOrCreate(context.Background(), ctxID, nil)
	require.NoError(t, err, "seed old DEK via GetOrCreate")

	oldRecord, err := mgr.ActiveDEKRow(context.Background(), ctxID)
	require.NoError(t, err)
	newDEKID, err := mgr.MintNewDEKForRekey(context.Background(), oldRecord.ID)
	require.NoError(t, err)

	repo := dek.NewCheckpointRepo(pool)
	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)

	coord := &fakePhase5Coordinator{}
	orch.SetPhase5Coordinator(coord)
	// Wire Phase 6 DEKDestroyer — *manager satisfies DEKDestroyer via
	// DestroyDEK + EvictCachedDEK.
	orch.SetDestroyer(mgr)

	return &phase6TestSetupImpl{
		t:        t,
		pool:     pool,
		Orch:     orch,
		Repo:     repo,
		Manager:  mgr,
		oldDEKID: oldRecord.ID,
		newDEKID: newDEKID,
	}
}

// RunUpToPhase5Complete seeds a checkpoint row at the FSM state that
// Phase 5 leaves behind on a clean N-of-N success:
// status=phase5_invalidate, phase5_missing_members IS NULL.
// Bypasses RunPhase1Fresh through RunPhase5 since Phase 6's contract
// under test is independent of how the checkpoint reached phase5_invalidate.
func (s *phase6TestSetupImpl) RunUpToPhase5Complete() dek.RequestID {
	s.t.Helper()
	rid := dek.RequestID(idgen.New())
	_, err := s.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, 'scene', '01PH6', $2, $3, '01PLAYER',
                'phase5_invalidate', $4, $5)
    `, rid[:], make([]byte, 32), make([]byte, 32), s.oldDEKID, s.newDEKID)
	require.NoError(s.t, err)
	return rid
}

// loadDestroyedAt reads the destroyed_at column from the crypto_keys row
// for the given dekID. Returns nil if the row has not yet been destroyed.
func (s *phase6TestSetupImpl) loadDestroyedAt(dekID int64) *time.Time {
	s.t.Helper()
	var destroyedAt *time.Time
	err := s.pool.QueryRow(context.Background(),
		`SELECT destroyed_at FROM crypto_keys WHERE id = $1`, dekID,
	).Scan(&destroyedAt)
	require.NoError(s.t, err)
	return destroyedAt
}

// TestOrchestrator_Phase6_DestroyOldDEK_Idempotent verifies:
//   - RunPhase6 advances checkpoint status to phase6_destroy_old (INV-E1)
//   - old crypto_keys row has destroyed_at set after Phase 6 (INV-E12)
//   - a second RunPhase6 invocation is a no-op (INV-E12-PHASE6-IDEMPOTENT)
func TestOrchestrator_Phase6_DestroyOldDEK_Idempotent(t *testing.T) {
	setup := newPhase6TestSetup(t)
	rid := setup.RunUpToPhase5Complete()

	// First invocation: should destroy old DEK and advance status.
	require.NoError(t, setup.Orch.RunPhase6(context.Background(), rid))

	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase6DestroyOld, ckpt.Status,
		"INV-E1: checkpoint must advance to phase6_destroy_old after RunPhase6")

	destroyedAt := setup.loadDestroyedAt(ckpt.OldDEKID)
	require.NotNil(t, destroyedAt,
		"INV-E12: old DEK row must have destroyed_at set after Phase 6")

	// Second invocation: idempotent — must succeed without error.
	require.NoError(t, setup.Orch.RunPhase6(context.Background(), rid),
		"INV-E12-PHASE6-IDEMPOTENT: second RunPhase6 on phase6_destroy_old must be a no-op")

	// Status must remain phase6_destroy_old (not re-transitioned).
	ckpt2, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase6DestroyOld, ckpt2.Status,
		"INV-E12-PHASE6-IDEMPOTENT: status must remain phase6_destroy_old after idempotent re-invoke")
}

// TestOrchestrator_Phase6_RequiresPreconditionPhase5Complete verifies:
//   - RunPhase6 rejects a checkpoint not in a valid Phase 6 entry state
//     with DEK_REKEY_PHASE_PRECONDITION_FAILED (INV-E1 FSM guard)
func TestOrchestrator_Phase6_RequiresPreconditionPhase5Complete(t *testing.T) {
	setup := newPhase6TestSetup(t)

	// Seed a checkpoint at 'pending' — not a valid Phase 6 entry point.
	rid := dek.RequestID(idgen.New())
	_, err := setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, 'scene', '01PH6B', $2, $3, '01PLAYER',
                'pending', $4, $5)
    `, rid[:], make([]byte, 32), make([]byte, 32), setup.oldDEKID, setup.newDEKID)
	require.NoError(t, err)

	runErr := setup.Orch.RunPhase6(context.Background(), rid)
	require.Error(t, runErr)
	errutil.AssertErrorCode(t, runErr, "DEK_REKEY_PHASE_PRECONDITION_FAILED")
}

// TestOrchestrator_Phase6_NoDestroyer_FailsClosed verifies that calling
// RunPhase6 without wiring a DEKDestroyer returns DEK_REKEY_DESTROYER_NIL
// rather than panicking (fail-closed per the SetPhase5Coordinator pattern).
func TestOrchestrator_Phase6_NoDestroyer_FailsClosed(t *testing.T) {
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	provider := newTestProvider(t)
	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
	mgr, err := dek.NewManager(provider, store, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	require.NoError(t, err)

	ctxID := dek.ContextID{Type: "scene", ID: "01PH6C"}
	_, err = mgr.GetOrCreate(context.Background(), ctxID, nil)
	require.NoError(t, err)
	oldRecord, err := mgr.ActiveDEKRow(context.Background(), ctxID)
	require.NoError(t, err)
	newDEKID, err := mgr.MintNewDEKForRekey(context.Background(), oldRecord.ID)
	require.NoError(t, err)

	repo := dek.NewCheckpointRepo(pool)
	// Intentionally NOT calling SetDEKDestroyer.
	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)

	rid := dek.RequestID(idgen.New())
	_, insertErr := pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, 'scene', '01PH6C', $2, $3, '01PLAYER',
                'phase5_invalidate', $4, $5)
    `, rid[:], make([]byte, 32), make([]byte, 32), oldRecord.ID, newDEKID)
	require.NoError(t, insertErr)

	runErr := orch.RunPhase6(context.Background(), rid)
	require.Error(t, runErr)
	errutil.AssertErrorCode(t, runErr, "DEK_REKEY_DESTROYER_NIL")
}
