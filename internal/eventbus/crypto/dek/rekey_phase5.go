// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"
	"encoding/json"
	"reflect"

	"github.com/samber/oops"
)

// Phase5Coordinator is the narrow seam the Orchestrator consumes for the
// Phase 5 cluster cache-invalidation fan-out. The production wiring is a
// thin closure over *invalidation.Coordinator's RequestInvalidation method
// (cmd/holomush/core.go). The seam is action-string-typed (not the
// invalidation.Action enum) to keep this package free of an upward
// dependency on internal/eventbus/crypto/invalidation.
//
// Returns nil on N-of-N success. On partial-ack timeout (the case
// INV-CRYPTO-109 / INV-CRYPTO-98 governs) the implementation MUST return an oops error
// whose Context()["missing_members"] is a []string or []cluster.MemberID;
// the orchestrator extracts that field and persists it on the checkpoint
// row. Any other error class is treated as fatal — the orchestrator
// surfaces it to the caller without persisting a missing-members set.
//
// Reuses the type shape of the existing dek.Invalidator func type (used by
// the *manager for rotate/add invalidations) — the two seams are
// intentionally signature-identical so a single Coordinator adapter
// satisfies both.
type Phase5Coordinator interface {
	RequestInvalidation(
		ctx context.Context,
		ctxID ContextID,
		action string,
		version, successorVersion uint32,
	) error
}

// Phase5CoordinatorFunc adapts a function to the Phase5Coordinator interface.
// Production wiring uses this to wrap a *invalidation.Coordinator without
// introducing an import cycle (dek → invalidation → dek).
type Phase5CoordinatorFunc func(
	ctx context.Context,
	ctxID ContextID,
	action string,
	version, successorVersion uint32,
) error

// RequestInvalidation calls f. Implements Phase5Coordinator.
func (f Phase5CoordinatorFunc) RequestInvalidation(
	ctx context.Context,
	ctxID ContextID,
	action string,
	version, successorVersion uint32,
) error {
	return f(ctx, ctxID, action, version, successorVersion)
}

// Phase5ActionRekey is the action-string the orchestrator publishes for
// the Phase 5 cache-invalidation fan-out. Mirrors invalidation.ActionRekey
// at the string layer so this package does not depend on the invalidation
// package. Production wiring asserts string equality with
// string(invalidation.ActionRekey) at startup (cmd/holomush/core.go).
const Phase5ActionRekey = "rekey"

// SetPhase5Coordinator installs the Phase 5 cluster cache-invalidation
// seam. Production wiring at cmd/holomush/core.go MUST call this after
// NewOrchestrator before invoking RunPhase5; calling RunPhase5 without a
// coordinator wired returns DEK_REKEY_COORDINATOR_NIL — fail closed.
//
// Mirrors the SetMaterialResolver pattern introduced by .21: additive
// post-construction seam, NewOrchestrator's signature unchanged so the
// Phase 1 / Phase 2 / Phase 3 wiring shipped in earlier beads continues
// to compile.
func (o *Orchestrator) SetPhase5Coordinator(c Phase5Coordinator) {
	o.phase5Coord = c
}

