// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
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

func TestSceneSubcommand_Pose_AuthorCharacterNameInPayload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		scene         string
		characterName string
		assertPayload func(t *testing.T, payload string)
	}{
		{
			name:          "includes character_name when the dispatcher provides one",
			scene:         "scene-author-test",
			characterName: "Alice",
			assertPayload: func(t *testing.T, payload string) {
				assert.Contains(t, payload, `"character_name":"Alice"`)
			},
		},
		{
			name:          "omits character_name when the dispatcher provides none",
			scene:         "scene-noauthor-test",
			characterName: "",
			assertPayload: func(t *testing.T, payload string) {
				assert.NotContains(t, payload, "character_name")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, sink := newTestPluginWithMember(t, tt.scene)

			resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
				Command:       "scene",
				Args:          "pose smiles",
				CharacterID:   "char-alice",
				CharacterName: tt.characterName,
			})
			require.NoError(t, err)
			assert.Equal(t, pluginsdk.CommandOK, resp.Status)

			found := findIntentByType(sink.intents, "core-scenes:scene_pose")
			require.NotNil(t, found)
			tt.assertPayload(t, found.Payload)
		})
	}
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
	// cannot disambiguate, and no connection focus is set, so the emit
	// returns a clear ambiguity error.
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
	assert.Contains(t, resp.Output, "2 scenes")
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

// --- Focus-aware emit routing tests ---

// TestHandleEmit_FocusedConnectionRoutesToFocusedScene verifies that when a
// connection carries an explicit scene focus, the emit is routed to that scene
// rather than falling back to single-membership inference (focus-aware routing,
// Task 7 of the web-portal-scenes plan).
func TestHandleEmit_FocusedConnectionRoutesToFocusedScene(t *testing.T) {
	t.Parallel()
	// char-alice is a member of both scene-a and scene-b; without focus the
	// single-membership fallback would return the ambiguous-scene error.
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-a", OwnerID: "char-owner",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-b", OwnerID: "char-owner",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-a", "char-alice")
	require.NoError(t, err)
	_, _, err = store.AddParticipant(context.Background(), "scene-b", "char-alice")
	require.NoError(t, err)

	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	fc := &fakeFocusClient{
		getConnFocusResult: &pluginsdk.FocusKey{
			Kind:     pluginsdk.FocusKindScene,
			TargetID: "scene-b",
		},
	}
	p := &scenePlugin{service: svc, focusClient: fc, evaluator: allowEvaluator{}}

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "pose hello from scene b",
		CharacterID:  "char-alice",
		ConnectionID: "conn-focused",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)

	found := findIntentByType(sink.intents, "core-scenes:scene_pose")
	require.NotNil(t, found, "focused connection MUST emit scene_pose")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-b"), found.Subject,
		"emit MUST land on the focused scene, not scene-a")
}

// TestHandleEmit_UnfocusedConnectionFallsBackToSingleMembership verifies that
// when a connection has no focus, the single-membership inference fallback
// still resolves correctly for a character in exactly one scene.
func TestHandleEmit_UnfocusedConnectionFallsBackToSingleMembership(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-fallback")
	fc := &fakeFocusClient{getConnFocusResult: nil} // nil = no focus / grid
	p.focusClient = fc

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "pose waves to the room",
		CharacterID:  "char-alice",
		ConnectionID: "conn-unfocused",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)

	found := findIntentByType(sink.intents, "core-scenes:scene_pose")
	require.NotNil(t, found, "unfocused single-member MUST still emit scene_pose via fallback")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-fallback"), found.Subject)
}

// TestHandleEmit_UnfocusedTwoMembershipsPreservesAmbiguityError verifies that
// when no focus is set and a character is in multiple scenes, the existing
// ambiguity error is preserved unchanged. The focus-routing change MUST NOT
// alter the fallback error path.
func TestHandleEmit_UnfocusedTwoMembershipsPreservesAmbiguityError(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-c", OwnerID: "char-owner",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-d", OwnerID: "char-owner",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-c", "char-alice")
	require.NoError(t, err)
	_, _, err = store.AddParticipant(context.Background(), "scene-d", "char-alice")
	require.NoError(t, err)

	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)
	fc := &fakeFocusClient{getConnFocusResult: nil} // nil = no focus
	p := &scenePlugin{service: svc, focusClient: fc}

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "pose hello",
		CharacterID:  "char-alice",
		ConnectionID: "conn-unfocused",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "2 scenes",
		"ambiguous-scene error MUST still fire when no focus is set")
	assert.Empty(t, sink.intents, "ambiguous-scene path MUST NOT emit")
}

// TestHandleEmit_FocusLookupErrorDegradesToMembershipFallback verifies a
// focus-service blip (GetConnectionFocus returns an error) does NOT break
// posing: the emit degrades to single-membership inference exactly as if the
// connection had no focus. A focus outage must never strand a poser.
func TestHandleEmit_FocusLookupErrorDegradesToMembershipFallback(t *testing.T) {
	t.Parallel()
	p, sink := newTestPluginWithMember(t, "scene-blip")
	p.focusClient = &fakeFocusClient{getConnFocusErr: errors.New("focus coordinator unreachable")}

	resp, err := p.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "pose carries on",
		CharacterID:  "char-alice",
		ConnectionID: "conn-blip",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status,
		"a focus-lookup error MUST degrade to the membership fallback, not fail the pose")

	found := findIntentByType(sink.intents, "core-scenes:scene_pose")
	require.NotNil(t, found, "fallback MUST still emit scene_pose despite the focus blip")
	assert.Equal(t, dotStyleSceneSubjectIC("main", "scene-blip"), found.Subject)
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
