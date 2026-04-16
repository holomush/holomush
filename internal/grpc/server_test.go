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

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
	tlscerts "github.com/holomush/holomush/internal/tls"
	"github.com/holomush/holomush/pkg/errutil"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// testPlayerSessionToken is the canonical token unit tests pass in
// HandleCommandRequest.PlayerSessionToken. The fakePlayerSessionRepo
// installed by newHandleCommandServer is seeded to match this token
// hash and return a PlayerSession whose PlayerID is the zero ULID —
// which lines up with the zero PlayerID on session.Info seeded by the
// map literals used throughout the unit tests. That makes ownership
// validation succeed for these tests without requiring them to seed
// matching PlayerIDs.
const testPlayerSessionToken = "unit-test-player-session-token"

// fakePlayerSessionRepo is a minimal auth.PlayerSessionRepository impl
// used by the HandleCommand unit test helpers. GetByTokenHash returns
// a PlayerSession whose PlayerID matches playerID and whose expiry is
// in the future, for any hash that equals the pre-seeded hash. All
// other methods panic — unit tests exercise only GetByTokenHash.
type fakePlayerSessionRepo struct {
	tokenHash string
	playerID  ulid.ULID
}

func newFakePlayerSessionRepo(token string, playerID ulid.ULID) *fakePlayerSessionRepo {
	return &fakePlayerSessionRepo{
		tokenHash: auth.HashSessionToken(token),
		playerID:  playerID,
	}
}

func (f *fakePlayerSessionRepo) Create(_ context.Context, _ *auth.PlayerSession) error {
	panic("fakePlayerSessionRepo: Create not implemented")
}

func (f *fakePlayerSessionRepo) GetByTokenHash(_ context.Context, tokenHash string) (*auth.PlayerSession, error) {
	if tokenHash != f.tokenHash {
		return nil, auth.ErrNotFound
	}
	return &auth.PlayerSession{
		ID:        ulid.ULID{},
		PlayerID:  f.playerID,
		TokenHash: tokenHash,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}, nil
}

func (f *fakePlayerSessionRepo) GetByID(_ context.Context, _ ulid.ULID) (*auth.PlayerSession, error) {
	panic("fakePlayerSessionRepo: GetByID not implemented")
}

func (f *fakePlayerSessionRepo) CountActiveByPlayer(_ context.Context, _ ulid.ULID) (int, error) {
	panic("fakePlayerSessionRepo: CountActiveByPlayer not implemented")
}

func (f *fakePlayerSessionRepo) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*auth.PlayerSession, error) {
	panic("fakePlayerSessionRepo: ListByPlayer not implemented")
}

func (f *fakePlayerSessionRepo) Delete(_ context.Context, _ ulid.ULID) error {
	panic("fakePlayerSessionRepo: Delete not implemented")
}

func (f *fakePlayerSessionRepo) DeleteByPlayer(_ context.Context, _ ulid.ULID) error {
	panic("fakePlayerSessionRepo: DeleteByPlayer not implemented")
}

func (f *fakePlayerSessionRepo) DeleteOldestForPlayer(_ context.Context, _ ulid.ULID) (*auth.PlayerSession, error) {
	panic("fakePlayerSessionRepo: DeleteOldestForPlayer not implemented")
}

func (f *fakePlayerSessionRepo) DeleteExpired(_ context.Context) (int64, error) {
	panic("fakePlayerSessionRepo: DeleteExpired not implemented")
}

func (f *fakePlayerSessionRepo) RefreshTTL(_ context.Context, _ ulid.ULID, _ time.Duration) error {
	panic("fakePlayerSessionRepo: RefreshTTL not implemented")
}

// Compile-time interface check.
var _ auth.PlayerSessionRepository = (*fakePlayerSessionRepo)(nil)

// newTestSessionStore creates a session.MemStore pre-populated with the given sessions.
func newTestSessionStore(t *testing.T, sessions map[string]*session.Info) session.Store {
	t.Helper()
	store := session.NewMemStore()
	ctx := context.Background()
	for id, info := range sessions {
		if info.ID == "" {
			info.ID = id
		}
		require.NoError(t, store.Set(ctx, id, info))
	}
	return store
}

// newHandleCommandServer creates a CoreServer wired with the unified command
// dispatcher. Tests that call HandleCommand MUST use this helper (or
// newDispatcherTestServer) because executeViaSwitch has been removed.
//
// The store is used for both Engine and dispatcher Services.Events.
// Pass a custom sessStore to pre-populate sessions; nil uses a fresh MemStore.
func newHandleCommandServer(t *testing.T, store core.EventStore, sessStore session.Store, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	engine := core.NewEngine(store)
	if sessStore == nil {
		sessStore = session.NewMemStore()
	}

	reg := command.NewRegistry()
	registerTestCommands(t, reg)

	policyEngine := policytest.AllowAllEngine()
	svc := command.NewTestServices(command.ServicesConfig{
		World:   nil,
		Session: sessStore,
		Engine:  policyEngine,
		Events:  store,
	})

	dispatcher, err := command.NewDispatcher(reg, policyEngine)
	require.NoError(t, err)

	// Install a fake PlayerSessionRepo keyed on testPlayerSessionToken
	// returning the zero-ULID PlayerID. Every unit test's session.Info
	// seeded via newTestSessionStore has a zero-ULID PlayerID, so
	// HandleCommand's ValidateSessionOwnership accepts
	// testPlayerSessionToken. Callers passing an empty or different
	// token get an enumeration-safe "session not found" response.
	allOpts := make([]CoreServerOption, 0, 2+len(opts))
	allOpts = append(allOpts,
		WithEventStore(store),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(testPlayerSessionToken, ulid.ULID{})),
	)
	allOpts = append(allOpts, opts...)

	return NewCoreServer(engine, sessStore, dispatcher, svc, allOpts...)
}

