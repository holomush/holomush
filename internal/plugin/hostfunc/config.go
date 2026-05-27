// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"strconv"
	"time"

	lua "github.com/yuin/gopher-lua"
)

// registerConfigTable installs a holomush.config subtable on mod with typed
// read-only accessors for the merged plugin config map. The accessors are
// opaque: they read pre-validated string values and parse them to the
// requested Go/Lua type on each call.
//
// Accessor behaviour:
//   - Non-require variants (duration/int/bool/string): return nil when the key
//     is absent (the plugin did not declare it or it has no effective value).
//   - require_* variants (require_duration/require_int/require_bool/
//     require_string): raise a Lua error when the key is absent.
//
// cfg may be nil or empty; all accessors will return nil in that case.
func registerConfigTable(L *lua.LState, mod *lua.LTable, cfg map[string]string) { //nolint:gocritic // L is conventional gopher-lua parameter name
	tbl := L.NewTable()

	// duration(key) → number (seconds) or nil
	L.SetField(tbl, "duration", L.NewFunction(func(ls *lua.LState) int {
		key := ls.CheckString(1)
		v, ok := cfg[key]
		if !ok {
			ls.Push(lua.LNil)
			return 1
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			ls.RaiseError("holomush.config.duration: key %q value %q is not a valid duration: %s", key, v, err)
			return 0
		}
		ls.Push(lua.LNumber(d.Seconds()))
		return 1
	}))

	// require_duration(key) → number (seconds); raises on absent key
	L.SetField(tbl, "require_duration", L.NewFunction(func(ls *lua.LState) int {
		key := ls.CheckString(1)
		v, ok := cfg[key]
		if !ok {
			ls.RaiseError("holomush.config.require_duration: required key %q absent from plugin config", key)
			return 0
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			ls.RaiseError("holomush.config.require_duration: key %q value %q is not a valid duration: %s", key, v, err)
			return 0
		}
		ls.Push(lua.LNumber(d.Seconds()))
		return 1
	}))

	// parseInt is shared logic for both int and require_int; the error label is
	// accessor-agnostic since both callers share it.
	parseInt := func(ls *lua.LState, key, val string) int {
		n, err := strconv.Atoi(val)
		if err != nil {
			ls.RaiseError("holomush.config: key %q value %q is not a valid integer: %s", key, val, err)
			return 0
		}
		ls.Push(lua.LNumber(n))
		return 1
	}

	// int(key) → number or nil
	L.SetField(tbl, "int", L.NewFunction(func(ls *lua.LState) int {
		key := ls.CheckString(1)
		v, ok := cfg[key]
		if !ok {
			ls.Push(lua.LNil)
			return 1
		}
		return parseInt(ls, key, v)
	}))

	// require_int(key) → number; raises on absent key
	L.SetField(tbl, "require_int", L.NewFunction(func(ls *lua.LState) int {
		key := ls.CheckString(1)
		v, ok := cfg[key]
		if !ok {
			ls.RaiseError("holomush.config.require_int: required key %q absent from plugin config", key)
			return 0
		}
		return parseInt(ls, key, v)
	}))

	// bool(key) → boolean or nil
	L.SetField(tbl, "bool", L.NewFunction(func(ls *lua.LState) int {
		key := ls.CheckString(1)
		v, ok := cfg[key]
		if !ok {
			ls.Push(lua.LNil)
			return 1
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			ls.RaiseError("holomush.config.bool: key %q value %q is not a valid boolean: %s", key, v, err)
			return 0
		}
		if b {
			ls.Push(lua.LTrue)
		} else {
			ls.Push(lua.LFalse)
		}
		return 1
	}))

	// require_bool(key) → boolean; raises on absent key
	L.SetField(tbl, "require_bool", L.NewFunction(func(ls *lua.LState) int {
		key := ls.CheckString(1)
		v, ok := cfg[key]
		if !ok {
			ls.RaiseError("holomush.config.require_bool: required key %q absent from plugin config", key)
			return 0
		}
		b, err := strconv.ParseBool(v)
		if err != nil {
			ls.RaiseError("holomush.config.require_bool: key %q value %q is not a valid boolean: %s", key, v, err)
			return 0
		}
		if b {
			ls.Push(lua.LTrue)
		} else {
			ls.Push(lua.LFalse)
		}
		return 1
	}))

	// string(key) → string or nil
	L.SetField(tbl, "string", L.NewFunction(func(ls *lua.LState) int {
		key := ls.CheckString(1)
		v, ok := cfg[key]
		if !ok {
			ls.Push(lua.LNil)
			return 1
		}
		ls.Push(lua.LString(v))
		return 1
	}))

	// require_string(key) → string; raises on absent key
	L.SetField(tbl, "require_string", L.NewFunction(func(ls *lua.LState) int {
		key := ls.CheckString(1)
		v, ok := cfg[key]
		if !ok {
			ls.RaiseError("holomush.config.require_string: required key %q absent from plugin config", key)
			return 0
		}
		ls.Push(lua.LString(v))
		return 1
	}))

	L.SetField(mod, "config", tbl)
}
