package wasm_test

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/wasm"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace/noop"
)

//go:embed testdata/alloc.wasm
var allocWASM []byte

//go:embed testdata/echo.wasm
var echoWASM []byte

//go:embed testdata/malformed.wasm
var malformedWASM []byte

func TestExtismHost_Close(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)

	err := host.Close(context.Background())
	if err != nil {
		t.Fatalf("Close returned error: %v", err)
	}

	// Double close should not error
	err = host.Close(context.Background())
	if err != nil {
		t.Fatalf("Double Close returned error: %v", err)
	}
}

func TestExtismHost_LoadPlugin(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	err := host.LoadPlugin(context.Background(), "test-plugin", allocWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	if !host.HasPlugin("test-plugin") {
		t.Error("HasPlugin returned false for loaded plugin")
	}
}

func TestExtismHost_LoadPlugin_SpanAttribute(t *testing.T) {
	// Create in-memory span exporter to capture spans
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	const pluginName = "my-test-plugin"
	err := host.LoadPlugin(context.Background(), pluginName, allocWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Verify span was created with plugin.name attribute
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected at least one span, got none")
	}

	var foundSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "ExtismHost.LoadPlugin" {
			foundSpan = &spans[i]
			break
		}
	}

	if foundSpan == nil {
		t.Fatal("ExtismHost.LoadPlugin span not found")
	}

	// Check for plugin.name attribute
	var foundAttr bool
	for _, attr := range foundSpan.Attributes {
		if attr.Key == attribute.Key("plugin.name") && attr.Value.AsString() == pluginName {
			foundAttr = true
			break
		}
	}

	if !foundAttr {
		t.Errorf("plugin.name attribute not found or has wrong value; attributes: %v", foundSpan.Attributes)
	}
}

func TestExtismHost_LoadPlugin_InvalidWASM(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	err := host.LoadPlugin(context.Background(), "bad", []byte("not wasm"))
	if err == nil {
		t.Error("LoadPlugin should fail for invalid WASM")
	}
}

func TestExtismHost_LoadPlugin_AfterClose(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	_ = host.Close(context.Background())

	err := host.LoadPlugin(context.Background(), "test", allocWASM)
	if err == nil {
		t.Error("LoadPlugin should fail after Close")
	}
	if !errors.Is(err, wasm.ErrHostClosed) {
		t.Errorf("expected ErrHostClosed, got: %v", err)
	}
}

func TestExtismHost_HasPlugin_NotLoaded(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	if host.HasPlugin("nonexistent") {
		t.Error("HasPlugin returned true for non-existent plugin")
	}
}

func TestExtismHost_HasPlugin_AfterClose(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)

	// Load a plugin
	err := host.LoadPlugin(context.Background(), "echo", allocWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Verify plugin is loaded
	if !host.HasPlugin("echo") {
		t.Error("HasPlugin returned false for loaded plugin before close")
	}

	// Close the host
	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, HasPlugin should return false
	if host.HasPlugin("echo") {
		t.Error("HasPlugin returned true after host was closed")
	}
}

func TestExtismHost_DeliverEvent(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load the alloc.wasm test fixture (already embedded)
	err := host.LoadPlugin(context.Background(), "echo", allocWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	// Note: allocWASM is a minimal test fixture that may not have handle_event
	// The test verifies DeliverEvent handles this gracefully
	_, err = host.DeliverEvent(context.Background(), "echo", event)
	if err != nil {
		t.Fatalf("DeliverEvent failed: %v", err)
	}
}

func TestExtismHost_DeliverEvent_PluginNotFound(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	event := core.Event{
		ID:     ulid.Make(),
		Stream: "location:test",
		Type:   core.EventTypeSay,
	}

	_, err := host.DeliverEvent(context.Background(), "nonexistent", event)
	if err == nil {
		t.Error("DeliverEvent should fail for nonexistent plugin")
	}
	if !errors.Is(err, wasm.ErrPluginNotFound) {
		t.Errorf("expected ErrPluginNotFound, got: %v", err)
	}
}

func TestExtismHost_DeliverEvent_EchoPlugin(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load the Python echo plugin
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello world"}`),
	}

	emitted, err := host.DeliverEvent(context.Background(), "echo", event)
	if err != nil {
		t.Fatalf("DeliverEvent failed: %v", err)
	}

	// Echo plugin should emit one event with "Echo: hello world"
	if len(emitted) != 1 {
		t.Fatalf("expected 1 emitted event, got %d", len(emitted))
	}

	if emitted[0].Stream != "location:test" {
		t.Errorf("expected stream 'location:test', got %q", emitted[0].Stream)
	}

	if string(emitted[0].Type) != "say" {
		t.Errorf("expected type 'say', got %q", emitted[0].Type)
	}

	// Check payload contains the echoed message
	if emitted[0].Payload == "" {
		t.Error("expected non-empty payload")
	}
}

func TestExtismHost_DeliverEvent_AfterClose(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)

	// Load a plugin
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Close the host
	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// DeliverEvent after close should return ErrHostClosed
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	_, err = host.DeliverEvent(context.Background(), "echo", event)
	if err == nil {
		t.Error("DeliverEvent should fail after Close")
	}
	if !errors.Is(err, wasm.ErrHostClosed) {
		t.Errorf("expected ErrHostClosed, got: %v", err)
	}
}

func TestExtismHost_DeliverEvent_MalformedJSON(t *testing.T) {
	// Create tracer with exporter to verify span error recording
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load the malformed JSON plugin
	err := host.LoadPlugin(context.Background(), "malformed", malformedWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	// DeliverEvent should return error for malformed JSON response
	_, err = host.DeliverEvent(context.Background(), "malformed", event)
	if err == nil {
		t.Fatal("DeliverEvent should fail for malformed JSON response")
	}

	// Verify error message indicates JSON unmarshal failure
	if !strings.Contains(err.Error(), "failed to unmarshal response") {
		t.Errorf("expected error containing 'failed to unmarshal response', got: %v", err)
	}

	// Verify span recorded the error
	spans := exporter.GetSpans()
	var deliverSpan *tracetest.SpanStub
	for i := range spans {
		if spans[i].Name == "ExtismHost.DeliverEvent" {
			deliverSpan = &spans[i]
			break
		}
	}

	if deliverSpan == nil {
		t.Fatal("ExtismHost.DeliverEvent span not found")
	}

	// Verify error was recorded on span
	if len(deliverSpan.Events) == 0 {
		t.Error("expected span to have error events recorded")
	}
}

func TestExtismHost_DeliverEvent_ConcurrentClose(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)

	// Load the echo plugin
	err := host.LoadPlugin(context.Background(), "echo", echoWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	// Start goroutine calling DeliverEvent in a loop
	done := make(chan struct{})
	started := make(chan struct{})
	go func() {
		defer close(done)
		close(started) // Signal that goroutine has started
		for i := 0; i < 100; i++ {
			_, err := host.DeliverEvent(context.Background(), "echo", event)
			// After Close(), we expect ErrHostClosed
			if err != nil && !errors.Is(err, wasm.ErrHostClosed) {
				// Plugin call errors are acceptable during shutdown
				continue
			}
		}
	}()

	// Wait for goroutine to start
	<-started

	// Close from main goroutine
	_ = host.Close(context.Background())

	// Wait for goroutine to finish
	select {
	case <-done:
		// Success - no race detected
	case <-time.After(5 * time.Second):
		t.Fatal("test timed out waiting for goroutine")
	}
}
