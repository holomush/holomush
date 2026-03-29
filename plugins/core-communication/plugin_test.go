// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package communication

import (
	"context"
	"encoding/json"
	"testing"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProxy is a configurable mock ServiceProxy for testing handlers.
type testProxy struct {
	sessions        map[string]*plugins.SessionResult // keyed by lowercase name
	lastWhispered   map[string]string                 // session ID -> name
	activeSessions  []plugins.SessionResult
	broadcastedMsgs []string
	logs            []string
	broadcastErr    error
	findSessionErr  error
	listSessionsErr error
	setWhisperedErr error
}

func newTestProxy() *testProxy {
	return &testProxy{
		sessions:      make(map[string]*plugins.SessionResult),
		lastWhispered: make(map[string]string),
	}
}

func (p *testProxy) addSession(name string, s *plugins.SessionResult) {
	p.sessions[name] = s
}

// --- ServiceProxy implementation (only methods used by communication handlers) ---

func (p *testProxy) FindSessionByName(_ context.Context, name string) (*plugins.SessionResult, error) {
	if p.findSessionErr != nil {
		return nil, p.findSessionErr
	}
	return p.sessions[name], nil
}

func (p *testProxy) SetLastWhispered(_ context.Context, sessionID, name string) error {
	if p.setWhisperedErr != nil {
		return p.setWhisperedErr
	}
	p.lastWhispered[sessionID] = name
	return nil
}

func (p *testProxy) ListActiveSessions(_ context.Context) ([]plugins.SessionResult, error) {
	if p.listSessionsErr != nil {
		return nil, p.listSessionsErr
	}
	return p.activeSessions, nil
}

func (p *testProxy) BroadcastSystemMessage(_ context.Context, msg string) error {
	if p.broadcastErr != nil {
		return p.broadcastErr
	}
	p.broadcastedMsgs = append(p.broadcastedMsgs, msg)
	return nil
}

func (p *testProxy) Log(_ context.Context, _, msg string) {
	p.logs = append(p.logs, msg)
}

// Unused stubs.
func (p *testProxy) QueryLocation(context.Context, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (p *testProxy) QueryCharacter(context.Context, string, string) (*plugins.CharacterResult, error) {
	return nil, nil
}

func (p *testProxy) QueryLocationCharacters(context.Context, string, string) ([]plugins.CharacterResult, error) {
	return nil, nil
}

func (p *testProxy) QueryObject(context.Context, string, string) (*plugins.ObjectResult, error) {
	return nil, nil
}

func (p *testProxy) FindLocation(context.Context, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (p *testProxy) GetCharactersByLocation(context.Context, string, string) ([]plugins.CharacterResult, error) {
	return nil, nil
}

func (p *testProxy) GetObjectsByLocation(context.Context, string, string) ([]plugins.ObjectResult, error) {
	return nil, nil
}

func (p *testProxy) CreateLocation(context.Context, string, string, string, string) (*plugins.LocationResult, error) {
	return nil, nil
}

func (p *testProxy) CreateExit(context.Context, string, string, string, string, plugins.CreateExitOpts) error {
	return nil
}

func (p *testProxy) CreateObject(context.Context, string, string, string) (*plugins.ObjectResult, error) {
	return nil, nil
}

func (p *testProxy) UpdateLocation(context.Context, string, string, string, string) error {
	return nil
}

func (p *testProxy) UpdateCharacterDescription(context.Context, string, string, string) error {
	return nil
}

func (p *testProxy) SetProperty(context.Context, string, string, string, string, string) error {
	return nil
}

func (p *testProxy) GetProperty(context.Context, string, string, string, string) (string, error) {
	return "", nil
}

func (p *testProxy) FindPropertyByPrefix(context.Context, string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}

func (p *testProxy) ListPropertiesByParent(context.Context, string, string, string) ([]plugins.PropertyInfo, error) {
	return nil, nil
}

func (p *testProxy) KVGet(context.Context, string, string) (string, bool, error) {
	return "", false, nil
}
func (p *testProxy) KVSet(context.Context, string, string, string) error     { return nil }
func (p *testProxy) KVDelete(context.Context, string, string) error          { return nil }
func (p *testProxy) DisconnectSession(context.Context, string, string) error { return nil }
func (p *testProxy) UpdateActivity(context.Context, string) error            { return nil }
func (p *testProxy) SetPlayerAlias(context.Context, string, string, string) error {
	return nil
}
func (p *testProxy) DeletePlayerAlias(context.Context, string, string) error { return nil }
func (p *testProxy) ListPlayerAliases(context.Context, string) ([]plugins.AliasEntry, error) {
	return nil, nil
}

func (p *testProxy) SetSystemAlias(context.Context, string, string, string) error {
	return nil
}
func (p *testProxy) DeleteSystemAlias(context.Context, string) error { return nil }
func (p *testProxy) ListSystemAliases(context.Context) ([]plugins.AliasEntry, error) {
	return nil, nil
}

func (p *testProxy) CheckAliasShadow(context.Context, string) (bool, string, error) {
	return false, "", nil
}

func (p *testProxy) ListCommands(context.Context, string) ([]plugins.CommandInfo, error) {
	return nil, nil
}

func (p *testProxy) GetCommandHelp(context.Context, string, string) (*plugins.CommandHelpInfo, error) {
	return nil, nil
}
func (p *testProxy) EmitEvent(context.Context, string, string, []byte) error { return nil }
func (p *testProxy) GetStartingLocationID(context.Context) (string, error)   { return "", nil }

var _ plugins.ServiceProxy = (*testProxy)(nil)

// --- Tests ---

func TestSayHandler(t *testing.T) {
	h := &SayHandler{}
	proxy := newTestProxy()
	ctx := context.Background()

	resp, err := h.HandleCommand(ctx, pluginsdk.CommandRequest{
		Command:       "say",
		Args:          "Hello everyone!",
		CharacterID:   "char-1",
		CharacterName: "Sean",
		LocationID:    "loc-1",
	}, proxy)

	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, "location:loc-1", resp.Events[0].Stream)
	assert.Equal(t, pluginsdk.EventTypeSay, resp.Events[0].Type)

	var p sayPayload
	require.NoError(t, json.Unmarshal([]byte(resp.Events[0].Payload), &p))
	assert.Equal(t, "Sean", p.CharacterName)
	assert.Equal(t, "Hello everyone!", p.Message)
}

func TestPoseHandler(t *testing.T) {
	tests := []struct {
		name      string
		invokedAs string
		args      string
		wantSpace bool
	}{
		{"normal pose", "pose", "waves hello", false},
		{"semipose", ";", "'s jaw drops", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &PoseHandler{}
			proxy := newTestProxy()

			resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:       "pose",
				Args:          tt.args,
				CharacterName: "Sean",
				LocationID:    "loc-1",
				InvokedAs:     tt.invokedAs,
			}, proxy)

			require.NoError(t, err)
			require.Len(t, resp.Events, 1)
			assert.Equal(t, "location:loc-1", resp.Events[0].Stream)

			var p posePayload
			require.NoError(t, json.Unmarshal([]byte(resp.Events[0].Payload), &p))
			assert.Equal(t, tt.args, p.Action)
			assert.Equal(t, tt.wantSpace, p.NoSpace)
		})
	}
}

