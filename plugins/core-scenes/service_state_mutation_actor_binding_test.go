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
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	scenev1 "github.com/holomush/holomush/pkg/proto/holomush/scene/v1"
)

// TestStateMutatingHandlersRejectForgedActingCharacter extends the
// defense-in-depth identity cross-check (mirroring WatchScene and the four
// membership handlers, holomush-fgly4) to the remaining state-mutating scene
// handlers: when the advisory actor metadata identifies one character but the
// request's acting character_id is a different one, the call MUST fail closed
// with PermissionDenied before any store mutation occurs.
//
// JoinScene is the strong case: a forged character_id keys AddParticipant on the
// wrong character, injecting an arbitrary participant. The lifecycle/update
// handlers key their mutation on scene_id (the store records the scene owner as
// the ops-event actor), so their guard is defense-in-depth and family-wide
// consistency — plus keeping a forged character_id off UpdateScene's emitted IC
// notice and out of every handler's span/log subject. The same guard covers all
// five.
func TestStateMutatingHandlersRejectForgedActingCharacter(t *testing.T) {
	t.Parallel()

	const (
		sceneID  = "scene-state-actor-binding"
		actualID = "char-actual" // authenticated actor carried in advisory metadata
		forgedID = "char-forged" // attacker-supplied req.CharacterId
		ownerID  = "char-owner"
	)

	// Advisory actor identity (actualID) contradicts the forged acting character
	// (forgedID) in every request below.
	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, actualID)

	// newSvc seeds a fresh open scene in the requested state. The evaluator
	// allows everything, so any rejection is the actor-binding guard's doing —
	// not an ABAC denial or a store precondition failure.
	newSvc := func(t *testing.T, state string) (*SceneServiceImpl, *fakeStore, *recordingEvaluator) {
		t.Helper()
		store := newFakeStore()
		require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
			ID:         sceneID,
			Title:      "original-title",
			OwnerID:    ownerID,
			State:      state,
			Visibility: string(SceneVisibilityOpen),
		}))
		ev := &recordingEvaluator{decision: pluginsdk.EvaluateDecision{Allowed: true}}
		svc := newTestService(t, store)
		svc.SetHostEvaluator(ev)
		return svc, store, ev
	}

	tests := []struct {
		name string
		// state is the scene's initial lifecycle state.
		state string
		// consultsABAC is true for handlers that call s.evaluator.Evaluate
		// inline (end/pause/resume/update). JoinScene is ABAC-gated at the
		// dispatch layer and never touches the service-layer evaluator, so
		// asserting ev.calls is empty would be vacuously true for it — the
		// per-handler verify is what proves the mutation was blocked there.
		consultsABAC bool
		call         func(svc *SceneServiceImpl) error
		verify       func(t *testing.T, store *fakeStore)
	}{
		{
			name:         "join rejects a forged acting character",
			state:        string(SceneStateActive),
			consultsABAC: false,
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.JoinScene(ctx, &scenev1.JoinSceneRequest{
					SceneId: sceneID, CharacterId: forgedID,
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				assert.NotContains(t, store.participants[sceneID], forgedID,
					"a rejected join MUST NOT add the forged character as a participant")
			},
		},
		{
			name:         "end rejects a forged acting character",
			state:        string(SceneStateActive),
			consultsABAC: true,
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.EndScene(ctx, &scenev1.EndSceneRequest{
					SceneId: sceneID, CharacterId: forgedID,
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				scene, err := store.Get(context.Background(), sceneID)
				require.NoError(t, err)
				assert.Equal(t, string(SceneStateActive), scene.State,
					"a rejected end MUST NOT transition the scene out of active")
			},
		},
		{
			name:         "pause rejects a forged acting character",
			state:        string(SceneStateActive),
			consultsABAC: true,
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.PauseScene(ctx, &scenev1.PauseSceneRequest{
					SceneId: sceneID, CharacterId: forgedID,
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				scene, err := store.Get(context.Background(), sceneID)
				require.NoError(t, err)
				assert.Equal(t, string(SceneStateActive), scene.State,
					"a rejected pause MUST NOT transition the scene to paused")
			},
		},
		{
			name:         "resume rejects a forged acting character",
			state:        string(SceneStatePaused),
			consultsABAC: true,
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.ResumeScene(ctx, &scenev1.ResumeSceneRequest{
					SceneId: sceneID, CharacterId: forgedID,
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				scene, err := store.Get(context.Background(), sceneID)
				require.NoError(t, err)
				assert.Equal(t, string(SceneStatePaused), scene.State,
					"a rejected resume MUST NOT transition the scene to active")
			},
		},
		{
			name:         "update rejects a forged acting character",
			state:        string(SceneStateActive),
			consultsABAC: true,
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.UpdateScene(ctx, &scenev1.UpdateSceneRequest{
					SceneId:     sceneID,
					CharacterId: forgedID,
					Title:       "hijacked-title",
					UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				scene, err := store.Get(context.Background(), sceneID)
				require.NoError(t, err)
				assert.Equal(t, "original-title", scene.Title,
					"a rejected update MUST NOT mutate scene metadata")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store, ev := newSvc(t, tt.state)

			err := tt.call(svc)

			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			if tt.consultsABAC {
				assert.Empty(t, ev.calls, "actor-binding guard MUST reject before consulting ABAC")
			}
			tt.verify(t, store)
		})
	}
}

// TestStateMutatingHandlersAllowMatchingActorMetadata is the positive companion:
// when the advisory actor metadata matches the request's acting character_id,
// the guard is transparent and each handler proceeds to its normal path,
// completing the intended state mutation.
func TestStateMutatingHandlersAllowMatchingActorMetadata(t *testing.T) {
	t.Parallel()

	const (
		sceneID = "scene-state-actor-binding-ok"
		actorID = "char-actor"
		ownerID = "char-owner"
	)

	// Advisory metadata identifies the same character that is acting.
	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, actorID)

	newSvc := func(t *testing.T, state string) (*SceneServiceImpl, *fakeStore) {
		t.Helper()
		store := newFakeStore()
		require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
			ID:         sceneID,
			Title:      "original-title",
			OwnerID:    ownerID,
			State:      state,
			Visibility: string(SceneVisibilityOpen),
		}))
		svc := newTestService(t, store)
		svc.SetHostEvaluator(allowEvaluator{})
		return svc, store
	}

	tests := []struct {
		name   string
		state  string
		call   func(svc *SceneServiceImpl) error
		verify func(t *testing.T, store *fakeStore)
	}{
		{
			name:  "join proceeds when actor metadata matches",
			state: string(SceneStateActive),
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.JoinScene(ctx, &scenev1.JoinSceneRequest{
					SceneId: sceneID, CharacterId: actorID,
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				assert.Contains(t, store.participants[sceneID], actorID,
					"a matching-actor join proceeds and adds the participant")
			},
		},
		{
			name:  "end proceeds when actor metadata matches",
			state: string(SceneStateActive),
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.EndScene(ctx, &scenev1.EndSceneRequest{
					SceneId: sceneID, CharacterId: actorID,
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				scene, err := store.Get(context.Background(), sceneID)
				require.NoError(t, err)
				assert.Equal(t, string(SceneStateEnded), scene.State,
					"a matching-actor end proceeds and ends the scene")
			},
		},
		{
			name:  "pause proceeds when actor metadata matches",
			state: string(SceneStateActive),
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.PauseScene(ctx, &scenev1.PauseSceneRequest{
					SceneId: sceneID, CharacterId: actorID,
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				scene, err := store.Get(context.Background(), sceneID)
				require.NoError(t, err)
				assert.Equal(t, string(SceneStatePaused), scene.State,
					"a matching-actor pause proceeds and pauses the scene")
			},
		},
		{
			name:  "resume proceeds when actor metadata matches",
			state: string(SceneStatePaused),
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.ResumeScene(ctx, &scenev1.ResumeSceneRequest{
					SceneId: sceneID, CharacterId: actorID,
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				scene, err := store.Get(context.Background(), sceneID)
				require.NoError(t, err)
				assert.Equal(t, string(SceneStateActive), scene.State,
					"a matching-actor resume proceeds and reactivates the scene")
			},
		},
		{
			name:  "update proceeds when actor metadata matches",
			state: string(SceneStateActive),
			call: func(svc *SceneServiceImpl) error {
				_, err := svc.UpdateScene(ctx, &scenev1.UpdateSceneRequest{
					SceneId:     sceneID,
					CharacterId: actorID,
					Title:       "renamed-title",
					UpdateMask:  &fieldmaskpb.FieldMask{Paths: []string{"title"}},
				})
				return err
			},
			verify: func(t *testing.T, store *fakeStore) {
				scene, err := store.Get(context.Background(), sceneID)
				require.NoError(t, err)
				assert.Equal(t, "renamed-title", scene.Title,
					"a matching-actor update proceeds and applies the change")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store := newSvc(t, tt.state)

			err := tt.call(svc)

			require.NoError(t, err)
			tt.verify(t, store)
		})
	}
}

// TestJoinSceneActorBindingGuardPassesThroughNonBindingMetadata pins the guard's
// two pass-through preconditions for a representative handler (JoinScene): the
// cross-check fires ONLY when character-kind advisory metadata is present AND
// disagrees with req.CharacterId. Absent metadata and non-character-kind
// metadata MUST proceed to the normal store path — otherwise a verbatim-copied
// guard with an inverted or dropped condition would silently reject legitimate
// host- or plugin-originated calls. Mirrors the WatchScene pass-through tests.
func TestJoinSceneActorBindingGuardPassesThroughNonBindingMetadata(t *testing.T) {
	t.Parallel()

	const (
		sceneID  = "scene-join-passthrough"
		ownerID  = "char-owner"
		joinerID = "char-joiner"
	)

	newSvc := func(t *testing.T) (*SceneServiceImpl, *fakeStore) {
		t.Helper()
		store := newFakeStore()
		require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
			ID:         sceneID,
			Title:      "passthrough",
			OwnerID:    ownerID,
			State:      string(SceneStateActive),
			Visibility: string(SceneVisibilityOpen),
		}))
		svc := newTestService(t, store)
		svc.SetHostEvaluator(allowEvaluator{})
		return svc, store
	}

	t.Run("proceeds when no advisory actor metadata is present", func(t *testing.T) {
		svc, store := newSvc(t)

		_, err := svc.JoinScene(context.Background(), &scenev1.JoinSceneRequest{
			SceneId: sceneID, CharacterId: joinerID,
		})

		require.NoError(t, err)
		assert.Contains(t, store.participants[sceneID], joinerID,
			"absent actor metadata MUST NOT trip the guard — the dispatch token is the gate")
	})

	t.Run("proceeds when advisory actor is a non-character kind", func(t *testing.T) {
		svc, store := newSvc(t)

		// A plugin-kind actor whose id differs from req.CharacterId: the guard
		// keys on ActorCharacter, so a non-character kind MUST pass through even
		// though the ids do not match.
		ctx := watchCtxWithActorMetadata(pluginsdk.ActorPlugin, "plugin-actor")
		_, err := svc.JoinScene(ctx, &scenev1.JoinSceneRequest{
			SceneId: sceneID, CharacterId: joinerID,
		})

		require.NoError(t, err)
		assert.Contains(t, store.participants[sceneID], joinerID,
			"non-character advisory actor kind MUST NOT trip the character-binding guard")
	})
}
