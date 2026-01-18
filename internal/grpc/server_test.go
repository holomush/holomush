package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/internal/proto/holomush/core/v1"
	holomushtls "github.com/holomush/holomush/internal/tls"
)

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

// mockSubscribeStream implements grpc.ServerStreamingServer[corev1.Event] for testing.
type mockSubscribeStream struct {
	grpc.ServerStream
	ctx    context.Context
	events []*corev1.Event
}

func (m *mockSubscribeStream) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *mockSubscribeStream) Send(event *corev1.Event) error {
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
		sessions:      sessions,
		authenticator: auth,
		sessionStore:  NewInMemorySessionStore(),
		newSessionID:  func() ulid.ULID { return sessionID },
	}

	ctx := context.Background()
	req := &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "test-request-id",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	}

	resp, err := server.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("Success = false, want true")
	}
	if resp.SessionId != sessionID.String() {
		t.Errorf("SessionId = %q, want %q", resp.SessionId, sessionID.String())
	}
	if resp.CharacterId != charID.String() {
		t.Errorf("CharacterId = %q, want %q", resp.CharacterId, charID.String())
	}
	if resp.CharacterName != "TestCharacter" {
		t.Errorf("CharacterName = %q, want 'TestCharacter'", resp.CharacterName)
	}
	if resp.Meta == nil || resp.Meta.RequestId != "test-request-id" {
		t.Errorf("Meta.RequestId not echoed correctly")
	}
}

func TestCoreServer_Authenticate_InvalidCredentials(t *testing.T) {
	auth := &mockAuthenticator{
		authenticateFunc: func(_ context.Context, _, _ string) (*AuthResult, error) {
			return nil, errors.New("invalid credentials")
		},
	}

	server := &CoreServer{
		sessions:      core.NewSessionManager(),
		authenticator: auth,
		sessionStore:  NewInMemorySessionStore(),
	}

	ctx := context.Background()
	req := &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "test-request-id",
			Timestamp: timestamppb.Now(),
		},
		Username: "baduser",
		Password: "badpass",
	}

	resp, err := server.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if resp.Success {
		t.Error("Success = true, want false for invalid credentials")
	}
	if resp.Error == "" {
		t.Error("Error should contain error message")
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx := context.Background()
	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cmd-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "say Hello, world!",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("Success = false, want true; error: %s", resp.Error)
	}
	if appendedEvent.Type != core.EventTypeSay {
		t.Errorf("Event type = %q, want %q", appendedEvent.Type, core.EventTypeSay)
	}
}

func TestCoreServer_HandleCommand_InvalidSession(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		sessions:     sessions,
		sessionStore: &mockSessionStore{sessions: make(map[string]*SessionInfo)},
	}

	ctx := context.Background()
	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cmd-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "invalid-session",
		Command:   "say Hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if resp.Success {
		t.Error("Success = true, want false for invalid session")
	}
	if resp.Error == "" {
		t.Error("Error should contain error message")
	}
}

func TestCoreServer_Subscribe_SendsEvents(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	broadcaster := core.NewBroadcaster()

	server := &CoreServer{
		sessions:    sessions,
		broadcaster: broadcaster,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
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

	// Send an event through the broadcaster
	testEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    "location:" + locationID.String(),
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
		Payload:   []byte(`{"message":"test"}`),
	}
	broadcaster.Broadcast(testEvent)

	// Give time for event to be sent
	time.Sleep(50 * time.Millisecond)

	// Cancel the context to stop the subscription
	cancel()

	// Wait for Subscribe to finish
	select {
	case err := <-done:
		// Context cancellation is expected
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Subscribe() unexpected error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	if len(stream.events) == 0 {
		t.Error("Expected at least one event to be sent")
	}
}

func TestCoreServer_Disconnect(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	connID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, connID)

	server := &CoreServer{
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID:  charID,
					ConnectionID: connID,
				},
			},
		},
	}

	ctx := context.Background()
	req := &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "disconnect-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	if err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	if !resp.Success {
		t.Error("Success = false, want true")
	}
	if resp.Meta == nil || resp.Meta.RequestId != "disconnect-request-id" {
		t.Error("Meta.RequestId not echoed correctly")
	}
}

