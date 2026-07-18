// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
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
//
// nudgeEnabled gates the OPTIONAL idle nudge (spec §4.4, OFF by default): when
// true, a scene_idle_nudge is emitted through sink after each active→paused
// transition. sink and gameID are the plugin's binary emit path (the same sink
// the publish emitters use) — the idle nudge is emitted via EventSink.Emit, not
// host-core eventbus.NewEvent() (review Concern 3).
type idleScheduler struct {
	store                  sceneIdleStore
	interval               time.Duration
	defaultIdleTimeoutSecs int
	nudgeEnabled           bool
	sink                   pluginsdk.EventSink
	gameID                 string
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
		// Optional idle nudge (spec §4.4, OFF by default). Emit failures are
		// WARN-logged and never abort the sweep — the transition already landed.
		if s.nudgeEnabled {
			if emitErr := s.emitIdleNudge(ctx, sc.ID); emitErr != nil {
				slog.WarnContext(ctx, "idle scheduler: idle-nudge emit failed",
					"scene_id", sc.ID, "err", emitErr)
			}
		}
	}
	return nil
}

// emitIdleNudge emits a scene_idle_nudge for the freshly-paused scene through
// the plugin's binary emit path (EventSink.Emit + EmitIntent), mirroring the
// publish emitters (publish_events.go). The wire type core-scenes:scene_idle_nudge
// and its emit-registry entry are already declared (main.go phase4EmitTypes +
// plugin.yaml crypto.emits) — this only calls the emitter (INV-PLUGIN-32).
//
// The payload carries only the scene_id; the telnet gateway reads it from the
// frame payload to render gamenotice.Idle (no DB lookup — gateway-boundary).
// sensitivity is never (plaintext by design): the notice leaks no scene content.
func (s *idleScheduler) emitIdleNudge(ctx context.Context, sceneID string) error {
	if s.sink == nil {
		return nil
	}
	payload, err := json.Marshal(struct {
		SceneID string `json:"scene_id"`
	}{SceneID: sceneID})
	if err != nil {
		return oops.Code("SCENE_IDLE_NUDGE_MARSHAL_FAILED").With("scene_id", sceneID).Wrap(err)
	}
	return s.sink.Emit(ctx, pluginsdk.EmitIntent{ //nolint:wrapcheck // EventSink error passes through as-is; the sweep logs it
		Subject:   dotStyleSceneSubjectIC(s.gameID, sceneID),
		Type:      pluginsdk.EventType("core-scenes:scene_idle_nudge"),
		Payload:   string(payload),
		Sensitive: false,
	})
}
