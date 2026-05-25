// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"os"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// recordingPublishEventer is a publishEventer that records emitAttemptsExtended
// calls (the only method the E1 tests assert) and no-ops the rest via the
// embedded noopPublishEventer.
type recordingPublishEventer struct {
	noopPublishEventer
	extended []extendedCall
}

type extendedCall struct {
	sceneID, adminID   string
	additional, newMax int
}

func (r *recordingPublishEventer) emitAttemptsExtended(_ context.Context, sceneID, adminID string, additional, newMax int) error {
	r.extended = append(r.extended, extendedCall{sceneID, adminID, additional, newMax})
	return nil
}

// startPublishFixture wires a SceneServiceImpl over a fakeStore seeded with a
// single scene in the given state, returning the store, service, and the
// scene/caller ULIDs the tests assert against.
func startPublishFixture(t *testing.T, state SceneState) (*fakeStore, *SceneServiceImpl, string, string) {
	t.Helper()
	sceneID := ulid.Make().String()
	callerID := ulid.Make().String()
	store := newFakeStore()
	store.scenes[sceneID] = &SceneRow{
		ID:      sceneID,
		OwnerID: callerID,
		State:   string(state),
	}
	svc := NewSceneServiceImpl(store)
	return store, svc, sceneID, callerID
}

func TestStartScenePublishCreatesAttemptForEndedSceneWithinBudget(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateEnded)
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{Total: 0, Active: 0, Published: 0}

	resp, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, resp.GetPublishedSceneId())
	assert.Equal(t, int32(1), resp.GetAttemptNumber())
	require.Len(t, store.createdAttempts, 1)
	assert.Equal(t, callerID, store.createdAttempts[0].InitiatedBy)
}

func TestStartScenePublishRejectsSceneNotInEndedState(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateActive)
	store.maxPublishAttempts[sceneID] = 3

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_INVALID_STATE", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

func TestStartScenePublishRejectsSceneWithExistingPublishedArchive(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateEnded)
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{Total: 1, Active: 0, Published: 1}

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_ALREADY_PUBLISHED", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

func TestStartScenePublishRejectsSceneWithActiveAttempt(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateEnded)
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{Total: 1, Active: 1, Published: 0}

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_ALREADY_ACTIVE", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

func TestStartScenePublishRejectsSceneThatExhaustedItsAttemptBudget(t *testing.T) {
	t.Parallel()
	store, svc, sceneID, callerID := startPublishFixture(t, SceneStateEnded)
	store.maxPublishAttempts[sceneID] = 3
	store.attemptCounts[sceneID] = AttemptCounts{Total: 3, Active: 0, Published: 0}

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           sceneID,
		CallerCharacterId: callerID,
	})

	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, "SCENE_PUBLISH_ATTEMPTS_EXHAUSTED", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

func TestStartScenePublishReturnsNotFoundForMissingScene(t *testing.T) {
	t.Parallel()
	store := newFakeStore() // no scene installed → store.Get returns SCENE_NOT_FOUND
	svc := NewSceneServiceImpl(store)

	_, err := svc.StartScenePublish(context.Background(), &scenev1.StartScenePublishRequest{
		SceneId:           ulid.Make().String(),
		CallerCharacterId: ulid.Make().String(),
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), "a missing scene MUST map to NotFound, not Internal")
	assert.Equal(t, "SCENE_NOT_FOUND", status.Convert(err).Message())
	assert.Empty(t, store.createdAttempts)
}

