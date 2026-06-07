// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// recordingEvaluator is a test HostEvaluator that records every Evaluate
// call so tests can assert whether (and with what) ABAC was consulted.
type recordingEvaluator struct {
	decision pluginsdk.EvaluateDecision
	err      error
	calls    []evaluateCall
}

type evaluateCall struct {
	action   string
	resource string
}

func (r *recordingEvaluator) Evaluate(_ context.Context, action, resource string) (pluginsdk.EvaluateDecision, error) {
	r.calls = append(r.calls, evaluateCall{action: action, resource: resource})
	return r.decision, r.err
}

// installWatchableScene seeds an open, active scene into the fake store and
// returns its ID.
func installWatchableScene(t *testing.T, store *fakeStore, visibility, state string) string {
	t.Helper()
	const id = "01WATCHSCENETESTSCENE00000"
	store.scenes[id] = &SceneRow{
		ID:         id,
		Title:      "Watchable",
		OwnerID:    "char-owner",
		State:      state,
		Visibility: visibility,
	}
	return id
}

// newWatchFixture wires a SceneServiceImpl with the given evaluator and a
// fakeFocusClient, backed by a fresh fakeStore.
func newWatchFixture(t *testing.T, ev pluginsdk.HostEvaluator) (*SceneServiceImpl, *fakeStore, *fakeFocusClient) {
	t.Helper()
	store := newFakeStore()
	svc := newTestService(t, store)
	fc := &fakeFocusClient{}
	svc.SetHostEvaluator(ev)
	svc.SetFocusClient(fc)
	return svc, store, fc
}

// Verifies: INV-SCENE-61
func TestWatchSceneRejectsNonOpenSceneBeforeConsultingABAC(t *testing.T) {
	// The evaluator returns an engine error: if WatchScene consulted ABAC
	// before the code gate, the error would surface as Internal instead of
	// SCENE_NOT_WATCHABLE — and the call counter would be non-zero.
	ev := &recordingEvaluator{err: errors.New("ABAC MUST NOT BE REACHED")}
	svc, store, _ := newWatchFixture(t, ev)
	sceneID := installWatchableScene(t, store, string(SceneVisibilityPrivate), string(SceneStateActive))

	resp, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_NOT_WATCHABLE", status.Convert(err).Message())
	assert.Empty(t, ev.calls, "ABAC evaluator must not be consulted for a non-open scene")
}

// Verifies: INV-SCENE-61
func TestWatchSceneRejectsEndedSceneBeforeConsultingABAC(t *testing.T) {
	ev := &recordingEvaluator{err: errors.New("ABAC MUST NOT BE REACHED")}
	svc, store, _ := newWatchFixture(t, ev)
	sceneID := installWatchableScene(t, store, string(SceneVisibilityOpen), string(SceneStateEnded))

	_, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_NOT_WATCHABLE", status.Convert(err).Message())
	assert.Empty(t, ev.calls, "ABAC evaluator must not be consulted for an ended scene")
}

func TestWatchSceneDeniedWhenSpectatePolicyDenies(t *testing.T) {
	svc, store, fc := newWatchFixture(t, denyEvaluator{})
	sceneID := installWatchableScene(t, store, string(SceneVisibilityOpen), string(SceneStateActive))

	resp, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Empty(t, store.participants[sceneID], "no observer row may be written on denial")
	assert.Empty(t, fc.joinCalls, "focus must not be joined on denial")
}

func TestWatchSceneFailsClosedWhenEvaluatorNotConfigured(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetFocusClient(&fakeFocusClient{})
	sceneID := installWatchableScene(t, store, string(SceneVisibilityOpen), string(SceneStateActive))

	_, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.Empty(t, store.participants[sceneID], "no observer row may be written when the evaluator is missing")
}

