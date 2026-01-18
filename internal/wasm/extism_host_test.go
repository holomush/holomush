package wasm_test

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/holomush/holomush/internal/wasm"
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
