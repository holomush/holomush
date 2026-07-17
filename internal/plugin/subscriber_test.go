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
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/eventbustest"
	"github.com/holomush/holomush/internal/eventvocab"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
)

// subscriberHost is a mock Host for subscriber tests.
type subscriberHost struct {
	delivered []pluginsdk.Event
	response  []pluginsdk.EmitEvent
	err       error
	mu        sync.Mutex
}

func (h *subscriberHost) Load(context.Context, *plugins.Manifest, string) error { return nil }
func (h *subscriberHost) Unload(context.Context, string) error                  { return nil }
func (h *subscriberHost) Plugins() []string                                     { return []string{"test"} }

func (h *subscriberHost) PluginEmitRegistry(string) ([]string, bool) { return nil, false }
func (h *subscriberHost) Close(context.Context) error                { return nil }

func (h *subscriberHost) DeliverCommand(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return nil, nil
}

func (h *subscriberHost) DeliverEvent(_ context.Context, _ string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivered = append(h.delivered, event)
	return h.response, h.err
}

func (h *subscriberHost) QuerySessionStreams(context.Context, string, plugins.SessionStreamsRequest) ([]string, error) {
	return nil, nil
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

func TestSubscriberDeliversEvents(t *testing.T) {
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
		Type:   pluginsdk.EventType("say"),
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, host.deliveredCount(), "delivered count")
}

func TestSubscriberFiltersEventTypes(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"}) // Only say events

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 2)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}
	events <- pluginsdk.Event{ID: "2", Stream: "location:123", Type: pluginsdk.EventType("pose")} // Should be filtered

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, host.deliveredCount(), "delivered count (pose should be filtered)")
}

func TestSubscriberFiltersStreams(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 2)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}
	events <- pluginsdk.Event{ID: "2", Stream: "location:456", Type: pluginsdk.EventType("say")} // Different stream

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, host.deliveredCount(), "delivered count (different stream should be filtered)")
}

func TestSubscriberSubscribeAllEventTypes(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", nil) // nil = all event types

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 3)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}
	events <- pluginsdk.Event{ID: "2", Stream: "location:123", Type: pluginsdk.EventType("pose")}
	events <- pluginsdk.Event{ID: "3", Stream: "location:123", Type: pluginsdk.EventType(eventvocab.EventTypeArrive)}

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 3, host.deliveredCount(), "delivered count (all types should be delivered)")
}

func TestSubscriberEmitsResponseEvents(t *testing.T) {
	host := &subscriberHost{
		response: []pluginsdk.EmitEvent{
			{Stream: "location:123", Type: pluginsdk.EventType("say"), Payload: `{"text":"hello"}`},
		},
	}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, emitter.emittedCount(), "emitted count")
}

func TestSubscriberHandlesHostError(t *testing.T) {
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
	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}

	time.Sleep(50 * time.Millisecond)

	// Host was called
	assert.Equal(t, 1, host.deliveredCount(), "delivered count")
	// But no events emitted due to error
	assert.Equal(t, 0, emitter.emittedCount(), "emitted count (error should prevent emit)")
}

func TestSubscriberMultiplePlugins(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("plugin-a", "location:123", []string{"say"})
	sub.Subscribe("plugin-b", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}

	time.Sleep(50 * time.Millisecond)

	// Both plugins should receive the event
	assert.Equal(t, 2, host.deliveredCount(), "delivered count (both plugins should receive)")
}

func TestSubscriberStopWaitsForCompletion(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}

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

func TestSubscriberChannelClose(t *testing.T) {
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

func TestSubscriberHandlesEmitterError(t *testing.T) {
	host := &subscriberHost{
		response: []pluginsdk.EmitEvent{
			{Stream: "location:123", Type: pluginsdk.EventType("say"), Payload: `{"text":"hello"}`},
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
	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}

	time.Sleep(50 * time.Millisecond)

	// Host was called and delivered
	assert.Equal(t, 1, host.deliveredCount(), "delivered count")
	// Emitter was called (even though it errors)
	assert.Equal(t, 1, emitter.emittedCount(), "emitted count (emitter should still be called)")
}

func TestSubscriberEmptyEventTypesSliceReceivesAll(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{}) // empty slice = all event types

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 3)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}
	events <- pluginsdk.Event{ID: "2", Stream: "location:123", Type: pluginsdk.EventType("pose")}
	events <- pluginsdk.Event{ID: "3", Stream: "location:123", Type: pluginsdk.EventType(eventvocab.EventTypeArrive)}

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 3, host.deliveredCount(), "delivered count (empty slice should deliver all)")
}

func TestSubscriberStopWaitsForInFlightDeliveries(t *testing.T) {
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
	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: pluginsdk.EventType("say")}

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

