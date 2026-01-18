// Package wasm provides WebAssembly plugin hosting using Extism.
package wasm

import (
	"context"
	"errors"
	"fmt"
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

// LoadPlugin loads a WASM plugin with the given name and binary.
func (h *ExtismHost) LoadPlugin(ctx context.Context, name string, wasmBytes []byte) error {
	_, span := h.tracer.Start(ctx, "ExtismHost.LoadPlugin")
	defer span.End()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrHostClosed
	}

	manifest := extism.Manifest{
		Wasm: []extism.Wasm{
			extism.WasmData{Data: wasmBytes},
		},
	}

	config := extism.PluginConfig{
		EnableWasi: true,
	}

	plugin, err := extism.NewPlugin(ctx, manifest, config, nil)
	if err != nil {
		return fmt.Errorf("failed to create plugin %s: %w", name, err)
	}

	h.plugins[name] = plugin
	return nil
}

// HasPlugin returns true if a plugin with the given name is loaded.
func (h *ExtismHost) HasPlugin(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.plugins[name]
	return ok
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
