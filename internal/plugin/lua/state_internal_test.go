// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua

import (
	"context"
	"strings"
	"testing"

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
	if err == nil {
		t.Fatal("expected error when library fails to load")
	}

	if !strings.Contains(err.Error(), "failed to open library failing-lib") {
		t.Errorf("error = %q, want error containing 'failed to open library failing-lib'", err)
	}
}

// TestDefaultSafeLibraries verifies the default library list.
func TestDefaultSafeLibraries(t *testing.T) {
	libs := defaultSafeLibraries()

	if len(libs) != 4 {
		t.Errorf("defaultSafeLibraries() returned %d libraries, want 4", len(libs))
	}

	expectedNames := map[string]bool{
		luavm.BaseLibName:   false,
		luavm.TabLibName:    false,
		luavm.StringLibName: false,
		luavm.MathLibName:   false,
	}

	for _, lib := range libs {
		if _, ok := expectedNames[lib.name]; !ok {
			t.Errorf("unexpected library %q in safe libraries", lib.name)
		}
		expectedNames[lib.name] = true
	}

	for name, found := range expectedNames {
		if !found {
			t.Errorf("expected library %q not in safe libraries", name)
		}
	}
}
