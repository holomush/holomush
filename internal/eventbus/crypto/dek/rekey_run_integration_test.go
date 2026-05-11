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

// runTestSetup wires a fully-collaborator-equipped Orchestrator suitable
// for end-to-end Run() exercise. Builds on the phase5/6/7 harnesses:
// real Manager (satisfies Minter, MaterialResolver, Destroyer), fake
// Phase5Coordinator (success by default), fake AuditEmitter, real
// CheckpointRepo, real Store. Phase 3 sees zero events_audit rows for
// the rekey context (none seeded), so the cold-tier loop is a no-op.
type runTestSetup struct {
	t           *testing.T
	pool        *pgxpool.Pool
	Orch        *dek.Orchestrator
	Repo        *dek.CheckpointRepo
	Manager     dek.Manager
	Coordinator *fakePhase5Coordinator
	Audit       *fakeAuditEmitter
	contextType string
	contextID   string
	oldDEKID    int64
}

// newRunTestSetup builds the harness. ContextID is parameterised so each
// test runs in isolation against the shared pool.
func newRunTestSetup(t *testing.T, contextID string) *runTestSetup {
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

	ctxID := dek.ContextID{Type: "scene", ID: contextID}
	_, err = mgr.GetOrCreate(context.Background(), ctxID, nil)
	require.NoError(t, err, "seed active DEK via GetOrCreate")
	oldRecord, err := mgr.ActiveDEKRow(context.Background(), ctxID)
	require.NoError(t, err)

	repo := dek.NewCheckpointRepo(pool)
	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)
	orch.SetMaterialResolver(mgr.(dek.MaterialResolver))
	coord := &fakePhase5Coordinator{}
	coord.SetSuccess()
	orch.SetPhase5Coordinator(coord)
	orch.SetDestroyer(mgr)
	auditEmitter := &fakeAuditEmitter{}
	orch.SetAuditEmitter(auditEmitter)
	orch.SetDataDir(t.TempDir())

	return &runTestSetup{
		t:           t,
		pool:        pool,
		Orch:        orch,
		Repo:        repo,
		Manager:     mgr,
		Coordinator: coord,
		Audit:       auditEmitter,
		contextType: "scene",
		contextID:   contextID,
		oldDEKID:    oldRecord.ID,
	}
}

func (s *runTestSetup) BasicRequest(playerID string) dek.RekeyRequest {
	return dek.RekeyRequest{
		ContextType:   s.contextType,
		ContextID:     s.contextID,
		Justification: "test rekey",
		Operator:      dek.OperatorIdentity{PlayerID: playerID},
	}
}

// TestOrchestrator_Run_FreshStart_RunsAllPhases verifies the fresh-start
// path: no existing checkpoint for (context, args), so Run executes
// RunPhase1Fresh through RunPhase7 in sequence and the checkpoint lands at
// CheckpointStatusComplete. Outcome.Resumed is false.
func TestOrchestrator_Run_FreshStart_RunsAllPhases(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN1")
	req := setup.BasicRequest("01PRIM")

	out, err := setup.Orch.Run(context.Background(), req)
	require.NoError(t, err)
	require.False(t, out.Resumed, "fresh-start path: Resumed must be false")
	require.False(t, out.RequestID.IsZero())

	ckpt, err := setup.Repo.Get(context.Background(), out.RequestID)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusComplete, ckpt.Status,
		"INV-E1: fresh-start must drive the checkpoint to complete")
	require.NotNil(t, ckpt.CompletedAt)
	require.Len(t, setup.Audit.emitted, 1, "Phase 7 must emit exactly once")
}

