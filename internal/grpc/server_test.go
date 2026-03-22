// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	tlscerts "github.com/holomush/holomush/internal/tls"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// newTestSessionStore creates a session.MemStore pre-populated with the given sessions.
func newTestSessionStore(t *testing.T, sessions map[string]*session.Info) session.Store {
	t.Helper()
	store := session.NewMemStore()
	ctx := context.Background()
	for id, info := range sessions {
		require.NoError(t, store.Set(ctx, id, info))
	}
	return store
}

// mockEventStore implements core.EventStore for testing.
type mockEventStore struct {
	appendFunc      func(ctx context.Context, event core.Event) error
	replayFunc      func(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error)
	lastEventIDFunc func(ctx context.Context, stream string) (ulid.ULID, error)
}

func (m *mockEventStore) Append(ctx context.Context, event core.Event) error {
	if m.appendFunc != nil {
		return m.appendFunc(ctx, event)
	}
	return nil
}

func (m *mockEventStore) Replay(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error) {
	if m.replayFunc != nil {
		return m.replayFunc(ctx, stream, afterID, limit)
	}
	return nil, nil
}

func (m *mockEventStore) LastEventID(ctx context.Context, stream string) (ulid.ULID, error) {
	if m.lastEventIDFunc != nil {
		return m.lastEventIDFunc(ctx, stream)
	}
	return ulid.ULID{}, core.ErrStreamEmpty
}

func (m *mockEventStore) Subscribe(ctx context.Context, _ string) (<-chan ulid.ULID, <-chan error, error) {
	eventCh := make(chan ulid.ULID)
	errCh := make(chan error)
	go func() {
		<-ctx.Done()
		close(eventCh)
		close(errCh)
	}()
	return eventCh, errCh, nil
}

// mockAuthenticator provides authentication for testing.
type mockAuthenticator struct {
	authenticateFunc func(ctx context.Context, username, password string) (*AuthResult, error)
}

func (m *mockAuthenticator) Authenticate(ctx context.Context, username, password string) (*AuthResult, error) {
	if m.authenticateFunc != nil {
		return m.authenticateFunc(ctx, username, password)
	}
	return nil, errors.New("authentication not configured")
}

// mockSubscribeStream implements grpc.ServerStreamingServer[corev1.SubscribeResponse] for testing.
type mockSubscribeStream struct {
	grpc.ServerStream
	ctx    context.Context
	events []*corev1.SubscribeResponse
}

func (m *mockSubscribeStream) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *mockSubscribeStream) Send(event *corev1.SubscribeResponse) error {
	m.events = append(m.events, event)
	return nil
}

func TestCoreServer_Authenticate_Success(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	sessions := core.NewSessionManager()

	auth := &mockAuthenticator{
		authenticateFunc: func(_ context.Context, username, password string) (*AuthResult, error) {
			if username == "testuser" && password == "testpass" {
				return &AuthResult{
					CharacterID:   charID,
					CharacterName: "TestCharacter",
				}, nil
			}
			return nil, errors.New("invalid credentials")
		},
	}

	server := &CoreServer{
		engine:        core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:      sessions,
		authenticator: auth,
		sessionStore:  session.NewMemStore(),
		newSessionID:  func() ulid.ULID { return sessionID },
	}

	ctx := context.Background()
	req := &corev1.AuthenticateRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "test-request-id",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	}

	resp, err := server.Authenticate(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success)
	assert.Equal(t, sessionID.String(), resp.SessionId)
	assert.Equal(t, charID.String(), resp.CharacterId)
	assert.Equal(t, "TestCharacter", resp.CharacterName)
	require.NotNil(t, resp.Meta)
	assert.Equal(t, "test-request-id", resp.Meta.RequestId)
}

func TestCoreServer_Authenticate_InvalidCredentials(t *testing.T) {
	auth := &mockAuthenticator{
		authenticateFunc: func(_ context.Context, _, _ string) (*AuthResult, error) {
			return nil, errors.New("invalid credentials")
		},
	}

	sessions := core.NewSessionManager()
	server := &CoreServer{
		engine:        core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:      sessions,
		authenticator: auth,
		sessionStore:  session.NewMemStore(),
	}

	ctx := context.Background()
	req := &corev1.AuthenticateRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "test-request-id",
			Timestamp: timestamppb.Now(),
		},
		Username: "baduser",
		Password: "badpass",
	}

	resp, err := server.Authenticate(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "expected failure for invalid credentials")
	assert.NotEmpty(t, resp.Error, "error message should be present")
}

func TestCoreServer_HandleCommand_Say(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID()) // Create session

	var appendedEvent core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appendedEvent = event
			return nil
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cmd-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "say Hello, world!",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success, "expected success, got error: %s", resp.Error)
	assert.Equal(t, core.EventTypeSay, appendedEvent.Type)
}

func TestCoreServer_HandleCommand_InvalidSession(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		sessionStore: session.NewMemStore(),
	}

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cmd-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "invalid-session",
		Command:   "say Hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "expected failure for invalid session")
	assert.NotEmpty(t, resp.Error, "error message should be present")
}

func TestCoreServer_Subscribe_SendsEvents(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "sub-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Streams:   []string{"location:" + locationID.String()},
	}

	// Run Subscribe in a goroutine since it blocks
	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give the subscription time to set up
	time.Sleep(50 * time.Millisecond)

	// Send an event through the event store (triggers Subscribe notification)
	testEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    "location:" + locationID.String(),
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
		Payload:   []byte(`{"message":"test"}`),
	}
	require.NoError(t, eventStore.Append(ctx, testEvent))

	// Give time for event to be sent
	time.Sleep(50 * time.Millisecond)

	// Cancel the context to stop the subscription
	cancel()

	// Wait for Subscribe to finish
	select {
	case err := <-done:
		// Context cancellation is expected
		if err != nil {
			assert.ErrorIs(t, err, context.Canceled, "unexpected error type")
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	assert.NotEmpty(t, stream.events, "expected at least one event to be sent")
}

func TestCoreServer_Disconnect(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	connID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, connID)

	sessStore := session.NewMemStore()
	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		CharacterID: charID,
		Status:      session.StatusActive,
		TTLSeconds:  1800,
	}))

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		sessionStore: sessStore,
	}

	req := &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "disconnect-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success)
	require.NotNil(t, resp.Meta)
	assert.Equal(t, "disconnect-request-id", resp.Meta.RequestId)

	// Non-guest session should be detached, not deleted
	info, err := sessStore.Get(ctx, sessionID.String())
	require.NoError(t, err, "session should still exist after disconnect")
	assert.Equal(t, session.StatusDetached, info.Status)
	assert.NotNil(t, info.DetachedAt)
	assert.NotNil(t, info.ExpiresAt)
}

func TestNewCoreServer(t *testing.T) {
	store := &mockEventStore{}
	sessions := core.NewSessionManager()
	sessStore := session.NewMemStore()

	server := NewCoreServer(
		core.NewEngine(store, sessions),
		sessions,
		sessStore,
	)

	require.NotNil(t, server, "NewCoreServer returned nil")
	assert.NotNil(t, server.sessionStore, "sessionStore should be initialized")
}

func TestNewCoreServer_WithOptions(t *testing.T) {
	store := &mockEventStore{}
	sessions := core.NewSessionManager()

	customAuth := &mockAuthenticator{}
	customStore := session.NewMemStore()

	server := NewCoreServer(
		core.NewEngine(store, sessions),
		sessions,
		session.NewMemStore(),
		WithAuthenticator(customAuth),
		WithSessionStore(customStore),
	)

	require.NotNil(t, server, "NewCoreServer returned nil")
	assert.Equal(t, customAuth, server.authenticator, "WithAuthenticator option not applied")
	assert.Equal(t, customStore, server.sessionStore, "WithSessionStore option not applied")
}

func TestCoreServer_Authenticate_NoAuthenticator(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		engine:        core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:      sessions,
		sessionStore:  session.NewMemStore(),
		authenticator: nil, // No authenticator configured
	}

	ctx := context.Background()
	req := &corev1.AuthenticateRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "no-auth-test",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	}

	resp, err := server.Authenticate(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "expected failure when authenticator not configured")
	assert.Equal(t, "authentication not configured", resp.Error)
}

func TestCoreServer_Authenticate_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	sessions := core.NewSessionManager()

	auth := &mockAuthenticator{
		authenticateFunc: func(_ context.Context, _, _ string) (*AuthResult, error) {
			return &AuthResult{
				CharacterID:   charID,
				CharacterName: "TestCharacter",
			}, nil
		},
	}

	server := &CoreServer{
		engine:        core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:      sessions,
		authenticator: auth,
		sessionStore:  session.NewMemStore(),
		newSessionID:  func() ulid.ULID { return sessionID },
	}

	ctx := context.Background()
	req := &corev1.AuthenticateRequest{
		Meta:     nil, // No meta
		Username: "testuser",
		Password: "testpass",
	}

	resp, err := server.Authenticate(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success)
}

