// Package wasm provides the WASM plugin host using wazero.
package wasm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// PluginHost manages WASM plugins.
type PluginHost struct {
	mu      sync.RWMutex
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
// After Close, the PluginHost should not be reused.
func (h *PluginHost) Close(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.runtime != nil {
		err := h.runtime.Close(ctx)
		h.runtime = nil
		h.modules = make(map[string]api.Module)
		return err
	}
	return nil
}

// LoadPlugin loads a WASM module.
func (h *PluginHost) LoadPlugin(ctx context.Context, name string, wasm []byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.runtime == nil {
		h.runtime = wazero.NewRuntime(ctx)
	}

	mod, err := h.runtime.Instantiate(ctx, wasm)
	if err != nil {
		slog.Error("failed to instantiate WASM plugin",
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

	mod, ok := h.modules[plugin]
	if !ok {
		return nil, fmt.Errorf("plugin %s not loaded", plugin)
	}

	fn := mod.ExportedFunction(function)
	if fn == nil {
		return nil, fmt.Errorf("function %s not found in %s", function, plugin)
	}

	return fn.Call(ctx, args...)
}

// HasPlugin checks if a plugin is loaded.
func (h *PluginHost) HasPlugin(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	_, ok := h.modules[name]
	return ok
}
