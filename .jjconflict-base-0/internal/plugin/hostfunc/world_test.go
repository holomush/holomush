// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/world"
)

// mockWorldQuerier implements hostfunc.WorldMutator for testing.
// It provides both read and write operations for world state.
type mockWorldQuerier struct {
	location   *world.Location
	character  *world.Character
	characters []*world.Character
	object     *world.Object
	err        error
}

// WorldMutator read methods (with subjectID for ABAC)
func (m *mockWorldQuerier) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.location, nil
}

func (m *mockWorldQuerier) GetCharacter(_ context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.character, nil
}

func (m *mockWorldQuerier) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.characters, nil
}

func (m *mockWorldQuerier) GetObject(_ context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.object, nil
}

// WorldMutator write methods
func (m *mockWorldQuerier) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *mockWorldQuerier) CreateExit(_ context.Context, _ string, _ *world.Exit) error {
	return nil
}

func (m *mockWorldQuerier) CreateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *mockWorldQuerier) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *mockWorldQuerier) UpdateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *mockWorldQuerier) FindLocationByName(_ context.Context, _, _ string) (*world.Location, error) {
	return nil, world.ErrNotFound
}

// Compile-time interface check.
var _ hostfunc.WorldMutator = (*mockWorldQuerier)(nil)

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, room.Type(), "expected nil room for not found")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Equal(t, "room not found", errVal.String(), "expected sanitized error message")
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

func TestQueryRoom_InternalError(t *testing.T) {
	roomID := ulid.Make()
	// Internal error should be sanitized - plugin should not see "database error"
	querier := &mockWorldQuerier{err: errors.New("database error connection timeout with stack trace")}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + roomID.String() + `")`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, room.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Should return sanitized message with correlation ID, not the actual error
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error (ref: ", "expected sanitized error message with reference ID")
	assert.NotContains(t, errStr, "database", "internal error details should not be exposed")

	// Extract and validate the reference ID is a valid ULID (26 characters)
	// Format: "internal error (ref: <26-char-ulid>)"
	const prefix = "internal error (ref: "
	const suffix = ")"
	require.True(t, len(errStr) >= len(prefix)+len(suffix)+26, "error message too short for ULID")
	refID := errStr[len(prefix) : len(errStr)-len(suffix)]
	assert.Len(t, refID, 26, "reference ID should be a 26-character ULID")
	_, parseErr := ulid.Parse(refID)
	assert.NoError(t, parseErr, "reference ID should be a valid ULID")
}

func TestQueryRoom_PermissionDenied(t *testing.T) {
	roomID := ulid.Make()
	querier := &mockWorldQuerier{err: world.ErrPermissionDenied}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + roomID.String() + `")`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, room.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "access denied", errVal.String(), "expected sanitized access denied message")
}

func TestQueryRoom_Timeout(t *testing.T) {
	roomID := ulid.Make()
	// Context timeout should be surfaced to plugins as "query timed out"
	querier := &mockWorldQuerier{err: context.DeadlineExceeded}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + roomID.String() + `")`)
	require.NoError(t, err)

	room := L.GetGlobal("room")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, room.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "query timed out", errVal.String(), "expected timeout error message")
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

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, character.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "character not found", errVal.String(), "expected sanitized error message")
}

func TestQueryCharacter_InternalError(t *testing.T) {
	charID := ulid.Make()
	// Internal error should be sanitized - plugin should not see "database error"
	querier := &mockWorldQuerier{err: errors.New("database error connection timeout with stack trace")}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + charID.String() + `")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, character.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Should return sanitized message with correlation ID, not the actual error
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error (ref: ", "expected sanitized error message with reference ID")
	assert.NotContains(t, errStr, "database", "internal error details should not be exposed")

	// Extract and validate the reference ID is a valid ULID (26 characters)
	const prefix = "internal error (ref: "
	const suffix = ")"
	require.True(t, len(errStr) >= len(prefix)+len(suffix)+26, "error message too short for ULID")
	refID := errStr[len(prefix) : len(errStr)-len(suffix)]
	assert.Len(t, refID, 26, "reference ID should be a 26-character ULID")
	_, parseErr := ulid.Parse(refID)
	assert.NoError(t, parseErr, "reference ID should be a valid ULID")
}

func TestQueryCharacter_PermissionDenied(t *testing.T) {
	charID := ulid.Make()
	querier := &mockWorldQuerier{err: world.ErrPermissionDenied}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`character, err = holomush.query_character("` + charID.String() + `")`)
	require.NoError(t, err)

	character := L.GetGlobal("character")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, character.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "access denied", errVal.String(), "expected sanitized access denied message")
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

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

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
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

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

func TestQueryRoomCharacters_NotFound(t *testing.T) {
	roomID := ulid.Make()
	querier := &mockWorldQuerier{err: world.ErrNotFound}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + roomID.String() + `")`)
	require.NoError(t, err)

	characters := L.GetGlobal("characters")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, characters.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "room not found", errVal.String(), "expected sanitized error message")
}

