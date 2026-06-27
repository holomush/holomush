// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

func newTestPlugin(t testing.TB) *scenePlugin {
	t.Helper()
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	svc.SetHostEvaluator(allowEvaluator{}) // allow all; propagate to service-level gates (EndScene/PauseScene/ResumeScene)
	return &scenePlugin{
		store:     nil, // not used by command handlers
		service:   svc,
		evaluator: allowEvaluator{}, // allow all by default; use newScenePluginWithEvaluator for deny/nil tests
	}
}

func TestHandleCommandReturnsUsageWhenSubcommandIsMissing(t *testing.T) {
	p := newTestPlugin(t)

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
	p := newTestPlugin(t)

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
	p := newTestPlugin(t)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "create A New Scene",
		CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Scene created")
	assert.False(t, strings.Contains(resp.Output, "scene-"),
		"scene id in output must be a bare ULID, not scene- prefixed (holomush-y5inx)")
	assert.Contains(t, resp.Output, "Scene created: ", "output should include the scene id")
}

func TestHandleCommandInfoShowsCreatedScene(t *testing.T) {
	p := newTestPlugin(t)

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
	p := newTestPlugin(t)

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
	p := newTestPlugin(t)
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
	p := newTestPlugin(t)
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
	p := newTestPlugin(t)
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
	p := newTestPlugin(t)
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
	p := newTestPlugin(t)
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
	p := newTestPlugin(t)
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
	p := newTestPlugin(t)
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
	plugin := &scenePlugin{service: newTestService(t, store), focusClient: fc, evaluator: allowEvaluator{}}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "join scene-cmd-j",
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "scene-cmd-j")
	require.Len(t, fc.joinCalls, 1)
	assert.Equal(t, "scene-cmd-j", fc.joinCalls[0].target.TargetID)
}

func TestSceneCommandLeaveRejectsMissingSceneID(t *testing.T) {
	// Gate resource-ref fails fast (before handler) when scene id is missing.
	plugin := &scenePlugin{service: newTestService(t, newFakeStore()), evaluator: allowEvaluator{}}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "leave", CharacterID: "char-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "scene id is required")
}

func TestSceneCommandInviteParsesSceneIDAndTarget(t *testing.T) {
	store := newFakeStore()
	require.NoError(t, store.CreateWithOwner(context.Background(), &SceneRow{
		ID: "scene-cmd-i", OwnerID: "char-alice",
		State: string(SceneStateActive), Visibility: string(SceneVisibilityPrivate),
	}))
	svc := newTestService(t, store)
	svc.SetHostEvaluator(allowEvaluator{})
	plugin := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

	resp, err := plugin.dispatchCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "invite scene-cmd-i char-bob", CharacterID: "char-alice",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "invited", store.participants["scene-cmd-i"]["char-bob"])
}

