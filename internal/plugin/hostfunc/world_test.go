// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/world"
)

// mockWorldQuerier implements hostfunc.WorldQuerier for testing.
type mockWorldQuerier struct {
	location   *world.Location
	character  *world.Character
	characters []*world.Character
	err        error
}

func (m *mockWorldQuerier) GetLocation(_ context.Context, _ ulid.ULID) (*world.Location, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.location, nil
}

func (m *mockWorldQuerier) GetCharacter(_ context.Context, _ ulid.ULID) (*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.character, nil
}

func (m *mockWorldQuerier) GetCharactersByLocation(_ context.Context, _ ulid.ULID) ([]*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.characters, nil
}

// Compile-time interface check.
var _ hostfunc.WorldQuerier = (*mockWorldQuerier)(nil)

// mockEnforcerAllow allows all capabilities.
type mockEnforcerAllow struct{}

func (m *mockEnforcerAllow) Check(_, _ string) bool {
	return true
}

func TestQueryRoom(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:          locID,
		Name:        "Test Room",
		Description: "A test room for testing",
		Type:        world.LocationTypePersistent,
	}

	querier := &mockWorldQuerier{location: loc}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + locID.String() + `")`)
	require.NoError(t, err)

	// Check err is nil
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	// Check room is a table with expected fields
	room := L.GetGlobal("room")
	require.Equal(t, lua.LTTable, room.Type(), "expected table result")

	tbl := room.(*lua.LTable)
	assert.Equal(t, locID.String(), tbl.RawGetString("id").String())
	assert.Equal(t, loc.Name, tbl.RawGetString("name").String())
	assert.Equal(t, loc.Description, tbl.RawGetString("description").String())
	assert.Equal(t, string(loc.Type), tbl.RawGetString("type").String())
}

func TestQueryRoom_InvalidID(t *testing.T) {
	querier := &mockWorldQuerier{}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("not-a-valid-ulid")`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, room.Type(), "expected nil room for invalid ID")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Contains(t, errVal.String(), "invalid room ID")
}

func TestQueryRoom_NotFound(t *testing.T) {
	querier := &mockWorldQuerier{err: world.ErrNotFound}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, room.Type(), "expected nil room for not found")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
}

func TestQueryRoom_NoQuerierConfigured(t *testing.T) {
	// No world querier provided
	funcs := hostfunc.New(nil, &mockEnforcerAllow{})

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, room.Type(), "expected nil room")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Contains(t, errVal.String(), "world service not configured - contact server administrator")
}

func TestQueryRoom_Error(t *testing.T) {
	roomID := ulid.Make()
	querier := &mockWorldQuerier{err: errors.New("database error")}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + roomID.String() + `")`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, room.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Contains(t, errVal.String(), "database error")
}

func TestQueryRoom_RequiresCapability(t *testing.T) {
	locID := ulid.Make()
	loc := &world.Location{
		ID:   locID,
		Name: "Test Room",
		Type: world.LocationTypePersistent,
	}

	querier := &mockWorldQuerier{location: loc}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + locID.String() + `")`)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestQueryCharacter(t *testing.T) {
	charID := ulid.Make()
	playerID := ulid.Make()
	locID := ulid.Make()
	char := &world.Character{
		ID:          charID,
		PlayerID:    playerID,
		Name:        "Test Character",
		Description: "A brave adventurer with a mysterious past.",
		LocationID:  &locID,
	}

	querier := &mockWorldQuerier{character: char}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + charID.String() + `")`)
	require.NoError(t, err)

	// Check err is nil
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	// Check character is a table with expected fields
	character := L.GetGlobal("character")
	require.Equal(t, lua.LTTable, character.Type(), "expected table result")

	tbl := character.(*lua.LTable)
	assert.Equal(t, charID.String(), tbl.RawGetString("id").String())
	assert.Equal(t, playerID.String(), tbl.RawGetString("player_id").String())
	assert.Equal(t, char.Name, tbl.RawGetString("name").String())
	assert.Equal(t, char.Description, tbl.RawGetString("description").String())
	assert.Equal(t, locID.String(), tbl.RawGetString("location_id").String())
}