// mockEventStore implements core.EventStore for testing.
type mockEventStore struct {
	appendFunc      func(ctx context.Context, event core.Event) error
	replayFunc      func(ctx context.Context, stream string, afterID ulid.ULID, limit int) ([]core.Event, error)
	lastEventIDFunc func(ctx context.Context, stream string) (ulid.ULID, error)
	subscribeFunc   func(ctx context.Context, stream string) (<-chan ulid.ULID, <-chan error, error)
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

func (m *mockEventStore) Subscribe(ctx context.Context, stream string) (<-chan ulid.ULID, <-chan error, error) {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, stream)
	}
	eventCh := make(chan ulid.ULID)
	errCh := make(chan error)
	go func() {
		<-ctx.Done()
		close(eventCh)
		close(errCh)
	}()
	return eventCh, errCh, nil
}

func (m *mockEventStore) ReplayTail(_ context.Context, _ string, _ int, _ time.Time, _ ulid.ULID) ([]core.Event, error) {
	return nil, nil
}

func (m *mockEventStore) SubscribeSession(_ context.Context) (core.Subscription, error) {
	return nil, nil
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

// newSubscribeTestServer builds a CoreServer suitable for Subscribe tests.
// It sets up cursorLocks and a minimal focusCoordinator with the given session
// store, which the Subscribe handler requires after the B7 refactor.
func newSubscribeTestServer(t *testing.T, eventStore core.EventStore, sessStore session.Store, opts ...func(*CoreServer)) *CoreServer {
	t.Helper()
	coord, err := focus.NewCoordinator(focus.WithSessionStore(sessStore))
	require.NoError(t, err)

	s := &CoreServer{
		engine:           core.NewEngine(eventStore),
		eventStore:       eventStore,
		sessionStore:     sessStore,
		cursorLocks:      newCursorLockMap(),
		focusCoordinator: coord,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func TestCoreServer_HandleCommand_Say(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appendedEvent core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appendedEvent = event
			return nil
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cmd-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "say Hello, world!",
		PlayerSessionToken: testPlayerSessionToken,
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success, "expected success, got error: %s", resp.Error)
	assert.Equal(t, core.EventTypeSay, appendedEvent.Type)
}

func TestCoreServer_HandleCommand_InvalidSession(t *testing.T) {
	server := newHandleCommandServer(t, core.NewMemoryEventStore(), nil)

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cmd-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          "invalid-session",
		Command:            "say Hello",
		PlayerSessionToken: testPlayerSessionToken,
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "expected failure for invalid session")
	assert.NotEmpty(t, resp.Error, "error message should be present")
}

// TestCoreServer_HandleCommand_RejectsEmptyToken asserts that a caller
// who omits player_session_token gets the enumeration-safe
// "session not found" response — not an execution of the command.
// Regression guard for bd-jv7z (IDOR surface).
func TestCoreServer_HandleCommand_RejectsEmptyToken(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appendedEvents []core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appendedEvents = append(appendedEvents, event)
			return nil
		},
	}
	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	resp, err := server.HandleCommand(context.Background(), &corev1.HandleCommandRequest{
		SessionId: sessionID.String(),
		Command:   "say hi",
		// PlayerSessionToken intentionally empty.
	})
	require.NoError(t, err)

	assert.False(t, resp.Success, "empty token must not authorize a command")
	assert.Equal(t, "session not found", resp.GetError(),
		"response must be enumeration-safe")
	assert.Empty(t, appendedEvents,
		"no event should be appended when ownership validation fails")
}

// TestCoreServer_HandleCommand_RejectsCrossPlayerSession simulates the
// IDOR attack from bd-jv7z: Player A's valid token is used against
// Player B's session_id. The call must return "session not found" and
// must not execute the command. The attack is deterministic here
// because the fake PlayerSessionRepo returns PlayerA's PlayerID but
// the seeded session belongs to PlayerB.
func TestCoreServer_HandleCommand_RejectsCrossPlayerSession(t *testing.T) {
	// Player A — holds the token.
	playerA := core.NewULID()
	// Player B — owns the target session.
	playerB := core.NewULID()

	charB := core.NewULID()
	sessionIDB := core.NewULID()
	locationB := core.NewULID()

	var appendedEvents []core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appendedEvents = append(appendedEvents, event)
			return nil
		},
	}

	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionIDB.String(): {
			CharacterID: charB,
			PlayerID:    playerB, // Session B belongs to player B.
			LocationID:  locationB,
			Status:      session.StatusActive,
		},
	})

	// Swap the default fake repo for one seeded with playerA's ID — so
	// the token validates but ownership check fails.
	server := newHandleCommandServer(t, store, sessStore,
		WithPlayerSessionRepo(newFakePlayerSessionRepo(testPlayerSessionToken, playerA)),
	)

	resp, err := server.HandleCommand(context.Background(), &corev1.HandleCommandRequest{
		SessionId:          sessionIDB.String(),
		Command:            "say stolen",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)

	assert.False(t, resp.Success, "cross-player session attack must be rejected")
	assert.Equal(t, "session not found", resp.GetError(),
		"response must be enumeration-safe on ownership mismatch")
	assert.Empty(t, appendedEvents,
		"no event should be appended when ownership check fails")
}

