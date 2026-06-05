// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"

	"github.com/samber/oops"
)

// resumeEntry identifies which RunPhaseN method the resume dispatcher must
// invoke first based on a checkpoint's current status. The dispatcher then
// falls through the remaining phases to drive the checkpoint to completion
// (or a terminal failure / Phase 5 timeout the operator must adjudicate).
//
// resumeEntry is an unexported package-internal enum; the public Run entry
// computes it via decideResumeEntry and consumes it in driveToCompletion.
type resumeEntry int

// Fresh-start entry (no checkpoint exists for this context+args) is selected
// by Run itself before decideResumeEntry is consulted, so the helper never
// returns a "fresh-start" sentinel — only entries that apply to an already-
// open checkpoint.
const (
	// resumeEntryPhase2: status ∈ {pending, phase1_auth}. Phase 1 finished
	// (or the checkpoint was just opened) but Phase 2 has not run yet.
	resumeEntryPhase2 resumeEntry = iota + 1

	// resumeEntryPhase3: status ∈ {phase2_mint_dek, phase3_reencrypt_cold}.
	// Phase 2 minted the new DEK. RunPhase3 is the entry; if status is
	// phase3_reencrypt_cold, RunPhase3 is idempotent (it resumes from the
	// stored cursor and exits immediately if all rows are already rewritten).
	resumeEntryPhase3

	// resumeEntryPhase5Retry: status = phase5_invalidate with
	// phase5_missing_members IS NOT NULL and req.ForceDestroy == false.
	// The operator wants another invalidation fan-out attempt.
	resumeEntryPhase5Retry

	// resumeEntryPhase5ForceDestroy: status = phase5_invalidate with
	// phase5_missing_members IS NOT NULL and req.ForceDestroy == true.
	// The operator chose the split-brain escape hatch; the dispatcher
	// invokes RunPhase5WithForceDestroy. INV-CRYPTO-97's gate is checked inside
	// that method.
	resumeEntryPhase5ForceDestroy

	// resumeEntryPhase6: status = phase5_invalidate with
	// phase5_missing_members IS NULL (clean Phase 5 success). Phase 6 is the
	// next step (destroy old DEK + advance to phase6_destroy_old).
	resumeEntryPhase6

	// resumeEntryPhase7: status = phase6_destroy_old. Phase 6 already
	// completed; Phase 7 emits the chained audit event and marks complete.
	resumeEntryPhase7

	// resumeEntryComplete: status = complete (terminal success). Resume is
	// idempotent (INV-CRYPTO-103): Run returns the prior outcome without invoking
	// any phase.
	resumeEntryComplete

	// resumeEntryAborted: status = aborted (terminal failure). Run surfaces
	// a typed terminal error; the operator must open a fresh rekey if they
	// still want to rotate the DEK.
	resumeEntryAborted

	// resumeEntryManual: the checkpoint is in a state the FSM cannot resume
	// safely through the existing RunPhaseN preconditions — currently only
	// status = phase7_audit (Phase 7 advanced status before the audit emit
	// failed; re-entering RunPhase7 would violate its precondition).
	// Operator intervention is required.
	resumeEntryManual
)

// decideResumeEntry maps a checkpoint's observable state to the first
// RunPhaseN the resume dispatcher must invoke. The function is pure
// (no I/O); the dispatcher consults it after loading the row.
//
//   - hasMissingMembers reflects checkpoint.Phase5HasMissingMembers().
//   - forceDestroy reflects req.ForceDestroy on the in-flight request
//     (not checkpoint.ForceDestroy — the operator decides whether to use the
//     escape hatch on each resume invocation).
//
// The second return value flags manual-recovery cases for caller diagnostics
// (currently true only when entry == resumeEntryManual).
func decideResumeEntry(status CheckpointStatus, hasMissingMembers, forceDestroy bool) (resumeEntry, bool) {
	switch status {
	case CheckpointStatusPending, CheckpointStatusPhase1Auth:
		return resumeEntryPhase2, false
	case CheckpointStatusPhase2MintDEK, CheckpointStatusPhase3ReencryptCold:
		return resumeEntryPhase3, false
	case CheckpointStatusPhase5Invalidate:
		if !hasMissingMembers {
			return resumeEntryPhase6, false
		}
		if forceDestroy {
			return resumeEntryPhase5ForceDestroy, false
		}
		return resumeEntryPhase5Retry, false
	case CheckpointStatusPhase6DestroyOld:
		return resumeEntryPhase7, false
	case CheckpointStatusPhase7Audit:
		// Phase 7 advanced status before the audit emit failed. The FSM
		// guard inside RunPhase7 only accepts phase6_destroy_old; we
		// surface a manual-recovery signal rather than silently retrying
		// in a way that breaks INV-CRYPTO-88.
		return resumeEntryManual, true
	case CheckpointStatusComplete:
		return resumeEntryComplete, false
	case CheckpointStatusAborted:
		return resumeEntryAborted, false
	default:
		// Unknown / unhandled status: treat as manual-recovery so the
		// operator is forced to inspect the row rather than us guessing
		// at intent.
		return resumeEntryManual, true
	}
}