func TestSceneCommandTransferRejectsMissingTarget(t *testing.T) {
	plugin := &scenePlugin{service: newTestService(t, newFakeStore()), evaluator: allowEvaluator{}}

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
	svc := newTestService(t, store)
	svc.SetHostEvaluator(allowEvaluator{})
	plugin := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

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
	svc := newTestService(t, store)
	svc.SetHostEvaluator(allowEvaluator{})
	plugin := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

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
	svc := newTestService(t, store)
	svc.SetHostEvaluator(allowEvaluator{})
	plugin := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

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
		{"observer is valid", ParticipantRoleObserver, true},
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
			// allowEvaluator lets the gate pass so the handler's arity guard fires.
			plugin := &scenePlugin{service: newTestService(t, newFakeStore()), evaluator: allowEvaluator{}}

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

type setConnFocusCall struct {
	connectionID string
	focusKey     *pluginsdk.FocusKey
	isSceneGrid  bool
}

type fakeFocusClient struct {
	joinCalls            []focusCall
	leaveCalls           []focusCall
	leaveByTargetCalls   []pluginsdk.FocusKey
	presentCalls         []focusCall
	setConnFocusCalls    []setConnFocusCall
	autoFocusOnJoinCalls []autoFocusOnJoinCall

	joinErr               error
	leaveErr              error
	leaveByTargetErr      error
	leaveByTargetResult   pluginsdk.LeaveByTargetResult
	presentErr            error
	setConnFocusErr       error
	autoFocusOnJoinResult pluginsdk.AutoFocusOnJoinResult
	autoFocusOnJoinErr    error

	// isAnyConnFocusedResult maps sceneID → focused bool for per-scene control.
	// If the scene is not in the map, the default is false.
	isAnyConnFocusedResult map[string]bool
	isAnyConnFocusedErr    error

	// getConnFocusResult / getConnFocusErr control GetConnectionFocus.
	// Nil result means grid-focused (no per-connection focus).
	getConnFocusResult *pluginsdk.FocusKey
	getConnFocusErr    error

	// queryHistoryEvents / queryHistoryErr drive QueryStreamHistory (scene log).
	queryHistoryEvents []pluginsdk.Event
	queryHistoryErr    error
}

type autoFocusOnJoinCall struct {
	characterID string
	sceneID     string
}

func (f *fakeFocusClient) JoinFocus(_ context.Context, sid string, t pluginsdk.FocusKey) error {
	f.joinCalls = append(f.joinCalls, focusCall{sessionID: sid, target: t})
	return f.joinErr
}

func (f *fakeFocusClient) LeaveFocus(_ context.Context, sid string, t pluginsdk.FocusKey) error {
	f.leaveCalls = append(f.leaveCalls, focusCall{sessionID: sid, target: t})
	return f.leaveErr
}

func (f *fakeFocusClient) LeaveFocusByTarget(_ context.Context, t pluginsdk.FocusKey) (pluginsdk.LeaveByTargetResult, error) {
	f.leaveByTargetCalls = append(f.leaveByTargetCalls, t)
	return f.leaveByTargetResult, f.leaveByTargetErr
}

func (f *fakeFocusClient) PresentFocus(_ context.Context, sid string, t pluginsdk.FocusKey) error {
	f.presentCalls = append(f.presentCalls, focusCall{sessionID: sid, target: t})
	return f.presentErr
}

func (f *fakeFocusClient) SetConnectionFocus(_ context.Context, connID string, fk *pluginsdk.FocusKey, isSceneGrid bool) error {
	f.setConnFocusCalls = append(f.setConnFocusCalls, setConnFocusCall{connectionID: connID, focusKey: fk, isSceneGrid: isSceneGrid})
	return f.setConnFocusErr
}

func (f *fakeFocusClient) AutoFocusOnJoin(_ context.Context, characterID, sceneID string) (pluginsdk.AutoFocusOnJoinResult, error) {
	f.autoFocusOnJoinCalls = append(f.autoFocusOnJoinCalls, autoFocusOnJoinCall{characterID: characterID, sceneID: sceneID})
	return f.autoFocusOnJoinResult, f.autoFocusOnJoinErr
}

func (f *fakeFocusClient) GetConnectionFocus(_ context.Context, _ string) (*pluginsdk.FocusKey, error) {
	return f.getConnFocusResult, f.getConnFocusErr
}

func (f *fakeFocusClient) IsAnyConnFocused(_ context.Context, _ string, sceneID string) (bool, error) {
	if f.isAnyConnFocusedErr != nil {
		return false, f.isAnyConnFocusedErr
	}
	return f.isAnyConnFocusedResult[sceneID], nil
}

func (f *fakeFocusClient) QueryStreamHistory(_ context.Context, _ pluginsdk.QueryStreamHistoryRequest) (pluginsdk.QueryStreamHistoryResponse, error) {
	if f.queryHistoryErr != nil {
		return pluginsdk.QueryStreamHistoryResponse{}, f.queryHistoryErr
	}
	return pluginsdk.QueryStreamHistoryResponse{Events: f.queryHistoryEvents}, nil
}

// newTestPluginWithFocus returns a scenePlugin wired with a fakeFocusClient
// and a fakeStore-backed SceneServiceImpl. Tests that exercise the
// command-path focus wiring use this in place of newTestPlugin.
func newTestPluginWithFocus(t testing.TB) (*scenePlugin, *fakeFocusClient) {
	t.Helper()
	p := newTestPlugin(t)
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
	p, fc := newTestPluginWithFocus(t)

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
	p, fc := newTestPluginWithFocus(t)

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
	p, fc := newTestPluginWithFocus(t)
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
	p, fc := newTestPluginWithFocus(t)
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
	p, fc := newTestPluginWithFocus(t)

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
	p, fc := newTestPluginWithFocus(t)

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
	p, fc := newTestPluginWithFocus(t)
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

func TestSceneEndCallsLeaveFocusByTargetForFanOut(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)
	fc.leaveByTargetResult = pluginsdk.LeaveByTargetResult{Succeeded: 2, TotalScanned: 2}

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

	// Single fan-out call targets the scene and carries no session ID — the
	// host sweeps every session holding the membership.
	require.Len(t, fc.leaveByTargetCalls, 1)
	assert.Equal(t, pluginsdk.FocusKindScene, fc.leaveByTargetCalls[0].Kind)
	assert.Equal(t, sceneID, fc.leaveByTargetCalls[0].TargetID)

	// Per-session LeaveFocus MUST NOT be called; the sweep subsumes owner + participants.
	assert.Empty(t, fc.leaveCalls, "scene end must fan out via LeaveFocusByTarget, not per-session LeaveFocus")
}

func TestSceneSwitchCallsPresentFocus(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

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
	p, fc := newTestPluginWithFocus(t)
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
	p, _ := newTestPluginWithFocus(t)

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

// --- Group C: focusClient-not-configured branches ---

func TestSceneJoinReturnsErrorWhenFocusClientNotConfigured(t *testing.T) {
	// newTestPlugin(t) has no focusClient wired in.
	p := newTestPlugin(t)

	// Create a scene first so JoinScene succeeds before hitting the nil-client guard.
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
	assert.Contains(t, resp.Output, "focus client not configured")
	assert.Contains(t, resp.Output, "administrator",
		"nil focusClient is a misconfiguration, not transient; message should direct operator, not user-retry")
	assert.NotContains(t, resp.Output, "retry",
		"nil focusClient retries hit the same guard; do not mislead the user")
}

func TestSceneSwitchReturnsErrorWhenFocusClientNotConfigured(t *testing.T) {
	p := newTestPlugin(t)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "switch scene-01",
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "focus client not configured")
}

func TestSceneSwitchReturnsGenericErrorForUnknownFailure(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)
	fc.presentErr = errors.New("unexpected storage error")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "switch scene-01",
		CharacterID: "char-bob",
		SessionID:   "sess-bob",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.True(t, strings.HasPrefix(resp.Output, "Failed to switch scene:"),
		"output should start with 'Failed to switch scene:'; got: %q", resp.Output)
}

