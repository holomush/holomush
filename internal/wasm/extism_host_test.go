package wasm_test

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"
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

//go:embed testdata/empty-stream.wasm
var emptyStreamWASM []byte

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
	host := getSharedEchoHost(t)

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

// TestExtismHost_NilTracer verifies that NewExtismHost with nil tracer
// uses a noop tracer instead of panicking.
func TestExtismHost_NilTracer(t *testing.T) {
	// This should not panic - nil tracer should be replaced with noop
	host := wasm.NewExtismHost(nil)
	defer func() { _ = host.Close(context.Background()) }()

	// Verify the host is usable by loading a plugin
	err := host.LoadPlugin(context.Background(), "test", allocWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed with nil tracer: %v", err)
	}

	// Verify we can check for plugins
	if !host.HasPlugin("test") {
		t.Error("HasPlugin returned false for loaded plugin")
	}

	// Verify DeliverEvent works (exercises tracer.Start internally)
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}
	_, err = host.DeliverEvent(context.Background(), "test", event)
	if err != nil {
		t.Fatalf("DeliverEvent failed with nil tracer: %v", err)
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

// logRecord captures a single log entry for testing.
type logRecord struct {
	Message string
	Attrs   map[string]any
}

// testLogHandler captures log records for verification in tests.
type testLogHandler struct {
	records []logRecord
}

func (h *testLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *testLogHandler) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{
		Message: r.Message,
		Attrs:   make(map[string]any),
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.Any()
		return true
	})
	h.records = append(h.records, rec)
	return nil
}

func (h *testLogHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *testLogHandler) WithGroup(_ string) slog.Handler      { return h }