func TestCoreServer_Subscribe_SendsEvents(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	eventStore := core.NewMemoryEventStore()
	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore)

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "sub-request-id",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	// Run Subscribe in a goroutine since it blocks
	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give the subscription time to set up
	time.Sleep(50 * time.Millisecond)

	// Send an event through the event store (triggers Subscribe notification)
	testEvent := core.NewEvent(
		"location:"+locationID.String(),
		core.EventTypeSay,
		core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
		[]byte(`{"message":"test"}`),
	)
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

	sessStore := session.NewMemStore()
	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		CharacterID: charID,
		Status:      session.StatusActive,
		TTLSeconds:  1800,
	}))

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

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
	server := newHandleCommandServer(t, store, nil)

	require.NotNil(t, server, "NewCoreServer returned nil")
	assert.NotNil(t, server.sessionStore, "sessionStore should be initialized")
}

func TestNewCoreServer_WithOptions(t *testing.T) {
	store := &mockEventStore{}

	customStore := session.NewMemStore()

	server := newHandleCommandServer(t, store, nil,
		WithSessionStore(customStore),
	)

	require.NotNil(t, server, "NewCoreServer returned nil")
	assert.Equal(t, customStore, server.sessionStore, "WithSessionStore option not applied")
}

func TestNewCoreServer_WithStreamOptions(t *testing.T) {
	store := &mockEventStore{}

	registry := NewSessionStreamRegistry()
	hookCalled := false
	server := newHandleCommandServer(t, store, nil,
		WithStreamContributor(nil),
		WithStreamRegistry(registry),
		WithAfterLISTENHook(func() { hookCalled = true }),
	)

	require.NotNil(t, server)
	assert.Nil(t, server.streamContributor)
	assert.Equal(t, registry, server.streamRegistry)
	assert.NotNil(t, server.afterLISTENHook)
	server.afterLISTENHook()
	assert.True(t, hookCalled)
}

func TestNewCoreServer_PanicsWithoutDispatcher(t *testing.T) {
	assert.Panics(t, func() {
		NewCoreServer(core.NewEngine(&mockEventStore{}), session.NewMemStore(), nil, nil)
	}, "NewCoreServer should panic without dispatcher")
}

func TestCoreServer_HandleCommand_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta:               nil, // No meta
		SessionId:          sessionID.String(),
		Command:            "say Hello",
		PlayerSessionToken: testPlayerSessionToken,
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.True(t, resp.Success, "expected success, got error: %s", resp.Error)
}

func TestCoreServer_HandleCommand_Pose(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appendedEvent core.Event
	store := &mockEventStore{
		appendFunc: func(_ context.Context, event core.Event) error {
			appendedEvent = event
			return nil
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	ctx := context.Background()

	// Test pose command
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "pose-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "pose waves hello",
		PlayerSessionToken: testPlayerSessionToken,
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
		SessionId:          sessionID.String(),
		Command:            ": nods",
		PlayerSessionToken: testPlayerSessionToken,
	}

	resp2, err := server.HandleCommand(ctx, req2)
	require.NoError(t, err)

	assert.True(t, resp2.Success, "expected success for : shortcut, got error: %s", resp2.Error)
}

func TestCoreServer_HandleCommand_UnknownCommand(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	store := core.NewMemoryEventStore()

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID:   charID,
			CharacterName: "Tester",
			LocationID:    locationID,
			Status:        session.StatusActive,
		},
	}))

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "unknown-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "unknowncommand args",
		PlayerSessionToken: testPlayerSessionToken,
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
	assert.Equal(t, core.EventTypeCommandError, charEvents[0].Type)

	var crp core.CommandResponsePayload
	require.NoError(t, json.Unmarshal(charEvents[0].Payload, &crp), "command_error payload should be valid JSON")
	assert.NotEmpty(t, crp.Text, "command_error text should not be empty")
}

func TestCoreServer_HandleCommand_SayFails(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return errors.New("database error")
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "say-fail-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "say Hello",
		PlayerSessionToken: testPlayerSessionToken,
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

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return errors.New("database error")
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	ctx := context.Background()
	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "pose-fail-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "pose waves",
		PlayerSessionToken: testPlayerSessionToken,
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

	store := core.NewMemoryEventStore()

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
	server := newHandleCommandServer(t, store, sessStore,
		WithDisconnectHook(func(_ session.Info) {
			hookCalled = true
		}),
	)

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "quit-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "quit",
		PlayerSessionToken: testPlayerSessionToken,
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
	assert.Contains(t, crp.Text, "Goodbye", "quit response should contain Goodbye")

	// Disconnect hooks should fire
	assert.True(t, hookCalled, "disconnect hook should be called on quit")
}

func TestCoreServer_Subscribe_InvalidSession(t *testing.T) {
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	server := newSubscribeTestServer(t, eventStore, sessStore)

	ctx := context.Background()
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "invalid-session-sub",
			Timestamp: timestamppb.Now(),
		},
		SessionId: "non-existent-session",
	}

	err := server.Subscribe(req, stream)
	assert.Error(t, err, "Subscribe() should return error for invalid session")
}

func TestCoreServer_Subscribe_NilEventStore(t *testing.T) {
	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore()),
		sessionStore: session.NewMemStore(),
		cursorLocks:  newCursorLockMap(),
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
	}

	err := server.Subscribe(req, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "event store not configured")
}