func TestSceneEndToleratesLeaveFocusByTargetEnumerationFailure(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	// Enumeration failed entirely: host could not list members (e.g., store down).
	fc.leaveByTargetErr = errors.New("store down")

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "end " + sceneID,
		CharacterID: "char-owner",
		SessionID:   "sess-owner",
	})
	require.NoError(t, err)
	// DB write succeeded; focus error is logged, not surfaced.
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "ended")
}

func TestSceneEndToleratesLeaveFocusByTargetPartialFailure(t *testing.T) {
	p, fc := newTestPluginWithFocus(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	// Partial sweep: 2 succeeded, 1 failed. Result returns nil err.
	fc.leaveByTargetResult = pluginsdk.LeaveByTargetResult{
		Succeeded:    2,
		TotalScanned: 3,
		Failed:       []pluginsdk.FailedLeave{{SessionID: "sess-bad"}},
	}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "end " + sceneID,
		CharacterID: "char-owner",
		SessionID:   "sess-owner",
	})
	require.NoError(t, err)
	// DB write succeeded; partial focus failure is logged, not surfaced.
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "ended")
}

func TestSceneEndReturnsOKWhenFocusClientNotConfigured(t *testing.T) {
	// handleEnd skips LeaveFocus when focusClient is nil and returns OK.
	p := newTestPlugin(t)

	createResp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create The Gate", CharacterID: "char-owner",
	})
	require.NoError(t, err)
	sceneID := extractSceneID(t, createResp.Output)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scene",
		Args:        "end " + sceneID,
		CharacterID: "char-owner",
		SessionID:   "sess-owner",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "ended")
}