func TestQueryRoomCharacters_InternalError(t *testing.T) {
	roomID := ulid.Make()
	// Internal error should be sanitized - plugin should not see "database error"
	querier := &mockWorldQuerier{err: errors.New("database error connection timeout with stack trace")}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + roomID.String() + `")`)
	require.NoError(t, err)

	characters := L.GetGlobal("characters")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, characters.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Should return sanitized message with correlation ID, not the actual error
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error (ref: ", "expected sanitized error message with reference ID")
	assert.NotContains(t, errStr, "database", "internal error details should not be exposed")

	// Extract and validate the reference ID is a valid ULID (26 characters)
	const prefix = "internal error (ref: "
	const suffix = ")"
	require.True(t, len(errStr) >= len(prefix)+len(suffix)+26, "error message too short for ULID")
	refID := errStr[len(prefix) : len(errStr)-len(suffix)]
	assert.Len(t, refID, 26, "reference ID should be a 26-character ULID")
	_, parseErr := ulid.Parse(refID)
	assert.NoError(t, parseErr, "reference ID should be a valid ULID")
}

func TestQueryRoomCharacters_PermissionDenied(t *testing.T) {
	roomID := ulid.Make()
	querier := &mockWorldQuerier{err: world.ErrPermissionDenied}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + roomID.String() + `")`)
	require.NoError(t, err)

	characters := L.GetGlobal("characters")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, characters.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "access denied", errVal.String(), "expected sanitized access denied message")
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

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`characters, err = holomush.query_room_characters("` + roomID.String() + `")`)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

func TestQueryObject(t *testing.T) {
	objID := ulid.Make()
	locID := ulid.Make()
	ownerID := ulid.Make()
	obj, err := world.NewObjectWithID(objID, "Magic Sword", world.InLocation(locID))
	require.NoError(t, err)
	obj.Description = "A glowing blade of ancient power."
	obj.OwnerID = &ownerID

	querier := &mockWorldQuerier{object: obj}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err = L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	// Check err is nil
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error")

	// Check obj is a table with expected fields
	objVal := L.GetGlobal("obj")
	require.Equal(t, lua.LTTable, objVal.Type(), "expected table result")

	tbl := objVal.(*lua.LTable)
	assert.Equal(t, objID.String(), tbl.RawGetString("id").String())
	assert.Equal(t, obj.Name, tbl.RawGetString("name").String())
	assert.Equal(t, obj.Description, tbl.RawGetString("description").String())
	assert.Equal(t, lua.LFalse, tbl.RawGetString("is_container"))
	assert.Equal(t, locID.String(), tbl.RawGetString("location_id").String())
	assert.Equal(t, ownerID.String(), tbl.RawGetString("owner_id").String())
	assert.Equal(t, "location", tbl.RawGetString("containment_type").String())
}

func TestQueryObject_WithContainer(t *testing.T) {
	objID := ulid.Make()
	containerID := ulid.Make()
	obj, err := world.NewObjectWithID(objID, "Gold Coins", world.ContainedInObject(containerID))
	require.NoError(t, err)
	obj.Description = "A pile of shiny gold coins."
	obj.IsContainer = true

	querier := &mockWorldQuerier{object: obj}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err = L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	require.Equal(t, lua.LTTable, objVal.Type())

	tbl := objVal.(*lua.LTable)
	assert.Equal(t, lua.LTrue, tbl.RawGetString("is_container"))
	assert.Equal(t, containerID.String(), tbl.RawGetString("contained_in_object_id").String())
	assert.Equal(t, "object", tbl.RawGetString("containment_type").String())
}

func TestQueryObject_HeldByCharacter(t *testing.T) {
	objID := ulid.Make()
	charID := ulid.Make()
	obj, err := world.NewObjectWithID(objID, "Magic Sword", world.HeldByCharacter(charID))
	require.NoError(t, err)
	obj.Description = "A glowing blade."

	querier := &mockWorldQuerier{object: obj}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err = L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	require.Equal(t, lua.LTTable, objVal.Type())

	tbl := objVal.(*lua.LTable)
	assert.Equal(t, charID.String(), tbl.RawGetString("held_by_character_id").String())
	assert.Equal(t, "character", tbl.RawGetString("containment_type").String())
}

