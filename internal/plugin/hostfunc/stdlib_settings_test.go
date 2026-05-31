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

func TestGetSettingPlayerScopeFailsClosedUntilResolverLands(t *testing.T) {
	// iokti.16 contract: a player ULID never equals the acting character's
	// ULID, so a PLAYER-scope read with a player principal is denied until
	// iokti.19. Using the acting character's own ID as principal also fails
	// for PLAYER because the player partition keys differ — here we assert
	// the foreign-principal denial path that mirrors the binary host.
	L := lua.NewState()
	defer L.Close()
	charID := core.NewULID().String()
	L.SetContext(characterActorCtx(charID))

	ops := newFakeSettingsOps()
	hf := hostfunc.New(nil,
		hostfunc.WithEngine(policytest.AllowAllEngine()),
		hostfunc.WithSettingsOps(ops))
	hf.Register(L, "lua-plug")

	playerID := core.NewULID().String() // distinct from charID
	require.NoError(t, L.DoString(`
		vals, err = holomush.get_setting("player", "`+playerID+`", "content.cw_block")
	`))
	assert.Equal(t, lua.LNil, L.GetGlobal("vals"))
	assert.NotEqual(t, lua.LNil, L.GetGlobal("err"))
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