// --- scene publish vote extend ABAC gate tests (E2) ---

// denyEvaluator is a test HostEvaluator that always denies.
type denyEvaluator struct{}

func (denyEvaluator) Evaluate(_ context.Context, _, _ string) (pluginsdk.EvaluateDecision, error) {
	return pluginsdk.EvaluateDecision{Allowed: false, Reason: "not permitted"}, nil
}

// allowEvaluator is a test HostEvaluator that always allows.
type allowEvaluator struct{}

func (allowEvaluator) Evaluate(_ context.Context, _, _ string) (pluginsdk.EvaluateDecision, error) {
	return pluginsdk.EvaluateDecision{Allowed: true}, nil
}

// errorEvaluator is a test HostEvaluator that always returns an engine error.
type errorEvaluator struct{}

func (errorEvaluator) Evaluate(_ context.Context, _, _ string) (pluginsdk.EvaluateDecision, error) {
	return pluginsdk.EvaluateDecision{}, fmt.Errorf("simulated engine failure")
}

// newScenePluginWithEvaluator builds a minimal scenePlugin wired with the
// given HostEvaluator, ready for extend gate tests. Propagates ev to the
// service so service-level gates (EndScene/PauseScene/ResumeScene) honour it.
func newScenePluginWithEvaluator(t *testing.T, ev pluginsdk.HostEvaluator) *scenePlugin {
	t.Helper()
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	svc.SetHostEvaluator(ev)
	return &scenePlugin{
		service:   svc,
		evaluator: ev,
	}
}

// newVoteExtendFixture seeds a scenePlugin with a scene and an initial attempt
// budget, ready for handleVoteExtend tests. Returns plugin, scene id, and the
// underlying fakeStore for budget inspection.
func newVoteExtendFixture(t *testing.T, ev pluginsdk.HostEvaluator) (*scenePlugin, string, *fakeStore) {
	t.Helper()
	store := newFakeStore()
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	sceneID := createSceneInPlugin(t, &scenePlugin{service: svc, evaluator: allowEvaluator{}}, "char-admin", "Test Scene")
	store.maxPublishAttempts[sceneID] = 3
	return &scenePlugin{service: svc, evaluator: ev}, sceneID, store
}

// createSceneInPlugin is a helper that creates a scene via HandleCommand and
// extracts the scene ID from the response. It uses an allow evaluator for
// the create step regardless of the provided plugin's evaluator.
func createSceneInPlugin(t *testing.T, p *scenePlugin, charID, title string) string {
	t.Helper()
	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "create " + title, CharacterID: charID,
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status)
	return extractSceneID(t, resp.Output)
}

// TestVoteExtendAdminSucceedsAndBumpsBudget verifies the E2 happy path:
// an admin caller with an allow evaluator bumps the budget and receives
// a success response containing the new max.
func TestVoteExtendAdminSucceedsAndBumpsBudget(t *testing.T) {
	p, sceneID, store := newVoteExtendFixture(t, allowEvaluator{})

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "publish vote extend 2 #" + sceneID, CharacterID: "char-admin",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "5") // 3 + 2 = 5
	assert.Equal(t, 5, store.maxPublishAttempts[sceneID], "fakeStore budget must be bumped")
}

