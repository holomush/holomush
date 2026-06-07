// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// newTestPluginWithMember returns a scene plugin with a single scene
// (owned by char-owner) where char-alice is added as a member. The
// recordingEventSink is wired so tests can inspect emitted intents.
// Returns the plugin and the sink.
func newTestPluginWithMember(t *testing.T, sceneID string) (*scenePlugin, *recordingEventSink) {
	t.Helper()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: sceneID, OwnerID: "char-owner",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), sceneID, "char-alice")
	require.NoError(t, err)

	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	// allowEvaluator: emit gate requires an evaluator; use allow-all so tests assert business logic.
	return &scenePlugin{service: svc, evaluator: allowEvaluator{}}, sink
}

func TestSceneSubcommand_Pose_EmitsSceneEventOnICFacet(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-pose-test")

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "pose smiles at the room",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "You pose: smiles at the room")

	found := findIntentByType(sink.intents, "core-scenes:scene_pose")
	require.NotNil(t, found, "scene pose MUST emit scene_pose")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-pose-test"), found.Subject)
	assert.True(t, found.Sensitive, "scene_pose MUST be Sensitive=true (sensitivity:always)")
	assert.Contains(t, found.Payload, `"actor_id":"char-alice"`)
	assert.Contains(t, found.Payload, `"scene_id":"scene-pose-test"`)
	assert.Contains(t, found.Payload, `"text":"smiles at the room"`)
}

func TestSceneSubcommand_Say_EmitsSceneEventOnICFacet(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-say-test")

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "say hello everyone",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "You say: hello everyone")

	found := findIntentByType(sink.intents, "core-scenes:scene_say")
	require.NotNil(t, found, "scene say MUST emit scene_say")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-say-test"), found.Subject)
	assert.True(t, found.Sensitive, "scene_say MUST be Sensitive=true (sensitivity:always)")
	assert.Contains(t, found.Payload, `"text":"hello everyone"`)
}

func TestSceneSubcommand_Emit_EmitsSceneEventOnICFacet(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-emit-test")

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "emit A bell rings in the distance.",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "You emit: A bell rings in the distance.")

	found := findIntentByType(sink.intents, "core-scenes:scene_emit")
	require.NotNil(t, found, "scene emit MUST emit scene_emit")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-emit-test"), found.Subject)
	assert.True(t, found.Sensitive, "scene_emit MUST be Sensitive=true (sensitivity:always)")
	assert.Contains(t, found.Payload, `"text":"A bell rings in the distance."`)
}

func TestSceneSubcommand_OOC_EmitsSceneEventOnOOCFacet(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-ooc-test")

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "ooc brb getting coffee",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "You ooc: brb getting coffee")

	found := findIntentByType(sink.intents, "core-scenes:scene_ooc")
	require.NotNil(t, found, "scene ooc MUST emit scene_ooc")
	assert.Equal(t, dotStyleSceneSubjectOOC("main", "scene-ooc-test"), found.Subject,
		"scene_ooc MUST land on the .ooc facet, not .ic")
	assert.True(t, found.Sensitive, "scene_ooc MUST be Sensitive=true (sensitivity:always)")
	assert.Contains(t, found.Payload, `"text":"brb getting coffee"`)
}

func TestSceneSubcommand_NonParticipant_PermissionDenied(t *testing.T) {
	t.Parallel()
	// scene-perm-test has char-owner + char-alice as members. char-bob is
	// not a participant of any scene. Single-membership inference returns
	// "no scene" for bob, not "wrong scene". To exercise the IsParticipant
	// defense-in-depth path we need bob to be a member of a *different*
	// scene; since only one scene exists, this test asserts the broader
	// "you are not currently in any scene" path. INV-SCENE-11 is also pinned
	// by the integration test (test-driven by store path).
	p, _ := newTestPluginWithMember(t, "scene-perm-test")

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "pose tries to butt in",
		CharacterID: "char-bob",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "not currently in any scene")
}

