// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/world"
)

// sanitizeErrorForPlugin converts internal errors to safe messages for plugins.
// It maps known error types to user-friendly messages and logs internal errors
// at ERROR level for operators while returning a generic message to plugins.
//
// For internal errors, a correlation ID (ULID) is generated and included in both
// the log entry and the error message returned to the plugin. This allows operators
// to correlate plugin-reported errors with server logs for debugging.
func sanitizeErrorForPlugin(pluginName, entityType, entityID string, err error) string {
	if errors.Is(err, world.ErrNotFound) {
		return fmt.Sprintf("%s not found", entityType)
	}
	if errors.Is(err, world.ErrPermissionDenied) {
		return "access denied"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		slog.Warn("plugin query timed out",
			"plugin", pluginName,
			"entity_type", entityType,
			"entity_id", entityID)
		return "query timed out"
	}
	// Generate correlation ID for this error instance.
	// This allows operators to find the corresponding log entry when a plugin
	// reports an internal error to users.
	errorID := ulid.Make().String()

	// Log full error for operators, return generic message with reference ID to plugin.
	// The oops error contains full context including stack traces which
	// should not be exposed to plugins.
	slog.Error("internal error in plugin query",
		"error_id", errorID,
		"plugin", pluginName,
		"entity_type", entityType,
		"entity_id", entityID,
		"error", err)
	return fmt.Sprintf("internal error (ref: %s)", errorID)
}

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

// queryRoomFn returns a Lua function that queries room information.
func (f *Functions) queryRoomFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			return f.pushServiceUnavailable(L, "query_room", pluginName)
		}

		roomID := L.CheckString(1)
		id, ok := parseULID(L, roomID, pluginName, "query_room", "room ID")
		if !ok {
			return 2
		}

		return f.withQueryContext(L, pluginName, func(ctx context.Context, adapter *WorldQuerierAdapter) int {
			loc, err := adapter.GetLocation(ctx, id)
			if err != nil {
				if errors.Is(err, world.ErrNotFound) {
					slog.Debug("query_room: room not found",
						"plugin", pluginName,
						"room_id", roomID)
				}
				return pushError(L, sanitizeErrorForPlugin(pluginName, "room", roomID, err))
			}

			room := L.NewTable()
			L.SetField(room, "id", lua.LString(loc.ID.String()))
			L.SetField(room, "name", lua.LString(loc.Name))
			L.SetField(room, "description", lua.LString(loc.Description))
			L.SetField(room, "type", lua.LString(string(loc.Type)))

			return pushSuccess(L, room)
		})
	}
}

// queryCharacterFn returns a Lua function that queries character information.
func (f *Functions) queryCharacterFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
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
				return pushError(L, sanitizeErrorForPlugin(pluginName, "character", charID, err))
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

// queryRoomCharactersFn returns a Lua function that queries characters in a room.
// Lua signature: query_room_characters(room_id, [opts])
// opts is an optional table with:
//   - limit: max results (default: 100)
//   - offset: number of results to skip (default: 0)
func (f *Functions) queryRoomCharactersFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			return f.pushServiceUnavailable(L, "query_room_characters", pluginName)
		}

		roomID := L.CheckString(1)
		id, ok := parseULID(L, roomID, pluginName, "query_room_characters", "room ID")
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
					slog.Debug("query_room_characters: room not found",
						"plugin", pluginName,
						"room_id", roomID)
				}
				return pushError(L, sanitizeErrorForPlugin(pluginName, "room", roomID, err))
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
		if f.worldService == nil {
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
				return pushError(L, sanitizeErrorForPlugin(pluginName, "object", objID, err))
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
