// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- scene list tests ---

// TestHandleSceneList_EmptyMemberships verifies the empty-state output when
// the character is not in any scenes.
func TestHandleSceneList_EmptyMemberships(t *testing.T) {
	p, _ := newTestPluginWithFocus(t)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "list",
		CharacterID: "char-nobody",
		SessionID:   "sess-nobody",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "You're not in any scenes.")
}

// TestHandleSceneList_RendersFocusedAndBackground verifies that two scene
// memberships render with [focused] and [background] markers respectively.
func TestHandleSceneList_RendersFocusedAndBackground(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	// Create two scenes and join char-alice to both.
	createA, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create Scene Alpha", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	sceneA := extractSceneID(t, createA.Output)

	createB, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create Scene Beta", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	sceneB := extractSceneID(t, createB.Output)

	// char-alice is owner of both scenes (implicit membership). Configure
	// IsAnyConnFocused: sceneA is focused, sceneB is not.
	fc.isAnyConnFocusedResult = map[string]bool{
		sceneA: true,
		sceneB: false,
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "list",
		CharacterID: "char-alice",
		SessionID:   "sess-alice",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "[focused]", "focused scene must show [focused] marker")
	assert.Contains(t, resp.Output, "[background]", "background scene must show [background] marker")
	assert.Contains(t, resp.Output, sceneA, "scene A id must appear in output")
	assert.Contains(t, resp.Output, sceneB, "scene B id must appear in output")
}

// TestHandleSceneList_FiltersNonSceneMemberships verifies that the list
// command only shows scene memberships, not channel or other kinds.
// The store only tracks scene participants, so a character with no scene
// memberships sees the empty-state message even if they have other focuses.
func TestHandleSceneList_FiltersNonSceneMemberships(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)
	// No scene memberships; isAnyConnFocused configured to return true for
	// a hypothetical non-scene target — must not appear.
	fc.isAnyConnFocusedResult = map[string]bool{
		"01HCHANNEL0000000000000001": true,
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "list",
		CharacterID: "char-nobody",
		SessionID:   "sess-nobody",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "You're not in any scenes.")
}

// TestHandleSceneList_RPCError verifies that an IsAnyConnFocused RPC error
// surfaces with a SCENE_LIST_FAILED oops code.
func TestHandleSceneList_RPCError(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)
	fc.isAnyConnFocusedErr = errors.New("coordinator unavailable")

	// Create a scene so the character has at least one membership to trigger
	// the IsAnyConnFocused call.
	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	_ = extractSceneID(t, createResp.Output)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "list",
		CharacterID: "char-alice",
		SessionID:   "sess-alice",
	})
	// Handler returns (nil, err) on RPC failure.
	require.Error(t, err)
	assert.Nil(t, resp)
	var oe oops.OopsError
	require.True(t, errors.As(err, &oe), "error must be an oops error")
	assert.Equal(t, "SCENE_LIST_FAILED", oe.Code())
}

// TestHandleSceneGrid_PreservesPresentingFocus pins INV-SCENE-26: scene grid
// calls SetConnectionFocus with focusKey=nil and isSceneGrid=true.
// The substrate skips the PresentingFocus write (D10) — the plugin's only
// responsibility is to issue the RPC with the correct arguments.
func TestHandleSceneGrid_PreservesPresentingFocus(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	connID := "01JW0000000000000000000001"
	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "grid",
		CharacterID:  "char-bob",
		SessionID:    "sess-bob",
		ConnectionID: connID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Focused on the grid.")

	// INV-SCENE-26: SetConnectionFocus MUST be called with focusKey=nil and isSceneGrid=true.
	require.Len(t, fc.setConnFocusCalls, 1, "SetConnectionFocus MUST be called exactly once")
	call := fc.setConnFocusCalls[0]
	assert.Equal(t, connID, call.connectionID, "connection ID MUST match req.ConnectionID")
	assert.Nil(t, call.focusKey, "focusKey MUST be nil for scene grid (D10)")
	assert.True(t, call.isSceneGrid, "isSceneGrid MUST be true so substrate skips PresentingFocus write (INV-SCENE-26)")
}