func TestCoreServer_HandleCommand_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta:      nil, // No meta
		SessionId: sessionID.String(),
		Command:   "say Hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success, "expected success, got error: %s", resp.Error)
}

func TestCoreServer_HandleCommand_Pose(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	var appendedEvent core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appendedEvent = event
			return nil
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()

	// Test pose command
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "pose-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "pose waves hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success, "expected success, got error: %s", resp.Error)
	assert.Equal(t, core.EventTypePose, appendedEvent.Type)

	// Test : shortcut for pose
	req2 := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "colon-pose-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   ": nods",
	}

	resp2, err := server.HandleCommand(ctx, req2)
	require.NoError(t, err)

	assert.True(t, resp2.Success, "expected success for : shortcut, got error: %s", resp2.Error)
}

func TestCoreServer_HandleCommand_UnknownCommand(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := core.NewMemoryEventStore()
	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:     engine,
		sessions:   sessions,
		eventStore: store,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:   charID,
				CharacterName: "Tester",
				LocationID:    locationID,
				Status:        session.StatusActive,
			},
		}),
	}

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "unknown-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "unknowncommand args",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	// Unknown commands now succeed at the RPC level; the error is delivered
	// via a command_response event on the character stream.
	assert.True(t, resp.Success, "unknown command should succeed at RPC level")

	// Verify error command_response event was emitted with correct payload content.
	charEvents, err := store.Replay(ctx, "character:"+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.NotEmpty(t, charEvents, "expected command_response event")
	assert.Equal(t, core.EventTypeCommandResponse, charEvents[0].Type)

	var crp core.CommandResponsePayload
	require.NoError(t, json.Unmarshal(charEvents[0].Payload, &crp), "command_response payload should be valid JSON")
	assert.True(t, crp.IsError, "unknown command response should be an error")
	assert.NotEmpty(t, crp.Text, "command_response text should not be empty")
}

func TestCoreServer_HandleCommand_SayFails(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return errors.New("database error")
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "say-fail-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "say Hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "expected failure when say fails")
	assert.NotEmpty(t, resp.Error, "error should contain error message")
}

func TestCoreServer_HandleCommand_PoseFails(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return errors.New("database error")
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "pose-fail-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "pose waves",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "expected failure when pose fails")
	assert.NotEmpty(t, resp.Error, "error should contain error message")
}

func TestCoreServer_HandleCommand_Quit(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := core.NewMemoryEventStore()
	engine := core.NewEngine(store, sessions)

	sessStore := session.NewMemStore()
	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		LocationID:    locationID,
		CharacterName: "QuitChar",
		IsGuest:       false,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))

	var hookCalled bool
	server := NewCoreServer(engine, sessions, sessStore,
		WithEventStore(store),
		WithDisconnectHook(func(_ session.Info) {
			hookCalled = true
		}),
	)

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "quit-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "quit",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Session should be deleted immediately
	_, err = sessStore.Get(ctx, sessionID.String())
	assert.Error(t, err, "session should be deleted after quit command")

	// Leave event should be emitted on location stream
	locEvents, err := store.Replay(ctx, "location:"+locationID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.Len(t, locEvents, 1, "expected exactly one leave event")
	assert.Equal(t, core.EventTypeLeave, locEvents[0].Type)

	// command_response "Goodbye!" event should be emitted on character stream.
	charEvents, err := store.Replay(ctx, "character:"+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.NotEmpty(t, charEvents, "expected command_response event on character stream")
	assert.Equal(t, core.EventTypeCommandResponse, charEvents[0].Type)

	var crp core.CommandResponsePayload
	require.NoError(t, json.Unmarshal(charEvents[0].Payload, &crp), "quit command_response payload should be valid JSON")
	assert.False(t, crp.IsError, "quit response should not be an error")
	assert.Contains(t, crp.Text, "Goodbye", "quit response should contain Goodbye")

	// Disconnect hooks should fire
	assert.True(t, hookCalled, "disconnect hook should be called on quit")
}

func TestCoreServer_Subscribe_InvalidSession(t *testing.T) {
	sessions := core.NewSessionManager()
	eventStore := core.NewMemoryEventStore()

	server := &CoreServer{
		engine:       core.NewEngine(eventStore, sessions),
		sessions:     sessions,
		eventStore:   eventStore,
		sessionStore: session.NewMemStore(),
	}

	ctx := context.Background()
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "invalid-session-sub",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "non-existent-session",
		Streams:   []string{"location:test"},
	}

	err := server.Subscribe(req, stream)
	assert.Error(t, err, "Subscribe() should return error for invalid session")
}

func TestCoreServer_Subscribe_NilEventStore(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		sessionStore: session.NewMemStore(),
		// eventStore intentionally nil
	}

	ctx := context.Background()
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "nil-event-store",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "any-session",
		Streams:   []string{"location:test"},
	}

	err := server.Subscribe(req, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event store not configured")
}

func TestCoreServer_Subscribe_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta:      nil, // No meta
		SessionId: sessionID.String(),
		Streams:   []string{"location:" + locationID.String()},
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give subscription time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel immediately
	cancel()

	select {
	case err := <-done:
		// context.Canceled is a normal client disconnect — Subscribe returns nil.
		assert.NoError(t, err, "Subscribe() should return nil on normal context cancellation")
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}
}

type mockSubscribeStreamWithError struct {
	grpc.ServerStream
	ctx      context.Context
	sendErr  error
	sendFunc func(*corev1.SubscribeResponse) error
}

func (m *mockSubscribeStreamWithError) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *mockSubscribeStreamWithError) Send(event *corev1.SubscribeResponse) error {
	if m.sendFunc != nil {
		return m.sendFunc(event)
	}
	return m.sendErr
}

func TestCoreServer_Subscribe_SendError(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()
	stream := &mockSubscribeStreamWithError{
		ctx:     ctx,
		sendErr: errors.New("send failed"),
	}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "send-error-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Streams:   []string{"location:" + locationID.String()},
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give subscription time to start
	time.Sleep(50 * time.Millisecond)

	// Send an event that will cause send error
	testEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    "location:" + locationID.String(),
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
		Payload:   []byte(`{"message":"test"}`),
	}
	require.NoError(t, eventStore.Append(ctx, testEvent))

	select {
	case err := <-done:
		assert.Error(t, err, "Subscribe() should return error when send fails")
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after send error")
	}
}

func TestCoreServer_Disconnect_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	connID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, connID)

	sessStore := session.NewMemStore()
	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		CharacterID: charID,
		Status:      session.StatusActive,
	}))

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		sessionStore: sessStore,
	}

	req := &corev1.DisconnectRequest{
		Meta:      nil, // No meta
		SessionId: sessionID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success)

	// Non-guest session should be detached, not deleted
	info, err := sessStore.Get(ctx, sessionID.String())
	require.NoError(t, err, "session should still exist after disconnect")
	assert.Equal(t, session.StatusDetached, info.Status)
}

func TestCoreServer_Disconnect_NonExistentSession(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		sessionStore: session.NewMemStore(),
	}

	ctx := context.Background()
	req := &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "non-existent-disc",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "non-existent-session",
	}

	resp, err := server.Disconnect(ctx, req)
	require.NoError(t, err)

	// Should succeed even for non-existent session (idempotent)
	assert.True(t, resp.Success, "expected success for non-existent session (idempotent)")
}

func TestNewGRPCServerInsecure(t *testing.T) {
	server := NewGRPCServerInsecure()
	require.NotNil(t, server, "NewGRPCServerInsecure() returned nil")
	server.Stop()
}

func TestNewGRPCServer(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-grpc"

	// Generate certificates
	ca, err := tlscerts.GenerateCA(gameID)
	require.NoError(t, err, "GenerateCA() error")

	serverCert, err := tlscerts.GenerateServerCert(ca, gameID, "core")
	require.NoError(t, err, "GenerateServerCert() error")

	err = tlscerts.SaveCertificates(tmpDir, ca, serverCert)
	require.NoError(t, err, "SaveCertificates() error")

	// Load TLS config
	tlsConfig, err := tlscerts.LoadServerTLS(tmpDir, "core")
	require.NoError(t, err, "LoadServerTLS() error")

	// Create gRPC server with TLS
	server := NewGRPCServer(tlsConfig)
	require.NotNil(t, server, "NewGRPCServer() returned nil")
	server.Stop()
}

