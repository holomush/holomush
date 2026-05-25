// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

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