// TestVoteExtendNonAdminDeniedAndRPCNotCalled verifies that a deny evaluator
// prevents the service RPC from being reached: the budget is unchanged.
func TestVoteExtendNonAdminDeniedAndRPCNotCalled(t *testing.T) {
	p, sceneID, store := newVoteExtendFixture(t, denyEvaluator{})

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "publish vote extend 2 #" + sceneID, CharacterID: "char-nonadmin",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Contains(t, resp.Output, "permitted")
	assert.Equal(t, 3, store.maxPublishAttempts[sceneID], "budget MUST NOT change when gate denies")
}

// TestVoteExtendNilEvaluatorFailsClosed verifies that a missing evaluator
// fails closed with CommandError and does not reach the service RPC.
func TestVoteExtendNilEvaluatorFailsClosed(t *testing.T) {
	p, sceneID, store := newVoteExtendFixture(t, nil)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "publish vote extend 2 #" + sceneID, CharacterID: "char-admin",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandError, resp.Status, "nil evaluator MUST fail closed")
	assert.Contains(t, resp.Output, "unavailable")
	assert.Equal(t, 3, store.maxPublishAttempts[sceneID], "budget MUST NOT change when evaluator absent")
}

// TestVoteExtendBadCountRejectsWithUsage verifies that a non-integer or
// zero count returns a usage error before any gate or RPC.
func TestVoteExtendBadCountRejectsWithUsage(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{"missing count", ""},
		{"non-integer", "abc"},
		{"zero count", "0"},
		{"negative count", "-3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newScenePluginWithEvaluator(t, allowEvaluator{})
			sceneID := createSceneInTest(t, p, "char-admin", "T")
			args := "publish vote extend"
			if tc.args != "" {
				args += " " + tc.args + " #" + sceneID
			}
			resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command: "scene", Args: args, CharacterID: "char-admin",
			})
			require.NoError(t, err)
			assert.Equal(t, pluginsdk.CommandError, resp.Status)
			assert.Contains(t, resp.Output, "Usage:")
		})
	}
}

// TestVoteExtendEngineErrorReturnsCommandFailure verifies that an evaluator
// returning an error produces CommandFailure (not CommandError or a panic).
func TestVoteExtendEngineErrorReturnsCommandFailure(t *testing.T) {
	errEv := errorEvaluator{}
	p, sceneID, store := newVoteExtendFixture(t, errEv)

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "scene", Args: "publish vote extend 1 #" + sceneID, CharacterID: "char-admin",
	})
	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandFailure, resp.Status)
	assert.Equal(t, 3, store.maxPublishAttempts[sceneID], "budget MUST NOT change on engine error")
}

// TestSceneGatedSubcommands_DenyWhenPolicyDenies is the INV-SCENE-59 backstop: every
// subcommand that carries an action gate MUST deny via the evaluator, not via
// a Go-level owner/participant check that bypasses the policy engine.
// Each row uses args that are structurally valid (so arity guards pass) and
// a denyEvaluator that always returns Allowed=false. The expected result is
// CommandError produced by the engine gate.
func TestSceneGatedSubcommands_DenyWhenPolicyDenies(t *testing.T) {
	cases := []struct {
		sub    string
		action string
		// args is the arg string after the subcommand name. For invite/kick/transfer
		// the scene ID must be the FIRST token, second token is target character.
		makeArgs func(sceneID string) string
	}{
		{"end", "end", func(id string) string { return id }},
		{"pause", "pause", func(id string) string { return id }},
		{"resume", "resume", func(id string) string { return id }},
		{"set", "update", func(id string) string { return id + " title=Foo" }},
		{"invite", "invite", func(id string) string { return id + " char-target" }},
		{"kick", "kick", func(id string) string { return id + " char-target" }},
		{"transfer", "transfer-ownership", func(id string) string { return id + " char-target" }},
		{"leave", "leave", func(id string) string { return id }},
		{"info", "read", func(id string) string { return id }},
	}
	for _, tc := range cases {
		t.Run(tc.sub, func(t *testing.T) {
			p := newScenePluginWithEvaluator(t, denyEvaluator{})
			sceneID := createSceneInTest(t, p, "char-alice", "T")
			resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "scene",
				Args:        tc.sub + " " + tc.makeArgs(sceneID),
				CharacterID: "char-bob",
			})
			require.NoError(t, err)
			assert.Equal(t, pluginsdk.CommandError, resp.Status,
				"subcommand %q with action %q must deny via engine gate", tc.sub, tc.action)
		})
	}
}

