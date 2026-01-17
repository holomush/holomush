package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/internal/proto/holomush/core/v1"
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
