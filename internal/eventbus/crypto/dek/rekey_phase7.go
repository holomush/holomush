// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// AuditEmitter is the narrow seam the Orchestrator consumes for Phase 7
// audit-event emission. The production implementation is *RekeyAuditEmitter
// (defined in audit.go). Tests may substitute a fake.
//
// Emit fills in the rekey_chain block (INV-CRYPTO-101: prev_hash, INV-CRYPTO-115: self_hash)
// and publishes the rekey audit event. Returns the minted event ULID along
// with the finalized payload (scope/prev_hash/self_hash populated) so the
// caller can persist the exact record on publish failure (INV-CRYPTO-100 fallback).
type AuditEmitter interface {
	Emit(ctx context.Context, payload RekeyAuditPayload) (ulid.ULID, RekeyAuditPayload, error)
}

// SetAuditEmitter installs the Phase 7 audit-event emitter and is the
// additive seam introduced by holomush-jxo8.7.24. NewOrchestrator's
// signature is intentionally unchanged (Phase 1–6 wiring from earlier
// beads must continue compiling); production wiring at cmd/holomush/core.go
// MUST call SetAuditEmitter after NewOrchestrator before invoking RunPhase7.
// Calling RunPhase7 without an emitter wired returns
// DEK_REKEY_AUDIT_EMITTER_NIL — fail closed.
//
// Mirrors the SetMaterialResolver / SetPhase5Coordinator / SetDestroyer
// additive post-construction seam pattern.
func (o *Orchestrator) SetAuditEmitter(e AuditEmitter) {
	o.auditEmitter = e
}

// SetDataDir configures the host-local data directory used for the
// Phase 7 fallback log. Production wiring must call this with the
// configured data_dir before invoking RunPhase7. If empty, the fallback
// log write is skipped (the caller logs the gap at Error level).
//
// The fallback log path is: <data_dir>/audit-fallback/rekey-<request_id>.log
// Per spec §4.3 Phase 7 and INV-CRYPTO-100.
func (o *Orchestrator) SetDataDir(dir string) {
	o.dataDir = dir
}

