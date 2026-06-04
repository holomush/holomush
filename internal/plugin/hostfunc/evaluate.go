// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// evaluateFn returns the holomush.evaluate(action, resource) Lua host function.
//
// Subject is derived from the host-stamped actor on the Lua VM's context
// (INV-PLUGIN-22: NEVER from Lua arguments). Delegates to pluginauthz.Evaluate with
// empty OwnedTypes — Lua plugins own no resource types, so entitlement
// degrades to the command: carve-out (spec §3).
//
// Return signature: (allowed bool, reason_or_err string|nil)
// On error:    (false, "<error message>")
// On decision: (LBool, reason string or LNil)
func (f *Functions) evaluateFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		action := L.CheckString(1)
		resource := L.CheckString(2)

		parentCtx := L.Context()
		if parentCtx == nil {
			parentCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(parentCtx, defaultPluginQueryTimeout)
		defer cancel()

		if f.engine == nil {
			slog.WarnContext(ctx, "holomush.evaluate called but no access engine configured",
				"plugin", pluginName)
			L.Push(lua.LFalse)
			L.Push(lua.LString("access engine not available"))
			return 2
		}

		actor, ok := core.ActorFromContext(ctx)
		subject := ""
		if ok {
			subject = pluginauthz.ActorSubject(actor)
		}
		// Empty subject → pluginauthz.Evaluate fails closed with EVALUATE_NO_SUBJECT.

		dec, err := pluginauthz.Evaluate(ctx, pluginauthz.Input{
			Engine:     f.engine,
			Auditor:    f.auditor,
			PluginName: pluginName,
			OwnedTypes: map[string]bool{}, // Lua plugins own no resource types
			Subject:    subject,
			Action:     action,
			Resource:   resource,
		})
		if err != nil {
			L.Push(lua.LFalse)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		L.Push(lua.LBool(dec.Allowed))
		if dec.Reason != "" {
			L.Push(lua.LString(dec.Reason))
		} else {
			L.Push(lua.LNil)
		}
		return 2
	}
}