func TestWatchSceneAddsObserverAndJoinsFocusWhenPermitted(t *testing.T) {
	ev := &recordingEvaluator{decision: pluginsdk.EvaluateDecision{Allowed: true}}
	svc, store, fc := newWatchFixture(t, ev)
	sceneID := installWatchableScene(t, store, string(SceneVisibilityOpen), string(SceneStateActive))

	resp, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.NoError(t, err)
	require.NotNil(t, resp.GetParticipant())
	assert.Equal(t, "char-watcher", resp.GetParticipant().GetCharacterId())
	assert.Equal(t, "observer", resp.GetParticipant().GetRole())
	assert.Equal(t, "observer", store.participants[sceneID]["char-watcher"])

	require.Len(t, ev.calls, 1)
	assert.Equal(t, evaluateCall{action: "spectate", resource: "scene:" + sceneID}, ev.calls[0])

	require.Len(t, fc.joinCalls, 1)
	assert.Equal(t, focusCall{
		sessionID: "sess-1",
		target:    pluginsdk.FocusKey{Kind: pluginsdk.FocusKindScene, TargetID: sceneID},
	}, fc.joinCalls[0])
}

func TestWatchSceneReturnsExistingRowWhenAlreadyParticipant(t *testing.T) {
	svc, store, fc := newWatchFixture(t, allowEvaluator{})
	sceneID := installWatchableScene(t, store, string(SceneVisibilityOpen), string(SceneStateActive))
	store.participants[sceneID] = map[string]string{"char-member": "member"}

	resp, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-member",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.NoError(t, err)
	assert.Equal(t, "member", resp.GetParticipant().GetRole(), "pre-existing row is returned unchanged")
	assert.Equal(t, "member", store.participants[sceneID]["char-member"], "role must not be downgraded")
	require.Len(t, fc.joinCalls, 1, "focus join is idempotent and still requested")
}

func TestWatchSceneReturnsNotFoundWhenSceneMissing(t *testing.T) {
	svc, _, fc := newWatchFixture(t, allowEvaluator{})

	_, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     "01MISSINGSCENE000000000000",
		SessionId:   "sess-1",
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, fc.joinCalls)
}

func TestWatchSceneFailsWhenFocusJoinFails(t *testing.T) {
	svc, store, fc := newWatchFixture(t, allowEvaluator{})
	fc.joinErr = errors.New("focus backend down")
	sceneID := installWatchableScene(t, store, string(SceneVisibilityOpen), string(SceneStateActive))

	_, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, status.Convert(err).Message(), "focus backend down",
		"inner error must not leak past the trust boundary")
}

func TestWatchSceneFailsClosedWhenFocusClientNotConfigured(t *testing.T) {
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetHostEvaluator(allowEvaluator{})
	sceneID := installWatchableScene(t, store, string(SceneVisibilityOpen), string(SceneStateActive))

	_, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestWatchSceneTreatesFocusAlreadyMemberAsSuccess(t *testing.T) {
	svc, store, fc := newWatchFixture(t, allowEvaluator{})
	// Simulate a focus backend that returns FOCUS_ALREADY_MEMBER — this happens
	// when the session already holds the focus membership (e.g. joined via
	// `scene join`, or a retry/page-reload). WatchScene must treat it as
	// idempotent success and return the participant row.
	fc.joinErr = oops.Code("FOCUS_ALREADY_MEMBER").Errorf("already a member")
	sceneID := installWatchableScene(t, store, string(SceneVisibilityOpen), string(SceneStateActive))

	resp, err := svc.WatchScene(context.Background(), &scenev1.WatchSceneRequest{
		CharacterId: "char-watcher",
		SceneId:     sceneID,
		SessionId:   "sess-1",
	})

	require.NoError(t, err, "FOCUS_ALREADY_MEMBER must be treated as success")
	require.NotNil(t, resp.GetParticipant())
	assert.Equal(t, "char-watcher", resp.GetParticipant().GetCharacterId())
	assert.Equal(t, "observer", resp.GetParticipant().GetRole())
	// The join attempt was still made.
	require.Len(t, fc.joinCalls, 1)
}
