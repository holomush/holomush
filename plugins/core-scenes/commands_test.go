// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func newTestPlugin() *scenePlugin {
	store := newFakeStore()
	svc := NewSceneServiceImpl(store)
	svc.SetEventSink(&recordingEventSink{})
	return &scenePlugin{
		store:   nil, // not used by command handlers
		service: svc,
	}
}

func TestHandleCommandReturnsUsageWhenSubcommandIsMissing(t *testing.T) {
	p := newTestPlugin()

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage")
}

func TestHandleCommandRejectsUnknownSubcommand(t *testing.T) {
	p := newTestPlugin()

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "frobnicate",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Unknown")
}

func TestHandleCommandCreateInvokesSceneServiceCreateScene(t *testing.T) {
	p := newTestPlugin()

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "create A New Scene",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Scene created")
	assert.True(t, strings.Contains(resp.Output, "scene-"), "output should include the scene id")
}

func TestHandleCommandInfoShowsCreatedScene(t *testing.T) {
	p := newTestPlugin()

	// Create a scene first.
	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "create The Manor",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, createResp.Status)

	// Extract the scene ID from the create output. Output format is:
	//   "Scene created: <id>"
	parts := strings.Split(createResp.Output, "Scene created:")
	require.Len(t, parts, 2)
	sceneID := strings.TrimSpace(parts[1])

	// Info on that scene.
	infoResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "info " + sceneID,
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, infoResp.Status)
	assert.Contains(t, infoResp.Output, "The Manor")
	assert.Contains(t, infoResp.Output, "char-alice")
}

func TestHandleCommandInfoReturnsErrorWhenSceneIDIsMissing(t *testing.T) {
	p := newTestPlugin()

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "info",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "scene id")
}

func TestHandleCommandEndCallsEndScene(t *testing.T) {
	p := newTestPlugin()
	// Pre-create a scene in the fake store via the service
	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "create The Manor",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, createResp.Status)

	parts := strings.Split(createResp.Output, "Scene created:")
	require.Len(t, parts, 2)
	sceneID := strings.TrimSpace(parts[1])

	// End it
	endResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "end " + sceneID,
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, endResp.Status)
	assert.Contains(t, endResp.Output, "ended")
}

func TestHandleCommandEndReturnsErrorWhenSceneIDIsMissing(t *testing.T) {
	p := newTestPlugin()
	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "end",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "scene id")
}

func TestHandleCommandPauseCallsPauseScene(t *testing.T) {
	p := newTestPlugin()
	sceneID := createSceneInTest(t, p, "char-alice", "Pausable")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "pause " + sceneID,
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "paused")
}

func TestHandleCommandResumeCallsResumeScene(t *testing.T) {
	p := newTestPlugin()
	sceneID := createSceneInTest(t, p, "char-alice", "Resumable")

	// Pause first
	_, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "pause " + sceneID,
		CharacterID: "char-alice",
	})
	require.NoError(t, err)

	// Then resume
	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "resume " + sceneID,
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "resumed")
}

func TestHandleCommandSetUpdatesTitle(t *testing.T) {
	p := newTestPlugin()
	sceneID := createSceneInTest(t, p, "char-alice", "Original Title")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "set " + sceneID + " title=New Title",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "updated")

	// Verify via info
	infoResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "info " + sceneID,
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Contains(t, infoResp.Output, "New Title")
}

func TestHandleCommandSetRejectsUnknownField(t *testing.T) {
	p := newTestPlugin()
	sceneID := createSceneInTest(t, p, "char-alice", "T")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "set " + sceneID + " bogus=foo",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "unknown field")
}

func TestHandleCommandSetRejectsMissingEqualsSeparator(t *testing.T) {
	p := newTestPlugin()
	sceneID := createSceneInTest(t, p, "char-alice", "T")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "set " + sceneID + " title",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "field=value")
}

