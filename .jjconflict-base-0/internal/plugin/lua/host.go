// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/samber/oops"
	lua "github.com/yuin/gopher-lua"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/pkg/holo"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Compile-time interface check.
var _ plugins.Host = (*Host)(nil)

// luaPlugin holds compiled Lua code for a plugins.
type luaPlugin struct {
	manifest *plugins.Manifest
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

// Load reads and validates a Lua plugins.
func (h *Host) Load(ctx context.Context, manifest *plugins.Manifest, dir string) error {
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

// Unload removes a plugins.
func (h *Host) Unload(_ context.Context, name string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.plugins[name]; !ok {
		return oops.In("lua").With("plugin", name).With("operation", "unload").New("plugin not loaded")
	}
	delete(h.plugins, name)
	return nil
}

// DeliverEvent executes the plugin's event handler.
// For command events, it calls on_command(ctx) if defined, falling back to on_event(event).
// For non-command events, it calls on_event(event).
//
// Partial Success Behavior: If the plugin returns emit events with validation errors (e.g.,
// missing required fields), those specific events are skipped and logged as warnings, but
// valid events are still returned. This ensures plugin bugs don't break game uptime while
// still providing visibility into issues via logs. The returned error is only non-nil for
// critical failures (plugin not found, Lua execution errors), not for emit validation issues.
func (h *Host) DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
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

	// Set context on the Lua state so host functions can inherit it
	L.SetContext(ctx)

	// Register host functions if available
	if h.hostFuncs != nil {
		h.hostFuncs.Register(L, name)
	}

	// Load plugin code
	if err := L.DoString(code); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "deliver_event").Hint("failed to load code").Wrap(err)
	}

	// For command events, try on_command first
	if event.Type == "command" {
		onCommand := L.GetGlobal("on_command")
		if onCommand.Type() != lua.LTNil {
			return h.callOnCommand(L, name, event, onCommand)
		}
		// Fall through to on_event if on_command not defined
	}

	// Check if on_event exists
	onEvent := L.GetGlobal("on_event")
	if onEvent.Type() == lua.LTNil {
		slog.Debug("plugin has no handler defined",
			"plugin", name,
			"event_type", event.Type)
		return nil, nil
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

	emits, validationErrs := h.parseEmitEvents(ret)
	if len(validationErrs) > 0 {
		slog.Warn("plugin emit validation errors",
			"plugin", name,
			"error_count", len(validationErrs),
			"errors", validationErrs)
	}
	return emits, nil
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

// callOnCommand calls the on_command handler with a typed CommandContext.
func (h *Host) callOnCommand(state *lua.LState, name string, event pluginsdk.Event, onCommand lua.LValue) ([]pluginsdk.EmitEvent, error) {
	// Parse command payload into CommandContext
	cmdCtx := holo.ParseCommandPayload(event.Payload)

	// Build Lua context table
	ctxTable := h.buildContextTable(state, cmdCtx)

	// Call on_command(ctx)
	if err := state.CallByParam(lua.P{
		Fn:      onCommand,
		NRet:    1,
		Protect: true,
	}, ctxTable); err != nil {
		return nil, oops.In("lua").With("plugin", name).With("operation", "on_command").Wrap(err)
	}

	// Get return value
	ret := state.Get(-1)
	state.Pop(1)

	emits, validationErrs := h.parseEmitEvents(ret)
	if len(validationErrs) > 0 {
		slog.Warn("plugin emit validation errors",
			"plugin", name,
			"error_count", len(validationErrs),
			"errors", validationErrs)
	}
	return emits, nil
}

// buildContextTable creates a Lua table from a CommandContext.
func (h *Host) buildContextTable(state *lua.LState, ctx holo.CommandContext) *lua.LTable {
	t := state.NewTable()
	state.SetField(t, "name", lua.LString(ctx.Name))
	state.SetField(t, "args", lua.LString(ctx.Args))
	state.SetField(t, "invoked_as", lua.LString(ctx.InvokedAs))
	state.SetField(t, "character_name", lua.LString(ctx.CharacterName))
	state.SetField(t, "character_id", lua.LString(ctx.CharacterID))
	state.SetField(t, "location_id", lua.LString(ctx.LocationID))
	state.SetField(t, "player_id", lua.LString(ctx.PlayerID))
	return t
}

func (h *Host) buildEventTable(state *lua.LState, event pluginsdk.Event) *lua.LTable {
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

func (h *Host) parseEmitEvents(ret lua.LValue) (emits []pluginsdk.EmitEvent, validationErrs []string) {
	if ret.Type() == lua.LTNil {
		return nil, nil
	}

	table, ok := ret.(*lua.LTable)
	if !ok {
		err := "returned non-table value: " + ret.Type().String()
		return nil, []string{err}
	}

	index := 0
	table.ForEach(func(_, v lua.LValue) {
		index++

		eventTable, ok := v.(*lua.LTable)
		if !ok {
			validationErrs = append(validationErrs,
				fmt.Sprintf("entry[%d]: expected table, got %s", index, v.Type().String()))
			return
		}

		stream := eventTable.RawGetString("stream").String()
		eventType := eventTable.RawGetString("type").String()
		payload := eventTable.RawGetString("payload").String()

		// Validate required fields
		if stream == "nil" || stream == "" {
			validationErrs = append(validationErrs,
				fmt.Sprintf("entry[%d]: missing required 'stream' field", index))
			return
		}

		if eventType == "nil" || eventType == "" {
			validationErrs = append(validationErrs,
				fmt.Sprintf("entry[%d]: missing required 'type' field (stream=%s)", index, stream))
			return
		}

		emit := pluginsdk.EmitEvent{
			Stream:  stream,
			Type:    pluginsdk.EventType(eventType),
			Payload: payload,
		}
		emits = append(emits, emit)
	})

	return emits, validationErrs
}
