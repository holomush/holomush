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

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
)

func TestCommandEntry_HasRequiredFields(t *testing.T) {
	entry := &CommandEntry{
		Name:         "say",
		capabilities: []Capability{{Action: "emit", Resource: "stream", Scope: ScopeLocal}},
		Help:         "Say something to the room",
		Usage:        "say <message>",
		HelpText:     "Speaks a message to everyone in the current location.",
		Source:       "core",
	}

	assert.Equal(t, "say", entry.Name)
	assert.Equal(t, []Capability{{Action: "emit", Resource: "stream", Scope: ScopeLocal}}, entry.GetCapabilities())
	assert.Equal(t, "Say something to the room", entry.Help)
	assert.Equal(t, "say <message>", entry.Usage)
	assert.Equal(t, "Speaks a message to everyone in the current location.", entry.HelpText)
	assert.Equal(t, "core", entry.Source)
	assert.Nil(t, entry.Handler(), "Handler() should return nil when not set")
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
	assert.Nil(t, svc.Engine(), "Engine service should be nil when not set")
	assert.Nil(t, svc.Events(), "Events service should be nil when not set")
}

func TestNewServices_NilWorld_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:   nil,
		Session: &mockAccess{},
		Engine:  &mockEngine{},
		Events:  &mockEventStore{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "World")
}

func TestNewServices_NilSession_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:   &world.Service{},
		Session: nil,
		Engine:  &mockEngine{},
		Events:  &mockEventStore{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Session")
}

func TestNewServices_NilEngine_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:   &world.Service{},
		Session: &mockAccess{},
		Engine:  nil,
		Events:  &mockEventStore{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Engine")
}

func TestNewServices_NilEvents_ReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:   &world.Service{},
		Session: &mockAccess{},
		Engine:  &mockEngine{},
		Events:  nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Events")
}

func TestNewServices_AllValid_ReturnsServices(t *testing.T) {
	worldSvc := &world.Service{}
	sessionSvc := &mockAccess{}
	engine := &mockEngine{}
	eventStore := &mockEventStore{}

	svc, err := NewServices(ServicesConfig{
		World:   worldSvc,
		Session: sessionSvc,
		Engine:  engine,
		Events:  eventStore,
	})
	require.NoError(t, err)
	assert.Same(t, worldSvc, svc.World())
	assert.Same(t, sessionSvc, svc.Session())
	assert.Same(t, engine, svc.Engine())
	assert.Same(t, eventStore, svc.Events())
}

func TestNewServices_MultipleNil_ReturnsFirstError(t *testing.T) {
	// When multiple fields are nil, should return error mentioning
	// World since that's checked first
	_, err := NewServices(ServicesConfig{
		World:   nil,
		Session: nil,
		Engine:  nil,
		Events:  nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "World")
}

// Mock types for testing
type mockAccess struct{}

func (m *mockAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	return nil, nil
}

func (m *mockAccess) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, nil
}

func (m *mockAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID, _ string) (*session.Info, error) {
	return nil, nil
}

func (m *mockAccess) UpdateActivity(_ context.Context, _ string) error {
	return nil
}

func (m *mockAccess) FindByCharacterName(_ context.Context, _ string) (*session.Info, error) {
	return nil, nil
}

func (m *mockAccess) UpdateLastPaged(_ context.Context, _ string, _ string) error {
	return nil
}

func (m *mockAccess) UpdateLastWhispered(_ context.Context, _ string, _ string) error {
	return nil
}

type mockEngine struct{}

func (m *mockEngine) Evaluate(_ context.Context, _ types.AccessRequest) (types.Decision, error) {
	return types.Decision{}, nil
}

func (m *mockEngine) CanPerformAction(_ context.Context, _, _, _, _ string) (bool, error) {
	return false, nil
}

// TestDecision_ZeroValue_IsDeny verifies that the zero-value Decision denies access.
// This is critical for fail-closed security - mocks returning Decision{} must deny by default.
func TestDecision_ZeroValue_IsDeny(t *testing.T) {
	t.Parallel()

	var d types.Decision
	assert.False(t, d.IsAllowed(), "Zero-value Decision should deny access (fail-closed)")
	assert.Equal(t, types.EffectDefaultDeny, d.Effect(), "Zero-value Decision should have DefaultDeny effect")
}

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
		Capabilities: []Capability{{Action: "emit", Resource: "stream", Scope: ScopeLocal}},
		Help:         "Say something to the room",
		Usage:        "say <message>",
		HelpText:     "Speaks a message to everyone in the current location.",
		Source:       "core",
	})

	require.NoError(t, err)
	assert.Equal(t, "say", entry.Name)
	assert.NotNil(t, entry.Handler())
	assert.Equal(t, []Capability{{Action: "emit", Resource: "stream", Scope: ScopeLocal}}, entry.GetCapabilities())
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
	assert.NotNil(t, entry.Handler())
	assert.Empty(t, entry.GetCapabilities())
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

func TestServices_BroadcastSystemMessage_NilEvents_IsNoOp(t *testing.T) {
	t.Parallel()

	// Create services with nil Events store
	svc := NewTestServices(ServicesConfig{
		Events: nil,
	})

	// Should not panic - this is a silent no-op
	assert.NotPanics(t, func() {
		svc.BroadcastSystemMessage(context.Background(), "test-stream", "test message")
	})
}

// Tests for CommandEntry.GetCapabilities defensive copy