// TestScenesBoardCommandDeclaresBrowseCapabilityGate is the INV-SCENE-59 backstop for
// the top-level `scenes` board command. Unlike the `scene` subcommands (covered
// by the deny/nil-evaluator tables above), handleScenesBoard is intentionally
// NOT gated by the plugin evaluator — the public board returns only open scenes
// any character may browse, so its authorization is the host dispatcher's
// Layer-2 capability pre-flight (CanPerformAction, dispatcher.go) on the
// capability the command DECLARES in the manifest, not a plugin-code check.
// Calling p.HandleCommand directly (as the unit/integration board tests do)
// bypasses that pre-flight, so the board is never exercised against a deny
// engine there. The meaningful regression guard is therefore that the
// capability declaration exists and is the DISTINCT browse action: removing it,
// or widening it to the membership-gated `read`, would silently change the
// board's authorization. This pins the gate's declaration (holomush-sl0ir.15).
func TestScenesBoardCommandDeclaresBrowseCapabilityGate(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("plugin.yaml")
	require.NoError(t, err)

	var manifest struct {
		Actions  []string `yaml:"actions"`
		Commands []struct {
			Name         string `yaml:"name"`
			Capabilities []struct {
				Action   string `yaml:"action"`
				Resource string `yaml:"resource"`
				Scope    string `yaml:"scope"`
			} `yaml:"capabilities"`
		} `yaml:"commands"`
	}
	require.NoError(t, yaml.Unmarshal(data, &manifest))

	var found bool
	for _, cmd := range manifest.Commands {
		if cmd.Name != "scenes" {
			continue
		}
		found = true
		require.Len(t, cmd.Capabilities, 1,
			"the `scenes` board command must declare exactly one capability (the host pre-flight gate)")
		c := cmd.Capabilities[0]
		assert.Equal(t, "browse", c.Action,
			"board must use the DISTINCT browse action, not the membership-gated read")
		assert.Equal(t, "scene", c.Resource)
		assert.Equal(t, "global", c.Scope)
	}
	require.True(t, found, "the `scenes` board command must be declared in the manifest")

	// The policy that PERMITS the browse capability must also ship; without it
	// the declared gate would deny every board browse.
	assert.Contains(t, string(data), "browse-open-scenes-board",
		"the policy permitting the board browse capability must be declared")

	// The browse action MUST ALSO be registered in the manifest's top-level
	// actions: list. CollectActions admits only core actions + each plugin's
	// declared actions to knownActions; an action used solely in a capability
	// (or policy) but absent from actions: is rejected at plugin load with
	// `unknown action "browse"`, which panics the whole-system integration
	// harness and stops the core server from booting. This pins the missing
	// link that broke CI: browse was declared on the capability + policy but
	// not registered here.
	assert.Contains(t, manifest.Actions, "browse",
		"browse must be declared in the manifest actions: list so CollectActions admits it to knownActions at plugin load")
}