// RunPhase5 drives the Phase 5 cluster cache-invalidation fan-out
// (INV-CRYPTO-109). It is the orchestrator's "publish the new DEK has arrived,
// every replica MUST evict its cached old-DEK material" step.
//
// Pre-condition: checkpoint.Status ∈
// {CheckpointStatusPhase3ReencryptCold, CheckpointStatusPhase5Invalidate}.
// The first case is the fresh-entry path (Phase 3 just finished). The
// second is the retry-after-timeout path (a prior RunPhase5 invocation
// returned DEK_REKEY_PHASE5_TIMEOUT and persisted phase5_missing_members;
// the operator is invoking again hoping the missing members are now
// reachable).
//
// Algorithm:
//  1. Load the checkpoint row; reject if status is out of band.
//  2. Increment phase5_attempt_count (best-effort observability).
//  3. Resolve old + new DEK versions via store.selectByPK (orchestrator
//     has the PKs from the checkpoint row).
//  4. Call Phase5Coordinator.RequestInvalidation with action="rekey".
//     5a. On N-of-N success (coord returns nil): RecordPhase5Success — the
//     checkpoint advances to phase5_invalidate with phase5_missing_members
//     cleared to NULL.
//     5b. On partial-ack failure (coord returns an oops error with
//     Context()["missing_members"]): extract the missing-member set, JSON-
//     encode it, RecordPhase5Timeout — the checkpoint advances to (or
//     stays at) phase5_invalidate with phase5_missing_members populated.
//     Returns DEK_REKEY_PHASE5_TIMEOUT.
//     5c. On any other Coordinator error: surface it wrapped under
//     DEK_REKEY_PHASE5_FAILED. No checkpoint mutation.
//
// Status-mapping note: the FSM (checkpoint_fsm.go, owned by .14) declares
// CheckpointStatusPhase5Invalidate as the single Phase-5-related status.
// The plan's symbolic labels (StatusPhase5Complete / StatusPhase5Timeout)
// do not exist as DB values; they're distinguished here via the
// phase5_missing_members column:
//
//	successful Phase 5: status=phase5_invalidate, missing_members IS NULL
//	timed-out Phase 5: status=phase5_invalidate, missing_members IS NOT NULL
//
// This matches the deviation note recorded in holomush-jxo8.7.21's close
// rationale: "Status names use real FSM constants per .14 not plan's
// symbolic labels."
//
// Concurrency: Phase 5 holds no lock (consistent with §4.6's table); the
// CAS predicates on the RecordPhase5Success / RecordPhase5Timeout
// UPDATEs are the synchronization point against concurrent abort or
// resume invocations.
func (o *Orchestrator) RunPhase5(ctx context.Context, rid RequestID) error {
	if o.phase5Coord == nil {
		return oops.Code("DEK_REKEY_COORDINATOR_NIL").
			Errorf("Phase 5 requires SetPhase5Coordinator(...) before RunPhase5")
	}

	ckpt, err := o.repo.Get(ctx, rid)
	if err != nil {
		return err
	}
	if ckpt.Status != CheckpointStatusPhase3ReencryptCold && ckpt.Status != CheckpointStatusPhase5Invalidate {
		return oops.Code("DEK_REKEY_PHASE_PRECONDITION_FAILED").
			With("expected", "phase3_reencrypt_cold or phase5_invalidate").
			With("actual", string(ckpt.Status)).
			Errorf("Phase 5 requires status=phase3_reencrypt_cold or phase5_invalidate")
	}
	if ckpt.NewDEKID == nil {
		return oops.Code("DEK_REKEY_NEW_DEK_MISSING").
			Errorf("Phase 5 requires new_dek_id set by Phase 2")
	}

	// Bump the attempt counter before the fan-out so an in-flight crash
	// is still reflected on the row when the operator resumes. IncrementPhase5Attempt
	// is best-effort: a failure here MUST NOT block the invalidation
	// fan-out (the counter is observability, not correctness). We log
	// and continue.
	if attemptErr := o.repo.IncrementPhase5Attempt(ctx, rid); attemptErr != nil {
		o.logger.WarnContext(ctx, "phase5 attempt counter increment failed",
			"request_id", rid.String(), "err", attemptErr.Error())
	}

	// Resolve the version numbers from the crypto_keys rows. The
	// orchestrator has the PKs on the checkpoint; selectByPK returns
	// the version column. INV-CRYPTO-109 calls for Version=old, SuccessorVersion=new
	// per the invalidation.Payload action table (rekey row).
	oldRow, err := o.store.selectByPK(ctx, ckpt.OldDEKID)
	if err != nil {
		return oops.Code("DEK_REKEY_PHASE5_OLD_ROW_LOOKUP_FAILED").
			With("old_dek_id", ckpt.OldDEKID).Wrap(err)
	}
	newRow, err := o.store.selectByPK(ctx, *ckpt.NewDEKID)
	if err != nil {
		return oops.Code("DEK_REKEY_PHASE5_NEW_ROW_LOOKUP_FAILED").
			With("new_dek_id", *ckpt.NewDEKID).Wrap(err)
	}

	ctxIDForCoord := ContextID{Type: ckpt.ContextType, ID: ckpt.ContextID}

	coordErr := o.phase5Coord.RequestInvalidation(
		ctx,
		ctxIDForCoord,
		Phase5ActionRekey,
		oldRow.Version,
		newRow.Version,
	)
	if coordErr == nil {
		// N-of-N success. Clear missing_members and ratchet status.
		if persistErr := o.repo.RecordPhase5Success(ctx, rid); persistErr != nil {
			return oops.Code("DEK_REKEY_PHASE5_RECORD_SUCCESS_FAILED").Wrap(persistErr)
		}
		return nil
	}

	// Partial-ack failure: extract the missing-member set from the oops
	// error context (the Coordinator stamps it via .With("missing_members", ...)).
	// Treat absence of the field as "unknown set" → empty list; the
	// operator surface still records the timeout with no member detail.
	missing := extractMissingMembers(coordErr)
	missingJSON, marshalErr := json.Marshal(missing)
	if marshalErr != nil {
		// Defensive: json.Marshal on []string cannot realistically fail.
		// If it does, persist an empty-list marker rather than abandoning
		// the timeout record.
		missingJSON = []byte("[]")
	}

	if persistErr := o.repo.RecordPhase5Timeout(ctx, rid, missingJSON); persistErr != nil {
		// Persisting the timeout failed AND the fan-out failed. Surface
		// both: the original Coordinator error is the cause, the persist
		// error is the secondary outcome the caller needs to know about
		// (the row is now in an indeterminate state from the operator's
		// perspective).
		return oops.Code("DEK_REKEY_PHASE5_TIMEOUT_PERSIST_FAILED").
			With("missing_members", missing).
			With("coordinator_err", coordErr.Error()).
			Wrap(persistErr)
	}

	return oops.Code("DEK_REKEY_PHASE5_TIMEOUT").
		With("missing_members", missing).
		With("attempt", ckpt.Phase5AttemptCount+1).
		With("coordinator_err", coordErr.Error()).
		Errorf("partial ack from coordinator; missing %d members", len(missing))
}