// Run is the top-level orchestrator entry. It handles both fresh-start and
// resume invocations atomically.
//
//   - Fresh-start path: no non-terminal checkpoint exists for
//     (context_type, context_id). Run computes op_args_hash, opens a new
//     checkpoint via RunPhase1Fresh, then drives the lifecycle to completion.
//   - Resume path (INV-CRYPTO-103): a non-terminal checkpoint exists with
//     op_args_hash matching the request AND primary_player_id matching the
//     operator's player_id. Run picks up the existing RequestID (INV-CRYPTO-91:
//     never re-enter Phase 1) and drives the remaining phases.
//   - Operator-mismatch (INV-CRYPTO-103): matching op_args_hash but different
//     primary_player_id → DEK_REKEY_RESUME_OPERATOR_MISMATCH.
//   - Args-conflict: non-terminal checkpoint with DIFFERENT op_args_hash
//     on the same context → DEK_REKEY_ARGS_CONFLICT.
//   - Already-complete (INV-CRYPTO-103 idempotency): if a terminal-complete row
//     exists matching args, that row is treated as terminal and the
//     dispatcher proceeds to fresh-start (per FindByContextAndArgs scope,
//     which already filters terminal rows out of the resume predicate).
//
// req.ForceDestroy is only honored on the resume path (INV-CRYPTO-98): a fresh
// rekey never bypasses Phase 5 invalidation.
func (o *Orchestrator) Run(ctx context.Context, req RekeyRequest) (RekeyOutcome, error) {
	argsHash, err := ComputeRekeyArgsHash(req)
	if err != nil {
		return RekeyOutcome{}, err
	}

	// INV-CRYPTO-103 resume predicate: same context + same args + non-terminal status.
	existing, found, err := o.repo.FindByContextAndArgs(ctx, req.ContextType, req.ContextID, argsHash[:])
	if err != nil {
		return RekeyOutcome{}, oops.Code("DEK_REKEY_RESUME_LOOKUP_FAILED").Wrap(err)
	}

	var rid RequestID
	resumed := false
	if found {
		// Operator-binding check (INV-CRYPTO-103).
		if existing.PrimaryPlayerID != req.Operator.PlayerID {
			return RekeyOutcome{}, oops.Code("DEK_REKEY_RESUME_OPERATOR_MISMATCH").
				With("expected_player_id", existing.PrimaryPlayerID).
				With("got_player_id", req.Operator.PlayerID).
				With("request_id", existing.RequestID.String()).
				Errorf("INV-CRYPTO-103: only the original primary may resume")
		}
		rid = existing.RequestID
		resumed = true
	} else {
		// Detect args-conflict: a non-terminal checkpoint with DIFFERENT
		// args on the same context. The operator must abort the
		// conflicting attempt before starting a new one.
		conflict, conflictFound, err := o.repo.FindNonTerminalByContext(ctx, req.ContextType, req.ContextID)
		if err != nil {
			return RekeyOutcome{}, oops.Code("DEK_REKEY_CONFLICT_LOOKUP_FAILED").Wrap(err)
		}
		if conflictFound {
			return RekeyOutcome{}, oops.Code("DEK_REKEY_ARGS_CONFLICT").
				With("existing_request_id", conflict.RequestID.String()).
				With("existing_status", string(conflict.Status)).
				With("existing_player_id", conflict.PrimaryPlayerID).
				Errorf("a non-terminal rekey with different args is in progress; abort it first")
		}
		// Fresh-start path: open the checkpoint via Phase 1.
		rid, err = o.RunPhase1Fresh(ctx, req)
		if err != nil {
			return RekeyOutcome{}, err
		}
	}

	return o.driveToCompletion(ctx, rid, req, resumed)
}