func TestSceneSubcommand_InvitedOnly_TreatedAsNonMember(t *testing.T) {
	t.Parallel()
	// Exercises resolveSingleSceneMembership returning empty for an invited-only
	// character. The resolver filters to owner+member rows; invited-only rows are
	// excluded, so the character appears as "not in any scene" to the emit path.
	// This is the correct outcome: invitation grants join rights, not write rights
	// (spec §5.4). The test confirms the user-facing error and that no emit intent
	// is recorded — both are consequences of the resolver returning empty before
	// the evaluator check is reached.
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-invited-only", OwnerID: "char-owner",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	_, err := store.InviteParticipant(context.Background(), "scene-invited-only", "char-owner", "char-bob")
	require.NoError(t, err)
	// Sanity check the fake: bob is invited (not member); IsParticipant returns false.
	isPart, err := store.IsParticipant(context.Background(), "scene-invited-only", "char-bob")
	require.NoError(t, err)
	require.False(t, isPart, "invited-only character must not be a participant")

	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	// No evaluator needed: bob is invited-only, membership resolution returns
	// empty and the "not in any scene" error fires before the evaluator check.
	p := &scenePlugin{service: svc}

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "pose tries to barge in",
		CharacterID: "char-bob",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Single-membership inference returns empty (bob is invited, not member),
	// so the user-facing error is the "no scene" message — same outcome:
	// bob cannot emit, no intent is recorded.
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Empty(t, sink.intents, "invited-only character MUST NOT cause any emit")
}

func TestSceneSubcommand_NoScene_ActionableError(t *testing.T) {
	t.Parallel()
	// char-orphan is not a member of any scene.
	store := newFakeStore()
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	p := &scenePlugin{service: svc}

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "pose hello",
		CharacterID: "char-orphan",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "not currently in any scene")
	assert.Contains(t, resp.Output, "scene join", "should hint at how to recover")
	assert.Empty(t, sink.intents, "no-scene path MUST NOT emit")
}

func TestSceneSubcommand_MultipleScenes_AmbiguousError(t *testing.T) {
	t.Parallel()
	// char-alice is a member of two scenes; single-membership inference
	// cannot disambiguate. Phase 5 will add focus-aware routing; Phase 4
	// returns a clear error.
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-a", OwnerID: "char-owner-a",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-b", OwnerID: "char-owner-b",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-a", "char-alice")
	require.NoError(t, err)
	_, _, err = store.AddParticipant(context.Background(), "scene-b", "char-alice")
	require.NoError(t, err)

	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	p := &scenePlugin{service: svc}

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "pose hello",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Phase 5")
	assert.Empty(t, sink.intents, "ambiguous-scene path MUST NOT emit")
}

func TestSceneSubcommand_EmptyArgs_UsageHint(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-usage")

	for _, verb := range []string{"pose", "say", "emit", "ooc"} {
		t.Run(verb, func(t *testing.T) {
			resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "scene",
				Args:        verb, // no text after verb
				CharacterID: "char-alice",
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, pluginsdk.CommandError, resp.Status)
			assert.Contains(t, resp.Output, "Usage: scene "+verb)
		})
	}
	assert.Empty(t, sink.intents, "empty-args path MUST NOT emit")
}

// TestSceneSubcommand_Order_NoScene verifies the user-friendly error when
// the caller is not in any scene.
func TestSceneSubcommand_Order_NoScene(t *testing.T) {
	t.Parallel()
	p := newTestPlugin(t) // empty store — char-alice has no scenes

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "order",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "not currently in any scene")
}

// TestSceneSubcommand_Order_FreeMode verifies the dispatcher wires handleOrder
// and the free-mode renderer returns a participant list.
func TestSceneSubcommand_Order_FreeMode(t *testing.T) {
	t.Parallel()
	p, _ := newTestPluginWithMember(t, "scene-order-free")

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "order",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Participants:")
	assert.Contains(t, resp.Output, "char-alice")
}