// RunPhase7 emits the chained rekey audit event and advances the checkpoint
// to complete. This is the final phase of the 7-phase Rekey orchestrator.
//
// Pre-condition: checkpoint.Status MUST be CheckpointStatusPhase6DestroyOld.
//
// Steps:
//  1. Load the checkpoint row; verify pre-condition.
//  2. Advance status phase6_destroy_old → phase7_audit (CAS UPDATE).
//  3. Look up old and new DEK rows to populate version numbers in the payload.
//  4. Build RekeyAuditPayload from the checkpoint row and request.
//     INV-CRYPTO-112: policy_hash read from checkpoint row, never re-queried.
//     INV-CRYPTO-98: force_destroy=true and final_missing_members populated when
//     the force-destroy path was used.
//  5. Emit via AuditEmitter (INV-CRYPTO-101: prev_hash from ComputePrevHashFor;
//     INV-CRYPTO-115: self_hash via RecomputeSelfHash).
//  6. On emit failure: write fallback log to
//     <data_dir>/audit-fallback/rekey-<request_id>.log (INV-CRYPTO-100);
//     return DEK_REKEY_PHASE7_AUDIT_FAILED. Checkpoint stays at phase7_audit
//     for retry.
//  7. On emit success: advance status phase7_audit → complete via MarkComplete.
//
// INV-CRYPTO-100: emit confirmed before complete transition.
func (o *Orchestrator) RunPhase7(ctx context.Context, rid RequestID, req RekeyRequest) (RekeyOutcome, error) {
	if o.auditEmitter == nil {
		return RekeyOutcome{}, oops.Code("DEK_REKEY_AUDIT_EMITTER_NIL").
			Errorf("Phase 7 requires SetAuditEmitter(...) before RunPhase7")
	}

	ckpt, err := o.repo.Get(ctx, rid)
	if err != nil {
		return RekeyOutcome{}, err
	}
	if ckpt.Status != CheckpointStatusPhase6DestroyOld {
		return RekeyOutcome{}, oops.Code("DEK_REKEY_PHASE_PRECONDITION_FAILED").
			With("expected", CheckpointStatusPhase6DestroyOld).
			With("actual", string(ckpt.Status)).
			Errorf("Phase 7 requires status=%s", CheckpointStatusPhase6DestroyOld)
	}

	// Advance to phase7_audit so a retry can distinguish "not started" from
	// "started but emit failed" (the checkpoint stays at phase7_audit on emit
	// failure, enabling the same RunPhase7 invocation to be retried by the
	// resume dispatcher without losing the state-machine position).
	if advErr := o.repo.UpdateStatus(ctx, rid, CheckpointStatusPhase6DestroyOld, CheckpointStatusPhase7Audit); advErr != nil {
		return RekeyOutcome{}, oops.Code("DEK_REKEY_PHASE7_ADVANCE_FAILED").Wrap(advErr)
	}

	// Reload the checkpoint after the status advance so the completed_at
	// and other fields are accurate.
	ckpt, err = o.repo.Get(ctx, rid)
	if err != nil {
		return RekeyOutcome{}, err
	}

	// Look up version numbers from the crypto_keys rows for the audit payload.
	oldRow, err := o.store.selectByPK(ctx, ckpt.OldDEKID)
	if err != nil {
		return RekeyOutcome{}, oops.Code("DEK_REKEY_PHASE7_OLD_ROW_LOOKUP_FAILED").
			With("old_dek_id", ckpt.OldDEKID).Wrap(err)
	}
	var newVersion uint32
	if ckpt.NewDEKID != nil {
		newRow, err := o.store.selectByPK(ctx, *ckpt.NewDEKID)
		if err != nil {
			return RekeyOutcome{}, oops.Code("DEK_REKEY_PHASE7_NEW_ROW_LOOKUP_FAILED").
				With("new_dek_id", *ckpt.NewDEKID).Wrap(err)
		}
		newVersion = newRow.Version
	}

	// Decode missing members for the INV-CRYPTO-98 force-destroy payload field.
	// Treat decode failure as an empty list — the operator surface will show
	// an empty list rather than blocking the audit emit.
	missingMembers, memberErr := ckpt.Phase5MissingMembers()
	if memberErr != nil {
		o.logger.WarnContext(
			ctx, "phase7 missing_members decode failed; using empty list",
			"request_id", rid.String(),
			"err", memberErr.Error(),
		)
	}
	// Normalise: spec says the field is [] not nil when no missing members.
	if missingMembers == nil {
		missingMembers = []string{}
	}

	// Build the audit payload. INV-CRYPTO-112: policy_hash is read from the
	// checkpoint row (frozen at Phase 1) — never re-queried from the chain.
	policyHashArr := ckpt.PolicyHash()
	payload := RekeyAuditPayload{
		RequestID: rid.String(),
		Context:   RekeyAuditContext{Type: ckpt.ContextType, ID: ckpt.ContextID},
		OldDEK:    RekeyAuditDEK{ID: ckpt.OldDEKID, Version: oldRow.Version},
		PrimaryOperator: RekeyAuditOp{
			PlayerID:         req.Operator.PlayerID,
			OSUser:           req.Operator.OSUser,
			TOTPVerified:     req.Operator.TOTPVerified,
			AuthProviderName: req.Operator.AuthProviderName,
		},
		Justification: req.Justification,
		// INV-CRYPTO-112: encoded verbatim from the row — never re-queried.
		PolicyHash: fmt.Sprintf("sha256:%s", hex.EncodeToString(policyHashArr[:])),
		Phases: RekeyAuditPhases{
			Phase3RowsRewritten:       ckpt.Phase3RowsRewritten,
			Phase5Attempts:            ckpt.Phase5AttemptCount,
			Phase5FinalMissingMembers: missingMembers,
			Phase6DestroyedAt:         time.Now(), // approximate; canonical record is DB state
		},
		ForceDestroy:   ckpt.ForceDestroy, // INV-CRYPTO-98: true when force-destroy path used
		StartedAt:      ckpt.StartedAt,
		CompletedAt:    time.Now(),
		ServerIdentity: o.serverID,
		SpecVersion:    "2026-04-25-event-payload-crypto-design.md @ §6.3",
	}
	if ckpt.NewDEKID != nil {
		payload.NewDEK = RekeyAuditDEK{ID: *ckpt.NewDEKID, Version: newVersion}
	}
	if req.DualControl != nil {
		payload.DualControlPartner = &RekeyAuditPart{
			PlayerID:          req.DualControl.PartnerPlayerID,
			ApprovalRequestID: req.DualControl.ApprovalRequestID.String(),
		}
	}

	// Step 5: emit via AuditEmitter (INV-CRYPTO-101 + INV-CRYPTO-115 are satisfied by the
	// emitter itself — it calls ComputePrevHashFor + RecomputeSelfHash).
	eventID, finalizedPayload, emitErr := o.auditEmitter.Emit(ctx, payload)
	if emitErr != nil {
		// INV-CRYPTO-100: on failure, write fallback log so the rekey record is
		// not silently lost. The rekey state in the DB (DEK rows) is
		// irreversibly committed; the audit emit is the cross-reference,
		// not the canonical record. Persist the FINALIZED payload (with
		// rekey_chain.scope/prev_hash/self_hash populated) so manual
		// recovery has the exact record that would have been emitted.
		if logErr := o.writeFallbackLog(rid, finalizedPayload); logErr != nil {
			o.logger.ErrorContext(
				ctx, "rekey audit fallback log write failed",
				"request_id", rid.String(),
				"err", logErr.Error(),
			)
		}
		return RekeyOutcome{}, oops.Code("DEK_REKEY_PHASE7_AUDIT_FAILED").Wrap(emitErr)
	}

	// Step 7: emit confirmed — advance to complete.
	if err := o.repo.MarkComplete(ctx, rid); err != nil {
		return RekeyOutcome{}, oops.Code("DEK_REKEY_PHASE7_MARK_COMPLETE_FAILED").Wrap(err)
	}

	return RekeyOutcome{
		RequestID:        rid,
		AuditEventID:     eventID,
		Phase3RowCount:   payload.Phases.Phase3RowsRewritten,
		Phase5Attempts:   ckpt.Phase5AttemptCount,
		ForceDestroyUsed: ckpt.ForceDestroy,
		StartedAt:        ckpt.StartedAt,
		CompletedAt:      time.Now(),
		DurationMs:       time.Since(ckpt.StartedAt).Milliseconds(), //nolint:noremoteclockcompare // observability-only; ckpt.StartedAt is used for duration display, not protocol decisions
	}, nil
}

// writeFallbackLog serialises the rekey audit payload to a JSON file at
// <data_dir>/audit-fallback/rekey-<request_id>.log with mode 0600.
// The directory is created with mode 0700 if it does not already exist.
//
// INV-CRYPTO-100: the fallback log is the sole record when Phase 7 audit emission
// fails. It is NOT the canonical record — that is DB state. Operators MUST
// consult the operational runbook (§11) for escalation steps.
func (o *Orchestrator) writeFallbackLog(rid RequestID, p RekeyAuditPayload) error {
	if o.dataDir == "" {
		return oops.Code("DEK_REKEY_FALLBACK_LOG_NO_DATA_DIR").
			Errorf("data_dir not configured; fallback log cannot be written")
	}
	dir := filepath.Join(o.dataDir, "audit-fallback")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return oops.Code("DEK_REKEY_FALLBACK_LOG_MKDIR_FAILED").Wrap(err)
	}
	path := filepath.Join(dir, "rekey-"+rid.String()+".log")
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return oops.Code("DEK_REKEY_FALLBACK_LOG_MARSHAL_FAILED").Wrap(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return oops.Code("DEK_REKEY_FALLBACK_LOG_WRITE_FAILED").Wrap(err)
	}
	return nil
}
