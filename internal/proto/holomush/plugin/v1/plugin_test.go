// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginv1_test

import (
	"testing"

	pluginv1 "github.com/holomush/holomush/internal/proto/holomush/plugin/v1"
)

// TestEventMessageFields verifies the Event message has expected fields.
func TestEventMessageFields(t *testing.T) {
	// Create an Event to verify field types compile
	event := &pluginv1.Event{
		Id:        "01HQGJ7K8P4R5VDXC9YM2N3EF6",
		Stream:    "room:room_abc123",
		Type:      "say",
		Timestamp: 1705678901234,
		ActorKind: "character", // string, not enum per task requirement
		ActorId:   "char_123",
		Payload:   `{"text":"Hello, world!"}`,
	}

	if event.Id == "" {
		t.Error("expected event ID to be set")
	}
	if event.Stream == "" {
		t.Error("expected event stream to be set")
	}
	if event.Type == "" {
		t.Error("expected event type to be set")
	}
	if event.Timestamp == 0 {
		t.Error("expected event timestamp to be set")
	}
	if event.ActorKind == "" {
		t.Error("expected actor_kind to be string type")
	}
	if event.ActorId == "" {
		t.Error("expected actor_id to be set")
	}
	if event.Payload == "" {
		t.Error("expected payload to be set")
	}
}

// TestHandleEventRequestResponse verifies Plugin service RPC types.
func TestHandleEventRequestResponse(t *testing.T) {
	// Verify HandleEventRequest exists and has Event field
	req := &pluginv1.HandleEventRequest{
		Event: &pluginv1.Event{
			Id:     "test_id",
			Stream: "test_stream",
			Type:   "say",
		},
	}
	if req.Event == nil {
		t.Error("expected HandleEventRequest to have Event field")
	}

	// Verify HandleEventResponse exists and has EmitEvents field
	resp := &pluginv1.HandleEventResponse{
		EmitEvents: []*pluginv1.EmitEvent{
			{
				Stream:  "room:room_123",
				Type:    "pose",
				Payload: `{"text":"waves"}`,
			},
		},
	}
	if len(resp.EmitEvents) != 1 {
		t.Errorf("expected 1 emit event, got %d", len(resp.EmitEvents))
	}
}

// TestEmitEventFields verifies the EmitEvent message.
func TestEmitEventFields(t *testing.T) {
	emit := &pluginv1.EmitEvent{
		Stream:  "room:room_123",
		Type:    "system",
		Payload: `{"message":"Plugin loaded"}`,
	}

	if emit.Stream == "" {
		t.Error("expected stream to be set")
	}
	if emit.Type == "" {
		t.Error("expected type to be set")
	}
	if emit.Payload == "" {
		t.Error("expected payload to be set")
	}
}
