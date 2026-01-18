// Package wasm provides WebAssembly plugin hosting using Extism.
package wasm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	extism "github.com/extism/go-sdk"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/plugin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// ErrPluginNotFound is returned when the requested plugin is not loaded.
var ErrPluginNotFound = errors.New("plugin not found")

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
	_, span := h.tracer.Start(ctx, "ExtismHost.LoadPlugin",
		trace.WithAttributes(attribute.String("plugin.name", name)))
	defer span.End()

	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		span.RecordError(ErrHostClosed)
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
		err = fmt.Errorf("failed to create plugin %s: %w", name, err)
		span.RecordError(err)
		return err
	}

	h.plugins[name] = plugin
	slog.Info("plugin loaded", "name", name, "wasm_size", len(wasmBytes))
	return nil
}

// HasPlugin returns true if a plugin with the given name is loaded.
// Returns false if the host has been closed.
func (h *ExtismHost) HasPlugin(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if h.closed {
		return false
	}
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
	for name, p := range h.plugins {
		if err := p.Close(ctx); err != nil {
			errs = append(errs, err)
			slog.Warn("failed to close plugin", "plugin", name, "error", err)
		}
	}
	h.plugins = nil
	h.closed = true
	return errors.Join(errs...)
}

// DeliverEvent sends an event to a plugin and returns any emitted events.
func (h *ExtismHost) DeliverEvent(ctx context.Context, pluginName string, event core.Event) ([]plugin.EmitEvent, error) {
	_, span := h.tracer.Start(ctx, "ExtismHost.DeliverEvent",
		trace.WithAttributes(
			attribute.String("plugin.name", pluginName),
			attribute.String("event.type", string(event.Type)),
			attribute.String("event.stream", event.Stream),
		))
	defer span.End()

	// Hold RLock for entire operation to prevent race with Close()
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed {
		span.RecordError(ErrHostClosed)
		return nil, ErrHostClosed
	}

	p, ok := h.plugins[pluginName]
	if !ok {
		err := fmt.Errorf("%w: %s", ErrPluginNotFound, pluginName)
		span.RecordError(err)
		return nil, err
	}

	// Check if plugin exports handle_event
	if !p.FunctionExists("handle_event") {
		// Plugin doesn't handle events - not an error
		return nil, nil
	}

	// Convert core.Event to plugin.Event and marshal to JSON
	eventJSON, err := json.Marshal(plugin.Event{
		ID:        event.ID.String(),
		Stream:    event.Stream,
		Type:      plugin.EventType(event.Type),
		Timestamp: event.Timestamp.UnixMilli(),
		ActorKind: plugin.ActorKind(event.Actor.Kind),
		ActorID:   event.Actor.ID,
		Payload:   string(event.Payload),
	})
	if err != nil {
		err = fmt.Errorf("failed to marshal event: %w", err)
		span.RecordError(err)
		return nil, err
	}

	// Call plugin's handle_event function
	// Extism handles memory allocation internally
	_, output, err := p.Call("handle_event", eventJSON)
	if err != nil {
		err = fmt.Errorf("plugin call failed: %w", err)
		span.RecordError(err)
		return nil, err
	}

	// Empty output means no events to emit
	if len(output) == 0 {
		return nil, nil
	}

	// Parse response
	var response plugin.Response
	if err := json.Unmarshal(output, &response); err != nil {
		err = fmt.Errorf("failed to unmarshal response: %w", err)
		span.RecordError(err)
		return nil, err
	}

	return response.Events, nil
}
