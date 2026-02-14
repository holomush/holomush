// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package holo

import (
	"bytes"
	"log/slog"
	"testing"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEmitter(t *testing.T) {
	emitter := NewEmitter()
	require.NotNil(t, emitter)

	// New emitter should have no events
	events, errs := emitter.Flush()
	assert.Empty(t, events)
	assert.Nil(t, errs)
}

func TestEmitter_Location(t *testing.T) {
	tests := []struct {
		name       string
		locationID string
		eventType  pluginsdk.EventType
		payload    Payload
		wantStream string
		wantType   pluginsdk.EventType
	}{
		{
			name:       "say event to location",
			locationID: "01ABC123",
			eventType:  pluginsdk.EventTypeSay,
			payload:    Payload{"message": "Hello!", "speaker": "Alice"},
			wantStream: "location:01ABC123",
			wantType:   pluginsdk.EventTypeSay,
		},
		{
			name:       "pose event to location",
			locationID: "01DEF456",
			eventType:  pluginsdk.EventTypePose,
			payload:    Payload{"message": "waves", "actor": "Bob"},
			wantStream: "location:01DEF456",
			wantType:   pluginsdk.EventTypePose,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := NewEmitter()
			emitter.Location(tt.locationID, tt.eventType, tt.payload)

			events, errs := emitter.Flush()
			require.Len(t, events, 1)
			assert.Nil(t, errs)
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
		eventType   pluginsdk.EventType
		payload     Payload
		wantStream  string
	}{
		{
			name:        "tell event to character",
			characterID: "01CHAR123",
			eventType:   pluginsdk.EventType("tell"),
			payload:     Payload{"message": "Psst!", "sender": "Alice"},
			wantStream:  "character:01CHAR123",
		},
		{
			name:        "system event to character",
			characterID: "01CHAR456",
			eventType:   pluginsdk.EventTypeSystem,
			payload:     Payload{"message": "You have mail"},
			wantStream:  "character:01CHAR456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emitter := NewEmitter()
			emitter.Character(tt.characterID, tt.eventType, tt.payload)

			events, errs := emitter.Flush()
			require.Len(t, events, 1)
			assert.Nil(t, errs)
			assert.Equal(t, tt.wantStream, events[0].Stream)
			assert.Equal(t, tt.eventType, events[0].Type)
		})
	}
}

func TestEmitter_Global(t *testing.T) {
	emitter := NewEmitter()
	emitter.Global(pluginsdk.EventTypeSystem, Payload{"message": "Server restart in 5 minutes"})

	events, errs := emitter.Flush()
	require.Len(t, events, 1)
	assert.Nil(t, errs)
	assert.Equal(t, "global", events[0].Stream)
	assert.Equal(t, pluginsdk.EventTypeSystem, events[0].Type)
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
			emitter.Global(pluginsdk.EventTypeSystem, tt.payload)

			events, errs := emitter.Flush()
			require.Len(t, events, 1)
			assert.Nil(t, errs)

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
		emitter.Location("loc1", pluginsdk.EventTypeSay, Payload{"m": "1"})
		emitter.Character("char1", pluginsdk.EventTypeSay, Payload{"m": "2"})
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"m": "3"})

		events, errs := emitter.Flush()
		require.Len(t, events, 3)
		assert.Nil(t, errs)

		// Second flush should return empty
		events2, errs2 := emitter.Flush()
		assert.Empty(t, events2)
		assert.Nil(t, errs2)
	})

	t.Run("empty emitter returns nil", func(t *testing.T) {
		emitter := NewEmitter()
		events, errs := emitter.Flush()
		assert.Nil(t, events)
		assert.Nil(t, errs)
	})
}

func TestEmitter_MultipleEmits(t *testing.T) {
	emitter := NewEmitter()

	// Emit multiple events
	emitter.Location("loc1", pluginsdk.EventTypeSay, Payload{"n": 1})
	emitter.Location("loc1", pluginsdk.EventTypePose, Payload{"n": 2})
	emitter.Character("char1", pluginsdk.EventTypeSystem, Payload{"n": 3})
	emitter.Global(pluginsdk.EventTypeSystem, Payload{"n": 4})

	events, errs := emitter.Flush()
	require.Len(t, events, 4)
	assert.Nil(t, errs)

	// Verify order preserved
	assert.Equal(t, "location:loc1", events[0].Stream)
	assert.Equal(t, pluginsdk.EventTypeSay, events[0].Type)

	assert.Equal(t, "location:loc1", events[1].Stream)
	assert.Equal(t, pluginsdk.EventTypePose, events[1].Type)

	assert.Equal(t, "character:char1", events[2].Stream)
	assert.Equal(t, pluginsdk.EventTypeSystem, events[2].Type)

	assert.Equal(t, "global", events[3].Stream)
	assert.Equal(t, pluginsdk.EventTypeSystem, events[3].Type)
}

func TestEmitter_JSONEncodingError(t *testing.T) {
	// Test that JSON encoding errors result in empty payload
	emitter := NewEmitter()

	// Create a payload with a value that cannot be JSON encoded (channel)
	badPayload := Payload{"invalid": make(chan int)}
	emitter.Global(pluginsdk.EventTypeSystem, badPayload)

	events, errs := emitter.Flush()
	require.Len(t, events, 1)
	// On encoding error, payload should be "{}"
	assert.Equal(t, "{}", events[0].Payload)
	// Error should be tracked
	require.Len(t, errs, 1)
}

