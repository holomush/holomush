// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"context"
	"testing"

	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
)

func TestStateFactory_NewState_LoadsSafeLibraries(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	defer L.Close()

	// Should have base, table, string, math
	safeLibs := []string{"table", "string", "math"}
	for _, lib := range safeLibs {
		if L.GetGlobal(lib).Type().String() == "nil" {
			t.Errorf("library %q not loaded", lib)
		}
	}
}

func TestStateFactory_NewState_BlocksUnsafeLibraries(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	defer L.Close()

	// Should NOT have os, io, debug, package
	unsafeLibs := []string{"os", "io", "debug", "package"}
	for _, lib := range unsafeLibs {
		if L.GetGlobal(lib).Type().String() != "nil" {
			t.Errorf("unsafe library %q should not be loaded", lib)
		}
	}
}

func TestStateFactory_NewState_CanExecuteLua(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	defer L.Close()

	err = L.DoString(`result = 1 + 1`)
	if err != nil {
		t.Fatalf("DoString() error = %v", err)
	}

	result := L.GetGlobal("result")
	if result.String() != "2" {
		t.Errorf("result = %v, want 2", result)
	}
}

func TestStateFactory_NewState_CanUseStringLibrary(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	defer L.Close()

	err = L.DoString(`result = string.upper("hello")`)
	if err != nil {
		t.Fatalf("DoString() error = %v", err)
	}

	result := L.GetGlobal("result")
	if result.String() != "HELLO" {
		t.Errorf("result = %v, want HELLO", result)
	}
}

func TestStateFactory_NewState_CanUseTableLibrary(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	defer L.Close()

	err = L.DoString(`
		t = {3, 1, 2}
		table.sort(t)
		result = t[1]
	`)
	if err != nil {
		t.Fatalf("DoString() error = %v", err)
	}

	result := L.GetGlobal("result")
	if result.String() != "1" {
		t.Errorf("result = %v, want 1", result)
	}
}

func TestStateFactory_NewState_CanUseMathLibrary(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	defer L.Close()

	err = L.DoString(`result = math.abs(-42)`)
	if err != nil {
		t.Fatalf("DoString() error = %v", err)
	}

	result := L.GetGlobal("result")
	if result.String() != "42" {
		t.Errorf("result = %v, want 42", result)
	}
}

func TestStateFactory_NewState_StateClose(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}

	// Close should not panic
	L.Close()
}

func TestStateFactory_NewState_MultipleStates(t *testing.T) {
	factory := pluginlua.NewStateFactory()

	// Create multiple states - they should be independent
	L1, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() L1 error = %v", err)
	}
	defer L1.Close()

	L2, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() L2 error = %v", err)
	}
	defer L2.Close()

	// Set variable in L1
	if err := L1.DoString(`foo = "bar"`); err != nil {
		t.Fatalf("L1.DoString() error = %v", err)
	}

	// L2 should not have the variable
	if L2.GetGlobal("foo").Type().String() != "nil" {
		t.Error("states should be independent - L2 should not have L1's variable")
	}
}

func TestStateFactory_NewState_BlocksFilesystemFunctions(t *testing.T) {
	factory := pluginlua.NewStateFactory()
	L, err := factory.NewState(context.Background())
	if err != nil {
		t.Fatalf("NewState() error = %v", err)
	}
	defer L.Close()

	// These functions are in base library but should be blocked for sandboxing.
	// They allow reading/executing arbitrary files from the filesystem.
	unsafeFuncs := []string{"dofile", "loadfile", "loadstring", "load"}
	for _, fn := range unsafeFuncs {
		if L.GetGlobal(fn).Type().String() != "nil" {
			t.Errorf("unsafe function %q should be blocked for sandboxing", fn)
		}
	}
}
