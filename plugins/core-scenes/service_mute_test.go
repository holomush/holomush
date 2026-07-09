// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// ── MuteScene (scene-scoped, participant-gated) ───────────────────────────────

// TestMuteSceneParticipantPersistsMute asserts that a participant (allow
// evaluator) persists the mute via SceneStore.SetSceneMute and the handler
// forwards the request character_id, scene_id, and muted flag verbatim.
func TestMuteSceneParticipantPersistsMute(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetHostEvaluator(allowEvaluator{})

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-alice")
	_, err := svc.MuteScene(ctx, &scenev1.MuteSceneRequest{
		CharacterId: "char-alice", SceneId: "scene-1", Muted: true,
	})
	require.NoError(t, err)
	require.Len(t, store.setSceneMuteCalls, 1)
	assert.Equal(t, muteCall{characterID: "char-alice", sceneID: "scene-1", muted: true}, store.setSceneMuteCalls[0])
}

// TestMuteSceneUnmuteClearsMute asserts that muted=false forwards a clear to the
// store (the `scene unmute` path).
func TestMuteSceneUnmuteClearsMute(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetHostEvaluator(allowEvaluator{})

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-alice")
	_, err := svc.MuteScene(ctx, &scenev1.MuteSceneRequest{
		CharacterId: "char-alice", SceneId: "scene-1", Muted: false,
	})
	require.NoError(t, err)
	require.Len(t, store.setSceneMuteCalls, 1)
	assert.False(t, store.setSceneMuteCalls[0].muted)
}

// TestMuteSceneScopedToSceneResource asserts the Layer-2 gate evaluates the
// "mute" action against the scene:<id> resource (participant-gated).
func TestMuteSceneScopedToSceneResource(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	ev := &recordingEvaluator{decision: pluginsdk.EvaluateDecision{Allowed: true}}
	svc.SetHostEvaluator(ev)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-alice")
	_, err := svc.MuteScene(ctx, &scenev1.MuteSceneRequest{
		CharacterId: "char-alice", SceneId: "scene-42", Muted: true,
	})
	require.NoError(t, err)
	require.Len(t, ev.calls, 1)
	assert.Equal(t, "mute", ev.calls[0].action)
	assert.Equal(t, "scene:scene-42", ev.calls[0].resource)
}

