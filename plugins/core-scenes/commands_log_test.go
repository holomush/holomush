// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"
	"testing"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	"github.com/holomush/holomush/pkg/plugin/comm"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSceneLogFixture wires a scenePlugin where `caller` is a participant of
// "scene-x" and the focus client returns the given history events. Used by the
// scene-log export (E4) tests.
func newSceneLogFixture(t *testing.T, events ...pluginsdk.Event) (p *scenePlugin, sceneID, caller string) {
	t.Helper()
	sceneID = "scene-x"
	caller = ulid.Make().String()
	store := newFakeStore()
	store.installRoster(sceneID, caller)
	fc := &fakeFocusClient{queryHistoryEvents: events}
	return &scenePlugin{service: newTestService(t, store), focusClient: fc}, sceneID, caller
}

// TestDecodeReplayEntries verifies the IC content kinds (pose/say/emit) are
// decoded from their {actor_id, text} payloads and non-content events are
// skipped.
func TestDecodeReplayEntries(t *testing.T) {
	t.Parallel()
	events := []pluginsdk.Event{
		{ID: "1", Type: pluginsdk.EventType("core-scenes:scene_pose"), Payload: `{"actor_id":"Alice","text":"smiles warmly."}`},
		{ID: "2", Type: pluginsdk.EventType("core-scenes:scene_join_ic"), Payload: `{"actor_id":"Bob"}`}, // non-content → skipped
		{ID: "3", Type: pluginsdk.EventType("core-scenes:scene_say"), Payload: `{"actor_id":"Bob","text":"Hello."}`},
		{ID: "4", Type: pluginsdk.EventType("core-scenes:scene_emit"), Payload: `{"actor_id":"Cara","text":"A bell rings."}`},
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
		{ID: "x", Type: pluginsdk.EventType("core-scenes:scene_say"), Payload: `{not json}`},
	})
	require.Error(t, err)
}

// TestDecodersPreserveSpeakerFromCommunicationContent is the characterization
// test for holomush-kk1ot.8: payloads built by the CommunicationContent builders
// (pkg/plugin/comm, wired into handleEmit by kk1ot.7) MUST still decode through
// BOTH scene-log decoders — decodeReplayEntries (scene log replay) and
// decodeSnapshotEntry (frozen published-scene snapshot + export) — with actor_id
// round-tripping to PublishedSceneEntry.Speaker. This guards replay/export/frozen
// rendering against a future divergence of the builder from the decoders. If this
// fails, the emit-side migration dropped or renamed actor_id.
func TestDecodersPreserveSpeakerFromCommunicationContent(t *testing.T) {
	t.Parallel()

	// A pose built by the real CommunicationContent builder (":" alias → pose).
	posePayload := comm.Pose(comm.Author{ID: "01HSPEAKER", Name: "Alaric"}, ":", "waves")
	want := PublishedSceneEntry{Speaker: "01HSPEAKER", Kind: EntryKindPose, Content: "waves"}

	t.Run("decodeReplayEntries reads actor_id as Speaker", func(t *testing.T) {
		t.Parallel()
		entries, err := decodeReplayEntries([]pluginsdk.Event{
			{ID: "1", Type: pluginsdk.EventType("core-scenes:scene_pose"), Payload: posePayload},
		})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, want, entries[0])
	})

	t.Run("decodeSnapshotEntry reads actor_id as Speaker", func(t *testing.T) {
		t.Parallel()
		entry, ok, err := decodeSnapshotEntry("core-scenes:scene_pose", []byte(posePayload))
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, want, entry)
	})

	t.Run("scene_emit is actorless (empty Speaker) by design", func(t *testing.T) {
		t.Parallel()
		emitPayload := comm.Emit("a bell rings")
		entries, err := decodeReplayEntries([]pluginsdk.Event{
			{ID: "1", Type: pluginsdk.EventType("core-scenes:scene_emit"), Payload: emitPayload},
		})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		assert.Equal(t, PublishedSceneEntry{Speaker: "", Kind: EntryKindEmit, Content: "a bell rings"}, entries[0])
	})
}

// TestHandleLogDeniesNonParticipant pins the INV-SCENE-60 gate: a non-participant is
// denied before any history read (the focus client is never consulted).
func TestHandleLogDeniesNonParticipant(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.installRoster("scene-x", "owner-char") // owner-char is the only participant
	svc := newTestService(t, store)
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
	svc := newTestService(t, store)
	fc := &fakeFocusClient{queryHistoryEvents: []pluginsdk.Event{
		{ID: "1", Type: pluginsdk.EventType("core-scenes:scene_say"), Payload: `{"actor_id":"owner-char","text":"Hello."}`},
		{ID: "2", Type: pluginsdk.EventType("core-scenes:scene_pose"), Payload: `{"actor_id":"owner-char","text":"waves."}`},
	}}
	p := &scenePlugin{service: svc, focusClient: fc}

	resp, err := p.handleLog(context.Background(), pluginsdk.CommandRequest{CharacterID: "owner-char"}, "#scene-x")

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, `owner-char says, "Hello."`)
	assert.Contains(t, resp.Output, "owner-char waves.")
}

// TestHandleLogExportMarkdown — "scene log export markdown #<id>" renders the
// history with the Markdown renderer (C1).
func TestHandleLogExportMarkdown(t *testing.T) {
	t.Parallel()
	p, sceneID, caller := newSceneLogFixture(t,
		pluginsdk.Event{ID: "1", Type: pluginsdk.EventType("core-scenes:scene_say"), Payload: `{"actor_id":"Alice","text":"Hi."}`})

	resp, err := p.handleLog(context.Background(),
		pluginsdk.CommandRequest{CharacterID: caller}, "export markdown #"+sceneID)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, `**Alice** says, "Hi."`)
}

// TestHandleLogExportJSONL — "export jsonl" renders with the JSONL renderer (C3).
func TestHandleLogExportJSONL(t *testing.T) {
	t.Parallel()
	p, sceneID, caller := newSceneLogFixture(t,
		pluginsdk.Event{ID: "1", Type: pluginsdk.EventType("core-scenes:scene_say"), Payload: `{"actor_id":"Alice","text":"Hi."}`})

	resp, err := p.handleLog(context.Background(),
		pluginsdk.CommandRequest{CharacterID: caller}, "export jsonl #"+sceneID)

	require.NoError(t, err)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, `{"speaker":"Alice","kind":"say","content":"Hi."}`)
}

// TestHandleLogExportRejectsBadFormat — an unsupported format is rejected before
// any history read.
func TestHandleLogExportRejectsBadFormat(t *testing.T) {
	t.Parallel()
	p, sceneID, caller := newSceneLogFixture(t)

	resp, err := p.handleLog(context.Background(),
		pluginsdk.CommandRequest{CharacterID: caller}, "export pdf #"+sceneID)

	require.NoError(t, err)
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Unsupported export format")
}

// TestHandleLogExportRequiresFormat — "export" with no format is a usage error.
func TestHandleLogExportRequiresFormat(t *testing.T) {
	t.Parallel()
	p, _, caller := newSceneLogFixture(t)

	resp, err := p.handleLog(context.Background(),
		pluginsdk.CommandRequest{CharacterID: caller}, "export")

	require.NoError(t, err)
	assert.NotEqual(t, pluginsdk.CommandOK, resp.Status)
	assert.Contains(t, resp.Output, "Usage: scene log export")
}
