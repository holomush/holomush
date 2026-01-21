// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	luavm "github.com/yuin/gopher-lua"
)

// TestNewState_LibraryLoadError tests the error path when a library fails to load.
func TestNewState_LibraryLoadError(t *testing.T) {
	// Create a library loader that always errors
	failingLoader := func(L *luavm.LState) int {
		L.RaiseError("simulated library load failure")
		return 0
	}

	factory := &StateFactory{
		libraries: []safeLibrary{
			{"failing-lib", failingLoader},
		},
	}

	_, err := factory.NewState(context.Background())
	require.Error(t, err, "expected error when library fails to load")
	// oops.Error() returns the underlying Lua error message
	assert.Contains(t, err.Error(), "simulated library load failure",
		"error should contain underlying Lua error message")
}

// TestDefaultSafeLibraries verifies the default library list.
func TestDefaultSafeLibraries(t *testing.T) {
	libs := defaultSafeLibraries()

	assert.Len(t, libs, 4, "defaultSafeLibraries() returned wrong number of libraries")

	expectedNames := map[string]bool{
		luavm.BaseLibName:   false,
		luavm.TabLibName:    false,
		luavm.StringLibName: false,
		luavm.MathLibName:   false,
	}

	for _, lib := range libs {
		_, ok := expectedNames[lib.name]
		assert.True(t, ok, "unexpected library %q in safe libraries", lib.name)
		expectedNames[lib.name] = true
	}

	for name, found := range expectedNames {
		assert.True(t, found, "expected library %q not in safe libraries", name)
	}
}