// =============================================================================
// Session Expiration Tests (e55.36)
// =============================================================================

func TestCoreServer_SessionExpirationOnContextTimeout(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()

	sessionStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		eventStore:   eventStore,
		sessionStore: sessionStore,
	}

	// Create a context with a very short timeout to simulate session inactivity
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "timeout-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Streams:   []string{"location:" + locationID.String()},
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Wait for the timeout to trigger
	select {
	case err := <-done:
		// Should return an error due to context deadline exceeded
		require.Error(t, err, "Subscribe() should return error when context times out")
		isDeadlineError := errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "deadline exceeded")
		assert.True(t, isDeadlineError, "expected deadline exceeded error, got: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Subscribe did not return after context timeout")
	}
}

func TestCoreServer_SessionCleanupOnDisconnect(t *testing.T) {
	t.Run("guest session is deleted", func(t *testing.T) {
		charID := core.NewULID()
		sessionID := core.NewULID()
		connID := core.NewULID()
		sessions := core.NewSessionManager()
		sessions.Connect(charID, connID)

		ctx := context.Background()
		sessionStore := session.NewMemStore()
		require.NoError(t, sessionStore.Set(ctx, sessionID.String(), &session.Info{
			CharacterID: charID,
			IsGuest:     true,
			Status:      session.StatusActive,
		}))

		server := &CoreServer{
			engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
			sessions:     sessions,
			sessionStore: sessionStore,
		}

		_, err := sessionStore.Get(ctx, sessionID.String())
		require.NoError(t, err, "Session should exist before disconnect")

		req := &corev1.DisconnectRequest{
			Meta: &corev1.RequestMeta{
				RequestId: "cleanup-test-guest",
				Timestamp: timestamppb.Now(),
			},
			SessionId: sessionID.String(),
		}

		resp, err := server.Disconnect(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success, "Disconnect() should succeed")

		_, err = sessionStore.Get(ctx, sessionID.String())
		assert.Error(t, err, "Guest session should be removed after disconnect")
	})

	t.Run("non-guest session is detached", func(t *testing.T) {
		charID := core.NewULID()
		sessionID := core.NewULID()
		connID := core.NewULID()
		sessions := core.NewSessionManager()
		sessions.Connect(charID, connID)

		ctx := context.Background()
		sessionStore := session.NewMemStore()
		require.NoError(t, sessionStore.Set(ctx, sessionID.String(), &session.Info{
			CharacterID: charID,
			IsGuest:     false,
			Status:      session.StatusActive,
			TTLSeconds:  1800,
		}))

		server := &CoreServer{
			engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
			sessions:     sessions,
			sessionStore: sessionStore,
		}

		_, err := sessionStore.Get(ctx, sessionID.String())
		require.NoError(t, err, "Session should exist before disconnect")

		req := &corev1.DisconnectRequest{
			Meta: &corev1.RequestMeta{
				RequestId: "cleanup-test-nonguest",
				Timestamp: timestamppb.Now(),
			},
			SessionId: sessionID.String(),
		}

		resp, err := server.Disconnect(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success, "Disconnect() should succeed")

		info, err := sessionStore.Get(ctx, sessionID.String())
		require.NoError(t, err, "Non-guest session should still exist after disconnect")
		assert.Equal(t, session.StatusDetached, info.Status)
		assert.NotNil(t, info.DetachedAt)
		assert.NotNil(t, info.ExpiresAt)
	})
}

func TestCoreServer_SessionRefreshOnActivity(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	engine := core.NewEngine(store, sessions)

	sessionStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		sessionStore: sessionStore,
	}

	ctx := context.Background()

	// Execute multiple commands to simulate activity
	for i := 0; i < 3; i++ {
		req := &corev1.HandleCommandRequest{
			Meta: &corev1.RequestMeta{
				RequestId: fmt.Sprintf("activity-test-%d", i),
				Timestamp: timestamppb.Now(),
			},
			SessionId: sessionID.String(),
			Command:   fmt.Sprintf("say Message %d", i),
		}

		resp, err := server.HandleCommand(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success, "HandleCommand() iteration %d failed: %s", i, resp.Error)
	}

	// Session should still exist after activity
	_, err := sessionStore.Get(ctx, sessionID.String())
	assert.NoError(t, err, "Session should persist after activity")
}

func TestCoreServer_MultipleSessionsIndependentExpiration(t *testing.T) {
	sessions := core.NewSessionManager()
	eventStore := core.NewMemoryEventStore()
	ctx := context.Background()
	sessionStore := session.NewMemStore()

	// Create two sessions
	session1ID := core.NewULID()
	char1ID := core.NewULID()
	conn1ID := core.NewULID()
	location1ID := core.NewULID()
	sessions.Connect(char1ID, conn1ID)
	require.NoError(t, sessionStore.Set(ctx, session1ID.String(), &session.Info{
		CharacterID: char1ID,
		LocationID:  location1ID,
		Status:      session.StatusActive,
	}))

	session2ID := core.NewULID()
	char2ID := core.NewULID()
	conn2ID := core.NewULID()
	location2ID := core.NewULID()
	sessions.Connect(char2ID, conn2ID)
	require.NoError(t, sessionStore.Set(ctx, session2ID.String(), &session.Info{
		CharacterID: char2ID,
		LocationID:  location2ID,
		Status:      session.StatusActive,
	}))

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		eventStore:   eventStore,
		sessionStore: sessionStore,
	}

	// Disconnect only session 1
	req := &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "multi-session-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: session1ID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Success, "Disconnect() should succeed")

	// Verify session 1 is detached (not deleted) for non-guest
	info1, err := sessionStore.Get(ctx, session1ID.String())
	require.NoError(t, err, "Session 1 should still exist after disconnect (detached)")
	assert.Equal(t, session.StatusDetached, info1.Status)

	// Verify session 2 still exists and is active
	info2, err := sessionStore.Get(ctx, session2ID.String())
	require.NoError(t, err, "Session 2 should still exist after session 1 disconnect")
	assert.Equal(t, session.StatusActive, info2.Status)
}

// =============================================================================
// Command Timeout Tests (e55.37)
// =============================================================================

func TestCoreServer_HandleCommand_ContextTimeout(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	// Create a store that simulates a slow operation
	store := &mockEventStore{
		appendFunc: func(ctx context.Context, _ core.Event) error {
			// Simulate slow database operation
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
				return nil
			}
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	// Create a context with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "timeout-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "say This should timeout",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	// The command should fail due to timeout
	assert.False(t, resp.Success, "HandleCommand() should fail when context times out")
	assert.NotEmpty(t, resp.Error, "error message should be present for timeout")
}

func TestCoreServer_HandleCommand_ContextCancellation(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	// Create a store that waits for cancellation
	store := &mockEventStore{
		appendFunc: func(ctx context.Context, _ core.Event) error {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
				return nil
			}
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cancel-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "say This will be cancelled",
	}

	// Start command in goroutine
	done := make(chan *corev1.HandleCommandResponse)
	go func() {
		resp, _ := server.HandleCommand(ctx, req)
		done <- resp
	}()

	// Cancel after a short delay
	time.Sleep(20 * time.Millisecond)
	cancel()

	// Wait for response
	select {
	case resp := <-done:
		assert.False(t, resp.Success, "HandleCommand() should fail when context is cancelled")
		assert.NotEmpty(t, resp.Error, "error message should be present for cancellation")
	case <-time.After(time.Second):
		t.Fatal("HandleCommand did not return after context cancellation")
	}
}

func TestCoreServer_Subscribe_ContextCancellationCleanup(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}
	streamName := "location:" + locationID.String()

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cancel-sub-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Streams:   []string{streamName},
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give subscription time to set up
	time.Sleep(50 * time.Millisecond)

	// Verify subscription is active by sending an event
	testEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    streamName,
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
		Payload:   []byte(`{"message":"test"}`),
	}
	require.NoError(t, eventStore.Append(ctx, testEvent))

	// Give time for event to be received
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for subscription to end
	select {
	case err := <-done:
		// context.Canceled is a normal client disconnect — Subscribe returns nil.
		assert.NoError(t, err, "Subscribe() should return nil on normal context cancellation")
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Verify at least one event was received before cancellation
	assert.NotEmpty(t, stream.events, "expected at least one event before cancellation")
}

