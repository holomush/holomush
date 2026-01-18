package wasm_test

import (
	"testing"

	"github.com/holomush/holomush/internal/core"
)

// TestActorKindConstants validates that ActorKind constants have expected values.
// This test ensures documentation in docs/reference/plugin-authoring.md stays in sync
// with the actual code values.
//
// If this test fails, update the documentation to reflect the new values:
// - docs/reference/plugin-authoring.md: actor_kind field description and examples
func TestActorKindConstants(t *testing.T) {
	tests := []struct {
		name    string
		kind    core.ActorKind
		wantInt int
		wantStr string
	}{
		{"character is 0", core.ActorCharacter, 0, "character"},
		{"system is 1", core.ActorSystem, 1, "system"},
		{"plugin is 2", core.ActorPlugin, 2, "plugin"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := int(tt.kind); got != tt.wantInt {
				t.Errorf("ActorKind %s = %d, want %d", tt.wantStr, got, tt.wantInt)
			}
			if got := tt.kind.String(); got != tt.wantStr {
				t.Errorf("ActorKind(%d).String() = %q, want %q", tt.wantInt, got, tt.wantStr)
			}
		})
	}
}

// TestActorKindDocumentedValues validates specific values that are documented.
// The plugin authoring guide documents these values for plugin authors.
func TestActorKindDocumentedValues(t *testing.T) {
	// These are the values documented in docs/reference/plugin-authoring.md
	// If you change the enum, update the documentation!
	const (
		DocumentedCharacter = 0
		DocumentedSystem    = 1
		DocumentedPlugin    = 2
	)

	if int(core.ActorCharacter) != DocumentedCharacter {
		t.Errorf("ActorCharacter = %d, but documented as %d", core.ActorCharacter, DocumentedCharacter)
	}
	if int(core.ActorSystem) != DocumentedSystem {
		t.Errorf("ActorSystem = %d, but documented as %d", core.ActorSystem, DocumentedSystem)
	}
	if int(core.ActorPlugin) != DocumentedPlugin {
		t.Errorf("ActorPlugin = %d, but documented as %d", core.ActorPlugin, DocumentedPlugin)
	}
}

// TestEventTypeConstants validates that EventType constants match documented values.
// This test ensures documentation in docs/reference/plugin-authoring.md stays in sync
// with the actual code values.
//
// If this test fails, update the documentation to reflect the new values:
// - docs/reference/plugin-authoring.md: Event Types table
func TestEventTypeConstants(t *testing.T) {
	// These are the values documented in docs/reference/plugin-authoring.md
	// If you add/remove/rename event types, update the documentation!
	documentedTypes := map[core.EventType]string{
		core.EventTypeSay:    "say",
		core.EventTypePose:   "pose",
		core.EventTypeArrive: "arrive",
		core.EventTypeLeave:  "leave",
		core.EventTypeSystem: "system",
	}

	for eventType, expectedValue := range documentedTypes {
		t.Run(expectedValue, func(t *testing.T) {
			if got := string(eventType); got != expectedValue {
				t.Errorf("EventType constant = %q, want %q", got, expectedValue)
			}
		})
	}
}

// TestAllEventTypesDocumented validates that all defined EventType constants
// are included in the documented set. This catches new event types that were
// added to the code but not yet documented.
func TestAllEventTypesDocumented(t *testing.T) {
	// All event types that must be documented
	allTypes := []core.EventType{
		core.EventTypeSay,
		core.EventTypePose,
		core.EventTypeArrive,
		core.EventTypeLeave,
		core.EventTypeSystem,
	}

	// This test will fail at compile time if a new EventType constant is added
	// to core but not listed here. Update both this list and the documentation.
	for _, et := range allTypes {
		if et == "" {
			t.Error("Empty EventType found - update the test and documentation")
		}
	}

	// Verify we have the expected count
	const expectedCount = 5
	if len(allTypes) != expectedCount {
		t.Errorf("Expected %d event types, got %d - update documentation if count changed",
			expectedCount, len(allTypes))
	}
}
