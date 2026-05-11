// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package dek_test

import (
	"context"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/samber/oops"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/eventbus/crypto/dek"
	"github.com/holomush/holomush/internal/eventbus/crypto/kek"
	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/pkg/errutil"
)

// phase5TestSetup is the canonical Phase 5 test harness. Mirrors
// phase3TestSetup in rekey_phase3_integration_test.go: real pgx pool,
// real Manager (which satisfies dek.Minter), but the Phase 5 cluster
// invalidation fan-out is faked by fakePhase5Coordinator so tests can
// drive the success / partial-timeout / retry paths deterministically.
//
// Tests interact with the harness through:
//   - setup.RunUpToPhase3Complete() — seeds a checkpoint at
//     phase3_reencrypt_cold (the actual FSM equivalent of plan-symbolic
//     "phase3_complete"), with old_dek_id + new_dek_id set on the row.
//   - setup.Orch.RunPhase5 / RunPhase5WithForceDestroy — under test.
//   - setup.Coordinator.SetSuccess / SetPartialTimeout — controls the
//     Coordinator seam's behavior on the next invocation.
//   - setup.Repo.Get(rid) — read-back to assert FSM state.
type phase5TestSetup struct {
	t           *testing.T
	pool        *pgxpool.Pool
	provider    kek.Provider
	manager     dek.Manager
	Orch        *dek.Orchestrator
	Repo        *dek.CheckpointRepo
	Coordinator *fakePhase5Coordinator
	oldDEKID    int64
	newDEKID    int64
}

// fakePhase5Coordinator is the test-side stand-in for
// invalidation.Coordinator. It exposes SetSuccess / SetPartialTimeout
// toggles so a single test can sequence multiple outcomes (e.g.,
// timeout-then-success for the retry test). All toggles are
// goroutine-safe so RunPhase5 invocations from t.Parallel children
// don't race the configuration mutators.
type fakePhase5Coordinator struct {
	mu        sync.Mutex
	mode      coordinatorMode
	missing   []string
	calls     []coordinatorCall
	memberIDs []string // optional override for non-string types
}

type coordinatorMode int

const (
	coordModeUnset coordinatorMode = iota
	coordModeSuccess
	coordModePartialTimeout
)

type coordinatorCall struct {
	Action           string
	ContextType      string
	ContextID        string
	Version          uint32
	SuccessorVersion uint32
}

// SetSuccess configures the next RequestInvalidation to return nil
// (N-of-N ack within window).
func (f *fakePhase5Coordinator) SetSuccess() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = coordModeSuccess
	f.missing = nil
}

// SetPartialTimeout configures the next RequestInvalidation to return
// the canonical INVALIDATION_PARTIAL_FAILURE oops error with the
// supplied missing-member list stamped into Context()["missing_members"].
// The error's shape MUST match what the real invalidation.Coordinator
// produces (see internal/eventbus/crypto/invalidation/coordinator.go).
func (f *fakePhase5Coordinator) SetPartialTimeout(missing []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mode = coordModePartialTimeout
	f.missing = append([]string(nil), missing...)
}

// Calls returns the recorded fan-out attempts. Used by tests asserting
// the attempt counter / payload shape / retry behavior.
func (f *fakePhase5Coordinator) Calls() []coordinatorCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]coordinatorCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// RequestInvalidation is the seam method. The signature MUST be
// byte-identical to dek.Phase5Coordinator.
func (f *fakePhase5Coordinator) RequestInvalidation(
	_ context.Context,
	ctxID dek.ContextID,
	action string,
	version, successorVersion uint32,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, coordinatorCall{
		Action:           action,
		ContextType:      ctxID.Type,
		ContextID:        ctxID.ID,
		Version:          version,
		SuccessorVersion: successorVersion,
	})
	switch f.mode {
	case coordModeSuccess:
		return nil
	case coordModePartialTimeout:
		// Mirror the real Coordinator's error shape: code +
		// missing_members context. The orchestrator's extractMissingMembers
		// must successfully decode this back to []string for the
		// timeout-record path.
		return oops.Code("INVALIDATION_PARTIAL_FAILURE").
			With("missing_members", append([]string(nil), f.missing...)).
			With("action", action).
			With("ctx_type", ctxID.Type).
			With("ctx_id", ctxID.ID).
			Errorf("simulated partial-ack timeout from fake coordinator")
	default:
		return oops.Code("FAKE_PHASE5_COORDINATOR_UNCONFIGURED").
			Errorf("test bug: SetSuccess or SetPartialTimeout was never called")
	}
}

