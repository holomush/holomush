// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package luabridge_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/luabridge"
	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

const validULID = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

// TestProtoToLuaTableMapsScalarFields round-trips a typed message into a Lua
// table, asserting a string scalar lands under its snake_case proto field name.
func TestProtoToLuaTableMapsScalarFields(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	tbl := luabridge.ProtoToLuaTable(L, &hostv1.GetPropertyResponse{Value: "Town Square"})

	assert.Equal(t, "Town Square", L.GetField(tbl, "value").String())
}

// TestLuaTableToProtoBuildsTypedRequest builds a typed request from a Lua table
// keyed by snake_case proto field names.
func TestLuaTableToProtoBuildsTypedRequest(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	arg := L.NewTable()
	L.SetField(arg, "entity_type", lua.LString("location"))
	L.SetField(arg, "entity_id", lua.LString(validULID))
	L.SetField(arg, "property", lua.LString("name"))

	var req hostv1.GetPropertyRequest
	require.NoError(t, luabridge.LuaTableToProto(arg, &req))

	assert.Equal(t, "location", req.GetEntityType())
	assert.Equal(t, validULID, req.GetEntityId())
	assert.Equal(t, "name", req.GetProperty())
}

// TestProtoToLuaTableMapsRepeatedNestedMessages converts a message with a
// repeated nested-message field into a 1-indexed Lua array of nested tables,
// covering repeated, nested-message, string, and bool kinds together.
func TestProtoToLuaTableMapsRepeatedNestedMessages(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	resp := &hostv1.ListActiveResponse{
		Sessions: []*hostv1.SessionInfo{
			{Id: "s1", CharacterName: "Alice", GridPresent: true},
			{Id: "s2", CharacterName: "Bob", GridPresent: false},
		},
	}

	tbl := luabridge.ProtoToLuaTable(L, resp)
	sessions, ok := L.GetField(tbl, "sessions").(*lua.LTable)
	require.True(t, ok, "sessions must be a table")
	require.Equal(t, 2, sessions.Len())

	first, ok := sessions.RawGetInt(1).(*lua.LTable)
	require.True(t, ok, "element 1 must be a nested table")
	assert.Equal(t, "s1", L.GetField(first, "id").String())
	assert.Equal(t, "Alice", L.GetField(first, "character_name").String())
	assert.Equal(t, lua.LBool(true), L.GetField(first, "grid_present"))

	second, ok := sessions.RawGetInt(2).(*lua.LTable)
	require.True(t, ok, "element 2 must be a nested table")
	assert.Equal(t, "Bob", L.GetField(second, "character_name").String())
	// grid_present false is the proto3 default, so it is omitted from the table.
	assert.Equal(t, lua.LNil, L.GetField(second, "grid_present"))
}

// TestLuaTableToProtoBuildsRepeatedNestedMessages builds a repeated
// nested-message field from a Lua array table, the inverse direction.
func TestLuaTableToProtoBuildsRepeatedNestedMessages(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	s1 := L.NewTable()
	L.SetField(s1, "id", lua.LString("s1"))
	L.SetField(s1, "character_name", lua.LString("Alice"))
	L.SetField(s1, "grid_present", lua.LBool(true))

	arr := L.NewTable()
	arr.Append(s1)

	root := L.NewTable()
	L.SetField(root, "sessions", arr)

	var resp hostv1.ListActiveResponse
	require.NoError(t, luabridge.LuaTableToProto(root, &resp))

	require.Len(t, resp.GetSessions(), 1)
	assert.Equal(t, "s1", resp.GetSessions()[0].GetId())
	assert.Equal(t, "Alice", resp.GetSessions()[0].GetCharacterName())
	assert.True(t, resp.GetSessions()[0].GetGridPresent())
}

// TestProtoToLuaTableMapsEnumField round-trips an enum field as its numeric
// value in both directions.
func TestProtoToLuaTableMapsEnumField(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	key := &hostv1.FocusKey{Kind: hostv1.FocusKind_FOCUS_KIND_SCENE, TargetId: validULID}
	tbl := luabridge.ProtoToLuaTable(L, key)
	assert.Equal(t, lua.LNumber(int(hostv1.FocusKind_FOCUS_KIND_SCENE)), L.GetField(tbl, "kind"))
	assert.Equal(t, validULID, L.GetField(tbl, "target_id").String())
}

// TestLuaTableToProtoBuildsEnumField builds an enum field from a Lua number.
func TestLuaTableToProtoBuildsEnumField(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	arg := L.NewTable()
	L.SetField(arg, "kind", lua.LNumber(int(hostv1.FocusKind_FOCUS_KIND_SCENE)))
	L.SetField(arg, "target_id", lua.LString(validULID))

	var key hostv1.FocusKey
	require.NoError(t, luabridge.LuaTableToProto(arg, &key))
	assert.Equal(t, hostv1.FocusKind_FOCUS_KIND_SCENE, key.GetKind())
	assert.Equal(t, validULID, key.GetTargetId())
}

// TestLuaTableToProtoRejectsTypeMismatch returns an error when a numeric field
// receives a string, rather than silently coercing.
func TestLuaTableToProtoRejectsTypeMismatch(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	arg := L.NewTable()
	L.SetField(arg, "kind", lua.LString("not-a-number"))

	var key hostv1.FocusKey
	err := luabridge.LuaTableToProto(arg, &key)
	require.Error(t, err)
}