// TestHandleSceneGrid_ReturnsErrorWhenFocusClientNil verifies that scene grid
// returns an error when the focus client is not configured, parallel to the
// scene switch not-configured test.
func TestHandleSceneGrid_ReturnsErrorWhenFocusClientNil(t *testing.T) {
	p := newTestPlugin(t) // no focusClient

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "grid",
		CharacterID:  "char-bob",
		SessionID:    "sess-bob",
		ConnectionID: "01JW0000000000000000000001",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "focus client not configured")
}

// TestHandleSceneGrid_PropagatesRPCError verifies that errors from
// SetConnectionFocus surface as an internal error (nil return from handler
// triggers host-side error logging per convention).
func TestHandleSceneGrid_PropagatesRPCError(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)
	fc.setConnFocusErr = errors.New("host unavailable")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "grid",
		CharacterID:  "char-bob",
		SessionID:    "sess-bob",
		ConnectionID: "01JW0000000000000000000001",
	})
	// Handler returns (nil, err) on RPC failure — host logs + surfaces to player.
	require.Error(t, err)
	assert.Nil(t, resp)
}

// --- scene focus tests (T19 / holomush-5rh.14.20) ---

// TestHandleSceneFocus_HappyPath verifies the canonical focus path: a valid
// scene reference with a scene the character is a member of causes
// SetConnectionFocus to be called with {Kind:scene, TargetID:sceneID} and
// isSceneGrid=false; the response contains the "You're now focused on Scene"
// message.
func TestHandleSceneFocus_HappyPath(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	connID := "01JW0000000000000000000010"
	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "focus #" + sceneID,
		CharacterID:  "char-owner",
		SessionID:    "sess-owner",
		ConnectionID: connID,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "You're now focused on Scene")
	assert.Contains(t, resp.Output, sceneID)

	// SetConnectionFocus MUST be called with the scene key and isSceneGrid=false.
	require.Len(t, fc.setConnFocusCalls, 1, "SetConnectionFocus MUST be called exactly once")
	call := fc.setConnFocusCalls[0]
	assert.Equal(t, connID, call.connectionID, "connection ID MUST match req.ConnectionID")
	require.NotNil(t, call.focusKey, "focusKey MUST be non-nil for scene focus")
	assert.Equal(t, pluginsdk.FocusKindScene, call.focusKey.Kind)
	assert.Equal(t, sceneID, call.focusKey.TargetID)
	assert.False(t, call.isSceneGrid, "isSceneGrid MUST be false for explicit scene focus")
}

// TestHandleSceneFocus_NotInScene verifies that when the substrate returns
// FOCUS_WITHOUT_MEMBERSHIP, the handler renders the membership-denied message
// without returning a Go error.
func TestHandleSceneFocus_NotInScene(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	// Substrate returns FOCUS_WITHOUT_MEMBERSHIP.
	fc.setConnFocusErr = oops.Code("FOCUS_WITHOUT_MEMBERSHIP").Errorf("not a member")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "focus #" + sceneID,
		CharacterID:  "char-outsider",
		SessionID:    "sess-outsider",
		ConnectionID: "01JW0000000000000000000011",
	})
	require.NoError(t, err, "FOCUS_WITHOUT_MEMBERSHIP is a user error, not a Go error")
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "You're not in Scene")
	assert.Contains(t, resp.Output, sceneID)
}

// TestHandleSceneFocus_OtherSubstrateError verifies that unexpected substrate
// errors surface as SCENE_FOCUS_FAILED internal errors.
func TestHandleSceneFocus_OtherSubstrateError(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	fc.setConnFocusErr = errors.New("coordinator unavailable")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "focus #" + sceneID,
		CharacterID:  "char-owner",
		SessionID:    "sess-owner",
		ConnectionID: "01JW0000000000000000000012",
	})
	require.Error(t, err, "non-membership errors must surface as Go errors")
	assert.Nil(t, resp)
	var oe oops.OopsError
	require.True(t, errors.As(err, &oe), "error must be oops-coded")
	assert.Equal(t, "SCENE_FOCUS_FAILED", oe.Code())
}

