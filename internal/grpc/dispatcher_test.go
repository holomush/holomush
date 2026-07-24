// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/testsupport/sessiontest"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	corecomm "github.com/holomush/holomush/plugins/core-communication"
)

// registerTestCommands registers quit/shutdown (compiled-in) plus stub handlers
// for say, pose, and ooc so that dispatcher and pipeline tests can exercise the
// full dispatch path. The stub payloads match the format expected by the web
// translation layer (character_name, message, action fields).
// registerTestCommands takes store directly (rather than reaching it via
// command.Services, which no longer holds an event sink post-07-06 D-02)
// so its say/pose/ooc stub handlers can append fabricated events into the
// same shared in-memory store the presence emitter and dispatcher-emitted
// events use, for store.Replay-based assertions.
func registerTestCommands(t *testing.T, reg *command.Registry, store eventbus.Publisher) {
	t.Helper()
	handlers.RegisterAll(reg)
	mustRegister := func(cfg command.CommandEntryConfig) {
		t.Helper()
		entry, err := command.NewCommandEntry(cfg)
		require.NoError(t, err)
		require.NoError(t, reg.Register(*entry))
	}
	mustRegister(command.CommandEntryConfig{
		Name:   "say",
		Source: "test",
		Handler: func(ctx context.Context, exec *command.CommandExecution) error {
			payload, _ := json.Marshal(map[string]string{
				"character_name": exec.CharacterName(),
				"message":        exec.Args,
			})
			event := eventbus.NewEvent(
				eventbus.Subject("location."+exec.LocationID().String()),
				eventbus.Type(corecomm.EventTypeSay),
				eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: exec.CharacterID()},
				payload,
			)
			return store.Publish(ctx, event)
		},
	})
	mustRegister(command.CommandEntryConfig{
		Name:   "pose",
		Source: "test",
		Handler: func(ctx context.Context, exec *command.CommandExecution) error {
			payload, _ := json.Marshal(map[string]string{
				"character_name": exec.CharacterName(),
				"action":         exec.Args,
			})
			event := eventbus.NewEvent(
				eventbus.Subject("location."+exec.LocationID().String()),
				eventbus.Type(corecomm.EventTypePose),
				eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: exec.CharacterID()},
				payload,
			)
			return store.Publish(ctx, event)
		},
	})
	mustRegister(command.CommandEntryConfig{
		Name:   "ooc",
		Source: "test",
		Handler: func(ctx context.Context, exec *command.CommandExecution) error {
			payload, _ := json.Marshal(eventvocab.OOCPayload{
				CharacterName: exec.CharacterName(),
				Message:       exec.Args,
				Style:         "say",
			})
			event := eventbus.NewEvent(
				eventbus.Subject("location."+exec.LocationID().String()),
				eventbus.Type(corecomm.EventTypeOOC),
				eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: exec.CharacterID()},
				payload,
			)
			return store.Publish(ctx, event)
		},
	})
}

// newDispatcherTestServer creates a CoreServer wired with the unified
// command dispatcher, using in-memory stores for testing.
func newDispatcherTestServer(t *testing.T, store eventbus.Publisher, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	return newHandleCommandServer(t, store, nil, opts...)
}

// newDispatcherTestServerWithAliases creates a CoreServer with MUSH aliases
// (`:`, `;`, `"`) loaded into the alias cache, matching production bootstrap.
func newDispatcherTestServerWithAliases(t *testing.T, store eventbus.Publisher, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	pres := newTestPresenceEmitter(store)
	sessStore := sessiontest.NewStore(t)

	reg := command.NewRegistry()
	registerTestCommands(t, reg, store)

	policyEngine := policytest.AllowAllEngine()
	aliasCache := command.NewAliasCache()
	aliasCache.LoadSystemAliases(map[string]string{
		":": "pose",
		";": "pose",
		`"`: "say",
	})

	svc := command.NewTestServices(command.ServicesConfig{
		World:   nil,
		Session: sessStore,
		Engine:  policyEngine,
	})

	dispatcher, err := command.NewDispatcher(
		reg, policyEngine,
		command.WithAliasCache(aliasCache),
	)
	require.NoError(t, err)

	allOpts := make([]CoreServerOption, 0, 2+len(opts))
	allOpts = append(
		allOpts,
		WithEventPublisher(store, func() string { return "main" }),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(ulid.ULID{})),
	)
	allOpts = append(allOpts, opts...)

	return NewCoreServer(pres, sessStore, dispatcher, svc, allOpts...)
}

