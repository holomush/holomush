// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// subscriberHost is a mock Host for subscriber tests.
type subscriberHost struct {
	delivered []pluginsdk.Event
	response  []pluginsdk.EmitEvent
	err       error
	mu        sync.Mutex
}

func (h *subscriberHost) Load(context.Context, *plugins.Manifest, string) error { return nil }
func (h *subscriberHost) Unload(context.Context, string) error                 { return nil }
func (h *subscriberHost) Plugins() []string                                    { return []string{"test"} }
func (h *subscriberHost) Close(context.Context) error                          { return nil }

func (h *subscriberHost) DeliverEvent(_ context.Context, _ string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivered = append(h.delivered, event)
	return h.response, h.err
}

func (h *subscriberHost) deliveredCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.delivered)
}

// subscriberEmitter is a mock EventEmitter for subscriber tests.
type subscriberEmitter struct {
	emitted []pluginsdk.EmitEvent
	err     error
	mu      sync.Mutex
}

func (e *subscriberEmitter) EmitPluginEvent(_ context.Context, _ string, event pluginsdk.EmitEvent) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = append(e.emitted, event)
	return e.err
}

func (e *subscriberEmitter) emittedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.emitted)
}

func TestSubscriber_DeliversEvents(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{
		ID:     "01ABC",
		Stream: "location:123",
		Type:   pluginsdk.EventTypeSay,
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, host.deliveredCount(), "delivered count")
}

func TestSubscriber_FiltersEventTypes(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"}) // Only say events

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 2)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}
	events <- pluginsdk.Event{ID: "2", Stream: "location:123", Type: pluginsdk.EventTypePose} // Should be filtered

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, host.deliveredCount(), "delivered count (pose should be filtered)")
}

func TestSubscriber_FiltersStreams(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 2)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}
	events <- pluginsdk.Event{ID: "2", Stream: "location:456", Type: pluginsdk.EventTypeSay} // Different stream

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, host.deliveredCount(), "delivered count (different stream should be filtered)")
}

func TestSubscriber_SubscribeAllEventTypes(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", nil) // nil = all event types

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 3)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}
	events <- pluginsdk.Event{ID: "2", Stream: "location:123", Type: pluginsdk.EventTypePose}
	events <- pluginsdk.Event{ID: "3", Stream: "location:123", Type: pluginsdk.EventTypeArrive}

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 3, host.deliveredCount(), "delivered count (all types should be delivered)")
}

func TestSubscriber_EmitsResponseEvents(t *testing.T) {
	host := &subscriberHost{
		response: []pluginsdk.EmitEvent{
			{Stream: "location:123", Type: pluginsdk.EventTypeSay, Payload: `{"text":"hello"}`},
		},
	}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, emitter.emittedCount(), "emitted count")
}

func TestSubscriber_HandlesHostError(t *testing.T) {
	host := &subscriberHost{
		err: errors.New("plugin error"),
	}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	// Should not panic on error
	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}

	time.Sleep(50 * time.Millisecond)

	// Host was called
	assert.Equal(t, 1, host.deliveredCount(), "delivered count")
	// But no events emitted due to error
	assert.Equal(t, 0, emitter.emittedCount(), "emitted count (error should prevent emit)")
}

func TestSubscriber_MultiplePlugins(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("plugin-a", "location:123", []string{"say"})
	sub.Subscribe("plugin-b", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}

	time.Sleep(50 * time.Millisecond)

	// Both plugins should receive the event
	assert.Equal(t, 2, host.deliveredCount(), "delivered count (both plugins should receive)")
}

func TestSubscriber_StopWaitsForCompletion(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}

	// Cancel context and stop
	cancel()

	// Use a channel to detect if Stop() completes
	done := make(chan struct{})
	go func() {
		sub.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success - Stop() completed
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() did not complete within timeout")
	}
}

func TestSubscriber_ChannelClose(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)

	ctx := context.Background()
	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	// Close channel should cause graceful exit
	close(events)

	// Use a channel to detect if Stop() completes
	done := make(chan struct{})
	go func() {
		sub.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success - Stop() completed
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() did not complete within timeout after channel close")
	}
}

func TestSubscriber_HandlesEmitterError(t *testing.T) {
	host := &subscriberHost{
		response: []pluginsdk.EmitEvent{
			{Stream: "location:123", Type: pluginsdk.EventTypeSay, Payload: `{"text":"hello"}`},
		},
	}
	emitter := &subscriberEmitter{err: errors.New("emit failed")}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	// Should not panic on emitter error
	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}

	time.Sleep(50 * time.Millisecond)

	// Host was called and delivered
	assert.Equal(t, 1, host.deliveredCount(), "delivered count")
	// Emitter was called (even though it errors)
	assert.Equal(t, 1, emitter.emittedCount(), "emitted count (emitter should still be called)")
}

func TestSubscriber_EmptyEventTypesSliceReceivesAll(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{}) // empty slice = all event types

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 3)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}
	events <- pluginsdk.Event{ID: "2", Stream: "location:123", Type: pluginsdk.EventTypePose}
	events <- pluginsdk.Event{ID: "3", Stream: "location:123", Type: pluginsdk.EventTypeArrive}

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 3, host.deliveredCount(), "delivered count (empty slice should deliver all)")
}

func TestSubscriber_StopWaitsForInFlightDeliveries(t *testing.T) {
	// Use a slow host to simulate in-flight delivery
	deliveryCh := make(chan struct{})
	host := &slowSubscriberHost{
		blockCh: deliveryCh,
	}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	// Send event that will block in delivery
	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventTypeSay}

	// Give time for the async delivery to start
	time.Sleep(20 * time.Millisecond)

	// Cancel context to stop the event loop
	cancel()

	// Start Stop() in background - it should wait for in-flight delivery
	stopDone := make(chan struct{})
	go func() {
		sub.Stop()
		close(stopDone)
	}()

	// Stop should NOT complete yet because delivery is blocked
	select {
	case <-stopDone:
		t.Fatal("Stop() should not complete while delivery is in-flight")
	case <-time.After(50 * time.Millisecond):
		// Expected - Stop() is still waiting
	}

	// Unblock the delivery
	close(deliveryCh)

	// Now Stop should complete
	select {
	case <-stopDone:
		// Success - Stop() completed after delivery finished
	case <-time.After(1 * time.Second):
		t.Fatal("Stop() did not complete after unblocking delivery")
	}

	// Verify delivery happened
	assert.Equal(t, 1, host.deliveredCount(), "delivered count")
}

// slowSubscriberHost blocks DeliverEvent until blockCh is closed.
type slowSubscriberHost struct {
	delivered []pluginsdk.Event
	blockCh   chan struct{}
	mu        sync.Mutex
}

func (h *slowSubscriberHost) Load(context.Context, *plugins.Manifest, string) error { return nil }
func (h *slowSubscriberHost) Unload(context.Context, string) error                 { return nil }
func (h *slowSubscriberHost) Plugins() []string                                    { return []string{"test"} }
func (h *slowSubscriberHost) Close(context.Context) error                          { return nil }

func (h *slowSubscriberHost) DeliverEvent(_ context.Context, _ string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	<-h.blockCh // Block until closed
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivered = append(h.delivered, event)
	return nil, nil
}

func (h *slowSubscriberHost) deliveredCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.delivered)
}
