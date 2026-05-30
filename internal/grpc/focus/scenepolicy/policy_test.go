// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package scenepolicy

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
)

func TestScenePolicyKindReturnsScene(t *testing.T) {
	p := New()
	assert.Equal(t, session.FocusKindScene, p.Kind())
}

func TestScenePolicyStreamsForReturnsTwoStreams(t *testing.T) {
	p := New()
	sceneID := ulid.Make()
	streams := p.StreamsFor(session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID})
	require.Len(t, streams, 2)
	assert.Equal(t, "scene."+sceneID.String()+".ic", streams[0])
	assert.Equal(t, "scene."+sceneID.String()+".ooc", streams[1])
}

func TestScenePolicyStreamsForPrimaryIsIC(t *testing.T) {
	p := New()
	streams := p.StreamsFor(session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()})
	assert.Contains(t, streams[0], ".ic")
}

func TestScenePolicyOnJoinReturnsBoundedTailForICAndLiveOnlyForOOC(t *testing.T) {
	p := New()
	sceneID := ulid.Make()
	pctx := focus.PolicyContext{
		SessionID:            "sess-1",
		Target:               session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID},
		SceneFocusReplayTail: 5,
	}
	result, err := p.OnJoin(pctx)
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, "scene."+sceneID.String()+".ic", result[0].Stream)
	assert.Equal(t, focus.ReplayModeBoundedTail, result[0].Mode)
	assert.Equal(t, 5, result[0].TailCount)
	assert.Equal(t, "scene."+sceneID.String()+".ooc", result[1].Stream)
	assert.Equal(t, focus.ReplayModeLiveOnly, result[1].Mode)
}

func TestScenePolicyOnJoinUsesZeroTailCountWhenConfiguredToZero(t *testing.T) {
	p := New()
	pctx := focus.PolicyContext{
		SessionID:            "sess-1",
		Target:               session.FocusKey{Kind: session.FocusKindScene, TargetID: ulid.Make()},
		SceneFocusReplayTail: 0,
	}
	result, err := p.OnJoin(pctx)
	require.NoError(t, err)
	assert.Equal(t, focus.ReplayModeBoundedTail, result[0].Mode)
	assert.Equal(t, 0, result[0].TailCount)
}

func TestScenePolicyOnRestoreReturnsFromCursorForBothStreams(t *testing.T) {
	p := New()
	sceneID := ulid.Make()
	pctx := focus.PolicyContext{
		SessionID:            "sess-1",
		Target:               session.FocusKey{Kind: session.FocusKindScene, TargetID: sceneID},
		SceneFocusReplayTail: 5,
	}
	result, err := p.OnRestore(pctx)
	require.NoError(t, err)
	require.Len(t, result, 2)
	assert.Equal(t, focus.ReplayModeFromCursor, result[0].Mode)
	assert.Equal(t, focus.ReplayModeFromCursor, result[1].Mode)
}

func TestScenePolicyIsPureAcrossDifferentTargets(t *testing.T) {
	p := New()
	pctx := focus.PolicyContext{SessionID: "sess-1", SceneFocusReplayTail: 3}
	id1, id2 := ulid.Make(), ulid.Make()

	pctx.Target = session.FocusKey{Kind: session.FocusKindScene, TargetID: id1}
	result1, err := p.OnJoin(pctx)
	require.NoError(t, err)

	pctx.Target = session.FocusKey{Kind: session.FocusKindScene, TargetID: id2}
	result2, err := p.OnJoin(pctx)
	require.NoError(t, err)

	require.Len(t, result1, len(result2))
	for i := range result1 {
		assert.Equal(t, result1[i].Mode, result2[i].Mode)
		assert.Equal(t, result1[i].TailCount, result2[i].TailCount)
	}
}
