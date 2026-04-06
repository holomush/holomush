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
// WorldQueryCapability — compile-time interface satisfaction
// =============================================================================

var _ hostfunc.Capability = (*hostfunc.WorldQueryCapability)(nil)

// =============================================================================
// mockWorldQueryAccess — test double for WorldQueryAccess
// =============================================================================

type mockWorldQueryAccess struct {
	objectsResult    []hostfunc.ObjectResult
	objectsErr       error
	charactersResult []hostfunc.CharacterResult
	charactersErr    error

	objectsCalls    []worldQueryCall
	charactersCalls []worldQueryCall
}

type worldQueryCall struct {
	SubjectID  string
	LocationID string
}

func (m *mockWorldQueryAccess) GetObjectsByLocation(_ context.Context, subjectID, locationID string) ([]hostfunc.ObjectResult, error) {
	m.objectsCalls = append(m.objectsCalls, worldQueryCall{SubjectID: subjectID, LocationID: locationID})
	return m.objectsResult, m.objectsErr
}

func (m *mockWorldQueryAccess) GetCharactersByLocation(_ context.Context, subjectID, locationID string) ([]hostfunc.CharacterResult, error) {
	m.charactersCalls = append(m.charactersCalls, worldQueryCall{SubjectID: subjectID, LocationID: locationID})
	return m.charactersResult, m.charactersErr
}

// =============================================================================
// helpers
// =============================================================================

func newCapWorldQueryState(t *testing.T, wq hostfunc.WorldQueryAccess) *lua.LState {
	t.Helper()
	worldCap := hostfunc.NewWorldQueryCapability(wq)
	L := lua.NewState()
	t.Cleanup(func() { L.Close() })
	worldCap.Register(L, "test-plugin")
	return L
}

// =============================================================================
// Namespace
// =============================================================================

func TestWorldQueryCapabilityNamespaceReturnsWorldExt(t *testing.T) {
	worldCap := hostfunc.NewWorldQueryCapability(nil)
	assert.Equal(t, "world_ext", worldCap.Namespace())
}

// =============================================================================
// Register — table structure
// =============================================================================

func TestWorldQueryCapabilityRegisterCreatesFunctionTable(t *testing.T) {
	wq := &mockWorldQueryAccess{}
	L := newCapWorldQueryState(t, wq)

	tbl := L.GetGlobal("world_ext")
	require.Equal(t, lua.LTTable, tbl.Type(), "world_ext global should be a table")

	for _, fn := range []string{"get_objects_by_location", "get_characters_by_location"} {
		field := L.GetField(tbl.(*lua.LTable), fn)
		assert.Equal(t, lua.LTFunction, field.Type(), "world_ext.%s should be a function", fn)
	}
}

// =============================================================================
// world_ext.get_objects_by_location
// =============================================================================

func TestWorldQueryGetObjectsByLocationReturnsArrayTable(t *testing.T) {
	wq := &mockWorldQueryAccess{
		objectsResult: []hostfunc.ObjectResult{
			{ID: testLocID1, Name: "Sword", Description: "A sharp blade", LocationID: testLocID1, OwnerID: testCharID1},
			{ID: "01BX5ZZKBKACTAV9WEVGEMMVS3", Name: "Shield", Description: "A sturdy shield", LocationID: testLocID1, OwnerID: testCharID2},
		},
	}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`result = world_ext.get_objects_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	tbl := result.(*lua.LTable)
	assert.Equal(t, 2, tbl.Len())
}

func TestWorldQueryGetObjectsByLocationReturnsCorrectFields(t *testing.T) {
	wq := &mockWorldQueryAccess{
		objectsResult: []hostfunc.ObjectResult{
			{ID: testLocID1, Name: "Torch", Description: "Burns bright", LocationID: testLocID1, OwnerID: testCharID1},
		},
	}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`result = world_ext.get_objects_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	row := L.GetGlobal("result").(*lua.LTable).RawGetInt(1).(*lua.LTable)
	assert.Equal(t, testLocID1, row.RawGetString("id").String())
	assert.Equal(t, "Torch", row.RawGetString("name").String())
	assert.Equal(t, "Burns bright", row.RawGetString("description").String())
	assert.Equal(t, testLocID1, row.RawGetString("location_id").String())
	assert.Equal(t, testCharID1, row.RawGetString("owner_id").String())
}