// TestSceneResourceRefTokenizesFirstField verifies that sceneResourceRef extracts
// only the first whitespace-separated token, so multi-token input like
// "scene-x extra" produces "scene:scene-x" rather than "scene:scene-x extra".
// A mis-parsed multi-token resource ref would cause a spurious ABAC gate
// denial before the handler's arity validation fires.
func TestSceneResourceRefTokenizesFirstField(t *testing.T) {
	tests := []struct {
		name    string
		args    string
		wantRef string
		wantErr bool
	}{
		{
			name:    "single token produces scene:<id>",
			args:    "scene-abc",
			wantRef: "scene:scene-abc",
		},
		{
			name:    "multi-token uses first token only",
			args:    "scene-abc extra",
			wantRef: "scene:scene-abc",
		},
		{
			name:    "leading whitespace is ignored",
			args:    "  scene-abc",
			wantRef: "scene:scene-abc",
		},
		{
			name:    "empty args returns error",
			args:    "",
			wantErr: true,
		},
		{
			name:    "whitespace-only args returns error",
			args:    "   ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := sceneResourceRef(tt.args)
			if tt.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantRef, got)
		})
	}
}

// TestSceneGatedSubcommands_NilEvaluatorFailsClosed verifies that every newly
// gated subcommand fails closed (CommandError) when no evaluator is wired,
// rather than running the handler ungated.
func TestSceneGatedSubcommands_NilEvaluatorFailsClosed(t *testing.T) {
	cases := []struct {
		sub      string
		makeArgs func(sceneID string) string
	}{
		{"end", func(id string) string { return id }},
		{"pause", func(id string) string { return id }},
		{"resume", func(id string) string { return id }},
		{"set", func(id string) string { return id + " title=Foo" }},
		{"invite", func(id string) string { return id + " char-target" }},
		{"kick", func(id string) string { return id + " char-target" }},
		{"transfer", func(id string) string { return id + " char-target" }},
		{"leave", func(id string) string { return id }},
		{"info", func(id string) string { return id }},
	}
	for _, tc := range cases {
		t.Run(tc.sub, func(t *testing.T) {
			p := newScenePluginWithEvaluator(t, nil)
			sceneID := createSceneInTest(t, p, "char-alice", "T")
			resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:     "scene",
				Args:        tc.sub + " " + tc.makeArgs(sceneID),
				CharacterID: "char-alice",
			})
			require.NoError(t, err)
			assert.Equal(t, pluginsdk.CommandError, resp.Status,
				"nil evaluator MUST fail closed for subcommand %q", tc.sub)
		})
	}
}

// ── scenes board command tests (iokti.14) ─────────────────────────────────────

// TestScenesBoardRendersOpenScenesWithCWLabels asserts that handleScenesBoard
// renders a scene's content_warnings in the output line so players can
// identify CW-tagged scenes on the board.
func TestScenesBoardRendersOpenScenesWithCWLabels(t *testing.T) {
	store := newFakeStore()
	store.listBoardRows = []*SceneRow{
		{
			ID:              "scene-cw-1",
			Title:           "Dark Alley",
			OwnerID:         "char-owner",
			State:           string(SceneStateActive),
			Visibility:      "open",
			PoseOrder:       string(PoseOrderModeFree),
			ContentWarnings: []string{"violence"},
			Tags:            []string{},
		},
	}
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	p := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scenes",
		Args:        "",
		CharacterID: "char-alice",
		PlayerID:    "player-alice",
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "violence", "CW label must appear in output")
	assert.Contains(t, resp.Output, "Dark Alley", "scene title must appear in output")
}

// TestScenesBoardParsesHideArgExcludesMatchingScene asserts that a hide:<cw>
// argument is forwarded as ExcludeContentWarnings to ListScenes, causing the
// board to omit scenes carrying that warning (verified via the BoardQuery
// recorded by the fake store).
func TestScenesBoardParsesHideArgExcludesMatchingScene(t *testing.T) {
	store := newFakeStore()
	// The fake store returns nothing — we care about the query shape.
	store.listBoardRows = []*SceneRow{}
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	p := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scenes",
		Args:        "hide:violence",
		CharacterID: "char-alice",
		PlayerID:    "player-alice",
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status)

	require.NotNil(t, store.listBoardGot, "ListBoard must have been called")
	assert.Contains(t, store.listBoardGot.BlockedCW, "violence",
		"hide:violence must be forwarded to BoardQuery.BlockedCW")
}