func TestDispatcher_HandleCommand_Say(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appended []eventbus.Event
	store := &mockEventStore{
		publishFunc: func(_ context.Context, event eventbus.Event) error {
			appended = append(appended, event)
			return nil
		},
	}

	server := newDispatcherTestServer(t, store)

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "Tester",
		LocationID:    locationID,
		Status:        session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:               &corev1.RequestMeta{RequestId: "say-test", Timestamp: timestamppb.Now()},
		SessionId:          sessionID.String(),
		Command:            "say Hello, world!",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, "say should succeed: %s", resp.Error)

	// Should emit a say event on the location stream
	require.NotEmpty(t, appended)
	assert.Equal(t, eventbus.Type(corecomm.EventTypeSay), appended[0].Type)
	assert.Equal(t, eventbus.Subject("location."+locationID.String()), appended[0].Subject)
}

func TestDispatcher_HandleCommand_Pose(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appended []eventbus.Event
	store := &mockEventStore{
		publishFunc: func(_ context.Context, event eventbus.Event) error {
			appended = append(appended, event)
			return nil
		},
	}

	server := newDispatcherTestServer(t, store)

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "Tester",
		LocationID:    locationID,
		Status:        session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:               &corev1.RequestMeta{RequestId: "pose-test", Timestamp: timestamppb.Now()},
		SessionId:          sessionID.String(),
		Command:            "pose waves hello",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, "pose should succeed: %s", resp.Error)

	require.NotEmpty(t, appended)
	assert.Equal(t, eventbus.Type(corecomm.EventTypePose), appended[0].Type)
}

func TestDispatcher_HandleCommand_ColonPrefix(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()

	var appended []eventbus.Event
	store := &mockEventStore{
		publishFunc: func(_ context.Context, event eventbus.Event) error {
			appended = append(appended, event)
			return nil
		},
	}

	server := newDispatcherTestServerWithAliases(t, store)

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "Tester",
		LocationID:    locationID,
		Status:        session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:               &corev1.RequestMeta{RequestId: "colon-test", Timestamp: timestamppb.Now()},
		SessionId:          sessionID.String(),
		Command:            ": nods",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, ": should expand to pose via alias: %s", resp.Error)

	require.NotEmpty(t, appended)
	assert.Equal(t, eventbus.Type(corecomm.EventTypePose), appended[0].Type)
}

func TestDispatcher_HandleCommand_UnknownCommand(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	store := newTestEventStore()

	server := newDispatcherTestServer(t, store)

	ctx := context.Background()
	require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "Tester",
		LocationID:    locationID,
		Status:        session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:               &corev1.RequestMeta{RequestId: "unknown-test", Timestamp: timestamppb.Now()},
		SessionId:          sessionID.String(),
		Command:            "unknowncommand args",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, "unknown command should succeed at RPC level")

	// Should emit error command_response on character stream
	charEvents, err := store.Replay(ctx, "character."+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.NotEmpty(t, charEvents, "expected command_response event")
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeCommandError), charEvents[0].Type)

	var crp eventvocab.CommandResponsePayload
	require.NoError(t, json.Unmarshal(charEvents[0].Payload, &crp))
}

