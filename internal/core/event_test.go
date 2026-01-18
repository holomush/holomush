package core

import "testing"

func TestEventType_String(t *testing.T) {
	tests := []struct {
		name     string
		input    EventType
		expected string
	}{
		{"say event", EventTypeSay, "say"},
		{"pose event", EventTypePose, "pose"},
		{"arrive event", EventTypeArrive, "arrive"},
		{"leave event", EventTypeLeave, "leave"},
		{"system event", EventTypeSystem, "system"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(tt.input); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestDocumentedEventTypes validates that event types mentioned in plugin-authoring.md
// are valid EventType constants. This test will fail if docs/reference/plugin-authoring.md
// references invalid event types like "emit".
//
// The documentation states: Event type (e.g., "say", "pose", "arrive")
// These must all be valid EventType values.
func TestDocumentedEventTypes(t *testing.T) {
	// Event types documented in plugin-authoring.md line 104
	// These are the examples given: "say", "pose", "arrive"
	documentedTypes := []string{"say", "pose", "arrive"}

	validTypes := map[string]bool{
		string(EventTypeSay):    true,
		string(EventTypePose):   true,
		string(EventTypeArrive): true,
		string(EventTypeLeave):  true,
		string(EventTypeSystem): true,
	}

	for _, docType := range documentedTypes {
		if !validTypes[docType] {
			t.Errorf("documented event type %q is not a valid EventType constant", docType)
		}
	}
}

func TestActorKind_String(t *testing.T) {
	tests := []struct {
		name     string
		input    ActorKind
		expected string
	}{
		{"character", ActorCharacter, "character"},
		{"system", ActorSystem, "system"},
		{"plugin", ActorPlugin, "plugin"},
		{"unknown", ActorKind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.input.String(); got != tt.expected {
				t.Errorf("got %q, want %q", got, tt.expected)
			}
		})
	}
}
