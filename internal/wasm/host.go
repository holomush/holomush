// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package wasm provides the WASM plugin host using wazero.
package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/pkg/plugin"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// ErrHostClosed is returned when operations are attempted on a closed PluginHost.
var ErrHostClosed = fmt.Errorf("plugin host is closed")

// PluginHost manages WASM plugins using wazero directly.
//
// Deprecated: Use ExtismHost instead. PluginHost is the original wazero-based
// implementation retained during migration to Extism. ExtismHost provides
// OpenTelemetry tracing and uses the Extism SDK which handles memory management.
type PluginHost struct {
	mu      sync.RWMutex
	closed  bool
	runtime wazero.Runtime
	modules map[string]api.Module
}

// NewPluginHost creates a new plugin host.
func NewPluginHost() *PluginHost {
	return &PluginHost{
		modules: make(map[string]api.Module),
	}
}

// Close shuts down the runtime and all modules.
// After Close, the PluginHost cannot be reused; further operations return ErrHostClosed.
func (h *PluginHost) Close(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.closed = true
	if h.runtime != nil {
		err := h.runtime.Close(ctx)
		h.runtime = nil
		h.modules = make(map[string]api.Module)
		if err != nil {
			return fmt.Errorf("failed to close WASM runtime: %w", err)
		}
	}
	return nil
}

// LoadPlugin loads a WASM module.
func (h *PluginHost) LoadPlugin(ctx context.Context, name string, wasm []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return ErrHostClosed
	}

	if h.runtime == nil {
		h.runtime = wazero.NewRuntime(ctx)
	}

	mod, err := h.runtime.Instantiate(ctx, wasm)
	if err != nil {
		slog.Debug("failed to instantiate WASM plugin",
			"plugin", name,
			"error", err,
		)
		return fmt.Errorf("failed to instantiate %s: %w", name, err)
	}

	slog.Debug("loaded WASM plugin", "plugin", name)
	h.modules[name] = mod
	return nil
}

// CallFunction calls an exported function in a loaded plugin.
// The read lock is held for the duration of the call to prevent
// concurrent Close() from invalidating the module mid-execution.
func (h *PluginHost) CallFunction(ctx context.Context, plugin, function string, args ...uint64) ([]uint64, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed {
		return nil, ErrHostClosed
	}

	mod, ok := h.modules[plugin]
	if !ok {
		return nil, fmt.Errorf("plugin %s not loaded", plugin)
	}

	fn := mod.ExportedFunction(function)
	if fn == nil {
		return nil, fmt.Errorf("function %s not found in %s", function, plugin)
	}

	result, err := fn.Call(ctx, args...)
	if err != nil {
		slog.Error("WASM function call failed",
			"plugin", plugin,
			"function", function,
			"error", err,
		)
		return nil, fmt.Errorf("failed to call %s.%s: %w", plugin, function, err)
	}
	return result, nil
}

// HasPlugin checks if a plugin is loaded.
func (h *PluginHost) HasPlugin(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	_, ok := h.modules[name]
	return ok
}

// toPluginEvent converts an internal Event to a plugin.Event.
func toPluginEvent(e core.Event) plugin.Event {
	return plugin.Event{
		ID:        e.ID.String(),
		Stream:    e.Stream,
		Type:      plugin.EventType(e.Type),
		Timestamp: e.Timestamp.UnixMilli(),
		ActorKind: plugin.ActorKind(e.Actor.Kind),
		ActorID:   e.Actor.ID,
		Payload:   string(e.Payload),
	}
}