func TestOOCHandler(t *testing.T) {
	tests := []struct {
		name      string
		args      string
		wantStyle string
		wantText  string
	}{
		{"say style", "brb", "say", "brb"},
		{"pose style", ":laughs", "pose", "laughs"},
		{"semipose style", ";'s phone rings", "semipose", "'s phone rings"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &OOCHandler{}
			proxy := newTestProxy()

			resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:       "ooc",
				Args:          tt.args,
				CharacterName: "Sean",
				LocationID:    "loc-1",
			}, proxy)

			require.NoError(t, err)
			require.Len(t, resp.Events, 1)

			var p oocPayload
			require.NoError(t, json.Unmarshal([]byte(resp.Events[0].Payload), &p))
			assert.Equal(t, tt.wantStyle, p.Style)
			assert.Equal(t, tt.wantText, p.Message)
		})
	}
}

func TestOOCHandler_EmptyArgs(t *testing.T) {
	h := &OOCHandler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "ooc",
		Args:    "",
	}, newTestProxy())

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage")
	assert.Empty(t, resp.Events)
}

func TestPageHandler_NormalPage(t *testing.T) {
	h := &PageHandler{}
	proxy := newTestProxy()
	proxy.addSession("Alex", &plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
		LocationID:    "loc-1",
	})

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "page",
		Args:          "Alex=Hey there!",
		CharacterID:   "char-1",
		CharacterName: "Sean",
		LocationID:    "loc-1",
		SessionID:     "sess-1",
	}, proxy)

	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, "character:char-2", resp.Events[0].Stream)
	assert.Contains(t, resp.Output, "You paged Alex")
	assert.Equal(t, "Alex", proxy.lastWhispered["sess-1"])

	var p pagePayload
	require.NoError(t, json.Unmarshal([]byte(resp.Events[0].Payload), &p))
	assert.Equal(t, "Sean pages: Hey there!", p.Message)
	assert.False(t, p.IsPose)
}