// TestHandleSceneFocus_LoneHashRejected verifies that a ref that normalizes to
// the empty string (a lone '#') returns a usage error without calling
// SetConnectionFocus. The '#' prefix is now optional (holomush-ehbnk); a bare
// ref is accepted (see TestSceneFocusAcceptsBareAndHash), so the only parse
// rejection left is an empty-after-normalize ref.
func TestHandleSceneFocus_LoneHashRejected(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "focus #", // normalizes to empty
		CharacterID:  "char-bob",
		SessionID:    "sess-bob",
		ConnectionID: "01JW0000000000000000000013",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage:")
	assert.Empty(t, fc.setConnFocusCalls, "SetConnectionFocus MUST NOT be called on empty ref")
}

// TestHandleSceneFocus_MissingArg verifies that `scene focus` with no argument
// returns a usage error without calling SetConnectionFocus.
func TestHandleSceneFocus_MissingArg(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:      "scene",
		Args:         "focus",
		CharacterID:  "char-bob",
		SessionID:    "sess-bob",
		ConnectionID: "01JW0000000000000000000014",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage:")
	assert.Empty(t, fc.setConnFocusCalls, "SetConnectionFocus MUST NOT be called on missing arg")
}

// --- scene join → AutoFocusOnJoin wiring tests (T23 / holomush-5rh.14.23) ---

// mustParseULID is a test helper that panics on invalid ULID strings, keeping
// table setups concise.
func mustParseULID(s string) ulid.ULID {
	id, err := ulid.ParseStrict(s)
	if err != nil {
		panic(fmt.Sprintf("mustParseULID(%q): %v", s, err))
	}
	return id
}

// TestHandleJoin_AutoFocus_Terminal verifies the terminal-focused render branch:
// when AutoFocusOnJoin returns focused=[conn] and skipped+failed empty, the
// response contains the "focused your terminal connection(s)" message.
func TestHandleJoin_AutoFocus_Terminal(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	// Create a scene so JoinScene succeeds.
	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	connID := mustParseULID("01JW0000000000000000000001")
	fc.autoFocusOnJoinResult = pluginsdk.AutoFocusOnJoinResult{
		FocusedConnectionIDs: []ulid.ULID{connID},
		TotalConnectionCount: 1,
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, sceneID)
	assert.Contains(t, resp.Output, "focused your terminal connection(s)")

	// Verify AutoFocusOnJoin was called with correct IDs.
	require.Len(t, fc.autoFocusOnJoinCalls, 1)
	assert.Equal(t, "char-bob", fc.autoFocusOnJoinCalls[0].characterID)
	assert.Equal(t, sceneID, fc.autoFocusOnJoinCalls[0].sceneID)
}

// TestHandleJoin_AutoFocus_SkippedExplicit verifies the skipped-explicit render branch:
// when skipped=[conn] and focused empty, the response indicates the terminal stays on
// its current focus (INV-SCENE-24 signal from substrate).
func TestHandleJoin_AutoFocus_SkippedExplicit(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	connID := mustParseULID("01JW0000000000000000000002")
	fc.autoFocusOnJoinResult = pluginsdk.AutoFocusOnJoinResult{
		SkippedConnectionIDs: []ulid.ULID{connID},
		TotalConnectionCount: 1,
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, sceneID)
	assert.Contains(t, resp.Output, "terminal stays on its current focus")
}

// TestHandleJoin_AutoFocus_CommsHubOnly verifies the comms-hub-only render branch:
// when TotalConnectionCount > 0 but both focused and skipped are empty, the substrate
// filtered out all connections as comms_hub (INV-SCENE-17).
func TestHandleJoin_AutoFocus_CommsHubOnly(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	// TotalConnectionCount=1 but no focused/skipped → comms_hub only.
	fc.autoFocusOnJoinResult = pluginsdk.AutoFocusOnJoinResult{
		TotalConnectionCount: 1,
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, sceneID)
	assert.Contains(t, resp.Output, "scene focus")
}