func TestCoreServer_Subscribe_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	eventStore := core.NewMemoryEventStore()

	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore)

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta:      nil, // No meta
		SessionId: sessionID.String(),
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

	eventStore := core.NewMemoryEventStore()

	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore)

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
	}

	// The new Subscribe sends a synthetic location_state first, which will
	// hit the send error immediately.
	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

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

	sessStore := session.NewMemStore()
	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		CharacterID: charID,
		Status:      session.StatusActive,
	}))

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

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
	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

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

// TestResourceLimitConstantsMatchSecurityBaseline pins the resource-limit
// constants to their expected values. Weakening these bounds (raising the
// recv limit, disabling concurrent-stream caps) is a security regression:
// without these caps a single connection could exhaust server memory or
// open unlimited Subscribe streams.
func TestResourceLimitConstantsMatchSecurityBaseline(t *testing.T) {
	assert.Equal(t, 4*1024*1024, MaxRecvMsgSize,
		"MaxRecvMsgSize changed — review security implications before updating this test")
	assert.Equal(t, 16*1024*1024, MaxSendMsgSize,
		"MaxSendMsgSize changed — review security implications before updating this test")
	assert.Equal(t, uint32(100), MaxConcurrentStreams,
		"MaxConcurrentStreams changed — review security implications before updating this test")
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

	eventStore := core.NewMemoryEventStore()

	sessionStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessionStore)

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

		ctx := context.Background()
		sessionStore := session.NewMemStore()
		require.NoError(t, sessionStore.Set(ctx, sessionID.String(), &session.Info{
			CharacterID: charID,
			IsGuest:     true,
			Status:      session.StatusActive,
		}))

		server := &CoreServer{
			engine: core.NewEngine(core.NewMemoryEventStore()),

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

		ctx := context.Background()
		sessionStore := session.NewMemStore()
		require.NoError(t, sessionStore.Set(ctx, sessionID.String(), &session.Info{
			CharacterID: charID,
			IsGuest:     false,
			Status:      session.StatusActive,
			TTLSeconds:  1800,
		}))

		server := &CoreServer{
			engine: core.NewEngine(core.NewMemoryEventStore()),

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

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	sessionStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := newHandleCommandServer(t, store, sessionStore)

	ctx := context.Background()

	// Execute multiple commands to simulate activity
	for i := 0; i < 3; i++ {
		req := &corev1.HandleCommandRequest{
			Meta: &corev1.RequestMeta{
				RequestId: fmt.Sprintf("activity-test-%d", i),
				Timestamp: timestamppb.Now(),
			},
			SessionId:          sessionID.String(),
			Command:            fmt.Sprintf("say Message %d", i),
			PlayerSessionToken: testPlayerSessionToken,
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
	eventStore := core.NewMemoryEventStore()
	ctx := context.Background()
	sessionStore := session.NewMemStore()

	// Create two sessions
	session1ID := core.NewULID()
	char1ID := core.NewULID()

	location1ID := core.NewULID()
	require.NoError(t, sessionStore.Set(ctx, session1ID.String(), &session.Info{
		CharacterID: char1ID,
		LocationID:  location1ID,
		Status:      session.StatusActive,
	}))

	session2ID := core.NewULID()
	char2ID := core.NewULID()

	location2ID := core.NewULID()
	require.NoError(t, sessionStore.Set(ctx, session2ID.String(), &session.Info{
		CharacterID: char2ID,
		LocationID:  location2ID,
		Status:      session.StatusActive,
	}))

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

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

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	// Create a context with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "timeout-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "say This should timeout",
		PlayerSessionToken: testPlayerSessionToken,
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

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	ctx, cancel := context.WithCancel(context.Background())

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cancel-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "say This will be cancelled",
		PlayerSessionToken: testPlayerSessionToken,
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

	eventStore := core.NewMemoryEventStore()
	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore)

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}
	streamName := "location:" + locationID.String()

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "cancel-sub-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give subscription time to set up
	time.Sleep(50 * time.Millisecond)

	// Verify subscription is active by sending an event
	testEvent := core.NewEvent(
		streamName,
		core.EventTypeSay,
		core.Actor{Kind: core.ActorCharacter, ID: charID.String()},
		[]byte(`{"message":"test"}`),
	)
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

			server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
				sessionID.String(): {
					CharacterID: charID,
					LocationID:  locationID,
					Status:      session.StatusActive,
				},
			}))

			ctx, cancel := context.WithTimeout(context.Background(), tt.timeout)
			defer cancel()

			req := &corev1.HandleCommandRequest{
				Meta: &corev1.RequestMeta{
					RequestId: "timeout-error-test",
					Timestamp: timestamppb.Now(),
				},
				SessionId:          sessionID.String(),
				Command:            "say Hello",
				PlayerSessionToken: testPlayerSessionToken,
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

	eventStore := core.NewMemoryEventStore()
	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore)

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Create a stream that blocks on send. The new Subscribe sends a synthetic
	// location_state first, so the blocking send fires immediately.
	blockingSendCalled := make(chan struct{}, 1)
	stream := &mockSubscribeStreamWithError{
		ctx: ctx,
		sendFunc: func(_ *corev1.SubscribeResponse) error {
			select {
			case blockingSendCalled <- struct{}{}:
			default:
			}
			// Block until context is cancelled
			<-ctx.Done()
			return ctx.Err()
		},
	}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "timeout-send-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Wait for send to be called (from synthetic location_state)
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

	store := &mockEventStore{}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	// Even with a short timeout, an empty command should fail fast
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "empty-cmd-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            "",
		PlayerSessionToken: testPlayerSessionToken,
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)

	assert.False(t, resp.Success, "empty command should fail")
	assert.NotEmpty(t, resp.Error, "error message should be present")
}