// createSceneInTest is a helper that creates a scene via the command path
// and returns its ID. Used by Phase 2 tests that need a scene to operate on.
//
//nolint:unparam // characterID is parameterised for clarity at call sites even though every current caller passes "char-alice"; future tests with multi-character setups will need this without changing the signature
func createSceneInTest(t *testing.T, p *scenePlugin, characterID, title string) string {
	t.Helper()
	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "create " + title,
		CharacterID: characterID,
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status)
	parts := strings.Split(resp.Output, "Scene created:")
	require.Len(t, parts, 2)
	return strings.TrimSpace(parts[1])
}

func TestSceneCommandJoinForwardsToServiceWithCorrectSceneID(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-cmd-j", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	fc := &fakeFocusClient{}
	plugin := &scenePlugin{service: NewSceneServiceImpl(store), focusClient: fc}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join scene-cmd-j",
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Joined scene scene-cmd-j")
	require.Len(t, fc.joinCalls, 1)
	assert.Equal(t, "scene-cmd-j", fc.joinCalls[0].target.TargetID)
}

func TestSceneCommandLeaveRejectsMissingSceneID(t *testing.T) {
	plugin := &scenePlugin{service: NewSceneServiceImpl(newFakeStore())}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "leave", CharacterID: "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage: scene leave")
}

func TestSceneCommandInviteParsesSceneIDAndTarget(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-cmd-i", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	plugin := &scenePlugin{service: NewSceneServiceImpl(store)}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "invite scene-cmd-i char-bob", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "invited", store.participants["scene-cmd-i"]["char-bob"])
}

func TestSceneCommandTransferRejectsMissingTarget(t *testing.T) {
	plugin := &scenePlugin{service: NewSceneServiceImpl(newFakeStore())}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "transfer scene-x", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "Usage: scene transfer")
}

func TestSceneCommandLeaveForwardsToServiceWithCorrectSceneID(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-cmd-l", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-cmd-l", "char-bob")
	require.NoError(t, err)
	plugin := &scenePlugin{service: NewSceneServiceImpl(store)}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "leave scene-cmd-l", CharacterID: "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Left scene scene-cmd-l")
}

func TestSceneCommandKickRemovesTargetFromScene(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-cmd-k", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-cmd-k", "char-bob")
	require.NoError(t, err)
	plugin := &scenePlugin{service: NewSceneServiceImpl(store)}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "kick scene-cmd-k char-bob", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Removed char-bob from scene scene-cmd-k")
}

func TestSceneCommandTransferChangesOwnership(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-cmd-t", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityOpen),
	}))
	_, _, err := store.AddParticipant(context.Background(), "scene-cmd-t", "char-bob")
	require.NoError(t, err)
	plugin := &scenePlugin{service: NewSceneServiceImpl(store)}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "transfer scene-cmd-t char-bob", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Transferred ownership of scene scene-cmd-t to char-bob")
}

func TestParticipantRoleIsValidReturnsExpectedResults(t *testing.T) {
	tests := []struct {
		name  string
		role  ParticipantRole
		valid bool
	}{
		{"owner is valid", ParticipantRoleOwner, true},
		{"member is valid", ParticipantRoleMember, true},
		{"invited is valid", ParticipantRoleInvited, true},
		{"empty string is invalid", ParticipantRole(""), false},
		{"arbitrary string is invalid", ParticipantRole("admin"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.valid, tt.role.IsValid())
		})
	}
}

