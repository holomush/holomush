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

// sceneIdleStore is the narrow persistence interface the idle scheduler needs.
// Kept minimal (not the full sceneStorer) so the scheduler dependency is
// independently mockable, mirroring sceneScheduleStore for publishScheduler.
type sceneIdleStore interface {
	// ListScenesIdlePastThreshold returns active scenes whose effective idle
	// timeout (per-scene override or the supplied game-wide default) has
	// elapsed as of nowNs (epoch-nanoseconds, Go-clock). Paused scenes are
	// never returned.
	ListScenesIdlePastThreshold(ctx context.Context, nowNs int64, defaultIdleTimeoutSecs int) ([]idleScene, error)
	// Pause transitions an active scene to paused, returning the post-update
	// row. A scene not in the active state yields a transition-miss error.
	Pause(ctx context.Context, id string) (*SceneRow, error)
}

// idleScheduler periodically sweeps for active scenes idle past their threshold
// and transitions them active → paused. It mirrors publishScheduler: an
// injected now func makes the sweep deterministically testable, and sweep is
// package-private so tests can call it directly without waiting for ticks.
//
// defaultIdleTimeoutSecs is the game-wide idle default decoded from plugin
// config (main.go applyConfig) and passed EXPLICITLY into the store query
// (review Finding 1) — the store holds only a pool and cannot see config; the
// per-scene idle_timeout_secs column overrides this default via COALESCE.
type idleScheduler struct {
	store                  sceneIdleStore
	interval               time.Duration
	defaultIdleTimeoutSecs int
	now                    func() time.Time
}

// Run starts the scheduler loop. It ticks at s.interval and calls sweep on each
// tick. The loop exits when ctx is cancelled (plugin shutdown).
func (s *idleScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.sweep(ctx); err != nil {
				errutil.LogErrorContext(ctx, "idle scheduler sweep failed", err)
			}
		}
	}
}

// sweep is one pass: transition every active scene idle past its threshold to
// paused. Per-scene failures are WARN-logged and the batch continues; one bad
// row MUST NOT abort the sweep (publishScheduler precedent). IsValidTransition
// gates each row defensively even though the query only returns active scenes.
func (s *idleScheduler) sweep(ctx context.Context) error {
	nowNs := s.now().UnixNano() // pgnanos-exempt: scheduler clock — injected now() returns Go-clock time; result is passed as a parameter to SQL (noremoteclockcompare-compliant)

	scenes, err := s.store.ListScenesIdlePastThreshold(ctx, nowNs, s.defaultIdleTimeoutSecs)
	if err != nil {
		return oops.Code("SCENE_IDLE_SCHEDULER_SCAN_FAILED").Wrap(err)
	}
	for _, sc := range scenes {
		if !IsValidTransition(SceneState(sc.State), SceneStatePaused) {
			continue
		}
		if _, pauseErr := s.store.Pause(ctx, sc.ID); pauseErr != nil {
			slog.WarnContext(ctx, "idle scheduler: pause failed",
				"scene_id", sc.ID, "err", pauseErr)
			// Continue to the next scene; one failure MUST NOT abort the sweep.
			continue
		}
	}
	return nil
}
