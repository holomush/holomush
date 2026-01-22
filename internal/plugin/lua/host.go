// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"os"
	"path/filepath"
	"sync"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// Compile-time interface check.
var _ plugin.Host = (*Host)(nil)

// luaPlugin holds compiled Lua code for a plugin.
type luaPlugin struct {
	manifest *plugin.Manifest
	code     string // Lua source (compiled at load time in future)
}

// Host manages Lua plugins.
type Host struct {
	factory   *StateFactory
	hostFuncs *hostfunc.Functions
	plugins   map[string]*luaPlugin
	mu        sync.RWMutex
	closed    bool
}

// NewHost creates a new Lua plugin host without host functions.
func NewHost() *Host {
	return &Host{
		factory: NewStateFactory(),
		plugins: make(map[string]*luaPlugin),
	}
}

// NewHostWithFunctions creates a Lua plugin host with host functions.
// The host functions enable plugins to call holomush.* APIs like log(), new_request_id(), and kv_*.
// Panics if hf is nil (consistent with hostfunc.New).
func NewHostWithFunctions(hf *hostfunc.Functions) *Host {
	if hf == nil {
		panic("lua.NewHostWithFunctions: hostFuncs cannot be nil")
	}
	return &Host{
		factory:   NewStateFactory(),
		hostFuncs: hf,
		plugins:   make(map[string]*luaPlugin),
	}
}

// Load reads and validates a Lua plugin.
func (h *Host) Load(ctx context.Context, manifest *plugin.Manifest, dir string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").New("host is closed")
	}

	entryPath := filepath.Join(dir, manifest.LuaPlugin.Entry)
	code, err := os.ReadFile(filepath.Clean(entryPath))
	if err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").With("path", entryPath).Hint("failed to read entry file").Wrap(err)
	}

	// Validate syntax by compiling in a throwaway state
	L, err := h.factory.NewState(ctx)
	if err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").Hint("failed to create validation state").Wrap(err)
	}
	defer L.Close()

	if err := L.DoString(string(code)); err != nil {
		return oops.In("lua").With("plugin", manifest.Name).With("operation", "load").With("entry", manifest.LuaPlugin.Entry).Hint("syntax error").Wrap(err)
	}

	h.plugins[manifest.Name] = &luaPlugin{
		manifest: manifest,
		code:     string(code),
	}

	return nil
}

// Unload removes a plugin.
func (h *Host) Unload(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.plugins[name]; !ok {
		return oops.In("lua").With("plugin", name).With("operation", "unload").New("plugin not loaded")
	}
	delete(h.plugins, name)
	return nil
}

// DeliverEvent executes the plugin's on_event function.
func (h *Host) DeliverEvent(ctx context.Context, name string, event pluginpkg.Event) ([]pluginpkg.EmitEvent, error) {
	h.mu.RLock()
	p, ok := h.plugins[name]
	if !ok {
		h.mu.RUnlock()
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_event").New("plugin not loaded")
	}
	code := p.code
	h.mu.RUnlock()

	// Create fresh state for this event
	L, err := h.factory.NewState(ctx)
	if err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_event").Hint("failed to create state").Wrap(err)
	}
	defer L.Close()

	// Register host functions if available
	if h.hostFuncs != nil {
		h.hostFuncs.Register(L, name)
	}

	// Load plugin code
	if err := L.DoString(code); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_event").Hint("failed to load code").Wrap(err)
	}

	// Check if on_event exists
	onEvent := L.GetGlobal("on_event")
	if onEvent.Type() == lua.LTNil {
		return nil, nil // No handler
	}

	// Build event table
	eventTable := h.buildEventTable(L, event)

	// Call on_event(event)
	if err := L.CallByParam(lua.P{
		Fn:      onEvent,
		NRet:    1,
		Protect: true,
	}, eventTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_event").Wrap(err)
	}

	// Get return value
	ret := L.Get(-1)
	L.Pop(1)

	return h.parseEmitEvents(ret), nil
}

// Plugins returns names of loaded plugins.
func (h *Host) Plugins() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	names := make([]string, 0, len(h.plugins))
	for name := range h.plugins {
		names = append(names, name)
	}
	return names
}

// Close shuts down the host.
func (h *Host) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	h.plugins = nil
	return nil
}

func (h *Host) buildEventTable(state *lua.LState, event pluginpkg.Event) *lua.LTable {
	t := state.NewTable()
	state.SetField(t, "id", lua.LString(event.ID))
	state.SetField(t, "stream", lua.LString(event.Stream))
	state.SetField(t, "type", lua.LString(string(event.Type)))
	state.SetField(t, "timestamp", lua.LNumber(event.Timestamp))
	state.SetField(t, "actor_kind", lua.LString(event.ActorKind.String()))
	state.SetField(t, "actor_id", lua.LString(event.ActorID))
	state.SetField(t, "payload", lua.LString(event.Payload))
	return t
}

func (h *Host) parseEmitEvents(ret lua.LValue) []pluginpkg.EmitEvent {
	if ret.Type() == lua.LTNil {
		return nil
	}

	table, ok := ret.(*lua.LTable)
	if !ok {
		return nil // Non-table return is ignored
	}

	var emits []pluginpkg.EmitEvent
	table.ForEach(func(_, v lua.LValue) {
		if eventTable, ok := v.(*lua.LTable); ok {
			emit := pluginpkg.EmitEvent{
				Stream:  eventTable.RawGetString("stream").String(),
				Type:    pluginpkg.EventType(eventTable.RawGetString("type").String()),
				Payload: eventTable.RawGetString("payload").String(),
			}
			emits = append(emits, emit)
		}
	})

	return emits
}