func TestCommandEntry_GetCapabilities_ReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	capOne := Capability{Action: "read", Resource: "location", Scope: ScopeLocal}
	capTwo := Capability{Action: "write", Resource: "exit", Scope: ScopeLocal}
	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:         "test",
		Handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Capabilities: []Capability{capOne, capTwo},
	})
	require.NoError(t, err)

	// Get capabilities
	caps1 := entry.GetCapabilities()
	caps2 := entry.GetCapabilities()

	// Verify values match
	assert.Equal(t, []Capability{capOne, capTwo}, caps1)
	assert.Equal(t, []Capability{capOne, capTwo}, caps2)

	// Modify returned slice
	caps1[0] = Capability{Action: "admin", Resource: "server", Scope: ScopeGlobal}

	// Original should be unchanged
	caps3 := entry.GetCapabilities()
	assert.Equal(t, []Capability{capOne, capTwo}, caps3,
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
		Capabilities: []Capability{}, // Explicitly empty
	})
	require.NoError(t, err)

	caps := entry.GetCapabilities()
	assert.NotNil(t, caps, "Should return non-nil empty slice")
	assert.Empty(t, caps, "Should return empty slice")
}

// Tests for Handler() getter

func TestCommandEntry_Handler_ReturnsHandler(t *testing.T) {
	t.Parallel()

	handlerCalled := false
	handler := func(_ context.Context, _ *CommandExecution) error {
		handlerCalled = true
		return nil
	}

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:    "test",
		Handler: handler,
	})
	require.NoError(t, err)

	// Get handler via getter
	retrievedHandler := entry.Handler()
	assert.NotNil(t, retrievedHandler, "Handler() should return non-nil handler")

	// Verify it's callable and works correctly
	err = retrievedHandler(context.Background(), &CommandExecution{})
	assert.NoError(t, err)
	assert.True(t, handlerCalled, "Handler should have been called")
}

func TestCommandEntry_Handler_IsReadOnly(t *testing.T) {
	t.Parallel()

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:    "test",
		Handler: func(_ context.Context, _ *CommandExecution) error { return nil },
	})
	require.NoError(t, err)

	// Handler field should not be directly accessible (compile-time check)
	// The following would fail to compile if uncommented:
	// entry.handler = nil  // ERROR: handler is private

	// But we can read it via the getter
	h := entry.Handler()
	assert.NotNil(t, h)
}

func TestCapability_Validate_Valid(t *testing.T) {
	tests := []struct {
		name string
		cap  Capability
	}{
		{"basic", Capability{Action: "read", Resource: "location"}},
		{"with local scope", Capability{Action: "write", Resource: "exit", Scope: ScopeLocal}},
		{"with global scope", Capability{Action: "admin", Resource: "server", Scope: ScopeGlobal}},
		{"self scope explicit", Capability{Action: "write", Resource: "character", Scope: ScopeSelf}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, tt.cap.Validate())
		})
	}
}

func TestCapability_Validate_Invalid(t *testing.T) {
	tests := []struct {
		name string
		cap  Capability
		want string
	}{
		{"empty action", Capability{Action: "", Resource: "location"}, "action"},
		{"empty resource", Capability{Action: "read", Resource: ""}, "resource"},
		{"unknown action", Capability{Action: "destroy", Resource: "location"}, "action"},
		{"unknown resource", Capability{Action: "read", Resource: "spaceship"}, "resource"},
		{"invalid scope", Capability{Action: "read", Resource: "location", Scope: "everywhere"}, "scope"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cap.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestCapability_EffectiveScope(t *testing.T) {
	assert.Equal(t, ScopeSelf, Capability{Action: "read", Resource: "character"}.EffectiveScope())
	assert.Equal(t, ScopeLocal, Capability{Action: "read", Resource: "location", Scope: ScopeLocal}.EffectiveScope())
	assert.Equal(t, ScopeGlobal, Capability{Action: "emit", Resource: "stream", Scope: ScopeGlobal}.EffectiveScope())
}

func TestServices_BroadcastSystemMessage_CreatesCorrectEvent(t *testing.T) {
	ctx := context.Background()
	store := core.NewMemoryEventStore()
	stream := "test-stream"
	testMessage := "Server is shutting down"

	svc := NewTestServices(ServicesConfig{
		Events: store,
	})

	// Append the message
	svc.BroadcastSystemMessage(ctx, stream, testMessage)

	// Replay to retrieve the appended event
	events, err := store.Replay(ctx, stream, ulid.ULID{}, 10)
	require.NoError(t, err, "Replay should succeed")
	require.Len(t, events, 1, "Expected exactly one event")

	event := events[0]

	// Verify stream
	assert.Equal(t, stream, event.Stream, "Stream should match input")

	// Verify event type
	assert.Equal(t, core.EventTypeSystem, event.Type, "Type should be EventTypeSystem")

	// Verify actor
	assert.Equal(t, core.ActorSystem, event.Actor.Kind, "Actor.Kind should be ActorSystem")
	assert.Equal(t, "system", event.Actor.ID, "Actor.ID should be 'system'")

	// Verify payload contains message
	var payload map[string]string
	require.NoError(t, json.Unmarshal(event.Payload, &payload), "Payload should be valid JSON")
	assert.Equal(t, testMessage, payload["message"], "Payload should contain the message")

	// Verify ID is set (non-zero)
	assert.False(t, event.ID.IsZero(), "Event ID should be set")

	// Verify timestamp is recent
	assert.WithinDuration(t, time.Now(), event.Timestamp, time.Second,
		"Timestamp should be recent")
}