// TestMuteSceneNonParticipantDenied asserts a non-participant (deny evaluator)
// is refused PermissionDenied with no store write.
func TestMuteSceneNonParticipantDenied(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetHostEvaluator(denyEvaluator{})

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-mallory")
	_, err := svc.MuteScene(ctx, &scenev1.MuteSceneRequest{
		CharacterId: "char-mallory", SceneId: "scene-1", Muted: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, store.setSceneMuteCalls, "denied mute MUST NOT write the store")
}

// TestMuteSceneEvaluatorErrorFailsClosed asserts an evaluator infra error is
// denied fail-closed (Internal) with no store write.
func TestMuteSceneEvaluatorErrorFailsClosed(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetHostEvaluator(errorEvaluator{})

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-alice")
	_, err := svc.MuteScene(ctx, &scenev1.MuteSceneRequest{
		CharacterId: "char-alice", SceneId: "scene-1", Muted: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Empty(t, store.setSceneMuteCalls)
}

// TestMuteSceneNilEvaluatorFailsClosed asserts an unconfigured evaluator is
// denied fail-closed with no store write.
func TestMuteSceneNilEvaluatorFailsClosed(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	// evaluator intentionally not set.

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-alice")
	_, err := svc.MuteScene(ctx, &scenev1.MuteSceneRequest{
		CharacterId: "char-alice", SceneId: "scene-1", Muted: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Empty(t, store.setSceneMuteCalls)
}

// TestMuteSceneRejectsForgedActingCharacter asserts the actor-metadata guard
// rejects a request whose character_id contradicts the vouched actor before any
// evaluation or store write.
func TestMuteSceneRejectsForgedActingCharacter(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	ev := &recordingEvaluator{decision: pluginsdk.EvaluateDecision{Allowed: true}}
	svc.SetHostEvaluator(ev)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-actual")
	_, err := svc.MuteScene(ctx, &scenev1.MuteSceneRequest{
		CharacterId: "char-forged", SceneId: "scene-1", Muted: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, ev.calls, "guard must reject before ABAC evaluation")
	assert.Empty(t, store.setSceneMuteCalls, "guard must reject before store write")
}

// ── SetSceneNotifyPref (character-self-scoped) ────────────────────────────────

// TestSetSceneNotifyPrefPersistsForSelf asserts a matching actor writes its own
// global pref and the handler performs NO scene:<id> ABAC evaluation.
func TestSetSceneNotifyPrefPersistsForSelf(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	ev := &recordingEvaluator{decision: pluginsdk.EvaluateDecision{Allowed: true}}
	svc.SetHostEvaluator(ev)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-alice")
	_, err := svc.SetSceneNotifyPref(ctx, &scenev1.SetSceneNotifyPrefRequest{
		CharacterId: "char-alice", Enabled: false,
	})
	require.NoError(t, err)
	require.Len(t, store.setSceneNotifyPrefCalls, 1)
	assert.Equal(t, notifyPrefCall{characterID: "char-alice", enabled: false}, store.setSceneNotifyPrefCalls[0])
	assert.Empty(t, ev.calls, "character-self notify-pref MUST NOT evaluate a scene:<id> resource")
}

// TestSetSceneNotifyPrefRejectsForgedActingCharacter asserts a mismatched actor
// is refused PermissionDenied with no store write (the self-scope guard).
func TestSetSceneNotifyPrefRejectsForgedActingCharacter(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-actual")
	_, err := svc.SetSceneNotifyPref(ctx, &scenev1.SetSceneNotifyPrefRequest{
		CharacterId: "char-forged", Enabled: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, store.setSceneNotifyPrefCalls)
}

// ── GetSceneNotifyPref (character-self-scoped) ────────────────────────────────

// TestGetSceneNotifyPrefReturnsPersistedPref asserts the caller's persisted
// enabled/mode round-trip out of the store.
func TestGetSceneNotifyPrefReturnsPersistedPref(t *testing.T) {
	store := newFakeStore()
	store.notifyPrefEnabled = false
	store.notifyPrefMode = "realtime"
	svc := newTestService(t, store)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-alice")
	resp, err := svc.GetSceneNotifyPref(ctx, &scenev1.GetSceneNotifyPrefRequest{
		CharacterId: "char-alice",
	})
	require.NoError(t, err)
	assert.False(t, resp.GetEnabled())
	assert.Equal(t, "realtime", resp.GetMode())
}

// TestGetSceneNotifyPrefDefaultsEnabled asserts the default-enabled read when
// the character has no prefs row (fakeStore configured to mirror the store's
// (true, "realtime") default).
func TestGetSceneNotifyPrefDefaultsEnabled(t *testing.T) {
	store := newFakeStore()
	store.notifyPrefEnabled = true // store default on pgx.ErrNoRows
	svc := newTestService(t, store)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-nobody")
	resp, err := svc.GetSceneNotifyPref(ctx, &scenev1.GetSceneNotifyPrefRequest{
		CharacterId: "char-nobody",
	})
	require.NoError(t, err)
	assert.True(t, resp.GetEnabled())
	assert.Equal(t, "realtime", resp.GetMode())
}

// TestGetSceneNotifyPrefRejectsForgedActingCharacter asserts a mismatched actor
// is refused PermissionDenied (self-scope).
func TestGetSceneNotifyPrefRejectsForgedActingCharacter(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-actual")
	_, err := svc.GetSceneNotifyPref(ctx, &scenev1.GetSceneNotifyPrefRequest{
		CharacterId: "char-forged",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// ── ListMutedScenes (character-self-scoped) ───────────────────────────────────

// TestListMutedScenesReturnsMutedIDs asserts the caller's muted scene ids
// round-trip out of the store.
func TestListMutedScenesReturnsMutedIDs(t *testing.T) {
	store := newFakeStore()
	store.mutedScenes = []string{"scene-a", "scene-b"}
	svc := newTestService(t, store)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-alice")
	resp, err := svc.ListMutedScenes(ctx, &scenev1.ListMutedScenesRequest{
		CharacterId: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"scene-a", "scene-b"}, resp.GetSceneIds())
}

// TestListMutedScenesRejectsForgedActingCharacter asserts a mismatched actor is
// refused PermissionDenied (self-scope) with no store read leaking.
func TestListMutedScenesRejectsForgedActingCharacter(t *testing.T) {
	store := newFakeStore()
	store.mutedScenes = []string{"scene-secret"}
	svc := newTestService(t, store)

	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, "char-actual")
	_, err := svc.ListMutedScenes(ctx, &scenev1.ListMutedScenesRequest{
		CharacterId: "char-forged",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// ── WR-02 (holomush-gl751): fail CLOSED on absent/unverified actor metadata ────
//
// The character-self notify-pref trio has NO ABAC backstop (the plugin evaluator
// rejects character:<id> resources outside owned types), so mismatchedActingCharacter
// is their SOLE gate. Absent advisory actor metadata means the caller's identity is
// unverified — it MUST be denied (default-deny), not allowed. In production
// BeginServiceDispatch always stamps a matching character actor (sub_grpc.go:592,
// sceneaccess_service.go facade), so these only fire on a non-character or
// misconfigured dispatch — closing the fail-OPEN hole flagged as WR-02.

// TestSetSceneNotifyPrefDeniesAbsentActorMetadata asserts a write is refused when
// the ctx carries no advisory actor metadata (no write leaks).
func TestSetSceneNotifyPrefDeniesAbsentActorMetadata(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)

	_, err := svc.SetSceneNotifyPref(context.Background(), &scenev1.SetSceneNotifyPrefRequest{
		CharacterId: "char-alice", Enabled: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, store.setSceneNotifyPrefCalls, "no write on unverified identity")
}

// TestGetSceneNotifyPrefDeniesAbsentActorMetadata asserts a read is refused when
// the ctx carries no advisory actor metadata.
func TestGetSceneNotifyPrefDeniesAbsentActorMetadata(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)

	_, err := svc.GetSceneNotifyPref(context.Background(), &scenev1.GetSceneNotifyPrefRequest{
		CharacterId: "char-alice",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// TestListMutedScenesDeniesAbsentActorMetadata asserts the muted-scene list does
// not leak when the ctx carries no advisory actor metadata.
func TestListMutedScenesDeniesAbsentActorMetadata(t *testing.T) {
	store := newFakeStore()
	store.mutedScenes = []string{"scene-secret"}
	svc := newTestService(t, store)

	_, err := svc.ListMutedScenes(context.Background(), &scenev1.ListMutedScenesRequest{
		CharacterId: "char-alice",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}
