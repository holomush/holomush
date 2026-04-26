// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEventType_String(t *testing.T) {
	tests := []struct {
		name     string
		input    EventType
		expected string
	}{
		// Host-owned event types (stay in internal/core)
		{"arrive event", EventTypeArrive, "arrive"},
		{"leave event", EventTypeLeave, "leave"},
		{"system event", EventTypeSystem, "system"},
		{"move event", EventTypeMove, "move"},
		{"object_create event", EventTypeObjectCreate, "object_create"},
		{"object_destroy event", EventTypeObjectDestroy, "object_destroy"},
		{"object_use event", EventTypeObjectUse, "object_use"},
		{"object_examine event", EventTypeObjectExamine, "object_examine"},
		{"object_give event", EventTypeObjectGive, "object_give"},
		{"location_state event", EventTypeLocationState, "location_state"},
		{"exit_update event", EventTypeExitUpdate, "exit_update"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, string(tt.input))
		})
	}
}

func TestEventTypeLocationStateConstantsMatchExpectedValues(t *testing.T) {
	assert.Equal(t, EventType("location_state"), EventTypeLocationState)
	assert.Equal(t, EventType("exit_update"), EventTypeExitUpdate)
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
			assert.Equal(t, tt.expected, tt.input.String())
		})
	}
}