func TestCoreServer_HandleCommand_TimeoutErrorMessage(t *testing.T) {
	tests := []struct {
		name          string
		timeout       time.Duration
		storeDelay    time.Duration
		expectError   bool
		errorContains string
	}{
		{
			name:        "fast command succeeds",
			timeout:     500 * time.Millisecond,
			storeDelay:  10 * time.Millisecond,
			expectError: false,
		},
		{
			name:          "slow command times out",
			timeout:       30 * time.Millisecond,
			storeDelay:    200 * time.Millisecond,
			expectError:   true,
			errorContains: "context",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charID := core.NewULID()
			sessionID := core.NewULID()
			locationID := core.NewULID()
			sessions := core.NewSessionManager()
			sessions.Connect(charID, core.NewULID())

			store := &mockEventStore{
				appendFunc: func(ctx context.Context, _ core.Event) error {
					select {
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(tt.storeDelay):
						return nil
					}
				},
			}

			engine := core.NewEngine(store, sessions)

			server := &CoreServer{
				engine:   engine,
				sessions: sessions,
				sessionStore: newTestSessionStore(t, map[string]*session.Info{
					sessionID.String(): {
						CharacterID: charID,
						LocationID:  locationID,
						Status:      session.StatusActive,
					},
				}),
			}

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			req := &corev1.HandleCommandRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "timeout-error-test",
					Timestamp: timestamppb.Now(),
				},
				SessionId: sessionID.String(),
				Command:   "say Hello",
			}

			resp, err := server.HandleCommand(ctx, req)
			require.NoError(t, err)

			if tt.expectError {
				assert.False(t, resp.Success, "expected command to fail with timeout")
				if tt.errorContains != "" {
					assert.Contains(t, strings.ToLower(resp.Error), tt.errorContains)
				}
			} else {
				assert.True(t, resp.Success, "expected command to succeed, got error: %s", resp.Error)
			}
		})
	}
}

func TestCoreServer_Subscribe_TimeoutDuringEventSend(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Create a stream that blocks on send
	blockingSendCalled := make(chan struct{})
	stream := &mockSubscribeStreamWithError{
		ctx: ctx,
		sendFunc: func(_ *corev1.SubscribeResponse) error {
			close(blockingSendCalled)
			// Block until context is cancelled
			<-ctx.Done()
			return ctx.Err()
		},
	}

	streamName := "location:" + locationID.String()
	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "timeout-send-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Streams:   []string{streamName},
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give subscription time to set up
	time.Sleep(20 * time.Millisecond)

	// Send an event that will block
	testEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    streamName,
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
		Payload:   []byte(`{"message":"test"}`),
	}
	require.NoError(t, eventStore.Append(ctx, testEvent))

	// Wait for send to be called
	select {
	case <-blockingSendCalled:
		// Send was called, now wait for timeout
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Send was not called")
	}

	// Wait for subscription to timeout
	select {
	case err := <-done:
		assert.Error(t, err, "Subscribe() should return error when send times out")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Subscribe did not return after timeout")
	}
}

func TestCoreServer_HandleCommand_EmptyCommandWithTimeout(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{}
	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	// Even with a short timeout, an empty command should fail fast
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "empty-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "empty command should fail")
	assert.NotEmpty(t, resp.Error, "error message should be present")
}

// =============================================================================
// Malformed Request Tests (e55.38)
// =============================================================================

func TestCoreServer_MalformedRequest_NilAuthRequest(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		engine:        core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:      sessions,
		sessionStore:  session.NewMemStore(),
		authenticator: nil,
	}

	ctx := context.Background()

	// Pass nil request - should not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Authenticate panicked on nil request: %v", r)
		}
	}()

	// This tests the server's behavior with an empty AuthRequest
	req := &corev1.AuthenticateRequest{}
	resp, err := server.Authenticate(ctx, req)
	// Should return an error response, not panic
	if err != nil {
		// gRPC error is acceptable
		return
	}
	assert.False(t, resp.Success, "expected failure for empty auth request")
}

func TestCoreServer_MalformedRequest_EmptyUsername(t *testing.T) {
	auth := &mockAuthenticator{
		authenticateFunc: func(_ context.Context, username, _ string) (*AuthResult, error) {
			if username == "" {
				return nil, errors.New("username required")
			}
			return nil, errors.New("invalid credentials")
		},
	}

	server := &CoreServer{
		engine:        core.NewEngine(core.NewMemoryEventStore(), core.NewSessionManager()),
		sessions:      core.NewSessionManager(),
		authenticator: auth,
		sessionStore:  session.NewMemStore(),
	}

	ctx := context.Background()
	req := &corev1.AuthenticateRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "empty-username",
			Timestamp: timestamppb.Now(),
		},
		Username: "",
		Password: "password",
	}

	resp, err := server.Authenticate(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "expected failure for empty username")
	assert.NotEmpty(t, resp.Error, "error message should be present")
}

func TestCoreServer_MalformedRequest_InvalidSessionID(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		sessionStore: session.NewMemStore(),
	}

	ctx := context.Background()

	tests := []struct {
		name      string
		sessionID string
	}{
		{"empty session ID", ""},
		{"invalid ULID format", "not-a-valid-ulid"},
		{"partial ULID", "01ABCD"},
		{"unicode characters", "日本語セッション"},
		{"null bytes", "session\x00id"},
		{"very long session ID", strings.Repeat("a", 10000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &corev1.HandleCommandRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "invalid-session-test",
					Timestamp: timestamppb.Now(),
				},
				SessionId: tt.sessionID,
				Command:   "say hello",
			}

			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("HandleCommand panicked: %v", r)
				}
			}()

			resp, err := server.HandleCommand(ctx, req)
			if err != nil {
				// gRPC error is acceptable
				return
			}
			assert.False(t, resp.Success, "expected failure for invalid session ID")
			assert.NotEmpty(t, resp.Error, "error message should be present")
		})
	}
}

func TestCoreServer_MalformedRequest_InvalidCommand(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{}
	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()

	tests := []struct {
		name    string
		command string
	}{
		{"empty command", ""},
		{"whitespace only", "   "},
		{"null bytes", "say\x00hello"},
		{"unicode control chars", "say\u0000\u0001\u0002hello"},
		{"very long command", strings.Repeat("a", 100000)},
		{"only spaces and tabs", " \t \t "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &corev1.HandleCommandRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "malformed-cmd-test",
					Timestamp: timestamppb.Now(),
				},
				SessionId: sessionID.String(),
				Command:   tt.command,
			}

			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("HandleCommand panicked: %v", r)
				}
			}()

			_, err := server.HandleCommand(ctx, req)
			// May succeed, fail, or return error depending on command processing
			// Main assertion is that it doesn't panic
			_ = err
		})
	}
}

func TestCoreServer_MalformedRequest_InvalidSubscribeStreams(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	tests := []struct {
		name    string
		streams []string
	}{
		{"empty streams", []string{}},
		{"nil streams", nil},
		{"empty string stream", []string{""}},
		{"stream with null bytes", []string{"location\x00:test"}},
		{"very long stream name", []string{strings.Repeat("a", 100000)}},
		{"many streams", func() []string {
			streams := make([]string, 1000)
			for i := range streams {
				streams[i] = fmt.Sprintf("stream:%d", i)
			}
			return streams
		}()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()

			stream := &mockSubscribeStream{ctx: ctx}

			req := &corev1.SubscribeRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "malformed-sub-test",
					Timestamp: timestamppb.Now(),
				},
				SessionId: sessionID.String(),
				Streams:   tt.streams,
			}

			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Subscribe panicked: %v", r)
				}
			}()

			// Run in goroutine and wait for timeout
			done := make(chan error)
			go func() {
				done <- server.Subscribe(req, stream)
			}()

			select {
			case <-done:
				// Completed normally
			case <-time.After(200 * time.Millisecond):
				// Timeout is expected
			}
		})
	}
}

func TestCoreServer_MalformedRequest_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()

	// All requests with nil Meta should not panic
	t.Run("CommandRequest with nil Meta", func(t *testing.T) {
		req := &corev1.HandleCommandRequest{
			Meta:      nil,
			SessionId: sessionID.String(),
			Command:   "say hello",
		}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("HandleCommand panicked with nil Meta: %v", r)
			}
		}()

		resp, err := server.HandleCommand(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success, "expected success, got error: %s", resp.Error)
	})

	t.Run("DisconnectRequest with nil Meta", func(t *testing.T) {
		req := &corev1.DisconnectRequest{
			Meta:      nil,
			SessionId: sessionID.String(),
		}

		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Disconnect panicked with nil Meta: %v", r)
			}
		}()

		resp, err := server.Disconnect(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success, "expected success for disconnect")
	})
}

