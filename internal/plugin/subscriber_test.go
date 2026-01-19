package plugin_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/plugin"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// subscriberHost is a mock Host for subscriber tests.
type subscriberHost struct {
	delivered []pluginpkg.Event
	response  []pluginpkg.EmitEvent
	err       error
	mu        sync.Mutex
}

func (h *subscriberHost) Load(context.Context, *plugin.Manifest, string) error { return nil }
func (h *subscriberHost) Unload(context.Context, string) error                 { return nil }
func (h *subscriberHost) Plugins() []string                                    { return []string{"test"} }
func (h *subscriberHost) Close(context.Context) error                          { return nil }

func (h *subscriberHost) DeliverEvent(_ context.Context, _ string, event pluginpkg.Event) ([]pluginpkg.EmitEvent, error) {
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
	emitted []pluginpkg.EmitEvent
	err     error
	mu      sync.Mutex
}

func (e *subscriberEmitter) EmitPluginEvent(_ context.Context, _ string, event pluginpkg.EmitEvent) error {
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

	sub := plugin.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginpkg.Event, 1)
	sub.Start(ctx, events)

	events <- pluginpkg.Event{
		ID:     "01ABC",
		Stream: "location:123",
		Type:   pluginpkg.EventTypeSay,
	}

	// Wait for delivery
	time.Sleep(50 * time.Millisecond)

	if got := host.deliveredCount(); got != 1 {
		t.Errorf("delivered = %d, want 1", got)
	}
}

func TestSubscriber_FiltersEventTypes(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugin.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"}) // Only say events

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginpkg.Event, 2)
	sub.Start(ctx, events)

	events <- pluginpkg.Event{ID: "1", Stream: "location:123", Type: pluginpkg.EventTypeSay}
	events <- pluginpkg.Event{ID: "2", Stream: "location:123", Type: pluginpkg.EventTypePose} // Should be filtered

	time.Sleep(50 * time.Millisecond)

	if got := host.deliveredCount(); got != 1 {
		t.Errorf("delivered = %d, want 1 (pose should be filtered)", got)
	}
}

func TestSubscriber_FiltersStreams(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugin.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginpkg.Event, 2)
	sub.Start(ctx, events)

	events <- pluginpkg.Event{ID: "1", Stream: "location:123", Type: pluginpkg.EventTypeSay}
	events <- pluginpkg.Event{ID: "2", Stream: "location:456", Type: pluginpkg.EventTypeSay} // Different stream

	time.Sleep(50 * time.Millisecond)

	if got := host.deliveredCount(); got != 1 {
		t.Errorf("delivered = %d, want 1 (different stream should be filtered)", got)
	}
}

func TestSubscriber_SubscribeAllEventTypes(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugin.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", nil) // nil = all event types

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginpkg.Event, 3)
	sub.Start(ctx, events)

	events <- pluginpkg.Event{ID: "1", Stream: "location:123", Type: pluginpkg.EventTypeSay}
	events <- pluginpkg.Event{ID: "2", Stream: "location:123", Type: pluginpkg.EventTypePose}
	events <- pluginpkg.Event{ID: "3", Stream: "location:123", Type: pluginpkg.EventTypeArrive}

	time.Sleep(50 * time.Millisecond)

	if got := host.deliveredCount(); got != 3 {
		t.Errorf("delivered = %d, want 3 (all types should be delivered)", got)
	}
}

func TestSubscriber_EmitsResponseEvents(t *testing.T) {
	host := &subscriberHost{
		response: []pluginpkg.EmitEvent{
			{Stream: "location:123", Type: pluginpkg.EventTypeSay, Payload: `{"text":"hello"}`},
		},
	}
	emitter := &subscriberEmitter{}

	sub := plugin.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginpkg.Event, 1)
	sub.Start(ctx, events)

	events <- pluginpkg.Event{ID: "1", Stream: "location:123", Type: pluginpkg.EventTypeSay}

	time.Sleep(50 * time.Millisecond)

	if got := emitter.emittedCount(); got != 1 {
		t.Errorf("emitted = %d, want 1", got)
	}
}

func TestSubscriber_HandlesHostError(t *testing.T) {
	host := &subscriberHost{
		err: errors.New("plugin error"),
	}
	emitter := &subscriberEmitter{}

	sub := plugin.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginpkg.Event, 1)
	sub.Start(ctx, events)

	// Should not panic on error
	events <- pluginpkg.Event{ID: "1", Stream: "location:123", Type: pluginpkg.EventTypeSay}

	time.Sleep(50 * time.Millisecond)

	// Host was called
	if got := host.deliveredCount(); got != 1 {
		t.Errorf("delivered = %d, want 1", got)
	}
	// But no events emitted due to error
	if got := emitter.emittedCount(); got != 0 {
		t.Errorf("emitted = %d, want 0 (error should prevent emit)", got)
	}
}

func TestSubscriber_MultiplePlugins(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugin.NewSubscriber(host, emitter)
	sub.Subscribe("plugin-a", "location:123", []string{"say"})
	sub.Subscribe("plugin-b", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan pluginpkg.Event, 1)
	sub.Start(ctx, events)

	events <- pluginpkg.Event{ID: "1", Stream: "location:123", Type: pluginpkg.EventTypeSay}

	time.Sleep(50 * time.Millisecond)

	// Both plugins should receive the event
	if got := host.deliveredCount(); got != 2 {
		t.Errorf("delivered = %d, want 2 (both plugins should receive)", got)
	}
}

func TestSubscriber_StopWaitsForCompletion(t *testing.T) {
	host := &subscriberHost{}
	emitter := &subscriberEmitter{}

	sub := plugin.NewSubscriber(host, emitter)
	sub.Subscribe("test-plugin", "location:123", []string{"say"})

	ctx, cancel := context.WithCancel(context.Background())

	events := make(chan pluginpkg.Event, 1)
	sub.Start(ctx, events)

	events <- pluginpkg.Event{ID: "1", Stream: "location:123", Type: pluginpkg.EventTypeSay}

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

	sub := plugin.NewSubscriber(host, emitter)

	ctx := context.Background()
	events := make(chan pluginpkg.Event, 1)
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
