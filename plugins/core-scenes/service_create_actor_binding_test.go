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

// TestCreateSceneRejectsForgedActingCharacter pins the actor-binding guard on
// CreateScene. CreateScene stamps req.CharacterId directly onto the new scene's
// OwnerID, so — unlike the scene_id-keyed lifecycle handlers whose guard is
// defense-in-depth — this is the strong confused-deputy case: a forged
// character_id makes character B the owner of a scene created from character
// A's session. The guard MUST fail closed with PermissionDenied before the
// store persists the row, mirroring JoinScene.
func TestCreateSceneRejectsForgedActingCharacter(t *testing.T) {
	t.Parallel()

	const (
		actualID = "char-actual" // authenticated actor carried in advisory metadata
		forgedID = "char-forged" // attacker-supplied req.CharacterId → would-be OwnerID
	)

	store := newFakeStore()
	sink := &recordingEventSink{}
	svc := newTestService(t, store)
	svc.SetEventSink(sink)

	// Advisory actor identity (actualID) contradicts the forged acting
	// character (forgedID) that the request tries to stamp as owner.
	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, actualID)

	_, err := svc.CreateScene(ctx, &scenev1.CreateSceneRequest{
		CharacterId: forgedID,
		Title:       "Forged Owner Scene",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.PermissionDenied, st.Code(),
		"a forged acting character MUST be rejected with PermissionDenied")

	assert.Empty(t, store.scenes,
		"a rejected create MUST NOT persist a scene with the forged owner")
	assert.Empty(t, sink.intents,
		"a rejected create MUST NOT emit a scene-created event")
}

// TestCreateSceneActorBindingGuardPassThrough pins the guard's transparency:
// the cross-check fires ONLY when character-kind advisory metadata is present
// AND disagrees with req.CharacterId. A matching actor, absent metadata, and a
// non-character actor kind MUST all proceed to the normal create path —
// otherwise a verbatim-copied guard with an inverted or dropped condition would
// silently reject legitimate host- or plugin-originated calls. Mirrors the
// JoinScene pass-through tests.
func TestCreateSceneActorBindingGuardPassThrough(t *testing.T) {
	t.Parallel()

	const actorID = "char-actor"

	newSvc := func(t *testing.T) (*SceneServiceImpl, *fakeStore) {
		t.Helper()
		store := newFakeStore()
		svc := newTestService(t, store)
		svc.SetEventSink(&recordingEventSink{})
		return svc, store
	}

	assertOwnedByActor := func(t *testing.T, store *fakeStore, err error) {
		t.Helper()
		require.NoError(t, err)
		require.Len(t, store.scenes, 1, "exactly one scene MUST be persisted")
		for _, row := range store.scenes {
			assert.Equal(t, actorID, row.OwnerID,
				"the created scene's owner MUST be the acting character")
		}
	}

	t.Run("matching actor metadata proceeds and owns the scene", func(t *testing.T) {
		t.Parallel()
		svc, store := newSvc(t)
		ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, actorID)
		_, err := svc.CreateScene(ctx, &scenev1.CreateSceneRequest{
			CharacterId: actorID, Title: "Owned Scene",
		})
		assertOwnedByActor(t, store, err)
	})

	t.Run("absent advisory metadata proceeds — the dispatch token is the gate", func(t *testing.T) {
		t.Parallel()
		svc, store := newSvc(t)
		_, err := svc.CreateScene(context.Background(), &scenev1.CreateSceneRequest{
			CharacterId: actorID, Title: "Owned Scene",
		})
		assertOwnedByActor(t, store, err)
	})

	t.Run("non-character advisory actor kind proceeds", func(t *testing.T) {
		t.Parallel()
		svc, store := newSvc(t)
		// A plugin-kind actor whose id differs from req.CharacterId: the guard
		// keys on ActorCharacter, so a non-character kind MUST pass through.
		ctx := watchCtxWithActorMetadata(pluginsdk.ActorPlugin, "plugin-actor")
		_, err := svc.CreateScene(ctx, &scenev1.CreateSceneRequest{
			CharacterId: actorID, Title: "Owned Scene",
		})
		assertOwnedByActor(t, store, err)
	})
}
