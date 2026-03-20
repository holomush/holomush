// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/core"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestTranslateEvent_Say(t *testing.T) {
	ev := &corev1.Event{
		Type:      "say",
		Timestamp: timestamppb.New(timestamppb.Now().AsTime()),
		Payload:   mustMarshal(t, sayPayload{CharacterName: "Alice", Message: "Hello!"}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "say", got.GetType())
	assert.Equal(t, "Alice", got.GetCharacterName())
	assert.Equal(t, "Hello!", got.GetText())
}

func TestTranslateEvent_Pose(t *testing.T) {
	ev := &corev1.Event{
		Type:    "pose",
		Payload: mustMarshal(t, posePayload{CharacterName: "Bob", Action: "waves hello."}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "pose", got.GetType())
	assert.Equal(t, "Bob", got.GetCharacterName())
	assert.Equal(t, "waves hello.", got.GetText())
}

func TestTranslateEvent_Arrive(t *testing.T) {
	ev := &corev1.Event{
		Type:    "arrive",
		Payload: mustMarshal(t, arriveLeavePayload{CharacterName: "Carol"}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "arrive", got.GetType())
	assert.Equal(t, "Carol", got.GetCharacterName())
	assert.Equal(t, "has arrived.", got.GetText())
}

func TestTranslateEvent_Leave(t *testing.T) {
	ev := &corev1.Event{
		Type:    "leave",
		Payload: mustMarshal(t, arriveLeavePayload{CharacterName: "Dave"}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "leave", got.GetType())
	assert.Equal(t, "Dave", got.GetCharacterName())
	assert.Equal(t, "has left.", got.GetText())
}

func TestTranslateEvent_SayChannel(t *testing.T) {
	ev := &corev1.Event{
		Type:    "say",
		Payload: mustMarshal(t, sayPayload{CharacterName: "Alice", Message: "Hello!"}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetChannel())
}

func TestTranslateEvent_PoseChannel(t *testing.T) {
	ev := &corev1.Event{
		Type:    "pose",
		Payload: mustMarshal(t, posePayload{CharacterName: "Bob", Action: "waves."}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetChannel())
}

func TestTranslateEvent_ArriveChannel(t *testing.T) {
	ev := &corev1.Event{
		Type:    "arrive",
		Payload: mustMarshal(t, arriveLeavePayload{CharacterName: "Carol"}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetChannel())
}

func TestTranslateEvent_LeaveChannel(t *testing.T) {
	ev := &corev1.Event{
		Type:    "leave",
		Payload: mustMarshal(t, arriveLeavePayload{CharacterName: "Dave"}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetChannel())
}

func TestTranslateEvent_System(t *testing.T) {
	ev := &corev1.Event{
		Type:    "system",
		Payload: mustMarshal(t, map[string]string{"message": "Server restarting."}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "system", got.GetType())
	assert.Equal(t, "Server restarting.", got.GetText())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_TERMINAL, got.GetChannel())
}

func TestTranslateEvent_Move(t *testing.T) {
	ev := &corev1.Event{
		Type:    "move",
		Payload: mustMarshal(t, map[string]string{"character_name": "Eve", "message": "Eve goes north."}),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "move", got.GetType())
	assert.Equal(t, "Eve goes north.", got.GetText())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_BOTH, got.GetChannel())
}

func TestTranslateEvent_LocationState(t *testing.T) {
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
			{Name: "Alice", Idle: false},
			{Name: "Bob", Idle: true},
		},
	}

	ev := &corev1.Event{
		Type:    "location_state",
		Payload: mustMarshal(t, payload),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "location_state", got.GetType())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_STATE, got.GetChannel())
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
	payload := core.ExitUpdatePayload{
		Exits: []core.LocationStateExit{
			{Direction: "south", Name: "Garden", Locked: false},
		},
	}

	ev := &corev1.Event{
		Type:    "exit_update",
		Payload: mustMarshal(t, payload),
	}

	got := translateEvent(ev)
	require.NotNil(t, got)
	assert.Equal(t, "exit_update", got.GetType())
	assert.Equal(t, webv1.EventChannel_EVENT_CHANNEL_STATE, got.GetChannel())
	require.NotNil(t, got.GetMetadata())

	meta := got.GetMetadata().AsMap()
	exits, ok := meta["exits"].([]interface{})
	require.True(t, ok)
	assert.Len(t, exits, 1)
}

func TestTranslateEvent_Unknown(t *testing.T) {
	ev := &corev1.Event{
		Type:    "teleport",
		Payload: []byte(`{}`),
	}

	got := translateEvent(ev)
	assert.Nil(t, got)
}

func TestTranslateEvent_CorruptPayload(t *testing.T) {
	ev := &corev1.Event{
		Type:    "say",
		Payload: []byte(`not-valid-json`),
	}

	got := translateEvent(ev)
	assert.Nil(t, got)
}
