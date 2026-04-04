// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestActorKind_String(t *testing.T) {
	tests := []struct {
		name     string
		kind     ActorKind
		expected string
	}{
		{"Character", ActorCharacter, "character"},
		{"System", ActorSystem, "system"},
		{"Plugin", ActorPlugin, "plugin"},
		{"UnknownValue", ActorKind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.kind.String())
		})
	}
}