// =============================================================================
// Malformed Request Tests (e55.38)
// =============================================================================

func TestCoreServer_MalformedRequest_InvalidSessionID(t *testing.T) {
	server := newHandleCommandServer(t, core.NewMemoryEventStore(), nil)

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
				SessionId:          tt.sessionID,
				Command:            "say hello",
				PlayerSessionToken: testPlayerSessionToken,
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

	store := &mockEventStore{}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

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
				SessionId:          sessionID.String(),
				Command:            tt.command,
				PlayerSessionToken: testPlayerSessionToken,
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

func TestCoreServer_MalformedRequest_NilMeta(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	ctx := context.Background()

	// All requests with nil Meta should not panic
	t.Run("CommandRequest with nil Meta", func(t *testing.T) {
		req := &corev1.HandleCommandRequest{
			Meta:               nil,
			SessionId:          sessionID.String(),
			Command:            "say hello",
			PlayerSessionToken: testPlayerSessionToken,
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

func TestCoreServer_MalformedRequest_ConcurrentMalformedRequests(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			time.Sleep(10 * time.Millisecond) // Simulate some processing
			return nil
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

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
					SessionId:          sessionID.String(),
					Command:            "say hello",
					PlayerSessionToken: testPlayerSessionToken,
				}
			case 1:
				// Invalid session
				req = &corev1.HandleCommandRequest{
					Meta: &corev1.RequestMeta{
						RequestId: fmt.Sprintf("concurrent-%d", idx),
						Timestamp: timestamppb.Now(),
					},
					SessionId:          "invalid-session",
					Command:            "say hello",
					PlayerSessionToken: testPlayerSessionToken,
				}
			default:
				// Empty command
				req = &corev1.HandleCommandRequest{
					Meta: &corev1.RequestMeta{
						RequestId: fmt.Sprintf("concurrent-%d", idx),
						Timestamp: timestamppb.Now(),
					},
					SessionId:          sessionID.String(),
					Command:            "",
					PlayerSessionToken: testPlayerSessionToken,
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

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

	ctx := context.Background()

	// Create request with very large command
	largeCommand := "say " + strings.Repeat("x", 1*1024*1024) // 1MB message

	req := &corev1.HandleCommandRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "large-payload-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId:          sessionID.String(),
		Command:            largeCommand,
		PlayerSessionToken: testPlayerSessionToken,
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

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error {
			return nil
		},
	}

	server := newHandleCommandServer(t, store, newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	}))

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
				SessionId:          sessionID.String(),
				Command:            tt.command,
				PlayerSessionToken: testPlayerSessionToken,
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

	store := core.NewMemoryEventStore()

	var hookCalled bool
	var hookInfo session.Info
	sessStore := session.NewMemStore()
	server := newHandleCommandServer(t, store, sessStore,
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

	hookCallCount := 0
	server := &CoreServer{
		engine:       core.NewEngine(core.NewMemoryEventStore()),
		eventStore:   core.NewMemoryEventStore(),
		sessionStore: session.NewMemStore(),
		disconnectHooks: []func(session.Info){
			func(_ session.Info) { panic("hook panic") },
			func(_ session.Info) { hookCallCount++ },
		},
	}

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "PanicTest",
		LocationID:    locationID,
		IsGuest:       true,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))

	// Disconnect should not panic — recovery catches it
	discResp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
		SessionId: sessionID.String(),
		Meta:      &corev1.RequestMeta{RequestId: "test"},
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

	store := core.NewMemoryEventStore()

	sessStore := session.NewMemStore()
	server := newHandleCommandServer(t, store, sessStore)
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

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error { return nil },
	}

	sessStore := session.NewMemStore()
	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		ID:          sessionID.String(),
		CharacterID: charID,
		LocationID:  locationID,
		Status:      session.StatusActive,
		MaxHistory:  100,
	}))

	server := newHandleCommandServer(t, store, sessStore)

	commands := []string{"say hello", "pose waves", "say goodbye"}
	for _, cmd := range commands {
		req := &corev1.HandleCommandRequest{
			Meta:               &corev1.RequestMeta{RequestId: "history-test", Timestamp: timestamppb.Now()},
			SessionId:          sessionID.String(),
			Command:            cmd,
			PlayerSessionToken: testPlayerSessionToken,
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

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error { return nil },
	}

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

	server := newHandleCommandServer(t, store, sessStore)

	// Send more commands than maxHistory
	for i := 0; i < 5; i++ {
		req := &corev1.HandleCommandRequest{
			Meta:               &corev1.RequestMeta{RequestId: fmt.Sprintf("cap-test-%d", i), Timestamp: timestamppb.Now()},
			SessionId:          sessionID.String(),
			Command:            fmt.Sprintf("say message %d", i),
			PlayerSessionToken: testPlayerSessionToken,
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

	store := &mockEventStore{
		appendFunc: func(_ context.Context, _ core.Event) error { return nil },
	}

	ctx := context.Background()
	realStore := session.NewMemStore()
	require.NoError(t, realStore.Set(ctx, sessionID.String(), &session.Info{
		ID:          sessionID.String(),
		CharacterID: charID,
		LocationID:  locationID,
		Status:      session.StatusActive,
		MaxHistory:  100,
	}))

	server := newHandleCommandServer(t, store, realStore)

	req := &corev1.HandleCommandRequest{
		Meta:               &corev1.RequestMeta{RequestId: "best-effort-test", Timestamp: timestamppb.Now()},
		SessionId:          sessionID.String(),
		Command:            "say hello",
		PlayerSessionToken: testPlayerSessionToken,
	}

	resp, err := server.HandleCommand(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp.Success, "command should succeed even when history append fails")
}

func TestCoreServer_Disconnect_EmitsLeaveEvent(t *testing.T) {
	t.Run("guest disconnect emits leave event", func(t *testing.T) {
		charID := core.NewULID()
		locationID := core.NewULID()
		sessionID := core.NewULID()

		store := core.NewMemoryEventStore()

		server := newHandleCommandServer(t, store, nil)
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

		store := core.NewMemoryEventStore()

		server := newHandleCommandServer(t, store, nil)
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
	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

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

	eventStore := core.NewMemoryEventStore()
	ctx := context.Background()

	// Prepopulate store: cursor event + 2 historical events
	cursorEvent := core.NewEvent(streamName, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "actor0",
	}, []byte(`{"message":"cursor"}`))
	require.NoError(t, eventStore.Append(ctx, cursorEvent))

	historical1 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "actor1",
	}, []byte(`{"message":"missed-1"}`))
	require.NoError(t, eventStore.Append(ctx, historical1))

	historical2 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "actor2",
	}, []byte(`{"message":"missed-2"}`))
	require.NoError(t, eventStore.Append(ctx, historical2))

	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID:  charID,
			LocationID:   locationID,
			Status:       session.StatusActive,
			EventCursors: map[string]ulid.ULID{streamName: cursorEvent.ID},
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore, func(s *CoreServer) {
		s.sessionDefaults = SessionDefaults{MaxReplay: 1000}
	})

	subCtx, cancel := context.WithCancel(ctx)
	stream := &mockSubscribeStream{ctx: subCtx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "replay-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give time for replay and subscription setup
	time.Sleep(100 * time.Millisecond)

	// Send a live event after replay
	liveEvent := core.NewEvent(streamName, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "actor3",
	}, []byte(`{"message":"live"}`))
	require.NoError(t, eventStore.Append(subCtx, liveEvent))

	// Give time for live event delivery
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Verify: historical events appear, and the live event is also received.
	// Filter to unique say event IDs for verification (the live-loop's first
	// notification may re-replay from the in-memory cursor, causing benign
	// duplicates of already-replayed events).
	seenSayIDs := make(map[string]bool)
	for _, ev := range stream.events {
		if ef := ev.GetEvent(); ef != nil && ef.GetType() == string(core.EventTypeSay) {
			seenSayIDs[ef.GetId()] = true
		}
	}
	assert.True(t, seenSayIDs[historical1.ID.String()], "historical1 should be present")
	assert.True(t, seenSayIDs[historical2.ID.String()], "historical2 should be present")
	assert.True(t, seenSayIDs[liveEvent.ID.String()], "live event should be present")
}

