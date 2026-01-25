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

	// GetCharactersByLocation retrieves all characters at a location.
	GetCharactersByLocation(ctx context.Context, locationID ulid.ULID) ([]*world.Character, error)
}

// queryRoomFn returns a Lua function that queries room information.
func (f *Functions) queryRoomFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		if f.world == nil {
			slog.Error("query_room called but world querier unavailable",
				"plugin", pluginName)
			L.Push(lua.LNil)
			L.Push(lua.LString("world querier not configured"))
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

		loc, err := f.world.GetLocation(ctx, id)
		if err != nil {
			if errors.Is(err, world.ErrNotFound) {
				slog.Debug("query_room: room not found",
					"plugin", pluginName,
					"room_id", roomID)
			} else {
				slog.Error("query_room failed",
					"plugin", pluginName,
					"room_id", roomID,
					"error", err)
			}
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
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
		if f.world == nil {
			slog.Error("query_character called but world querier unavailable",
				"plugin", pluginName)
			L.Push(lua.LNil)
			L.Push(lua.LString("world querier not configured"))
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

		char, err := f.world.GetCharacter(ctx, id)
		if err != nil {
			if errors.Is(err, world.ErrNotFound) {
				slog.Debug("query_character: character not found",
					"plugin", pluginName,
					"character_id", charID)
			} else {
				slog.Error("query_character failed",
					"plugin", pluginName,
					"character_id", charID,
					"error", err)
			}
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Return character info as a table
		character := L.NewTable()
		L.SetField(character, "id", lua.LString(char.ID.String()))
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
		if f.world == nil {
			slog.Error("query_room_characters called but world querier unavailable",
				"plugin", pluginName)
			L.Push(lua.LNil)
			L.Push(lua.LString("world querier not configured"))
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

		chars, err := f.world.GetCharactersByLocation(ctx, id)
		if err != nil {
			if errors.Is(err, world.ErrNotFound) {
				slog.Debug("query_room_characters: room not found",
					"plugin", pluginName,
					"room_id", roomID)
			} else {
				slog.Error("query_room_characters failed",
					"plugin", pluginName,
					"room_id", roomID,
					"error", err)
			}
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}

		// Return array of character info
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