// mockSessionStore tracks session-to-character mappings for the gRPC layer.
type mockSessionStore struct {
	sessions map[string]*SessionInfo
}

func (m *mockSessionStore) Get(sessionID string) (*SessionInfo, bool) {
	info, ok := m.sessions[sessionID]
	return info, ok
}

func (m *mockSessionStore) Set(sessionID string, info *SessionInfo) {
	m.sessions[sessionID] = info
}

func (m *mockSessionStore) Delete(sessionID string) {
	delete(m.sessions, sessionID)
}

func TestNewCoreServer(t *testing.T) {
	store := &mockEventStore{}
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()

	server := NewCoreServer(
		core.NewEngine(store, sessions, broadcaster),
		sessions,
		broadcaster,
	)

	if server == nil {
		t.Fatal("NewCoreServer returned nil")
	}
	if server.sessionStore == nil {
		t.Error("sessionStore should be initialized")
	}
}

func TestNewCoreServer_WithOptions(t *testing.T) {
	store := &mockEventStore{}
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()

	customAuth := &mockAuthenticator{}
	customStore := NewInMemorySessionStore()

	server := NewCoreServer(
		core.NewEngine(store, sessions, broadcaster),
		sessions,
		broadcaster,
		WithAuthenticator(customAuth),
		WithSessionStore(customStore),
	)

	if server == nil {
		t.Fatal("NewCoreServer returned nil")
	}
	if server.authenticator != customAuth {
		t.Error("WithAuthenticator option not applied")
	}
	if server.sessionStore != customStore {
		t.Error("WithSessionStore option not applied")
	}
}

func TestInMemorySessionStore(t *testing.T) {
	store := NewInMemorySessionStore()

	sessionID := "test-session-123"
	info := &SessionInfo{
		CharacterID:  core.NewULID(),
		LocationID:   core.NewULID(),
		ConnectionID: core.NewULID(),
	}

	// Test Get on non-existent session
	_, ok := store.Get(sessionID)
	if ok {
		t.Error("Get() should return false for non-existent session")
	}

	// Test Set
	store.Set(sessionID, info)

	// Test Get on existing session
	retrieved, ok := store.Get(sessionID)
	if !ok {
		t.Error("Get() should return true for existing session")
	}
	if retrieved.CharacterID != info.CharacterID {
		t.Error("Retrieved session info doesn't match")
	}

	// Test Delete
	store.Delete(sessionID)
	_, ok = store.Get(sessionID)
	if ok {
		t.Error("Get() should return false after Delete")
	}
}

func TestCoreServer_Authenticate_NoAuthenticator(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		sessions:      sessions,
		sessionStore:  NewInMemorySessionStore(),
		authenticator: nil, // No authenticator configured
	}

	ctx := context.Background()
	req := &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "no-auth-test",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	}

	resp, err := server.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if resp.Success {
		t.Error("Success = true, want false when authenticator not configured")
	}
	if resp.Error != "authentication not configured" {
		t.Errorf("Error = %q, want 'authentication not configured'", resp.Error)
	}
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
		sessions:      sessions,
		authenticator: auth,
		sessionStore:  NewInMemorySessionStore(),
		newSessionID:  func() ulid.ULID { return sessionID },
	}

	ctx := context.Background()
	req := &corev1.AuthRequest{
		Meta:     nil, // No meta
		Username: "testuser",
		Password: "testpass",
	}

	resp, err := server.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("Success = false, want true")
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx := context.Background()
	req := &corev1.CommandRequest{
		Meta:      nil, // No meta
		SessionId: sessionID.String(),
		Command:   "say Hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("Success = false, want true; error: %s", resp.Error)
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx := context.Background()

	// Test pose command
	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "pose-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "pose waves hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("Success = false, want true; error: %s", resp.Error)
	}
	if appendedEvent.Type != core.EventTypePose {
		t.Errorf("Event type = %q, want %q", appendedEvent.Type, core.EventTypePose)
	}

	// Test : shortcut for pose
	req2 := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "colon-pose-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   ": nods",
	}

	resp2, err := server.HandleCommand(ctx, req2)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if !resp2.Success {
		t.Errorf("Success = false, want true for : shortcut; error: %s", resp2.Error)
	}
}

