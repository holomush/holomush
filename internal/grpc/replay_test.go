// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/grpc/focus"
	"github.com/holomush/holomush/internal/session"
)

// newReplayTestServer constructs a CoreServer with the minimum fields needed
// for replayRestorePlan, fetchForMode, and applyCtrlUpdate tests.
func newReplayTestServer(t *testing.T, eventStore core.EventStore, sessStore session.Store) *CoreServer {
	t.Helper()
	return &CoreServer{
		engine:       core.NewEngine(eventStore),
		eventStore:   eventStore,
		sessionStore: sessStore,
		cursorLocks:  newCursorLockMap(),
	}
}

// --- replayRestorePlan ---

func TestReplayRestorePlanMergeSortsByULID(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	sessionID := "sess-merge"
	info := &session.Info{
		ID:     sessionID,
		Status: session.StatusActive,
	}
	require.NoError(t, sessStore.Set(ctx, sessionID, info))

	// Create events on two streams with interleaved ULIDs.
	// Stream A gets events 1 and 3; stream B gets event 2.
	streamA := "test:streamA"
	streamB := "test:streamB"

	ev1 := core.NewEvent(streamA, core.EventTypeSay, core.Actor{Kind: core.ActorCharacter, ID: "c1"}, []byte(`{"msg":"a1"}`))
	time.Sleep(time.Millisecond)
	ev2 := core.NewEvent(streamB, core.EventTypeSay, core.Actor{Kind: core.ActorCharacter, ID: "c1"}, []byte(`{"msg":"b1"}`))
	time.Sleep(time.Millisecond)
	ev3 := core.NewEvent(streamA, core.EventTypeSay, core.Actor{Kind: core.ActorCharacter, ID: "c1"}, []byte(`{"msg":"a2"}`))

	require.NoError(t, eventStore.Append(ctx, ev1))
	require.NoError(t, eventStore.Append(ctx, ev2))
	require.NoError(t, eventStore.Append(ctx, ev3))

	server := newReplayTestServer(t, eventStore, sessStore)
	stream := &mockSubscribeStream{ctx: ctx}

	plan := focus.RestorePlan{
		Streams: []focus.StreamWithMode{
			{Stream: streamA, Mode: focus.ReplayModeFromCursor},
			{Stream: streamB, Mode: focus.ReplayModeFromCursor},
		},
	}

	err := server.replayRestorePlan(ctx, info, plan, stream, nil)
	require.NoError(t, err)

	// Verify events are delivered in global ULID order: ev1 < ev2 < ev3.
	require.Len(t, stream.events, 3)
	assert.Equal(t, ev1.ID.String(), stream.events[0].GetEvent().GetId())
	assert.Equal(t, ev2.ID.String(), stream.events[1].GetEvent().GetId())
	assert.Equal(t, ev3.ID.String(), stream.events[2].GetEvent().GetId())
}

func TestReplayRestorePlanUpdatesInMemoryCursors(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	sessionID := "sess-cursor"
	info := &session.Info{
		ID:     sessionID,
		Status: session.StatusActive,
	}
	require.NoError(t, sessStore.Set(ctx, sessionID, info))

	streamName := "test:cursor-check"
	ev1 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
	time.Sleep(time.Millisecond)
	ev2 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
	require.NoError(t, eventStore.Append(ctx, ev1))
	require.NoError(t, eventStore.Append(ctx, ev2))

	server := newReplayTestServer(t, eventStore, sessStore)
	stream := &mockSubscribeStream{ctx: ctx}

	plan := focus.RestorePlan{
		Streams: []focus.StreamWithMode{
			{Stream: streamName, Mode: focus.ReplayModeFromCursor},
		},
	}

	err := server.replayRestorePlan(ctx, info, plan, stream, nil)
	require.NoError(t, err)

	// In-memory cursor should point to the last event replayed.
	require.NotNil(t, info.EventCursors)
	assert.Equal(t, ev2.ID, info.EventCursors[streamName])
}

