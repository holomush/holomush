// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

func TestSceneResolverGetSchemaReturnsSceneAttributes(t *testing.T) {
	r := NewSceneResolver(newFakeStore())

	resp, err := r.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp.GetResourceTypes())
	sceneSchema, ok := resp.GetResourceTypes()["scene"]
	require.True(t, ok, "schema must include 'scene' resource type")
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, sceneSchema.GetAttributes()["owner"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, sceneSchema.GetAttributes()["state"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING, sceneSchema.GetAttributes()["visibility"])
}

func TestSceneResolverResolveResourceReturnsSceneAttributes(t *testing.T) {
	store := newFakeStore()
	store.scenes["scene-01"] = &SceneRow{
		ID:         "scene-01",
		OwnerID:    "char-alice",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}
	r := NewSceneResolver(store)

	resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "scene",
		ResourceId:   "scene-01",
	})
	require.NoError(t, err)
	attrs := resp.GetAttributes()
	require.NotNil(t, attrs["owner"])
	assert.Equal(t, "char-alice", attrs["owner"].GetStringValue())
	require.NotNil(t, attrs["state"])
	assert.Equal(t, "active", attrs["state"].GetStringValue())
	require.NotNil(t, attrs["visibility"])
	assert.Equal(t, "open", attrs["visibility"].GetStringValue())
}

func TestSceneResolverResolveResourceRejectsForeignResourceType(t *testing.T) {
	r := NewSceneResolver(newFakeStore())

	_, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "widget",
		ResourceId:   "widget-1",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.True(t, strings.Contains(st.Message(), "scene"), "error should mention 'scene'")
}

func TestSceneResolverResolveResourceReturnsNotFoundForMissingScene(t *testing.T) {
	r := NewSceneResolver(newFakeStore())

	_, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "scene",
		ResourceId:   "scene-missing",
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGetSchemaIncludesParticipantsAndInviteesAttributes(t *testing.T) {
	resolver := NewSceneResolver(newFakeStore())

	resp, err := resolver.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
	require.NoError(t, err)

	sceneSchema, ok := resp.GetResourceTypes()["scene"]
	require.True(t, ok, "scene resource type missing from schema")

	attrs := sceneSchema.GetAttributes()
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST, attrs["participants"])
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_STRING_LIST, attrs["invitees"])
}

func TestResolveResourceReturnsParticipantsAndInviteesLists(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-r-1",
		Title:      "T",
		OwnerID:    "char-alice",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityPrivate),
	}))
	resolver := NewSceneResolver(store)

	resp, err := resolver.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "scene",
		ResourceId:   "scene-r-1",
	})
	require.NoError(t, err)

	participantsAttr := resp.GetAttributes()["participants"]
	require.NotNil(t, participantsAttr)
	require.NotNil(t, participantsAttr.GetStringListValue())
	assert.ElementsMatch(t, []string{"char-alice"}, participantsAttr.GetStringListValue().GetValues())

	inviteesAttr := resp.GetAttributes()["invitees"]
	require.NotNil(t, inviteesAttr)
	require.NotNil(t, inviteesAttr.GetStringListValue())
	assert.Empty(t, inviteesAttr.GetStringListValue().GetValues())
}

// TestResolveResourceDoesNotLeakPoseOrderMetadata pins INV-P4-5: the ABAC
// attribute path MUST NOT expose pose-order metadata (last_pose_at,
// last_pose_seq, total_pose_count). Even when a scene has those fields
// populated in the store, ResolveResource must omit them from the
// returned attribute map.
//
// Spec §5.5 hard-privacy boundary / INV-S9 / ADR holomush-nt2d:
// pose-order data is reachable exclusively via the gated GetPoseOrder RPC.
func TestResolveResourceDoesNotLeakPoseOrderMetadata(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-inv-p4-5",
		Title:      "Pose Meta Scene",
		OwnerID:    "char-bob",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}))
	resolver := NewSceneResolver(store)

	resp, err := resolver.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "scene",
		ResourceId:   "scene-inv-p4-5",
	})
	require.NoError(t, err)

	attrs := resp.GetAttributes()

	// INV-P4-5: these keys MUST NOT appear in the attribute map regardless
	// of what pose-metadata columns exist in the underlying store/database.
	assert.NotContains(t, attrs, "last_pose_at", "INV-P4-5: resolver MUST NOT expose last_pose_at")
	assert.NotContains(t, attrs, "last_pose_seq", "INV-P4-5: resolver MUST NOT expose last_pose_seq")
	assert.NotContains(t, attrs, "total_pose_count", "INV-P4-5: resolver MUST NOT expose total_pose_count")
	assert.NotContains(t, attrs, "LastPoseAt", "INV-P4-5: resolver MUST NOT expose LastPoseAt")
	assert.NotContains(t, attrs, "LastPoseSeq", "INV-P4-5: resolver MUST NOT expose LastPoseSeq")
	assert.NotContains(t, attrs, "TotalPoseCount", "INV-P4-5: resolver MUST NOT expose TotalPoseCount")

	// Verify the expected allowed attributes ARE present (regression guard).
	assert.Contains(t, attrs, "owner")
	assert.Contains(t, attrs, "state")
	assert.Contains(t, attrs, "visibility")
	assert.Contains(t, attrs, "participants")
}
