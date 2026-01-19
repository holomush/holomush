// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package wasm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/plugin"
	"github.com/oklog/ulid/v2"
)

// Minimal WASM module that exports an "add" function: (i32, i32) -> i32
// Built from WAT:
//
//	(module
//	  (func (export "add") (param i32 i32) (result i32)
//	    local.get 0
//	    local.get 1
//	    i32.add))
var addWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, // magic
	0x01, 0x00, 0x00, 0x00, // version
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, // type section
	0x03, 0x02, 0x01, 0x00, // function section
	0x07, 0x07, 0x01, 0x03, 0x61, 0x64, 0x64, 0x00, 0x00, // export section
	0x0a, 0x09, 0x01, 0x07, 0x00, 0x20, 0x00, 0x20, 0x01, 0x6a, 0x0b, // code section
}

// eventHandlerWASM is a minimal WASM module with memory, alloc, and handle_event exports.
// - alloc(size i32) -> i32: always returns 0
// - handle_event(ptr i32, len i32) -> i64: always returns 0 (no response)
var eventHandlerWASM = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, // magic + version
	0x01, 0x0c, 0x02, 0x60, 0x01, 0x7f, 0x01, 0x7f, // type section
	0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7e, // (i32, i32) -> i64
	0x03, 0x03, 0x02, 0x00, 0x01, // function section
	0x05, 0x03, 0x01, 0x00, 0x01, // memory section
	0x07, 0x21, 0x03, // export section
	0x06, 0x6d, 0x65, 0x6d, 0x6f, 0x72, 0x79, 0x02, 0x00, // "memory"
	0x05, 0x61, 0x6c, 0x6c, 0x6f, 0x63, 0x00, 0x00, // "alloc"
	0x0c, 0x68, 0x61, 0x6e, 0x64, 0x6c, 0x65, 0x5f, 0x65, 0x76, 0x65, 0x6e, 0x74, 0x00, 0x01, // "handle_event"
	0x0a, 0x0b, 0x02, // code section
	0x04, 0x00, 0x41, 0x00, 0x0b, // alloc: return 0
	0x04, 0x00, 0x42, 0x00, 0x0b, // handle_event: return 0
}

func TestPluginHost_LoadAndCall(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx) // Best effort cleanup in tests
	}()

	// Load the add module
	err := host.LoadPlugin(ctx, "math", addWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Call add(2, 3)
	result, err := host.CallFunction(ctx, "math", "add", 2, 3)
	if err != nil {
		t.Fatalf("CallFunction failed: %v", err)
	}

	if len(result) != 1 || result[0] != 5 {
		t.Errorf("Expected [5], got %v", result)
	}
}

func TestPluginHost_LoadInvalidWASM(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx) // Best effort cleanup in tests
	}()

	err := host.LoadPlugin(ctx, "invalid", []byte{0x00, 0x01, 0x02, 0x03})
	if err == nil {
		t.Error("Expected error for invalid WASM")
	}
}

