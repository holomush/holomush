// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"testing"

	"github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEmitter(t *testing.T) {
	emitter := NewEmitter()
	require.NotNil(t, emitter)

	// New emitter should have no events
	events := emitter.Flush()
	assert.Empty(t, events)
}

func TestEmitter_Location(t *testing.T) {
	tests := []struct {
		name       string
		locationID string
		eventType  plugin.EventType
		payload    Payload
		wantStream string
		wantType   plugin.EventType
	}{
		{
			name:       "say event to location",
			locationID: "01ABC123",
			eventType:  plugin.EventTypeSay,
			payload:    Payload{"message": "Hello!", "speaker": "Alice"},
			wantStream: "location:01ABC123",
			wantType:   plugin.EventTypeSay,
		},
		{
			name:       "pose event to location",
			locationID: "01DEF456",
			eventType:  plugin.EventTypePose,
			payload:    Payload{"message": "waves", "actor": "Bob"},
			wantStream: "location:01DEF456",
			wantType:   plugin.EventTypePose,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := NewEmitter()
			emitter.Location(tt.locationID, tt.eventType, tt.payload)

			events := emitter.Flush()
			require.Len(t, events, 1)
			assert.Equal(t, tt.wantStream, events[0].Stream)
			assert.Equal(t, tt.wantType, events[0].Type)
			assert.NotEmpty(t, events[0].Payload)
		})
	}
}

func TestEmitter_Character(t *testing.T) {
	tests := []struct {
		name        string
		characterID string
		eventType   plugin.EventType
		payload     Payload
		wantStream  string
	}{
		{
			name:        "tell event to character",
			characterID: "01CHAR123",
			eventType:   plugin.EventType("tell"),
			payload:     Payload{"message": "Psst!", "sender": "Alice"},
			wantStream:  "char:01CHAR123",
		},
		{
			name:        "system event to character",
			characterID: "01CHAR456",
			eventType:   plugin.EventTypeSystem,
			payload:     Payload{"message": "You have mail"},
			wantStream:  "char:01CHAR456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := NewEmitter()
			emitter.Character(tt.characterID, tt.eventType, tt.payload)

			events := emitter.Flush()
			require.Len(t, events, 1)
			assert.Equal(t, tt.wantStream, events[0].Stream)
			assert.Equal(t, tt.eventType, events[0].Type)
		})
	}
}

func TestEmitter_Global(t *testing.T) {
	emitter := NewEmitter()
	emitter.Global(plugin.EventTypeSystem, Payload{"message": "Server restart in 5 minutes"})

	events := emitter.Flush()
	require.Len(t, events, 1)
	assert.Equal(t, "global", events[0].Stream)
	assert.Equal(t, plugin.EventTypeSystem, events[0].Type)
}

func TestEmitter_PayloadJSONEncoding(t *testing.T) {
	tests := []struct {
		name    string
		payload Payload
		want    string
	}{
		{
			name:    "simple string",
			payload: Payload{"message": "hello"},
			want:    `{"message":"hello"}`,
		},
		{
			name:    "string with quotes",
			payload: Payload{"message": `She said "hello"`},
			want:    `{"message":"She said \"hello\""}`,
		},
		{
			name:    "string with newlines",
			payload: Payload{"message": "line1\nline2"},
			want:    `{"message":"line1\nline2"}`,
		},
		{
			name:    "string with unicode",
			payload: Payload{"message": "Hello 世界"},
			want:    `{"message":"Hello 世界"}`,
		},
		{
			name:    "multiple fields",
			payload: Payload{"message": "hi", "speaker": "Alice"},
			// JSON key order is not guaranteed, so we check both ways
		},
		{
			name:    "numeric value",
			payload: Payload{"count": 42},
			want:    `{"count":42}`,
		},
		{
			name:    "boolean value",
			payload: Payload{"visible": true},
			want:    `{"visible":true}`,
		},
		{
			name:    "special characters",
			payload: Payload{"message": "backslash: \\ tab:\t"},
			want:    `{"message":"backslash: \\ tab:\t"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := NewEmitter()
			emitter.Global(plugin.EventTypeSystem, tt.payload)

			events := emitter.Flush()
			require.Len(t, events, 1)

			if tt.name == "multiple fields" {
				// Check that both fields are present
				assert.Contains(t, events[0].Payload, `"message":"hi"`)
				assert.Contains(t, events[0].Payload, `"speaker":"Alice"`)
			} else {
				assert.Equal(t, tt.want, events[0].Payload)
			}
		})
	}
}

func TestEmitter_Flush(t *testing.T) {
	t.Run("returns accumulated events and clears buffer", func(t *testing.T) {
		emitter := NewEmitter()
		emitter.Location("loc1", plugin.EventTypeSay, Payload{"m": "1"})
		emitter.Character("char1", plugin.EventTypeSay, Payload{"m": "2"})
		emitter.Global(plugin.EventTypeSystem, Payload{"m": "3"})

		events := emitter.Flush()
		require.Len(t, events, 3)

		// Second flush should return empty
		events2 := emitter.Flush()
		assert.Empty(t, events2)
	})

	t.Run("empty emitter returns nil", func(t *testing.T) {
		emitter := NewEmitter()
		events := emitter.Flush()
		assert.Nil(t, events)
	})
}

func TestEmitter_MultipleEmits(t *testing.T) {
	emitter := NewEmitter()

	// Emit multiple events
	emitter.Location("loc1", plugin.EventTypeSay, Payload{"n": 1})
	emitter.Location("loc1", plugin.EventTypePose, Payload{"n": 2})
	emitter.Character("char1", plugin.EventTypeSystem, Payload{"n": 3})
	emitter.Global(plugin.EventTypeSystem, Payload{"n": 4})

	events := emitter.Flush()
	require.Len(t, events, 4)

	// Verify order preserved
	assert.Equal(t, "location:loc1", events[0].Stream)
	assert.Equal(t, plugin.EventTypeSay, events[0].Type)

	assert.Equal(t, "location:loc1", events[1].Stream)
	assert.Equal(t, plugin.EventTypePose, events[1].Type)

	assert.Equal(t, "char:char1", events[2].Stream)
	assert.Equal(t, plugin.EventTypeSystem, events[2].Type)

	assert.Equal(t, "global", events[3].Stream)
	assert.Equal(t, plugin.EventTypeSystem, events[3].Type)
}

func TestEmitter_JSONEncodingError(t *testing.T) {
	// Test that JSON encoding errors result in empty payload
	emitter := NewEmitter()

	// Create a payload with a value that cannot be JSON encoded (channel)
	badPayload := Payload{"invalid": make(chan int)}
	emitter.Global(plugin.EventTypeSystem, badPayload)

	events := emitter.Flush()
	require.Len(t, events, 1)
	// On encoding error, payload should be "{}"
	assert.Equal(t, "{}", events[0].Payload)
}
