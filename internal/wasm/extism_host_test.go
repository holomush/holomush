package wasm_test

import (
	"context"
	"testing"

	"github.com/holomush/holomush/internal/wasm"
	"go.opentelemetry.io/otel/trace/noop"
)

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