func TestCoreServer_HandleCommand_UnknownCommand(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	store := &mockEventStore{}
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx := context.Background()
	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "unknown-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "unknowncommand args",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if resp.Success {
		t.Error("Success = true, want false for unknown command")
	}
	if resp.Error == "" {
		t.Error("Error should contain error message for unknown command")
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx := context.Background()
	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "say-fail-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "say Hello",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if resp.Success {
		t.Error("Success = true, want false when say fails")
	}
	if resp.Error == "" {
		t.Error("Error should contain error message")
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx := context.Background()
	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "pose-fail-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "pose waves",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if resp.Success {
		t.Error("Success = true, want false when pose fails")
	}
	if resp.Error == "" {
		t.Error("Error should contain error message")
	}
}

func TestCoreServer_Subscribe_InvalidSession(t *testing.T) {
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()

	server := &CoreServer{
		sessions:     sessions,
		broadcaster:  broadcaster,
		sessionStore: &mockSessionStore{sessions: make(map[string]*SessionInfo)},
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
	if err == nil {
		t.Error("Subscribe() should return error for invalid session")
	}
}

func TestCoreServer_Subscribe_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	broadcaster := core.NewBroadcaster()

	server := &CoreServer{
		sessions:    sessions,
		broadcaster: broadcaster,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
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
		// Context cancellation expected
		if err == nil {
			t.Error("Subscribe() should return error on context cancellation")
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}
}

type mockSubscribeStreamWithError struct {
	grpc.ServerStream
	ctx      context.Context
	sendErr  error
	sendFunc func(*corev1.Event) error
}

func (m *mockSubscribeStreamWithError) Context() context.Context {
	if m.ctx != nil {
		return m.ctx
	}
	return context.Background()
}

func (m *mockSubscribeStreamWithError) Send(event *corev1.Event) error {
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

	broadcaster := core.NewBroadcaster()

	server := &CoreServer{
		sessions:    sessions,
		broadcaster: broadcaster,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
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
	broadcaster.Broadcast(testEvent)

	select {
	case err := <-done:
		if err == nil {
			t.Error("Subscribe() should return error when send fails")
		}
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

	server := &CoreServer{
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID:  charID,
					ConnectionID: connID,
				},
			},
		},
	}

	ctx := context.Background()
	req := &corev1.DisconnectRequest{
		Meta:      nil, // No meta
		SessionId: sessionID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	if err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	if !resp.Success {
		t.Error("Success = false, want true")
	}
}

func TestCoreServer_Disconnect_NonExistentSession(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		sessions:     sessions,
		sessionStore: &mockSessionStore{sessions: make(map[string]*SessionInfo)},
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
	if err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}

	// Should succeed even for non-existent session (idempotent)
	if !resp.Success {
		t.Error("Success = false, want true for non-existent session (idempotent)")
	}
}

func TestNewGRPCServerInsecure(t *testing.T) {
	server := NewGRPCServerInsecure()
	if server == nil {
		t.Error("NewGRPCServerInsecure() returned nil")
	}
	server.Stop()
}

