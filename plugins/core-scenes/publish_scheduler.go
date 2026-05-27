// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
)

// scheduledAttempt is a minimal projection of a published_scenes row
// carrying only the fields the scheduler needs: the attempt ID, the scene ID
// (for runSnapshot's scene_id argument), and the scene's game ID is sourced
// from the service (s.svc.gameID). The store scan methods return this to
// avoid loading content_entries (unused here) or importing the full
// PublishedScene for a subset of fields.
type scheduledAttempt struct {
	ID      string // published_scenes.id (attempt ID)
	SceneID string // published_scenes.scene_id
}

// publishScheduler periodically sweeps for:
//   - COLLECTING attempts whose vote_window has elapsed → applyTrigger(TriggerTimeout)
//   - COOLOFF attempts whose cooloff_window has elapsed → runSnapshot(...)
//
// The now field makes the sweep deterministically testable — tests inject a
// fixed time; production defaults to time.Now. The sweep method is exported
// (lowercase, package-private) so integration tests can call it directly
// without waiting for ticker intervals.
type publishScheduler struct {
	svc      *SceneServiceImpl
	store    sceneScheduleStore
	interval time.Duration
	now      func() time.Time
}

// sceneScheduleStore is the persistence interface required by publishScheduler.
// Separated from sceneStorer so the scheduler dependency is minimal and
// independently mockable.
type sceneScheduleStore interface {
	// ListExpiredCollecting returns COLLECTING attempts whose
	// initiated_at + vote_window ≤ nowNs (epoch-nanoseconds, Go-clock supplied).
	// Returns only the ID and SceneID columns needed by the timeout sweep.
	ListExpiredCollecting(ctx context.Context, nowNs int64) ([]scheduledAttempt, error)

	// ListExpiredCoolOff returns COOLOFF attempts whose
	// cooloff_started_at + cooloff_window ≤ nowNs (epoch-nanoseconds, Go-clock).
	// Returns only the ID and SceneID columns needed by the snapshot sweep.
	ListExpiredCoolOff(ctx context.Context, nowNs int64) ([]scheduledAttempt, error)
}

// Run starts the scheduler loop. It ticks at s.interval and calls sweep on
// each tick. The loop exits when ctx is cancelled (plugin shutdown).
func (s *publishScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sweep(ctx); err != nil {
				errutil.LogErrorContext(ctx, "publish scheduler sweep failed", err)
			}
		}
	}
}

// sweep is the work unit: one pass over expired COLLECTING and COOLOFF attempts.
// Per-attempt errors are WARN-logged and the sweep continues; a failed attempt
// must not abort processing of the remaining batch. The method is idempotent:
// applyTrigger and runSnapshot both re-validate the attempt status under a lock,
// so a double-fire is a safe no-op.
func (s *publishScheduler) sweep(ctx context.Context) error {
	nowNs := s.now().UnixNano() // pgnanos-exempt: scheduler clock — injected now() returns Go-clock time; result is passed as a parameter to SQL (noremoteclockcompare-compliant)

	// ── Phase 1: COLLECTING → ATTEMPT_FAILED (timeout) ───────────────────────
	collecting, err := s.store.ListExpiredCollecting(ctx, nowNs)
	if err != nil {
		return oops.Code("SCENE_SCHEDULER_SCAN_COLLECTING_FAILED").Wrap(err)
	}
	for _, a := range collecting {
		if applyErr := s.svc.applyTrigger(ctx, a.ID, TriggerTimeout); applyErr != nil {
			slog.WarnContext(ctx, "scheduler: applyTrigger(TriggerTimeout) failed",
				"attempt_id", a.ID, "scene_id", a.SceneID, "err", applyErr)
			// Continue to the next attempt; one failure MUST NOT abort the sweep.
		}
	}

	// ── Phase 2: COOLOFF → PUBLISHED (snapshot) ───────────────────────────────
	cooloff, err := s.store.ListExpiredCoolOff(ctx, nowNs)
	if err != nil {
		return oops.Code("SCENE_SCHEDULER_SCAN_COOLOFF_FAILED").Wrap(err)
	}
	for _, a := range cooloff {
		// Pass the PRODUCTION .ic subject so the AEAD AAD matches the encrypt-time
		// subject. A wrong subject = silent tag-check failure (every decrypt fails).
		// INV-CRIT-E5: this is the C7 fidelity invariant both reviewers flagged.
		icSubject := dotStyleSceneSubjectIC(s.svc.gameID, a.SceneID)
		if snapErr := s.svc.runSnapshot(ctx, a.ID, a.SceneID, icSubject); snapErr != nil {
			slog.WarnContext(ctx, "scheduler: runSnapshot failed",
				"attempt_id", a.ID, "scene_id", a.SceneID, "err", snapErr)
			// Continue to the next attempt.
		}
	}

	return nil
}