func TestDispatcher_HandleCommand_Quit(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	store := newTestEventStore()

	var hookCalled bool
	server := newDispatcherTestServer(
		t, store,
		WithDisconnectHook(func(_ session.Info) {
			hookCalled = true
		}),
	)

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
		Meta:               &corev1.RequestMeta{RequestId: "quit-test", Timestamp: timestamppb.Now()},
		SessionId:          sessionID.String(),
		Command:            "quit",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Session should be deleted
	_, err = sessStore.Get(ctx, sessionID.String())
	assert.Error(t, err, "session should be deleted after quit")

	// Leave event should be emitted on location stream
	locEvents, err := store.Replay(ctx, "location."+locationID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.Len(t, locEvents, 1, "expected exactly one leave event")
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeLeave), locEvents[0].Type)

	// Goodbye command_response event on character stream
	charEvents, err := store.Replay(ctx, "character."+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.NotEmpty(t, charEvents)
	assert.Equal(t, eventbus.Type(eventvocab.EventTypeCommandResponse), charEvents[0].Type)

	var crp eventvocab.CommandResponsePayload
	require.NoError(t, json.Unmarshal(charEvents[0].Payload, &crp))
	assert.Contains(t, crp.Text, "Goodbye")

	// Disconnect hook should fire
	assert.True(t, hookCalled)
}

// TestQuitPathAppendsSessionEndedOnCharacterStream verifies that the quit
// handler emits a session_ended event on the character's own stream between
// HandleDisconnect and sessionStore.Delete. See Task 8 of the session lifecycle
// as events plan (docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md).
func TestQuitPathAppendsSessionEndedOnCharacterStream(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	store := newTestEventStore()

	server := newDispatcherTestServer(t, store)

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
		Meta:               &corev1.RequestMeta{RequestId: "quit-test", Timestamp: timestamppb.Now()},
		SessionId:          sessionID.String(),
		Command:            "quit",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// session_ended event should be present on character stream with
	// cause=quit, the correct SessionID, and Reason="Goodbye!".
	charEvents, err := store.Replay(ctx, "character."+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)

	var sessionEnded *eventbus.Event
	for i := range charEvents {
		if charEvents[i].Type == eventbus.Type(eventvocab.EventTypeSessionEnded) {
			sessionEnded = &charEvents[i]
			break
		}
	}
	require.NotNil(t, sessionEnded, "expected a session_ended event on character stream")
	assert.Equal(t, eventbus.Subject("events.main.character."+charID.String()), sessionEnded.Subject)
	assert.Equal(t, eventbus.ActorKindCharacter, sessionEnded.Actor.Kind,
		"cause=quit uses ActorCharacter per Design Decision #1")

	var payload core.SessionEndedPayload
	require.NoError(t, json.Unmarshal(sessionEnded.Payload, &payload))
	assert.Equal(t, sessionID.String(), payload.SessionID)
	assert.Equal(t, charID.String(), payload.CharacterID)
	assert.Equal(t, core.SessionEndedCauseQuit, payload.Cause)
	assert.Equal(t, "Goodbye!", payload.Reason)
}

// TestGuestDisconnectEmitsSessionEndedOnCharacterStream verifies that the
// guest-disconnect path (IsGuest=true with no remaining connections) emits a
// session_ended event on the character's own stream with cause=guest_end
// between HandleDisconnect and sessionStore.Delete. See Task 8 of the session
// lifecycle as events plan
// (docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md).
func TestGuestDisconnectEmitsSessionEndedOnCharacterStream(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	store := newTestEventStore()

	server := newDispatcherTestServer(t, store)

	ctx := context.Background()
	sessStore := server.sessionStore
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		ID:            sessionID.String(),
		CharacterID:   charID,
		CharacterName: "GuestChar",
		LocationID:    locationID,
		IsGuest:       true,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))

	resp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
		Meta:               &corev1.RequestMeta{RequestId: "guest-disconnect-test", Timestamp: timestamppb.Now()},
		SessionId:          sessionID.String(),
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// session_ended event should be present on character stream with
	// cause=guest_end, the correct SessionID, and Reason="Session ended.".
	charEvents, err := store.Replay(ctx, "character."+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)

	var sessionEnded *eventbus.Event
	for i := range charEvents {
		if charEvents[i].Type == eventbus.Type(eventvocab.EventTypeSessionEnded) {
			sessionEnded = &charEvents[i]
			break
		}
	}
	require.NotNil(t, sessionEnded, "expected a session_ended event on character stream for guest disconnect")
	assert.Equal(t, eventbus.Subject("events.main.character."+charID.String()), sessionEnded.Subject)

	var payload core.SessionEndedPayload
	require.NoError(t, json.Unmarshal(sessionEnded.Payload, &payload))
	assert.Equal(t, sessionID.String(), payload.SessionID)
	assert.Equal(t, charID.String(), payload.CharacterID)
	assert.Equal(t, core.SessionEndedCauseGuestEnd, payload.Cause)
	assert.Equal(t, "Session ended.", payload.Reason)
}

