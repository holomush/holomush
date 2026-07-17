// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/core/coretest"
	"github.com/holomush/holomush/internal/eventvocab"
	"github.com/holomush/holomush/internal/session"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/errutil"
)

func TestCommandEntryHasRequiredFields(t *testing.T) {
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

func TestCommandExecutionHasRequiredFields(t *testing.T) {
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

func TestServicesHasAllDependencies(t *testing.T) {
	svc := NewTestServices(ServicesConfig{})

	assert.Nil(t, svc.World(), "World service should be nil when not set")
	assert.Nil(t, svc.Session(), "Session service should be nil when not set")
	assert.Nil(t, svc.Engine(), "Engine service should be nil when not set")
	assert.Nil(t, svc.Events(), "Events service should be nil when not set")
}

func TestNewServicesNilWorldReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:   nil,
		Session: &mockAccess{},
		Engine:  &mockEngine{},
		Events:  &mockEventStore{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "World")
}

func TestNewServicesNilSessionReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:   &world.Service{},
		Session: nil,
		Engine:  &mockEngine{},
		Events:  &mockEventStore{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Session")
}

func TestNewServicesNilEngineReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:   &world.Service{},
		Session: &mockAccess{},
		Engine:  nil,
		Events:  &mockEventStore{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Engine")
}

func TestNewServicesNilEventsReturnsError(t *testing.T) {
	_, err := NewServices(ServicesConfig{
		World:   &world.Service{},
		Session: &mockAccess{},
		Engine:  &mockEngine{},
		Events:  nil,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Events")
}

func TestNewServicesAllValidReturnsServices(t *testing.T) {
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

func TestNewServicesMultipleNilReturnsFirstError(t *testing.T) {
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

func (m *mockAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
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
func TestDecisionZeroValueIsDeny(t *testing.T) {
	t.Parallel()

	var d types.Decision
	assert.False(t, d.IsAllowed(), "Zero-value Decision should deny access (fail-closed)")
	assert.Equal(t, types.EffectDefaultDeny, d.Effect(), "Zero-value Decision should have DefaultDeny effect")
}

type mockEventStore struct{}

func (m *mockEventStore) Append(_ context.Context, _ core.Event) error { return nil }

var _ core.EventAppender = (*mockEventStore)(nil)

func TestCommandHandlerSignature(t *testing.T) {
	// Verify CommandHandler can be assigned a function with the correct signature
	var handler CommandHandler = func(_ context.Context, _ *CommandExecution) error {
		return nil
	}
	assert.NotNil(t, handler, "Handler should be assignable")
}

// Tests for NewCommandEntry constructor

func TestNewCommandEntryValidInputReturnsEntry(t *testing.T) {
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

func TestNewCommandEntryEmptyNameReturnsError(t *testing.T) {
	handler := func(_ context.Context, _ *CommandExecution) error { return nil }

	_, err := NewCommandEntry(CommandEntryConfig{
		Name:    "",
		Handler: handler,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Name")
}

func TestNewCommandEntryNilHandlerReturnsError(t *testing.T) {
	_, err := NewCommandEntry(CommandEntryConfig{
		Name:    "say",
		Handler: nil,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Handler")
}

func TestNewCommandEntryMinimalValidReturnsEntry(t *testing.T) {
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

func TestNewCommandExecutionValidInputReturnsExecution(t *testing.T) {
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

func TestNewCommandExecutionZeroCharacterIDReturnsError(t *testing.T) {
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

func TestNewCommandExecutionNilServicesReturnsError(t *testing.T) {
	output := &mockWriter{}

	_, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      output,
		Services:    nil,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Services")
}

func TestNewCommandExecutionNilOutputReturnsError(t *testing.T) {
	services := NewTestServices(ServicesConfig{})

	_, err := NewCommandExecution(CommandExecutionConfig{
		CharacterID: ulid.Make(),
		Output:      nil,
		Services:    services,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Output")
}

func TestNewCommandExecutionMinimalValidReturnsExecution(t *testing.T) {
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

func TestCommandExecutionGettersReturnCorrectValues(t *testing.T) {
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

func TestCommandExecutionPublicFieldsAreModifiable(t *testing.T) {
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

func TestServicesBroadcastSystemMessageNilEventsIsNoOp(t *testing.T) {
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

func TestCommandEntryGetCapabilitiesReturnsDefensiveCopy(t *testing.T) {
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

func TestCommandEntryGetCapabilitiesNilCapabilitiesReturnsNil(t *testing.T) {
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

func TestCommandEntryGetCapabilitiesEmptyCapabilitiesReturnsEmpty(t *testing.T) {
	t.Parallel()

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:         "test",
		Handler:      func(_ context.Context, _ *CommandExecution) error { return nil },
		Capabilities: []Capability{}, // Explicitly empty
	})
	require.NoError(t, err)

	caps := entry.GetCapabilities()
	assert.Empty(t, caps, "Should return empty capabilities")
	assert.Nil(t, caps, "Defensive copy returns nil for empty input")
}

// Tests for Handler() getter

func TestCommandEntryHandlerReturnsHandler(t *testing.T) {
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

func TestCommandEntryHandlerIsReadOnly(t *testing.T) {
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

func TestCapability_ValidateValid(t *testing.T) {
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

func TestCapability_ValidateInvalid(t *testing.T) {
	tests := []struct {
		name string
		cap  Capability
		want string
	}{
		{"empty action", Capability{Action: "", Resource: "location"}, "action"},
		{"empty resource", Capability{Action: "read", Resource: ""}, "resource"},
		// Note: unknown resource type is NOT checked by Validate() — it's checked
		// by ValidateResourceType() at load time with cross-plugin context.
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

func TestCapability_ValidateAcceptsUnknownResourceType(t *testing.T) {
	// Validate() is structural only — unknown resource types pass.
	// ValidateResourceType() checks membership at load time.
	c := Capability{Action: "read", Resource: "widget", Scope: ScopeLocal}
	assert.NoError(t, c.Validate())
}

func TestCapability_ValidateAcceptsUnknownAction(t *testing.T) {
	// Validate() is structural only — unknown actions pass here.
	// ValidateAction() checks membership at load time with cross-plugin context.
	c := Capability{Action: "destroy", Resource: "location"}
	assert.NoError(t, c.Validate())
}

func TestCapability_ValidateResourceTypeKnown(t *testing.T) {
	known := map[string]bool{"location": true, "widget": true}
	assert.NoError(t, Capability{Action: "read", Resource: "widget"}.ValidateResourceType(known))
	assert.NoError(t, Capability{Action: "read", Resource: "location"}.ValidateResourceType(known))
}

func TestCapability_ValidateResourceTypeUnknown(t *testing.T) {
	known := map[string]bool{"location": true}
	err := Capability{Action: "read", Resource: "spaceship"}.ValidateResourceType(known)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spaceship")
	errutil.AssertErrorCode(t, err, "INVALID_CAPABILITY")
}

func TestCoreResourceTypesReturnsCopy(t *testing.T) {
	types := CoreResourceTypes()
	assert.True(t, types["location"])
	assert.True(t, types["character"])
	// Mutating the copy doesn't affect the original
	types["spaceship"] = true
	types2 := CoreResourceTypes()
	assert.False(t, types2["spaceship"])
}

func TestCoreActionsContainsExpectedDefaults(t *testing.T) {
	actions := CoreActions()
	for _, expected := range []string{"read", "write", "emit", "enter", "use", "delete", "execute", "admin"} {
		assert.True(t, actions[expected], "core action %q must be present", expected)
	}
}

func TestCoreActionsReturnsCopy(t *testing.T) {
	actions := CoreActions()
	assert.True(t, actions["read"])
	// Mutating the copy must not affect a second call.
	actions["invent"] = true
	actions2 := CoreActions()
	assert.False(t, actions2["invent"], "subsequent calls must not see prior mutations")
}

func TestCapability_ValidateActionWithKnownAction(t *testing.T) {
	known := map[string]bool{"join": true, "read": true}
	assert.NoError(t, Capability{Action: "join", Resource: "channel"}.ValidateAction(known))
	assert.NoError(t, Capability{Action: "read", Resource: "location"}.ValidateAction(known))
}

func TestCapability_ValidateActionWithUnknownAction(t *testing.T) {
	known := map[string]bool{"read": true}
	err := Capability{Action: "join", Resource: "channel"}.ValidateAction(known)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "join")
	errutil.AssertErrorCode(t, err, "INVALID_CAPABILITY")
}

func TestCapability_ValidateActionBoundaryEmptyKnownMap(t *testing.T) {
	err := Capability{Action: "read", Resource: "location"}.ValidateAction(map[string]bool{})
	require.Error(t, err, "empty known map must reject any action")
}

func TestCapabilityEffectiveScope(t *testing.T) {
	assert.Equal(t, ScopeSelf, Capability{Action: "read", Resource: "character"}.EffectiveScope())
	assert.Equal(t, ScopeLocal, Capability{Action: "read", Resource: "location", Scope: ScopeLocal}.EffectiveScope())
	assert.Equal(t, ScopeGlobal, Capability{Action: "emit", Resource: "stream", Scope: ScopeGlobal}.EffectiveScope())
}

func TestNewCommandEntry_InvalidCapabilityReturnsError(t *testing.T) {
	handler := func(_ context.Context, _ *CommandExecution) error { return nil }

	tests := []struct {
		name string
		caps []Capability
		want string
	}{
		// Note: unknown resource type is NOT checked by Validate() at construction
		// time — resource type validation happens at load time via ValidateResourceType().
		{
			name: "invalid scope",
			caps: []Capability{{Action: "read", Resource: "location", Scope: "everywhere"}},
			want: "scope",
		},
		{
			name: "empty action",
			caps: []Capability{{Action: "", Resource: "location"}},
			want: "action",
		},
		{
			name: "empty resource",
			caps: []Capability{{Action: "read", Resource: ""}},
			want: "resource",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCommandEntry(CommandEntryConfig{
				Name:         "test",
				Handler:      handler,
				Capabilities: tt.caps,
			})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)

			oopsErr, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, "INVALID_CAPABILITY", oopsErr.Code())
		})
	}
}

func TestNewCommandEntryValidCapabilitiesSucceeds(t *testing.T) {
	handler := func(_ context.Context, _ *CommandExecution) error { return nil }

	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:    "teleport",
		Handler: handler,
		Capabilities: []Capability{
			{Action: "write", Resource: "location", Scope: ScopeGlobal},
			{Action: "enter", Resource: "location", Scope: ScopeGlobal},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "teleport", entry.Name)
	assert.Len(t, entry.GetCapabilities(), 2)
}

func TestNewCommandEntryPluginNameWithCapabilities(t *testing.T) {
	entry, err := NewCommandEntry(CommandEntryConfig{
		Name:       "dig",
		PluginName: "core-building",
		Capabilities: []Capability{
			{Action: "write", Resource: "exit"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "core-building", entry.PluginName())
	assert.Len(t, entry.GetCapabilities(), 1)
}

func TestServicesBroadcastSystemMessageCreatesCorrectEvent(t *testing.T) {
	ctx := context.Background()
	store := coretest.NewMemoryEventStore()
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
	assert.Equal(t, eventvocab.EventTypeSystem, event.Type, "Type should be EventTypeSystem")

	// Verify actor
	assert.Equal(t, core.ActorSystem, event.Actor.Kind, "Actor.Kind should be ActorSystem")
	assert.Equal(t, core.ActorSystemID, event.Actor.ID, "Actor.ID should be ActorSystemID")

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

func TestServicesBroadcastSystemMessageProducesMonotonicEventIDs(t *testing.T) {
	// BroadcastSystemMessage must mint monotonic ULIDs so two consecutive
	// broadcasts to the same stream do not produce lex-inverted event IDs.
	// Non-monotonic IDs cause PostgresEventStore.Replay to silently skip
	// events (it uses WHERE id > afterID ORDER BY id), and would cause
	// the PostgresSessionStore.UpdateCursors CAS to reject
	// legitimate cursor advances.
	//
	// Loops 1000 times because same-millisecond collisions are the
	// failure mode: on a fast machine this loop spans ~1-5ms and produces
	// many same-ms call pairs, exercising the regression scenario.
	captured := &captureEventStore{}
	svc := NewTestServices(ServicesConfig{
		Events: captured,
	})
	ctx := context.Background()

	const n = 1000
	for i := 0; i < n; i++ {
		svc.BroadcastSystemMessage(ctx, "stream:test", "msg")
	}

	require.Len(t, captured.events, n)
	for i := 1; i < n; i++ {
		prev := captured.events[i-1].ID.String()
		cur := captured.events[i].ID.String()
		// String comparison (not ID.Compare) is deliberate — it mirrors the
		// SQL semantics (WHERE id > afterID, COLLATE "C" CAS) that depend
		// on this property. See core.NewULID doc comment.
		require.True(t, prev < cur,
			"non-monotonic event IDs at index %d: prev=%s cur=%s",
			i, prev, cur)
	}
}

// captureEventStore is a minimal core.EventAppender fake that records every
// Append call for assertion.
type captureEventStore struct {
	events []core.Event
}

func (c *captureEventStore) Append(_ context.Context, ev core.Event) error {
	c.events = append(c.events, ev)
	return nil
}

var _ core.EventAppender = (*captureEventStore)(nil)
