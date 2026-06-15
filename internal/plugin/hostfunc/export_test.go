// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/pluginauthz"
)

// AuditorForTest exposes the Functions' internal auditor field for wiring-guard
// tests. This accessor exists so that external test packages (e.g.
// internal/plugin/setup) can verify that hostfunc.WithAuditLogger correctly
// propagates the auditor into Functions without relying on Lua round-trips.
// It MUST NOT be used in production code.
func (f *Functions) AuditorForTest() pluginauthz.Auditor {
	return f.auditor
}

// RegisterCapabilityFuncsForTest installs the per-domain functions for the ten
// retired capability domains (kv, world.query, world.mutation, property,
// session, session.admin, focus, eval, settings, emit) onto the existing
// holomush module table on ls, for the purpose of unit-testing the underlying
// *Fn implementations in isolation.
//
// The atomic capability cutover (holomush-eykuh.4, spec R1 / ADR holomush-05f3v)
// removed these from the production hostfunc.Register surface — they now flow
// through the host-brokered RegisterHostCaps path. The implementations themselves
// (kvGetFn, queryLocationFn, evaluateFn, …) are unchanged and still warrant
// direct coverage; this helper re-installs them on the holomush table so the
// existing legacy-surface unit tests keep exercising that logic.
//
// MUST be called after Register (which creates and globals the holomush table).
// MUST NOT be used in production code.
func (f *Functions) RegisterCapabilityFuncsForTest(ls *lua.LState, pluginName string) {
	mod, ok := ls.GetGlobal("holomush").(*lua.LTable)
	if !ok {
		ls.RaiseError("RegisterCapabilityFuncsForTest: holomush module not installed (call Register first)")
		return
	}

	// KV operations
	ls.SetField(mod, "kv_get", ls.NewFunction(f.kvGetFn(pluginName)))
	ls.SetField(mod, "kv_set", ls.NewFunction(f.kvSetFn(pluginName)))
	ls.SetField(mod, "kv_delete", ls.NewFunction(f.kvDeleteFn(pluginName)))

	// World queries
	ls.SetField(mod, "query_location", ls.NewFunction(f.queryLocationFn(pluginName)))
	ls.SetField(mod, "query_character", ls.NewFunction(f.queryCharacterFn(pluginName)))
	ls.SetField(mod, "query_location_characters", ls.NewFunction(f.queryLocationCharactersFn(pluginName)))
	ls.SetField(mod, "query_object", ls.NewFunction(f.queryObjectFn(pluginName)))

	// World mutations
	ls.SetField(mod, "create_location", ls.NewFunction(f.createLocationFn(pluginName)))
	ls.SetField(mod, "create_exit", ls.NewFunction(f.createExitFn(pluginName)))
	ls.SetField(mod, "create_object", ls.NewFunction(f.createObjectFn(pluginName)))
	ls.SetField(mod, "find_location", ls.NewFunction(f.findLocationFn(pluginName)))
	ls.SetField(mod, "set_property", ls.NewFunction(f.setPropertyFn(pluginName)))
	ls.SetField(mod, "get_property", ls.NewFunction(f.getPropertyFn(pluginName)))

	// Authorization query
	ls.SetField(mod, "evaluate", ls.NewFunction(f.evaluateFn(pluginName)))

	// Plugin-partitioned settings
	ls.SetField(mod, "get_setting", ls.NewFunction(f.getSettingFn(pluginName)))
	ls.SetField(mod, "set_setting", ls.NewFunction(f.setSettingFn(pluginName)))

	// session.* (holo.session namespace) + focus.*
	if f.sessionAccess != nil {
		if holoTable, holoOK := ls.GetGlobal("holo").(*lua.LTable); holoOK {
			RegisterSessionFuncs(ls, holoTable, f.sessionAccess)
		}
	}
	RegisterFocusFuncs(ls, mod, f.focusOps, f.historyReader)
}
