// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/gatewaymetrics"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// newTestHandler creates a Handler. The gateway no longer holds a
// VerbRegistry — rendering metadata travels on the wire via
// EventFrame.Rendering. Tests build EventFrames with the appropriate
// Rendering sub-message via testRendering.
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	return &Handler{}
}

// testRenderings maps the event types these tests exercise to the
// rendering metadata that the core process's RenderingPublisher would
// otherwise stamp on outbound events at emit time. Production rendering
// is sourced from plugin manifests + host builtins; tests short-circuit.
var testRenderings = map[string]*corev1.RenderingMetadata{
	"core-communication:say":            {Category: "communication", Format: "speech", Label: "says", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:pose":           {Category: "communication", Format: "action", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:page":           {Category: "communication", Format: "speech", Label: "pages", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:whisper":        {Category: "communication", Format: "speech", Label: "whispers", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:whisper_notice": {Category: "communication", Format: "action", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:ooc":            {Category: "communication", Format: "action", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:emit":           {Category: "communication", Format: "action", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-communication:pemit":          {Category: "command", Format: "narrative", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-communication"},
	"core-objects:object_create":        {Category: "state", Format: "delta", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE, SourcePlugin: "core-objects"},
	"core-objects:object_destroy":       {Category: "state", Format: "delta", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE, SourcePlugin: "core-objects"},
	"core-objects:object_use":           {Category: "command", Format: "narrative", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-objects"},
	"core-objects:object_examine":       {Category: "command", Format: "narrative", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-objects"},
	"core-objects:object_give":          {Category: "command", Format: "narrative", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "core-objects"},

	// Host-owned builtins (registered by BootstrapVerbRegistry in production).
	"arrive":           {Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, SourcePlugin: "builtin"},
	"leave":            {Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, SourcePlugin: "builtin"},
	"move":             {Category: "movement", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_BOTH, SourcePlugin: "builtin"},
	"system":           {Category: "system", Format: "notification", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "builtin"},
	"command_response": {Category: "command", Format: "narrative", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "builtin"},
	"command_error":    {Category: "command", Format: "error", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL, SourcePlugin: "builtin"},
	"location_state":   {Category: "state", Format: "snapshot", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE, SourcePlugin: "builtin"},
	"exit_update":      {Category: "state", Format: "delta", DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_STATE, SourcePlugin: "builtin"},
}

// withRendering returns a copy of ev with Rendering populated from
// testRenderings (if present for ev.Type). Tests use this helper to
// simulate the core process's RenderingPublisher.
func withRendering(ev *corev1.EventFrame) *corev1.EventFrame {
	if ev.Rendering != nil {
		return ev
	}
	if r, ok := testRenderings[ev.GetType()]; ok {
		ev.Rendering = r
	}
	return ev
}

func TestTranslateEvent_Say(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:      "core-communication:say",
		Timestamp: timestamppb.New(timestamppb.Now().AsTime()),
		ActorId:   "01HYXCHARALICE0000000000AA",
		Payload:   mustMarshal(t, map[string]string{"character_name": "Alice", "message": "Hello!"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "core-communication:say", got.GetType())
	assert.Equal(t, "communication", got.GetCategory())
	assert.Equal(t, "speech", got.GetFormat())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	assert.Equal(t, "Alice", got.GetActor())
	// holomush-5b2j.13: actor_id (ULID) is now forwarded from corev1.EventFrame
	// so the client can key by stable identity (e.g., presence list, self-message
	// detection) instead of by display name.
	assert.Equal(t, "01HYXCHARALICE0000000000AA", got.GetActorId())
	assert.Equal(t, "Hello!", got.GetText())
	require.NotNil(t, got.GetMetadata())
	assert.Equal(t, "says", got.GetMetadata().AsMap()["label"])
}

func TestTranslateEvent_Pose(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "core-communication:pose",
		Payload: mustMarshal(t, map[string]any{"character_name": "Bob", "action": "waves hello."}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "core-communication:pose", got.GetType())
	assert.Equal(t, "communication", got.GetCategory())
	assert.Equal(t, "action", got.GetFormat())
	assert.Equal(t, "Bob", got.GetActor())
	assert.Equal(t, "waves hello.", got.GetText())
}

func TestTranslateEvent_PoseNoSpace(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "core-communication:pose",
		Payload: mustMarshal(t, map[string]any{"character_name": "Bob", "action": "'s face turns red.", "no_space": true}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "communication", got.GetCategory())
	assert.Equal(t, "action", got.GetFormat())
	require.NotNil(t, got.GetMetadata())
	assert.Equal(t, true, got.GetMetadata().AsMap()["no_space"])
}

func TestTranslateEvent_Arrive(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "arrive",
		Payload: mustMarshal(t, map[string]string{"character_name": "Carol"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "arrive", got.GetType())
	assert.Equal(t, "movement", got.GetCategory())
	assert.Equal(t, "notification", got.GetFormat())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetDisplayTarget())
	assert.Equal(t, "Carol", got.GetActor())
}

func TestTranslateEvent_Leave(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "leave",
		Payload: mustMarshal(t, map[string]string{"character_name": "Dave"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "leave", got.GetType())
	assert.Equal(t, "movement", got.GetCategory())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetDisplayTarget())
	assert.Equal(t, "Dave", got.GetActor())
}

func TestTranslateEvent_System(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "system",
		Payload: mustMarshal(t, map[string]string{"message": "Server restarting."}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "system", got.GetType())
	assert.Equal(t, "system", got.GetCategory())
	assert.Equal(t, "notification", got.GetFormat())
	assert.Equal(t, "Server restarting.", got.GetText())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
}

func TestTranslateEvent_Move(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "move",
		Payload: mustMarshal(t, map[string]string{"character_name": "Eve", "message": "Eve goes north."}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "move", got.GetType())
	assert.Equal(t, "movement", got.GetCategory())
	assert.Equal(t, "Eve goes north.", got.GetText())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetDisplayTarget())
}

func TestTranslateEvent_CommandResponse(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "command_response",
		Payload: mustMarshal(t, map[string]string{"text": "Goodbye!"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "command_response", got.GetType())
	assert.Equal(t, "command", got.GetCategory())
	assert.Equal(t, "narrative", got.GetFormat())
	assert.Equal(t, "Goodbye!", got.GetText())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
}

func TestTranslateEvent_CommandError(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "command_error",
		Payload: mustMarshal(t, map[string]string{"text": "Unknown command: foo"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "command_error", got.GetType())
	assert.Equal(t, "command", got.GetCategory())
	assert.Equal(t, "error", got.GetFormat())
	assert.Equal(t, "Unknown command: foo", got.GetText())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
}

func TestTranslateEvent_LocationState(t *testing.T) {
	h := newTestHandler(t)
	payload := core.LocationStatePayload{
		Location: core.LocationStateInfo{
			ID:          "loc-123",
			Name:        "Town Square",
			Description: "A bustling town square.",
		},
		Exits: []core.LocationStateExit{
			{Direction: "north", Name: "Market", Locked: false},
			{Direction: "east", Name: "Library", Locked: true},
		},
		Present: []core.LocationStateChar{
			// CharacterID is opaque to translate.go (no parsing), so test
			// fixtures use clearly-fake strings consistent with the surrounding
			// "loc-123" convention rather than fake-ULID strings.
			{CharacterID: "char-alice", Name: "Alice", Idle: false},
			{CharacterID: "char-bob", Name: "Bob", Idle: true},
		},
	}

	ev := &corev1.EventFrame{
		Type:    "location_state",
		Payload: mustMarshal(t, payload),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "location_state", got.GetType())
	assert.Equal(t, "state", got.GetCategory())
	assert.Equal(t, "snapshot", got.GetFormat())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_STATE, got.GetDisplayTarget())
	require.NotNil(t, got.GetMetadata())

	meta := got.GetMetadata().AsMap()
	loc, ok := meta["location"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "Town Square", loc["name"])
	assert.Equal(t, "loc-123", loc["id"])

	exits, ok := meta["exits"].([]interface{})
	require.True(t, ok)
	assert.Len(t, exits, 2)

	present, ok := meta["present"].([]interface{})
	require.True(t, ok)
	assert.Len(t, present, 2)
}

func TestTranslateEvent_ExitUpdate(t *testing.T) {
	h := newTestHandler(t)
	payload := core.ExitUpdatePayload{
		Exits: []core.LocationStateExit{
			{Direction: "south", Name: "Garden", Locked: false},
		},
	}

	ev := &corev1.EventFrame{
		Type:    "exit_update",
		Payload: mustMarshal(t, payload),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "exit_update", got.GetType())
	assert.Equal(t, "state", got.GetCategory())
	assert.Equal(t, "delta", got.GetFormat())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_STATE, got.GetDisplayTarget())
	require.NotNil(t, got.GetMetadata())

	meta := got.GetMetadata().AsMap()
	exits, ok := meta["exits"].([]interface{})
	require.True(t, ok)
	assert.Len(t, exits, 1)
}

func TestTranslateEvent_OOC(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "core-communication:ooc",
		Payload: mustMarshal(t, core.OOCPayload{CharacterName: "Alice", Message: "brb", Style: "say"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "core-communication:ooc", got.GetType())
	assert.Equal(t, "communication", got.GetCategory())
	assert.Equal(t, "action", got.GetFormat())
	assert.Equal(t, "Alice", got.GetActor())
	assert.Equal(t, "brb", got.GetText())
	require.NotNil(t, got.GetMetadata())
	assert.Equal(t, "say", got.GetMetadata().AsMap()["style"])
}

func TestTranslateEvent_OOC_PoseStyle(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "core-communication:ooc",
		Payload: mustMarshal(t, core.OOCPayload{CharacterName: "Bob", Message: "waves.", Style: "pose"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "core-communication:ooc", got.GetType())
	require.NotNil(t, got.GetMetadata())
	assert.Equal(t, "pose", got.GetMetadata().AsMap()["style"])
}

func TestTranslateEvent_Pemit(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type: "core-communication:pemit",
		Payload: mustMarshal(t, core.PemitPayload{
			SenderName: "Alice",
			Message:    "Secret message.",
		}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, "core-communication:pemit", got.GetType())
	assert.Equal(t, "command", got.GetCategory())
	assert.Equal(t, "narrative", got.GetFormat())
	assert.Equal(t, "Secret message.", got.GetText())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
}

func TestTranslateEvent_Page(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type: "core-communication:page",
		Payload: mustMarshal(t, core.PagePayload{
			SenderName: "Alice",
			Message:    "Hey there!",
		}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got, "page events should now be translated (previously dropped)")
	assert.Equal(t, "core-communication:page", got.GetType())
	assert.Equal(t, "communication", got.GetCategory())
	assert.Equal(t, "speech", got.GetFormat())
	assert.Equal(t, "Alice", got.GetActor())
	assert.Equal(t, "Hey there!", got.GetText())
	require.NotNil(t, got.GetMetadata())
	assert.Equal(t, "pages", got.GetMetadata().AsMap()["label"])
}

func TestTranslateEventTranslatesEventWithUnknownTypeButPresentRendering(t *testing.T) {
	// When rendering IS present (a future plugin defines its own types
	// beyond the host catalog), the gateway translates the event using
	// the on-the-wire metadata. The gateway no longer "guesses" a fallback
	// for unknown types — that responsibility belongs to the core process.
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "teleport",
		Payload: mustMarshal(t, map[string]string{"message": "You teleport away."}),
		Rendering: &corev1.RenderingMetadata{
			Category:      "system",
			Format:        "narrative",
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
			SourcePlugin:  "future-plugin",
		},
	}

	got := h.translateEvent(ev)
	require.NotNil(t, got, "events with rendering must translate even if type is unknown to the host")
	assert.Equal(t, "teleport", got.GetType())
	assert.Equal(t, "system", got.GetCategory())
	assert.Equal(t, "narrative", got.GetFormat())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetDisplayTarget())
	assert.Equal(t, "You teleport away.", got.GetText())
}

func TestTranslateEvent_ScenePoseUsesCharacterNameAsActor(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "core-scenes:scene_pose",
		ActorId: "01HYXCHARALICE0000000000AA",
		Payload: mustMarshal(t, map[string]string{"character_name": "Alice", "text": "smiles"}),
		Rendering: &corev1.RenderingMetadata{
			Category:      "communication",
			Format:        "action",
			DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
			SourcePlugin:  "core-scenes",
		},
	}
	got := h.translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "Alice", got.GetActor())
}

func TestTranslateEvent_CorruptPayload(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "core-communication:say",
		Payload: []byte(`not-valid-json`),
	}

	got := h.translateEvent(withRendering(ev))
	assert.Nil(t, got)
}

func TestTranslateEventDropsEventWithNilRenderingAndIncrementsMetric(t *testing.T) {
	// INV-EVENTBUS-6: events arriving without RenderingMetadata are dropped at
	// the gateway and counted via gatewaymetrics.DroppedNilRenderingTotal.
	// A non-zero counter indicates an upstream invariant violation in the
	// core process's RenderingPublisher.
	h := newTestHandler(t)
	before := testutil.ToFloat64(gatewaymetrics.DroppedNilRenderingTotal.WithLabelValues(gatewaymetrics.SurfaceWeb, "core-communication:say"))

	ev := &corev1.EventFrame{
		Type:    "core-communication:say",
		Payload: mustMarshal(t, map[string]string{"character_name": "Alice", "message": "Hello!"}),
		// Rendering deliberately omitted.
	}

	got := h.translateEvent(ev)
	assert.Nil(t, got, "events without rendering must be dropped (return nil)")

	after := testutil.ToFloat64(gatewaymetrics.DroppedNilRenderingTotal.WithLabelValues(gatewaymetrics.SurfaceWeb, "core-communication:say"))
	assert.Equal(t, before+1, after, "drop counter must increment exactly once")
}

func TestTranslateEvent_StateCorruptPayload(t *testing.T) {
	h := newTestHandler(t)
	ev := &corev1.EventFrame{
		Type:    "location_state",
		Payload: []byte(`not-valid-json`),
	}

	got := h.translateEvent(withRendering(ev))
	assert.Nil(t, got)
}

func TestTranslateEvent_PopulatesEventIdForCommunicationEvents(t *testing.T) {
	h := newTestHandler(t)
	expectedID := core.NewULID().String()
	ev := &corev1.EventFrame{
		Id:        expectedID,
		Type:      "core-communication:say",
		Timestamp: timestamppb.New(timestamppb.Now().AsTime()),
		Payload:   mustMarshal(t, map[string]string{"character_name": "Alice", "message": "Hello!"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, expectedID, got.GetEventId())
}

func TestTranslateEvent_PopulatesEventIdForStateEvents(t *testing.T) {
	h := newTestHandler(t)
	expectedID := core.NewULID().String()
	ev := &corev1.EventFrame{
		Id:        expectedID,
		Type:      "location_state",
		Timestamp: timestamppb.New(timestamppb.Now().AsTime()),
		Payload:   mustMarshal(t, map[string]any{"name": "Cafe", "description": "a place"}),
	}

	got := h.translateEvent(withRendering(ev))
	require.NotNil(t, got)
	assert.Equal(t, expectedID, got.GetEventId())
}

// TestTranslateEvent_SceneICEventStampsSceneIdFromSubject asserts that a live
// scene IC frame (e.g. core-scenes:scene_pose delivered on the
// events.<game>.scene.<id>.ic subject) has metadata["scene_id"] set from the
// subject token immediately after "scene". This is the essential field the
// web scenes workspace routes on; no top-level fallback exists in the client.
func TestTranslateEvent_SceneICEventStampsSceneIdFromSubject(t *testing.T) {
	tests := []struct {
		name           string
		eventType      string
		stream         string
		wantSceneID    string
		wantSceneIDKey bool
	}{
		{
			name:           "pose with fully-qualified dot subject stamps scene_id",
			eventType:      "core-scenes:scene_pose",
			stream:         "events.main.scene.01KTQKNB5EQR3048BBNVJTMVG5.ic",
			wantSceneID:    "01KTQKNB5EQR3048BBNVJTMVG5",
			wantSceneIDKey: true,
		},
		{
			name:           "say with different game id stamps scene_id",
			eventType:      "core-scenes:scene_say",
			stream:         "events.game42.scene.01HSCENEID00000000000000AB.ic",
			wantSceneID:    "01HSCENEID00000000000000AB",
			wantSceneIDKey: true,
		},
		{
			name:           "ooc stamps scene_id",
			eventType:      "core-scenes:scene_ooc",
			stream:         "events.main.scene.01HSCENEID00000000000000CC.ic",
			wantSceneID:    "01HSCENEID00000000000000CC",
			wantSceneIDKey: true,
		},
		{
			name:           "emit stamps scene_id",
			eventType:      "core-scenes:scene_emit",
			stream:         "events.main.scene.01HSCENEID00000000000000DD.ic",
			wantSceneID:    "01HSCENEID00000000000000DD",
			wantSceneIDKey: true,
		},
		{
			name:           "non-scene say event does not get scene_id",
			eventType:      "core-communication:say",
			stream:         "events.main.character.01HCHARID000000000000AAAA",
			wantSceneIDKey: false,
		},
		{
			name:           "arrive movement event does not get scene_id",
			eventType:      "arrive",
			stream:         "events.main.location.01HLOCID0000000000000BBBB",
			wantSceneIDKey: false,
		},
		{
			name:           "malformed subject with scene as last token does not panic and no scene_id",
			eventType:      "core-scenes:scene_pose",
			stream:         "events.main.scene",
			wantSceneIDKey: false,
		},
		{
			name:           "empty subject does not panic and no scene_id",
			eventType:      "core-scenes:scene_pose",
			stream:         "",
			wantSceneIDKey: false,
		},
		{
			name:           "subject without scene segment does not get scene_id",
			eventType:      "core-scenes:scene_pose",
			stream:         "events.main.location.01HLOCID0000000000000XXXX.ic",
			wantSceneIDKey: false,
		},
		{
			// game_id literally "scene": the domain "scene" is matched only at
			// its canonical position (index 2), so parts[3] is the real id.
			name:           "game_id equal to scene resolves the real scene id from index 3",
			eventType:      "core-scenes:scene_pose",
			stream:         "events.scene.scene.01HSCENEID00000000000000EE.ic",
			wantSceneID:    "01HSCENEID00000000000000EE",
			wantSceneIDKey: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandler(t)

			// Build a rendering for the event type (use communication category for
			// scene events; we just need a valid non-nil Rendering).
			rendering := &corev1.RenderingMetadata{
				Category:      "communication",
				Format:        "action",
				DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
				SourcePlugin:  "core-scenes",
			}
			switch tt.eventType {
			case "arrive":
				rendering = testRenderings["arrive"]
			case "core-communication:say":
				rendering = testRenderings["core-communication:say"]
			}

			ev := &corev1.EventFrame{
				Type:      tt.eventType,
				Stream:    tt.stream,
				Payload:   mustMarshal(t, map[string]any{"character_name": "Alice", "action": "waves.", "message": "Hello!"}),
				Rendering: rendering,
			}

			// Must not panic.
			got := h.translateEvent(ev)
			require.NotNil(t, got)

			if tt.wantSceneIDKey {
				require.NotNil(t, got.GetMetadata(), "metadata must be set when scene_id expected")
				meta := got.GetMetadata().AsMap()
				assert.Equal(t, tt.wantSceneID, meta["scene_id"],
					"metadata[scene_id] must equal the id token from the subject")
			} else {
				// Assert unconditionally: a nil metadata trivially has no
				// scene_id, and a non-nil metadata must not carry the key.
				var hasSceneID bool
				if got.GetMetadata() != nil {
					_, hasSceneID = got.GetMetadata().AsMap()["scene_id"]
				}
				assert.False(t, hasSceneID, "non-scene events must not have scene_id in metadata")
			}
		})
	}
}

// TestEventChannelEnumsInLockstep is INV-EVENTBUS-16. corev1.EventChannel and
// webv1.EventChannel MUST stay in lockstep — same enum values, same names,
// same numeric assignments.
func TestEventChannelEnumsInLockstep(t *testing.T) {
	cases := []struct {
		name string
		core corev1.EventChannel
		web  webv1.EventChannel
	}{
		{"UNSPECIFIED", corev1.EventChannel_EVENT_CHANNEL_UNSPECIFIED, webv1.EventChannel_EVENT_CHANNEL_UNSPECIFIED},
		{"TERMINAL", corev1.EventChannel_EVENT_CHANNEL_TERMINAL, webv1.EventChannel_EVENT_CHANNEL_TERMINAL},
		{"STATE", corev1.EventChannel_EVENT_CHANNEL_STATE, webv1.EventChannel_EVENT_CHANNEL_STATE},
		{"BOTH", corev1.EventChannel_EVENT_CHANNEL_BOTH, webv1.EventChannel_EVENT_CHANNEL_BOTH},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, int32(c.core), int32(c.web), "numeric mismatch")
			coreName := corev1.EventChannel_name[int32(c.core)]
			webName := webv1.EventChannel_name[int32(c.web)]
			assert.Equal(t, coreName, webName)
		})
	}
	assert.Equal(t, len(corev1.EventChannel_name), len(webv1.EventChannel_name))
}