func TestQueryCharacter_NilLocation(t *testing.T) {
	charID := ulid.Make()
	char := &world.Character{
		ID:         charID,
		Name:       "Test Character",
		LocationID: nil, // Not in world
	}

	querier := &mockWorldQuerier{character: char}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + charID.String() + `")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	require.Equal(t, lua.LTTable, character.Type())

	tbl := character.(*lua.LTable)
	locID := tbl.RawGetString("location_id")
	assert.Equal(t, lua.LTNil, locID.Type(), "expected nil location_id")
}

func TestQueryCharacter_InvalidID(t *testing.T) {
	querier := &mockWorldQuerier{}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("not-valid")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, character.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Contains(t, errVal.String(), "invalid character ID")
}

func TestQueryCharacter_NotFound(t *testing.T) {
	querier := &mockWorldQuerier{err: world.ErrNotFound}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, character.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
}

func TestQueryCharacter_Error(t *testing.T) {
	charID := ulid.Make()
	querier := &mockWorldQuerier{err: errors.New("database error")}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + charID.String() + `")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, character.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Contains(t, errVal.String(), "database error")
}

func TestQueryCharacter_NoQuerierConfigured(t *testing.T) {
	// No world querier provided
	funcs := hostfunc.New(nil, &mockEnforcerAllow{})

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, character.Type(), "expected nil character")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Contains(t, errVal.String(), "world service not configured - contact server administrator")
}

func TestQueryCharacter_RequiresCapability(t *testing.T) {
	charID := ulid.Make()
	char := &world.Character{
		ID:   charID,
		Name: "Test Character",
	}

	querier := &mockWorldQuerier{character: char}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + charID.String() + `")`)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestQueryRoomCharacters(t *testing.T) {
	roomID := ulid.Make()
	char1 := &world.Character{
		ID:   ulid.Make(),
		Name: "Alice",
	}
	char2 := &world.Character{
		ID:   ulid.Make(),
		Name: "Bob",
	}

	querier := &mockWorldQuerier{characters: []*world.Character{char1, char2}}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + roomID.String() + `")`)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	characters := L.GetGlobal("characters")
	require.Equal(t, lua.LTTable, characters.Type(), "expected table result")

	tbl := characters.(*lua.LTable)
	assert.Equal(t, 2, tbl.Len(), "expected 2 characters")

	// Check first character
	first := tbl.RawGetInt(1)
	require.Equal(t, lua.LTTable, first.Type())
	firstTbl := first.(*lua.LTable)
	assert.Equal(t, char1.ID.String(), firstTbl.RawGetString("id").String())
	assert.Equal(t, char1.Name, firstTbl.RawGetString("name").String())

	// Check second character
	second := tbl.RawGetInt(2)
	require.Equal(t, lua.LTTable, second.Type())
	secondTbl := second.(*lua.LTable)
	assert.Equal(t, char2.ID.String(), secondTbl.RawGetString("id").String())
	assert.Equal(t, char2.Name, secondTbl.RawGetString("name").String())
}

func TestQueryRoomCharacters_EmptyRoom(t *testing.T) {
	roomID := ulid.Make()

	querier := &mockWorldQuerier{characters: []*world.Character{}}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + roomID.String() + `")`)
	require.NoError(t, err)

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type())

	characters := L.GetGlobal("characters")
	require.Equal(t, lua.LTTable, characters.Type())

	tbl := characters.(*lua.LTable)
	assert.Equal(t, 0, tbl.Len(), "expected empty table")
}

func TestQueryRoomCharacters_InvalidID(t *testing.T) {
	querier := &mockWorldQuerier{}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("invalid")`)
	require.NoError(t, err)

	characters := L.GetGlobal("characters")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, characters.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Contains(t, errVal.String(), "invalid room ID")
}

func TestQueryRoomCharacters_Error(t *testing.T) {
	roomID := ulid.Make()
	querier := &mockWorldQuerier{err: errors.New("database error")}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + roomID.String() + `")`)
	require.NoError(t, err)

	characters := L.GetGlobal("characters")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, characters.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Contains(t, errVal.String(), "database error")
}

func TestQueryRoomCharacters_NoQuerierConfigured(t *testing.T) {
	// No world querier provided
	funcs := hostfunc.New(nil, &mockEnforcerAllow{})

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	characters := L.GetGlobal("characters")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, characters.Type(), "expected nil characters")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Contains(t, errVal.String(), "world service not configured - contact server administrator")
}

func TestQueryRoomCharacters_RequiresCapability(t *testing.T) {
	roomID := ulid.Make()
	querier := &mockWorldQuerier{characters: []*world.Character{}}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldQuerier(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + roomID.String() + `")`)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}
