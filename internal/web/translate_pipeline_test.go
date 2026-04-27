// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

func TestFormatMovementText(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		actor     string
		payload   *genericPayload
		want      string
	}{
		{"arrive", "arrive", "Ruby", &genericPayload{}, "Ruby has arrived."},
		{"leave", "leave", "Pearl", &genericPayload{}, "Pearl has left."},
		{"leave with reason", "leave", "Opal", &genericPayload{Reason: "disconnected"}, "Opal has left (disconnected)."},
		{"empty actor", "arrive", "", &genericPayload{}, ""},
		{"unknown type", "move", "Ruby", &genericPayload{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatMovementText(tt.eventType, tt.actor, tt.payload)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestTranslatePipeline_CategoryRendering verifies that the event translation
// pipeline produces GameEvent fields that drive category-based rendering on
// the web client. Each subtest represents one renderer category.
func TestTranslatePipeline_CategoryRendering(t *testing.T) {
	h := newTestHandler(t)

	t.Run("CommunicationRenderer/say", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "core-communication:say",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]string{"character_name": "Alice", "message": "Hello world"}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got, "say event must not be dropped")
		assert.Equal(t, "core-communication:say", got.GetType())
		assert.Equal(t, "communication", got.GetCategory())
		assert.Equal(t, "speech", got.GetFormat())
		assert.Equal(t, "Alice", got.GetActor())
		assert.Equal(t, "Hello world", got.GetText())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	})

	t.Run("CommunicationRenderer/pose", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "core-communication:pose",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]any{"character_name": "Bob", "action": "waves cheerfully."}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got, "pose event must not be dropped")
		assert.Equal(t, "core-communication:pose", got.GetType())
		assert.Equal(t, "communication", got.GetCategory())
		assert.Equal(t, "action", got.GetFormat())
		assert.Equal(t, "Bob", got.GetActor())
		assert.Equal(t, "waves cheerfully.", got.GetText())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	})

	t.Run("CommunicationRenderer/pose_no_space", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "core-communication:pose",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]any{"character_name": "Bob", "action": "'s eyes widen.", "no_space": true}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "core-communication:pose", got.GetType())
		require.NotNil(t, got.GetMetadata(), "semipose must carry no_space metadata")
		assert.Equal(t, true, got.GetMetadata().AsMap()["no_space"])
	})

	t.Run("CommunicationRenderer/ooc_say", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "core-communication:ooc",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]any{"character_name": "Carol", "message": "heading out soon", "style": "say"}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "core-communication:ooc", got.GetType())
		assert.Equal(t, "communication", got.GetCategory())
		assert.Equal(t, "Carol", got.GetActor())
		assert.Equal(t, "heading out soon", got.GetText())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
		// "say" style should still be in metadata since style is non-empty.
		require.NotNil(t, got.GetMetadata())
		assert.Equal(t, "say", got.GetMetadata().AsMap()["style"])
	})

	t.Run("CommunicationRenderer/ooc_pose", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "core-communication:ooc",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]any{"character_name": "Dave", "message": "waves.", "style": "pose"}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "core-communication:ooc", got.GetType())
		require.NotNil(t, got.GetMetadata(), "pose-style OOC must carry style metadata")
		assert.Equal(t, "pose", got.GetMetadata().AsMap()["style"])
	})

	t.Run("CommunicationRenderer/pemit", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "core-communication:pemit",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]any{"sender_name": "Eve", "message": "A whispered secret reaches you."}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "core-communication:pemit", got.GetType())
		assert.Equal(t, "A whispered secret reaches you.", got.GetText())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	})

	t.Run("CommandRenderer/command_response_success", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "command_response",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]string{"text": "You see a bustling marketplace."}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got, "command_response must not be dropped")
		assert.Equal(t, "command_response", got.GetType())
		assert.Equal(t, "command", got.GetCategory())
		assert.Equal(t, "narrative", got.GetFormat())
		assert.Equal(t, "You see a bustling marketplace.", got.GetText())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	})

	t.Run("CommandRenderer/command_error", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "command_error",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]string{"text": "Unknown command: xyzzy"}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got, "command_error must not be dropped")
		assert.Equal(t, "command_error", got.GetType())
		assert.Equal(t, "command", got.GetCategory())
		assert.Equal(t, "error", got.GetFormat())
		assert.Equal(t, "Unknown command: xyzzy", got.GetText())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	})

	t.Run("SystemRenderer/system", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "system",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]string{"message": "Server will restart in 5 minutes."}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got, "system event must not be dropped")
		assert.Equal(t, "system", got.GetType())
		assert.Equal(t, "system", got.GetCategory())
		assert.Equal(t, "notification", got.GetFormat())
		assert.Equal(t, "Server will restart in 5 minutes.", got.GetText())
		assert.Empty(t, got.GetActor(), "system events have no actor")
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	})

	t.Run("MovementRenderer/arrive", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "arrive",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]string{"character_name": "Frank"}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "arrive", got.GetType())
		assert.Equal(t, "movement", got.GetCategory())
		assert.Equal(t, "notification", got.GetFormat())
		assert.Equal(t, "Frank", got.GetActor())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetDisplayTarget())
	})

	t.Run("MovementRenderer/leave", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "leave",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]string{"character_name": "Grace"}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "leave", got.GetType())
		assert.Equal(t, "movement", got.GetCategory())
		assert.Equal(t, "Grace", got.GetActor())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetDisplayTarget())
	})

	t.Run("MovementRenderer/move", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "move",
			Timestamp: timestamppb.Now(),
			Payload:   mustMarshal(t, map[string]any{"character_name": "Heidi", "message": "Heidi goes north."}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "move", got.GetType())
		assert.Equal(t, "movement", got.GetCategory())
		assert.Equal(t, "Heidi", got.GetActor())
		assert.Equal(t, "Heidi goes north.", got.GetText())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetDisplayTarget())
	})

	t.Run("StateRenderer/location_state", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "location_state",
			Timestamp: timestamppb.Now(),
			Payload: mustMarshal(t, map[string]any{
				"location": map[string]any{
					"id":          "loc-456",
					"name":        "Dark Forest",
					"description": "Twisted trees loom overhead.",
				},
				"exits": []map[string]any{
					{"direction": "south", "name": "Clearing"},
				},
				"present": []map[string]any{
					{"name": "Alice"},
				},
			}),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got, "location_state must not be dropped")
		assert.Equal(t, "location_state", got.GetType())
		assert.Equal(t, "state", got.GetCategory())
		assert.Equal(t, "snapshot", got.GetFormat())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_STATE, got.GetDisplayTarget())
		require.NotNil(t, got.GetMetadata())
		meta := got.GetMetadata().AsMap()
		loc, ok := meta["location"].(map[string]interface{})
		require.True(t, ok, "metadata must contain location object")
		assert.Equal(t, "Dark Forest", loc["name"])
	})

	t.Run("FallbackRenderer/unknown_type", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "custom_plugin_event",
			Timestamp: timestamppb.Now(),
			Payload:   []byte(`{"message": "plugin data"}`),
		}

		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got, "unknown types should get system/narrative fallback, not be dropped")
		assert.Equal(t, "custom_plugin_event", got.GetType())
		assert.Equal(t, "system", got.GetCategory())
		assert.Equal(t, "narrative", got.GetFormat())
		assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	})
}