// removedSignalStore wraps a session.Store and forces RemoveConnectionAndCount
// to return fixed (counts, removed) values, so Disconnect's duplicate-disconnect
// guard can be exercised deterministically (holomush-cizj). All other methods
// delegate to the embedded store.
type removedSignalStore struct {
	session.Store
	counts  session.ConnectionCounts
	removed bool
}

func (s removedSignalStore) RemoveConnectionAndCount(
	context.Context, string, ulid.ULID,
) (session.ConnectionCounts, bool, error) {
	return s.counts, s.removed, nil
}

// TestDisconnectGatesGuestCleanupOnRemovalSignal pins the holomush-cizj
// duplicate-disconnect guard at the Disconnect level: guest cleanup
// (HandleDisconnect + EndSession) runs only for the caller whose
// RemoveConnectionAndCount actually removed the connection. A duplicate
// disconnect for an already-removed connection_id reports removed=false and,
// even with Total==0, MUST NOT re-emit session_ended.
func TestDisconnectGatesGuestCleanupOnRemovalSignal(t *testing.T) {
	tests := []struct {
		name           string
		removed        bool
		wantEndedCount int
	}{
		{"removing caller runs cleanup", true, 1},
		{"duplicate no-op removal skips cleanup", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charID := core.NewULID()
			sessionID := core.NewULID()
			locationID := core.NewULID()
			store := newTestEventStore()
			server := newDispatcherTestServer(t, store)

			ctx := context.Background()
			require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
				ID:            sessionID.String(),
				CharacterID:   charID,
				CharacterName: "GuestChar",
				LocationID:    locationID,
				IsGuest:       true,
				Status:        session.StatusActive,
				TTLSeconds:    1800,
			}))
			// Wrap so RemoveConnectionAndCount reports Total==0 with the removed
			// signal under test, deleting nothing — isolating the gate.
			server.sessionStore = removedSignalStore{
				Store:   server.sessionStore,
				counts:  session.ConnectionCounts{Total: 0, Grid: 0},
				removed: tt.removed,
			}
			// The cluster units snapshot their collaborators at construction,
			// so a post-construction store swap needs a rebuild to reach them.
			server.buildHandlers()

			resp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
				Meta:               &corev1.RequestMeta{RequestId: "guard-test", Timestamp: timestamppb.Now()},
				SessionId:          sessionID.String(),
				ConnectionId:       ulid.Make().String(),
				PlayerSessionToken: testPlayerSessionToken,
			})
			require.NoError(t, err)
			assert.True(t, resp.Success, "Disconnect is idempotent success regardless of the guard")

			charEvents, err := store.Replay(ctx, "character."+charID.String(), ulid.ULID{}, 100)
			require.NoError(t, err)
			ended := 0
			for i := range charEvents {
				if charEvents[i].Type == eventbus.Type(eventvocab.EventTypeSessionEnded) {
					ended++
				}
			}
			assert.Equal(t, tt.wantEndedCount, ended,
				"holomush-cizj: guest session_ended emission MUST be gated on the removal signal")
		})
	}
}

// disconnectErrStore wraps a session.Store and injects errors into the methods
// Disconnect calls while counting connections, so the handler's defensive
// skip/log branches can be exercised. Unset error fields delegate to the
// embedded store.
type disconnectErrStore struct {
	session.Store
	rcacErr   error // RemoveConnectionAndCount
	countErr  error // CountConnections
	byTypeErr error // CountConnectionsByType
}

func (s disconnectErrStore) RemoveConnectionAndCount(
	ctx context.Context, sid string, cid ulid.ULID,
) (session.ConnectionCounts, bool, error) {
	if s.rcacErr != nil {
		return session.ConnectionCounts{}, false, s.rcacErr
	}
	return s.Store.RemoveConnectionAndCount(ctx, sid, cid)
}

