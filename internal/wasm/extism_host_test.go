package wasm_test

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/wasm"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/trace/noop"
)

//go:embed testdata/alloc.wasm
var allocWASM []byte

func TestExtismHost_New(t *testing.T) {
	tracer := noop.NewTracerProvider().Tracer("test")
	host := wasm.NewExtismHost(tracer)

	if host == nil {
		t.Fatal("NewExtismHost returned nil")
	}
}

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