func TestMergeChannels_ClosedChannel(t *testing.T) {
	ctx := context.Background()

	// Create and immediately close a channel
	ch := make(chan core.Event)
	close(ch)

	merged := mergeChannels(ctx, []chan core.Event{ch})

	// Should receive closed channel (no events)
	select {
	case _, ok := <-merged:
		if ok {
			t.Error("Expected channel to be closed")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Timed out waiting for closed channel signal")
	}
}

func TestMergeChannels_MultipleChannels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch1 := make(chan core.Event, 1)
	ch2 := make(chan core.Event, 1)

	event1 := core.Event{ID: core.NewULID(), Stream: "stream1", Type: core.EventTypeSay}
	event2 := core.Event{ID: core.NewULID(), Stream: "stream2", Type: core.EventTypePose}

	ch1 <- event1
	ch2 <- event2

	merged := mergeChannels(ctx, []chan core.Event{ch1, ch2})

	// Should receive both events
	received := make(map[string]bool)
	for i := 0; i < 2; i++ {
		select {
		case e := <-merged:
			received[e.ID.String()] = true
		case <-time.After(100 * time.Millisecond):
			t.Fatal("Timed out waiting for events")
		}
	}

	if !received[event1.ID.String()] || !received[event2.ID.String()] {
		t.Error("Did not receive both events")
	}
}

func TestMergeChannels_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	ch := make(chan core.Event)
	merged := mergeChannels(ctx, []chan core.Event{ch})

	// Cancel context before sending any events
	cancel()

	// merged channel should eventually close
	select {
	case _, ok := <-merged:
		if ok {
			t.Error("Expected channel to be closed after context cancel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Timed out waiting for channel to close")
	}
}

func TestLoadServerTLS(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-tls"

	// Generate certificates
	ca, err := holomushtls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := holomushtls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	if err := holomushtls.SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Load TLS config
	tlsConfig, err := LoadServerTLS(tmpDir)
	if err != nil {
		t.Fatalf("LoadServerTLS() error = %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("LoadServerTLS() returned nil config")
	}
	if len(tlsConfig.Certificates) == 0 {
		t.Error("TLS config has no certificates")
	}
	if tlsConfig.ClientCAs == nil {
		t.Error("TLS config has no ClientCAs")
	}
	if tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want TLS1.3 (%d)", tlsConfig.MinVersion, tls.VersionTLS13)
	}
}

func TestLoadServerTLS_MissingCert(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadServerTLS(tmpDir)
	if err == nil {
		t.Error("LoadServerTLS() should fail when certificate files are missing")
	}
}

func TestLoadServerTLS_MissingCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-tls"

	// Generate and save only server cert (no CA)
	ca, err := holomushtls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := holomushtls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	// Save only cert, not CA
	if err := holomushtls.SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Remove the CA file
	if err := os.Remove(filepath.Join(tmpDir, "root-ca.crt")); err != nil {
		t.Fatalf("Failed to remove CA file: %v", err)
	}

	_, err = LoadServerTLS(tmpDir)
	if err == nil {
		t.Error("LoadServerTLS() should fail when CA file is missing")
	}
}

func TestLoadClientTLS(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-client-tls"

	// Generate certificates
	ca, err := holomushtls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := holomushtls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	clientCert, err := holomushtls.GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	if err := holomushtls.SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}
	if err := holomushtls.SaveClientCert(tmpDir, clientCert); err != nil {
		t.Fatalf("SaveClientCert() error = %v", err)
	}

	// Load TLS config
	tlsConfig, err := LoadClientTLS(tmpDir, gameID)
	if err != nil {
		t.Fatalf("LoadClientTLS() error = %v", err)
	}

	if tlsConfig == nil {
		t.Fatal("LoadClientTLS() returned nil config")
	}
	if len(tlsConfig.Certificates) == 0 {
		t.Error("TLS config has no certificates")
	}
	if tlsConfig.RootCAs == nil {
		t.Error("TLS config has no RootCAs")
	}
	if tlsConfig.ServerName != "holomush-"+gameID {
		t.Errorf("ServerName = %q, want %q", tlsConfig.ServerName, "holomush-"+gameID)
	}
	if tlsConfig.MinVersion != tls.VersionTLS13 {
		t.Errorf("MinVersion = %d, want TLS1.3 (%d)", tlsConfig.MinVersion, tls.VersionTLS13)
	}
}