func TestEmitter_JSONEncodingError_LogsError(t *testing.T) {
	// Capture log output
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	logger := slog.New(handler)

	emitter := NewEmitterWithLogger(logger)

	// Create a payload with a value that cannot be JSON encoded (channel)
	badPayload := Payload{"invalid": make(chan int)}
	emitter.Global(pluginsdk.EventTypeSystem, badPayload)

	events, errs := emitter.Flush()
	require.Len(t, events, 1)
	assert.Equal(t, "{}", events[0].Payload)
	require.Len(t, errs, 1)

	// Verify error was logged
	logOutput := buf.String()
	assert.Contains(t, logOutput, "json marshal failed")
	assert.Contains(t, logOutput, "stream=global")
	assert.Contains(t, logOutput, "event_type=system")
}

func TestEmitter_JSONEncodingError_LogsPayloadType(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	logger := slog.New(handler)

	emitter := NewEmitterWithLogger(logger)

	// Use a function which cannot be marshaled - shows type info in log
	badPayload := Payload{"callback": func() {}}
	emitter.Location("room123", pluginsdk.EventTypeSay, badPayload)

	_, errs := emitter.Flush()
	require.Len(t, errs, 1)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "json marshal failed")
	assert.Contains(t, logOutput, "stream=location:room123")
}

func TestEmitter_NilLogger_NoLogging(t *testing.T) {
	// NewEmitter (no logger) should not panic on marshal error
	emitter := NewEmitter()

	badPayload := Payload{"invalid": make(chan int)}
	emitter.Global(pluginsdk.EventTypeSystem, badPayload)

	events, errs := emitter.Flush()
	require.Len(t, events, 1)
	assert.Equal(t, "{}", events[0].Payload)
	// Error should still be tracked even without logger
	require.Len(t, errs, 1)
}

func TestEmitter_Flush_ReturnsAccumulatedErrors(t *testing.T) {
	t.Run("single error", func(t *testing.T) {
		emitter := NewEmitter()

		badPayload := Payload{"invalid": make(chan int)}
		emitter.Global(pluginsdk.EventTypeSystem, badPayload)

		events, errs := emitter.Flush()
		require.Len(t, events, 1)
		require.Len(t, errs, 1)

		// Error should contain context
		assert.Contains(t, errs[0].Error(), "stream=global")
		assert.Contains(t, errs[0].Error(), "type=system")
	})

	t.Run("multiple errors", func(t *testing.T) {
		emitter := NewEmitter()

		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad1": make(chan int)})
		emitter.Location("room1", pluginsdk.EventTypeSay, Payload{"bad2": func() {}})
		emitter.Character("char1", pluginsdk.EventTypePose, Payload{"bad3": make(chan string)})

		events, errs := emitter.Flush()
		require.Len(t, events, 3)
		require.Len(t, errs, 3)

		// Verify errors have different stream contexts
		assert.Contains(t, errs[0].Error(), "stream=global")
		assert.Contains(t, errs[1].Error(), "stream=location:room1")
		assert.Contains(t, errs[2].Error(), "stream=character:char1")
	})

	t.Run("mixed success and errors", func(t *testing.T) {
		emitter := NewEmitter()

		emitter.Global(pluginsdk.EventTypeSystem, Payload{"ok": "value"})
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad": make(chan int)})
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"also_ok": 123})

		events, errs := emitter.Flush()
		require.Len(t, events, 3)
		require.Len(t, errs, 1)

		// Verify the one error
		assert.Contains(t, errs[0].Error(), "stream=global")
	})

	t.Run("no errors returns nil slice", func(t *testing.T) {
		emitter := NewEmitter()

		emitter.Global(pluginsdk.EventTypeSystem, Payload{"ok": "value"})

		events, errs := emitter.Flush()
		require.Len(t, events, 1)
		assert.Nil(t, errs)
	})

	t.Run("flush clears errors", func(t *testing.T) {
		emitter := NewEmitter()

		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad": make(chan int)})

		_, errs1 := emitter.Flush()
		require.Len(t, errs1, 1)

		// Second flush should have no errors
		_, errs2 := emitter.Flush()
		assert.Nil(t, errs2)
	})
}

func TestEmitter_HasErrors(t *testing.T) {
	t.Run("returns false when no errors", func(t *testing.T) {
		emitter := NewEmitter()
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"ok": "value"})
		assert.False(t, emitter.HasErrors())
	})

	t.Run("returns true when errors present", func(t *testing.T) {
		emitter := NewEmitter()
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad": make(chan int)})
		assert.True(t, emitter.HasErrors())
	})

	t.Run("resets after flush", func(t *testing.T) {
		emitter := NewEmitter()
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad": make(chan int)})
		assert.True(t, emitter.HasErrors())

		emitter.Flush()
		assert.False(t, emitter.HasErrors())
	})
}

func TestEmitter_ErrorCount(t *testing.T) {
	t.Run("returns 0 when no errors", func(t *testing.T) {
		emitter := NewEmitter()
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"ok": "value"})
		assert.Equal(t, 0, emitter.ErrorCount())
	})

	t.Run("counts multiple errors", func(t *testing.T) {
		emitter := NewEmitter()
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad1": make(chan int)})
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad2": func() {}})
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad3": make(chan string)})
		assert.Equal(t, 3, emitter.ErrorCount())
	})

	t.Run("resets after flush", func(t *testing.T) {
		emitter := NewEmitter()
		emitter.Global(pluginsdk.EventTypeSystem, Payload{"bad": make(chan int)})
		assert.Equal(t, 1, emitter.ErrorCount())

		emitter.Flush()
		assert.Equal(t, 0, emitter.ErrorCount())
	})
}
