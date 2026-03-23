// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// newDispatcherTestServer creates a CoreServer wired with the unified
// command dispatcher, using in-memory stores for testing.
func newDispatcherTestServer(t *testing.T, store core.EventStore, opts ...CoreServerOption) (*CoreServer, *core.SessionManager) {
	t.Helper()
	sessions := core.NewSessionManager()
	engine := core.NewEngine(store, sessions)

	reg := command.NewRegistry()
	handlers.RegisterAll(reg)

	policyEngine := policytest.AllowAllEngine()
	svc := command.NewTestServices(command.ServicesConfig{
		World:   nil,
		Session: sessions,
		Engine:  policyEngine,
		Events:  store,
	})

	dispatcher, err := command.NewDispatcher(reg, policyEngine)
	require.NoError(t, err)

	allOpts := make([]CoreServerOption, 0, 2+len(opts))
	allOpts = append(allOpts,
		WithEventStore(store),
		WithDispatcher(dispatcher, svc),
	)
	allOpts = append(allOpts, opts...)

	server := NewCoreServer(engine, sessions, session.NewMemStore(), allOpts...)
	return server, sessions
}

func TestDispatcher_HandleCommand_Say(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appended []core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appended = append(appended, event)
			return nil
		},
	}

	server, sessions := newDispatcherTestServer(t, store)
	sessions.Connect(charID, core.NewULID())

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "Tester",
		LocationID:    locationID,
		Status:        session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:      &corev1.RequestMeta{RequestId: "say-test", Timestamp: timestamppb.Now()},
		SessionId: sessionID.String(),
		Command:   "say Hello, world!",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, "say should succeed: %s", resp.Error)

	// Should emit a say event on the location stream
	require.NotEmpty(t, appended)
	assert.Equal(t, core.EventTypeSay, appended[0].Type)
	assert.Equal(t, "location:"+locationID.String(), appended[0].Stream)
}

func TestDispatcher_HandleCommand_Pose(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appended []core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appended = append(appended, event)
			return nil
		},
	}

	server, sessions := newDispatcherTestServer(t, store)
	sessions.Connect(charID, core.NewULID())

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "Tester",
		LocationID:    locationID,
		Status:        session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:      &corev1.RequestMeta{RequestId: "pose-test", Timestamp: timestamppb.Now()},
		SessionId: sessionID.String(),
		Command:   "pose waves hello",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, "pose should succeed: %s", resp.Error)

	require.NotEmpty(t, appended)
	assert.Equal(t, core.EventTypePose, appended[0].Type)
}

func TestDispatcher_HandleCommand_ColonPrefix(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appended []core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appended = append(appended, event)
			return nil
		},
	}

	server, sessions := newDispatcherTestServer(t, store)
	sessions.Connect(charID, core.NewULID())

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "Tester",
		LocationID:    locationID,
		Status:        session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:      &corev1.RequestMeta{RequestId: "colon-test", Timestamp: timestamppb.Now()},
		SessionId: sessionID.String(),
		Command:   ": nods",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, ": should expand to pose: %s", resp.Error)

	require.NotEmpty(t, appended)
	assert.Equal(t, core.EventTypePose, appended[0].Type)
}

func TestDispatcher_HandleCommand_UnknownCommand(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	store := core.NewMemoryEventStore()

	server, sessions := newDispatcherTestServer(t, store)
	sessions.Connect(charID, core.NewULID())

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "Tester",
		LocationID:    locationID,
		Status:        session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:      &corev1.RequestMeta{RequestId: "unknown-test", Timestamp: timestamppb.Now()},
		SessionId: sessionID.String(),
		Command:   "unknowncommand args",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, "unknown command should succeed at RPC level")

	// Should emit error command_response on character stream
	charEvents, err := store.Replay(ctx, "character:"+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.NotEmpty(t, charEvents, "expected command_response event")
	assert.Equal(t, core.EventTypeCommandResponse, charEvents[0].Type)

	var crp core.CommandResponsePayload
	require.NoError(t, json.Unmarshal(charEvents[0].Payload, &crp))
	assert.True(t, crp.IsError)
}

func TestDispatcher_HandleCommand_Quit(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	store := core.NewMemoryEventStore()

	var hookCalled bool
	server, sessions := newDispatcherTestServer(t, store,
		WithDisconnectHook(func(_ session.Info) {
			hookCalled = true
		}),
	)
	sessions.Connect(charID, core.NewULID())

	ctx := context.Background()
	sessStore := server.sessionStore
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "QuitChar",
		LocationID:    locationID,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:      &corev1.RequestMeta{RequestId: "quit-test", Timestamp: timestamppb.Now()},
		SessionId: sessionID.String(),
		Command:   "quit",
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Session should be deleted
	_, err = sessStore.Get(ctx, sessionID.String())
	assert.Error(t, err, "session should be deleted after quit")

	// Leave event should be emitted on location stream
	locEvents, err := store.Replay(ctx, "location:"+locationID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.Len(t, locEvents, 1, "expected exactly one leave event")
	assert.Equal(t, core.EventTypeLeave, locEvents[0].Type)

	// Goodbye command_response event on character stream
	charEvents, err := store.Replay(ctx, "character:"+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.NotEmpty(t, charEvents)
	assert.Equal(t, core.EventTypeCommandResponse, charEvents[0].Type)

	var crp core.CommandResponsePayload
	require.NoError(t, json.Unmarshal(charEvents[0].Payload, &crp))
	assert.False(t, crp.IsError)
	assert.Contains(t, crp.Text, "Goodbye")

	// Disconnect hook should fire
	assert.True(t, hookCalled)
}

func TestExpandMUSHPrefix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{": waves", "pose  waves"},
		{":waves", "pose waves"},
		{"say hello", "say hello"},
		{"look", "look"},
		{"", ""},
		{"  ", "  "},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, expandMUSHPrefix(tt.input))
		})
	}
}