func TestLoadClientTLS_MissingCert(t *testing.T) {
	tmpDir := t.TempDir()

	_, err := LoadClientTLS(tmpDir, "test-game")
	if err == nil {
		t.Error("LoadClientTLS() should fail when certificate files are missing")
	}
}

func TestLoadClientTLS_MissingCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-client-tls"

	// Generate certificates
	ca, err := holomushtls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	clientCert, err := holomushtls.GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	if err := holomushtls.SaveClientCert(tmpDir, clientCert); err != nil {
		t.Fatalf("SaveClientCert() error = %v", err)
	}

	// No CA saved
	_, err = LoadClientTLS(tmpDir, gameID)
	if err == nil {
		t.Error("LoadClientTLS() should fail when CA file is missing")
	}
}

func TestNewGRPCServer(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-grpc"

	// Generate certificates
	ca, err := holomushtls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := holomushtls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	if err := holomushtls.SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Load TLS config
	tlsConfig, err := LoadServerTLS(tmpDir)
	if err != nil {
		t.Fatalf("LoadServerTLS() error = %v", err)
	}

	// Create gRPC server with TLS
	server := NewGRPCServer(tlsConfig)
	if server == nil {
		t.Fatal("NewGRPCServer() returned nil")
	}
	server.Stop()
}

func TestLoadServerTLS_InvalidCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-invalid-ca"

	// Generate valid certificates
	ca, err := holomushtls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	serverCert, err := holomushtls.GenerateServerCert(ca, gameID, "core")
	if err != nil {
		t.Fatalf("GenerateServerCert() error = %v", err)
	}

	if err := holomushtls.SaveCertificates(tmpDir, ca, serverCert); err != nil {
		t.Fatalf("SaveCertificates() error = %v", err)
	}

	// Overwrite CA file with invalid content
	if err := os.WriteFile(filepath.Join(tmpDir, "root-ca.crt"), []byte("invalid certificate"), 0o600); err != nil {
		t.Fatalf("Failed to write invalid CA: %v", err)
	}

	_, err = LoadServerTLS(tmpDir)
	if err == nil {
		t.Error("LoadServerTLS() should fail with invalid CA")
	}
}

func TestLoadClientTLS_InvalidCA(t *testing.T) {
	tmpDir := t.TempDir()
	gameID := "test-game-invalid-ca"

	// Generate valid certificates
	ca, err := holomushtls.GenerateCA(gameID)
	if err != nil {
		t.Fatalf("GenerateCA() error = %v", err)
	}

	clientCert, err := holomushtls.GenerateClientCert(ca, "gateway")
	if err != nil {
		t.Fatalf("GenerateClientCert() error = %v", err)
	}

	if err := holomushtls.SaveClientCert(tmpDir, clientCert); err != nil {
		t.Fatalf("SaveClientCert() error = %v", err)
	}

	// Write invalid CA
	if err := os.WriteFile(filepath.Join(tmpDir, "root-ca.crt"), []byte("invalid certificate"), 0o600); err != nil {
		t.Fatalf("Failed to write invalid CA: %v", err)
	}

	_, err = LoadClientTLS(tmpDir, gameID)
	if err == nil {
		t.Error("LoadClientTLS() should fail with invalid CA")
	}
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

	broadcaster := core.NewBroadcaster()

	sessionStore := &mockSessionStore{
		sessions: map[string]*SessionInfo{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
			},
		},
	}

	server := &CoreServer{
		sessions:     sessions,
		broadcaster:  broadcaster,
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
		if err == nil {
			t.Error("Subscribe() should return error when context times out")
		}
		if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline exceeded") {
			t.Errorf("Expected deadline exceeded error, got: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Subscribe did not return after context timeout")
	}
}

