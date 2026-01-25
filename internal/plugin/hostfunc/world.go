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

	// GetCharactersByLocation retrieves all characters at a location.
	GetCharactersByLocation(ctx context.Context, locationID ulid.ULID) ([]*world.Character, error)

	// GetObject retrieves an object by ID.
	GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error)
}

// queryRoomFn returns a Lua function that queries room information.
func (f *Functions) queryRoomFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("query_room called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		roomID := L.CheckString(1)
		id, err := ulid.Parse(roomID)
		if err != nil {
			slog.Debug("query_room: invalid room ID format",
				"plugin", pluginName,
				"room_id", roomID,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid room ID: " + err.Error()))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		// Create adapter for this plugin's authorization
		adapter := NewWorldQuerierAdapter(f.worldService, pluginName)
		loc, err := adapter.GetLocation(ctx, id)
		if err != nil {
			if errors.Is(err, world.ErrNotFound) {
				slog.Debug("query_room: room not found",
					"plugin", pluginName,
					"room_id", roomID)
			}
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "room", roomID, err)))
			return 2
		}

		// Return room info as a table
		room := L.NewTable()
		L.SetField(room, "id", lua.LString(loc.ID.String()))
		L.SetField(room, "name", lua.LString(loc.Name))
		L.SetField(room, "description", lua.LString(loc.Description))
		L.SetField(room, "type", lua.LString(string(loc.Type)))

		L.Push(room)
		L.Push(lua.LNil)
		return 2
	}
}

// queryCharacterFn returns a Lua function that queries character information.
func (f *Functions) queryCharacterFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("query_character called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		charID := L.CheckString(1)
		id, err := ulid.Parse(charID)
		if err != nil {
			slog.Debug("query_character: invalid character ID format",
				"plugin", pluginName,
				"character_id", charID,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid character ID: " + err.Error()))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		// Create adapter for this plugin's authorization
		adapter := NewWorldQuerierAdapter(f.worldService, pluginName)
		char, err := adapter.GetCharacter(ctx, id)
		if err != nil {
			if errors.Is(err, world.ErrNotFound) {
				slog.Debug("query_character: character not found",
					"plugin", pluginName,
					"character_id", charID)
			}
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "character", charID, err)))
			return 2
		}

		// Return character info as a table
		character := L.NewTable()
		L.SetField(character, "id", lua.LString(char.ID.String()))
		L.SetField(character, "player_id", lua.LString(char.PlayerID.String()))
		L.SetField(character, "name", lua.LString(char.Name))
		L.SetField(character, "description", lua.LString(char.Description))
		if char.LocationID != nil {
			L.SetField(character, "location_id", lua.LString(char.LocationID.String()))
		}

		L.Push(character)
		L.Push(lua.LNil)
		return 2
	}
}

// queryRoomCharactersFn returns a Lua function that queries characters in a room.
func (f *Functions) queryRoomCharactersFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("query_room_characters called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		roomID := L.CheckString(1)
		id, err := ulid.Parse(roomID)
		if err != nil {
			slog.Debug("query_room_characters: invalid room ID format",
				"plugin", pluginName,
				"room_id", roomID,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid room ID: " + err.Error()))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		// Create adapter for this plugin's authorization
		adapter := NewWorldQuerierAdapter(f.worldService, pluginName)
		chars, err := adapter.GetCharactersByLocation(ctx, id)
		if err != nil {
			if errors.Is(err, world.ErrNotFound) {
				slog.Debug("query_room_characters: room not found",
					"plugin", pluginName,
					"room_id", roomID)
			}
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "room", roomID, err)))
			return 2
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

		L.Push(characters)
		L.Push(lua.LNil)
		return 2
	}
}

// queryObjectFn returns a Lua function that queries object information.
func (f *Functions) queryObjectFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.worldService == nil {
			slog.Error("query_object called but world service unavailable",
				"plugin", pluginName,
				"hint", "use WithWorldService option when creating hostfunc.Functions")
			L.Push(lua.LNil)
			L.Push(lua.LString("world service not configured - contact server administrator"))
			return 2
		}

		objID := L.CheckString(1)
		id, err := ulid.Parse(objID)
		if err != nil {
			slog.Debug("query_object: invalid object ID format",
				"plugin", pluginName,
				"object_id", objID,
				"error", err)
			L.Push(lua.LNil)
			L.Push(lua.LString("invalid object ID: " + err.Error()))
			return 2
		}

		ctx, cancel := context.WithTimeout(context.Background(), kvTimeout)
		defer cancel()

		// Create adapter for this plugin's authorization
		adapter := NewWorldQuerierAdapter(f.worldService, pluginName)
		obj, err := adapter.GetObject(ctx, id)
		if err != nil {
			if errors.Is(err, world.ErrNotFound) {
				slog.Debug("query_object: object not found",
					"plugin", pluginName,
					"object_id", objID)
			}
			L.Push(lua.LNil)
			L.Push(lua.LString(sanitizeErrorForPlugin(pluginName, "object", objID, err)))
			return 2
		}

		// Return object info as a table
		object := L.NewTable()
		L.SetField(object, "id", lua.LString(obj.ID.String()))
		L.SetField(object, "name", lua.LString(obj.Name))
		L.SetField(object, "description", lua.LString(obj.Description))
		L.SetField(object, "is_container", lua.LBool(obj.IsContainer))

		// Optional fields - only set if non-nil
		if obj.LocationID != nil {
			L.SetField(object, "location_id", lua.LString(obj.LocationID.String()))
		}
		if obj.HeldByCharacterID != nil {
			L.SetField(object, "held_by_character_id", lua.LString(obj.HeldByCharacterID.String()))
		}
		if obj.ContainedInObjectID != nil {
			L.SetField(object, "contained_in_object_id", lua.LString(obj.ContainedInObjectID.String()))
		}
		if obj.OwnerID != nil {
			L.SetField(object, "owner_id", lua.LString(obj.OwnerID.String()))
		}

		// Containment type
		containment := obj.Containment()
		L.SetField(object, "containment_type", lua.LString(string((&containment).Type())))

		L.Push(object)
		L.Push(lua.LNil)
		return 2
	}
}
