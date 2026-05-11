// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// fakeAuditEmitter is a test-double for *dek.RekeyAuditEmitter. It captures
// the payloads passed to Emit and can be configured to return an error.
type fakeAuditEmitter struct {
	emitted []dek.RekeyAuditPayload
	failErr error
}

// Emit records the payload and returns the configured error (if any).
func (f *fakeAuditEmitter) Emit(_ context.Context, p dek.RekeyAuditPayload) (ulid.ULID, dek.RekeyAuditPayload, error) {
	if f.failErr != nil {
		return ulid.ULID{}, p, f.failErr
	}
	f.emitted = append(f.emitted, p)
	return ulid.Make(), p, nil
}

// SetEmitErrorForTest makes the next Emit call return err.
func (f *fakeAuditEmitter) SetEmitErrorForTest(err error) { f.failErr = err }

// phase7TestSetupImpl is the Phase 7 integration test harness.
// Builds on the Phase 6 harness shape.
type phase7TestSetupImpl struct {
	t            *testing.T
	pool         *pgxpool.Pool
	Orch         *dek.Orchestrator
	Repo         *dek.CheckpointRepo
	Manager      dek.Manager
	AuditEmitter *fakeAuditEmitter
	DataDir      string
	oldDEKID     int64
	newDEKID     int64
}

// newPhase7TestSetup builds a harness ready to drive RunPhase7.
func newPhase7TestSetup(t *testing.T) *phase7TestSetupImpl {
	t.Helper()
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	provider := newTestProvider(t)
	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: time.Minute})

	mgr, err := dek.NewManager(
		provider, store, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	require.NoError(t, err)

	ctxID := dek.ContextID{Type: "scene", ID: "01PH7"}
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
	orch.SetDestroyer(mgr)

	auditEmitter := &fakeAuditEmitter{}
	orch.SetAuditEmitter(auditEmitter)

	dataDir := t.TempDir()
	orch.SetDataDir(dataDir)

	return &phase7TestSetupImpl{
		t:            t,
		pool:         pool,
		Orch:         orch,
		Repo:         repo,
		Manager:      mgr,
		AuditEmitter: auditEmitter,
		DataDir:      dataDir,
		oldDEKID:     oldRecord.ID,
		newDEKID:     newDEKID,
	}
}

// RunUpToPhase6Complete seeds a checkpoint row at the FSM state that
// Phase 6 leaves behind on success: status=phase6_destroy_old.
// Bypasses RunPhase1Fresh through RunPhase6 since Phase 7's contract
// under test is independent of how the checkpoint reached phase6_destroy_old.
func (s *phase7TestSetupImpl) RunUpToPhase6Complete() dek.RequestID {
	s.t.Helper()
	rid := dek.RequestID(idgen.New())
	_, err := s.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, 'scene', '01PH7', $2, $3, '01PRIM',
                'phase6_destroy_old', $4, $5)
    `, rid[:], make([]byte, 32), make([]byte, 32), s.oldDEKID, s.newDEKID)
	require.NoError(s.t, err)
	return rid
}

// RunUpToPhase6CompleteWithForceDestroy seeds a checkpoint row at phase6_destroy_old
// with force_destroy=true and phase5_missing_members populated to simulate
// the force-destroy path for INV-E11 assertions.
func (s *phase7TestSetupImpl) RunUpToPhase6CompleteWithForceDestroy(missingMembers string) dek.RequestID {
	s.t.Helper()
	rid := dek.RequestID(idgen.New())
	_, err := s.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id,
           force_destroy, phase5_missing_members)
        VALUES ($1, 'scene', '01PH7FD', $2, $3, '01PRIM',
                'phase6_destroy_old', $4, $5,
                true, $6::jsonb)
    `, rid[:], make([]byte, 32), make([]byte, 32), s.oldDEKID, s.newDEKID, missingMembers)
	require.NoError(s.t, err)
	return rid
}

// TestOrchestrator_Phase7_EmitsChainedAudit_AdvancesToComplete verifies:
//   - RunPhase7 emits the rekey audit event via the audit emitter (INV-E14)
//   - outcome.AuditEventID is non-empty
//   - checkpoint advances to status=complete (INV-E1)
//   - completed_at is set
func TestOrchestrator_Phase7_EmitsChainedAudit_AdvancesToComplete(t *testing.T) {
	setup := newPhase7TestSetup(t)
	rid := setup.RunUpToPhase6Complete()

	outcome, err := setup.Orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{
		ContextType:   "scene",
		ContextID:     "01PH7",
		Justification: "test",
		Operator:      dek.OperatorIdentity{PlayerID: "01PRIM"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, outcome.AuditEventID,
		"INV-E14: AuditEventID must be populated after Phase 7")

	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusComplete, ckpt.Status,
		"INV-E1: checkpoint must advance to complete after RunPhase7")
	require.NotNil(t, ckpt.CompletedAt,
		"completed_at must be set after Phase 7")

	// Verify the audit emitter received the payload.
	require.Len(t, setup.AuditEmitter.emitted, 1)
	emittedPayload := setup.AuditEmitter.emitted[0]
	require.Equal(t, "scene", emittedPayload.Context.Type)
	require.Equal(t, "01PH7", emittedPayload.Context.ID)
}