// driveToCompletion advances the checkpoint identified by rid through
// whatever RunPhaseN methods remain. The starting phase is determined by
// decideResumeEntry against the current status; the function then falls
// through the remaining phases in order. RunPhase3 and RunPhase6 are
// idempotent (re-invoking on their post-state is a no-op), so the
// fall-through structure naturally handles boundary cases where the
// previous run advanced exactly one phase.
//
// resumed records whether the entry path was fresh-start (false) or
// resume (true); it is propagated to RekeyOutcome.Resumed.
func (o *Orchestrator) driveToCompletion(ctx context.Context, rid RequestID, req RekeyRequest, resumed bool) (RekeyOutcome, error) {
	ckpt, err := o.repo.Get(ctx, rid)
	if err != nil {
		return RekeyOutcome{}, err
	}

	entry, _ := decideResumeEntry(ckpt.Status, ckpt.Phase5HasMissingMembers(), req.ForceDestroy)

	// Terminal / idempotent / manual-recovery branches return without
	// invoking any phase.
	switch entry {
	case resumeEntryComplete:
		// INV-CRYPTO-103 idempotency: return the prior outcome. The original
		// AuditEventID is not stored on the checkpoint (it lives only on
		// the events_audit row), so we synthesise a minimal RekeyOutcome
		// from the row. Callers needing the audit event ID query
		// events_audit by request_id.
		out := RekeyOutcome{
			RequestID:        rid,
			Phase5Attempts:   ckpt.Phase5AttemptCount,
			ForceDestroyUsed: ckpt.ForceDestroy,
			Resumed:          true,
			StartedAt:        ckpt.StartedAt,
		}
		if ckpt.CompletedAt != nil {
			out.CompletedAt = *ckpt.CompletedAt
			// observability-only duration: both timestamps originate on this
			// host's clock (StartedAt at Phase 1 INSERT, CompletedAt at Phase 7
			// MarkComplete), so the cross-host clock-skew concern that
			// INV-CLUSTER-8 guards against does not apply. The value is for CLI
			// display, never for protocol decisions.
			out.DurationMs = ckpt.CompletedAt.Sub(ckpt.StartedAt).Milliseconds() //nolint:noremoteclockcompare // observability-only; both timestamps are local host wall-clock writes.
		}
		return out, nil
	case resumeEntryAborted:
		return RekeyOutcome{RequestID: rid}, oops.Code("DEK_REKEY_CHECKPOINT_TERMINAL").
			With("request_id", rid.String()).
			With("aborted_reason", derefString(ckpt.AbortedReason)).
			Errorf("checkpoint already aborted")
	case resumeEntryManual:
		return RekeyOutcome{RequestID: rid}, oops.Code("DEK_REKEY_PHASE7_AUDIT_RETRY_REQUIRED").
			With("request_id", rid.String()).
			With("status", string(ckpt.Status)).
			Errorf("checkpoint is in phase7_audit; manual audit-emit recovery required")
	}

	// Phase dispatch: fall through the remaining phases in order. Each
	// case advances the checkpoint and the next iteration of the switch
	// is implicit (we use a chained-if structure rather than Go's
	// fallthrough so post-case branching on ForceDestroy / Phase 5 retry
	// stays readable).
	switch entry {
	case resumeEntryPhase2:
		if err := o.RunPhase2(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		fallthrough
	case resumeEntryPhase3:
		if _, err := o.RunPhase3(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		// After Phase 3 status is phase3_reencrypt_cold. Phase 5 is the
		// next step; route through the same Phase-5-aware entry the
		// resume-from-phase5_invalidate branch uses.
		if err := o.RunPhase5(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		// Phase 5 left status at phase5_invalidate with missing_members IS NULL.
		if err := o.RunPhase6(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		return o.runPhase7AndSetResumed(ctx, rid, req, resumed)
	case resumeEntryPhase5Retry:
		// Retry the cluster invalidation fan-out.
		if err := o.RunPhase5(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		if err := o.RunPhase6(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		return o.runPhase7AndSetResumed(ctx, rid, req, resumed)
	case resumeEntryPhase5ForceDestroy:
		// Operator chose the split-brain escape (INV-CRYPTO-97 / INV-CRYPTO-98).
		// RunPhase5WithForceDestroy advances directly to phase6_destroy_old
		// without invoking the destroyer; we still call RunPhase6 to
		// soft-delete the row (it observes phase6_destroy_old and runs
		// idempotently — see INV-CRYPTO-99 in rekey_phase6.go).
		if err := o.RunPhase5WithForceDestroy(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		if err := o.RunPhase6(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		return o.runPhase7AndSetResumed(ctx, rid, req, resumed)
	case resumeEntryPhase6:
		if err := o.RunPhase6(ctx, rid); err != nil {
			return RekeyOutcome{}, err
		}
		return o.runPhase7AndSetResumed(ctx, rid, req, resumed)
	case resumeEntryPhase7:
		return o.runPhase7AndSetResumed(ctx, rid, req, resumed)
	}

	// Unreachable: terminal / manual cases returned above; phase cases
	// all return. A new resumeEntry constant added without a switch arm
	// triggers this path — fail closed with a typed error.
	return RekeyOutcome{}, oops.Code("DEK_REKEY_RESUME_DISPATCH_UNREACHABLE").
		With("entry", entry).
		Errorf("internal error: unhandled resume entry")
}

// runPhase7AndSetResumed wraps RunPhase7 to stamp Resumed on the outcome
// before returning. RunPhase7 itself does not know whether the call was
// fresh-start or resume; only the dispatcher does.
func (o *Orchestrator) runPhase7AndSetResumed(ctx context.Context, rid RequestID, req RekeyRequest, resumed bool) (RekeyOutcome, error) {
	out, err := o.RunPhase7(ctx, rid, req)
	if err != nil {
		return out, err
	}
	out.Resumed = resumed
	return out, nil
}

// RunByRequestID resumes the checkpoint identified by rid, bypassing the
// context-and-args lookup that Run performs. It is the explicit-resume entry
// for the RekeyResume RPC path, where the operator supplies a request_id
// directly instead of repeating all original arguments.
//
// RunByRequestID:
//  1. Loads the checkpoint row (error on DEK_REKEY_CHECKPOINT_NOT_FOUND).
//  2. Verifies the checkpoint is non-terminal (returns
//     DEK_REKEY_CHECKPOINT_TERMINAL on status=complete or status=aborted).
//  3. Enforces the operator-binding invariant (INV-CRYPTO-103): if the checkpoint's
//     primary_player_id does not match req.Operator.PlayerID, returns
//     DEK_REKEY_RESUME_OPERATOR_MISMATCH.
//  4. Calls driveToCompletion with the checkpoint's ContextType, ContextID,
//     and Justification rehydrated from the stored checkpoint row (so that
//     RunPhase7's audit payload carries the operator's original justification
//     on the explicit-resume path).
//
// req.ForceDestroy is honored on the resume path per INV-CRYPTO-98.
func (o *Orchestrator) RunByRequestID(ctx context.Context, rid RequestID, req RekeyRequest) (RekeyOutcome, error) {
	ckpt, err := o.repo.Get(ctx, rid)
	if err != nil {
		return RekeyOutcome{}, oops.Code("DEK_REKEY_RESUME_CHECKPOINT_LOAD_FAILED").
			With("request_id", rid.String()).
			Wrap(err)
	}

	switch ckpt.Status {
	case CheckpointStatusComplete:
		return RekeyOutcome{}, oops.Code("DEK_REKEY_CHECKPOINT_TERMINAL").
			With("request_id", rid.String()).
			With("status", string(ckpt.Status)).
			Errorf("checkpoint already complete; no resume needed")
	case CheckpointStatusAborted:
		return RekeyOutcome{}, oops.Code("DEK_REKEY_CHECKPOINT_TERMINAL").
			With("request_id", rid.String()).
			With("status", string(ckpt.Status)).
			Errorf("checkpoint already aborted; open a fresh rekey to retry")
	}

	// INV-CRYPTO-103: only the original primary operator may resume.
	if ckpt.PrimaryPlayerID != req.Operator.PlayerID {
		return RekeyOutcome{}, oops.Code("DEK_REKEY_RESUME_OPERATOR_MISMATCH").
			With("expected_player_id", ckpt.PrimaryPlayerID).
			With("got_player_id", req.Operator.PlayerID).
			With("request_id", rid.String()).
			Errorf("INV-CRYPTO-103: only the original primary operator may resume")
	}

	// Populate context fields from the stored checkpoint row so RunPhase7's
	// audit payload carries accurate context metadata, including the original
	// justification rehydrated from the checkpoint row (holomush-jxo8.7.55).
	resumeReq := RekeyRequest{
		ContextType:   ckpt.ContextType,
		ContextID:     ckpt.ContextID,
		Operator:      req.Operator,
		ForceDestroy:  req.ForceDestroy,
		Justification: ckpt.Justification,
	}

	return o.driveToCompletion(ctx, rid, resumeReq, true)
}

// derefString safely dereferences *string for error context. Returns the
// empty string when p is nil rather than panicking.
func derefString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
