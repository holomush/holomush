// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"errors"
	"log/slog"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/world"
)

// WorldQuerier provides read access to world data.
type WorldQuerier interface {
	// GetLocation retrieves a location by ID.
	GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error)

	// GetCharacter retrieves a character by ID.
	GetCharacter(ctx context.Context, id ulid.ULID) (*world.Character, error)

	// GetCharactersByLocation retrieves characters at a location with pagination.
	GetCharactersByLocation(ctx context.Context, locationID ulid.ULID, opts world.ListOptions) ([]*world.Character, error)

	// GetObject retrieves an object by ID.
	GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error)
}

// queryLocationFn returns a Lua function that queries location information.
func (f *Functions) queryLocationFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldMutator == nil {
			return f.pushServiceUnavailable(L, "query_location", pluginName)
		}

		locationID := L.CheckString(1)
		id, ok := parseULID(L, locationID, pluginName, "query_location", "location ID")
		if !ok {
			return 2
		}

		return f.withQueryContext(L, pluginName, func(ctx context.Context, adapter *WorldQuerierAdapter) int {
			loc, err := adapter.GetLocation(ctx, id)
			if err != nil {
				if errors.Is(err, world.ErrNotFound) {
					slog.Debug("query_location: location not found",
						"plugin", pluginName,
						"location_id", locationID)
				}
				return pushError(L, SanitizeErrorForPlugin(PluginErrorContext{Plugin: pluginName, Operation: "query_location", Subject: "location", SubjectID: locationID}, err))
			}

			location := L.NewTable()
			L.SetField(location, "id", lua.LString(loc.ID.String()))
			L.SetField(location, "name", lua.LString(loc.Name))
			L.SetField(location, "description", lua.LString(loc.Description))
			L.SetField(location, "type", lua.LString(string(loc.Type)))

			return pushSuccess(L, location)
		})
	}
}

// queryCharacterFn returns a Lua function that queries character information.
func (f *Functions) queryCharacterFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldMutator == nil {
			return f.pushServiceUnavailable(L, "query_character", pluginName)
		}

		charID := L.CheckString(1)
		id, ok := parseULID(L, charID, pluginName, "query_character", "character ID")
		if !ok {
			return 2
		}

		return f.withQueryContext(L, pluginName, func(ctx context.Context, adapter *WorldQuerierAdapter) int {
			char, err := adapter.GetCharacter(ctx, id)
			if err != nil {
				if errors.Is(err, world.ErrNotFound) {
					slog.Debug("query_character: character not found",
						"plugin", pluginName,
						"character_id", charID)
				}
				return pushError(L, SanitizeErrorForPlugin(PluginErrorContext{Plugin: pluginName, Operation: "query_character", Subject: "character", SubjectID: charID}, err))
			}

			character := L.NewTable()
			L.SetField(character, "id", lua.LString(char.ID.String()))
			L.SetField(character, "player_id", lua.LString(char.PlayerID.String()))
			L.SetField(character, "name", lua.LString(char.Name))
			L.SetField(character, "description", lua.LString(char.Description))
			if char.LocationID != nil {
				L.SetField(character, "location_id", lua.LString(char.LocationID.String()))
			}

			return pushSuccess(L, character)
		})
	}
}

// queryLocationCharactersFn returns a Lua function that queries characters at a location.
// Lua signature: query_location_characters(location_id, [opts])
// opts is an optional table with:
//   - limit: max results (default: 100)
//   - offset: number of results to skip (default: 0)
func (f *Functions) queryLocationCharactersFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldMutator == nil {
			return f.pushServiceUnavailable(L, "query_location_characters", pluginName)
		}

		locationID := L.CheckString(1)
		id, ok := parseULID(L, locationID, pluginName, "query_location_characters", "location ID")
		if !ok {
			return 2
		}

		// Parse optional pagination options from second argument
		opts := world.ListOptions{}
		if L.GetTop() >= 2 && L.Get(2).Type() == lua.LTTable {
			optsTable := L.ToTable(2)
			if limitVal := optsTable.RawGetString("limit"); limitVal.Type() == lua.LTNumber {
				opts.Limit = int(lua.LVAsNumber(limitVal))
			}
			if offsetVal := optsTable.RawGetString("offset"); offsetVal.Type() == lua.LTNumber {
				opts.Offset = int(lua.LVAsNumber(offsetVal))
			}
		}

		return f.withQueryContext(L, pluginName, func(ctx context.Context, adapter *WorldQuerierAdapter) int {
			chars, err := adapter.GetCharactersByLocation(ctx, id, opts)
			if err != nil {
				if errors.Is(err, world.ErrNotFound) {
					slog.Debug("query_location_characters: location not found",
						"plugin", pluginName,
						"location_id", locationID)
				}
				return pushError(L, SanitizeErrorForPlugin(PluginErrorContext{Plugin: pluginName, Operation: "query_location_characters", Subject: "location", SubjectID: locationID}, err))
			}

			// Return lightweight list of characters (id, name only).
			// For full character details (player_id, description, location_id),
			// use query_character on individual character IDs.
			characters := L.NewTable()
			for i, char := range chars {
				c := L.NewTable()
				L.SetField(c, "id", lua.LString(char.ID.String()))
				L.SetField(c, "name", lua.LString(char.Name))
				characters.RawSetInt(i+1, c)
			}

			return pushSuccess(L, characters)
		})
	}
}

// queryObjectFn returns a Lua function that queries object information.
func (f *Functions) queryObjectFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldMutator == nil {
			return f.pushServiceUnavailable(L, "query_object", pluginName)
		}

		objID := L.CheckString(1)
		id, ok := parseULID(L, objID, pluginName, "query_object", "object ID")
		if !ok {
			return 2
		}

		return f.withQueryContext(L, pluginName, func(ctx context.Context, adapter *WorldQuerierAdapter) int {
			obj, err := adapter.GetObject(ctx, id)
			if err != nil {
				if errors.Is(err, world.ErrNotFound) {
					slog.Debug("query_object: object not found",
						"plugin", pluginName,
						"object_id", objID)
				}
				return pushError(L, SanitizeErrorForPlugin(PluginErrorContext{Plugin: pluginName, Operation: "query_object", Subject: "object", SubjectID: objID}, err))
			}

			object := L.NewTable()
			L.SetField(object, "id", lua.LString(obj.ID.String()))
			L.SetField(object, "name", lua.LString(obj.Name))
			L.SetField(object, "description", lua.LString(obj.Description))
			L.SetField(object, "is_container", lua.LBool(obj.IsContainer))

			// Optional fields - only set if non-nil
			if obj.LocationID() != nil {
				L.SetField(object, "location_id", lua.LString(obj.LocationID().String()))
			}
			if obj.HeldByCharacterID() != nil {
				L.SetField(object, "held_by_character_id", lua.LString(obj.HeldByCharacterID().String()))
			}
			if obj.ContainedInObjectID() != nil {
				L.SetField(object, "contained_in_object_id", lua.LString(obj.ContainedInObjectID().String()))
			}
			if obj.OwnerID != nil {
				L.SetField(object, "owner_id", lua.LString(obj.OwnerID.String()))
			}

			// Containment type
			containment := obj.Containment()
			L.SetField(object, "containment_type", lua.LString(string((&containment).Type())))

			return pushSuccess(L, object)
		})
	}
}