// TestQueryObject_NilOptionalFields tests the host function's defensive handling of nil
// optional fields when returning object data to Lua plugins.
//
// NOTE: This test intentionally creates an Object with invalid state (no containment set)
// to verify the host function gracefully handles nil containment fields. In production,
// objects are always created via NewObjectWithID() which requires containment, so this
// invalid state should never occur. This test ensures plugins won't crash if they somehow
// receive an object in an unexpected state.
func TestQueryObject_NilOptionalFields(t *testing.T) {
	objID := ulid.Make()
	obj := &world.Object{
		ID:          objID,
		Name:        "Simple Object",
		Description: "Nothing special.",
		IsContainer: false,
		// Intentionally leaving containment unset to test defensive nil handling.
		// This is NOT a valid production state - see function comment above.
	}

	querier := &mockWorldQuerier{object: obj}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	require.Equal(t, lua.LTTable, objVal.Type())

	tbl := objVal.(*lua.LTable)
	assert.Equal(t, lua.LTNil, tbl.RawGetString("location_id").Type())
	assert.Equal(t, lua.LTNil, tbl.RawGetString("held_by_character_id").Type())
	assert.Equal(t, lua.LTNil, tbl.RawGetString("contained_in_object_id").Type())
	assert.Equal(t, lua.LTNil, tbl.RawGetString("owner_id").Type())
}

func TestQueryObject_InvalidID(t *testing.T) {
	querier := &mockWorldQuerier{}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`obj, err = holomush.query_object("not-valid-ulid")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, objVal.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Contains(t, errVal.String(), "invalid object ID")
}

func TestQueryObject_NotFound(t *testing.T) {
	querier := &mockWorldQuerier{err: world.ErrNotFound}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`obj, err = holomush.query_object("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, objVal.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "object not found", errVal.String(), "expected sanitized error message")
}

func TestQueryObject_InternalError(t *testing.T) {
	objID := ulid.Make()
	// Internal error should be sanitized - plugin should not see "database error"
	querier := &mockWorldQuerier{err: errors.New("database error connection timeout with stack trace")}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, objVal.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	// Should return sanitized message with correlation ID, not the actual error
	errStr := errVal.String()
	assert.Contains(t, errStr, "internal error (ref: ", "expected sanitized error message with reference ID")
	assert.NotContains(t, errStr, "database", "internal error details should not be exposed")

	// Extract and validate the reference ID is a valid ULID (26 characters)
	const prefix = "internal error (ref: "
	const suffix = ")"
	require.True(t, len(errStr) >= len(prefix)+len(suffix)+26, "error message too short for ULID")
	refID := errStr[len(prefix) : len(errStr)-len(suffix)]
	assert.Len(t, refID, 26, "reference ID should be a 26-character ULID")
	_, parseErr := ulid.Parse(refID)
	assert.NoError(t, parseErr, "reference ID should be a valid ULID")
}

func TestQueryObject_PermissionDenied(t *testing.T) {
	objID := ulid.Make()
	querier := &mockWorldQuerier{err: world.ErrPermissionDenied}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, objVal.Type())
	assert.Equal(t, lua.LTString, errVal.Type())
	assert.Equal(t, "access denied", errVal.String(), "expected sanitized access denied message")
}

