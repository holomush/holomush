// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeSceneID covers the shared scene-ref token normalizer: it strips
// a single optional leading '#' and surrounding whitespace, yielding the bare
// scene ULID used downstream (holomush-ehbnk / holomush-y5inx).
func TestNormalizeSceneID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare id is returned unchanged", "01KSTF08ABCDEF", "01KSTF08ABCDEF"},
		{"single leading hash is stripped", "#01KSTF08ABCDEF", "01KSTF08ABCDEF"},
		{"surrounding whitespace is trimmed", "  #01KSTF08ABCDEF  ", "01KSTF08ABCDEF"},
		{"only the first hash is stripped", "##01KSTF08ABCDEF", "#01KSTF08ABCDEF"},
		{"bare with whitespace is trimmed", "  01KSTF08ABCDEF ", "01KSTF08ABCDEF"},
		{"lone hash yields empty", "#", ""},
		{"empty stays empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizeSceneID(tt.input))
		})
	}
}

// TestSceneResourceRefStripsHashPrefix verifies the ABAC resource-ref helpers
// strip an optional '#' so the resource ref evaluated by the host engine uses
// the same bare scene id the handler passes downstream (no scene:#<id> skew).
func TestSceneResourceRefStripsHashPrefix(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) (string, error)
	}{
		{"sceneResourceRef", sceneResourceRef},
		{"sceneResourceRefFirstField", sceneResourceRefFirstField},
	}
	for _, tt := range tests {
		t.Run(tt.name+" strips hash", func(t *testing.T) {
			got, err := tt.fn("#01KSTF08ABCDEF extra")
			require.NoError(t, err)
			assert.Equal(t, "scene:01KSTF08ABCDEF", got)
		})
		t.Run(tt.name+" accepts bare", func(t *testing.T) {
			got, err := tt.fn("01KSTF08ABCDEF extra")
			require.NoError(t, err)
			assert.Equal(t, "scene:01KSTF08ABCDEF", got)
		})
		t.Run(tt.name+" rejects empty", func(t *testing.T) {
			_, err := tt.fn("   ")
			require.Error(t, err)
		})
	}
}

// TestSceneFocusAcceptsBareAndHash verifies `scene focus` leniently accepts
// both the '#'-prefixed display form and a bare scene id, dispatching the bare
// id to SetConnectionFocus in both cases (holomush-ehbnk direction A).
func TestSceneFocusAcceptsBareAndHash(t *testing.T) {
	const bareID = "01KSTF08ABCDEF"
	for _, form := range []string{bareID, "#" + bareID} {
		t.Run("focus "+form, func(t *testing.T) {
			p, fc := newTestPluginWithFocus(t)
			resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:      "scene",
				Args:         "focus " + form,
				CharacterID:  "char-bob",
				SessionID:    "sess-bob",
				ConnectionID: "01JW0000000000000000000013",
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, pluginsdk.CommandOK, resp.Status)
			require.Len(t, fc.setConnFocusCalls, 1, "SetConnectionFocus MUST be called once")
			require.NotNil(t, fc.setConnFocusCalls[0].focusKey)
			assert.Equal(t, bareID, fc.setConnFocusCalls[0].focusKey.TargetID,
				"downstream MUST receive the bare scene id (no '#')")
		})
	}
}

// TestSceneJoinAcceptsBareAndHash verifies `scene join` leniently accepts both
// the '#'-prefixed display form (as surfaced in the web RECENT panel) and a
// bare id, passing the bare id to the focus substrate in both cases.
func TestSceneJoinAcceptsBareAndHash(t *testing.T) {
	for _, prefix := range []string{"", "#"} {
		t.Run("join prefix="+prefix, func(t *testing.T) {
			p, fc := newTestPluginWithFocus(t)
			createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
			})
			require.NoError(t, err)
			sceneID := extractSceneID(t, createResp.Output)

			resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "scene",
				Args:        "join " + prefix + sceneID,
				CharacterID: "char-bob",
				SessionID:   "sess-bob",
			})
			require.NoError(t, err)
			assert.Equal(t, pluginsdk.CommandOK, resp.Status)
			require.Len(t, fc.joinCalls, 1, "JoinFocus MUST be called once")
			assert.Equal(t, sceneID, fc.joinCalls[0].target.TargetID,
				"JoinFocus MUST receive the bare scene id")
			require.Len(t, fc.autoFocusOnJoinCalls, 1, "AutoFocusOnJoin MUST be called once")
			assert.Equal(t, sceneID, fc.autoFocusOnJoinCalls[0].sceneID,
				"AutoFocusOnJoin MUST receive the bare scene id")
		})
	}
}

// TestSceneSwitchAcceptsBareAndHash verifies `scene switch` leniently accepts
// both forms, passing the bare id to PresentFocus.
func TestSceneSwitchAcceptsBareAndHash(t *testing.T) {
	const bareID = "01KSTF08ABCDEF"
	for _, form := range []string{bareID, "#" + bareID} {
		t.Run("switch "+form, func(t *testing.T) {
			p, fc := newTestPluginWithFocus(t)
			resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "scene",
				Args:        "switch " + form,
				CharacterID: "char-bob",
				SessionID:   "sess-bob",
			})
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, pluginsdk.CommandOK, resp.Status)
			require.Len(t, fc.presentCalls, 1, "PresentFocus MUST be called once")
			assert.Equal(t, bareID, fc.presentCalls[0].target.TargetID,
				"PresentFocus MUST receive the bare scene id")
		})
	}
}
