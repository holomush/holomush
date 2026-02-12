// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
			assert.Equal(t, tt.expected, tt.kind.String())
		})
	}
}
