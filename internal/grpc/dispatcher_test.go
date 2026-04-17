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

// registerTestCommands registers quit/shutdown (compiled-in) plus stub handlers
// for say, pose, and ooc so that dispatcher and pipeline tests can exercise the
// full dispatch path. The stub payloads match the format expected by the web
// translation layer (character_name, message, action fields).
func registerTestCommands(t *testing.T, reg *command.Registry) {
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
			event := core.NewEvent(
				"location:"+exec.LocationID().String(),
				core.EventTypeSay,
				core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
				payload,
			)
			return exec.Services().Events().Append(ctx, event)
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
			event := core.NewEvent(
				"location:"+exec.LocationID().String(),
				core.EventTypePose,
				core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
				payload,
			)
			return exec.Services().Events().Append(ctx, event)
		},
	})
	mustRegister(command.CommandEntryConfig{
		Name:   "ooc",
		Source: "test",
		Handler: func(ctx context.Context, exec *command.CommandExecution) error {
			payload, _ := json.Marshal(core.OOCPayload{
				CharacterName: exec.CharacterName(),
				Message:       exec.Args,
				Style:         "say",
			})
			event := core.NewEvent(
				"location:"+exec.LocationID().String(),
				core.EventTypeOOC,
				core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
				payload,
			)
			return exec.Services().Events().Append(ctx, event)
		},
	})
}

// newDispatcherTestServer creates a CoreServer wired with the unified
// command dispatcher, using in-memory stores for testing.
func newDispatcherTestServer(t *testing.T, store core.EventStore, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	return newHandleCommandServer(t, store, nil, opts...)
}

// newDispatcherTestServerWithAliases creates a CoreServer with MUSH aliases
// (`:`, `;`, `"`) loaded into the alias cache, matching production bootstrap.
func newDispatcherTestServerWithAliases(t *testing.T, store core.EventStore, opts ...CoreServerOption) *CoreServer {
	t.Helper()
	engine := core.NewEngine(store)
	sessStore := session.NewMemStore()

	reg := command.NewRegistry()
	registerTestCommands(t, reg)

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
		Events:  store,
	})

	dispatcher, err := command.NewDispatcher(reg, policyEngine,
		command.WithAliasCache(aliasCache),
	)
	require.NoError(t, err)

	allOpts := make([]CoreServerOption, 0, 2+len(opts))
	allOpts = append(allOpts,
		WithEventStore(store),
		WithPlayerSessionRepo(newFakePlayerSessionRepo(ulid.ULID{})),
	)
	allOpts = append(allOpts, opts...)

	return NewCoreServer(engine, sessStore, dispatcher, svc, allOpts...)
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
	assert.Equal(t, core.EventTypePose, appended[0].Type)
}

func TestDispatcher_HandleCommand_UnknownCommand(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	store := core.NewMemoryEventStore()

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
	charEvents, err := store.Replay(ctx, "character:"+charID.String(), ulid.ULID{}, 100)
	require.NoError(t, err)
	require.NotEmpty(t, charEvents, "expected command_response event")
	assert.Equal(t, core.EventTypeCommandError, charEvents[0].Type)

	var crp core.CommandResponsePayload
	require.NoError(t, json.Unmarshal(charEvents[0].Payload, &crp))
}

func TestDispatcher_HandleCommand_Quit(t *testing.T) {
	charID := core.NewULID()
	sessionID := core.NewULID()
	locationID := core.NewULID()
	store := core.NewMemoryEventStore()

	var hookCalled bool
	server := newDispatcherTestServer(t, store,
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
	assert.Contains(t, crp.Text, "Goodbye")

	// Disconnect hook should fire
	assert.True(t, hookCalled)
}
