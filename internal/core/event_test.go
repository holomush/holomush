// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package core

import (
	"testing"

	"github.com/stretchr/testify/assert"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
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

func TestHostEventTypesMatchPluginSDKReExports(t *testing.T) {
	// The host's authoritative event-type strings are in this file.
	// pkg/plugin re-exports them as pluginsdk.HostEventType* so plugin
	// code (which cannot import internal/core) has typed references.
	// Verify the two sides agree string-for-string.
	cases := []struct {
		name string
		core EventType
		sdk  pluginsdk.EventType
	}{
		{"core and sdk agree on system event type string", EventTypeSystem, pluginsdk.HostEventTypeSystem},
		{"core and sdk agree on session_ended event type string", EventTypeSessionEnded, pluginsdk.HostEventTypeSessionEnded},
		{"core and sdk agree on command_response event type string", EventTypeCommandResponse, pluginsdk.HostEventTypeCommandResponse},
		{"core and sdk agree on command_error event type string", EventTypeCommandError, pluginsdk.HostEventTypeCommandError},
		{"core and sdk agree on arrive event type string", EventTypeArrive, pluginsdk.HostEventTypeArrive},
		{"core and sdk agree on leave event type string", EventTypeLeave, pluginsdk.HostEventTypeLeave},
		{"core and sdk agree on move event type string", EventTypeMove, pluginsdk.HostEventTypeMove},
		{"core and sdk agree on location_state event type string", EventTypeLocationState, pluginsdk.HostEventTypeLocationState},
		{"core and sdk agree on exit_update event type string", EventTypeExitUpdate, pluginsdk.HostEventTypeExitUpdate},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, string(c.core), string(c.sdk),
				"host event-type drift between internal/core and pkg/plugin")
		})
	}
}