// TestMembershipCommandsRejectExtraPositionalTokens exercises the strict
// arity check added to the membership command handlers. Without this check,
// `scene join scn-123 typo` would forward "scn-123 typo" to the SceneService
// as SceneId and surface a confusing RPC error to the player instead of a
// usage message. Each subtest sends a command with one trailing token beyond
// what the handler accepts and asserts the response is CommandError with the
// usage hint.
func TestMembershipCommandsRejectExtraPositionalTokens(t *testing.T) {
	tests := []struct {
		name      string
		args      string
		wantUsage string
	}{
		{
			name:      "join rejects one trailing token",
			args:      "join scene-x extra",
			wantUsage: "Usage: scene join",
		},
		{
			name:      "leave rejects one trailing token",
			args:      "leave scene-x extra",
			wantUsage: "Usage: scene leave",
		},
		{
			name:      "invite rejects one trailing token beyond scene-id and target",
			args:      "invite scene-x char-bob typo",
			wantUsage: "Usage: scene invite",
		},
		{
			name:      "kick rejects one trailing token beyond scene-id and target",
			args:      "kick scene-x char-bob typo",
			wantUsage: "Usage: scene kick",
		},
		{
			name:      "transfer rejects one trailing token beyond scene-id and new owner",
			args:      "transfer scene-x char-bob typo",
			wantUsage: "Usage: scene transfer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plugin := &scenePlugin{service: NewSceneServiceImpl(newFakeStore())}

			resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "scene",
				Args:        tt.args,
				CharacterID: "char-alice",
			})
			require.NoError(t, err)
			assert.Equal(t, pluginsdk.CommandError, resp.Status,
				"expected CommandError for args %q", tt.args)
			assert.Contains(t, resp.Output, tt.wantUsage,
				"expected usage hint for args %q", tt.args)
		})
	}
}

// --- fakeFocusClient ---

type focusCall struct {
	sessionID string
	target    pluginsdk.FocusKey
}

type fakeFocusClient struct {
	joinCalls    []focusCall
	leaveCalls   []focusCall
	presentCalls []focusCall

	joinErr    error
	leaveErr   error
	presentErr error
}

func (f *fakeFocusClient) JoinFocus(_ context.Context, sid string, t pluginsdk.FocusKey) error {
	f.joinCalls = append(f.joinCalls, focusCall{sessionID: sid, target: t})
	return f.joinErr
}

func (f *fakeFocusClient) LeaveFocus(_ context.Context, sid string, t pluginsdk.FocusKey) error {
	f.leaveCalls = append(f.leaveCalls, focusCall{sessionID: sid, target: t})
	return f.leaveErr
}

func (f *fakeFocusClient) PresentFocus(_ context.Context, sid string, t pluginsdk.FocusKey) error {
	f.presentCalls = append(f.presentCalls, focusCall{sessionID: sid, target: t})
	return f.presentErr
}

func (f *fakeFocusClient) QueryStreamHistory(_ context.Context, _ pluginsdk.QueryStreamHistoryRequest) ([]pluginsdk.Event, error) {
	return nil, nil
}

// newTestPluginWithFocus returns a scenePlugin wired with a fakeFocusClient
// and a fakeStore-backed SceneServiceImpl. Tests that exercise the
// command-path focus wiring use this in place of newTestPlugin.
func newTestPluginWithFocus() (*scenePlugin, *fakeFocusClient) {
	p := newTestPlugin()
	fc := &fakeFocusClient{}
	p.focusClient = fc
	return p, fc
}

// extractSceneID parses a "Scene created: <id>" output into the id substring.
func extractSceneID(t *testing.T, output string) string {
	t.Helper()
	parts := strings.Split(output, "Scene created:")
	require.Len(t, parts, 2)
	return strings.TrimSpace(parts[1])
}

// --- scene join / leave / end / switch focus-wiring tests ---

func TestSceneJoinCallsFocusClientJoinFocus(t *testing.T) {
	p, fc := newTestPluginWithFocus()

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	require.Len(t, fc.joinCalls, 1)
	assert.Equal(t, "sess-bob", fc.joinCalls[0].sessionID)
	assert.Equal(t, pluginsdk.FocusKindScene, fc.joinCalls[0].target.Kind)
	assert.Equal(t, sceneID, fc.joinCalls[0].target.TargetID)
}

func TestSceneJoinPropagatesJoinSceneError(t *testing.T) {
	p, fc := newTestPluginWithFocus()

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join scene-does-not-exist",
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Empty(t, fc.joinCalls, "JoinFocus MUST NOT run when service.JoinScene fails")
}