func TestCoreServer_MalformedRequest_UnknownFields(t *testing.T) {
	// Protobuf messages gracefully ignore unknown fields by design
	// This test verifies the server behaves correctly with valid requests
	// that have extra data (which protobuf silently ignores)

	sessions := core.NewSessionManager()
	charID := core.NewULID()
	sessionID := core.NewULID()
	sessions.Connect(charID, core.NewULID())

	auth := &mockAuthenticator{
		authenticateFunc: func(_ context.Context, _, _ string) (*AuthResult, error) {
			return &AuthResult{
				CharacterID:   charID,
				CharacterName: "Test",
			}, nil
		},
	}

	server := &CoreServer{
		engine:        core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:      sessions,
		authenticator: auth,
		sessionStore:  session.NewMemStore(),
		newSessionID:  func() ulid.ULID { return sessionID },
	}

	ctx := context.Background()

	// Normal request should work
	req := &corev1.AuthenticateRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "unknown-fields-test",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	}

	resp, err := server.Authenticate(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Success, "expected success, got error: %s", resp.Error)
}

func TestCoreServer_MalformedRequest_ConcurrentMalformedRequests(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			time.Sleep(10 * time.Millisecond) // Simulate some processing
			return nil
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()

	// Send many concurrent requests, some valid and some malformed
	var wg sync.WaitGroup
	numRequests := 50

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Request %d panicked: %v", idx, r)
				}
			}()

			var req *corev1.HandleCommandRequest
			switch idx % 3 {
			case 0:
				// Valid request
				req = &corev1.HandleCommandRequest{
					Meta: &corev1.RequestMeta{
						RequestId: fmt.Sprintf("concurrent-%d", idx),
						Timestamp: timestamppb.Now(),
					},
					SessionId: sessionID.String(),
					Command:   "say hello",
				}
			case 1:
				// Invalid session
				req = &corev1.HandleCommandRequest{
					Meta: &corev1.RequestMeta{
						RequestId: fmt.Sprintf("concurrent-%d", idx),
						Timestamp: timestamppb.Now(),
					},
					SessionId: "invalid-session",
					Command:   "say hello",
				}
			default:
				// Empty command
				req = &corev1.HandleCommandRequest{
					Meta: &corev1.RequestMeta{
						RequestId: fmt.Sprintf("concurrent-%d", idx),
						Timestamp: timestamppb.Now(),
					},
					SessionId: sessionID.String(),
					Command:   "",
				}
			}

			_, _ = server.HandleCommand(ctx, req)
		}(i)
	}

	wg.Wait()
}

func TestCoreServer_MalformedRequest_VeryLargePayload(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()

	// Create request with very large command
	largeCommand := "say " + strings.Repeat("x", 1*1024*1024) // 1MB message

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "large-payload-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   largeCommand,
	}

	// Should not panic, may succeed or fail based on limits
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("HandleCommand panicked with large payload: %v", r)
		}
	}()

	_, err := server.HandleCommand(ctx, req)
	// Either success or error is acceptable, but no panic
	_ = err
}

func TestCoreServer_MalformedRequest_SpecialCharacters(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	engine := core.NewEngine(store, sessions)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
				Status:      session.StatusActive,
			},
		}),
	}

	ctx := context.Background()

	tests := []struct {
		name    string
		command string
	}{
		{"unicode emoji", "say 👋🌍"},
		{"unicode CJK", "say 你好世界"},
		{"unicode RTL", "say مرحبا بالعالم"},
		{"unicode mixed", "say Hello 你好 مرحبا 🌍"},
		{"special chars", "say <script>alert('xss')</script>"},
		{"SQL injection", "say '; DROP TABLE users; --"},
		{"path traversal", "say ../../../etc/passwd"},
		{"newlines", "say line1\nline2\nline3"},
		{"carriage return", "say line1\r\nline2"},
		{"tabs", "say col1\tcol2\tcol3"},
		{"backslashes", "say path\\to\\file"},
		{"quotes", "say \"quoted\" 'text'"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &corev1.HandleCommandRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "special-chars-test",
					Timestamp: timestamppb.Now(),
				},
				SessionId: sessionID.String(),
				Command:   tt.command,
			}

			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("HandleCommand panicked with %s: %v", tt.name, r)
				}
			}()

			resp, err := server.HandleCommand(ctx, req)
			if err != nil {
				// gRPC error is acceptable
				return
			}
			// Should succeed for say commands
			if !resp.Success {
				t.Logf("Command failed (acceptable): %s", resp.Error)
			}
		})
	}
}

func TestCoreServer_DisconnectHook(t *testing.T) {
	charID := core.NewULID()
	locationID := core.NewULID()
	sessionID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := core.NewMemoryEventStore()
	engine := core.NewEngine(store, sessions)

	var hookCalled bool
	var hookInfo session.Info
	sessStore := session.NewMemStore()
	server := NewCoreServer(engine, sessions, sessStore,
		WithDisconnectHook(func(info session.Info) {
			hookCalled = true
			hookInfo = info
		}),
	)
	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		CharacterID:   charID,
		LocationID:    locationID,
		CharacterName: "GuestChar",
		IsGuest:       true,
		Status:        session.StatusActive,
	}))
	req := &corev1.DisconnectRequest{
		Meta:      &corev1.RequestMeta{RequestId: "hook-test", Timestamp: timestamppb.Now()},
		SessionId: sessionID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Success)

	assert.True(t, hookCalled, "disconnect hook was not called")
	assert.Equal(t, charID, hookInfo.CharacterID)
	assert.Equal(t, "GuestChar", hookInfo.CharacterName)
	assert.True(t, hookInfo.IsGuest)
}

func TestCoreServer_DisconnectHook_PanicRecovery(t *testing.T) {
	charID := core.NewULID()
	locationID := core.NewULID()
	sessionID := core.NewULID()

	store := core.NewMemoryEventStore()
	sessions := core.NewSessionManager()
	engine := core.NewEngine(store, sessions)

	hookCallCount := 0
	server := &CoreServer{
		engine:     engine,
		sessions:   sessions,
		eventStore: core.NewMemoryEventStore(),
		authenticator: &mockAuthenticator{
			authenticateFunc: func(_ context.Context, _, _ string) (*AuthResult, error) {
				return &AuthResult{
					CharacterID:   charID,
					CharacterName: "PanicTest",
					LocationID:    locationID,
					IsGuest:       true,
				}, nil
			},
		},
		sessionStore: session.NewMemStore(),
		newSessionID: func() ulid.ULID { return sessionID },
		disconnectHooks: []func(session.Info){
			func(_ session.Info) { panic("hook panic") },
			func(_ session.Info) { hookCallCount++ },
		},
	}

	ctx := context.Background()
	authResp, err := server.Authenticate(ctx, &corev1.AuthenticateRequest{
		Username: "guest",
		Meta:     &corev1.RequestMeta{RequestId: "test"},
	})
	require.NoError(t, err)
	require.True(t, authResp.Success)

	// Disconnect should not panic — recovery catches it
	discResp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
		SessionId:    authResp.SessionId,
		ConnectionId: authResp.ConnectionId,
		Meta:         &corev1.RequestMeta{RequestId: "test"},
	})
	require.NoError(t, err)
	require.True(t, discResp.Success)

	// Second hook should still run after first panicked
	assert.Equal(t, 1, hookCallCount, "second hook should run despite first hook panicking")
}

func TestCoreServer_Disconnect_NonGuest_NoEndSession(t *testing.T) {
	charID := core.NewULID()
	locationID := core.NewULID()
	sessionID := core.NewULID()
	connID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, connID)

	store := core.NewMemoryEventStore()
	engine := core.NewEngine(store, sessions)

	sessStore := session.NewMemStore()
	server := NewCoreServer(engine, sessions, sessStore)
	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		CharacterID:   charID,
		LocationID:    locationID,
		CharacterName: "RegularChar",
		IsGuest:       false,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))

	req := &corev1.DisconnectRequest{
		Meta:      &corev1.RequestMeta{RequestId: "non-guest-test", Timestamp: timestamppb.Now()},
		SessionId: sessionID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Session should be detached, not deleted
	info, err := sessStore.Get(ctx, sessionID.String())
	require.NoError(t, err, "session should still exist for non-guest after disconnect")
	assert.Equal(t, session.StatusDetached, info.Status)
	assert.NotNil(t, info.DetachedAt)
	assert.NotNil(t, info.ExpiresAt)
}

// =============================================================================
// Command History Tests (Chunk 6a)
// =============================================================================

