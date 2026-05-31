// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// fakeSettingsOps is an in-memory hostfunc.SettingsOps double. It records the
// scope/pluginName/principalID it was called with and stores list values keyed
// by (scope, pluginName, principalID, key) so round-trips work within a test.
type fakeSettingsOps struct {
	mu   sync.Mutex
	data map[string][]string
}

func newFakeSettingsOps() *fakeSettingsOps {
	return &fakeSettingsOps{data: map[string][]string{}}
}

func (f *fakeSettingsOps) key(scope pluginv1.SettingScope, pluginName, principalID, key string) string {
	return scope.String() + "|" + pluginName + "|" + principalID + "|" + key
}

func (f *fakeSettingsOps) GetSetting(
	_ context.Context, scope pluginv1.SettingScope, pluginName, principalID, key string,
) ([]string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.data[f.key(scope, pluginName, principalID, key)]
	return v, ok, nil
}

func (f *fakeSettingsOps) SetSetting(
	_ context.Context, scope pluginv1.SettingScope, pluginName, principalID, key string, values []string,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.data[f.key(scope, pluginName, principalID, key)] = values
	return nil
}

var _ hostfunc.SettingsOps = (*fakeSettingsOps)(nil)

func characterActorCtx(id string) context.Context {
	return core.WithActor(context.Background(), core.Actor{Kind: core.ActorCharacter, ID: id})
}

// characterActorCtxOwning stamps both the acting character actor AND the
// host-vouched owning player on the ctx — exactly as the command dispatcher does
// via core.WithActor + core.WithOwningPlayer. PLAYER-scope ownership compares
// req principal_id against this owner (holomush-iokti.19).
func characterActorCtxOwning(charID, owningPlayerID string) context.Context {
	return core.WithOwningPlayer(characterActorCtx(charID), owningPlayerID)
}

func TestGetSettingReturnsStoredListForOwnedCharacterPrincipal(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	require.NoError(t, ops.SetSetting(context.Background(),
		pluginv1.SettingScope_SETTING_SCOPE_CHARACTER, "lua-plug", charID,
		"content.cw_block", []string{"gore", "spiders"}))

	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		vals, found = holomush.get_setting("character", "`+charID+`", "content.cw_block")
	`))
	assert.True(t, bool(L.GetGlobal("found").(lua.LBool)))
	tbl, ok := L.GetGlobal("vals").(*lua.LTable)
	require.True(t, ok, "vals must be a table")
	assert.Equal(t, 2, tbl.Len())
	assert.Equal(t, "gore", tbl.RawGetInt(1).String())
	assert.Equal(t, "spiders", tbl.RawGetInt(2).String())
}

func TestSetSettingRoundTripsForOwnedCharacterPrincipal(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		ok, err = holomush.set_setting("character", "`+charID+`", "content.cw_block", {"gore", "nsfw"})
	`))
	assert.True(t, bool(L.GetGlobal("ok").(lua.LBool)))
	assert.Equal(t, lua.LNil, L.GetGlobal("err"))

	got, found, err := ops.GetSetting(context.Background(),
		pluginv1.SettingScope_SETTING_SCOPE_CHARACTER, "lua-plug", charID, "content.cw_block")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"gore", "nsfw"}, got)
}