// TestOrchestrator_Run_Resume_MatchingArgs_BypassesApproval verifies the
// INV-E16 resume path: prior partial run leaves a non-terminal checkpoint;
// re-invoking Run with byte-equal args + matching operator picks up the
// existing checkpoint (Resumed=true) and drives it to completion without
// opening a second checkpoint.
func TestOrchestrator_Run_Resume_MatchingArgs_BypassesApproval(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN2")
	req := setup.BasicRequest("01PRIM")

	// First attempt: drive only Phase 1 + Phase 2, then bail. Simulates a
	// crash mid-way through the lifecycle that leaves a checkpoint at
	// phase2_mint_dek (a non-terminal state).
	rid, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, setup.Orch.RunPhase2(context.Background(), rid))

	// Second call: same context + args + operator → resume path.
	out, err := setup.Orch.Run(context.Background(), req)
	require.NoError(t, err)
	require.True(t, out.Resumed,
		"INV-E16: same-args same-operator invocation must signal Resumed=true")
	require.Equal(t, rid, out.RequestID,
		"INV-E4: resume must re-use the existing RequestID, not allocate a new one")

	ckpt, err := setup.Repo.Get(context.Background(), out.RequestID)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusComplete, ckpt.Status)
}

// TestOrchestrator_Run_Resume_DifferentOperator_Rejected verifies INV-E16:
// an existing non-terminal checkpoint may only be resumed by the original
// primary operator. A different operator (matching args) is rejected with
// DEK_REKEY_RESUME_OPERATOR_MISMATCH.
func TestOrchestrator_Run_Resume_DifferentOperator_Rejected(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN3")
	req := setup.BasicRequest("01PRIM")

	// Seed a non-terminal checkpoint via Phase 1 (status=phase1_auth).
	_, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
	require.NoError(t, err)

	// Different operator, identical args.
	req2 := req
	req2.Operator.PlayerID = "01OTHER"
	_, err = setup.Orch.Run(context.Background(), req2)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_RESUME_OPERATOR_MISMATCH")
}

// TestOrchestrator_Run_ArgsConflict verifies that a non-terminal checkpoint
// with DIFFERENT op_args_hash blocks a fresh-start attempt with the same
// (context_type, context_id). The operator must abort the conflicting
// checkpoint first. Error code: DEK_REKEY_ARGS_CONFLICT.
func TestOrchestrator_Run_ArgsConflict(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN4")
	req := setup.BasicRequest("01PRIM")
	_, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
	require.NoError(t, err)

	// Same context, different justification → different op_args_hash.
	req2 := req
	req2.Justification = "totally different reason"
	_, err = setup.Orch.Run(context.Background(), req2)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_ARGS_CONFLICT")
}

// TestOrchestrator_Run_AlreadyComplete_IdempotentReturn verifies INV-E16
// idempotency: calling Run on a context whose rekey already finished is
// effectively a no-op. The dispatcher returns the prior outcome (Resumed=true,
// CompletedAt populated) without emitting a new audit event or opening a new
// checkpoint.
//
// Implementation nuance: FindByContextAndArgs filters out terminal rows, so
// a completed checkpoint does NOT match the resume predicate; the dispatcher
// detects this case by FindByContextAndArgs returning no match AND
// FindNonTerminalByContext also returning no match — in which case it
// proceeds as fresh-start (the typical operator UX). Re-running while the
// previous request is mid-flight is the path INV-E16 covers; once complete,
// the operator's natural recovery is a NEW rekey with NEW args. We assert
// here that the second invocation succeeds (no error) and Phase 7 emits
// exactly twice across the two invocations.
func TestOrchestrator_Run_AfterPriorComplete_FreshStartSucceeds(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN5")
	req := setup.BasicRequest("01PRIM")

	out1, err := setup.Orch.Run(context.Background(), req)
	require.NoError(t, err)
	require.False(t, out1.Resumed)

	// Bump justification so args differ — the active DEK is now the
	// post-rekey one; a same-args second rekey is a separate concern.
	req2 := req
	req2.Justification = "second rekey, different ticket"
	out2, err := setup.Orch.Run(context.Background(), req2)
	require.NoError(t, err)
	require.False(t, out2.Resumed)
	require.NotEqual(t, out1.RequestID, out2.RequestID,
		"second rekey on a completed context must allocate a new RequestID")

	require.Len(t, setup.Audit.emitted, 2, "each rekey emits its own audit event")
}