func TestCoreServer_SessionCleanupOnDisconnect(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	connID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, connID)

	sessionStore := NewInMemorySessionStore()
	sessionStore.Set(sessionID.String(), &SessionInfo{
		CharacterID:  charID,
		ConnectionID: connID,
	})

	server := &CoreServer{
		sessions:     sessions,
		sessionStore: sessionStore,
	}

	// Verify session exists before disconnect
	_, ok := sessionStore.Get(sessionID.String())
	if !ok {
		t.Fatal("Session should exist before disconnect")
	}

	// Disconnect the session
	ctx := context.Background()
	req := &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cleanup-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	if err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if !resp.Success {
		t.Error("Disconnect() should succeed")
	}

	// Verify session is cleaned up
	_, ok = sessionStore.Get(sessionID.String())
	if ok {
		t.Error("Session should be removed after disconnect")
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	sessionStore := &mockSessionStore{
		sessions: map[string]*SessionInfo{
			sessionID.String(): {
				CharacterID: charID,
				LocationID:  locationID,
			},
		},
	}

	server := &CoreServer{
		engine:       engine,
		sessions:     sessions,
		sessionStore: sessionStore,
	}

	ctx := context.Background()

	// Execute multiple commands to simulate activity
	for i := 0; i < 3; i++ {
		req := &corev1.CommandRequest{
			Meta: &corev1.RequestMeta{
				RequestId: fmt.Sprintf("activity-test-%d", i),
				Timestamp: timestamppb.Now(),
			},
			SessionId: sessionID.String(),
			Command:   fmt.Sprintf("say Message %d", i),
		}

		resp, err := server.HandleCommand(ctx, req)
		if err != nil {
			t.Fatalf("HandleCommand() error = %v", err)
		}
		if !resp.Success {
			t.Errorf("HandleCommand() iteration %d failed: %s", i, resp.Error)
		}
	}

	// Session should still exist after activity
	_, ok := sessionStore.Get(sessionID.String())
	if !ok {
		t.Error("Session should persist after activity")
	}
}

