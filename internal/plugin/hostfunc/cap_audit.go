// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"time"

	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/audit"
)

// AuditCapability implements the Capability interface for the "audit"
// namespace. It injects an audit.deny / audit.allow global into the Lua
// VM so Lua plugin handlers can emit audit hints during command processing.
//
// The capability has no external dependencies — it reads the dispatcher
// context off the LState (via luaContext) and pushes events onto the
// context-bound slice. Context propagation is the dispatcher's
// responsibility (done via L.SetContext before invoking the handler).
type AuditCapability struct{}

// NewAuditCapability creates an AuditCapability. No dependencies required.
func NewAuditCapability() *AuditCapability {
	return &AuditCapability{}
}

// Namespace returns "audit", the Lua global table name for this capability.
func (c *AuditCapability) Namespace() string {
	return "audit"
}

// Register injects the audit.* functions into the Lua state as a global table.
func (c *AuditCapability) Register(L *lua.LState, pluginName string) { //nolint:gocritic // L is conventional gopher-lua parameter name
	tbl := L.NewTable()
	L.SetField(tbl, "deny", L.NewFunction(c.denyFn(pluginName)))
	L.SetField(tbl, "allow", L.NewFunction(c.allowFn(pluginName)))
	L.SetGlobal("audit", tbl)
}

// denyFn returns a Lua function implementing audit.deny(id, message, attrs).
// Signature: audit.deny(id: string, message: string, attrs: table?) -> nil
//
// Pushes an audit.Event with EffectDeny onto the context-bound slice.
// If no dispatch context is attached to the LState, the call is a silent
// no-op.
func (c *AuditCapability) denyFn(pluginName string) lua.LGFunction {
	return c.recordFn(pluginName, types.EffectDeny)
}

// allowFn returns a Lua function implementing audit.allow(id, message, attrs).
func (c *AuditCapability) allowFn(pluginName string) lua.LGFunction {
	return c.recordFn(pluginName, types.EffectAllow)
}

func (c *AuditCapability) recordFn(pluginName string, effect types.Effect) lua.LGFunction {
	return func(L *lua.LState) int {
		id := L.CheckString(1)
		message := L.CheckString(2)
		attrs := L.OptTable(3, nil)

		ctx := luaContext(L)
		event := audit.Event{
			ID:         id,
			Message:    message,
			Source:     audit.SourcePlugin, // host-stamped
			Component:  pluginName,         // host-stamped
			Effect:     effect,
			Attributes: luaTableToAttributes(attrs),
			Timestamp:  time.Now(),
		}

		audit.AddEventToContext(ctx, event)
		return 0
	}
}

// luaTableToAttributes converts a Lua table of string/string pairs into a
// Go map[string]any. Non-string values are coerced via Lua's string
// representation; keys that are not strings are skipped.
func luaTableToAttributes(tbl *lua.LTable) map[string]any {
	if tbl == nil {
		return nil
	}
	out := make(map[string]any)
	tbl.ForEach(func(k, v lua.LValue) {
		keyStr, ok := k.(lua.LString)
		if !ok {
			return
		}
		out[string(keyStr)] = v.String()
	})
	if len(out) == 0 {
		return nil
	}
	return out
}
