// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// =============================================================================
// AliasCapability — compile-time interface satisfaction
// =============================================================================

var _ hostfunc.Capability = (*hostfunc.AliasCapability)(nil)

// =============================================================================
// mockAliasAccess — test double for AliasAccess
// =============================================================================

type mockAliasAccess struct {
	// player alias
	setPlayerErr    error
	deletePlayerErr error
	listPlayerErr   error
	listPlayerRet   []hostfunc.AliasEntry

	// system alias
	setSystemErr    error
	deleteSystemErr error
	listSystemErr   error
	listSystemRet   []hostfunc.AliasEntry

	// shadow check
	shadowRet     bool
	shadowCmd     string
	shadowErr     error

	// call capture
	setPlayerCalls    []aliasPlayerCall
	deletePlayerCalls []aliasDeleteCall
	setSystemCalls    []aliasSystemCall
	deleteSystemCalls []string
	shadowCalls       []string
}

type aliasPlayerCall struct {
	PlayerID string
	Alias    string
	Command  string
}

type aliasDeleteCall struct {
	PlayerID string
	Alias    string
}

type aliasSystemCall struct {
	Alias     string
	Command   string
	CreatedBy string
}

func (m *mockAliasAccess) SetPlayerAlias(_ context.Context, playerID, alias, command string) error {
	m.setPlayerCalls = append(m.setPlayerCalls, aliasPlayerCall{PlayerID: playerID, Alias: alias, Command: command})
	return m.setPlayerErr
}

func (m *mockAliasAccess) DeletePlayerAlias(_ context.Context, playerID, alias string) error {
	m.deletePlayerCalls = append(m.deletePlayerCalls, aliasDeleteCall{PlayerID: playerID, Alias: alias})
	return m.deletePlayerErr
}

func (m *mockAliasAccess) ListPlayerAliases(_ context.Context, _ string) ([]hostfunc.AliasEntry, error) {
	return m.listPlayerRet, m.listPlayerErr
}

func (m *mockAliasAccess) CheckAliasShadow(_ context.Context, alias string) (bool, string, error) {
	m.shadowCalls = append(m.shadowCalls, alias)
	return m.shadowRet, m.shadowCmd, m.shadowErr
}

func (m *mockAliasAccess) SetSystemAlias(_ context.Context, alias, command, createdBy string) error {
	m.setSystemCalls = append(m.setSystemCalls, aliasSystemCall{Alias: alias, Command: command, CreatedBy: createdBy})
	return m.setSystemErr
}

func (m *mockAliasAccess) DeleteSystemAlias(_ context.Context, alias string) error {
	m.deleteSystemCalls = append(m.deleteSystemCalls, alias)
	return m.deleteSystemErr
}

func (m *mockAliasAccess) ListSystemAliases(_ context.Context) ([]hostfunc.AliasEntry, error) {
	return m.listSystemRet, m.listSystemErr
}

// =============================================================================
// helpers
// =============================================================================

func newCapAliasState(t *testing.T, aa hostfunc.AliasAccess) *lua.LState {
	t.Helper()
	cap := hostfunc.NewAliasCapability(aa)
	L := lua.NewState()
	t.Cleanup(func() { L.Close() })
	cap.Register(L, "test-plugin")
	return L
}

// =============================================================================
// Namespace
// =============================================================================

func TestAliasCapabilityNamespaceReturnsAlias(t *testing.T) {
	cap := hostfunc.NewAliasCapability(nil)
	assert.Equal(t, "alias", cap.Namespace())
}

// =============================================================================
// Register — table structure
// =============================================================================

func TestAliasCapabilityRegisterCreatesFunctionTable(t *testing.T) {
	aa := &mockAliasAccess{}
	L := newCapAliasState(t, aa)

	tbl := L.GetGlobal("alias")
	require.Equal(t, lua.LTTable, tbl.Type(), "alias global should be a table")

	for _, fn := range []string{
		"set_player", "delete_player", "list_player",
		"check_shadow",
		"set_system", "delete_system", "list_system",
	} {
		field := L.GetField(tbl.(*lua.LTable), fn)
		assert.Equal(t, lua.LTFunction, field.Type(), "alias.%s should be a function", fn)
	}
}

// =============================================================================
// alias.set_player
// =============================================================================

func TestAliasCapabilitySetPlayerCallsService(t *testing.T) {
	aa := &mockAliasAccess{}
	L := newCapAliasState(t, aa)

	err := L.DoString(`alias.set_player("player-1", "go", "move north")`)
	require.NoError(t, err)

	require.Len(t, aa.setPlayerCalls, 1)
	assert.Equal(t, "player-1", aa.setPlayerCalls[0].PlayerID)
	assert.Equal(t, "go", aa.setPlayerCalls[0].Alias)
	assert.Equal(t, "move north", aa.setPlayerCalls[0].Command)
}

