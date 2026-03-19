// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
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