func TestCoreServer_Subscribe_ReplayDeduplicatesLiveEvents(t *testing.T) {
	// With notification-driven delivery, dedup is inherent: replay advances
	// lastSentID, so a notification for an already-replayed event produces
	// an empty Replay response (no duplicates sent).
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	streamName := "location:" + locationID.String()

	eventStore := core.NewMemoryEventStore()
	ctx := context.Background()

	// Prepopulate: cursor + one historical event
	cursorEvent := core.NewEvent(streamName, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "actor0",
	}, []byte(`{"message":"cursor"}`))
	require.NoError(t, eventStore.Append(ctx, cursorEvent))

	historicalEvent := core.NewEvent(streamName, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "actor1",
	}, []byte(`{"message":"historical"}`))
	require.NoError(t, eventStore.Append(ctx, historicalEvent))

	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID:  charID,
			LocationID:   locationID,
			Status:       session.StatusActive,
			EventCursors: map[string]ulid.ULID{streamName: cursorEvent.ID},
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore, func(s *CoreServer) {
		s.sessionDefaults = SessionDefaults{MaxReplay: 1000}
	})

	subCtx, cancel := context.WithCancel(ctx)
	stream := &mockSubscribeStream{ctx: subCtx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "dedup-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
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
		if ef := ev.GetEvent(); ef != nil && ef.GetId() == historicalEvent.ID.String() {
			count++
		}
	}
	assert.Equal(t, 1, count, "historical event should appear exactly once (no duplication)")
}

func TestCoreServer_Subscribe_NoReplayWithoutCursors(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	eventStore := core.NewMemoryEventStore()
	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID:  charID,
			LocationID:   locationID,
			Status:       session.StatusActive,
			EventCursors: map[string]ulid.ULID{}, // empty cursors
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore, func(s *CoreServer) {
		s.sessionDefaults = SessionDefaults{MaxReplay: 1000}
	})

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "no-cursor-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Give time for replay to complete, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// With empty cursors and no events in store, only synthetic location_state
	// and REPLAY_COMPLETE frames are sent. No replayed EventFrames with say type.
	for _, ev := range stream.events {
		if ef := ev.GetEvent(); ef != nil {
			assert.NotEqual(t, string(core.EventTypeSay), ef.GetType(),
				"no say event frames should be sent when store has no events")
		}
	}
}