func TestReplayRestorePlanEmptyPlanReturnsNoError(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	info := &session.Info{ID: "sess-empty", Status: session.StatusActive}
	require.NoError(t, sessStore.Set(ctx, info.ID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	stream := &mockSubscribeStream{ctx: ctx}

	err := server.replayRestorePlan(ctx, info, focus.RestorePlan{}, stream, nil)
	require.NoError(t, err)
	assert.Empty(t, stream.events)
}

// --- fetchForMode ---

func TestFetchForModeFromCursorReplaysFromStoredCursor(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	streamName := "test:from-cursor"
	ev1 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
	time.Sleep(time.Millisecond)
	ev2 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
	require.NoError(t, eventStore.Append(ctx, ev1))
	require.NoError(t, eventStore.Append(ctx, ev2))

	info := &session.Info{
		ID:           "sess-fc",
		Status:       session.StatusActive,
		EventCursors: map[string]ulid.ULID{streamName: ev1.ID},
	}
	require.NoError(t, sessStore.Set(ctx, info.ID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	sm := focus.StreamWithMode{Stream: streamName, Mode: focus.ReplayModeFromCursor}
	events, err := server.fetchForMode(ctx, info, sm)
	require.NoError(t, err)

	// Should only return ev2, since cursor is at ev1.
	require.Len(t, events, 1)
	assert.Equal(t, ev2.ID, events[0].ID)
}

func TestFetchForModeFromCursorDefaultsToZero(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	streamName := "test:default-cursor"
	ev1 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
	require.NoError(t, eventStore.Append(ctx, ev1))

	info := &session.Info{ID: "sess-dc", Status: session.StatusActive}
	require.NoError(t, sessStore.Set(ctx, info.ID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	sm := focus.StreamWithMode{Stream: streamName, Mode: focus.ReplayModeFromCursor}
	events, err := server.fetchForMode(ctx, info, sm)
	require.NoError(t, err)

	// No cursor → replay from beginning.
	require.Len(t, events, 1)
	assert.Equal(t, ev1.ID, events[0].ID)
}

func TestFetchForModeBoundedTailUsesReplayTail(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	streamName := "test:tail"
	for i := 0; i < 5; i++ {
		ev := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
		require.NoError(t, eventStore.Append(ctx, ev))
		time.Sleep(time.Millisecond)
	}

	info := &session.Info{ID: "sess-tail", Status: session.StatusActive}
	require.NoError(t, sessStore.Set(ctx, info.ID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	sm := focus.StreamWithMode{
		Stream:    streamName,
		Mode:      focus.ReplayModeBoundedTail,
		TailCount: 2,
	}
	events, err := server.fetchForMode(ctx, info, sm)
	require.NoError(t, err)

	// Should return the last 2 events.
	require.Len(t, events, 2)
}

func TestFetchForModeLiveOnlyAdvancesCursorWithoutReplay(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	streamName := "test:live-only"
	ev1 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
	require.NoError(t, eventStore.Append(ctx, ev1))

	sessionID := "sess-lo"
	info := &session.Info{ID: sessionID, Status: session.StatusActive}
	require.NoError(t, sessStore.Set(ctx, sessionID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	sm := focus.StreamWithMode{Stream: streamName, Mode: focus.ReplayModeLiveOnly}
	events, err := server.fetchForMode(ctx, info, sm)
	require.NoError(t, err)

	// No events should be returned.
	assert.Nil(t, events)

	// In-memory cursor should be advanced to the tail.
	require.NotNil(t, info.EventCursors)
	assert.Equal(t, ev1.ID, info.EventCursors[streamName])

	// Stored cursor should also be updated.
	stored, getErr := sessStore.Get(ctx, sessionID)
	require.NoError(t, getErr)
	assert.Equal(t, ev1.ID, stored.EventCursors[streamName])
}

// --- applyCtrlUpdate ---

func TestApplyCtrlUpdateAddsStreamAndReplays(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	streamName := "plugin:channel-1"
	ev1 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{"msg":"hi"}`))
	require.NoError(t, eventStore.Append(ctx, ev1))

	sessionID := "sess-ctrl-add"
	info := &session.Info{ID: sessionID, Status: session.StatusActive}
	require.NoError(t, sessStore.Set(ctx, sessionID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	grpcStream := &mockSubscribeStream{ctx: ctx}
	mockSub := newMockSubscription()

	ctrl := sessionStreamUpdate{
		stream:     streamName,
		add:        true,
		replayMode: focus.ReplayModeFromCursor,
	}

	err := server.applyCtrlUpdate(ctx, info, mockSub, ctrl, grpcStream, nil)
	require.NoError(t, err)

	// Stream should be added to subscription.
	require.Len(t, mockSub.addedStreams, 1)
	assert.Equal(t, streamName, mockSub.addedStreams[0])

	// Event should be replayed to the client.
	require.Len(t, grpcStream.events, 1)
	assert.Equal(t, ev1.ID.String(), grpcStream.events[0].GetEvent().GetId())
}

func TestApplyCtrlUpdateRemovesStream(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	sessionID := "sess-ctrl-rm"
	info := &session.Info{ID: sessionID, Status: session.StatusActive}
	require.NoError(t, sessStore.Set(ctx, sessionID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	grpcStream := &mockSubscribeStream{ctx: ctx}
	mockSub := newMockSubscription()

	ctrl := sessionStreamUpdate{
		stream: "plugin:channel-old",
		add:    false,
	}

	err := server.applyCtrlUpdate(ctx, info, mockSub, ctrl, grpcStream, nil)
	require.NoError(t, err)

	require.Len(t, mockSub.removedStreams, 1)
	assert.Equal(t, "plugin:channel-old", mockSub.removedStreams[0])
	assert.Empty(t, grpcStream.events)
}

func TestApplyCtrlUpdateUpdatesInMemoryCursors(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	streamName := "plugin:channel-cursor"
	ev1 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
	time.Sleep(time.Millisecond)
	ev2 := core.NewEvent(streamName, core.EventTypeSay, core.Actor{}, []byte(`{}`))
	require.NoError(t, eventStore.Append(ctx, ev1))
	require.NoError(t, eventStore.Append(ctx, ev2))

	sessionID := "sess-ctrl-cursor"
	info := &session.Info{ID: sessionID, Status: session.StatusActive}
	require.NoError(t, sessStore.Set(ctx, sessionID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	grpcStream := &mockSubscribeStream{ctx: ctx}
	mockSub := newMockSubscription()

	ctrl := sessionStreamUpdate{
		stream:     streamName,
		add:        true,
		replayMode: focus.ReplayModeFromCursor,
	}

	err := server.applyCtrlUpdate(ctx, info, mockSub, ctrl, grpcStream, nil)
	require.NoError(t, err)

	// Both events should be replayed.
	require.Len(t, grpcStream.events, 2)

	// In-memory cursor should point to the last event.
	require.NotNil(t, info.EventCursors)
	assert.Equal(t, ev2.ID, info.EventCursors[streamName])
}

func TestApplyCtrlUpdateRejectsLocationStream(t *testing.T) {
	ctx := context.Background()
	eventStore := core.NewMemoryEventStore()
	sessStore := session.NewMemStore()

	sessionID := "sess-ctrl-reject-loc"
	info := &session.Info{ID: sessionID, Status: session.StatusActive}
	require.NoError(t, sessStore.Set(ctx, sessionID, info))

	server := newReplayTestServer(t, eventStore, sessStore)
	grpcStream := &mockSubscribeStream{ctx: ctx}
	mockSub := newMockSubscription()

	locID := ulid.Make()
	ctrl := sessionStreamUpdate{
		stream:     "location:" + locID.String(),
		add:        true,
		replayMode: focus.ReplayModeFromCursor,
	}

	err := server.applyCtrlUpdate(ctx, info, mockSub, ctrl, grpcStream, nil)
	require.NoError(t, err)

	// Stream should NOT be added — locationFollower owns location streams.
	assert.Empty(t, mockSub.addedStreams)
	assert.Empty(t, grpcStream.events)
}
