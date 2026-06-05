// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package dek

import (
	"context"

	"github.com/samber/oops"
)

// Destroyer abstracts the Phase 6 old-DEK destruction + local-cache
// eviction operations so that the Orchestrator is decoupled from the
// concrete *manager type. Mirrors the Minter / MaterialResolver / Phase5Coordinator
// seam pattern used by earlier phases.
//
// The sole production implementation is *manager (via DestroyDEK +
// EvictCachedDEK). Tests may substitute a fake.
type Destroyer interface {
	// DestroyDEK soft-deletes the crypto_keys row by primary key id.
	// Idempotent: a row already destroyed is a no-op success
	// (INV-CRYPTO-99).
	DestroyDEK(ctx context.Context, dekID int64) error

	// EvictCachedDEK removes the DEK's context from the local DEK
	// material and participants caches.
	EvictCachedDEK(ctx context.Context, dekID int64) error
}

// SetDestroyer installs the Phase 6 destroy + evict seam. Production
// wiring at cmd/holomush/core.go MUST call this after NewOrchestrator before
// invoking RunPhase6. Calling RunPhase6 without a destroyer wired returns
// DEK_REKEY_DESTROYER_NIL — fail closed.
//
// Mirrors the SetMaterialResolver / SetPhase5Coordinator additive
// post-construction seam pattern: NewOrchestrator's signature is
// intentionally unchanged so wiring from earlier beads continues to compile.
func (o *Orchestrator) SetDestroyer(d Destroyer) {
	o.dekDestroyer = d
}

// RunPhase6 destroys the old DEK (INV-CRYPTO-99) and advances
// the checkpoint from phase5_invalidate to phase6_destroy_old.
//
// Pre-conditions: checkpoint.Status MUST be CheckpointStatusPhase5Invalidate
// (clean success path, phase5_missing_members IS NULL) or
// CheckpointStatusPhase6DestroyOld (already-destroyed idempotent path). The
// force-destroy variant (phase5_invalidate with missing_members set) is
// handled by RunPhase5WithForceDestroy in rekey_phase5.go, which transitions
// to phase6_destroy_old directly; RunPhase6 then runs normally from there.
//
// Algorithm:
//  1. Load the checkpoint row; verify status is a valid Phase 6 entry point.
//  2. If status == phase6_destroy_old, return nil (idempotent re-invoke).
//  3. Call DEKDestroyer.DestroyDEK(ctx, ckpt.OldDEKID) — idempotent SQL UPDATE.
//  4. Call DEKDestroyer.EvictCachedDEK(ctx, ckpt.OldDEKID) — local cache
//     eviction (other replicas already evicted in Phase 5 cluster invalidation).
//  5. CAS UPDATE: transition phase5_invalidate → phase6_destroy_old.
//
// INV-CRYPTO-99: a second RunPhase6 invocation on an already-
// phase6_destroy_old checkpoint returns nil immediately (step 2). The
// underlying markDestroyedByPK SQL also uses WHERE destroyed_at IS NULL,
// making the DB write independently idempotent.
func (o *Orchestrator) RunPhase6(ctx context.Context, rid RequestID) error {
	if o.dekDestroyer == nil {
		return oops.Code("DEK_REKEY_DESTROYER_NIL").
			Errorf("Phase 6 requires SetDEKDestroyer(...) before RunPhase6")
	}

	ckpt, err := o.repo.Get(ctx, rid)
	if err != nil {
		return err
	}

	switch ckpt.Status {
	case CheckpointStatusPhase5Invalidate:
		// Normal happy path: Phase 5 succeeded (missing_members IS NULL).
		// force-destroy path enters as phase6_destroy_old (see RunPhase5WithForceDestroy).
		if ckpt.Phase5HasMissingMembers() {
			// Phase 5 timed out with missing members — operator must invoke
			// RunPhase5WithForceDestroy before RunPhase6. Reject cleanly.
			return oops.Code("DEK_REKEY_PHASE_PRECONDITION_FAILED").
				With("status", string(ckpt.Status)).
				With("phase5_missing_members_set", true).
				Errorf("Phase 6 requires phase5_invalidate with missing_members IS NULL; " +
					"use --force-destroy to bypass cluster invalidation requirement")
		}
	case CheckpointStatusPhase6DestroyOld:
		// Idempotent re-invoke: old DEK already destroyed. Return nil
		// per INV-CRYPTO-99.
		return nil
	default:
		return oops.Code("DEK_REKEY_PHASE_PRECONDITION_FAILED").
			With("expected", "phase5_invalidate (clean) or phase6_destroy_old").
			With("actual", string(ckpt.Status)).
			Errorf("Phase 6 requires status=phase5_invalidate (missing_members IS NULL) or phase6_destroy_old")
	}

	// Soft-delete the old DEK row. Idempotent at the SQL layer.
	if err := o.dekDestroyer.DestroyDEK(ctx, ckpt.OldDEKID); err != nil {
		return oops.Code("DEK_REKEY_DESTROY_FAILED").Wrap(err)
	}

	// Evict local caches so in-process reads no longer find old material.
	// Other replicas already evicted in Phase 5's cluster invalidation.
	if err := o.dekDestroyer.EvictCachedDEK(ctx, ckpt.OldDEKID); err != nil {
		// Cache eviction failure is non-fatal for correctness — the row is
		// already destroyed so any Resolve call will miss the DB row. Log
		// via the CAS update path; do not block the state transition.
		o.logger.WarnContext(
			ctx, "phase6 local cache eviction failed; proceeding with state transition",
			"request_id", rid.String(),
			"old_dek_id", ckpt.OldDEKID,
			"err", err.Error(),
		)
	}

	// CAS UPDATE: phase5_invalidate → phase6_destroy_old.
	return o.repo.UpdateStatus(ctx, rid, CheckpointStatusPhase5Invalidate, CheckpointStatusPhase6DestroyOld)
}