// newPhase5TestSetup builds a harness ready to drive RunPhase5. The
// caller invokes setup.RunUpToPhase3Complete() to obtain a RequestID
// pointing at a checkpoint pre-positioned at phase3_reencrypt_cold,
// then exercises Phase 5 via setup.Orch.
func newPhase5TestSetup(t *testing.T) *phase5TestSetup {
	t.Helper()
	pool := testIntegrationPool(t)
	const gameID = "g1"
	dek.SetGameIDForTest(gameID)

	provider := newTestProvider(t)
	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 64, TTL: 0})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 64, TTL: 0})

	mgr, err := dek.NewManager(
		provider, store, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	require.NoError(t, err)

	// Seed an active DEK row + mint a new one, mirroring phase3TestSetup.
	ctxID := dek.ContextID{Type: "scene", ID: "01PH5"}
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

	return &phase5TestSetup{
		t:           t,
		pool:        pool,
		provider:    provider,
		manager:     mgr,
		Orch:        orch,
		Repo:        repo,
		Coordinator: coord,
		oldDEKID:    oldRecord.ID,
		newDEKID:    newDEKID,
	}
}

// RunUpToPhase3Complete seeds a checkpoint row at the FSM state that
// Phase 3 leaves behind on a clean run: status=phase3_reencrypt_cold
// with old_dek_id + new_dek_id populated. This mirrors the
// "phase3_complete" plan-symbolic state — the actual FSM only has one
// Phase-3-related status (see checkpoint_fsm.go and the .21 close
// rationale).
//
// Bypasses RunPhase1Fresh / RunPhase2 / RunPhase3 because Phase 5's
// contract under test is independent of how the checkpoint got to its
// pre-condition state.
func (s *phase5TestSetup) RunUpToPhase3Complete() dek.RequestID {
	s.t.Helper()
	rid := dek.RequestID(idgen.New())
	_, err := s.pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, 'scene', '01PH5', $2, $3, '01PLAYER',
                'phase3_reencrypt_cold', $4, $5)
    `, rid[:], make([]byte, 32), make([]byte, 32), s.oldDEKID, s.newDEKID)
	require.NoError(s.t, err)
	return rid
}

// TestOrchestrator_Phase5_NofN_AdvancesToComplete — INV-E22 happy path.
// On full N-of-N ack the checkpoint advances to phase5_invalidate with
// phase5_missing_members cleared to NULL (the FSM equivalent of the
// plan-symbolic StatusPhase5Complete).
func TestOrchestrator_Phase5_NofN_AdvancesToComplete(t *testing.T) {
	setup := newPhase5TestSetup(t)
	rid := setup.RunUpToPhase3Complete()

	setup.Coordinator.SetSuccess()
	require.NoError(t, setup.Orch.RunPhase5(context.Background(), rid))

	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase5Invalidate, ckpt.Status,
		"successful Phase 5 ratchets status to phase5_invalidate (FSM equiv of plan StatusPhase5Complete)")
	require.False(t, ckpt.Phase5HasMissingMembers(),
		"on success, phase5_missing_members MUST be NULL")
	require.False(t, ckpt.ForceDestroy,
		"no force-destroy on happy path")
	require.Equal(t, 1, ckpt.Phase5AttemptCount,
		"attempt counter bumped exactly once for the single fan-out")

	calls := setup.Coordinator.Calls()
	require.Len(t, calls, 1)
	require.Equal(t, dek.Phase5ActionRekey, calls[0].Action)
	require.Equal(t, "scene", calls[0].ContextType)
	require.Equal(t, "01PH5", calls[0].ContextID)
	// Old DEK was minted at v=1 by GetOrCreate; MintNewDEKForRekey
	// produces v=old+1 = 2. INV-E22 payload-row table: Version=old,
	// SuccessorVersion=new.
	require.Equal(t, uint32(1), calls[0].Version)
	require.Equal(t, uint32(2), calls[0].SuccessorVersion)
}

// TestOrchestrator_Phase5_PartialTimeout_PersistsMissingMembers — INV-E14
// timeout semantics. On partial-ack timeout the orchestrator:
//   - returns DEK_REKEY_PHASE5_TIMEOUT (operator-facing typed error)
//   - persists missing_members as JSON on the checkpoint row
//   - sets phase5_attempt_count to reflect the fan-out attempt
//   - leaves status at phase5_invalidate (FSM equiv of plan
//     StatusPhase5Timeout — distinguished by missing_members IS NOT NULL)
func TestOrchestrator_Phase5_PartialTimeout_PersistsMissingMembers(t *testing.T) {
	setup := newPhase5TestSetup(t)
	rid := setup.RunUpToPhase3Complete()

	missing := []string{"member-2", "member-4"}
	setup.Coordinator.SetPartialTimeout(missing)
	err := setup.Orch.RunPhase5(context.Background(), rid)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_PHASE5_TIMEOUT")

	ckpt, gerr := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, gerr)
	require.Equal(t, dek.CheckpointStatusPhase5Invalidate, ckpt.Status,
		"timeout ratchets status into phase5_invalidate state machine slot")
	require.True(t, ckpt.Phase5HasMissingMembers(),
		"timeout MUST populate phase5_missing_members (FSM equiv of plan StatusPhase5Timeout)")
	require.Equal(t, 1, ckpt.Phase5AttemptCount,
		"attempt counter bumped once before the fan-out")

	got, decodeErr := ckpt.Phase5MissingMembers()
	require.NoError(t, decodeErr)
	require.ElementsMatch(t, missing, got,
		"persisted missing_members MUST match what the Coordinator reported")
}

// TestOrchestrator_Phase5_RetryAfterTimeout_AdvancesIfSucceeds — confirms
// the (status=phase5_invalidate, missing_members!=NULL) state is the
// retry entry point. Second invocation observes the timed-out state,
// runs the fan-out again, and on success clears missing_members.
func TestOrchestrator_Phase5_RetryAfterTimeout_AdvancesIfSucceeds(t *testing.T) {
	setup := newPhase5TestSetup(t)
	rid := setup.RunUpToPhase3Complete()

	// Attempt 1: timeout.
	setup.Coordinator.SetPartialTimeout([]string{"m2"})
	timeoutErr := setup.Orch.RunPhase5(context.Background(), rid)
	require.Error(t, timeoutErr)
	errutil.AssertErrorCode(t, timeoutErr, "DEK_REKEY_PHASE5_TIMEOUT")

	// Attempt 2: success on re-run.
	setup.Coordinator.SetSuccess()
	require.NoError(t, setup.Orch.RunPhase5(context.Background(), rid))

	ckpt, err := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, err)
	require.Equal(t, dek.CheckpointStatusPhase5Invalidate, ckpt.Status,
		"after retry-success status remains phase5_invalidate (FSM target slot)")
	require.False(t, ckpt.Phase5HasMissingMembers(),
		"retry-success MUST clear missing_members back to NULL")
	require.Equal(t, 2, ckpt.Phase5AttemptCount,
		"counter records both attempts")
	require.Len(t, setup.Coordinator.Calls(), 2,
		"two fan-out attempts: one timeout + one success")
}

// TestOrchestrator_Phase5_ForceDestroy_OnlyAfterTimeout — INV-E10 gate
// enforcement. Force-destroy is rejected when the precondition isn't
// (status==phase5_invalidate AND missing_members IS NOT NULL); once the
// state is in the timed-out slot, force-destroy advances directly to
// phase6_destroy_old, skipping the normal phase5_invalidate-clean path.
func TestOrchestrator_Phase5_ForceDestroy_OnlyAfterTimeout(t *testing.T) {
	setup := newPhase5TestSetup(t)
	rid := setup.RunUpToPhase3Complete()

	// At phase3_reencrypt_cold (Phase 5 hasn't run): force-destroy
	// rejected. INV-E10 rejects fresh checkpoint with no fan-out attempt.
	err := setup.Orch.RunPhase5WithForceDestroy(context.Background(), rid)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_FORCE_DESTROY_FORBIDDEN")

	// Drive Phase 5 into the timed-out state.
	setup.Coordinator.SetPartialTimeout([]string{"m2"})
	timeoutErr := setup.Orch.RunPhase5(context.Background(), rid)
	require.Error(t, timeoutErr)
	errutil.AssertErrorCode(t, timeoutErr, "DEK_REKEY_PHASE5_TIMEOUT")

	// Now force-destroy is permitted. Status advances DIRECTLY to
	// phase6_destroy_old (FSM equiv of plan StatusPhase6Complete) with
	// force_destroy=true on the row.
	require.NoError(t, setup.Orch.RunPhase5WithForceDestroy(context.Background(), rid))

	ckpt, gerr := setup.Repo.Get(context.Background(), rid)
	require.NoError(t, gerr)
	require.True(t, ckpt.ForceDestroy,
		"force_destroy MUST be true after the bypass call")
	require.Equal(t, dek.CheckpointStatusPhase6DestroyOld, ckpt.Status,
		"force-destroy advances DIRECTLY to phase6_destroy_old (skips clean phase5_invalidate)")

	// The missing_members set was persisted during the timeout and MUST
	// remain readable on the row — Phase 7's audit emit consumes it
	// to populate the INV-E11 final_missing_members audit field
	// (handled in holomush-jxo8.7.24, not this bead).
	missing, decodeErr := ckpt.Phase5MissingMembers()
	require.NoError(t, decodeErr)
	require.ElementsMatch(t, []string{"m2"}, missing,
		"final_missing_members snapshot MUST survive the force-destroy advance")
}

// TestOrchestrator_Phase5_ForceDestroy_RejectedOnRepeatedInvocation —
// the second force-destroy call sees status=phase6_destroy_old and
// rejects via INV-E10. Documents the non-idempotency of force-destroy:
// the operator escape hatch is one-shot per checkpoint.
func TestOrchestrator_Phase5_ForceDestroy_RejectedOnRepeatedInvocation(t *testing.T) {
	setup := newPhase5TestSetup(t)
	rid := setup.RunUpToPhase3Complete()

	setup.Coordinator.SetPartialTimeout([]string{"m2"})
	_ = setup.Orch.RunPhase5(context.Background(), rid)
	require.NoError(t, setup.Orch.RunPhase5WithForceDestroy(context.Background(), rid))

	// Second force-destroy: status is now phase6_destroy_old, INV-E10
	// gate trips again.
	err := setup.Orch.RunPhase5WithForceDestroy(context.Background(), rid)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DEK_REKEY_FORCE_DESTROY_FORBIDDEN")
}

// TestOrchestrator_Phase5_RejectsWrongStatus — INV-E22 / FSM-precondition
// guard. Phase 5 must refuse to run when the checkpoint is at a status
// that isn't a valid entry point (phase3_reencrypt_cold or
// phase5_invalidate). Tests the pending-state case as the failure
// representative.
func TestOrchestrator_Phase5_RejectsWrongStatus(t *testing.T) {
	setup := newPhase5TestSetup(t)
	// Open a checkpoint via Open() — status='pending'.
	req, err := dek.NewCheckpointOpenRequest(
		"scene", "01WRONG",
		make([]byte, 32), make([]byte, 32),
		"01PLAYER", setup.oldDEKID,
		"",
	)
	require.NoError(t, err)
	rid, err := setup.Repo.Open(context.Background(), req)
	require.NoError(t, err)

	setup.Coordinator.SetSuccess()
	runErr := setup.Orch.RunPhase5(context.Background(), rid)
	require.Error(t, runErr)
	errutil.AssertErrorCode(t, runErr, "DEK_REKEY_PHASE_PRECONDITION_FAILED")
	require.Empty(t, setup.Coordinator.Calls(),
		"precondition failure MUST short-circuit before any fan-out")
}

// TestOrchestrator_Phase5_NoCoordinator_FailsClosed — symmetric to
// TestPhase3_NoResolver_FailsClosed: calling RunPhase5 without the seam
// installed surfaces a typed error code, never a nil-pointer dereference.
func TestOrchestrator_Phase5_NoCoordinator_FailsClosed(t *testing.T) {
	pool := testIntegrationPool(t)
	dek.SetGameIDForTest("g1")

	store := dek.NewStore(pool)
	cache := dek.NewCache(dek.CacheConfig{Capacity: 16, TTL: 0})
	pcache := dek.NewParticipantsCache(dek.CacheConfig{Capacity: 16, TTL: 0})

	provider := newTestProvider(t)
	mgr, err := dek.NewManager(
		provider, store, cache, pcache,
		func(_ context.Context, _ dek.ContextID, _ string, _, _ uint32) error { return nil },
		&stubBindingResolver{},
	)
	require.NoError(t, err)

	repo := dek.NewCheckpointRepo(pool)
	orch := dek.NewOrchestrator(store, repo, &nilPolicyHashSource{}, mgr)
	// Deliberately NOT calling SetPhase5Coordinator.

	rid := dek.RequestID(idgen.New())
	// Open a row at the right status so the precondition check passes
	// and we exercise the coordinator-nil branch specifically.
	ctxID := dek.ContextID{Type: "scene", ID: "01NOCOORD"}
	_, err = mgr.GetOrCreate(context.Background(), ctxID, nil)
	require.NoError(t, err)
	active, err := mgr.ActiveDEKRow(context.Background(), ctxID)
	require.NoError(t, err)
	newID, err := mgr.MintNewDEKForRekey(context.Background(), active.ID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
        INSERT INTO crypto_rekey_checkpoints
          (request_id, context_type, context_id, op_args_hash, policy_hash,
           primary_player_id, status, old_dek_id, new_dek_id)
        VALUES ($1, 'scene', '01NOCOORD', $2, $3, '01PLAYER',
                'phase3_reencrypt_cold', $4, $5)
    `, rid[:], make([]byte, 32), make([]byte, 32), active.ID, newID)
	require.NoError(t, err)

	runErr := orch.RunPhase5(context.Background(), rid)
	require.Error(t, runErr)
	errutil.AssertErrorCode(t, runErr, "DEK_REKEY_COORDINATOR_NIL")
}
