// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package luabridge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/luabridge"
)

// TestBridgeRegistersDeclaredHostCapsOnly asserts that RegisterHostCaps injects
// only the globals for tokens the plugin declared in its manifest capability
// requirements. A plugin declaring only "kv" gets the kv global, NOT session.
// This is the declaration gate (INV-PLUGIN-44/45): undeclared caps are never
// injected regardless of what the host supports.
func TestBridgeRegistersDeclaredHostCapsOnly(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// nil conn is fine for registration — no RPC is actually dialed here.
	luabridge.RegisterHostCaps(L, nil, "echo-bot", []string{"kv"})

	assert.Equal(t, lua.LTTable, L.GetGlobal("kv").Type(), "declared 'kv' cap must be injected as a table")
	assert.Equal(t, lua.LTNil, L.GetGlobal("session").Type(), "undeclared 'session' must not be injected")
	assert.Equal(t, lua.LTNil, L.GetGlobal("audit").Type(), "undeclared 'audit' must not be injected")
}

// TestBridgeSkipsOptedOutPlugin asserts that calling RegisterHostCaps with an
// empty declared-capabilities slice injects no bridge globals. This covers the
// "opted-out plugin gets nothing" case (production plugins with no capability:
// declarations are unaffected by the bridge).
func TestBridgeSkipsTokensWithNoBinding(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Declare an unknown token (no entry in registeredHostCapBindings).
	luabridge.RegisterHostCaps(L, nil, "echo-bot", []string{"nonexistent-token"})

	// The unknown token must not panic and must not create any stray globals.
	assert.Equal(t, lua.LTNil, L.GetGlobal("nonexistent-token").Type(), "unknown token must not create a global")
}

// TestBridgeEmptyDeclaredCapsInjectsNothing asserts that a plugin declaring no
// capabilities (empty slice) receives no bridge globals — the opted-out case.
func TestBridgeEmptyDeclaredCapsInjectsNothing(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	luabridge.RegisterHostCaps(L, nil, "echo-bot", []string{})

	// None of the known tokens should appear.
	for _, token := range []string{"kv", "session", "audit", "eval", "emit", "focus"} {
		assert.Equal(t, lua.LTNil, L.GetGlobal(token).Type(),
			"opted-out plugin must not have global %q", token)
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

// TestBridgeMultipleTokensInjectedCorrectly asserts that when a plugin declares
// multiple capability tokens, all of them (that have bindings) are injected.
func TestBridgeMultipleTokensInjectedCorrectly(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	luabridge.RegisterHostCaps(L, nil, "core-scenes", []string{"kv", "eval", "focus"})

	assert.Equal(t, lua.LTTable, L.GetGlobal("kv").Type(), "kv must be injected")
	assert.Equal(t, lua.LTTable, L.GetGlobal("eval").Type(), "eval must be injected")
	assert.Equal(t, lua.LTTable, L.GetGlobal("focus").Type(), "focus must be injected")

	// Undeclared tokens stay nil.
	assert.Equal(t, lua.LTNil, L.GetGlobal("session").Type(), "undeclared session must not be injected")
	assert.Equal(t, lua.LTNil, L.GetGlobal("property").Type(), "undeclared property must not be injected")
}