func TestPluginHost_ClosedState(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()

	// Load a plugin first
	err := host.LoadPlugin(ctx, "math", addWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Close the host
	err = host.Close(ctx)
	if err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// LoadPlugin should fail on closed host
	err = host.LoadPlugin(ctx, "math2", addWASM)
	if !errors.Is(err, ErrHostClosed) {
		t.Errorf("Expected ErrHostClosed, got: %v", err)
	}

	// CallFunction should fail on closed host
	_, err = host.CallFunction(ctx, "math", "add", 1, 2)
	if !errors.Is(err, ErrHostClosed) {
		t.Errorf("Expected ErrHostClosed, got: %v", err)
	}

	// HasPlugin should return false after close
	if host.HasPlugin("math") {
		t.Error("Expected HasPlugin to return false after close")
	}
}

func TestToPluginEvent(t *testing.T) {
	now := time.Now()
	id := ulid.Make()

	coreEvent := core.Event{
		ID:        id,
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: now,
		Actor: core.Actor{
			Kind: core.ActorCharacter,
			ID:   "char-456",
		},
		Payload: []byte(`{"message":"Hello"}`),
	}

	pluginEvent := toPluginEvent(coreEvent)

	if pluginEvent.ID != id.String() {
		t.Errorf("ID: got %s, want %s", pluginEvent.ID, id.String())
	}
	if pluginEvent.Stream != "location:123" {
		t.Errorf("Stream: got %s, want location:123", pluginEvent.Stream)
	}
	if pluginEvent.Type != plugin.EventTypeSay {
		t.Errorf("Type: got %s, want say", pluginEvent.Type)
	}
	if pluginEvent.Timestamp != now.UnixMilli() {
		t.Errorf("Timestamp: got %d, want %d", pluginEvent.Timestamp, now.UnixMilli())
	}
	if pluginEvent.ActorKind != plugin.ActorCharacter {
		t.Errorf("ActorKind: got %d, want %d", pluginEvent.ActorKind, plugin.ActorCharacter)
	}
	if pluginEvent.ActorID != "char-456" {
		t.Errorf("ActorID: got %s, want char-456", pluginEvent.ActorID)
	}
	if pluginEvent.Payload != `{"message":"Hello"}` {
		t.Errorf("Payload: got %s, want {\"message\":\"Hello\"}", pluginEvent.Payload)
	}
}

func TestDeliverEvent_PluginWithoutHandler(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	// Load a plugin that doesn't have event handlers (the add module)
	err := host.LoadPlugin(ctx, "math", addWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	// DeliverEvent should return nil, nil for plugins without handlers
	emits, err := host.DeliverEvent(ctx, "math", event)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if emits != nil {
		t.Errorf("Expected nil emits, got: %v", emits)
	}
}

func TestDeliverEvent_PluginNotLoaded(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	_, err := host.DeliverEvent(ctx, "nonexistent", event)
	if err == nil {
		t.Error("Expected error for nonexistent plugin")
	}
}

func TestDeliverEvent_ClosedHost(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()

	_ = host.Close(ctx)

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	_, err := host.DeliverEvent(ctx, "test", event)
	if !errors.Is(err, ErrHostClosed) {
		t.Errorf("Expected ErrHostClosed, got: %v", err)
	}
}

// mockEventEmitter records plugin events for testing.
type mockEventEmitter struct {
	mu     sync.Mutex
	events []struct {
		plugin string
		emit   plugin.EmitEvent
	}
}

func (m *mockEventEmitter) EmitPluginEvent(_ context.Context, pluginName string, evt plugin.EmitEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, struct {
		plugin string
		emit   plugin.EmitEvent
	}{plugin: pluginName, emit: evt})
	return nil
}

func (m *mockEventEmitter) getEvents() []struct {
	plugin string
	emit   plugin.EmitEvent
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]struct {
		plugin string
		emit   plugin.EmitEvent
	}, len(m.events))
	copy(result, m.events)
	return result
}

func TestPluginSubscriber_Subscribe(t *testing.T) {
	host := NewPluginHost()
	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)

	sub.Subscribe("echo", "location:123")
	sub.Subscribe("echo", "location:456")
	sub.Subscribe("other", "location:123")

	if len(sub.plugins) != 2 {
		t.Errorf("Expected 2 plugins subscribed, got %d", len(sub.plugins))
	}
	if len(sub.plugins["echo"]) != 2 {
		t.Errorf("Expected echo to have 2 streams, got %d", len(sub.plugins["echo"]))
	}
}

func TestPluginSubscriber_StartStop(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)
	sub.Subscribe("echo", "location:123")

	// Start should not block
	sub.Start(ctx)

	// Stop should return without hanging
	done := make(chan bool)
	go func() {
		sub.Stop()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Stop took too long")
	}
}

func TestPluginSubscriber_DispatchToPlugins(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	// Load a plugin (without handlers, so it won't emit responses)
	err := host.LoadPlugin(ctx, "math", addWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)
	sub.Subscribe("math", "location:123")
	sub.Start(ctx)
	defer sub.Stop()

	// Give goroutines time to start
	time.Sleep(10 * time.Millisecond)

	// Broadcast an event
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}
	broadcaster.Broadcast(event)

	// Give time for dispatch
	time.Sleep(50 * time.Millisecond)

	// The math plugin doesn't emit responses, so emitter should have no events
	events := emitter.getEvents()
	if len(events) != 0 {
		t.Errorf("Expected no emitted events, got %d", len(events))
	}
}

func TestCallFunction_PluginNotLoaded(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	_, err := host.CallFunction(ctx, "nonexistent", "add", 1, 2)
	if err == nil {
		t.Error("Expected error for nonexistent plugin")
	}
	if err.Error() != "plugin nonexistent not loaded" {
		t.Errorf("Expected 'plugin nonexistent not loaded', got: %v", err)
	}
}

