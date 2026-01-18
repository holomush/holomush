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