// TestTranslatePipeline_CorruptPayloadGraceful verifies that corrupt payloads
// are handled gracefully (return nil, no panic) for every event type.
func TestTranslatePipeline_CorruptPayloadGraceful(t *testing.T) {
	h := newTestHandler(t)

	// Non-state types produce nil on corrupt payload (generic unmarshal fails).
	nonStateTypes := []string{
		"say", "pose", "arrive", "leave", "system", "move",
		"command_response", "ooc", "pemit",
	}
	for _, typ := range nonStateTypes {
		t.Run(typ, func(t *testing.T) {
			ev := &corev1.EventFrame{
				Type:      typ,
				Timestamp: timestamppb.Now(),
				Payload:   []byte(`<<<not json>>>`),
			}
			got := h.translateEvent(withRendering(ev))
			assert.Nil(t, got, "corrupt %s payload should be dropped gracefully", typ)
		})
	}

	// Movement types produce arrive/leave text from character_name.
	t.Run("arrive event synthesizes text", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "arrive",
			Timestamp: timestamppb.Now(),
			Payload:   []byte(`{"character_name":"Ruby_Helium"}`),
		}
		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "Ruby_Helium has arrived.", got.Text)
		assert.Equal(t, "Ruby_Helium", got.Actor)
		assert.Equal(t, "movement", got.Category)
	})

	t.Run("leave event synthesizes text", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "leave",
			Timestamp: timestamppb.Now(),
			Payload:   []byte(`{"character_name":"Pearl_Copper"}`),
		}
		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "Pearl_Copper has left.", got.Text)
	})

	t.Run("leave event with reason", func(t *testing.T) {
		ev := &corev1.EventFrame{
			Type:      "leave",
			Timestamp: timestamppb.Now(),
			Payload:   []byte(`{"character_name":"Opal_Neon","reason":"disconnected"}`),
		}
		got := h.translateEvent(withRendering(ev))
		require.NotNil(t, got)
		assert.Equal(t, "Opal_Neon has left (disconnected).", got.Text)
	})

	// State types also produce nil on corrupt payload.
	stateTypes := []string{"location_state", "exit_update"}
	for _, typ := range stateTypes {
		t.Run(typ, func(t *testing.T) {
			ev := &corev1.EventFrame{
				Type:      typ,
				Timestamp: timestamppb.Now(),
				Payload:   []byte(`<<<not json>>>`),
			}
			got := h.translateEvent(withRendering(ev))
			assert.Nil(t, got, "corrupt %s payload should be dropped gracefully", typ)
		})
	}
}