func TestGetSettingDeniesForeignPrincipal(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	foreign := core.NewULID().String()
	require.NoError(t, L.DoString(`
		vals, err = holomush.get_setting("character", "`+foreign+`", "content.cw_block")
	`))
	assert.Equal(t, lua.LNil, L.GetGlobal("vals"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"foreign principal MUST be denied with a non-nil error")
}

func TestSetSettingDeniesForeignPrincipal(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	foreign := core.NewULID().String()
	require.NoError(t, L.DoString(`
		ok, err = holomush.set_setting("character", "`+foreign+`", "content.cw_block", {"x"})
	`))
	assert.NotEqual(t, lua.LTrue, L.GetGlobal("ok"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"))

	_, found, err := ops.GetSetting(context.Background(),
		pluginv1.SettingScope_SETTING_SCOPE_CHARACTER, "lua-plug", foreign, "content.cw_block")
	require.NoError(t, err)
	assert.False(t, found, "denied write MUST NOT reach the store")
}

func TestGetSettingPlayerScopeDeniedWhenNoOwningPlayerOnContext(t *testing.T) {
	// iokti.19 fail-closed: with only an actor on the ctx and NO owning player
	// stamped (characterActorCtx), a PLAYER-scope read is denied. This is the Lua
	// mirror of the binary "no host-vouched owning player on the token" denial.
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID)) // no owning player stamped

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	playerID := core.NewULID().String()
	require.NoError(t, L.DoString(`
		vals, err = holomush.get_setting("player", "`+playerID+`", "content.cw_block")
	`))
	assert.Equal(t, lua.LNil, L.GetGlobal("vals"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"))
}

// TestPlayerSettingRoundTripsForOwningPlayer is the iokti.19 Lua functional
// success test and the SYMMETRY counterpart of the binary
// TestPlayerSettingRoundTripsForOwningPlayer: when the ctx carries an owning
// player equal to the request's principal_id, PLAYER-scope set/get succeeds and
// round-trips. Both runtimes converge on the same shared ownership gate.
func TestPlayerSettingRoundTripsForOwningPlayer(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	owningPlayer := core.NewULID().String()
	L.SetContext(characterActorCtxOwning(charID, owningPlayer))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		ok, err = holomush.set_setting("player", "`+owningPlayer+`", "content.cw_block", {"gore", "nsfw"})
	`))
	assert.True(t, bool(L.GetGlobal("ok").(lua.LBool)))
	assert.Equal(t, lua.LNil, L.GetGlobal("err"))

	require.NoError(t, L.DoString(`
		vals, found = holomush.get_setting("player", "`+owningPlayer+`", "content.cw_block")
	`))
	assert.True(t, bool(L.GetGlobal("found").(lua.LBool)))
	tbl, ok := L.GetGlobal("vals").(*lua.LTable)
	require.True(t, ok, "vals must be a table")
	assert.Equal(t, 2, tbl.Len())
	assert.Equal(t, "gore", tbl.RawGetInt(1).String())
	assert.Equal(t, "nsfw", tbl.RawGetInt(2).String())

	// The owner partition is keyed by the player ULID, matching the binary path.
	got, found, err := ops.GetSetting(context.Background(),
		pluginv1.SettingScope_SETTING_SCOPE_PLAYER, "lua-plug", owningPlayer, "content.cw_block")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, []string{"gore", "nsfw"}, got)
}

// TestPlayerSettingDeniedWhenPrincipalNotOwningPlayer: even with an owning player
// on the ctx, a principal_id that does NOT equal it is denied (holomush-iokti.19).
func TestPlayerSettingDeniedWhenPrincipalNotOwningPlayer(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	owningPlayer := core.NewULID().String()
	otherPlayer := core.NewULID().String() // distinct from the vouched owner
	L.SetContext(characterActorCtxOwning(charID, owningPlayer))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		vals, err = holomush.get_setting("player", "`+otherPlayer+`", "content.cw_block")
	`))
	assert.Equal(t, lua.LNil, L.GetGlobal("vals"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"PLAYER principal_id MUST equal the ctx's owning player")
}

// TestSetSettingPlayerScopeDeniedWhenNoOwningPlayerOnContext is the Lua SET-path
// mirror of the binary TestPlayerScopeDeniedWhenNoOwningPlayerVouched and of the
// Lua GET-path TestGetSettingPlayerScopeDeniedWhenNoOwningPlayerOnContext: a
// PLAYER-scope WRITE with no owning player stamped on the ctx fails closed, and
// the denied write never reaches the store. Closes the runtime-symmetry test gap
// (INV-8, holomush-sl0ir.13): the gate is shared (set_setting → resolveSettingsAccess
// → CheckPrincipalOwnership), but the write path lacked an explicit Lua mirror.
func TestSetSettingPlayerScopeDeniedWhenNoOwningPlayerOnContext(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID)) // no owning player stamped

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	playerID := core.NewULID().String()
	require.NoError(t, L.DoString(`
		ok, err = holomush.set_setting("player", "`+playerID+`", "content.cw_block", {"gore"})
	`))
	assert.NotEqual(t, lua.LTrue, L.GetGlobal("ok"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"PLAYER-scope write with no owning player on ctx MUST fail closed")

	_, found, err := ops.GetSetting(context.Background(),
		pluginv1.SettingScope_SETTING_SCOPE_PLAYER, "lua-plug", playerID, "content.cw_block")
	require.NoError(t, err)
	assert.False(t, found, "denied write MUST NOT reach the store")
}

// TestSetSettingPlayerScopeDeniedWhenPrincipalNotOwningPlayer is the Lua SET-path
// mirror of the binary TestSetSettingPlayerForeignPrincipalDenied: even with an
// owning player on the ctx, a PLAYER-scope WRITE whose principal_id does NOT
// equal the vouched owner is denied and never reaches the store
// (holomush-sl0ir.13 / INV-8).
func TestSetSettingPlayerScopeDeniedWhenPrincipalNotOwningPlayer(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	owningPlayer := core.NewULID().String()
	otherPlayer := core.NewULID().String() // distinct from the vouched owner
	L.SetContext(characterActorCtxOwning(charID, owningPlayer))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		ok, err = holomush.set_setting("player", "`+otherPlayer+`", "content.cw_block", {"gore"})
	`))
	assert.NotEqual(t, lua.LTrue, L.GetGlobal("ok"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"PLAYER-scope write principal_id MUST equal the ctx's owning player")

	_, found, err := ops.GetSetting(context.Background(),
		pluginv1.SettingScope_SETTING_SCOPE_PLAYER, "lua-plug", otherPlayer, "content.cw_block")
	require.NoError(t, err)
	assert.False(t, found, "denied write MUST NOT reach the store")
}

func TestGetSettingGameScopeNeedsNoPrincipalOwnership(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	require.NoError(t, ops.SetSetting(context.Background(),
		pluginv1.SettingScope_SETTING_SCOPE_GAME, "lua-plug", "",
		"content.global_cw", []string{"flashing"}))

	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	// principal_id is ignored for GAME scope; pass empty.
	require.NoError(t, L.DoString(`
		vals, found = holomush.get_setting("game", "", "content.global_cw")
	`))
	assert.True(t, bool(L.GetGlobal("found").(lua.LBool)))
	tbl, ok := L.GetGlobal("vals").(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, "flashing", tbl.RawGetInt(1).String())
}

func TestSetSettingGameScopeDeniedWithoutOperatorAuthz(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	// DenyAllEngine → the GAME-write operator authorization is refused,
	// mirroring authorizeGameWrite on the binary path.
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.DenyAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		ok, err = holomush.set_setting("game", "", "content.global_cw", {"x"})
	`))
	assert.NotEqual(t, lua.LTrue, L.GetGlobal("ok"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"))
}

func TestSetSettingGameScopeAllowedWithOperatorAuthz(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		ok, err = holomush.set_setting("game", "", "content.global_cw", {"flashing"})
	`))
	assert.True(t, bool(L.GetGlobal("ok").(lua.LBool)))
	assert.Equal(t, lua.LNil, L.GetGlobal("err"))
}

func TestGetSettingNoActorFailsClosed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	L.SetContext(context.Background()) // no actor

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	charID := core.NewULID().String()
	require.NoError(t, L.DoString(`
		vals, err = holomush.get_setting("character", "`+charID+`", "content.cw_block")
	`))
	assert.Equal(t, lua.LNil, L.GetGlobal("vals"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"missing actor MUST fail closed")
}

func TestGetSettingNilOpsFailsClosed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	// No WithSettingsOps — ops is nil.
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		vals, err = holomush.get_setting("character", "`+charID+`", "content.cw_block")
	`))
	assert.Equal(t, lua.LNil, L.GetGlobal("vals"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"nil settings ops MUST fail closed")
}

func TestGetSettingUnknownScopeFailsClosed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	require.NoError(t, L.DoString(`
		vals, err = holomush.get_setting("bogus", "x", "content.cw_block")
	`))
	assert.Equal(t, lua.LNil, L.GetGlobal("vals"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"),
		"unknown scope MUST fail closed")
}