func TestAliasCapabilitySetPlayerReturnsErrorOnFailure(t *testing.T) {
	aa := &mockAliasAccess{setPlayerErr: errors.New("store failure")}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result, set_err = alias.set_player("player-1", "go", "move north")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("set_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// alias.delete_player
// =============================================================================

func TestAliasCapabilityDeletePlayerCallsService(t *testing.T) {
	aa := &mockAliasAccess{}
	L := newCapAliasState(t, aa)

	err := L.DoString(`alias.delete_player("player-1", "go")`)
	require.NoError(t, err)

	require.Len(t, aa.deletePlayerCalls, 1)
	assert.Equal(t, "player-1", aa.deletePlayerCalls[0].PlayerID)
	assert.Equal(t, "go", aa.deletePlayerCalls[0].Alias)
}

func TestAliasCapabilityDeletePlayerReturnsErrorOnFailure(t *testing.T) {
	aa := &mockAliasAccess{deletePlayerErr: errors.New("delete failed")}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result, del_err = alias.delete_player("player-1", "go")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("del_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// alias.list_player
// =============================================================================

func TestAliasCapabilityListPlayerReturnsTable(t *testing.T) {
	aa := &mockAliasAccess{
		listPlayerRet: []hostfunc.AliasEntry{
			{Alias: "go", Command: "move north"},
			{Alias: "l", Command: "look"},
		},
	}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result = alias.list_player("player-1")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 2, result.(*lua.LTable).Len())
}

func TestAliasCapabilityListPlayerReturnsEmptyTableWhenNone(t *testing.T) {
	aa := &mockAliasAccess{listPlayerRet: nil}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result = alias.list_player("player-1")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 0, result.(*lua.LTable).Len())
}

func TestAliasCapabilityListPlayerReturnsNilAndErrorOnFailure(t *testing.T) {
	aa := &mockAliasAccess{listPlayerErr: errors.New("list error")}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result, list_err = alias.list_player("player-1")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("list_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// alias.check_shadow
// =============================================================================

func TestAliasCapabilityCheckShadowReturnsTrueWhenShadowed(t *testing.T) {
	aa := &mockAliasAccess{shadowRet: true, shadowCmd: "look"}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result = alias.check_shadow("l")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	tbl := result.(*lua.LTable)
	assert.Equal(t, lua.LTrue, tbl.RawGetString("shadows"))
	assert.Equal(t, "look", tbl.RawGetString("command").String())
}

func TestAliasCapabilityCheckShadowReturnsFalseWhenNotShadowed(t *testing.T) {
	aa := &mockAliasAccess{shadowRet: false, shadowCmd: ""}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result = alias.check_shadow("mymove")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	tbl := result.(*lua.LTable)
	assert.Equal(t, lua.LFalse, tbl.RawGetString("shadows"))
}

func TestAliasCapabilityCheckShadowCallsServiceWithAlias(t *testing.T) {
	aa := &mockAliasAccess{}
	L := newCapAliasState(t, aa)

	err := L.DoString(`alias.check_shadow("go")`)
	require.NoError(t, err)

	require.Len(t, aa.shadowCalls, 1)
	assert.Equal(t, "go", aa.shadowCalls[0])
}

func TestAliasCapabilityCheckShadowReturnsNilAndErrorOnFailure(t *testing.T) {
	aa := &mockAliasAccess{shadowErr: errors.New("shadow check failed")}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result, shadow_err = alias.check_shadow("go")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("shadow_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// alias.set_system
// =============================================================================

func TestAliasCapabilitySetSystemCallsService(t *testing.T) {
	aa := &mockAliasAccess{}
	L := newCapAliasState(t, aa)

	err := L.DoString(`alias.set_system("n", "move north", "admin")`)
	require.NoError(t, err)

	require.Len(t, aa.setSystemCalls, 1)
	assert.Equal(t, "n", aa.setSystemCalls[0].Alias)
	assert.Equal(t, "move north", aa.setSystemCalls[0].Command)
	assert.Equal(t, "admin", aa.setSystemCalls[0].CreatedBy)
}

func TestAliasCapabilitySetSystemReturnsErrorOnFailure(t *testing.T) {
	aa := &mockAliasAccess{setSystemErr: errors.New("store failure")}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result, set_err = alias.set_system("n", "move north", "admin")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("set_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// alias.delete_system
// =============================================================================

func TestAliasCapabilityDeleteSystemCallsService(t *testing.T) {
	aa := &mockAliasAccess{}
	L := newCapAliasState(t, aa)

	err := L.DoString(`alias.delete_system("n")`)
	require.NoError(t, err)

	require.Len(t, aa.deleteSystemCalls, 1)
	assert.Equal(t, "n", aa.deleteSystemCalls[0])
}

func TestAliasCapabilityDeleteSystemReturnsErrorOnFailure(t *testing.T) {
	aa := &mockAliasAccess{deleteSystemErr: errors.New("delete failed")}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result, del_err = alias.delete_system("n")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("del_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// alias.list_system
// =============================================================================

func TestAliasCapabilityListSystemReturnsTable(t *testing.T) {
	aa := &mockAliasAccess{
		listSystemRet: []hostfunc.AliasEntry{
			{Alias: "n", Command: "move north"},
			{Alias: "s", Command: "move south"},
			{Alias: "e", Command: "move east"},
		},
	}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result = alias.list_system()`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 3, result.(*lua.LTable).Len())
}

func TestAliasCapabilityListSystemReturnsEmptyTableWhenNone(t *testing.T) {
	aa := &mockAliasAccess{listSystemRet: nil}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result = alias.list_system()`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 0, result.(*lua.LTable).Len())
}

func TestAliasCapabilityListSystemReturnsNilAndErrorOnFailure(t *testing.T) {
	aa := &mockAliasAccess{listSystemErr: errors.New("list error")}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result, list_err = alias.list_system()`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("list_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// alias entry table shape
// =============================================================================

func TestAliasCapabilityListPlayerEntryHasAliasAndCommandFields(t *testing.T) {
	aa := &mockAliasAccess{
		listPlayerRet: []hostfunc.AliasEntry{
			{Alias: "go", Command: "move north"},
		},
	}
	L := newCapAliasState(t, aa)

	err := L.DoString(`result = alias.list_player("player-1"); entry = result[1]`)
	require.NoError(t, err)

	entry := L.GetGlobal("entry")
	require.Equal(t, lua.LTTable, entry.Type())
	tbl := entry.(*lua.LTable)
	assert.Equal(t, "go", tbl.RawGetString("alias").String())
	assert.Equal(t, "move north", tbl.RawGetString("command").String())
}
