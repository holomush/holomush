// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"

	lua "github.com/yuin/gopher-lua"
)

// ObjectResult carries object data from the host to Lua plugins.
// It is a flat, string-keyed struct to avoid coupling plugins to internal types.
type ObjectResult struct {
	ID          string
	Name        string
	Description string
	LocationID  string
	OwnerID     string
}

// CharacterResult carries character data from the host to Lua plugins.
// It is a flat, string-keyed struct to avoid coupling plugins to internal types.
type CharacterResult struct {
	ID          string
	PlayerID    string
	Name        string
	Description string
	LocationID  string
}

// WorldQueryAccess is the narrow interface required by WorldQueryCapability.
// It covers the extended world query operations needed by the capability module,
// supplementing the base world hostfuncs (query_location, query_character,
// query_object) which are always available without a capability declaration.
type WorldQueryAccess interface {
	// GetObjectsByLocation returns all objects at a location visible to the subject.
	GetObjectsByLocation(ctx context.Context, subjectID, locationID string) ([]ObjectResult, error)

	// GetCharactersByLocation returns all characters at a location visible to the subject.
	GetCharactersByLocation(ctx context.Context, subjectID, locationID string) ([]CharacterResult, error)
}

// WorldQueryCapability implements the Capability interface for the world_ext namespace.
// It registers Lua host functions under the global "world_ext" table, providing
// extended world queries beyond the always-available base hostfuncs.
type WorldQueryCapability struct {
	world WorldQueryAccess
}

// Ensure WorldQueryCapability satisfies the Capability interface at compile time.
var _ Capability = (*WorldQueryCapability)(nil)

// NewWorldQueryCapability creates a WorldQueryCapability backed by the given WorldQueryAccess.
func NewWorldQueryCapability(world WorldQueryAccess) *WorldQueryCapability {
	return &WorldQueryCapability{world: world}
}

// Namespace returns "world_ext", the Lua global table name for this capability.
// The base "world" queries (query_location, query_character, query_object) are
// always-available hostfuncs; this namespace covers the extended set.
func (c *WorldQueryCapability) Namespace() string {
	return "world_ext"
}

// Register injects the world_ext.* functions into the Lua state as a global table.
func (c *WorldQueryCapability) Register(L *lua.LState, pluginName string) { //nolint:gocritic // L is conventional gopher-lua parameter name
	tbl := L.NewTable()
	L.SetField(tbl, "get_objects_by_location", L.NewFunction(c.getObjectsByLocationFn(pluginName)))
	L.SetField(tbl, "get_characters_by_location", L.NewFunction(c.getCharactersByLocationFn(pluginName)))
	L.SetGlobal("world_ext", tbl)
}

// getObjectsByLocationFn returns a Lua function implementing
// world_ext.get_objects_by_location(subject_id, location_id).
// Returns an array of {id, name, description, location_id, owner_id} tables,
// or nil + error string on failure.
func (c *WorldQueryCapability) getObjectsByLocationFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		subjectID := L.CheckString(1)
		locationID := L.CheckString(2)

		ctx := luaContext(L)
		objects, err := c.world.GetObjectsByLocation(ctx, subjectID, locationID)
		if err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "get_objects_by_location",
				Subject:   "location",
				SubjectID: locationID,
			}, err)
		}

		arr := L.NewTable()
		for _, o := range objects {
			row := L.NewTable()
			L.SetField(row, "id", lua.LString(o.ID))
			L.SetField(row, "name", lua.LString(o.Name))
			L.SetField(row, "description", lua.LString(o.Description))
			L.SetField(row, "location_id", lua.LString(o.LocationID))
			L.SetField(row, "owner_id", lua.LString(o.OwnerID))
			arr.Append(row)
		}
		L.Push(arr)
		return 1
	}
}

// getCharactersByLocationFn returns a Lua function implementing
// world_ext.get_characters_by_location(subject_id, location_id).
// Returns an array of {id, player_id, name, description, location_id} tables,
// or nil + error string on failure.
func (c *WorldQueryCapability) getCharactersByLocationFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		subjectID := L.CheckString(1)
		locationID := L.CheckString(2)

		ctx := luaContext(L)
		characters, err := c.world.GetCharactersByLocation(ctx, subjectID, locationID)
		if err != nil {
			return capError(L, PluginErrorContext{
				Plugin:    pluginName,
				Operation: "get_characters_by_location",
				Subject:   "location",
				SubjectID: locationID,
			}, err)
		}

		arr := L.NewTable()
		for _, ch := range characters {
			row := L.NewTable()
			L.SetField(row, "id", lua.LString(ch.ID))
			L.SetField(row, "player_id", lua.LString(ch.PlayerID))
			L.SetField(row, "name", lua.LString(ch.Name))
			L.SetField(row, "description", lua.LString(ch.Description))
			L.SetField(row, "location_id", lua.LString(ch.LocationID))
			arr.Append(row)
		}
		L.Push(arr)
		return 1
	}
}