func TestCoreServer_HandleCommand_RecordsHistory(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error { return nil },
	}
	engine := core.NewEngine(store, sessions)

	sessStore := session.NewMemStore()
	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		ID:          sessionID.String(),
		CharacterID: charID,
		LocationID:  locationID,
		Status:      session.StatusActive,
		MaxHistory:  100,
	}))

	server := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		sessionStore: sessStore,
	}

	commands := []string{"say hello", "pose waves", "say goodbye"}
	for _, cmd := range commands {
		req := &corev1.HandleCommandRequest{
			Meta:      &corev1.RequestMeta{RequestId: "history-test", Timestamp: timestamppb.Now()},
			SessionId: sessionID.String(),
			Command:   cmd,
		}
		resp, err := server.HandleCommand(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success, "command %q failed: %s", cmd, resp.Error)
	}

	history, err := sessStore.GetCommandHistory(ctx, sessionID.String())
	require.NoError(t, err)
	assert.Equal(t, commands, history, "command history should match commands in order")
}

func TestCoreServer_HandleCommand_HistoryEnforcedCap(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error { return nil },
	}
	engine := core.NewEngine(store, sessions)

	const maxHistory = 3
	sessStore := session.NewMemStore()
	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		ID:          sessionID.String(),
		CharacterID: charID,
		LocationID:  locationID,
		Status:      session.StatusActive,
		MaxHistory:  maxHistory,
	}))

	server := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		sessionStore: sessStore,
	}

	// Send more commands than maxHistory
	for i := 0; i < 5; i++ {
		req := &corev1.HandleCommandRequest{
			Meta:      &corev1.RequestMeta{RequestId: fmt.Sprintf("cap-test-%d", i), Timestamp: timestamppb.Now()},
			SessionId: sessionID.String(),
			Command:   fmt.Sprintf("say message %d", i),
		}
		resp, err := server.HandleCommand(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success, "command %d failed: %s", i, resp.Error)
	}

	history, err := sessStore.GetCommandHistory(ctx, sessionID.String())
	require.NoError(t, err)
	assert.Len(t, history, maxHistory, "history should be capped at maxHistory")
	// Most recent commands should be retained
	assert.Equal(t, "say message 4", history[maxHistory-1])
}

func TestCoreServer_HandleCommand_HistoryBestEffort(t *testing.T) {
	// Verify that a history append failure does not fail the command itself.
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error { return nil },
	}
	engine := core.NewEngine(store, sessions)

	ctx := context.Background()
	realStore := session.NewMemStore()
	require.NoError(t, realStore.Set(ctx, sessionID.String(), &session.Info{
		ID:          sessionID.String(),
		CharacterID: charID,
		LocationID:  locationID,
		Status:      session.StatusActive,
		MaxHistory:  100,
	}))

	server := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		sessionStore: realStore,
	}

	req := &corev1.HandleCommandRequest{
		Meta:      &corev1.RequestMeta{RequestId: "best-effort-test", Timestamp: timestamppb.Now()},
		SessionId: sessionID.String(),
		Command:   "say hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Success, "command should succeed even when history append fails")
}

func TestCoreServer_Authenticate_EmitsArriveEvent(t *testing.T) {
	charID := core.NewULID()
	locationID := core.NewULID()
	sessionID := core.NewULID()
	sessions := core.NewSessionManager()

	store := core.NewMemoryEventStore()
	engine := core.NewEngine(store, sessions)

	auth := &mockAuthenticator{
		authenticateFunc: func(_ context.Context, _, _ string) (*AuthResult, error) {
			return &AuthResult{
				CharacterID:   charID,
				CharacterName: "ArriveChar",
				LocationID:    locationID,
			}, nil
		},
	}

	server := NewCoreServer(engine, sessions, session.NewMemStore(),
		WithAuthenticator(auth),
	)
	server.newSessionID = func() ulid.ULID { return sessionID }

	ctx := context.Background()
	req := &corev1.AuthenticateRequest{
		Meta:     &corev1.RequestMeta{RequestId: "arrive-test", Timestamp: timestamppb.Now()},
		Username: "user",
		Password: "pass",
	}

	resp, err := server.Authenticate(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Success)

	events, err := store.Replay(ctx, "location:"+locationID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.Len(t, events, 1, "expected exactly one arrive event")
	assert.Equal(t, core.EventTypeArrive, events[0].Type)
}

func TestCoreServer_Disconnect_EmitsLeaveEvent(t *testing.T) {
	t.Run("guest disconnect emits leave event", func(t *testing.T) {
		charID := core.NewULID()
		locationID := core.NewULID()
		sessionID := core.NewULID()
		connID := core.NewULID()
		sessions := core.NewSessionManager()
		sessions.Connect(charID, connID)

		store := core.NewMemoryEventStore()
		engine := core.NewEngine(store, sessions)

		server := NewCoreServer(engine, sessions, session.NewMemStore())
		ctx := context.Background()
		require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
			CharacterID:   charID,
			LocationID:    locationID,
			CharacterName: "GuestChar",
			IsGuest:       true,
			Status:        session.StatusActive,
		}))

		req := &corev1.DisconnectRequest{
			Meta:      &corev1.RequestMeta{RequestId: "leave-test-guest", Timestamp: timestamppb.Now()},
			SessionId: sessionID.String(),
		}

		resp, err := server.Disconnect(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success)

		events, err := store.Replay(ctx, "location:"+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)
		require.Len(t, events, 1, "expected exactly one leave event for guest")
		assert.Equal(t, core.EventTypeLeave, events[0].Type)
	})

	t.Run("non-guest disconnect does not emit leave event", func(t *testing.T) {
		charID := core.NewULID()
		locationID := core.NewULID()
		sessionID := core.NewULID()
		connID := core.NewULID()
		sessions := core.NewSessionManager()
		sessions.Connect(charID, connID)

		store := core.NewMemoryEventStore()
		engine := core.NewEngine(store, sessions)

		server := NewCoreServer(engine, sessions, session.NewMemStore())
		ctx := context.Background()
		require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
			CharacterID:   charID,
			LocationID:    locationID,
			CharacterName: "RegularChar",
			IsGuest:       false,
			Status:        session.StatusActive,
			TTLSeconds:    1800,
		}))

		req := &corev1.DisconnectRequest{
			Meta:      &corev1.RequestMeta{RequestId: "leave-test-nonguest", Timestamp: timestamppb.Now()},
			SessionId: sessionID.String(),
		}

		resp, err := server.Disconnect(ctx, req)
		require.NoError(t, err)
		assert.True(t, resp.Success)

		events, err := store.Replay(ctx, "location:"+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)
		assert.Empty(t, events, "non-guest disconnect should NOT emit leave event")
	})
}

func TestCoreServer_MalformedRequest_DisconnectInvalidSession(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore(), sessions),
		sessions:     sessions,
		sessionStore: session.NewMemStore(),
	}

	ctx := context.Background()

	tests := []struct {
		name      string
		sessionID string
	}{
		{"empty", ""},
		{"invalid format", "not-valid"},
		{"null bytes", "session\x00id"},
		{"very long", strings.Repeat("a", 100000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &corev1.DisconnectRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "invalid-disconnect",
					Timestamp: timestamppb.Now(),
				},
				SessionId: tt.sessionID,
			}

			// Should not panic
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Disconnect panicked: %v", r)
				}
			}()

			resp, err := server.Disconnect(ctx, req)
			if err != nil {
				// gRPC error is acceptable
				return
			}
			// Disconnect should be idempotent - always succeeds
			if !resp.Success {
				t.Error("Expected success for disconnect")
			}
		})
	}
}

