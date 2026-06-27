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

// TestMembershipHandlersRejectForgedActingCharacter pins the defense-in-depth
// identity cross-check on the four membership handlers (mirroring WatchScene):
// when the advisory actor metadata identifies one character but the request's
// acting character_id is a different one, the call MUST fail closed with
// PermissionDenied BEFORE ABAC is evaluated or any store mutation occurs.
//
// Without the guard a caller authorized as one participant could submit
// another character's id and, e.g., remove a different member via LeaveScene —
// the confused-deputy gap CodeRabbit flagged on PR #4531.
func TestMembershipHandlersRejectForgedActingCharacter(t *testing.T) {
	t.Parallel()

	const (
		sceneID   = "scene-actor-binding"
		actualID  = "char-actual" // authenticated actor carried in advisory metadata
		forgedID  = "char-forged" // attacker-supplied req.CharacterId (seeded as owner)
		victimID  = "char-victim" // a different member that must remain untouched
		inviteeID = "char-newbie" // invite target that must NOT be added on rejection
	)

	// forgedID is seeded as the scene owner so that the owner-only handlers
	// (invite/kick/transfer) would otherwise pass their ABAC gate — proving the
	// new actor-binding guard, not an unrelated permission failure, is what
	// rejects the forged request.
	newSvc := func(t *testing.T) (*SceneServiceImpl, *fakeStore, *recordingEvaluator) {
		t.Helper()
		store := newFakeStore()
		require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
			ID:         sceneID,
			OwnerID:    forgedID,
			State:      string(SceneStateActive),
			Visibility: string(SceneVisibilityOpen),
		}))
		store.participants[sceneID][victimID] = "member"
		ev := &recordingEvaluator{decision: pluginsdk.EvaluateDecision{Allowed: true}}
		svc := newTestService(t, store)
		svc.SetHostEvaluator(ev)
		return svc, store, ev
	}

	// Incoming context whose advisory actor identity (actualID) contradicts the
	// forged acting character in every request below.
	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, actualID)

	tests := []struct {
		name string
		call func(svc *SceneServiceImpl) error
	}{
		{"leave rejects a forged acting character", func(svc *SceneServiceImpl) error {
			_, err := svc.LeaveScene(ctx, &scenev1.LeaveSceneRequest{
				SceneId: sceneID, CharacterId: forgedID,
			})
			return err
		}},
		{"invite rejects a forged acting character", func(svc *SceneServiceImpl) error {
			_, err := svc.InviteToScene(ctx, &scenev1.InviteToSceneRequest{
				SceneId: sceneID, CharacterId: forgedID, TargetCharacterId: inviteeID,
			})
			return err
		}},
		{"kick rejects a forged acting character", func(svc *SceneServiceImpl) error {
			_, err := svc.KickFromScene(ctx, &scenev1.KickFromSceneRequest{
				SceneId: sceneID, CharacterId: forgedID, TargetCharacterId: victimID,
			})
			return err
		}},
		{"transfer rejects a forged acting character", func(svc *SceneServiceImpl) error {
			_, err := svc.TransferOwnership(ctx, &scenev1.TransferOwnershipRequest{
				SceneId: sceneID, CharacterId: forgedID, NewOwnerCharacterId: victimID,
			})
			return err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, store, ev := newSvc(t)

			err := tt.call(svc)

			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.Empty(t, ev.calls, "actor-binding guard MUST reject before consulting ABAC")

			// No handler-specific mutation may have occurred before the guard
			// returned: the shared victim check alone would miss an invite that
			// added the target, a leave that removed the acting participant, or
			// a transfer that reassigned ownership.
			assert.Equal(t, "member", store.participants[sceneID][victimID],
				"a rejected kick MUST NOT remove the victim member")
			assert.Contains(t, store.participants[sceneID], forgedID,
				"a rejected leave MUST NOT remove the acting participant")
			assert.NotContains(t, store.participants[sceneID], inviteeID,
				"a rejected invite MUST NOT add the target participant")
			scene, getErr := store.Get(context.Background(), sceneID)
			require.NoError(t, getErr)
			assert.Equal(t, forgedID, scene.OwnerID,
				"a rejected transfer MUST NOT change ownership")
		})
	}
}

// TestMembershipHandlersAllowMatchingActorMetadata is the positive companion:
// when the advisory actor metadata matches the request's acting character_id,
// the guard is transparent and the handler proceeds to its normal ABAC + store
// path (here a voluntary leave that succeeds).
func TestMembershipHandlersAllowMatchingActorMetadata(t *testing.T) {
	t.Parallel()

	const (
		sceneID  = "scene-actor-binding-ok"
		ownerID  = "char-owner"
		memberID = "char-member"
	)

	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         sceneID,
		OwnerID:    ownerID,
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}))
	store.participants[sceneID][memberID] = "member"
	svc := newTestService(t, store)
	svc.SetHostEvaluator(allowEvaluator{})

	// Advisory metadata identifies the same character that is acting.
	ctx := watchCtxWithActorMetadata(pluginsdk.ActorCharacter, memberID)
	_, err := svc.LeaveScene(ctx, &scenev1.LeaveSceneRequest{
		SceneId: sceneID, CharacterId: memberID,
	})

	require.NoError(t, err)
	assert.NotContains(t, store.participants[sceneID], memberID,
		"a matching-actor leave proceeds and removes the member")
}
