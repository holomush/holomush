// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// newEmitRegistryTestState mints a Lua state with an isolated "holomush"
// module table on which holomush.register_emit_type is installed (via
// hostfunc.RegisterEmitTypeFuncs). The state is closed via t.Cleanup.
func newEmitRegistryTestState(t *testing.T, reg *hostfunc.LuaEmitRegistry) *lua.LState {
	t.Helper()
	L := lua.NewState()
	t.Cleanup(L.Close)
	mod := L.NewTable()
	hostfunc.RegisterEmitTypeFuncs(L, mod, reg)
	L.SetGlobal("holomush", mod)
	return L
}

// TestRegisterEmitType_LuaSemantics covers the holomush.register_emit_type
// Lua entry across single/duplicate/invalid-arg scenarios. Each case
// exercises the same call shape — minted state with capture hostfunc
// installed, run a Lua snippet, assert against reg.Types() + error.
func TestRegisterEmitType_LuaSemantics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		luaScript  string
		wantTypes  []string
		wantErr    bool
		assertNote string
	}{
		{
			name:      "single call accumulates the registered type",
			luaScript: `holomush.register_emit_type("alpha")`,
			wantTypes: []string{"alpha"},
		},
		{
			name: "duplicate registrations are idempotent",
			luaScript: `
holomush.register_emit_type("foo")
holomush.register_emit_type("foo")
holomush.register_emit_type("bar")
holomush.register_emit_type("foo")
`,
			wantTypes: []string{"bar", "foo"},
		},
		// gopher-lua's CheckString coerces numbers via lua's tostring semantics,
		// so passing 123 succeeds; only types Lua won't auto-coerce (nil,
		// boolean, table, function) raise.
		{
			name:       "non-string, non-coercible argument raises Lua error",
			luaScript:  `holomush.register_emit_type({not = "a string"})`,
			wantErr:    true,
			assertNote: "no type recorded when arg is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reg := hostfunc.NewLuaEmitRegistry()
			L := newEmitRegistryTestState(t, reg)

			err := L.DoString(tt.luaScript)
			if tt.wantErr {
				require.Error(t, err)
				require.Empty(t, reg.Types(), tt.assertNote)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantTypes, reg.Types())
		})
	}
}