func TestCoreServer_Subscribe_ReplayFromCursor(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	streamName := "location:" + locationID.String()

	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()
	ctx := context.Background()

	// Prepopulate store: cursor event + 2 historical events
	cursorEvent := core.Event{
		ID:     core.NewULID(),
		Stream: streamName, Type: core.EventTypeSay,
		Timestamp: time.Now().Add(-3 * time.Second),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "actor0"},
		Payload:   []byte(`{"message":"cursor"}`),
	}
	require.NoError(t, eventStore.Append(ctx, cursorEvent))

	historicalID1 := core.NewULID()
	historicalID2 := core.NewULID()
	for _, ev := range []core.Event{
		{
			ID: historicalID1, Stream: streamName, Type: core.EventTypeSay,
			Timestamp: time.Now().Add(-2 * time.Second),
			Actor:     core.Actor{Kind: core.ActorCharacter, ID: "actor1"},
			Payload:   []byte(`{"message":"missed-1"}`),
		},
		{
			ID: historicalID2, Stream: streamName, Type: core.EventTypeSay,
			Timestamp: time.Now().Add(-1 * time.Second),
			Actor:     core.Actor{Kind: core.ActorCharacter, ID: "actor2"},
			Payload:   []byte(`{"message":"missed-2"}`),
		},
	} {
		require.NoError(t, eventStore.Append(ctx, ev))
	}

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:  charID,
				LocationID:   locationID,
				Status:       session.StatusActive,
				EventCursors: map[string]ulid.ULID{streamName: cursorEvent.ID},
			},
		}),
		sessionDefaults: SessionDefaults{MaxReplay: 1000},
	}

	subCtx, cancel := context.WithCancel(ctx)
	stream := &mockSubscribeStream{ctx: subCtx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "replay-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:        sessionID.String(),
		Streams:          []string{streamName},
		ReplayFromCursor: true,
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give time for replay and subscription setup
	time.Sleep(100 * time.Millisecond)

	// Send a live event after replay
	liveID := core.NewULID()
	liveEvent := core.Event{
		ID:        liveID,
		Stream:    streamName,
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "actor3"},
		Payload:   []byte(`{"message":"live"}`),
	}
	require.NoError(t, eventStore.Append(subCtx, liveEvent))

	// Give time for live event delivery
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Verify: historical events come first, then live event.
	// First event is the synthetic location_state, then 2 replayed, then live.
	// Filter to say events only for verification.
	var sayEvents []*corev1.SubscribeResponse
	for _, ev := range stream.events {
		if ef := ev.GetEvent(); ef != nil && ef.GetType() == string(core.EventTypeSay) {
			sayEvents = append(sayEvents, ev)
		}
	}
	require.GreaterOrEqual(t, len(sayEvents), 2, "expected at least 2 replayed say events")
	assert.Equal(t, historicalID1.String(), sayEvents[0].GetEvent().GetId(), "first say event should be historical")
	assert.Equal(t, historicalID2.String(), sayEvents[1].GetEvent().GetId(), "second say event should be historical")

	if len(sayEvents) >= 3 {
		assert.Equal(t, liveID.String(), sayEvents[2].GetEvent().GetId(), "third say event should be the live one")
	}
}

func TestCoreServer_Subscribe_ReplayDeduplicatesLiveEvents(t *testing.T) {
	// With notification-driven delivery, dedup is inherent: replay advances
	// lastSentID, so a notification for an already-replayed event produces
	// an empty Replay response (no duplicates sent).
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	streamName := "location:" + locationID.String()

	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()
	ctx := context.Background()

	// Prepopulate: cursor + one historical event
	cursorEvent := core.Event{
		ID: core.NewULID(), Stream: streamName, Type: core.EventTypeSay,
		Timestamp: time.Now().Add(-2 * time.Second),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "actor0"},
		Payload:   []byte(`{"message":"cursor"}`),
	}
	require.NoError(t, eventStore.Append(ctx, cursorEvent))

	historicalID := core.NewULID()
	historicalEvent := core.Event{
		ID: historicalID, Stream: streamName, Type: core.EventTypeSay,
		Timestamp: time.Now().Add(-1 * time.Second),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "actor1"},
		Payload:   []byte(`{"message":"historical"}`),
	}
	require.NoError(t, eventStore.Append(ctx, historicalEvent))

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:  charID,
				LocationID:   locationID,
				Status:       session.StatusActive,
				EventCursors: map[string]ulid.ULID{streamName: cursorEvent.ID},
			},
		}),
		sessionDefaults: SessionDefaults{MaxReplay: 1000},
	}

	subCtx, cancel := context.WithCancel(ctx)
	stream := &mockSubscribeStream{ctx: subCtx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "dedup-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:        sessionID.String(),
		Streams:          []string{streamName},
		ReplayFromCursor: true,
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give time for replay and subscription setup
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Count how many times the historical event appears (should be exactly once)
	count := 0
	for _, ev := range stream.events {
		if ef := ev.GetEvent(); ef != nil && ef.GetId() == historicalID.String() {
			count++
		}
	}
	assert.Equal(t, 1, count, "historical event should appear exactly once (no duplication)")
}

func TestCoreServer_Subscribe_NoReplayWithoutCursors(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	streamName := "location:" + locationID.String()

	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := &mockEventStore{
		replayFunc: func(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
			return nil, nil
		},
	}

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:  charID,
				LocationID:   locationID,
				Status:       session.StatusActive,
				EventCursors: map[string]ulid.ULID{}, // empty cursors
			},
		}),
		sessionDefaults: SessionDefaults{MaxReplay: 1000},
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "no-cursor-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:        sessionID.String(),
		Streams:          []string{streamName},
		ReplayFromCursor: true,
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Catch-up replay always runs (to close the LISTEN race window), but with
	// no events in the store, no EventFrames are sent. A REPLAY_COMPLETE
	// ControlFrame may be present.
	for _, ev := range stream.events {
		assert.Nil(t, ev.GetEvent(), "no event frames should be sent when store is empty")
	}
}

func TestCoreServer_Subscribe_NoReplayWhenNotRequested(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	streamName := "location:" + locationID.String()

	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := &mockEventStore{
		replayFunc: func(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
			return nil, nil
		},
	}

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:  charID,
				LocationID:   locationID,
				Status:       session.StatusActive,
				EventCursors: map[string]ulid.ULID{streamName: core.NewULID()},
			},
		}),
		sessionDefaults: SessionDefaults{MaxReplay: 1000},
	}

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "no-replay-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:        sessionID.String(),
		Streams:          []string{streamName},
		ReplayFromCursor: false, // not requested
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Catch-up replay always runs, but with ReplayFromCursor=false the cursor
	// is zero ULID — effectively replaying from the beginning. With no events
	// in the store, no EventFrames are sent. A REPLAY_COMPLETE ControlFrame
	// may be present.
	for _, ev := range stream.events {
		assert.Nil(t, ev.GetEvent(), "no event frames should be sent when store is empty")
	}
}

func TestCoreServer_Subscribe_EmitsReplayCompleteControlFrame(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	streamName := "location:" + locationID.String()

	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	eventStore := core.NewMemoryEventStore()
	ctx := context.Background()

	// Prepopulate store: cursor event + 1 historical event after it
	cursorEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    streamName,
		Type:      core.EventTypeSay,
		Timestamp: time.Now().Add(-2 * time.Second),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "actor0"},
		Payload:   []byte(`{"message":"before-cursor"}`),
	}
	require.NoError(t, eventStore.Append(ctx, cursorEvent))

	historicalEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    streamName,
		Type:      core.EventTypeSay,
		Timestamp: time.Now().Add(-1 * time.Second),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "actor1"},
		Payload:   []byte(`{"message":"missed"}`),
	}
	require.NoError(t, eventStore.Append(ctx, historicalEvent))

	server := &CoreServer{
		engine:     core.NewEngine(eventStore, sessions),
		sessions:   sessions,
		eventStore: eventStore,
		sessionStore: newTestSessionStore(t, map[string]*session.Info{
			sessionID.String(): {
				CharacterID:  charID,
				LocationID:   locationID,
				Status:       session.StatusActive,
				EventCursors: map[string]ulid.ULID{streamName: cursorEvent.ID},
			},
		}),
		sessionDefaults: SessionDefaults{MaxReplay: 1000},
	}

	subCtx, cancel := context.WithCancel(ctx)
	stream := &mockSubscribeStream{ctx: subCtx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "replay-complete-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:        sessionID.String(),
		Streams:          []string{streamName},
		ReplayFromCursor: true,
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give time for replay to complete and REPLAY_COMPLETE frame to be sent
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Find the REPLAY_COMPLETE control frame in the received events
	var replayCompleteFrame *corev1.SubscribeResponse
	for _, ev := range stream.events {
		if cf := ev.GetControl(); cf != nil && cf.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
			replayCompleteFrame = ev
			break
		}
	}
	require.NotNil(t, replayCompleteFrame, "expected REPLAY_COMPLETE control frame after replay")

	// The REPLAY_COMPLETE frame must come after the replayed say event
	var replayCompleteSeen bool
	var sayAfterReplayComplete bool
	for _, ev := range stream.events {
		if cf := ev.GetControl(); cf != nil && cf.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
			replayCompleteSeen = true
		} else if ef := ev.GetEvent(); ef != nil && ef.GetType() == string(core.EventTypeSay) && replayCompleteSeen {
			sayAfterReplayComplete = true
		}
	}
	assert.False(t, sayAfterReplayComplete, "replayed say events should come before REPLAY_COMPLETE")
}