func (s disconnectErrStore) CountConnections(ctx context.Context, sid string) (int, error) {
	if s.countErr != nil {
		return 0, s.countErr
	}
	return s.Store.CountConnections(ctx, sid)
}

func (s disconnectErrStore) CountConnectionsByType(ctx context.Context, sid, ct string) (int, error) {
	if s.byTypeErr != nil {
		return 0, s.byTypeErr
	}
	return s.Store.CountConnectionsByType(ctx, sid, ct)
}

// TestDisconnectToleratesStoreCountErrors covers Disconnect's defensive
// branches when the session store errors while counting connections: a
// RemoveConnectionAndCount error (connection_id path) and a CountConnections
// error (session-level path) skip the lifecycle transition; CountConnectionsByType
// errors are logged but non-fatal. Every case still returns idempotent success.
func TestDisconnectToleratesStoreCountErrors(t *testing.T) {
	boom := errors.New("store boom")
	tests := []struct {
		name   string
		connID string
		wrap   func(base session.Store) session.Store
	}{
		{
			name:   "RemoveConnectionAndCount error on connection_id path",
			connID: ulid.Make().String(),
			wrap:   func(base session.Store) session.Store { return disconnectErrStore{Store: base, rcacErr: boom} },
		},
		{
			name:   "CountConnections error on session-level path",
			connID: "",
			wrap:   func(base session.Store) session.Store { return disconnectErrStore{Store: base, countErr: boom} },
		},
		{
			name:   "CountConnectionsByType errors are non-fatal",
			connID: "",
			wrap:   func(base session.Store) session.Store { return disconnectErrStore{Store: base, byTypeErr: boom} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			charID := core.NewULID()
			sessionID := core.NewULID()
			store := newTestEventStore()
			server := newDispatcherTestServer(t, store)

			ctx := context.Background()
			require.NoError(t, server.sessionStore.Set(ctx, sessionID.String(), &session.Info{
				ID:          sessionID.String(),
				CharacterID: charID,
				Status:      session.StatusActive,
				TTLSeconds:  1800,
			}))
			server.sessionStore = tt.wrap(server.sessionStore)

			resp, err := server.Disconnect(ctx, &corev1.DisconnectRequest{
				Meta:               &corev1.RequestMeta{RequestId: "err-path-test", Timestamp: timestamppb.Now()},
				SessionId:          sessionID.String(),
				ConnectionId:       tt.connID,
				PlayerSessionToken: testPlayerSessionToken,
			})
			require.NoError(t, err)
			assert.True(t, resp.Success, "Disconnect returns idempotent success despite store count errors")
		})
	}
}

