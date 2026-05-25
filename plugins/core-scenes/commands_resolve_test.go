// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeMembershipLookup returns a fixed slice of scenes the caller is in.
type fakeMembershipLookup struct {
	scenes []string
	err    error
}

func (f *fakeMembershipLookup) ListScenesForCharacter(_ context.Context, _ string) ([]string, error) {
	return f.scenes, f.err
}

// TestResolveSceneRef_SingleMembershipNoArg — exactly one active membership and
// no arg resolves to that scene.
func TestResolveSceneRef_SingleMembershipNoArg(t *testing.T) {
	t.Parallel()
	look := &fakeMembershipLookup{scenes: []string{"scene-abc"}}
	sceneID, err := resolveSceneRef(context.Background(), look, "char-1", "")
	require.NoError(t, err)
	assert.Equal(t, "scene-abc", sceneID)
}

// TestResolveSceneRef_WhitespaceArgIsNoArg — a whitespace-only arg is treated
// as no-arg (trimmed), falling through to single-membership inference.
func TestResolveSceneRef_WhitespaceArgIsNoArg(t *testing.T) {
	t.Parallel()
	look := &fakeMembershipLookup{scenes: []string{"scene-abc"}}
	sceneID, err := resolveSceneRef(context.Background(), look, "char-2", "   ")
	require.NoError(t, err)
	assert.Equal(t, "scene-abc", sceneID)
}

// TestResolveSceneRef_ExplicitHashArg — an explicit "#<id>" overrides inference.
func TestResolveSceneRef_ExplicitHashArg(t *testing.T) {
	t.Parallel()
	look := &fakeMembershipLookup{scenes: []string{"scene-default"}}
	sceneID, err := resolveSceneRef(context.Background(), look, "char-1", "#scene-xyz")
	require.NoError(t, err)
	assert.Equal(t, "scene-xyz", sceneID, "explicit arg overrides single-membership")
}

// TestResolveSceneRef_ExplicitHashArgTrimsOuterWhitespace — surrounding
// whitespace on the whole arg is trimmed; the "#<id>" itself stays strict.
func TestResolveSceneRef_ExplicitHashArgTrimsOuterWhitespace(t *testing.T) {
	t.Parallel()
	look := &fakeMembershipLookup{scenes: []string{"scene-default"}}
	sceneID, err := resolveSceneRef(context.Background(), look, "char-1", "  #scene-xyz  ")
	require.NoError(t, err)
	assert.Equal(t, "scene-xyz", sceneID)
}

// TestResolveSceneRef_NoMembershipNoArg — zero memberships with no arg is
// ambiguous → SCENE_PUBLISH_NO_FOCUSED_SCENE.
func TestResolveSceneRef_NoMembershipNoArg(t *testing.T) {
	t.Parallel()
	look := &fakeMembershipLookup{scenes: nil}
	_, err := resolveSceneRef(context.Background(), look, "char-1", "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_PUBLISH_NO_FOCUSED_SCENE")
}

// TestResolveSceneRef_MultiMembershipNoArgRequiresExplicit — multiple
// memberships with no arg requires an explicit "#<id>".
func TestResolveSceneRef_MultiMembershipNoArgRequiresExplicit(t *testing.T) {
	t.Parallel()
	look := &fakeMembershipLookup{scenes: []string{"scene-a", "scene-b"}}
	_, err := resolveSceneRef(context.Background(), look, "char-1", "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_PUBLISH_NO_FOCUSED_SCENE")
}

// TestResolveSceneRef_ExplicitMalformed — malformed "#" references and bare
// (non-"#") args are rejected with SCENE_PUBLISH_REF_INVALID.
func TestResolveSceneRef_ExplicitMalformed(t *testing.T) {
	t.Parallel()
	look := &fakeMembershipLookup{scenes: []string{"scene-default"}}
	cases := []string{
		"notahash",           // missing '#' prefix
		"#",                  // empty id after '#'
		"##abc",              // '#' embedded in id
		"# scene-with-space", // space after '#' is not the strict "#<id>" form
		"#ab cd",             // embedded space in id
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			t.Parallel()
			_, err := resolveSceneRef(context.Background(), look, "char-1", c)
			require.Error(t, err)
			errutil.AssertErrorCode(t, err, "SCENE_PUBLISH_REF_INVALID")
		})
	}
}

// TestResolveSceneRef_LookupErrorWraps — a store lookup failure surfaces as
// SCENE_PUBLISH_REF_LOOKUP_FAILED (only on the inference path).
func TestResolveSceneRef_LookupErrorWraps(t *testing.T) {
	t.Parallel()
	look := &fakeMembershipLookup{err: assert.AnError}
	_, err := resolveSceneRef(context.Background(), look, "char-1", "")
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCENE_PUBLISH_REF_LOOKUP_FAILED")
}
