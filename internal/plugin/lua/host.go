package lua

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/holomush/holomush/internal/plugin"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
	lua "github.com/yuin/gopher-lua"
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
	factory *StateFactory
	plugins map[string]*luaPlugin
	mu      sync.RWMutex
	closed  bool
}

// NewHost creates a new Lua plugin host.
func NewHost() *Host {
	return &Host{
		factory: NewStateFactory(),
		plugins: make(map[string]*luaPlugin),
	}
}

// Load reads and validates a Lua plugin.
func (h *Host) Load(ctx context.Context, manifest *plugin.Manifest, dir string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return fmt.Errorf("host is closed")
	}

	entryPath := filepath.Join(dir, manifest.LuaPlugin.Entry)
	code, err := os.ReadFile(entryPath) //nolint:gosec // entryPath is from trusted manifest
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", entryPath, err)
	}

	// Validate syntax by compiling in a throwaway state
	L, err := h.factory.NewState(ctx)
	if err != nil {
		return fmt.Errorf("failed to create validation state: %w", err)
	}
	defer L.Close()

	if err := L.DoString(string(code)); err != nil {
		return fmt.Errorf("syntax error in %s: %w", manifest.LuaPlugin.Entry, err)
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
		return fmt.Errorf("plugin %s not loaded", name)
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
		return nil, fmt.Errorf("plugin %s not loaded", name)
	}
	code := p.code
	h.mu.RUnlock()

	// Create fresh state for this event
	L, err := h.factory.NewState(ctx)
	if err != nil {
		return nil, fmt.Errorf("plugin %s: failed to create state: %w", name, err)
	}
	defer L.Close()

	// Load plugin code
	if err := L.DoString(code); err != nil {
		return nil, fmt.Errorf("plugin %s: failed to load code: %w", name, err)
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
		return nil, fmt.Errorf("plugin %s: on_event failed: %w", name, err)
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
	state.SetField(t, "actor_kind", lua.LString(actorKindToString(event.ActorKind)))
	state.SetField(t, "actor_id", lua.LString(event.ActorID))
	state.SetField(t, "payload", lua.LString(event.Payload))
	return t
}

func actorKindToString(kind pluginpkg.ActorKind) string {
	switch kind {
	case pluginpkg.ActorCharacter:
		return "character"
	case pluginpkg.ActorSystem:
		return "system"
	case pluginpkg.ActorPlugin:
		return "plugin"
	default:
		return "unknown"
	}
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