// TestAdminBootEmitsSessionEndedWithKickedCause verifies that the admin-boot
// teardown path (triggered when a command records a BootedSession via
// exec.RecordBootedSession) emits a session_ended event on the target
// character's own stream with cause=kicked and a reason referencing the
// administrator. See Task 8 of the session lifecycle as events plan
// (docs/superpowers/specs/2026-04-18-session-lifecycle-as-events-design.md).
func TestAdminBootEmitsSessionEndedWithKickedCause(t *testing.T) {
	adminCharID := core.NewULID()
	adminSessionID := core.NewULID()
	adminLocationID := core.NewULID()

	targetCharID := core.NewULID()
	targetSessionID := core.NewULID()
	targetLocationID := core.NewULID()

	store := newTestEventStore()

	// Build a server with a "testboot" command that records the target
	// session as booted. Server teardown logic then emits the session_ended
	// event, runs disconnect hooks, and deletes the session.
	pres := newTestPresenceEmitter(store)
	sessStore := sessiontest.NewStore(t)
	reg := command.NewRegistry()
	registerTestCommands(t, reg, store)

	entry, err := command.NewCommandEntry(command.CommandEntryConfig{
		Name:   "testboot",
		Source: "test",
		Handler: func(_ context.Context, exec *command.CommandExecution) error {
			exec.RecordBootedSession(command.BootedSession{
				// Leave CharacterRef zero so server.go looks up the
				// target session and fills in the ref — this exercises
				// the full plugin-originated boot path.
				SessionInfo: session.Info{ID: targetSessionID.String()},
			})
			return nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, reg.Register(*entry))

	policyEngine := policytest.AllowAllEngine()
	svc := command.NewTestServices(command.ServicesConfig{
		World:   nil,
		Session: sessStore,
		Engine:  policyEngine,
	})
	dispatcher, err := command.NewDispatcher(reg, policyEngine)
	require.NoError(t, err)

	server := NewCoreServer(
		pres, sessStore, dispatcher, svc,
		WithEventPublisher(store, func() string { return "main" }),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(ulid.ULID{})),
	)

	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, adminSessionID.String(), &session.Info{
		ID:            adminSessionID.String(),
		CharacterID:   adminCharID,
		CharacterName: "Admin",
		LocationID:    adminLocationID,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))
	require.NoError(t, sessStore.Set(ctx, targetSessionID.String(), &session.Info{
		ID:            targetSessionID.String(),
		CharacterID:   targetCharID,
		CharacterName: "Target",
		LocationID:    targetLocationID,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:               &corev1.RequestMeta{RequestId: "boot-test", Timestamp: timestamppb.Now()},
		SessionId:          adminSessionID.String(),
		Command:            "testboot",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// session_ended event should be present on the TARGET character's
	// stream with cause=kicked, the correct SessionID, and a reason
	// mentioning the administrator.
	charEvents, err := store.Replay(ctx, "character."+targetCharID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)

	var sessionEnded *eventbus.Event
	for i := range charEvents {
		if charEvents[i].Type == eventbus.Type(eventvocab.EventTypeSessionEnded) {
			sessionEnded = &charEvents[i]
			break
		}
	}
	require.NotNil(t, sessionEnded, "expected a session_ended event on target character stream for admin boot")
	assert.Equal(t, eventbus.Subject("events.main.character."+targetCharID.String()), sessionEnded.Subject)

	var payload core.SessionEndedPayload
	require.NoError(t, json.Unmarshal(sessionEnded.Payload, &payload))
	assert.Equal(t, targetSessionID.String(), payload.SessionID)
	assert.Equal(t, targetCharID.String(), payload.CharacterID)
	assert.Equal(t, core.SessionEndedCauseKicked, payload.Cause)
	assert.Contains(t, payload.Reason, "administrator",
		"reason should reference the administrator performing the boot")
}

// TestAdminBootRetainsSessionWhenEndSessionFails verifies that when EndSession
// fails on the admin-boot path, the target session row is RETAINED (not
// deleted) so the reaper can retry — otherwise subscribers lose STREAM_CLOSED.
// Also verifies the loop continues past the failed target (other booted
// sessions are still processed).
func TestAdminBootRetainsSessionWhenEndSessionFails(t *testing.T) {
	adminCharID := core.NewULID()
	adminSessionID := core.NewULID()
	adminLocationID := core.NewULID()

	targetCharID := core.NewULID()
	targetSessionID := core.NewULID()
	targetLocationID := core.NewULID()

	// Fail publish for session_ended events specifically; allow all others.
	store := &mockEventStore{
		publishFunc: func(_ context.Context, ev eventbus.Event) error {
			if ev.Type == eventbus.Type(eventvocab.EventTypeSessionEnded) {
				return errors.New("event store unavailable")
			}
			return nil
		},
	}

	pres := newTestPresenceEmitter(store)
	sessStore := sessiontest.NewStore(t)
	reg := command.NewRegistry()
	registerTestCommands(t, reg, store)

	entry, err := command.NewCommandEntry(command.CommandEntryConfig{
		Name:   "testboot",
		Source: "test",
		Handler: func(_ context.Context, exec *command.CommandExecution) error {
			exec.RecordBootedSession(command.BootedSession{
				SessionInfo: session.Info{ID: targetSessionID.String()},
			})
			return nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, reg.Register(*entry))

	policyEngine := policytest.AllowAllEngine()
	svc := command.NewTestServices(command.ServicesConfig{
		World:   nil,
		Session: sessStore,
		Engine:  policyEngine,
	})
	dispatcher, err := command.NewDispatcher(reg, policyEngine)
	require.NoError(t, err)

	var hookCalled bool
	server := NewCoreServer(
		pres, sessStore, dispatcher, svc,
		WithEventPublisher(store, func() string { return "main" }),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(ulid.ULID{})),
		WithDisconnectHook(func(_ session.Info) {
			hookCalled = true
		}),
	)

	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, adminSessionID.String(), &session.Info{
		ID:            adminSessionID.String(),
		CharacterID:   adminCharID,
		CharacterName: "Admin",
		LocationID:    adminLocationID,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))
	require.NoError(t, sessStore.Set(ctx, targetSessionID.String(), &session.Info{
		ID:            targetSessionID.String(),
		CharacterID:   targetCharID,
		CharacterName: "Target",
		LocationID:    targetLocationID,
		Status:        session.StatusActive,
		TTLSeconds:    1800,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:               &corev1.RequestMeta{RequestId: "boot-fail-test", Timestamp: timestamppb.Now()},
		SessionId:          adminSessionID.String(),
		Command:            "testboot",
		PlayerSessionToken: testPlayerSessionToken,
	})
	require.NoError(t, err)
	assert.True(t, resp.Success)

	// Target session must be RETAINED despite EndSession error so the reaper
	// can retry and emit STREAM_CLOSED to subscribers.
	info, getErr := sessStore.Get(ctx, targetSessionID.String())
	require.NoError(t, getErr, "target session must be retained when EndSession fails")
	assert.Equal(t, targetSessionID.String(), info.ID)

	// Disconnect hook must still fire so in-process cleanup proceeds.
	assert.True(t, hookCalled, "disconnect hook should fire even when EndSession fails")
}

// TestHandleCommand_ConnectionIDThreadedToExecution verifies that
// HandleCommandRequest.connection_id is threaded through the gRPC server-side
// handler into CommandExecutionConfig.ConnectionID so that T20-T23 scene-focus
// autofocus commands can identify the originating connection.
// This is the wire-level companion to TestDispatcher_PassesConnectionIDToPluginCommand
// (which verifies the Go-side dispatcher round-trip).
func TestHandleCommand_ConnectionIDThreadedToExecution(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	connID := core.NewULID()

	var capturedConnID ulid.ULID

	store := &mockEventStore{}
	pres := newTestPresenceEmitter(store)
	sessStore := sessiontest.NewStore(t)

	reg := command.NewRegistry()
	// Register a probe command that captures exec.ConnectionID().
	entry, err := command.NewCommandEntry(command.CommandEntryConfig{
		Name: "probeconnid",
		Handler: func(_ context.Context, exec *command.CommandExecution) error {
			capturedConnID = exec.ConnectionID()
			return nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, reg.Register(*entry))

	policyEngine := policytest.AllowAllEngine()
	svc := command.NewTestServices(command.ServicesConfig{
		World:   nil,
		Session: sessStore,
		Engine:  policyEngine,
	})
	dispatcher, err := command.NewDispatcher(reg, policyEngine)
	require.NoError(t, err)

	server := NewCoreServer(
		pres, sessStore, dispatcher, svc,
		WithEventPublisher(store, func() string { return "main" }),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(ulid.ULID{})),
	)

	ctx := context.Background()
	require.NoError(t, sessStore.Set(ctx, sessionID.String(), &session.Info{
		ID:          sessionID.String(),
		CharacterID: charID,
		LocationID:  locationID,
		Status:      session.StatusActive,
	}))

	resp, err := server.HandleCommand(ctx, &corev1.HandleCommandRequest{
		Meta:               &corev1.RequestMeta{RequestId: "connid-test"},
		SessionId:          sessionID.String(),
		Command:            "probeconnid",
		PlayerSessionToken: testPlayerSessionToken,
		ConnectionId:       connID.String(),
	})
	require.NoError(t, err)
	assert.True(t, resp.Success, "probeconnid should succeed: %s", resp.Error)

	assert.Equal(t, connID, capturedConnID,
		"HandleCommandRequest.connection_id must reach CommandExecution.ConnectionID()")
}
