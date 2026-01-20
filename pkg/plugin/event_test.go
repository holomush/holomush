// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugin

import "testing"

func TestActorKind_String(t *testing.T) {
	tests := []struct {
		kind     ActorKind
		expected string
	}{
		{ActorCharacter, "character"},
		{ActorSystem, "system"},
		{ActorPlugin, "plugin"},
		{ActorKind(99), "unknown"}, // Unknown kind
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.kind.String(); got != tt.expected {
				t.Errorf("ActorKind(%d).String() = %q, want %q", tt.kind, got, tt.expected)
			}
		})
	}
}
