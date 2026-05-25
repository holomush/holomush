// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecodeReplayEntries verifies the IC content kinds (pose/say/emit) are
// decoded from their {actor_id, text} payloads and non-content events are
// skipped.
func TestDecodeReplayEntries(t *testing.T) {
	t.Parallel()
	events := []pluginsdk.Event{
		{ID: "1", Type: pluginsdk.EventType("scene_pose"), Payload: `{"actor_id":"Alice","text":"smiles warmly."}`},
		{ID: "2", Type: pluginsdk.EventType("scene_join_ic"), Payload: `{"actor_id":"Bob"}`}, // non-content → skipped
		{ID: "3", Type: pluginsdk.EventType("scene_say"), Payload: `{"actor_id":"Bob","text":"Hello."}`},
		{ID: "4", Type: pluginsdk.EventType("scene_emit"), Payload: `{"actor_id":"Cara","text":"A bell rings."}`},
	}

	entries, err := decodeReplayEntries(events)

	require.NoError(t, err)
	require.Len(t, entries, 3, "the non-content join event is skipped")
	assert.Equal(t, PublishedSceneEntry{Speaker: "Alice", Kind: EntryKindPose, Content: "smiles warmly."}, entries[0])
	assert.Equal(t, PublishedSceneEntry{Speaker: "Bob", Kind: EntryKindSay, Content: "Hello."}, entries[1])
	assert.Equal(t, PublishedSceneEntry{Speaker: "Cara", Kind: EntryKindEmit, Content: "A bell rings."}, entries[2])
}

// TestDecodeReplayEntriesRejectsMalformedPayload verifies a payload that won't
// decode fails the replay rather than silently dropping a line.
func TestDecodeReplayEntriesRejectsMalformedPayload(t *testing.T) {
	t.Parallel()
	_, err := decodeReplayEntries([]pluginsdk.Event{
		{ID: "x", Type: pluginsdk.EventType("scene_say"), Payload: `{not json}`},
	})
	require.Error(t, err)
}

// TestHandleLogDeniesNonParticipant pins the INV-S9 gate: a non-participant is
// denied before any history read (the focus client is never consulted).
func TestHandleLogDeniesNonParticipant(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.installRoster("scene-x", "owner-char") // owner-char is the only participant
	svc := NewSceneServiceImpl(store)
	p := &scenePlugin{service: svc, focusClient: &fakeFocusClient{}}

	resp, err := p.handleLog(context.Background(), pluginsdk.CommandRequest{CharacterID: "outsider"}, "#scene-x")

	require.NoError(t, err) // command-level rejections are CommandResponses, not Go errors
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "not a participant")
}

// TestHandleLogRendersForParticipant pins the happy path: a participant gets the
// decrypted IC history rendered as plain text.
func TestHandleLogRendersForParticipant(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.installRoster("scene-x", "owner-char")
	svc := NewSceneServiceImpl(store)
	fc := &fakeFocusClient{queryHistoryEvents: []pluginsdk.Event{
		{ID: "1", Type: pluginsdk.EventType("scene_say"), Payload: `{"actor_id":"owner-char","text":"Hello."}`},
		{ID: "2", Type: pluginsdk.EventType("scene_pose"), Payload: `{"actor_id":"owner-char","text":"waves."}`},
	}}
	p := &scenePlugin{service: svc, focusClient: fc}

	resp, err := p.handleLog(context.Background(), pluginsdk.CommandRequest{CharacterID: "owner-char"}, "#scene-x")

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, `owner-char says, "Hello."`)
	assert.Contains(t, resp.Output, "owner-char waves.")
}