// TestHandleJoin_AutoFocus_NoConnections verifies the no-connections render branch:
// TotalConnectionCount == 0 → plain join message (admin / scripted join).
func TestHandleJoin_AutoFocus_NoConnections(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	fc.autoFocusOnJoinResult = pluginsdk.AutoFocusOnJoinResult{
		TotalConnectionCount: 0,
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, sceneID)
	// Plain join: no focus-related text.
	assert.NotContains(t, resp.Output, "focus")
	assert.NotContains(t, resp.Output, "terminal")
}

// TestHandleJoin_AutoFocus_FailedConnections verifies the failure-render
// branch fires FIRST (per CodeRabbit PR #4191 ordering fix). Without this
// pin, a future branch-order regression that places the failure check below
// the success branch could mask per-connection auto-focus failures.
func TestHandleJoin_AutoFocus_FailedConnections(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Hall", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	failedConn := ulid.Make()
	// Substrate returns a mix: 1 focused success + 1 failed conn. The
	// failure-first branch ordering MUST surface the warning, NOT the
	// success message.
	fc.autoFocusOnJoinResult = pluginsdk.AutoFocusOnJoinResult{
		FocusedConnectionIDs: []ulid.ULID{ulid.Make()},
		FailedConnectionIDs: []pluginsdk.AutoFocusFailure{
			{ConnectionID: failedConn, Reason: "membership_absent"},
		},
		TotalConnectionCount: 2,
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "auto-focus failed", "failure-first branch MUST fire even when focused is non-empty")
	assert.Contains(t, resp.Output, sceneID)
}

// TestHandleJoin_AutoFocus_MixedFocusedSkipped verifies the mixed-render branch
// (D-07): when AutoFocusOnJoin returns BOTH focused and skipped connections
// (and no failures), the render surfaces an explicit informative line rather
// than falling to the least-informative default "Joined scene #X." — closing
// the SCENEFWD-03 silent-failure edge case.
func TestHandleJoin_AutoFocus_MixedFocusedSkipped(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	// One terminal connection auto-focused, another skipped (explicitly
	// focused elsewhere), no failures.
	fc.autoFocusOnJoinResult = pluginsdk.AutoFocusOnJoinResult{
		FocusedConnectionIDs: []ulid.ULID{ulid.Make()},
		SkippedConnectionIDs: []ulid.ULID{ulid.Make()},
		TotalConnectionCount: 2,
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, sceneID)
	// The mixed-outcome message informs about both outcomes and points at
	// 'scene focus' — distinct from the bare "Joined scene #X." default.
	assert.Contains(t, resp.Output, "focused some connection(s)")
	assert.Contains(t, resp.Output, "stay on their current focus")
	assert.Contains(t, resp.Output, "scene focus")
	assert.NotEqual(t, fmt.Sprintf("Joined scene #%s.", sceneID), resp.Output,
		"mixed outcome MUST NOT render the bare default")
}

// TestHandleJoin_AutoFocus_RPCError_NonFatal verifies that AutoFocusOnJoin RPC
// errors are non-fatal: the join succeeds (CommandOK), the error is included in
// the output as a warning, and no Go error is returned to the host.
func TestHandleJoin_AutoFocus_RPCError_NonFatal(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	fc.autoFocusOnJoinErr = errors.New("coordinator unavailable")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	// Non-fatal: no Go error returned.
	require.NoError(t, err)
	require.NotNil(t, resp)
	// Join succeeded despite AutoFocus failure.
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, sceneID)
	// Warning text present (case-insensitive — message starts with "Auto-focus"
	// at sentence position).
	assert.Contains(t, strings.ToLower(resp.Output), "auto-focus")
	// PR #4191 round 6: the raw RPC error MUST NOT appear in the
	// user-facing output (logged only). We swapped the error-bearing
	// "(auto-focus call failed: <err>)" suffix for a fixed fallback.
	assert.NotContains(t, resp.Output, "coordinator unavailable")
}