func TestCallFunction_FunctionNotFound(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	err := host.LoadPlugin(ctx, "math", addWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	_, err = host.CallFunction(ctx, "math", "nonexistent", 1, 2)
	if err == nil {
		t.Error("Expected error for nonexistent function")
	}
	if err.Error() != "function nonexistent not found in math" {
		t.Errorf("Expected 'function nonexistent not found in math', got: %v", err)
	}
}

func TestPluginHost_CloseWithNilRuntime(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()

	// Close without ever loading a plugin (nil runtime)
	err := host.Close(ctx)
	if err != nil {
		t.Errorf("Expected no error closing nil runtime, got: %v", err)
	}
}

func TestPluginSubscriber_StopWithNilCancel(_ *testing.T) {
	host := NewPluginHost()
	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)
	// Don't call Start, so cancel is nil

	// Stop should handle nil cancel gracefully
	sub.Stop()
}

func TestPluginSubscriber_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	host := NewPluginHost()
	defer func() {
		_ = host.Close(context.Background())
	}()

	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)
	sub.Subscribe("math", "location:123")
	sub.Start(ctx)

	// Cancel the context to trigger dispatchLoop exit via ctx.Done()
	cancel()

	// Give time for the context cancellation to be processed
	time.Sleep(50 * time.Millisecond)

	// Stop should complete quickly since the goroutine should have exited
	done := make(chan bool)
	go func() {
		sub.Stop()
		done <- true
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Stop took too long after context cancel")
	}
}

func TestPluginSubscriber_DeliverAndEmitError(_ *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)
	// Subscribe a non-existent plugin to trigger delivery error
	sub.Subscribe("nonexistent", "location:123")

	// Manually call dispatchToPlugins to test error handling
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	// This should log an error but not panic
	sub.dispatchToPlugins(ctx, "location:123", event)
}

func TestPluginSubscriber_MultipleStreams(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	err := host.LoadPlugin(ctx, "math", addWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)
	sub.Subscribe("math", "location:123")
	sub.Subscribe("math", "location:456")
	sub.Start(ctx)
	defer sub.Stop()

	// Give goroutines time to start
	time.Sleep(10 * time.Millisecond)

	// Broadcast to first stream
	event1 := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test1"}`),
	}
	broadcaster.Broadcast(event1)

	// Broadcast to second stream
	event2 := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:456",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test2"}`),
	}
	broadcaster.Broadcast(event2)

	// Give time for dispatch
	time.Sleep(50 * time.Millisecond)
}

func TestPluginSubscriber_NoMatchingStream(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	err := host.LoadPlugin(ctx, "math", addWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)
	sub.Subscribe("math", "location:123")

	// Manually dispatch to a stream that math isn't subscribed to
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:999",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	// This should do nothing (no matching stream for math plugin)
	sub.dispatchToPlugins(ctx, "location:999", event)
}

func TestDeliverEvent_WithEventHandler(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	// Load the event handler module
	err := host.LoadPlugin(ctx, "handler", eventHandlerWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}

	// DeliverEvent should succeed and return nil (handle_event returns 0)
	emits, err := host.DeliverEvent(ctx, "handler", event)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if emits != nil {
		t.Errorf("Expected nil emits, got: %v", emits)
	}
}

func TestDeliverEvent_MemoryWriteWithEventHandler(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	// Load the event handler module
	err := host.LoadPlugin(ctx, "handler", eventHandlerWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Create an event with a large payload to test memory operations
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"This is a longer message to test memory write"}`),
	}

	emits, err := host.DeliverEvent(ctx, "handler", event)
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
	if emits != nil {
		t.Errorf("Expected nil emits, got: %v", emits)
	}
}

func TestPluginSubscriber_DispatchWithEventHandler(t *testing.T) {
	ctx := context.Background()
	host := NewPluginHost()
	defer func() {
		_ = host.Close(ctx)
	}()

	err := host.LoadPlugin(ctx, "handler", eventHandlerWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	broadcaster := core.NewBroadcaster()
	emitter := &mockEventEmitter{}

	sub := NewPluginSubscriber(host, broadcaster, emitter)
	sub.Subscribe("handler", "location:123")
	sub.Start(ctx)
	defer sub.Stop()

	// Give goroutines time to start
	time.Sleep(10 * time.Millisecond)

	// Broadcast an event
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:123",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "test"},
		Payload:   []byte(`{"message":"test"}`),
	}
	broadcaster.Broadcast(event)

	// Give time for dispatch
	time.Sleep(50 * time.Millisecond)

	// The handler returns 0 (no response), so emitter should have no events
	events := emitter.getEvents()
	if len(events) != 0 {
		t.Errorf("Expected no emitted events, got %d", len(events))
	}
}
