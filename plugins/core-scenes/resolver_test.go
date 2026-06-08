// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"regexp"
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

// TestResolverNeverExposesContentByForbiddenAttributeName pins INV-SCENE-34
// (spec §9.3): the scene attribute resolver MUST NOT expose any attribute
// whose name could carry IC content (pose/say/emit/ooc text, the publication
// log, or content_entries). The hard privacy boundary (INV-SCENE-60) keeps log
// content out of the ABAC attribute path entirely; this is the regression
// lock. It passes today — GetSchema exposes only id/owner/state/visibility/
// location/has_location/participants/invitees — and fails any future PR that
// adds a content-bearing attribute to the resolver schema.
func TestResolverNeverExposesContentByForbiddenAttributeName(t *testing.T) {
	t.Parallel()
	r := NewSceneResolver(newFakeStore())

	resp, err := r.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
	require.NoError(t, err)
	sceneSchema, ok := resp.GetResourceTypes()["scene"]
	require.True(t, ok, "schema must include 'scene' resource type")

	forbidden := regexp.MustCompile(`^(content|content_entries|poses?|says?|emits?|ooc|log|entries|publication)$`)
	for name := range sceneSchema.GetAttributes() {
		assert.False(t, forbidden.MatchString(name),
			"INV-SCENE-34 violation: resolver exposes attribute %q matching forbidden content pattern", name)
	}
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

// TestResolveResourceLocationWitness exercises the omit-optional-attrs contract
// for the scene location attribute (.claude/rules/abac-providers.md, ADR
// holomush-ti1b): the resolver emits the "location" key only when LocationID
// resolves to a non-empty value, and the has_location boolean witness is always
// present. An empty-string sentinel ("location": "") would fail-open in the DSL
// evaluator — a present "" is a value, not a missing key — so an absent or
// empty-string location omits the key entirely.
func TestResolveResourceLocationWitness(t *testing.T) {
	empty := ""
	loc := "loc-tavern"
	tests := []struct {
		name        string
		locationID  *string
		wantPresent bool
		wantValue   string
	}{
		{"omits location when LocationID is nil", nil, false, ""},
		{"omits location when LocationID derefs to empty string", &empty, false, ""},
		{"emits location when LocationID is set", &loc, true, "loc-tavern"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newFakeStore()
			store.scenes["scene-loc"] = &SceneRow{
				ID:         "scene-loc",
				OwnerID:    "char-alice",
				State:      string(SceneStateActive),
				Visibility: string(SceneVisibilityOpen),
				LocationID: tt.locationID,
			}
			r := NewSceneResolver(store)

			resp, err := r.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
				ResourceType: "scene",
				ResourceId:   "scene-loc",
			})
			require.NoError(t, err)
			attrs := resp.GetAttributes()

			require.NotNil(t, attrs["has_location"], "has_location witness MUST always be present")
			assert.Equal(t, tt.wantPresent, attrs["has_location"].GetBoolValue(),
				"has_location witness MUST reflect whether location resolved")
			if tt.wantPresent {
				require.NotNil(t, attrs["location"], "location key MUST be present when LocationID is set")
				assert.Equal(t, tt.wantValue, attrs["location"].GetStringValue())
			} else {
				assert.NotContains(t, attrs, "location",
					"abac-providers.md: location key MUST be absent (no empty-string sentinel)")
			}
		})
	}
}

// TestGetSchemaDeclaresHasLocationWitness verifies the has_location witness is
// declared in the schema as a BOOL so policies can type-check it.
func TestGetSchemaDeclaresHasLocationWitness(t *testing.T) {
	r := NewSceneResolver(newFakeStore())

	resp, err := r.GetSchema(context.Background(), &pluginv1.GetSchemaRequest{})
	require.NoError(t, err)
	sceneSchema, ok := resp.GetResourceTypes()["scene"]
	require.True(t, ok, "schema must include 'scene' resource type")
	assert.Equal(t, pluginv1.AttributeType_ATTRIBUTE_TYPE_BOOL,
		sceneSchema.GetAttributes()["has_location"],
		"has_location MUST be declared as a BOOL witness")
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

// TestResolveResourceExcludesObserverFromParticipantsAttribute pins the
// structural exclusion that makes write-scene-as-participant deny observers:
// GetWithMembership filters role IN ('owner','member'), so an observer row
// MUST NOT appear in the resolved resource.scene.participants list.
// This is the attribute-path gate referenced by INV-SCENE-61.
//
// Verifies: INV-SCENE-61
func TestResolveResourceExcludesObserverFromParticipantsAttribute(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID:         "scene-obs-excl",
		Title:      "Observer Exclusion",
		OwnerID:    "char-member",
		State:      string(SceneStateActive),
		Visibility: string(SceneVisibilityOpen),
	}))
	// Seed an observer row directly into the fake store's participants map.
	store.participants["scene-obs-excl"]["char-observer"] = "observer"

	resolver := NewSceneResolver(store)
	resp, err := resolver.ResolveResource(context.Background(), &pluginv1.ResolveResourceRequest{
		ResourceType: "scene",
		ResourceId:   "scene-obs-excl",
	})
	require.NoError(t, err)

	participantsAttr := resp.GetAttributes()["participants"]
	require.NotNil(t, participantsAttr)
	vals := participantsAttr.GetStringListValue().GetValues()
	assert.Contains(t, vals, "char-member",
		"the owner/member must appear in the participants attribute")
	assert.NotContains(t, vals, "char-observer",
		"observer MUST NOT appear in participants attribute — "+
			"this is the gate that denies observer write access via write-scene-as-participant policy")
}

// TestResolveResourceDoesNotLeakPoseOrderMetadata pins INV-SCENE-5: the ABAC
// attribute path MUST NOT expose pose-order metadata (last_pose_at,
// last_pose_seq, total_pose_count). Even when a scene has those fields
// populated in the store, ResolveResource must omit them from the
// returned attribute map.
//
// Spec §5.5 hard-privacy boundary / INV-SCENE-60 / ADR holomush-nt2d:
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

	// INV-SCENE-5: these keys MUST NOT appear in the attribute map regardless
	// of what pose-metadata columns exist in the underlying store/database.
	assert.NotContains(t, attrs, "last_pose_at", "INV-SCENE-5: resolver MUST NOT expose last_pose_at")
	assert.NotContains(t, attrs, "last_pose_seq", "INV-SCENE-5: resolver MUST NOT expose last_pose_seq")
	assert.NotContains(t, attrs, "total_pose_count", "INV-SCENE-5: resolver MUST NOT expose total_pose_count")
	assert.NotContains(t, attrs, "LastPoseAt", "INV-SCENE-5: resolver MUST NOT expose LastPoseAt")
	assert.NotContains(t, attrs, "LastPoseSeq", "INV-SCENE-5: resolver MUST NOT expose LastPoseSeq")
	assert.NotContains(t, attrs, "TotalPoseCount", "INV-SCENE-5: resolver MUST NOT expose TotalPoseCount")

	// Verify the expected allowed attributes ARE present (regression guard).
	assert.Contains(t, attrs, "owner")
	assert.Contains(t, attrs, "state")
	assert.Contains(t, attrs, "visibility")
	assert.Contains(t, attrs, "participants")
}