func TestExtismHost_DeliverEvent_LogsWhenPluginLacksHandleEvent(t *testing.T) {
	// Set up log capture
	handler := &testLogHandler{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load alloc.wasm which doesn't have handle_event
	err := host.LoadPlugin(context.Background(), "no-handler", allocWASM)
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

	_, err = host.DeliverEvent(context.Background(), "no-handler", event)
	if err != nil {
		t.Fatalf("DeliverEvent failed: %v", err)
	}

	// Verify debug log was emitted
	var found bool
	for _, rec := range handler.records {
		if rec.Message == "plugin lacks handle_event export" {
			if rec.Attrs["plugin"] == "no-handler" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected debug log 'plugin lacks handle_event export' with plugin='no-handler'")
	}
}

func TestExtismHost_DeliverEvent_LogsWhenPluginReturnsEmptyOutput(t *testing.T) {
	// Set up log capture
	handler := &testLogHandler{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)
	defer func() { _ = host.Close(context.Background()) }()

	// Load empty-stream.wasm - it returns early (empty output) for non-say events
	err := host.LoadPlugin(context.Background(), "empty-output", emptyStreamWASM)
	if err != nil {
		t.Fatalf("LoadPlugin failed: %v", err)
	}

	// Use EventTypePose which empty-stream.wasm doesn't handle (only "say")
	// causing it to return early with no output
	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test",
		Type:      core.EventTypePose,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	_, err = host.DeliverEvent(context.Background(), "empty-output", event)
	if err != nil {
		t.Fatalf("DeliverEvent failed: %v", err)
	}

	// Verify debug log was emitted
	var found bool
	for _, rec := range handler.records {
		if rec.Message == "plugin returned empty output" {
			if rec.Attrs["plugin"] == "empty-output" && rec.Attrs["event_type"] == "pose" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("expected debug log 'plugin returned empty output' with plugin='empty-output' and event_type='pose'")
	}
}

// TestExtismHost_DeliverEvent_ContextCancellation verifies DeliverEvent behavior
// with context cancellation. This documents the SDK's behavior for graceful shutdown.
// See issue holomush-ci8.
//
// Key findings:
// 1. WASM compilation (LoadPlugin) is slow (~1.5s for echo plugin)
// 2. Plugin calls (DeliverEvent) after compilation are fast (~1-2ms)
// 3. Extism's CallWithContext does NOT interrupt in-flight WASM execution
//
// Conclusion: Goroutine leaks during shutdown are NOT a concern because:
// - Plugin calls complete in ~1-2ms, well under any reasonable timeout
// - The deliveryTimeout (default 5s) provides a safety boundary
// - ExtismSubscriber.Stop() waits for in-flight deliveries via WaitGroup
//
// If a plugin were to infinite-loop, the deliveryTimeout would eventually
// cause the context to expire, and subsequent operations would fail.
func TestExtismHost_DeliverEvent_ContextCancellation(t *testing.T) {
	host := getSharedEchoHost(t)

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "location:test",
		Type:      core.EventTypeSay,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char1"},
		Payload:   []byte(`{"message":"hello"}`),
	}

	t.Run("pre-cancelled context", func(t *testing.T) {
		// Create a context that we'll cancel immediately
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		// Call DeliverEvent with already-cancelled context
		start := time.Now()
		_, err := host.DeliverEvent(ctx, "echo", event)
		elapsed := time.Since(start)

		// Document observed behavior:
		// Extism's CallWithContext does NOT check context before/during execution.
		// Fast plugins complete before any cancellation check would occur.
		// This is acceptable for the echo plugin as it completes in ~1-2 seconds.
		if elapsed > 3*time.Second {
			t.Errorf("DeliverEvent took %v with cancelled context, expected < 3s", elapsed)
		}

		// Log the behavior for documentation purposes
		if err != nil {
			if errors.Is(err, context.Canceled) {
				t.Logf("Extism SDK respected context cancellation (returned context.Canceled)")
			} else {
				t.Logf("DeliverEvent returned error with cancelled context: %v", err)
			}
		} else {
			// This is the expected behavior with Extism - fast plugins complete
			t.Logf("DeliverEvent succeeded with cancelled context (plugin completed before cancellation checked)")
		}
	})

	t.Run("concurrent cancellation during call", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		// Cancel after a short delay while call is in progress
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		start := time.Now()
		_, err := host.DeliverEvent(ctx, "echo", event)
		elapsed := time.Since(start)

		// The echo plugin takes ~1-2 seconds to execute due to WASM overhead.
		// Context cancellation during execution doesn't interrupt the WASM call.
		if elapsed > 3*time.Second {
			t.Errorf("DeliverEvent took %v with concurrent cancellation, expected < 3s", elapsed)
		}

		if err != nil {
			t.Logf("DeliverEvent returned error with concurrent cancellation: %v", err)
		} else {
			t.Logf("DeliverEvent succeeded despite concurrent cancellation (plugin completed)")
		}
	})

	t.Run("short timeout context", func(t *testing.T) {
		// Test with a very short timeout to see if Extism respects DeadlineExceeded
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
		defer cancel()

		start := time.Now()
		_, err := host.DeliverEvent(ctx, "echo", event)
		elapsed := time.Since(start)

		// Document observed behavior:
		// Even with a 1ms timeout, Extism doesn't interrupt the WASM call.
		// The call runs to completion (~1-2s) because wazero's context handling
		// is asynchronous and plugin execution continues until the next context check.
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				t.Logf("Extism respected timeout (returned DeadlineExceeded after %v)", elapsed)
			} else {
				t.Logf("DeliverEvent returned error with short timeout: %v (elapsed: %v)", err, elapsed)
			}
		} else {
			// This is the observed behavior - plugin completes despite expired timeout
			t.Logf("DeliverEvent succeeded despite expired timeout (plugin completed in %v)", elapsed)
		}

		// Verify test completed reasonably - if Extism ever gains true cancellation,
		// this test will document that behavior by showing a much shorter elapsed time
		if elapsed < 100*time.Millisecond {
			t.Logf("Note: Extism may now support immediate context cancellation!")
		}
	})
}
