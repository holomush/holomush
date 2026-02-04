// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

func TestCommandEntry_HasRequiredFields(t *testing.T) {
	entry := &CommandEntry{
		Name:         "say",
		Capabilities: []string{"rp:speak"},
		Help:         "Say something to the room",
		Usage:        "say <message>",
		HelpText:     "Speaks a message to everyone in the current location.",
		Source:       "core",
	}

	assert.Equal(t, "say", entry.Name)
	assert.Equal(t, []string{"rp:speak"}, entry.Capabilities)
	assert.Equal(t, "Say something to the room", entry.Help)
	assert.Equal(t, "say <message>", entry.Usage)
	assert.Equal(t, "Speaks a message to everyone in the current location.", entry.HelpText)
	assert.Equal(t, "core", entry.Source)
	assert.Nil(t, entry.Handler, "Handler should be nil when not set")
}

func TestCommandExecution_HasRequiredFields(t *testing.T) {
	// Create a minimal valid CommandExecution to test field access via getters
	exec, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &mockWriter{},
		Services:    NewTestServices(ServicesConfig{}),
	})
	require.NoError(t, err)

	// Verify getter methods exist and work (this validates the API)
	_ = exec.CharacterID()
	_ = exec.LocationID()
	_ = exec.CharacterName()
	_ = exec.PlayerID()
	_ = exec.SessionID()
	_ = exec.Output()
	_ = exec.Services()

	// Verify public fields are accessible
	_ = exec.Args
	_ = exec.InvokedAs
}

func TestServices_HasAllDependencies(t *testing.T) {
	svc := NewTestServices(ServicesConfig{})

	assert.Nil(t, svc.World(), "World service should be nil when not set")
	assert.Nil(t, svc.Session(), "Session service should be nil when not set")
	assert.Nil(t, svc.Access(), "Access service should be nil when not set")
	assert.Nil(t, svc.Events(), "Events service should be nil when not set")
}

func TestNewServices_NilWorld_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       nil,
		Session:     &mockSessionService{},
		Access:      &mockAccessControl{},
		Events:      &mockEventStore{},
		Broadcaster: &core.Broadcaster{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "World")
}

func TestNewServices_NilSession_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     nil,
		Access:      &mockAccessControl{},
		Events:      &mockEventStore{},
		Broadcaster: &core.Broadcaster{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Session")
}

func TestNewServices_NilAccess_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     &mockSessionService{},
		Access:      nil,
		Events:      &mockEventStore{},
		Broadcaster: &core.Broadcaster{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Access")
}

func TestNewServices_NilEvents_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     &mockSessionService{},
		Access:      &mockAccessControl{},
		Events:      nil,
		Broadcaster: &core.Broadcaster{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Events")
}

func TestNewServices_NilBroadcaster_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:       &world.Service{},
		Session:     &mockSessionService{},
		Access:      &mockAccessControl{},
		Events:      &mockEventStore{},
		Broadcaster: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Broadcaster")
}

func TestNewServices_AllValid_ReturnsServices(t *testing.T) {
	worldSvc := &world.Service{}
	sessionSvc := &mockSessionService{}
	accessCtrl := &mockAccessControl{}
	eventStore := &mockEventStore{}
	broadcaster := &core.Broadcaster{}

	svc, err := NewServices(ServicesConfig{
		World:       worldSvc,
		Session:     sessionSvc,
		Access:      accessCtrl,
		Events:      eventStore,
		Broadcaster: broadcaster,
	})
	require.NoError(t, err)
	assert.Same(t, worldSvc, svc.World())
	assert.Same(t, sessionSvc, svc.Session())
	assert.Same(t, accessCtrl, svc.Access())
	assert.Same(t, eventStore, svc.Events())
	assert.Same(t, broadcaster, svc.Broadcaster())
}