// RunPhase5WithForceDestroy is the operator-driven split-brain escape
// hatch. It bypasses Phase 5's invalidation fan-out and transitions the
// checkpoint directly to phase6_destroy_old (Phase 6 territory). This
// runs ONLY on the resume path AND only when the checkpoint is currently
// stuck in the "timed-out" state (status=phase5_invalidate AND
// phase5_missing_members IS NOT NULL).
//
// INV-CRYPTO-97: any other status / missing-members combo
// MUST be rejected with DEK_REKEY_FORCE_DESTROY_FORBIDDEN. This is the
// hard gate that prevents force-destroy from skipping Phase 5 when the
// fan-out has never been attempted.
//
// Steps:
//  1. Load the checkpoint; verify gate (INV-CRYPTO-97).
//  2. SetForceDestroy(true) so the audit projection and operator surface
//     can see the bypass was used.
//  3. UpdateStatusForceDestroy advances phase5_invalidate → phase6_destroy_old
//     with the CAS predicate (status='phase5_invalidate' AND force_destroy=true).
//
// On the operator side, the audit emit (Phase 7, holomush-jxo8.7.24) records
// `force_destroy: true` and `final_missing_members: [...]` per INV-CRYPTO-98.
// That row already persists from RunPhase5's RecordPhase5Timeout call; the
// Phase 7 emitter reads it back from the checkpoint.
//
// This call is NOT idempotent against repeated force-destroy invocations:
// after the first call advances status to phase6_destroy_old, a second
// call observes status=phase6_destroy_old and rejects with INV-CRYPTO-97. That's
// the right shape — repeated force-destroy is a real operator error
// (Phase 6 is the next step, not another Phase 5 retry).
func (o *Orchestrator) RunPhase5WithForceDestroy(ctx context.Context, rid RequestID) error {
	ckpt, err := o.repo.Get(ctx, rid)
	if err != nil {
		return err
	}
	// INV-CRYPTO-97: gate is (status==phase5_invalidate AND
	// phase5_missing_members IS NOT NULL). Phase5HasMissingMembers
	// encodes the NULL / "null" / "[]" tolerance.
	if ckpt.Status != CheckpointStatusPhase5Invalidate || !ckpt.Phase5HasMissingMembers() {
		return oops.Code("DEK_REKEY_FORCE_DESTROY_FORBIDDEN").
			With("status", string(ckpt.Status)).
			With("phase5_missing_members_set", ckpt.Phase5HasMissingMembers()).
			Errorf("INV-CRYPTO-97: --force-destroy requires checkpoint at phase5_invalidate with missing_members set")
	}

	// Audit signal: emit a structured warn-level log line so the
	// operator surface and any log-based monitoring catch the split-brain
	// bypass. INV-CRYPTO-98's audit-event capture is the canonical record
	// (lands in Phase 7); this log is the in-flight signal. A decode
	// failure here is non-fatal — the row clears INV-CRYPTO-97 by
	// Phase5HasMissingMembers, and the log line still fires with a nil
	// member list.
	missing, decodeErr := ckpt.Phase5MissingMembers()
	if decodeErr != nil {
		o.logger.WarnContext(
			ctx, "phase5 force-destroy: missing_members decode failed; logging without member list",
			"request_id", rid.String(),
			"err", decodeErr.Error(),
		)
		missing = nil
	}
	o.logger.WarnContext(
		ctx, "phase5 force-destroy bypass invoked — split-brain risk",
		"request_id", rid.String(),
		"context_type", ckpt.ContextType,
		"context_id", ckpt.ContextID,
		"missing_members", missing,
		"phase5_attempt_count", ckpt.Phase5AttemptCount,
	)

	if err := o.repo.SetForceDestroy(ctx, rid); err != nil {
		return oops.Code("DEK_REKEY_FORCE_DESTROY_SET_FAILED").Wrap(err)
	}
	if err := o.repo.UpdateStatusForceDestroy(ctx, rid); err != nil {
		return oops.Code("DEK_REKEY_FORCE_DESTROY_UPDATE_FAILED").Wrap(err)
	}
	return nil
}