func TestSubscriberCustomEventTypePassesThrough(t *testing.T) {
	host := &subscriberHost{
		response: []pluginsdk.EmitEvent{
			{Stream: "location:123", Type: "telepathy", Payload: `{"text":"you hear a voice"}`},
		},
	}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", nil) // all types

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	// Deliver an event with a custom type not in the SDK constants
	events <- pluginsdk.Event{
		ID:     "1",
		Stream: "location:123",
		Type:   "custom_event",
	}

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, host.deliveredCount(), "custom event type should be delivered to plugin")
	assert.Equal(t, 1, emitter.emittedCount(), "custom event type response should be emitted")

	emitter.mu.Lock()
	defer emitter.mu.Unlock()
	assert.Equal(t, pluginsdk.EventType("telepathy"), emitter.emitted[0].Type,
		"emitted event should preserve custom type from plugin response")
}

func TestSubscriberCustomEventTypeFilteredBySubscription(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugins.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"telepathy"}) // subscribe to custom type

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 2)
	sub.Start(ctx, events)

	events <- pluginsdk.Event{ID: "1", Stream: "location:123", Type: "telepathy"}
	events <- pluginsdk.Event{ID: "2", Stream: "location:123", Type: pluginsdk.EventType("say")} // should be filtered

	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, 1, host.deliveredCount(), "only telepathy event should be delivered")
}

func TestSubscriberRoutesResponseEventsThroughSharedEmitterWithIncomingActor(t *testing.T) {
	pluginsDir := setupRoutingFixture(t)
	bus := eventbustest.New(t)
	mockLua := mocks.NewMockHost(t)

	mockLua.EXPECT().Load(mock.Anything, mock.Anything, mock.Anything).Return(nil).Times(2)
	mockLua.EXPECT().Close(mock.Anything).Return(nil)

	bootstrapReg, bootErr := core.BootstrapVerbRegistry("test")
	require.NoError(t, bootErr)
	require.NoError(t, bootstrapReg.RegisterWithSource(core.VerbRegistration{
		Type:          "say",
		Category:      "communication",
		Format:        "speech",
		Label:         "says",
		DisplayTarget: corev1.EventChannel_EVENT_CHANNEL_TERMINAL,
		Source:        "core-communication",
	}, "1.0.0"))
	manager, mgrErr := plugins.NewManager(pluginsDir, plugins.WithLuaHost(mockLua), plugins.WithVerbRegistry(bootstrapReg))
	require.NoError(t, mgrErr)
	t.Cleanup(func() { _ = manager.Close(context.Background()) })

	require.NoError(t, manager.LoadAll(context.Background()))
	manager.ConfigureEventEmitter(bus.Bus.Publisher())

	host := &subscriberHost{
		response: []pluginsdk.EmitEvent{
			{Stream: "location.123", Type: pluginsdk.EventType("say"), Payload: `{"text":"echo"}`},
		},
	}

	sub := plugins.NewSubscriber(host, manager)
	sub.Subscribe("echo-bot", "location.123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginsdk.Event, 1)
	sub.Start(ctx, events)

	// Use a real ULID so the bridge preserves it in App-Actor-ID.
	actorID := core.NewULID()
	events <- pluginsdk.Event{
		ID:        "1",
		Stream:    "location.123",
		Type:      pluginsdk.EventType("say"),
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   actorID.String(),
	}

	require.Eventually(t, func() bool {
		return len(drainStream(t, bus.JS)) > 0
	}, 2*time.Second, 25*time.Millisecond, "expected emitter to publish event")

	msgs := drainStream(t, bus.JS)
	require.Len(t, msgs, 1)
	assert.Equal(t, "events.main.location.123", msgs[0].Subject)
	assert.Equal(t, "character", msgs[0].Header.Get(eventbus.HeaderActorKind))
	assert.Equal(t, actorID.String(), msgs[0].Header.Get(eventbus.HeaderActorID))
	assert.Equal(t, string(pluginsdk.EventType("say")), msgs[0].Header.Get(eventbus.HeaderEventType))
}

// slowSubscriberHost blocks DeliverEvent until blockCh is closed.
type slowSubscriberHost struct {
	delivered []pluginsdk.Event
	blockCh   chan struct{}
	mu        sync.Mutex
}

func (h *slowSubscriberHost) Load(context.Context, *plugins.Manifest, string) error { return nil }
func (h *slowSubscriberHost) Unload(context.Context, string) error                  { return nil }
func (h *slowSubscriberHost) Plugins() []string                                     { return []string{"test"} }

func (h *slowSubscriberHost) PluginEmitRegistry(string) ([]string, bool) { return nil, false }
func (h *slowSubscriberHost) Close(context.Context) error                { return nil }

func (h *slowSubscriberHost) DeliverCommand(_ context.Context, _ string, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return nil, nil
}

func (h *slowSubscriberHost) DeliverEvent(_ context.Context, _ string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	<-h.blockCh // Block until closed
	h.mu.Lock()
	defer h.mu.Unlock()
	h.delivered = append(h.delivered, event)
	return nil, nil
}

func (h *slowSubscriberHost) QuerySessionStreams(context.Context, string, plugins.SessionStreamsRequest) ([]string, error) {
	return nil, nil
}

func (h *slowSubscriberHost) deliveredCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.delivered)
}