func TestSceneJoinHandlesJoinFocusError(t *testing.T) {
	p, fc := newTestPluginWithFocus()
	fc.joinErr = oops.Code("FOCUS_POLICY_FAILED").Errorf("policy rejected")

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "retry")
}

func TestSceneJoinTreatsFocusAlreadyMemberAsSuccess(t *testing.T) {
	p, fc := newTestPluginWithFocus()
	fc.joinErr = oops.Code("FOCUS_ALREADY_MEMBER").Errorf("duplicate")

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join " + sceneID,
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
}

func TestSceneLeaveCallsLeaveScene(t *testing.T) {
	p, fc := newTestPluginWithFocus()

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	_, err = p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "join " + sceneID, CharacterID: "char-bob", SessionID: "sess-bob",
	})
	require.NoError(t, err)

	fc.joinCalls = nil

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "leave " + sceneID, CharacterID: "char-bob", SessionID: "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	require.Len(t, fc.leaveCalls, 1)
	assert.Equal(t, "sess-bob", fc.leaveCalls[0].sessionID)
	assert.Equal(t, sceneID, fc.leaveCalls[0].target.TargetID)
}

func TestSceneLeaveRejectsOwnerBeforeFocusChange(t *testing.T) {
	p, fc := newTestPluginWithFocus()

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "leave " + sceneID, CharacterID: "char-owner", SessionID: "sess-owner",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Empty(t, fc.leaveCalls, "LeaveFocus MUST NOT run when owner-leave is rejected")
}

func TestSceneLeaveToleratesLeaveFocusError(t *testing.T) {
	p, fc := newTestPluginWithFocus()
	fc.leaveErr = errors.New("transient host error")

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	_, err = p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "join " + sceneID, CharacterID: "char-bob", SessionID: "sess-bob",
	})
	require.NoError(t, err)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "leave " + sceneID, CharacterID: "char-bob", SessionID: "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status, "DB leave succeeded; focus-side failure is logged, not surfaced")
}

func TestSceneEndCallsLeaveFocusForOwner(t *testing.T) {
	p, fc := newTestPluginWithFocus()

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "end " + sceneID, CharacterID: "char-owner", SessionID: "sess-owner",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	require.Len(t, fc.leaveCalls, 1)
	assert.Equal(t, "sess-owner", fc.leaveCalls[0].sessionID)
	assert.Equal(t, sceneID, fc.leaveCalls[0].target.TargetID)
}

func TestSceneSwitchCallsPresentFocus(t *testing.T) {
	p, fc := newTestPluginWithFocus()

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "switch scene-01", CharacterID: "char-bob", SessionID: "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	require.Len(t, fc.presentCalls, 1)
	assert.Equal(t, "sess-bob", fc.presentCalls[0].sessionID)
	assert.Equal(t, pluginsdk.FocusKindScene, fc.presentCalls[0].target.Kind)
	assert.Equal(t, "scene-01", fc.presentCalls[0].target.TargetID)
}

func TestSceneSwitchReturnsNotMemberError(t *testing.T) {
	p, fc := newTestPluginWithFocus()
	fc.presentErr = oops.Code("FOCUS_NOT_MEMBER").Errorf("not joined")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "switch scene-01", CharacterID: "char-bob", SessionID: "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "not a member")
	assert.Contains(t, resp.Output, "scene join")
}

func TestSceneSwitchStrictArity(t *testing.T) {
	p, _ := newTestPluginWithFocus()

	tests := []struct {
		name string
		args string
	}{
		{"rejects empty", "switch"},
		{"rejects trailing tokens", "switch scene-01 trailing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command: "scene", Args: tt.args, CharacterID: "char-bob", SessionID: "sess-bob",
			})
			require.NoError(t, err)
			assert.Equal(t, pluginsdk.CommandError, resp.Status)
			assert.Contains(t, resp.Output, "Usage")
		})
	}
}