// TestOrchestrator_Phase7_AuditEmitFailure_FallbackLog verifies (INV-E13):
//   - on audit emit failure, RunPhase7 returns DEK_REKEY_PHASE7_AUDIT_FAILED
//   - the fallback log file is written at <data_dir>/audit-fallback/rekey-<rid>.log
//   - the checkpoint does NOT advance to complete (DB state is preserved)
func TestOrchestrator_Phase7_AuditEmitFailure_FallbackLog(t *testing.T) {
	setup := newPhase7TestSetup(t)
	rid := setup.RunUpToPhase6Complete()
	setup.AuditEmitter.SetEmitErrorForTest(errors.New("simulated emit failure"))

	_, err := setup.Orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{
		ContextType:   "scene",
		ContextID:     "01PH7",
		Justification: "test",
	})
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_PHASE7_AUDIT_FAILED")

	// INV-E13: fallback log written to <data_dir>/audit-fallback/rekey-<rid>.log.
	logPath := filepath.Join(setup.DataDir, "audit-fallback", "rekey-"+rid.String()+".log")
	require.FileExists(t, logPath,
		"INV-E13: fallback log must be written on audit emit failure")

	// The checkpoint must NOT be at complete — rekey DB state is committed,
	// but the phase7_audit status means retry is possible.
	ckpt, cerr := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, cerr)
	require.NotEqual(t, dek.CheckpointStatusComplete, ckpt.Status,
		"INV-E13: checkpoint must not advance to complete on audit emit failure")
}

// TestOrchestrator_Phase7_ForceDestroyPath_AuditCaptures verifies (INV-E11):
//   - when force_destroy=true on the checkpoint, the emitted payload has
//     ForceDestroy=true and the missing_members list is non-nil.
func TestOrchestrator_Phase7_ForceDestroyPath_AuditCaptures(t *testing.T) {
	setup := newPhase7TestSetup(t)
	rid := setup.RunUpToPhase6CompleteWithForceDestroy(`["m1","m2"]`)

	_, err := setup.Orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{
		ContextType:   "scene",
		ContextID:     "01PH7FD",
		Justification: "force destroy test",
		Operator:      dek.OperatorIdentity{PlayerID: "01PRIM"},
	})
	require.NoError(t, err)

	// INV-E11: audit payload must capture force_destroy and missing_members.
	require.Len(t, setup.AuditEmitter.emitted, 1)
	p := setup.AuditEmitter.emitted[0]
	require.True(t, p.ForceDestroy,
		"INV-E11: audit payload must have force_destroy=true")
	require.ElementsMatch(t, []string{"m1", "m2"}, p.Phases.Phase5FinalMissingMembers,
		"INV-E11: audit payload must embed final_missing_members from the checkpoint")
}

// TestOrchestrator_Phase7_RequiresPreconditionPhase6Complete verifies:
//   - RunPhase7 with a checkpoint not in phase6_destroy_old returns
//     DEK_REKEY_PHASE_PRECONDITION_FAILED (INV-E1 FSM guard).
func TestOrchestrator_Phase7_RequiresPreconditionPhase6Complete(t *testing.T) {
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	// Seed a DEK row for this test (fixed id, no return value).
	const dekID = int64(500)
	seedDEKRow(t, pool, dekID, "scene", "01PH7PRE", 1)

	// Open a checkpoint at 'phase1_auth' — not a valid Phase 7 entry point.
	repo := dek.NewCheckpointRepo(pool)
	rid := dek.RequestID(idgen.New())
	_, err := pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id)
        VALUES ($1, 'scene', '01PH7PRE', $2, $3, '01PLAYER', 'phase1_auth', $4)
    `, rid[:], make([]byte, 32), make([]byte, 32), dekID)
	require.NoError(t, err)

	store := dek.NewStore(pool)
	provider := newTestProvider(t)
	mgr, merr := dek.NewManager(
		provider, store,
		dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
		dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	require.NoError(t, merr)

	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)
	orch.SetAuditEmitter(&fakeAuditEmitter{})
	orch.SetDataDir(t.TempDir())

	_, runErr := orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{
		ContextType: "scene", ContextID: "01PH7PRE",
	})
	require.Error(t, runErr)
	errutil.AssertErrorCode(t, runErr, "DEK_REKEY_PHASE_PRECONDITION_FAILED")
}

// TestOrchestrator_Phase7_NoAuditEmitter_FailsClosed verifies that calling
// RunPhase7 without wiring an AuditEmitter returns DEK_REKEY_AUDIT_EMITTER_NIL
// rather than panicking (fail-closed per the SetPhase5Coordinator pattern).
func TestOrchestrator_Phase7_NoAuditEmitter_FailsClosed(t *testing.T) {
	pool := testIntegrationPool(t)
	dek.SetGameIDForTest("g1")

	provider := newTestProvider(t)
	store := dek.NewStore(pool)
	mgr, merr := dek.NewManager(
		provider, store,
		dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
		dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: time.Minute}),
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	require.NoError(t, merr)

	repo := dek.NewCheckpointRepo(pool)
	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)
	// Deliberately NOT calling SetAuditEmitter.
	orch.SetDataDir(t.TempDir())

	rid := dek.RequestID(idgen.New())
	_, runErr := orch.RunPhase7(context.Background(), rid, dek.RekeyRequest{})
	require.Error(t, runErr)
	errutil.AssertErrorCode(t, runErr, "DEK_REKEY_AUDIT_EMITTER_NIL")
}