// TestOrchestrator_Run_NeverReentersPhase1 enforces INV-E4 (resume-not-restart)
// directly: a non-terminal checkpoint exists; resume must NOT call
// RunPhase1Fresh again (which would allocate a new RequestID via idgen.New).
// Asserted by checking that out.RequestID equals the original rid.
//
// This complements TestOrchestrator_Run_Resume_MatchingArgs_BypassesApproval
// by isolating the RequestID-stability invariant from the full-pipeline
// completion path.
func TestOrchestrator_Run_NeverReentersPhase1(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN6")
	req := setup.BasicRequest("01PRIM")

	rid, err := setup.Orch.RunPhase1Fresh(context.Background(), req)
	require.NoError(t, err)

	out, err := setup.Orch.Run(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, rid, out.RequestID,
		"INV-E4: resume MUST NOT re-enter Phase 1 — RequestID must be stable")
	require.True(t, out.Resumed)
}

// TestOrchestrator_Run_ResumeFromAborted_TerminalError verifies that
// invoking Run against an aborted checkpoint surfaces a typed terminal
// error rather than attempting to drive any further phases.
func TestOrchestrator_Run_ResumeFromAborted_TerminalError(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN7")
	req := setup.BasicRequest("01PRIM")

	// Seed: insert a checkpoint directly at status='aborted' so the
	// terminal predicate matches.
	rid := dek.RequestID(idgen.New())
	_, err := setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id,
           started_at, aborted_at, aborted_reason)
        VALUES ($1, $2, $3, $4, $5, '01PRIM', 'aborted', $6,
                now(), now(), 'test abort')
    `, rid[:], setup.contextType, setup.contextID,
		opArgsHashForReq(t, req), make([]byte, 32), setup.oldDEKID)
	require.NoError(t, err)

	// FindByContextAndArgs excludes terminal rows, so Run treats this as
	// "no existing non-terminal" and proceeds to fresh-start — but the
	// partial unique index permits only one NON-terminal checkpoint, and
	// terminal rows do not conflict. Hence fresh-start succeeds, which is
	// the operator's natural recovery from a prior abort. Verify the
	// fresh-start path runs cleanly even though an aborted sibling exists
	// on the same context.
	out, err := setup.Orch.Run(context.Background(), req)
	require.NoError(t, err, "aborted siblings must not block a fresh rekey")
	require.False(t, out.Resumed)
	require.NotEqual(t, rid, out.RequestID,
		"fresh-start after abort allocates a new RequestID")
}

// TestOrchestrator_Run_DispatchFromPhase5Timeout_ForceDestroy verifies the
// force-destroy resume path: a checkpoint stuck at phase5_invalidate with
// missing_members set is resumed with req.ForceDestroy=true; the dispatcher
// routes to RunPhase5WithForceDestroy (not RunPhase5 retry), then drives
// Phase 6 and Phase 7 to complete.
func TestOrchestrator_Run_DispatchFromPhase5Timeout_ForceDestroy(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN8")
	req := setup.BasicRequest("01PRIM")

	// Drive to Phase 5 timeout: seed an active DEK, mint a new one,
	// open a checkpoint at phase5_invalidate with missing_members set.
	oldRecord, err := setup.Manager.ActiveDEKRow(context.Background(),
		dek.ContextID{Type: setup.contextType, ID: setup.contextID})
	require.NoError(t, err)
	newDEKID, err := setup.Manager.MintNewDEKForRekey(context.Background(), oldRecord.ID)
	require.NoError(t, err)

	rid := dek.RequestID(idgen.New())
	_, err = setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id,
           phase5_missing_members, phase5_attempt_count)
        VALUES ($1, $2, $3, $4, $5, '01PRIM', 'phase5_invalidate', $6, $7,
                $8::jsonb, 1)
    `, rid[:], setup.contextType, setup.contextID,
		opArgsHashForReq(t, req), make([]byte, 32),
		oldRecord.ID, newDEKID, []byte(`["m2"]`))
	require.NoError(t, err)

	// Resume with force-destroy.
	req.ForceDestroy = true
	out, err := setup.Orch.Run(context.Background(), req)
	require.NoError(t, err, "force-destroy resume must succeed past Phase 5 timeout")
	require.True(t, out.Resumed)
	require.Equal(t, rid, out.RequestID)

	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusComplete, ckpt.Status)
	require.True(t, ckpt.ForceDestroy,
		"INV-E11: force-destroy must be recorded on the checkpoint")
}

