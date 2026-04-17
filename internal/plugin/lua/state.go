// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package lua provides a sandboxed Lua runtime for plugin execution.
package lua

import (
	"context"

	"github.com/samber/oops"
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
	// registryMaxSize bounds the Lua value registry per state. Zero means
	// "use gopher-lua default" (unbounded growth).
	registryMaxSize int
}

// StateFactoryOption customizes StateFactory construction.
type StateFactoryOption func(*StateFactory)

// WithRegistryMaxSize sets the upper bound on the Lua value registry per
// state. Overflow causes gopher-lua to panic; CallByParam(Protect=true)
// catches it and returns an error. Zero disables the cap.
func WithRegistryMaxSize(n int) StateFactoryOption {
	return func(f *StateFactory) { f.registryMaxSize = n }
}

// NewStateFactory creates a new state factory.
func NewStateFactory(opts ...StateFactoryOption) *StateFactory {
	f := &StateFactory{
		libraries: defaultSafeLibraries(),
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// unsafeBaseFunctions lists base library functions that must be blocked for security.
// dofile, loadfile: filesystem access breaks sandboxing.
// loadstring, load: dynamic code execution from strings.
var unsafeBaseFunctions = []string{"dofile", "loadfile", "loadstring", "load"}

// NewState creates a fresh Lua state with only safe libraries loaded.
// Safe libraries: base, table, string, math.
// Blocked libraries: os, io, debug, package.
// Blocked base functions: dofile, loadfile (filesystem), loadstring, load (dynamic code).
//
// The ctx parameter is reserved for future cancellation/timeout support.
func (f *StateFactory) NewState(_ context.Context) (*lua.LState, error) {
	L := lua.NewState(lua.Options{
		SkipOpenLibs:    true, // Don't load any libraries by default
		RegistryMaxSize: f.registryMaxSize,
	})

	for _, lib := range f.libraries {
		if err := L.CallByParam(lua.P{
			Fn:      L.NewFunction(lib.fn),
			NRet:    0,
			Protect: true,
		}, lua.LString(lib.name)); err != nil {
			L.Close()
			return nil, oops.In("lua").With("library", lib.name).Hint("failed to open library").Wrap(err)
		}
	}

	// Block unsafe functions from base library that allow filesystem access.
	for _, fn := range unsafeBaseFunctions {
		L.SetGlobal(fn, lua.LNil)
	}

	return L, nil
}
