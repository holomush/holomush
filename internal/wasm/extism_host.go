// Package wasm provides WebAssembly plugin hosting using Extism.
package wasm

import (
	"context"
	"errors"
	"sync"

	extism "github.com/extism/go-sdk"
	"go.opentelemetry.io/otel/trace"
)

// ExtismHost manages Extism-based WASM plugins with OpenTelemetry tracing.
type ExtismHost struct {
	mu      sync.RWMutex
	plugins map[string]*extism.Plugin
	tracer  trace.Tracer
	closed  bool
}

// NewExtismHost creates a new ExtismHost with the provided tracer.
func NewExtismHost(tracer trace.Tracer) *ExtismHost {
	return &ExtismHost{
		plugins: make(map[string]*extism.Plugin),
		tracer:  tracer,
	}
}

// Close releases all loaded plugins.
func (h *ExtismHost) Close(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return nil
	}

	var errs []error
	for _, p := range h.plugins {
		if err := p.Close(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	h.plugins = nil
	h.closed = true
	return errors.Join(errs...)
}