func TestPageHandler_PosePage(t *testing.T) {
	h := &PageHandler{}
	proxy := newTestProxy()
	proxy.addSession("Alex", &plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
	})

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "page",
		Args:          "Alex=:waves hello.",
		CharacterID:   "char-1",
		CharacterName: "Sean",
		SessionID:     "sess-1",
	}, proxy)

	require.NoError(t, err)

	var p pagePayload
	require.NoError(t, json.Unmarshal([]byte(resp.Events[0].Payload), &p))
	assert.True(t, p.IsPose)
	assert.Contains(t, p.Message, "From afar, Sean waves hello.")
}

func TestPageHandler_LastPaged(t *testing.T) {
	h := &PageHandler{}
	proxy := newTestProxy()
	proxy.addSession("Sean", &plugins.SessionResult{
		ID:            "sess-1",
		CharacterName: "Sean",
		LastWhispered: "Alex",
	})
	proxy.addSession("Alex", &plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
	})

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "page",
		Args:          "How's it going?",
		CharacterID:   "char-1",
		CharacterName: "Sean",
		SessionID:     "sess-1",
	}, proxy)

	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Contains(t, resp.Output, "You paged Alex")
}

func TestPageHandler_NotFound(t *testing.T) {
	h := &PageHandler{}
	proxy := newTestProxy()

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "page",
		Args:    "Nobody=hello",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No one named")
	assert.Empty(t, resp.Events)
}

func TestPageHandler_EmptyArgs(t *testing.T) {
	h := &PageHandler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "page",
		Args:    "",
	}, newTestProxy())

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage")
}

func TestWhisperHandler_NormalWhisper(t *testing.T) {
	h := &WhisperHandler{}
	proxy := newTestProxy()
	proxy.addSession("Alex", &plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
		LocationID:    "loc-1",
	})

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "whisper",
		Args:          "Alex=Let's go",
		CharacterID:   "char-1",
		CharacterName: "Sean",
		LocationID:    "loc-1",
		SessionID:     "sess-1",
	}, proxy)

	require.NoError(t, err)
	require.Len(t, resp.Events, 2)

	// First event: location notice
	assert.Equal(t, "location:loc-1", resp.Events[0].Stream)
	assert.Equal(t, pluginsdk.EventType("whisper_notice"), resp.Events[0].Type)

	// Second event: whisper to target
	assert.Equal(t, "character:char-2", resp.Events[1].Stream)
	assert.Equal(t, pluginsdk.EventType("whisper"), resp.Events[1].Type)

	var wp whisperPayload
	require.NoError(t, json.Unmarshal([]byte(resp.Events[1].Payload), &wp))
	assert.Contains(t, wp.Message, `Sean whispers, "Let's go"`)

	assert.Contains(t, resp.Output, "You whisper to Alex")
	assert.Equal(t, "Alex", proxy.lastWhispered["sess-1"])
}

func TestWhisperHandler_PoseWhisper(t *testing.T) {
	h := &WhisperHandler{}
	proxy := newTestProxy()
	proxy.addSession("Alex", &plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
		LocationID:    "loc-1",
	})

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "whisper",
		Args:          "Alex=:nods meaningfully.",
		CharacterName: "Sean",
		LocationID:    "loc-1",
		SessionID:     "sess-1",
	}, proxy)

	require.NoError(t, err)

	var wp whisperPayload
	require.NoError(t, json.Unmarshal([]byte(resp.Events[1].Payload), &wp))
	assert.True(t, wp.IsPose)
	assert.Contains(t, wp.Message, "From nearby, Sean nods meaningfully.")
}

func TestWhisperHandler_DifferentLocation(t *testing.T) {
	h := &WhisperHandler{}
	proxy := newTestProxy()
	proxy.addSession("Alex", &plugins.SessionResult{
		CharacterName: "Alex",
		LocationID:    "loc-2", // different location
	})

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "whisper",
		Args:          "Alex=hello",
		CharacterName: "Sean",
		LocationID:    "loc-1",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "don't see anyone")
	assert.Empty(t, resp.Events)
}

func TestWhisperHandler_ShortForm(t *testing.T) {
	h := &WhisperHandler{}
	proxy := newTestProxy()
	proxy.addSession("Sean", &plugins.SessionResult{
		CharacterName: "Sean",
		LastWhispered: "Alex",
	})
	proxy.addSession("Alex", &plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
		LocationID:    "loc-1",
	})

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "w",
		Args:          "quick message",
		CharacterName: "Sean",
		LocationID:    "loc-1",
		SessionID:     "sess-1",
		InvokedAs:     "w",
	}, proxy)

	require.NoError(t, err)
	require.Len(t, resp.Events, 2)
	assert.Contains(t, resp.Output, "You whisper to Alex")
}