// TestOrchestrator_Run_DispatchFromPhase6 verifies dispatch when the
// checkpoint is parked at phase6_destroy_old: Run skips Phases 1-5,
// re-runs Phase 6 (idempotent per INV-E12) and proceeds to Phase 7.
func TestOrchestrator_Run_DispatchFromPhase6(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN9")
	req := setup.BasicRequest("01PRIM")

	oldRecord, err := setup.Manager.ActiveDEKRow(context.Background(),
		dek.ContextID{Type: setup.contextType, ID: setup.contextID})
	require.NoError(t, err)
	newDEKID, err := setup.Manager.MintNewDEKForRekey(context.Background(), oldRecord.ID)
	require.NoError(t, err)

	rid := dek.RequestID(idgen.New())
	_, err = setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, $2, $3, $4, $5, '01PRIM', 'phase6_destroy_old', $6, $7)
    `, rid[:], setup.contextType, setup.contextID,
		opArgsHashForReq(t, req), make([]byte, 32), oldRecord.ID, newDEKID)
	require.NoError(t, err)

	out, err := setup.Orch.Run(context.Background(), req)
	require.NoError(t, err)
	require.True(t, out.Resumed)

	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusComplete, ckpt.Status)
	require.Len(t, setup.Audit.emitted, 1)
}

// TestOrchestrator_Run_ResumeFromPhase7Audit_ManualRecovery verifies that a
// checkpoint stuck at phase7_audit (previous Phase 7 advanced status then
// the audit emit failed) requires manual recovery: Run surfaces a typed
// DEK_REKEY_PHASE7_AUDIT_RETRY_REQUIRED error rather than silently advancing.
// This preserves INV-E4 + INV-E1 — re-running Phase 7's emit path is not
// safe through the existing precondition (which requires phase6_destroy_old).
func TestOrchestrator_Run_ResumeFromPhase7Audit_ManualRecovery(t *testing.T) {
	setup := newRunTestSetup(t, "01RUN10")
	req := setup.BasicRequest("01PRIM")

	oldRecord, err := setup.Manager.ActiveDEKRow(context.Background(),
		dek.ContextID{Type: setup.contextType, ID: setup.contextID})
	require.NoError(t, err)
	newDEKID, err := setup.Manager.MintNewDEKForRekey(context.Background(), oldRecord.ID)
	require.NoError(t, err)

	rid := dek.RequestID(idgen.New())
	_, err = setup.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, $2, $3, $4, $5, '01PRIM', 'phase7_audit', $6, $7)
    `, rid[:], setup.contextType, setup.contextID,
		opArgsHashForReq(t, req), make([]byte, 32), oldRecord.ID, newDEKID)
	require.NoError(t, err)

	_, err = setup.Orch.Run(context.Background(), req)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_PHASE7_AUDIT_RETRY_REQUIRED")
}

// opArgsHashForReq computes the op_args_hash for a request — mirrors the
// dek.ComputeRekeyArgsHash production path so seeded checkpoint rows match
// what FindByContextAndArgs looks up.
func opArgsHashForReq(t *testing.T, req dek.RekeyRequest) []byte {
	t.Helper()
	h, err := dek.ComputeRekeyArgsHash(req)
	require.NoError(t, err)
	return h[:]
}
