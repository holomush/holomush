// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)

func TestStateFactory_NewState_LoadsSafeLibraries(t *testing.T) {
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

func TestStateFactory_NewState_BlocksUnsafeLibraries(t *testing.T) {
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

func TestStateFactory_NewState_CanExecuteLua(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	err = L.DoString(`result = 1 + 1`)
	require.NoError(t, err, "DoString() failed")

	result := L.GetGlobal("result")
	assert.Equal(t, "2", result.String())
}

func TestStateFactory_NewState_CanUseStringLibrary(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	err = L.DoString(`result = string.upper("hello")`)
	require.NoError(t, err, "DoString() failed")

	result := L.GetGlobal("result")
	assert.Equal(t, "HELLO", result.String())
}

func TestStateFactory_NewState_CanUseTableLibrary(t *testing.T) {
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

func TestStateFactory_NewState_CanUseMathLibrary(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")
	defer L.Close()

	err = L.DoString(`result = math.abs(-42)`)
	require.NoError(t, err, "DoString() failed")

	result := L.GetGlobal("result")
	assert.Equal(t, "42", result.String())
}

func TestStateFactory_NewState_StateClose(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	require.NoError(t, err, "NewState() failed")

	// Close should not panic
	L.Close()
}

func TestStateFactory_NewState_MultipleStates(t *testing.T) {
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

func TestStateFactory_NewState_BlocksFilesystemFunctions(t *testing.T) {
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