func TestPemitHandler_Normal(t *testing.T) {
	h := &PemitHandler{}
	proxy := newTestProxy()
	proxy.addSession("Alex", &plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
	})

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "pemit",
		Args:          "Alex=You feel a chill.",
		CharacterID:   "char-1",
		CharacterName: "Sean",
	}, proxy)

	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, "character:char-2", resp.Events[0].Stream)
	assert.Contains(t, resp.Output, "Pemit sent to Alex")

	var p pemitPayload
	require.NoError(t, json.Unmarshal([]byte(resp.Events[0].Payload), &p))
	assert.Equal(t, "You feel a chill.", p.Message)
	assert.Equal(t, "char-2", p.TargetID)
}

func TestPemitHandler_NotFound(t *testing.T) {
	h := &PemitHandler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "pemit",
		Args:    "Nobody=hello",
	}, newTestProxy())

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "No character found")
	assert.Empty(t, resp.Events)
}

func TestPemitHandler_BadSyntax(t *testing.T) {
	h := &PemitHandler{}

	tests := []struct {
		name string
		args string
	}{
		{"no equals", "just some text"},
		{"empty message", "Alex="},
		{"empty args", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command: "pemit",
				Args:    tt.args,
			}, newTestProxy())

			require.NoError(t, err)
			assert.Contains(t, resp.Output, "Usage")
		})
	}
}

func TestEmitHandler_Normal(t *testing.T) {
	h := &EmitHandler{}
	proxy := newTestProxy()

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:    "emit",
		Args:       "The ground shakes violently.",
		LocationID: "loc-1",
	}, proxy)

	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, "location:loc-1", resp.Events[0].Stream)
	assert.Equal(t, pluginsdk.EventType("emit"), resp.Events[0].Type)

	var p emitPayload
	require.NoError(t, json.Unmarshal([]byte(resp.Events[0].Payload), &p))
	assert.Equal(t, "The ground shakes violently.", p.Message)
}

func TestEmitHandler_Empty(t *testing.T) {
	h := &EmitHandler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "emit",
		Args:    "",
	}, newTestProxy())

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "What do you want to emit?")
	assert.Empty(t, resp.Events)
}

func TestWallHandler_Normal(t *testing.T) {
	h := &WallHandler{}
	proxy := newTestProxy()
	proxy.activeSessions = []plugins.SessionResult{
		{ID: "sess-1", CharacterID: "char-1"},
		{ID: "sess-2", CharacterID: "char-2"},
	}

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "wall",
		Args:          "Server restart in 10 minutes",
		CharacterName: "Admin",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "2 sessions")
	require.Len(t, proxy.broadcastedMsgs, 1)
	assert.Contains(t, proxy.broadcastedMsgs[0], "[ADMIN ANNOUNCEMENT]")
	assert.Contains(t, proxy.broadcastedMsgs[0], "Server restart in 10 minutes")
}

func TestWallHandler_WithUrgency(t *testing.T) {
	tests := []struct {
		name       string
		args       string
		wantPrefix string
	}{
		{"info", "info Hello", "[ADMIN ANNOUNCEMENT]"},
		{"warning", "warning Maintenance soon", "[ADMIN WARNING]"},
		{"critical", "critical Emergency", "[ADMIN CRITICAL]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &WallHandler{}
			proxy := newTestProxy()
			proxy.activeSessions = []plugins.SessionResult{{ID: "sess-1"}}

			resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:       "wall",
				Args:          tt.args,
				CharacterName: "Admin",
			}, proxy)

			require.NoError(t, err)
			assert.Contains(t, resp.Output, "1 session")
			require.Len(t, proxy.broadcastedMsgs, 1)
			assert.Contains(t, proxy.broadcastedMsgs[0], tt.wantPrefix)
		})
	}
}

func TestWallHandler_EmptyArgs(t *testing.T) {
	h := &WallHandler{}
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "wall",
		Args:    "",
	}, newTestProxy())

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage")
}

func TestNewHandler_Routes(t *testing.T) {
	h := NewHandler()
	proxy := newTestProxy()

	// Verify say routes correctly.
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "say",
		Args:          "Hello",
		CharacterName: "Sean",
		LocationID:    "loc-1",
	}, proxy)

	require.NoError(t, err)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, pluginsdk.EventTypeSay, resp.Events[0].Type)
}

func TestNewHandler_UnknownCommand(t *testing.T) {
	h := NewHandler()
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "nonexistent",
	}, newTestProxy())

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Unknown communication command")
}
