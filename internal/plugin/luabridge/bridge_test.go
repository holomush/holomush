// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package luabridge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/luabridge"
)

// TestBridgeRegisterHostCaps asserts the declaration gate (INV-PLUGIN-44/45):
// RegisterHostCaps injects a Lua global only for each token the plugin declared
// in its manifest capability requirements, and never for undeclared, unknown, or
// (in the empty case) any token — regardless of what the host supports. A nil
// conn is fine: registration dials no RPC.
func TestBridgeRegisterHostCaps(t *testing.T) {
	tests := []struct {
		name       string
		pluginName string
		declared   []string
		wantTable  []string // tokens that must be injected as a Lua table
		wantNil    []string // tokens that must remain absent (nil)
	}{
		{
			name:       "injects only the declared cap",
			pluginName: "echo-bot",
			declared:   []string{"kv"},
			wantTable:  []string{"kv"},
			wantNil:    []string{"session", "audit"},
		},
		{
			name:       "unknown token creates no global",
			pluginName: "echo-bot",
			declared:   []string{"nonexistent-token"},
			wantNil:    []string{"nonexistent-token"},
		},
		{
			name:       "empty declared caps injects nothing",
			pluginName: "echo-bot",
			declared:   []string{},
			wantNil:    []string{"kv", "session", "audit", "eval", "emit", "focus"},
		},
		{
			name:       "injects every declared bound token",
			pluginName: "core-scenes",
			declared:   []string{"kv", "eval", "focus"},
			wantTable:  []string{"kv", "eval", "focus"},
			wantNil:    []string{"session", "property"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			luabridge.RegisterHostCaps(L, nil, tc.pluginName, tc.declared)

			for _, tok := range tc.wantTable {
				assert.Equal(t, lua.LTTable, L.GetGlobal(tok).Type(),
					"declared %q must be injected as a table", tok)
			}
			for _, tok := range tc.wantNil {
				assert.Equal(t, lua.LTNil, L.GetGlobal(tok).Type(),
					"%q must not be injected", tok)
			}
		})
	}
}

// TestBridgeNoDoubleInjectPreExistingGlobal asserts the defensive no-clobber
// behaviour: if a global is already set (e.g. by the legacy hostfunc
// cap_session.go path), RegisterHostCaps MUST NOT overwrite it. The original
// value must survive unchanged after RegisterHostCaps is called with the
// colliding token.
//
// This guards the coexistence invariant (spec §5): legacy-shim globals set by
// hostfunc.Functions.Register are not silently replaced by bridge globals for
// the same token.
func TestBridgeNoDoubleInjectPreExistingGlobal(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Simulate the legacy cap_session.go path setting the "session" global
	// before the bridge runs.
	legacyTbl := L.NewTable()
	L.SetField(legacyTbl, "legacy_marker", lua.LString("from_legacy"))
	L.SetGlobal("session", legacyTbl)

	// Bridge is called with "session" declared — should NOT clobber.
	luabridge.RegisterHostCaps(L, nil, "echo-bot", []string{"session"})

	// The global must still be the legacy table.
	got := L.GetGlobal("session")
	tbl, ok := got.(*lua.LTable)
	assert.True(t, ok, "session global must still be a table after RegisterHostCaps")
	assert.Equal(t, lua.LTString, L.GetField(tbl, "legacy_marker").Type(),
		"legacy_marker field must be preserved; bridge must not have overwritten the global")
	assert.Equal(t, "from_legacy", L.GetField(tbl, "legacy_marker").String(),
		"legacy table value must be unchanged")
}
