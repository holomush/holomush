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
	"github.com/holomush/holomush/internal/session"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// Tests for drainNotificationsUntilQuiet (Destroyed-branch drain in Subscribe).
// The drain exists so events emitted just before sessionStore.Delete reach the
// client ahead of STREAM_CLOSED, closing the async-NOTIFY-delivery sub-problem
// of holomush-umxj Mode B. See server.go drainNotificationsUntilQuiet doc.

func TestDrainNotificationsUntilQuietReturnsImmediatelyWhenNoNotificationsPending(t *testing.T) {
	sub := newMockSubscription()
	store := &mockEventStore{}
	server := &CoreServer{
		eventStore:  store,
		cursorLocks: newCursorLockMap(),
	}
	info := &session.Info{ID: "s", EventCursors: map[string]ulid.ULID{}}
	stream := &mockSubscribeStream{}

	start := time.Now()
	server.drainNotificationsUntilQuiet(context.Background(), info, sub, stream, nil,
		50*time.Millisecond, 500*time.Millisecond)
	elapsed := time.Since(start)

	assert.GreaterOrEqual(t, elapsed, 40*time.Millisecond,
		"drain should wait at least the quiet window for late notifications")
	assert.Less(t, elapsed, 200*time.Millisecond,
		"drain should not exceed the hard cap when no notifications arrive")
	assert.Empty(t, stream.events, "no notifications means no sends")
}

func TestDrainNotificationsUntilQuietDeliversPendingEventsBeforeReturning(t *testing.T) {
	streamName := "character:abc"
	eventID := ulid.Make()
	sub := newMockSubscription()
	store := &mockEventStore{
		replayFunc: func(_ context.Context, s string, _ ulid.ULID, _ int) ([]core.Event, error) {
			if s == streamName {
				return []core.Event{{ID: eventID, Stream: streamName, Type: core.EventTypeCommandResponse, Payload: []byte(`{}`)}}, nil
			}
			return nil, nil
		},
	}
	sessStore := session.NewMemStore()
	require.NoError(t, sessStore.Set(context.Background(), "s", &session.Info{ID: "s", EventCursors: map[string]ulid.ULID{}}))
	server := &CoreServer{
		eventStore:   store,
		sessionStore: sessStore,
		cursorLocks:  newCursorLockMap(),
	}
	info := &session.Info{ID: "s", EventCursors: map[string]ulid.ULID{}}
	stream := &mockSubscribeStream{}

	sub.notifCh <- core.StreamNotification{Stream: streamName, EventID: eventID}

	server.drainNotificationsUntilQuiet(context.Background(), info, sub, stream, nil,
		50*time.Millisecond, 500*time.Millisecond)

	require.Len(t, stream.events, 1, "expected the pending event to be delivered")
	ef, ok := stream.events[0].GetFrame().(*corev1.SubscribeResponse_Event)
	require.True(t, ok, "expected SubscribeResponse_Event frame")
	assert.Equal(t, eventID.String(), ef.Event.GetId())
	assert.Equal(t, eventID, info.EventCursors[streamName],
		"cursor should be advanced to the last delivered event")
}

func TestDrainNotificationsUntilQuietReturnsOnCtxCancel(t *testing.T) {
	sub := newMockSubscription()
	store := &mockEventStore{}
	server := &CoreServer{eventStore: store, cursorLocks: newCursorLockMap()}
	info := &session.Info{ID: "s", EventCursors: map[string]ulid.ULID{}}
	stream := &mockSubscribeStream{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	server.drainNotificationsUntilQuiet(ctx, info, sub, stream, nil,
		10*time.Second, 10*time.Second)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 100*time.Millisecond, "drain should return immediately on ctx.Done")
}

func TestDrainNotificationsUntilQuietReturnsOnSubscriptionError(t *testing.T) {
	sub := newMockSubscription()
	store := &mockEventStore{}
	server := &CoreServer{eventStore: store, cursorLocks: newCursorLockMap()}
	info := &session.Info{ID: "s", EventCursors: map[string]ulid.ULID{}}
	stream := &mockSubscribeStream{}

	sub.errCh <- assertAnErrorValue

	start := time.Now()
	server.drainNotificationsUntilQuiet(context.Background(), info, sub, stream, nil,
		10*time.Second, 10*time.Second)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 100*time.Millisecond, "drain should return immediately when sub.Errors() fires")
	assert.Empty(t, stream.events)
}

// assertAnErrorValue is any non-nil error used to fire the sub.Errors() path.
var assertAnErrorValue = &subscriptionErrorForTests{}

type subscriptionErrorForTests struct{}

func (*subscriptionErrorForTests) Error() string { return "test subscription error" }

func TestDrainNotificationsUntilQuietStopsAtHardCapUnderSustainedNotifications(t *testing.T) {
	streamName := "character:abc"
	sub := newMockSubscription()
	store := &mockEventStore{
		replayFunc: func(_ context.Context, _ string, _ ulid.ULID, _ int) ([]core.Event, error) {
			return nil, nil // no events to deliver; just exercise the drain loop
		},
	}
	server := &CoreServer{eventStore: store, cursorLocks: newCursorLockMap()}
	info := &session.Info{ID: "s", EventCursors: map[string]ulid.ULID{}}
	stream := &mockSubscribeStream{}

	// Keep feeding notifications until the drain exits, then stop.
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer close(done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				select {
				case sub.notifCh <- core.StreamNotification{Stream: streamName, EventID: ulid.Make()}:
				default:
				}
			}
		}
	}()

	quiet := 100 * time.Millisecond
	hardCap := 300 * time.Millisecond

	start := time.Now()
	server.drainNotificationsUntilQuiet(ctx, info, sub, stream, nil, quiet, hardCap)
	elapsed := time.Since(start)
	cancel()
	<-done

	assert.GreaterOrEqual(t, elapsed, hardCap-50*time.Millisecond,
		"drain should run for close to hardCap under sustained traffic")
	assert.Less(t, elapsed, hardCap+200*time.Millisecond,
		"drain must exit within hardCap plus scheduling slack, not be held indefinitely by resetting quiet timer")
}
