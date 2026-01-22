// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginv1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
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

	assert.NotEmpty(t, event.Id, "expected event ID to be set")
	assert.NotEmpty(t, event.Stream, "expected event stream to be set")
	assert.NotEmpty(t, event.Type, "expected event type to be set")
	assert.NotZero(t, event.Timestamp, "expected event timestamp to be set")
	assert.NotEmpty(t, event.ActorKind, "expected actor_kind to be string type")
	assert.NotEmpty(t, event.ActorId, "expected actor_id to be set")
	assert.NotEmpty(t, event.Payload, "expected payload to be set")
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
	require.NotNil(t, req.Event, "expected HandleEventRequest to have Event field")

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
	assert.Len(t, resp.EmitEvents, 1, "expected 1 emit event")
}

// TestEmitEventFields verifies the EmitEvent message.
func TestEmitEventFields(t *testing.T) {
	emit := &pluginv1.EmitEvent{
		Stream:  "room:room_123",
		Type:    "system",
		Payload: `{"message":"Plugin loaded"}`,
	}

	assert.NotEmpty(t, emit.Stream, "expected stream to be set")
	assert.NotEmpty(t, emit.Type, "expected type to be set")
	assert.NotEmpty(t, emit.Payload, "expected payload to be set")
}
