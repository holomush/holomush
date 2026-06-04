// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"sort"
	"sync"

	lua "github.com/yuin/gopher-lua"
)

// LuaEmitRegistry accumulates registrations from holomush.register_emit_type
// calls during a Lua plugin's INV-PLUGIN-32 Load-pass. One instance per plugin.
type LuaEmitRegistry struct {
	mu    sync.Mutex
	types map[string]struct{}
}

// NewLuaEmitRegistry returns a fresh, empty LuaEmitRegistry.
func NewLuaEmitRegistry() *LuaEmitRegistry {
	return &LuaEmitRegistry{types: make(map[string]struct{})}
}

func (r *LuaEmitRegistry) add(t string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.types[t] = struct{}{}
}

// Types returns the sorted set of all registered event-type strings.
func (r *LuaEmitRegistry) Types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.types))
	for t := range r.types {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// RegisterEmitTypeFuncs installs holomush.register_emit_type(type) on
// the given module table; calls append to reg.
//
// Called via Functions.RegisterWithEmitCapture during the Lua Host's
// INV-PLUGIN-32 Load-pass to install the capturing variant. The standard
// per-delivery Functions.Register path installs a no-op variant of
// register_emit_type so that Lua plugin code (whose main.lua executes
// at top level on every event/command delivery) doesn't crash on the
// idempotent register call; only Load-time captures are honored by the
// substrate validator.
func RegisterEmitTypeFuncs(ls *lua.LState, mod *lua.LTable, reg *LuaEmitRegistry) {
	ls.SetField(mod, "register_emit_type", ls.NewFunction(func(ls *lua.LState) int {
		eventType := ls.CheckString(1)
		reg.add(eventType)
		ls.Push(lua.LTrue)
		return 1
	}))
}