// DeliverEvent sends an event to a plugin and returns any response events.
// The plugin must export: alloc(size i32) -> ptr i32, handle_event(ptr i32, len i32) -> packed i64
// The packed result contains ptr in upper 32 bits and len in lower 32 bits.
// A zero result indicates no response events.
func (h *PluginHost) DeliverEvent(ctx context.Context, pluginName string, event core.Event) ([]plugin.EmitEvent, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.closed {
		return nil, ErrHostClosed
	}

	mod, ok := h.modules[pluginName]
	if !ok {
		return nil, fmt.Errorf("plugin %s not loaded", pluginName)
	}

	// Convert and serialize event
	pe := toPluginEvent(event)
	eventJSON, err := json.Marshal(pe)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event: %w", err)
	}

	// Get required exports
	allocFn := mod.ExportedFunction("alloc")
	handleFn := mod.ExportedFunction("handle_event")
	if allocFn == nil || handleFn == nil {
		slog.Debug("plugin missing event handler exports",
			"plugin", pluginName,
			"has_alloc", allocFn != nil,
			"has_handle_event", handleFn != nil,
		)
		return nil, nil // Plugin doesn't handle events - not an error
	}

	// Allocate memory for event JSON
	eventLen := uint64(len(eventJSON))
	results, err := allocFn.Call(ctx, eventLen)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate memory in %s: %w", pluginName, err)
	}
	// #nosec G115 -- WASM32 pointers are intentionally 32-bit
	eventPtr := uint32(results[0])

	// Write event JSON to WASM memory
	mem := mod.Memory()
	if !mem.Write(eventPtr, eventJSON) {
		return nil, fmt.Errorf("failed to write event to %s memory", pluginName)
	}

	// Call handle_event
	results, err = handleFn.Call(ctx, uint64(eventPtr), eventLen)
	if err != nil {
		slog.Error("plugin handle_event failed",
			"plugin", pluginName,
			"error", err,
		)
		return nil, fmt.Errorf("handle_event failed in %s: %w", pluginName, err)
	}

	// Unpack result (upper 32 bits = ptr, lower 32 bits = len)
	packed := results[0]
	if packed == 0 {
		return nil, nil // No response
	}

	// #nosec G115 -- WASM32 pointers/lengths are intentionally 32-bit
	respPtr := uint32(packed >> 32)
	// #nosec G115 -- WASM32 pointers/lengths are intentionally 32-bit
	respLen := uint32(packed & 0xFFFFFFFF)

	// Read response JSON from WASM memory
	respJSON, ok := mem.Read(respPtr, respLen)
	if !ok {
		return nil, fmt.Errorf("failed to read response from %s memory", pluginName)
	}

	// Parse response
	var resp plugin.Response
	if err := json.Unmarshal(respJSON, &resp); err != nil {
		slog.Error("failed to parse plugin response",
			"plugin", pluginName,
			"error", err,
			"response", string(respJSON),
		)
		return nil, fmt.Errorf("failed to parse response from %s: %w", pluginName, err)
	}

	return resp.Events, nil
}

// EventEmitter publishes events from plugins.
type EventEmitter interface {
	// EmitPluginEvent creates and broadcasts an event from a plugin.
	EmitPluginEvent(ctx context.Context, pluginName string, evt plugin.EmitEvent) error
}

// PluginSubscriber subscribes plugins to event streams and dispatches events.
//
// Deprecated: Use ExtismSubscriber instead. This subscriber works with the
// deprecated PluginHost; see PluginHost deprecation notice for details.
type PluginSubscriber struct {
	host        *PluginHost
	broadcaster *core.Broadcaster
	emitter     EventEmitter
	plugins     map[string][]string // plugin name -> subscribed streams
	mu          sync.RWMutex
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// NewPluginSubscriber creates a new subscriber that wires plugins to the broadcaster.
func NewPluginSubscriber(host *PluginHost, broadcaster *core.Broadcaster, emitter EventEmitter) *PluginSubscriber {
	return &PluginSubscriber{
		host:        host,
		broadcaster: broadcaster,
		emitter:     emitter,
		plugins:     make(map[string][]string),
	}
}

// Subscribe registers a plugin to receive events from a stream.
func (ps *PluginSubscriber) Subscribe(pluginName, stream string) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.plugins[pluginName] = append(ps.plugins[pluginName], stream)
}

// Start begins listening for events and dispatching to plugins.
// Call Stop to shut down gracefully.
func (ps *PluginSubscriber) Start(ctx context.Context) {
	ctx, ps.cancel = context.WithCancel(ctx)

	ps.mu.RLock()
	defer ps.mu.RUnlock()

	// Create a unique set of streams to subscribe to
	streams := make(map[string]bool)
	for _, pluginStreams := range ps.plugins {
		for _, stream := range pluginStreams {
			streams[stream] = true
		}
	}

	// Subscribe to each stream
	for stream := range streams {
		ch := ps.broadcaster.Subscribe(stream)
		ps.wg.Add(1)
		go ps.dispatchLoop(ctx, stream, ch)
	}
}

// Stop shuts down the subscriber and waits for goroutines to finish.
func (ps *PluginSubscriber) Stop() {
	if ps.cancel != nil {
		ps.cancel()
	}
	ps.wg.Wait()
}

func (ps *PluginSubscriber) dispatchLoop(ctx context.Context, stream string, ch chan core.Event) {
	defer ps.wg.Done()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			ps.dispatchToPlugins(ctx, stream, event)
		}
	}
}

func (ps *PluginSubscriber) dispatchToPlugins(ctx context.Context, stream string, event core.Event) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	for pluginName, streams := range ps.plugins {
		for _, s := range streams {
			if s == stream {
				ps.deliverAndEmit(ctx, pluginName, event)
				break
			}
		}
	}
}

func (ps *PluginSubscriber) deliverAndEmit(ctx context.Context, pluginName string, event core.Event) {
	// Use a timeout for plugin execution
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	emitEvents, err := ps.host.DeliverEvent(ctx, pluginName, event)
	if err != nil {
		slog.Error("failed to deliver event to plugin",
			"plugin", pluginName,
			"event_id", event.ID.String(),
			"error", err,
		)
		return
	}

	// Emit any response events from the plugin
	for _, emit := range emitEvents {
		if err := ps.emitter.EmitPluginEvent(ctx, pluginName, emit); err != nil {
			slog.Error("failed to emit plugin event",
				"plugin", pluginName,
				"stream", emit.Stream,
				"error", err,
			)
		}
	}
}
