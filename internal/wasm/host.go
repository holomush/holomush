// Package wasm provides the WASM plugin host using wazero.
package wasm

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// PluginHost manages WASM plugins.
type PluginHost struct {
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
func (h *PluginHost) Close(ctx context.Context) error {
	if h.runtime != nil {
		return h.runtime.Close(ctx)
	}
	return nil
}

// LoadPlugin loads a WASM module.
func (h *PluginHost) LoadPlugin(ctx context.Context, name string, wasm []byte) error {
	if h.runtime == nil {
		h.runtime = wazero.NewRuntime(ctx)
	}

	mod, err := h.runtime.Instantiate(ctx, wasm)
	if err != nil {
		return fmt.Errorf("failed to instantiate %s: %w", name, err)
	}

	h.modules[name] = mod
	return nil
}

// CallFunction calls an exported function in a loaded plugin.
func (h *PluginHost) CallFunction(ctx context.Context, plugin, function string, args ...uint64) ([]uint64, error) {
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
	_, ok := h.modules[name]
	return ok
}
