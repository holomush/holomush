// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package communication

import (
	"context"
	"encoding/json"
	"testing"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginmocks "github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Tests ---

func TestSayHandler(t *testing.T) {
	h := &SayHandler{}
	proxy := pluginmocks.NewMockServiceProxy(t)
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
			proxy := pluginmocks.NewMockServiceProxy(t)

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
			proxy := pluginmocks.NewMockServiceProxy(t)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "ooc",
		Args:    "",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage")
	assert.Empty(t, resp.Events)
}

func TestPageHandler_NormalPage(t *testing.T) {
	h := &PageHandler{}
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Alex").Return(&plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
		LocationID:    "loc-1",
	}, nil)
	proxy.On("SetLastWhispered", mock.Anything, "sess-1", "Alex").Return(nil)

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

	var p pagePayload
	require.NoError(t, json.Unmarshal([]byte(resp.Events[0].Payload), &p))
	assert.Equal(t, "Sean pages: Hey there!", p.Message)
	assert.False(t, p.IsPose)
}

func TestPageHandler_PosePage(t *testing.T) {
	h := &PageHandler{}
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Alex").Return(&plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
	}, nil)
	proxy.On("SetLastWhispered", mock.Anything, "sess-1", "Alex").Return(nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Sean").Return(&plugins.SessionResult{
		ID:            "sess-1",
		CharacterName: "Sean",
		LastWhispered: "Alex",
	}, nil)
	proxy.On("FindSessionByName", mock.Anything, "Alex").Return(&plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
	}, nil)
	proxy.On("SetLastWhispered", mock.Anything, "sess-1", "Alex").Return(nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Nobody").Return(nil, nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "page",
		Args:    "",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage")
}

func TestWhisperHandler_NormalWhisper(t *testing.T) {
	h := &WhisperHandler{}
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Alex").Return(&plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
		LocationID:    "loc-1",
	}, nil)
	proxy.On("SetLastWhispered", mock.Anything, "sess-1", "Alex").Return(nil)

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
}

func TestWhisperHandler_PoseWhisper(t *testing.T) {
	h := &WhisperHandler{}
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Alex").Return(&plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
		LocationID:    "loc-1",
	}, nil)
	proxy.On("SetLastWhispered", mock.Anything, "sess-1", "Alex").Return(nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Alex").Return(&plugins.SessionResult{
		CharacterName: "Alex",
		LocationID:    "loc-2", // different location
	}, nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Sean").Return(&plugins.SessionResult{
		CharacterName: "Sean",
		LastWhispered: "Alex",
	}, nil)
	proxy.On("FindSessionByName", mock.Anything, "Alex").Return(&plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
		LocationID:    "loc-1",
	}, nil)
	proxy.On("SetLastWhispered", mock.Anything, "sess-1", "Alex").Return(nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Alex").Return(&plugins.SessionResult{
		ID:            "sess-2",
		CharacterID:   "char-2",
		CharacterName: "Alex",
	}, nil)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("FindSessionByName", mock.Anything, "Nobody").Return(nil, nil)

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "pemit",
		Args:    "Nobody=hello",
	}, proxy)

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
			proxy := pluginmocks.NewMockServiceProxy(t)
			resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command: "pemit",
				Args:    tt.args,
			}, proxy)

			require.NoError(t, err)
			assert.Contains(t, resp.Output, "Usage")
		})
	}
}

func TestEmitHandler_Normal(t *testing.T) {
	h := &EmitHandler{}
	proxy := pluginmocks.NewMockServiceProxy(t)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "emit",
		Args:    "",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "What do you want to emit?")
	assert.Empty(t, resp.Events)
}

func TestWallHandler_Normal(t *testing.T) {
	h := &WallHandler{}
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("ListActiveSessions", mock.Anything).Return([]plugins.SessionResult{
		{ID: "sess-1", CharacterID: "char-1"},
		{ID: "sess-2", CharacterID: "char-2"},
	}, nil)
	proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return()

	var capturedMsg string
	proxy.On("BroadcastSystemMessage", mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) { capturedMsg = args.String(1) }).
		Return(nil)

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command:       "wall",
		Args:          "Server restart in 10 minutes",
		CharacterName: "Admin",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "2 sessions")
	assert.Contains(t, capturedMsg, "[ADMIN ANNOUNCEMENT]")
	assert.Contains(t, capturedMsg, "Server restart in 10 minutes")
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
			proxy := pluginmocks.NewMockServiceProxy(t)
			proxy.On("ListActiveSessions", mock.Anything).Return([]plugins.SessionResult{{ID: "sess-1"}}, nil)
			proxy.On("Log", mock.Anything, mock.Anything, mock.Anything).Return()

			var capturedMsg string
			proxy.On("BroadcastSystemMessage", mock.Anything, mock.Anything).
				Run(func(args mock.Arguments) { capturedMsg = args.String(1) }).
				Return(nil)

			resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
				Command:       "wall",
				Args:          tt.args,
				CharacterName: "Admin",
			}, proxy)

			require.NoError(t, err)
			assert.Contains(t, resp.Output, "1 session")
			assert.Contains(t, capturedMsg, tt.wantPrefix)
		})
	}
}

func TestWallHandler_EmptyArgs(t *testing.T) {
	h := &WallHandler{}
	proxy := pluginmocks.NewMockServiceProxy(t)
	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "wall",
		Args:    "",
	}, proxy)

	require.NoError(t, err)
	assert.Contains(t, resp.Output, "Usage")
}

func TestNewHandler_Routes(t *testing.T) {
	h := NewHandler()
	proxy := pluginmocks.NewMockServiceProxy(t)

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
	proxy := pluginmocks.NewMockServiceProxy(t)
	proxy.On("Log", mock.Anything, "error", mock.Anything).Return()

	resp, err := h.HandleCommand(context.Background(), pluginsdk.CommandRequest{
		Command: "nonexistent",
	}, proxy)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandFailure, resp.Status)
	assert.Contains(t, resp.Output, "temporarily unavailable")
}
