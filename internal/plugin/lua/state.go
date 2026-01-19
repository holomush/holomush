// Package lua provides a sandboxed Lua runtime for plugin execution.
package lua

import (
	"context"
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// safeLibrary represents a Lua library that is safe to load in sandboxed state.
type safeLibrary struct {
	name string
	fn   lua.LGFunction
}

// defaultSafeLibraries returns the list of libraries safe to load.
// Safe: base, table, string, math.
// Blocked: os, io, debug, package.
func defaultSafeLibraries() []safeLibrary {
	return []safeLibrary{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	}
}

// StateFactory creates sandboxed Lua states with only safe libraries.
type StateFactory struct {
	// libraries allows overriding the default safe libraries for testing.
	libraries []safeLibrary
}

// NewStateFactory creates a new state factory.
func NewStateFactory() *StateFactory {
	return &StateFactory{
		libraries: defaultSafeLibraries(),
	}
}

// NewState creates a fresh Lua state with only safe libraries loaded.
// Safe libraries: base, table, string, math.
// Blocked libraries: os, io, debug, package.
func (f *StateFactory) NewState(_ context.Context) (*lua.LState, error) {
	L := lua.NewState(lua.Options{
		SkipOpenLibs: true, // Don't load any libraries by default
	})

	for _, lib := range f.libraries {
		if err := L.CallByParam(lua.P{
			Fn:      L.NewFunction(lib.fn),
			NRet:    0,
			Protect: true,
		}, lua.LString(lib.name)); err != nil {
			L.Close()
			return nil, fmt.Errorf("failed to open library %s: %w", lib.name, err)
		}
	}

	return L, nil
}