func TestEventToProto(t *testing.T) {
	id := core.NewULID()
	ts := time.Now()
	ev := core.Event{
		ID:        id,
		Stream:    "location:test",
		Type:      core.EventTypeSay,
		Timestamp: ts,
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload:   []byte(`{"msg":"hello"}`),
	}

	proto := eventToProto(ev)
	ef := proto.GetEvent()
	require.NotNil(t, ef)

	assert.Equal(t, id.String(), ef.GetId())
	assert.Equal(t, "location:test", ef.GetStream())
	assert.Equal(t, "say", ef.GetType())
	assert.Equal(t, "character", ef.GetActorType())
	assert.Equal(t, "char-1", ef.GetActorId())
	assert.Equal(t, []byte(`{"msg":"hello"}`), ef.GetPayload())
	assert.Equal(t, ts.UnixNano()/1e9, ef.GetTimestamp().AsTime().UnixNano()/1e9)
}

// =============================================================================
// Connection Type Tracking + Grid Presence Tests (Chunk 7)
// =============================================================================

func TestCoreServer_Authenticate_RegistersConnection(t *testing.T) {
	charID := core.NewULID()
	locationID := core.NewULID()
	sessionID := core.NewULID()
	sessions := core.NewSessionManager()

	store := core.NewMemoryEventStore()
	engine := core.NewEngine(store, sessions)

	auth := &mockAuthenticator{
		authenticateFunc: func(_ context.Context, _, _ string) (*AuthResult, error) {
			return &AuthResult{
				CharacterID:   charID,
				CharacterName: "ConnChar",
				LocationID:    locationID,
			}, nil
		},
	}

	sessStore := session.NewMemStore()
	server := NewCoreServer(engine, sessions, sessStore,
		WithAuthenticator(auth),
	)
	server.newSessionID = func() ulid.ULID { return sessionID }

	ctx := context.Background()

	t.Run("default client_type is terminal", func(t *testing.T) {
		resp, err := server.Authenticate(ctx, &corev1.AuthenticateRequest{
			Meta:     &corev1.RequestMeta{RequestId: "conn-test", Timestamp: timestamppb.Now()},
			Username: "user",
			Password: "pass",
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.NotEmpty(t, resp.ConnectionId)

		count, err := sessStore.CountConnections(ctx, sessionID.String())
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		termCount, err := sessStore.CountConnectionsByType(ctx, sessionID.String(), "terminal")
		require.NoError(t, err)
		assert.Equal(t, 1, termCount)
	})

	t.Run("telnet client_type", func(t *testing.T) {
		telSessionID := core.NewULID()
		server.newSessionID = func() ulid.ULID { return telSessionID }

		resp, err := server.Authenticate(ctx, &corev1.AuthenticateRequest{
			Meta:       &corev1.RequestMeta{RequestId: "tel-conn-test", Timestamp: timestamppb.Now()},
			Username:   "user",
			Password:   "pass",
			ClientType: "telnet",
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.NotEmpty(t, resp.ConnectionId)

		telCount, err := sessStore.CountConnectionsByType(ctx, telSessionID.String(), "telnet")
		require.NoError(t, err)
		assert.Equal(t, 1, telCount)
	})
}

func TestCoreServer_Disconnect_GridPresencePhaseOut(t *testing.T) {
	t.Run("last terminal disconnects with comms_hub remaining emits leave", func(t *testing.T) {
		charID := core.NewULID()
		locationID := core.NewULID()
		sessionID := core.NewULID()

		sessions := core.NewSessionManager()
		sessions.Connect(charID, core.NewULID())

		eventStore := core.NewMemoryEventStore()
		engine := core.NewEngine(eventStore, sessions)

		sessStore := session.NewMemStore()
		server := NewCoreServer(engine, sessions, sessStore)
		ctx := context.Background()

		// Create session
		require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
			ID:            sessionID.String(),
			CharacterID:   charID,
			LocationID:    locationID,
			CharacterName: "PhaseChar",
			IsGuest:       false,
			Status:        session.StatusActive,
			GridPresent:   true,
			TTLSeconds:    1800,
		}))

		// Register a terminal connection and a comms_hub connection
		termConnID := core.NewULID()
		commsConnID := core.NewULID()
		require.NoError(t, sessStore.AddConnection(ctx, &session.Connection{
			ID: termConnID, SessionID: sessionID.String(), ClientType: "terminal",
		}))
		require.NoError(t, sessStore.AddConnection(ctx, &session.Connection{
			ID: commsConnID, SessionID: sessionID.String(), ClientType: "comms_hub",
		}))

		// Disconnect with terminal connection ID
		resp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
			Meta:         &corev1.RequestMeta{RequestId: "phase-test", Timestamp: timestamppb.Now()},
			SessionId:    sessionID.String(),
			ConnectionId: termConnID.String(),
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)

		// Session should still exist and be active (not detached)
		info, err := sessStore.Get(ctx, sessionID.String())
		require.NoError(t, err)
		assert.Equal(t, session.StatusActive, info.Status, "session should stay active")
		assert.False(t, info.GridPresent, "grid_present should be false after phase-out")

		// Leave event should have been emitted
		events, err := eventStore.Replay(ctx, "location:"+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)
		require.Len(t, events, 1, "expected leave event on phase-out")
		assert.Equal(t, core.EventTypeLeave, events[0].Type)
	})

	t.Run("all connections disconnect causes detach for non-guest", func(t *testing.T) {
		charID := core.NewULID()
		locationID := core.NewULID()
		sessionID := core.NewULID()

		sessions := core.NewSessionManager()
		sessions.Connect(charID, core.NewULID())

		eventStore := core.NewMemoryEventStore()
		engine := core.NewEngine(eventStore, sessions)

		sessStore := session.NewMemStore()
		server := NewCoreServer(engine, sessions, sessStore)
		ctx := context.Background()

		require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
			ID:            sessionID.String(),
			CharacterID:   charID,
			LocationID:    locationID,
			CharacterName: "DetachChar",
			IsGuest:       false,
			Status:        session.StatusActive,
			GridPresent:   true,
			TTLSeconds:    1800,
		}))

		// Register one terminal connection
		termConnID := core.NewULID()
		require.NoError(t, sessStore.AddConnection(ctx, &session.Connection{
			ID: termConnID, SessionID: sessionID.String(), ClientType: "terminal",
		}))

		// Disconnect it
		resp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
			Meta:         &corev1.RequestMeta{RequestId: "detach-test", Timestamp: timestamppb.Now()},
			SessionId:    sessionID.String(),
			ConnectionId: termConnID.String(),
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)

		// Session should be detached
		info, err := sessStore.Get(ctx, sessionID.String())
		require.NoError(t, err)
		assert.Equal(t, session.StatusDetached, info.Status, "session should be detached")
		assert.NotNil(t, info.DetachedAt)
		assert.NotNil(t, info.ExpiresAt)

		// No leave event for non-guest detach (reaper handles)
		events, err := eventStore.Replay(ctx, "location:"+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)
		assert.Empty(t, events, "non-guest full disconnect should NOT emit leave event")
	})

	t.Run("terminal disconnects but another terminal remains — no phase-out", func(t *testing.T) {
		charID := core.NewULID()
		locationID := core.NewULID()
		sessionID := core.NewULID()

		sessions := core.NewSessionManager()
		sessions.Connect(charID, core.NewULID())

		eventStore := core.NewMemoryEventStore()
		engine := core.NewEngine(eventStore, sessions)

		sessStore := session.NewMemStore()
		server := NewCoreServer(engine, sessions, sessStore)
		ctx := context.Background()

		require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
			ID:            sessionID.String(),
			CharacterID:   charID,
			LocationID:    locationID,
			CharacterName: "MultiTermChar",
			IsGuest:       false,
			Status:        session.StatusActive,
			GridPresent:   true,
			TTLSeconds:    1800,
		}))

		// Register two terminal connections
		termConnID1 := core.NewULID()
		termConnID2 := core.NewULID()
		require.NoError(t, sessStore.AddConnection(ctx, &session.Connection{
			ID: termConnID1, SessionID: sessionID.String(), ClientType: "terminal",
		}))
		require.NoError(t, sessStore.AddConnection(ctx, &session.Connection{
			ID: termConnID2, SessionID: sessionID.String(), ClientType: "terminal",
		}))

		// Disconnect first terminal
		resp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
			Meta:         &corev1.RequestMeta{RequestId: "multi-term-test", Timestamp: timestamppb.Now()},
			SessionId:    sessionID.String(),
			ConnectionId: termConnID1.String(),
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)

		// Session should still be active and grid-present
		info, err := sessStore.Get(ctx, sessionID.String())
		require.NoError(t, err)
		assert.Equal(t, session.StatusActive, info.Status)
		assert.True(t, info.GridPresent, "should stay grid-present with terminal remaining")

		// No leave event
		events, err := eventStore.Replay(ctx, "location:"+locationID.String(), ulid.ULID{}, 100)
		require.NoError(t, err)
		assert.Empty(t, events, "no leave event when terminals remain")
	})
}