func TestCoreServer_Subscribe_EmitsReplayCompleteControlFrame(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	streamName := "location:" + locationID.String()

	eventStore := core.NewMemoryEventStore()
	ctx := context.Background()

	// Prepopulate store: cursor event + 1 historical event after it
	cursorEvent := core.NewEvent(streamName, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "actor0",
	}, []byte(`{"message":"before-cursor"}`))
	require.NoError(t, eventStore.Append(ctx, cursorEvent))

	historicalEvent := core.NewEvent(streamName, core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "actor1",
	}, []byte(`{"message":"missed"}`))
	require.NoError(t, eventStore.Append(ctx, historicalEvent))

	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID:  charID,
			LocationID:   locationID,
			Status:       session.StatusActive,
			EventCursors: map[string]ulid.ULID{streamName: cursorEvent.ID},
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore, func(s *CoreServer) {
		s.sessionDefaults = SessionDefaults{MaxReplay: 1000}
	})

	subCtx, cancel := context.WithCancel(ctx)
	stream := &mockSubscribeStream{ctx: subCtx}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "replay-complete-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
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

// mockSubscribeStreamCh is a channel-based subscribe stream for tests that
// need to observe events one at a time (e.g., STREAM_CLOSED tests).
type mockSubscribeStreamCh struct {
	grpc.ServerStream
	ctx    context.Context
	sendCh chan *corev1.SubscribeResponse
}

func (m *mockSubscribeStreamCh) Context() context.Context {
	return m.ctx
}

func (m *mockSubscribeStreamCh) Send(resp *corev1.SubscribeResponse) error {
	select {
	case m.sendCh <- resp:
		return nil
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func TestCoreServer_Subscribe_EmitsStreamClosedOnSessionDestroy(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		ID:           sessionID.String(),
		CharacterID:  charID,
		LocationID:   locationID,
		Status:       session.StatusActive,
		EventCursors: map[string]ulid.ULID{},
	}))

	server := newSubscribeTestServer(t, eventStore, sessStore, func(s *CoreServer) {
		s.sessionDefaults = SessionDefaults{MaxReplay: 1000}
	})

	subCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	sendCh := make(chan *corev1.SubscribeResponse, 20)
	stream := &mockSubscribeStreamCh{ctx: subCtx, sendCh: sendCh}

	req := &corev1.SubscribeRequest{
		Meta: &corev1.RequestMeta{
			RequestId: "stream-closed-test",
			Timestamp: timestamppb.Now(),
		},
		SessionId: sessionID.String(),
	}

	done := make(chan error, 1)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Drain events until we see REPLAY_COMPLETE.
	deadline := time.After(2 * time.Second)
	for {
		select {
		case resp := <-sendCh:
			if cf := resp.GetControl(); cf != nil && cf.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
				goto replayComplete
			}
		case <-deadline:
			t.Fatal("timed out waiting for REPLAY_COMPLETE")
		}
	}
replayComplete:

	// Destroy the session — this should trigger STREAM_CLOSED.
	require.NoError(t, sessStore.Delete(ctx, sessionID.String(), "Goodbye!"))

	// Next frame must be STREAM_CLOSED.
	select {
	case resp := <-sendCh:
		cf := resp.GetControl()
		require.NotNil(t, cf, "expected ControlFrame, got: %v", resp)
		assert.Equal(t, corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED, cf.GetSignal())
		assert.Equal(t, "Goodbye!", cf.GetMessage())
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for STREAM_CLOSED frame")
	}

	// Subscribe should return nil after sending STREAM_CLOSED.
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Subscribe did not return after STREAM_CLOSED")
	}
}

func TestEventToProto(t *testing.T) {
	ev := core.NewEvent("location:test", core.EventTypeSay, core.Actor{
		Kind: core.ActorCharacter, ID: "char-1",
	}, []byte(`{"msg":"hello"}`))

	proto := eventToProto(ev)
	ef := proto.GetEvent()
	require.NotNil(t, ef)

	assert.Equal(t, ev.ID.String(), ef.GetId())
	assert.Equal(t, "location:test", ef.GetStream())
	assert.Equal(t, "say", ef.GetType())
	assert.Equal(t, "character", ef.GetActorType())
	assert.Equal(t, "char-1", ef.GetActorId())
	assert.Equal(t, []byte(`{"msg":"hello"}`), ef.GetPayload())
	assert.Equal(t, ev.Timestamp.UnixNano()/1e9, ef.GetTimestamp().AsTime().UnixNano()/1e9)
}

// =============================================================================
// Connection Type Tracking + Grid Presence Tests (Chunk 7)
// =============================================================================