func TestWorldQueryGetObjectsByLocationReturnsEmptyTableWhenNone(t *testing.T) {
	wq := &mockWorldQueryAccess{objectsResult: nil}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`result = world_ext.get_objects_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 0, result.(*lua.LTable).Len())
}

func TestWorldQueryGetObjectsByLocationReturnsNilAndErrorOnFailure(t *testing.T) {
	wq := &mockWorldQueryAccess{objectsErr: errors.New("store failure")}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`result, q_err = world_ext.get_objects_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("q_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

func TestWorldQueryGetObjectsByLocationPassesSubjectAndLocationIDs(t *testing.T) {
	wq := &mockWorldQueryAccess{}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`world_ext.get_objects_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	require.Len(t, wq.objectsCalls, 1)
	assert.Equal(t, testCharID1, wq.objectsCalls[0].SubjectID)
	assert.Equal(t, testLocID1, wq.objectsCalls[0].LocationID)
}

// =============================================================================
// world_ext.get_characters_by_location
// =============================================================================

func TestWorldQueryGetCharactersByLocationReturnsArrayTable(t *testing.T) {
	wq := &mockWorldQueryAccess{
		charactersResult: []hostfunc.CharacterResult{
			{ID: testCharID1, PlayerID: testLocID1, Name: "Alice", Description: "A brave hero", LocationID: testLocID1},
			{ID: testCharID2, PlayerID: "01BX5ZZKBKACTAV9WEVGEMMVS4", Name: "Bob", Description: "A cunning rogue", LocationID: testLocID1},
		},
	}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`result = world_ext.get_characters_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	tbl := result.(*lua.LTable)
	assert.Equal(t, 2, tbl.Len())
}

func TestWorldQueryGetCharactersByLocationReturnsCorrectFields(t *testing.T) {
	playerID := "01BX5ZZKBKACTAV9WEVGEMMVS4"
	wq := &mockWorldQueryAccess{
		charactersResult: []hostfunc.CharacterResult{
			{ID: testCharID1, PlayerID: playerID, Name: "Alice", Description: "A brave hero", LocationID: testLocID1},
		},
	}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`result = world_ext.get_characters_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	row := L.GetGlobal("result").(*lua.LTable).RawGetInt(1).(*lua.LTable)
	assert.Equal(t, testCharID1, row.RawGetString("id").String())
	assert.Equal(t, playerID, row.RawGetString("player_id").String())
	assert.Equal(t, "Alice", row.RawGetString("name").String())
	assert.Equal(t, "A brave hero", row.RawGetString("description").String())
	assert.Equal(t, testLocID1, row.RawGetString("location_id").String())
}

func TestWorldQueryGetCharactersByLocationReturnsEmptyTableWhenNone(t *testing.T) {
	wq := &mockWorldQueryAccess{charactersResult: nil}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`result = world_ext.get_characters_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 0, result.(*lua.LTable).Len())
}

func TestWorldQueryGetCharactersByLocationReturnsNilAndErrorOnFailure(t *testing.T) {
	wq := &mockWorldQueryAccess{charactersErr: errors.New("query failed")}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`result, q_err = world_ext.get_characters_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("q_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

func TestWorldQueryGetCharactersByLocationPassesSubjectAndLocationIDs(t *testing.T) {
	wq := &mockWorldQueryAccess{}
	L := newCapWorldQueryState(t, wq)

	err := L.DoString(`world_ext.get_characters_by_location("` + testCharID1 + `", "` + testLocID1 + `")`)
	require.NoError(t, err)

	require.Len(t, wq.charactersCalls, 1)
	assert.Equal(t, testCharID1, wq.charactersCalls[0].SubjectID)
	assert.Equal(t, testLocID1, wq.charactersCalls[0].LocationID)
}