// extractMissingMembers pulls the missing-member set from the
// Coordinator's typed oops error. The invalidation.Coordinator stamps
// the field via .With("missing_members", []cluster.MemberID) for
// PARTIAL_FAILURE, and the slice element type is cluster.MemberID
// (a string alias). To avoid an upward dependency on
// internal/cluster, we reflect over the underlying slice elements
// generically: strings stringify as themselves; ~string types
// stringify via fmt formatting; everything else falls back to %v.
//
// Returns nil when the field is absent or the wrong shape — callers
// treat that as "unknown missing set" rather than failing the timeout
// record.
func extractMissingMembers(err error) []string {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return nil
	}
	raw, ok := oopsErr.Context()["missing_members"]
	if !ok || raw == nil {
		return nil
	}
	// Fast path 1: []string directly.
	if s, ok := raw.([]string); ok {
		out := make([]string, len(s))
		copy(out, s)
		return out
	}
	// Fast path 2: []byte-style slice that JSON-decodes to []string. The
	// Coordinator does not currently produce this, but defensive.
	if b, ok := raw.([]byte); ok {
		var out []string
		if jsonErr := json.Unmarshal(b, &out); jsonErr == nil {
			return out
		}
		return nil
	}
	// Generic path: any slice whose elements have a string-like type
	// (cluster.MemberID is `type MemberID string`). Use a non-reflect
	// loop driven by a type switch on []any, then fall through to
	// reflection only if nothing matched.
	if anySlice, ok := raw.([]any); ok {
		out := make([]string, 0, len(anySlice))
		for _, v := range anySlice {
			out = append(out, stringifyMember(v))
		}
		return out
	}
	// The Coordinator's actual production type is []cluster.MemberID.
	// cluster.MemberID is `type MemberID string` (verified at
	// internal/cluster). Go does not auto-convert []MemberID to []string
	// at the type-assert layer, so we route through a generic helper.
	return stringSliceFromOpaque(raw)
}

// stringifyMember coerces a single element into its string representation.
func stringifyMember(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	default:
		return ""
	}
}

// stringSliceFromOpaque handles the []cluster.MemberID (and similar
// named-string-slice) case via reflection. Kept as a separate function
// so reflection appears only on the partial-failure cold path.
func stringSliceFromOpaque(raw any) []string {
	rv := reflect.ValueOf(raw)
	if !rv.IsValid() || rv.Kind() != reflect.Slice {
		return nil
	}
	n := rv.Len()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		elem := rv.Index(i)
		if elem.Kind() == reflect.String {
			out = append(out, elem.String())
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
