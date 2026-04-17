// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)

func TestStateFactoryNewStateLoadsSafeLibraries(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	// Should have base, table, string, math
	safeLibs := []string{"table", "string", "math"}
	for _, lib := range safeLibs {
		assert.NotEqual(t, "nil", L.GetGlobal(lib).Type().String(), "library %q not loaded", lib)
	}
}

func TestStateFactoryNewStateBlocksUnsafeLibraries(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	// Should NOT have os, io, debug, package
	unsafeLibs := []string{"os", "io", "debug", "package"}
	for _, lib := range unsafeLibs {
		assert.Equal(t, "nil", L.GetGlobal(lib).Type().String(), "unsafe library %q should not be loaded", lib)
	}
}

func TestStateFactoryNewStateCanExecuteLua(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	err = L.DoString(`result = 1 + 1`)
	require.NoError(t, err, "DoString() failed")

	result := L.GetGlobal("result")
	assert.Equal(t, "2", result.String())
}

func TestStateFactoryNewStateCanUseStringLibrary(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	err = L.DoString(`result = string.upper("hello")`)
	require.NoError(t, err, "DoString() failed")

	result := L.GetGlobal("result")
	assert.Equal(t, "HELLO", result.String())
}

func TestStateFactoryNewStateCanUseTableLibrary(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	err = L.DoString(`
		t = {3, 1, 2}
		table.sort(t)
		result = t[1]
	`)
	require.NoError(t, err, "DoString() failed")

	result := L.GetGlobal("result")
	assert.Equal(t, "1", result.String())
}

func TestStateFactoryNewStateCanUseMathLibrary(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	err = L.DoString(`result = math.abs(-42)`)
	require.NoError(t, err, "DoString() failed")

	result := L.GetGlobal("result")
	assert.Equal(t, "42", result.String())
}

func TestStateFactoryNewStateStateClose(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")

	// Close should not panic
	L.Close()
}

func TestStateFactoryNewStateMultipleStates(t *testing.T) {
	factory := pluginlua.NewStateFactory()

	// Create multiple states - they should be independent
	L1, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() L1 failed")
	defer L1.Close()

	L2, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() L2 failed")
	defer L2.Close()

	// Set variable in L1
	err = L1.DoString(`foo = "bar"`)
	require.NoError(t, err, "L1.DoString() failed")

	// L2 should not have the variable
	assert.Equal(t, "nil", L2.GetGlobal("foo").Type().String(), "states should be independent - L2 should not have L1's variable")
}

// TestNewStateRegistryMaxSizeApplied verifies that a StateFactory configured
// with a small RegistryMaxSize causes a table-allocation bomb to fail at the
// registry cap (surfaced as a panic caught by CallByParam Protect=true).
func TestNewStateRegistryMaxSizeApplied(t *testing.T) {
	factory := pluginlua.NewStateFactory(pluginlua.WithRegistryMaxSize(1024))
	L, err := factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	// Load a script that grows an array aggressively.
	bomb := `
local function recurse(n)
    if n <= 0 then return 0 end
    local a, b, c, d, e, f, g, h = n, n, n, n, n, n, n, n
    return recurse(n - 1) + a + b + c + d + e + f + g + h
end
return recurse(100000)
`
	// Use CallByParam with Protect=true to catch panics.
	fn := L.NewFunction(func(innerL *lua.LState) int {
		if innerErr := innerL.DoString(bomb); innerErr != nil {
			innerL.RaiseError("%s", innerErr.Error())
		}
		return 0
	})
	err = L.CallByParam(lua.P{
		Fn:      fn,
		NRet:    0,
		Protect: true,
	})
	assert.Error(t, err, "expected registry overflow to surface as an error")
}

// TestNewStateRegistryUnboundedWhenZero verifies the factory treats
// RegistryMaxSize=0 (default) as "no cap configured" — many-value scripts
// complete without error. This is the legacy behavior.
func TestNewStateRegistryUnboundedWhenZero(t *testing.T) {
	factory := pluginlua.NewStateFactory() // no option — zero default
	L, err := factory.NewState(context.Background())
	require.NoError(t, err)
	defer L.Close()

	script := `
local t = {}
for i = 1, 5000 do
    t[#t + 1] = i
end
return #t
`
	err = L.DoString(script)
	assert.NoError(t, err, "5000 values should fit in the default registry")
}

func TestStateFactoryNewStateBlocksFilesystemFunctions(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	// These functions are in base library but should be blocked for sandboxing.
	// They allow reading/executing arbitrary files from the filesystem.
	unsafeFuncs := []string{"dofile", "loadfile", "loadstring", "load"}
	for _, fn := range unsafeFuncs {
		assert.Equal(t, "nil", L.GetGlobal(fn).Type().String(), "unsafe function %q should be blocked for sandboxing", fn)
	}
}