func TestNewServices_MultipleNil_ReturnsFirstError(t *testing.T) {
	// When multiple fields are nil, should return error mentioning
	// World since that's checked first
	_, err := NewServices(ServicesConfig{
		World:       nil,
		Session:     nil,
		Access:      nil,
		Events:      nil,
		Broadcaster: nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "World")
}

// Mock types for testing
type mockSessionService struct{}

func (m *mockSessionService) ListActiveSessions() []*core.Session  { return nil }
func (m *mockSessionService) GetSession(_ ulid.ULID) *core.Session { return nil }
func (m *mockSessionService) EndSession(_ ulid.ULID) error         { return nil }

type mockAccessControl struct{}

func (m *mockAccessControl) Check(_ context.Context, _, _, _ string) bool { return false }

type mockEventStore struct{}

func (m *mockEventStore) Append(_ context.Context, _ core.Event) error { return nil }
func (m *mockEventStore) Replay(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
	return nil, nil
}

func (m *mockEventStore) LastEventID(_ context.Context, _ string) (ulid.ULID, error) {
	return ulid.ULID{}, nil
}

func (m *mockEventStore) Subscribe(_ context.Context, _ string) (<-chan ulid.ULID, <-chan error, error) {
	return nil, nil, nil
}

func TestCommandHandler_Signature(t *testing.T) {
	// Verify CommandHandler can be assigned a function with the correct signature
	var handler CommandHandler = func(_ context.Context, _ *CommandExecution) error {
		return nil
	}
	assert.NotNil(t, handler, "Handler should be assignable")
}

// Tests for NewCommandEntry constructor

func TestNewCommandEntry_ValidInput_ReturnsEntry(t *testing.T) {
	handler := func(_ context.Context, _ *CommandExecution) error { return nil }

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:         "say",
		Handler:      handler,
		Capabilities: []string{"rp:speak"},
		Help:         "Say something to the room",
		Usage:        "say <message>",
		HelpText:     "Speaks a message to everyone in the current location.",
		Source:       "core",
	})

	require.NoError(t, err)
	assert.Equal(t, "say", entry.Name)
	assert.NotNil(t, entry.Handler)
	assert.Equal(t, []string{"rp:speak"}, entry.Capabilities)
	assert.Equal(t, "Say something to the room", entry.Help)
	assert.Equal(t, "say <message>", entry.Usage)
	assert.Equal(t, "Speaks a message to everyone in the current location.", entry.HelpText)
	assert.Equal(t, "core", entry.Source)
}

func TestNewCommandEntry_EmptyName_ReturnsError(t *testing.T) {
	handler := func(_ context.Context, _ *CommandExecution) error { return nil }

	_, err := NewCommandEntry(CommandEntryConfig{
		Name:    "",
		Handler: handler,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Name")
}

func TestNewCommandEntry_NilHandler_ReturnsError(t *testing.T) {
	_, err := NewCommandEntry(CommandEntryConfig{
		Name:    "say",
		Handler: nil,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Handler")
}

func TestNewCommandEntry_MinimalValid_ReturnsEntry(t *testing.T) {
	handler := func(_ context.Context, _ *CommandExecution) error { return nil }

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:    "say",
		Handler: handler,
	})

	require.NoError(t, err)
	assert.Equal(t, "say", entry.Name)
	assert.NotNil(t, entry.Handler)
	assert.Empty(t, entry.Capabilities)
	assert.Empty(t, entry.Help)
	assert.Empty(t, entry.Usage)
	assert.Empty(t, entry.HelpText)
	assert.Empty(t, entry.Source)
}

// Tests for NewCommandExecution constructor

func TestNewCommandExecution_ValidInput_ReturnsExecution(t *testing.T) {
	charID := ulid.Make()
	locID := ulid.Make()
	playerID := ulid.Make()
	sessionID := ulid.Make()
	output := &mockWriter{}
	services := NewTestServices(ServicesConfig{})

	exec, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID:   charID,
		LocationID:    locID,
		CharacterName: "Alice",
		PlayerID:      playerID,
		SessionID:     sessionID,
		Args:          "hello world",
		Output:        output,
		Services:      services,
		InvokedAs:     "say",
	})

	require.NoError(t, err)
	assert.Equal(t, charID, exec.CharacterID())
	assert.Equal(t, locID, exec.LocationID())
	assert.Equal(t, "Alice", exec.CharacterName())
	assert.Equal(t, playerID, exec.PlayerID())
	assert.Equal(t, sessionID, exec.SessionID())
	assert.Equal(t, "hello world", exec.Args)
	assert.Same(t, output, exec.Output())
	assert.Same(t, services, exec.Services())
	assert.Equal(t, "say", exec.InvokedAs)
}

