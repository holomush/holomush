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
