// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

func TestLuaConfigAccessorsReturnTypedValuesForPresentKeys(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	mod := L.NewTable()
	registerConfigTable(L, mod, map[string]string{"vote_window": "168h", "n": "3", "on": "true", "s": "hi"})
	L.SetGlobal("holomush", mod)

	require.NoError(t, L.DoString(`
		assert(holomush.config.duration("vote_window") == 168*60*60)  -- seconds
		assert(holomush.config.int("n") == 3)
		assert(holomush.config.bool("on") == true)
		assert(holomush.config.string("s") == "hi")
		assert(holomush.config.duration("absent") == nil)
		local ok = pcall(function() holomush.config.require_duration("absent") end)
		assert(ok == false)  -- require_* errors when absent
	`))
}

func TestLuaConfigAccessorsReturnNilWhenConfigMapIsNil(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	mod := L.NewTable()
	registerConfigTable(L, mod, nil)
	L.SetGlobal("holomush", mod)

	require.NoError(t, L.DoString(`
		assert(holomush.config.duration("anything") == nil)
		assert(holomush.config.int("n") == nil)
		assert(holomush.config.bool("on") == nil)
		assert(holomush.config.string("s") == nil)
	`))
}

func TestLuaConfigRequireIntReturnsValueAndRaisesWhenKeyAbsent(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	mod := L.NewTable()
	registerConfigTable(L, mod, map[string]string{"count": "5"})
	L.SetGlobal("holomush", mod)

	require.NoError(t, L.DoString(`
		assert(holomush.config.require_int("count") == 5)
		local ok = pcall(function() holomush.config.require_int("missing") end)
		assert(ok == false)
	`))
}

func TestLuaConfigRequireBoolReturnsValueAndRaisesWhenKeyAbsent(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	mod := L.NewTable()
	registerConfigTable(L, mod, map[string]string{"enabled": "true"})
	L.SetGlobal("holomush", mod)

	require.NoError(t, L.DoString(`
		assert(holomush.config.require_bool("enabled") == true)
		local ok = pcall(function() holomush.config.require_bool("missing") end)
		assert(ok == false)
	`))
}

func TestLuaConfigRequireStringReturnsValueAndRaisesWhenKeyAbsent(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	mod := L.NewTable()
	registerConfigTable(L, mod, map[string]string{"label": "hi"})
	L.SetGlobal("holomush", mod)

	require.NoError(t, L.DoString(`
		assert(holomush.config.require_string("label") == "hi")
		local ok = pcall(function() holomush.config.require_string("missing") end)
		assert(ok == false)
	`))
}

func TestLuaConfigAccessorsRaiseWhenValueDoesNotParseToType(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	mod := L.NewTable()
	registerConfigTable(L, mod, map[string]string{"d": "banana", "n": "banana", "b": "banana"})
	L.SetGlobal("holomush", mod)

	// Fail-loud: a present-but-unparseable value raises rather than returning a
	// zero/garbage value, mirroring the Go SDK decode contract.
	require.NoError(t, L.DoString(`
		assert(pcall(function() holomush.config.duration("d") end) == false)
		assert(pcall(function() holomush.config.int("n") end) == false)
		assert(pcall(function() holomush.config.bool("b") end) == false)
	`))
}