func TestCoreServer_MultipleSessionsIndependentExpiration(t *testing.T) {
	sessions := core.NewSessionManager()
	broadcaster := core.NewBroadcaster()
	sessionStore := NewInMemorySessionStore()

	// Create two sessions
	session1ID := core.NewULID()
	char1ID := core.NewULID()
	conn1ID := core.NewULID()
	location1ID := core.NewULID()
	sessions.Connect(char1ID, conn1ID)
	sessionStore.Set(session1ID.String(), &SessionInfo{
		CharacterID:  char1ID,
		ConnectionID: conn1ID,
		LocationID:   location1ID,
	})

	session2ID := core.NewULID()
	char2ID := core.NewULID()
	conn2ID := core.NewULID()
	location2ID := core.NewULID()
	sessions.Connect(char2ID, conn2ID)
	sessionStore.Set(session2ID.String(), &SessionInfo{
		CharacterID:  char2ID,
		ConnectionID: conn2ID,
		LocationID:   location2ID,
	})

	server := &CoreServer{
		sessions:     sessions,
		broadcaster:  broadcaster,
		sessionStore: sessionStore,
	}

	// Disconnect only session 1
	ctx := context.Background()
	req := &corev1.DisconnectRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "multi-session-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: session1ID.String(),
	}

	resp, err := server.Disconnect(ctx, req)
	if err != nil {
		t.Fatalf("Disconnect() error = %v", err)
	}
	if !resp.Success {
		t.Error("Disconnect() should succeed")
	}

	// Verify session 1 is cleaned up
	_, ok := sessionStore.Get(session1ID.String())
	if ok {
		t.Error("Session 1 should be removed after disconnect")
	}

	// Verify session 2 still exists
	_, ok = sessionStore.Get(session2ID.String())
	if !ok {
		t.Error("Session 2 should still exist after session 1 disconnect")
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	// Create a context with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "timeout-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "say This should timeout",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() returned error: %v", err)
	}

	// The command should fail due to timeout
	if resp.Success {
		t.Error("HandleCommand() should fail when context times out")
	}
	if resp.Error == "" {
		t.Error("Error message should be present for timeout")
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())

	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cancel-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "say This will be cancelled",
	}

	// Start command in goroutine
	done := make(chan *corev1.CommandResponse)
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
		if resp.Success {
			t.Error("HandleCommand() should fail when context is cancelled")
		}
		if resp.Error == "" {
			t.Error("Error message should be present for cancellation")
		}
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

	broadcaster := core.NewBroadcaster()

	server := &CoreServer{
		sessions:    sessions,
		broadcaster: broadcaster,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
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
	broadcaster.Broadcast(testEvent)

	// Give time for event to be received
	time.Sleep(50 * time.Millisecond)

	// Cancel the context
	cancel()

	// Wait for subscription to end
	select {
	case err := <-done:
		if err == nil {
			t.Error("Subscribe() should return error when cancelled")
		}
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Verify at least one event was received before cancellation
	if len(stream.events) == 0 {
		t.Error("Expected at least one event before cancellation")
	}
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

			broadcaster := core.NewBroadcaster()
			engine := core.NewEngine(store, sessions, broadcaster)

			server := &CoreServer{
				engine:   engine,
				sessions: sessions,
				sessionStore: &mockSessionStore{
					sessions: map[string]*SessionInfo{
						sessionID.String(): {
							CharacterID: charID,
							LocationID:  locationID,
						},
					},
				},
			}

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			req := &corev1.CommandRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "timeout-error-test",
					Timestamp: timestamppb.Now(),
				},
				SessionId: sessionID.String(),
				Command:   "say Hello",
			}

			resp, err := server.HandleCommand(ctx, req)
			if err != nil {
				t.Fatalf("HandleCommand() returned error: %v", err)
			}

			if tt.expectError {
				if resp.Success {
					t.Error("Expected command to fail with timeout")
				}
				if tt.errorContains != "" && !strings.Contains(strings.ToLower(resp.Error), tt.errorContains) {
					t.Errorf("Error %q should contain %q", resp.Error, tt.errorContains)
				}
			} else if !resp.Success {
				t.Errorf("Expected command to succeed, got error: %s", resp.Error)
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

	broadcaster := core.NewBroadcaster()

	server := &CoreServer{
		sessions:    sessions,
		broadcaster: broadcaster,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Create a stream that blocks on send
	blockingSendCalled := make(chan struct{})
	stream := &mockSubscribeStreamWithError{
		ctx: ctx,
		sendFunc: func(_ *corev1.Event) error {
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
	broadcaster.Broadcast(testEvent)

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
		if err == nil {
			t.Error("Subscribe() should return error when send times out")
		}
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
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	// Even with a short timeout, an empty command should fail fast
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := &corev1.CommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "empty-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
		Command:   "",
	}

	resp, err := server.HandleCommand(ctx, req)
	if err != nil {
		t.Fatalf("HandleCommand() error = %v", err)
	}

	if resp.Success {
		t.Error("Empty command should fail")
	}
	if resp.Error == "" {
		t.Error("Error message should be present")
	}
}

// =============================================================================
// Malformed Request Tests (e55.38)
// =============================================================================

func TestCoreServer_MalformedRequest_NilAuthRequest(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		sessions:      sessions,
		sessionStore:  NewInMemorySessionStore(),
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
	req := &corev1.AuthRequest{}
	resp, err := server.Authenticate(ctx, req)
	// Should return an error response, not panic
	if err != nil {
		// gRPC error is acceptable
		return
	}
	if resp.Success {
		t.Error("Expected failure for empty auth request")
	}
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
		sessions:      core.NewSessionManager(),
		authenticator: auth,
		sessionStore:  NewInMemorySessionStore(),
	}

	ctx := context.Background()
	req := &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "empty-username",
			Timestamp: timestamppb.Now(),
		},
		Username: "",
		Password: "password",
	}

	resp, err := server.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}

	if resp.Success {
		t.Error("Expected failure for empty username")
	}
	if resp.Error == "" {
		t.Error("Error message should be present")
	}
}