func TestNewCommandExecution_ZeroCharacterID_ReturnsError(t *testing.T) {
	output := &mockWriter{}
	services := NewTestServices(ServicesConfig{})

	_, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID: ulid.ULID{}, // zero value
		Output:      output,
		Services:    services,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "CharacterID")
}

func TestNewCommandExecution_NilServices_ReturnsError(t *testing.T) {
	output := &mockWriter{}

	_, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      output,
		Services:    nil,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Services")
}

func TestNewCommandExecution_NilOutput_ReturnsError(t *testing.T) {
	services := NewTestServices(ServicesConfig{})

	_, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      nil,
		Services:    services,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Output")
}

func TestNewCommandExecution_MinimalValid_ReturnsExecution(t *testing.T) {
	charID := ulid.Make()
	output := &mockWriter{}
	services := NewTestServices(ServicesConfig{})

	exec, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID: charID,
		Output:      output,
		Services:    services,
	})

	require.NoError(t, err)
	assert.Equal(t, charID, exec.CharacterID())
	assert.Same(t, output, exec.Output())
	assert.Same(t, services, exec.Services())
	// Optional fields should be zero/empty
	assert.True(t, exec.LocationID().IsZero())
	assert.Empty(t, exec.CharacterName())
	assert.True(t, exec.PlayerID().IsZero())
	assert.True(t, exec.SessionID().IsZero())
	assert.Empty(t, exec.Args)
	assert.Empty(t, exec.InvokedAs)
}

// Tests for CommandExecution getters - verify immutability

func TestCommandExecution_Getters_ReturnCorrectValues(t *testing.T) {
	t.Parallel()

	charID := ulid.Make()
	locID := ulid.Make()
	playerID := ulid.Make()
	sessionID := ulid.Make()
	output := &mockWriter{}
	services := NewTestServices(ServicesConfig{})

	exec, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID:   charID,
		LocationID:    locID,
		CharacterName: "TestChar",
		PlayerID:      playerID,
		SessionID:     sessionID,
		Args:          "test args",
		Output:        output,
		Services:      services,
		InvokedAs:     "testcmd",
	})
	require.NoError(t, err)

	// Verify all getters return correct values
	assert.Equal(t, charID, exec.CharacterID())
	assert.Equal(t, locID, exec.LocationID())
	assert.Equal(t, "TestChar", exec.CharacterName())
	assert.Equal(t, playerID, exec.PlayerID())
	assert.Equal(t, sessionID, exec.SessionID())
	assert.Same(t, output, exec.Output())
	assert.Same(t, services, exec.Services())

	// Public fields should still be accessible directly
	assert.Equal(t, "test args", exec.Args)
	assert.Equal(t, "testcmd", exec.InvokedAs)
}

func TestCommandExecution_PublicFields_AreModifiable(t *testing.T) {
	t.Parallel()

	exec, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      &mockWriter{},
		Services:    NewTestServices(ServicesConfig{}),
	})
	require.NoError(t, err)

	// Args and InvokedAs should be modifiable by dispatcher
	exec.Args = "new args"
	exec.InvokedAs = "alias"

	assert.Equal(t, "new args", exec.Args)
	assert.Equal(t, "alias", exec.InvokedAs)
}