// TestScenesBoardParsesTagArgFiltersBoard asserts that a tag:<t> argument is
// forwarded as Tags to the BoardQuery so the board is filtered by tag.
func TestScenesBoardParsesTagArgFiltersBoard(t *testing.T) {
	store := newFakeStore()
	store.listBoardRows = []*SceneRow{}
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	p := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scenes",
		Args:        "tag:action",
		CharacterID: "char-alice",
		PlayerID:    "player-alice",
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status)

	require.NotNil(t, store.listBoardGot, "ListBoard must have been called")
	assert.Contains(t, store.listBoardGot.Tags, "action",
		"tag:action must be forwarded to BoardQuery.Tags")
}

// TestScenesBoardEmptyBoardRendersNoScenesMessage asserts that when the board
// has no open scenes, the output contains an appropriate "no open scenes"
// message rather than a blank or partial output.
func TestScenesBoardEmptyBoardRendersNoScenesMessage(t *testing.T) {
	store := newFakeStore()
	store.listBoardRows = []*SceneRow{}
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	p := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scenes",
		Args:        "",
		CharacterID: "char-alice",
		PlayerID:    "player-alice",
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "No open scenes")
}

// TestScenesBoardRendersPausedMarkerWhenSceneIsPaused asserts that a paused
// scene displays a [paused] marker in the board listing.
func TestScenesBoardRendersPausedMarkerWhenSceneIsPaused(t *testing.T) {
	store := newFakeStore()
	store.listBoardRows = []*SceneRow{
		{
			ID:              "scene-paused-1",
			Title:           "Quiet Meadow",
			OwnerID:         "char-owner",
			State:           string(SceneStatePaused),
			Visibility:      "open",
			PoseOrder:       string(PoseOrderModeFree),
			ContentWarnings: []string{},
			Tags:            []string{},
		},
	}
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	p := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scenes",
		Args:        "",
		CharacterID: "char-alice",
		PlayerID:    "player-alice",
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "[paused]", "paused scene must show [paused] marker")
}

// TestScenesBoardUsesAuthenticatedIdentityForCWBlocking asserts that the
// PlayerID and CharacterID from the authenticated CommandRequest are forwarded
// to ListScenes — not from parsed args — so the CW block settings read is
// gated by the dispatch-token-owned principal (iokti.14 safety invariant).
func TestScenesBoardUsesAuthenticatedIdentityForCWBlocking(t *testing.T) {
	store := newFakeStore()
	store.listBoardRows = []*SceneRow{}
	svc := newTestService(t, store)
	svc.SetEventSink(&recordingEventSink{})
	// Wire a settings client that returns a block for "char-alice" at character scope.
	svc.settings = &scopedFakeSettingsClient{
		byScope: map[pluginsdk.SettingScope]scopedFakeOutcome{
			pluginsdk.SettingScopeCharacter: {values: []string{"gore"}, found: true},
		},
	}
	p := &scenePlugin{service: svc, evaluator: allowEvaluator{}}

	resp, err := p.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:     "scenes",
		Args:        "",
		CharacterID: "char-alice",
		PlayerID:    "player-alice",
	})
	require.NoError(t, err)
	require.Equal(t, pluginsdk.CommandOK, resp.Status)

	// The CW block must have been applied via the authenticated identity.
	require.NotNil(t, store.listBoardGot, "ListBoard must have been called")
	assert.Contains(t, store.listBoardGot.BlockedCW, "gore",
		"character-scope CW block must be resolved and forwarded to BoardQuery")
}