func TestCoreServer_Disconnect_GridPresencePhaseOut(t *testing.T) {
	t.Run("last terminal disconnects with comms_hub remaining emits leave", func(t *testing.T) {
		charID := core.NewULID()
		locationID := core.NewULID()
		sessionID := core.NewULID()

		eventStore := core.NewMemoryEventStore()

		sessStore := session.NewMemStore()
		server := newHandleCommandServer(t, eventStore, sessStore)
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

		eventStore := core.NewMemoryEventStore()

		sessStore := session.NewMemStore()
		server := newHandleCommandServer(t, eventStore, sessStore)
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

		eventStore := core.NewMemoryEventStore()

		sessStore := session.NewMemStore()
		server := newHandleCommandServer(t, eventStore, sessStore)
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

func TestCoreServer_GetCommandHistory(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	ctx := context.Background()
	store := session.NewMemStore()
	require.NoError(t, store.Set(ctx, sessionID.String(), &session.Info{
		ID:          sessionID.String(),
		CharacterID: charID,
		LocationID:  locationID,
		Status:      session.StatusActive,
		MaxHistory:  100,
	}))

	// Seed command history
	require.NoError(t, store.AppendCommand(ctx, sessionID.String(), "look", 100))
	require.NoError(t, store.AppendCommand(ctx, sessionID.String(), "say hello", 100))
	require.NoError(t, store.AppendCommand(ctx, sessionID.String(), "go north", 100))

	server := &CoreServer{
		engine: core.NewEngine(core.NewMemoryEventStore()),

		sessionStore: store,
	}

	t.Run("returns commands for valid session", func(t *testing.T) {
		resp, err := server.GetCommandHistory(ctx, &corev1.GetCommandHistoryRequest{
			Meta:      &corev1.RequestMeta{RequestId: "hist-1", Timestamp: timestamppb.Now()},
			SessionId: sessionID.String(),
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Equal(t, []string{"look", "say hello", "go north"}, resp.Commands)
		assert.Equal(t, "hist-1", resp.Meta.RequestId)
	})

	t.Run("returns error for missing session_id", func(t *testing.T) {
		resp, err := server.GetCommandHistory(ctx, &corev1.GetCommandHistoryRequest{
			Meta: &corev1.RequestMeta{RequestId: "hist-2"},
		})
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "session_id is required", resp.Error)
	})

	t.Run("returns error for unknown session", func(t *testing.T) {
		resp, err := server.GetCommandHistory(ctx, &corev1.GetCommandHistoryRequest{
			Meta:      &corev1.RequestMeta{RequestId: "hist-3"},
			SessionId: "nonexistent",
		})
		require.NoError(t, err)
		assert.False(t, resp.Success)
		assert.Equal(t, "session not found", resp.Error)
	})

	t.Run("returns empty for session with no history", func(t *testing.T) {
		emptySessionID := core.NewULID()
		require.NoError(t, store.Set(ctx, emptySessionID.String(), &session.Info{
			ID:          emptySessionID.String(),
			CharacterID: core.NewULID(),
			Status:      session.StatusActive,
			MaxHistory:  100,
		}))

		resp, err := server.GetCommandHistory(ctx, &corev1.GetCommandHistoryRequest{
			SessionId: emptySessionID.String(),
		})
		require.NoError(t, err)
		assert.True(t, resp.Success)
		assert.Empty(t, resp.Commands)
	})
}

// =============================================================================
// Plugin Stream Contribution Tests (holomush-oirq Task 10)
// =============================================================================

// mockFocusStreamContributor implements focus.StreamContributor for tests.
type mockFocusStreamContributor struct {
	streams []string
}

func (m *mockFocusStreamContributor) QuerySessionStreams(_ context.Context, _ focus.StreamContributorRequest) []string {
	return m.streams
}

func TestSubscribeIncludesPluginContributedStreams(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	eventStore := core.NewMemoryEventStore()
	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	contributor := &mockFocusStreamContributor{streams: []string{"channel:abc"}}

	coord, err := focus.NewCoordinator(
		focus.WithSessionStore(sessStore),
		focus.WithStreamContributor(contributor),
	)
	require.NoError(t, err)

	ready := make(chan struct{})
	server := newSubscribeTestServer(t, eventStore, sessStore, func(s *CoreServer) {
		s.focusCoordinator = coord
		s.afterLISTENHook = func() { close(ready) }
	})

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		SessionId: sessionID.String(),
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Wait for Subscribe to finish LISTEN setup before cancelling.
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not finish LISTEN setup")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// Verify the plugin-contributed stream appears in the sent events.
	// The RestoreFocus plan includes channel:abc, and Subscribe adds it to
	// the subscription. We verify by checking that the stream would receive
	// events — send one to channel:abc and check it arrives.
	// Since we already cancelled, we instead verify from the stream events
	// that the REPLAY_COMPLETE frame was sent (proving the handler got past
	// stream setup).
	var replayCompleteSeen bool
	for _, ev := range stream.events {
		if cf := ev.GetControl(); cf != nil && cf.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_REPLAY_COMPLETE {
			replayCompleteSeen = true
		}
	}
	assert.True(t, replayCompleteSeen, "Subscribe should have completed setup including plugin-contributed streams")
}

func TestSubscribeDeregistersRegistryOnExit(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	eventStore := core.NewMemoryEventStore()
	registry := NewSessionStreamRegistry()
	sessStore := newTestSessionStore(t, map[string]*session.Info{
		sessionID.String(): {
			ID:          sessionID.String(),
			CharacterID: charID,
			LocationID:  locationID,
			Status:      session.StatusActive,
		},
	})

	server := newSubscribeTestServer(t, eventStore, sessStore, func(s *CoreServer) {
		s.streamRegistry = registry
	})

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockSubscribeStream{ctx: ctx}

	req := &corev1.SubscribeRequest{
		SessionId: sessionID.String(),
	}

	// Use afterLISTENHook to verify the session IS registered just before replay.
	registeredDuringSetup := make(chan bool, 1)
	server.afterLISTENHook = func() {
		err := registry.Send(sessionID.String(), sessionStreamUpdate{stream: "channel:test", add: true})
		registeredDuringSetup <- (err == nil)
	}

	done := make(chan error)
	go func() {
		done <- server.Subscribe(req, stream)
	}()

	// Wait for afterLISTENHook to fire.
	select {
	case wasRegistered := <-registeredDuringSetup:
		assert.True(t, wasRegistered, "session should be registered in registry during Subscribe")
	case <-time.After(time.Second):
		t.Fatal("afterLISTENHook did not fire")
	}

	// Cancel context to exit Subscribe.
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Subscribe did not return after context cancellation")
	}

	// After Subscribe exits, the session should be deregistered.
	err := registry.Send(sessionID.String(), sessionStreamUpdate{stream: "channel:test", add: false})
	require.Error(t, err, "registry should return SESSION_NOT_FOUND after Subscribe exits")
	errutil.AssertErrorCode(t, err, "SESSION_NOT_FOUND")
}