// TestExtendScenePublishVoteAttemptsBumpsBudgetAndEmits covers the E1 happy
// path: the budget is bumped by `additional`, the new max is returned, and a
// scene_publish_vote_attempts_extended event fires carrying the admin id. The
// handler performs NO in-plugin admin check — admin is ABAC-gated by the host
// (spec §8), so this test does not (and cannot) assert a non-admin denial at
// the handler level; that is the host policy's job (verified by the policy
// declaration test below + at E2/integration).
func TestExtendScenePublishVoteAttemptsBumpsBudgetAndEmits(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.maxPublishAttempts["scene-e1"] = 3
	rec := &recordingPublishEventer{}
	svc := NewSceneServiceImpl(store)
	svc.SetPublishEventer(rec)

	resp, err := svc.ExtendScenePublishVoteAttempts(context.Background(), &scenev1.ExtendScenePublishVoteAttemptsRequest{
		CallerCharacterId: "char-admin",
		SceneId:           "scene-e1",
		Additional:        2,
	})

	require.NoError(t, err)
	assert.Equal(t, int32(5), resp.GetNewMax(), "3 + 2 = 5")
	require.Len(t, rec.extended, 1, "exactly one scene_publish_vote_attempts_extended event must fire")
	assert.Equal(t, extendedCall{sceneID: "scene-e1", adminID: "char-admin", additional: 2, newMax: 5}, rec.extended[0])
}

// TestExtendScenePublishVoteAttemptsRejectsNonPositiveCount verifies a zero or
// negative extension is rejected with InvalidArgument before any write — it
// would otherwise no-op or decrement the budget. No event fires.
func TestExtendScenePublishVoteAttemptsRejectsNonPositiveCount(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.maxPublishAttempts["scene-e1"] = 3
	rec := &recordingPublishEventer{}
	svc := NewSceneServiceImpl(store)
	svc.SetPublishEventer(rec)

	for _, additional := range []int32{0, -5} {
		_, err := svc.ExtendScenePublishVoteAttempts(context.Background(), &scenev1.ExtendScenePublishVoteAttemptsRequest{
			CallerCharacterId: "char-admin",
			SceneId:           "scene-e1",
			Additional:        additional,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "non-positive additional must be InvalidArgument")
	}
	assert.Empty(t, rec.extended, "no event fires on a rejected extension")
	assert.Equal(t, 3, store.maxPublishAttempts["scene-e1"], "budget unchanged on rejection")
}

// TestExtendScenePublishVoteAttemptsPropagatesStoreNotFound verifies a missing
// scene surfaces as NotFound (not Internal) and no event fires.
func TestExtendScenePublishVoteAttemptsPropagatesStoreNotFound(t *testing.T) {
	t.Parallel()
	store := &notFoundExtendStore{fakeStore: newFakeStore()}
	rec := &recordingPublishEventer{}
	svc := NewSceneServiceImpl(store)
	svc.SetPublishEventer(rec)

	_, err := svc.ExtendScenePublishVoteAttempts(context.Background(), &scenev1.ExtendScenePublishVoteAttemptsRequest{
		CallerCharacterId: "char-admin",
		SceneId:           "missing",
		Additional:        1,
	})

	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
	assert.Empty(t, rec.extended, "no event fires when the bump fails")
}

// notFoundExtendStore overrides ExtendMaxPublishAttempts to return the store's
// not-found code, exercising the handler's error mapping.
type notFoundExtendStore struct {
	*fakeStore
}

func (s *notFoundExtendStore) ExtendMaxPublishAttempts(_ context.Context, sceneID string, _ int) (int, error) {
	return 0, oops.Code("SCENE_PUBLISH_NOT_FOUND").With("scene_id", sceneID).Errorf("scene not found")
}

// TestPluginManifestDeclaresAdminExtendPublishAttemptsPolicy pins the ABAC
// policy that gates the RPC: admin-only via "admin" in principal.character.roles
// on the extend_publish_attempts action (spec §8). Enforcement is host-side at
// command dispatch (E2); this asserts the declaration is correct and present.
func TestPluginManifestDeclaresAdminExtendPublishAttemptsPolicy(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)
	manifest := string(data)

	assert.Contains(t, manifest, "admin-extend-publish-attempts", "the E1 ABAC policy must be declared")
	assert.Contains(t, manifest, `action in ["extend_publish_attempts"]`, "policy must gate the extend_publish_attempts action")
	assert.Contains(t, manifest, "resource is scene", "policy must target scene resources")
	assert.Contains(t, manifest, `"admin" in principal.character.roles`, "policy must be admin-role-only")
}