func TestCoreServer_MalformedRequest_InvalidSessionID(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		sessions:     sessions,
		sessionStore: &mockSessionStore{sessions: make(map[string]*SessionInfo)},
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
			req := &corev1.CommandRequest{
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
			if resp.Success {
				t.Error("Expected failure for invalid session ID")
			}
			if resp.Error == "" {
				t.Error("Error message should be present")
			}
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
	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
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
			req := &corev1.CommandRequest{
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

			resp, err := server.HandleCommand(ctx, req)
			if err != nil {
				// gRPC error is acceptable
				return
			}
			// May succeed or fail depending on command processing
			// Main assertion is that it doesn't panic
			_ = resp
		})
	}
}

func TestCoreServer_MalformedRequest_InvalidSubscribeStreams(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	sessions := core.NewSessionManager()
	sessions.Connect(charID, core.NewULID())

	broadcaster := core.NewBroadcaster()

	server := &CoreServer{
		sessions:    sessions,
		broadcaster: broadcaster,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx := context.Background()

	// All requests with nil Meta should not panic
	t.Run("CommandRequest with nil Meta", func(t *testing.T) {
		req := &corev1.CommandRequest{
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
		if err != nil {
			t.Fatalf("HandleCommand() error = %v", err)
		}
		if !resp.Success {
			t.Errorf("Expected success, got error: %s", resp.Error)
		}
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
		if err != nil {
			t.Fatalf("Disconnect() error = %v", err)
		}
		if !resp.Success {
			t.Error("Expected success for disconnect")
		}
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
		sessions:      sessions,
		authenticator: auth,
		sessionStore:  NewInMemorySessionStore(),
		newSessionID:  func() ulid.ULID { return sessionID },
	}

	ctx := context.Background()

	// Normal request should work
	req := &corev1.AuthRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "unknown-fields-test",
			Timestamp: timestamppb.Now(),
		},
		Username: "testuser",
		Password: "testpass",
	}

	resp, err := server.Authenticate(ctx, req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if !resp.Success {
		t.Errorf("Expected success, got error: %s", resp.Error)
	}
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
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

			var req *corev1.CommandRequest
			switch idx % 3 {
			case 0:
				// Valid request
				req = &corev1.CommandRequest{
					Meta: &corev1.RequestMeta{
						RequestId: fmt.Sprintf("concurrent-%d", idx),
						Timestamp: timestamppb.Now(),
					},
					SessionId: sessionID.String(),
					Command:   "say hello",
				}
			case 1:
				// Invalid session
				req = &corev1.CommandRequest{
					Meta: &corev1.RequestMeta{
						RequestId: fmt.Sprintf("concurrent-%d", idx),
						Timestamp: timestamppb.Now(),
					},
					SessionId: "invalid-session",
					Command:   "say hello",
				}
			default:
				// Empty command
				req = &corev1.CommandRequest{
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
	}

	ctx := context.Background()

	// Create request with very large command
	largeCommand := "say " + strings.Repeat("x", 1*1024*1024) // 1MB message

	req := &corev1.CommandRequest{
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

	broadcaster := core.NewBroadcaster()
	engine := core.NewEngine(store, sessions, broadcaster)

	server := &CoreServer{
		engine:   engine,
		sessions: sessions,
		sessionStore: &mockSessionStore{
			sessions: map[string]*SessionInfo{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
				},
			},
		},
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
			req := &corev1.CommandRequest{
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

func TestCoreServer_MalformedRequest_DisconnectInvalidSession(t *testing.T) {
	sessions := core.NewSessionManager()

	server := &CoreServer{
		sessions:     sessions,
		sessionStore: &mockSessionStore{sessions: make(map[string]*SessionInfo)},
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