func TestQueryObject_NoQuerierConfigured(t *testing.T) {
	// No world querier provided
	funcs := hostfunc.New(nil, &mockEnforcerAllow{})

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`obj, err = holomush.query_object("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	objVal := L.GetGlobal("obj")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, objVal.Type(), "expected nil object")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string")
	assert.Contains(t, errVal.String(), "world service not configured - contact server administrator")
}

func TestQueryObject_RequiresCapability(t *testing.T) {
	objID := ulid.Make()
	obj := &world.Object{
		ID:   objID,
		Name: "Test Object",
	}

	querier := &mockWorldQuerier{object: obj}
	enforcer := capability.NewEnforcer()
	// No capabilities granted

	funcs := hostfunc.New(nil, enforcer, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	funcs.Register(L, "test-plugin")

	err := L.DoString(`obj, err = holomush.query_object("` + objID.String() + `")`)
	require.Error(t, err, "expected capability error")
	assert.Contains(t, err.Error(), "capability denied")
}

// contextAwareWorldQuerier passes through the context to allow testing context propagation.
type contextAwareWorldQuerier struct {
	ctxChan chan context.Context // receives the context passed to queries
	err     error                // error to return
}

// WorldMutator read methods (with subjectID for ABAC)
func (m *contextAwareWorldQuerier) GetLocation(ctx context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	if m.ctxChan != nil {
		select {
		case m.ctxChan <- ctx:
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &world.Location{ID: ulid.Make(), Name: "Test"}, nil
}

func (m *contextAwareWorldQuerier) GetCharacter(ctx context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	if m.ctxChan != nil {
		select {
		case m.ctxChan <- ctx:
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &world.Character{ID: ulid.Make(), Name: "Test"}, nil
}

func (m *contextAwareWorldQuerier) GetCharactersByLocation(ctx context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	if m.ctxChan != nil {
		select {
		case m.ctxChan <- ctx:
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return []*world.Character{}, nil
}

func (m *contextAwareWorldQuerier) GetObject(ctx context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	if m.ctxChan != nil {
		select {
		case m.ctxChan <- ctx:
		default:
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	return &world.Object{ID: ulid.Make(), Name: "Test"}, nil
}

// WorldMutator write methods
func (m *contextAwareWorldQuerier) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *contextAwareWorldQuerier) CreateExit(_ context.Context, _ string, _ *world.Exit) error {
	return nil
}

func (m *contextAwareWorldQuerier) CreateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *contextAwareWorldQuerier) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *contextAwareWorldQuerier) UpdateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *contextAwareWorldQuerier) FindLocationByName(_ context.Context, _, _ string) (*world.Location, error) {
	return nil, world.ErrNotFound
}

// Compile-time interface check.
var _ hostfunc.WorldMutator = (*contextAwareWorldQuerier)(nil)

func TestQueryRoom_InheritsParentContext(t *testing.T) {
	// Create a parent context with a custom value to verify inheritance
	type ctxKey string
	const testKey ctxKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	L.SetContext(parentCtx) // Set the parent context on the Lua state
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	// The context passed to the querier should inherit from the Lua state's context
	select {
	case receivedCtx := <-ctxChan:
		// Verify the context inherits values from the parent
		val := receivedCtx.Value(testKey)
		assert.Equal(t, "test-value", val, "derived context should inherit values from parent context")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryCharacter_InheritsParentContext(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	L.SetContext(parentCtx)
	funcs.Register(L, "test-plugin")

	err := L.DoString(`char, err = holomush.query_character("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	select {
	case receivedCtx := <-ctxChan:
		val := receivedCtx.Value(testKey)
		assert.Equal(t, "test-value", val, "derived context should inherit values from parent context")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryRoomCharacters_InheritsParentContext(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	L.SetContext(parentCtx)
	funcs.Register(L, "test-plugin")

	err := L.DoString(`chars, err = holomush.query_room_characters("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	select {
	case receivedCtx := <-ctxChan:
		val := receivedCtx.Value(testKey)
		assert.Equal(t, "test-value", val, "derived context should inherit values from parent context")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryObject_InheritsParentContext(t *testing.T) {
	type ctxKey string
	const testKey ctxKey = "test-key"
	parentCtx := context.WithValue(context.Background(), testKey, "test-value")

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	L.SetContext(parentCtx)
	funcs.Register(L, "test-plugin")

	err := L.DoString(`obj, err = holomush.query_object("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	select {
	case receivedCtx := <-ctxChan:
		val := receivedCtx.Value(testKey)
		assert.Equal(t, "test-value", val, "derived context should inherit values from parent context")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryRoom_InheritsContextDeadline(t *testing.T) {
	// Create a context with a short deadline (10ms) - shorter than the 5s default
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	L.SetContext(ctx)
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	// Verify the derived context respects the parent's deadline
	select {
	case receivedCtx := <-ctxChan:
		deadline, ok := receivedCtx.Deadline()
		require.True(t, ok, "derived context should have a deadline")
		// The deadline should be within ~10ms of now (the parent's deadline)
		// rather than 5 seconds (the default query timeout)
		assert.WithinDuration(t, time.Now(), deadline, 50*time.Millisecond,
			"deadline should inherit from parent context, not use the 5s default")
	default:
		t.Fatal("querier was not called")
	}
}

func TestQueryRoom_FallbackToBackgroundContext(t *testing.T) {
	// Test that when Lua state has no context set, we fall back to context.Background()
	// This ensures backwards compatibility

	ctxChan := make(chan context.Context, 1)
	querier := &contextAwareWorldQuerier{ctxChan: ctxChan}
	funcs := hostfunc.New(nil, &mockEnforcerAllow{}, hostfunc.WithWorldService(querier))

	L := lua.NewState()
	defer L.Close()
	// Note: NOT calling L.SetContext() - context is nil
	funcs.Register(L, "test-plugin")

	err := L.DoString(`room, err = holomush.query_room("` + ulid.Make().String() + `")`)
	require.NoError(t, err)

	// The query should still work and use a 5-second timeout derived from Background
	select {
	case receivedCtx := <-ctxChan:
		deadline, ok := receivedCtx.Deadline()
		require.True(t, ok, "derived context should have a deadline from default timeout")
		// Deadline should be roughly 5 seconds from now (the default query timeout)
		assert.WithinDuration(t, time.Now().Add(5*time.Second), deadline, 100*time.Millisecond,
			"should use default 5s timeout when no parent context set")
	default:
		t.Fatal("querier was not called")
	}

	// Query should succeed
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, errVal.Type(), "query should succeed with fallback context")
}
