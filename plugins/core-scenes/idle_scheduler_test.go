// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// fakeIdleStore is a hand-rolled sceneIdleStore double for sweep unit tests.
// The sweep's SQL behaviour (threshold boundary, per-scene COALESCE override,
// paused-excluded) is covered by the integration suite against a real DB; this
// double exercises the sweep control flow (transition per row, per-row fault
// tolerance, defensive IsValidTransition gate, scan-error wrap).
type fakeIdleStore struct {
	listResult []idleScene
	listErr    error
	pauseErr   map[string]error // per-scene-id Pause error (nil ⇒ success)
	paused     []string         // scene ids Pause was called on, in order
}

func (f *fakeIdleStore) ListScenesIdlePastThreshold(_ context.Context, _ int64, _ int) ([]idleScene, error) {
	return f.listResult, f.listErr
}

func (f *fakeIdleStore) Pause(_ context.Context, id string) (*SceneRow, error) {
	f.paused = append(f.paused, id)
	if f.pauseErr != nil {
		if err := f.pauseErr[id]; err != nil {
			return nil, err
		}
	}
	return &SceneRow{ID: id, State: string(SceneStatePaused)}, nil
}

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

// fakeEmitSink captures EmitIntents so idle-nudge emission can be asserted.
type fakeEmitSink struct {
	intents []pluginsdk.EmitIntent
}

func (f *fakeEmitSink) Emit(_ context.Context, intent pluginsdk.EmitIntent) error {
	f.intents = append(f.intents, intent)
	return nil
}

func TestIdleSweepDoesNotEmitNudgeWhenDisabled(t *testing.T) {
	t.Parallel()
	store := &fakeIdleStore{listResult: []idleScene{
		{ID: "01SCENE_IDLE_NOEMIT", State: string(SceneStateActive)},
	}}
	sink := &fakeEmitSink{}
	sched := &idleScheduler{
		store: store, defaultIdleTimeoutSecs: 1800, now: fixedNow(time.Unix(0, 1)),
		nudgeEnabled: false, sink: sink, gameID: "main",
	}

	require.NoError(t, sched.sweep(context.Background()))
	assert.Equal(t, []string{"01SCENE_IDLE_NOEMIT"}, store.paused,
		"the scene is still transitioned when the nudge is off")
	assert.Empty(t, sink.intents, "no idle nudge is emitted when the flag is OFF (default)")
}

func TestIdleSweepEmitsNudgeWithScenePayloadWhenEnabled(t *testing.T) {
	t.Parallel()
	store := &fakeIdleStore{listResult: []idleScene{
		{ID: "01SCENE_IDLE_EMIT", State: string(SceneStateActive)},
	}}
	sink := &fakeEmitSink{}
	sched := &idleScheduler{
		store: store, defaultIdleTimeoutSecs: 1800, now: fixedNow(time.Unix(0, 1)),
		nudgeEnabled: true, sink: sink, gameID: "main",
	}

	require.NoError(t, sched.sweep(context.Background()))
	require.Len(t, sink.intents, 1, "exactly one idle nudge is emitted when the flag is ON")
	got := sink.intents[0]
	assert.Equal(t, pluginsdk.EventType("core-scenes:scene_idle_nudge"), got.Type)
	assert.False(t, got.Sensitive, "the idle nudge is sensitivity: never (plaintext by design)")

	var payload struct {
		SceneID string `json:"scene_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(got.Payload), &payload))
	assert.Equal(t, "01SCENE_IDLE_EMIT", payload.SceneID,
		"the idle-nudge payload carries the scene_id the telnet render reads")
}

func TestIdleSweepPausesEveryActiveSceneReturned(t *testing.T) {
	t.Parallel()
	store := &fakeIdleStore{listResult: []idleScene{
		{ID: "01SCENE_IDLE_A", State: string(SceneStateActive)},
		{ID: "01SCENE_IDLE_B", State: string(SceneStateActive)},
	}}
	sched := &idleScheduler{store: store, defaultIdleTimeoutSecs: 1800, now: fixedNow(time.Unix(0, 1))}

	require.NoError(t, sched.sweep(context.Background()))
	assert.Equal(t, []string{"01SCENE_IDLE_A", "01SCENE_IDLE_B"}, store.paused,
		"every active scene past threshold must be transitioned to paused")
}

func TestIdleSweepContinuesWhenOneRowPauseFails(t *testing.T) {
	t.Parallel()
	store := &fakeIdleStore{
		listResult: []idleScene{
			{ID: "01SCENE_IDLE_FAIL", State: string(SceneStateActive)},
			{ID: "01SCENE_IDLE_OK", State: string(SceneStateActive)},
		},
		pauseErr: map[string]error{"01SCENE_IDLE_FAIL": errors.New("transient pause failure")},
	}
	sched := &idleScheduler{store: store, defaultIdleTimeoutSecs: 1800, now: fixedNow(time.Unix(0, 1))}

	require.NoError(t, sched.sweep(context.Background()),
		"a per-row pause failure MUST NOT abort the sweep")
	assert.Equal(t, []string{"01SCENE_IDLE_FAIL", "01SCENE_IDLE_OK"}, store.paused,
		"the sweep continues to the next scene after a per-row failure")
}

func TestIdleSweepSkipsNonActiveScenesViaTransitionGate(t *testing.T) {
	t.Parallel()
	// A paused row (which the query should never return, but the sweep gates
	// defensively) is skipped: IsValidTransition(paused, paused) is false.
	store := &fakeIdleStore{listResult: []idleScene{
		{ID: "01SCENE_ALREADY_PAUSED", State: string(SceneStatePaused)},
	}}
	sched := &idleScheduler{store: store, defaultIdleTimeoutSecs: 1800, now: fixedNow(time.Unix(0, 1))}

	require.NoError(t, sched.sweep(context.Background()))
	assert.Empty(t, store.paused,
		"a non-active scene must not be re-transitioned (defensive IsValidTransition gate)")
}

func TestIdleSweepWrapsScanError(t *testing.T) {
	t.Parallel()
	store := &fakeIdleStore{listErr: errors.New("boom")}
	sched := &idleScheduler{store: store, defaultIdleTimeoutSecs: 1800, now: fixedNow(time.Unix(0, 1))}

	err := sched.sweep(context.Background())
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_IDLE_SCHEDULER_SCAN_FAILED")
}