// mockWriter implements io.Writer for testing
type mockWriter struct{}

func (m *mockWriter) Write(p []byte) (n int, err error) {
	return len(p), nil
}

// Tests for BroadcastSystemMessage

func TestServices_BroadcastSystemMessage_NilBroadcaster_IsNoOp(t *testing.T) {
	t.Parallel()

	// Create services with nil Broadcaster
	svc := NewTestServices(ServicesConfig{
		Broadcaster: nil,
	})

	// Should not panic - this is a silent no-op
	assert.NotPanics(t, func() {
		svc.BroadcastSystemMessage("test-stream", "test message")
	})
}

// Tests for CommandEntry.GetCapabilities defensive copy

func TestCommandEntry_GetCapabilities_ReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:         "test",
		Handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Capabilities: []string{"cap:one", "cap:two"},
	})
	require.NoError(t, err)

	// Get capabilities
	caps1 := entry.GetCapabilities()
	caps2 := entry.GetCapabilities()

	// Verify values match
	assert.Equal(t, []string{"cap:one", "cap:two"}, caps1)
	assert.Equal(t, []string{"cap:one", "cap:two"}, caps2)

	// Modify returned slice
	caps1[0] = "cap:modified"

	// Original should be unchanged
	caps3 := entry.GetCapabilities()
	assert.Equal(t, []string{"cap:one", "cap:two"}, caps3,
		"Modifying returned slice should not affect entry")
}

func TestCommandEntry_GetCapabilities_NilCapabilities_ReturnsNil(t *testing.T) {
	t.Parallel()

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:    "test",
		Handler: func(_ context.Context, _ *CommandExecution) error { return nil },
		// No capabilities set
	})
	require.NoError(t, err)

	caps := entry.GetCapabilities()
	assert.Nil(t, caps, "Should return nil when no capabilities set")
}

func TestCommandEntry_GetCapabilities_EmptyCapabilities_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:         "test",
		Handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Capabilities: []string{}, // Explicitly empty
	})
	require.NoError(t, err)

	caps := entry.GetCapabilities()
	assert.NotNil(t, caps, "Should return non-nil empty slice")
	assert.Empty(t, caps, "Should return empty slice")
}

func TestServices_BroadcastSystemMessage_CreatesCorrectEvent(t *testing.T) {
	// Create a real broadcaster so we can subscribe and capture the event
	broadcaster := core.NewBroadcaster()
	stream := "test-stream"
	testMessage := "Server is shutting down"

	// Subscribe before broadcasting
	ch := broadcaster.Subscribe(stream)

	svc := NewTestServices(ServicesConfig{
		Broadcaster: broadcaster,
	})

	// Broadcast the message
	svc.BroadcastSystemMessage(stream, testMessage)

	// Receive the event
	select {
	case event := <-ch:
		// Verify stream
		assert.Equal(t, stream, event.Stream, "Stream should match input")

		// Verify event type
		assert.Equal(t, core.EventTypeSystem, event.Type, "Type should be EventTypeSystem")

		// Verify actor
		assert.Equal(t, core.ActorSystem, event.Actor.Kind, "Actor.Kind should be ActorSystem")
		assert.Equal(t, "system", event.Actor.ID, "Actor.ID should be 'system'")

		// Verify payload contains message
		var payload map[string]string
		err := json.Unmarshal(event.Payload, &payload)
		require.NoError(t, err, "Payload should be valid JSON")
		assert.Equal(t, testMessage, payload["message"], "Payload should contain the message")

		// Verify ID is set (non-zero)
		assert.False(t, event.ID.IsZero(), "Event ID should be set")

		// Verify timestamp is recent
		assert.WithinDuration(t, time.Now(), event.Timestamp, time.Second,
			"Timestamp should be recent")
	default:
		t.Fatal("Expected to receive an event from the broadcaster")
	}

	// Cleanup
	broadcaster.Unsubscribe(stream, ch)
}
