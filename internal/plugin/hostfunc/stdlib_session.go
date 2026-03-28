// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/session"
)

// sessionAccessRegistryKey is used to store the session.Access in Lua's registry.
const sessionAccessRegistryKey = "__holo_session_access"

// RegisterSessionFuncs adds the holo.session.* namespace to an existing holoTable.
// It stores the session.Access in the Lua state registry so individual host
// functions can retrieve it without needing a Go closure over the interface.
func RegisterSessionFuncs(ls *lua.LState, holoTable *lua.LTable, sa session.Access) {
	// Store session.Access in the Lua state registry.
	ud := ls.NewUserData()
	ud.Value = sa
	ls.SetGlobal(sessionAccessRegistryKey, ud)

	sessionMod := ls.NewTable()
	ls.SetField(sessionMod, "find_by_name", ls.NewFunction(sessionFindByName))
	ls.SetField(sessionMod, "set_last_whispered", ls.NewFunction(sessionSetLastWhispered))
	ls.SetField(holoTable, "session", sessionMod)
}

// getSessionAccess retrieves the session.Access from the Lua state registry.
// Returns nil if not found, which indicates RegisterSessionFuncs was not called.
func getSessionAccess(ls *lua.LState) session.Access {
	ud := ls.GetGlobal(sessionAccessRegistryKey)
	if ud.Type() == lua.LTUserData {
		userData, ok := ud.(*lua.LUserData)
		if ok {
			if sa, saOK := userData.Value.(session.Access); saOK {
				return sa
			}
		}
	}
	slog.Error("session access not found in Lua state registry",
		"registry_key", sessionAccessRegistryKey,
		"hint", "RegisterSessionFuncs must be called before holo.session functions")
	return nil
}

// sessionFindByName implements holo.session.find_by_name(name).
// Returns a table {character_id, character_name, location_id} or nil if not found.
//
// Lua signature: result = holo.session.find_by_name(name)
func sessionFindByName(ls *lua.LState) int {
	name := ls.CheckString(1)

	sa := getSessionAccess(ls)
	if sa == nil {
		ls.RaiseError("holo.session: session access not initialized (RegisterSessionFuncs not called)")
		return 0
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	info, err := sa.FindByCharacterName(ctx, name)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	if info == nil {
		ls.Push(lua.LNil)
		return 1
	}

	t := ls.NewTable()
	ls.SetField(t, "character_id", lua.LString(info.CharacterID.String()))
	ls.SetField(t, "character_name", lua.LString(info.CharacterName))
	ls.SetField(t, "location_id", lua.LString(info.LocationID.String()))
	ls.Push(t)
	return 1
}

// sessionSetLastWhispered implements holo.session.set_last_whispered(session_id, name).
// Updates the caller's session to record the last character whispered to.
//
// Lua signature: holo.session.set_last_whispered(session_id, name)
func sessionSetLastWhispered(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	name := ls.CheckString(2)

	sa := getSessionAccess(ls)
	if sa == nil {
		ls.RaiseError("holo.session: session access not initialized (RegisterSessionFuncs not called)")
		return 0
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := sa.UpdateLastWhispered(ctx, sessionID, name); err != nil {
		slog.WarnContext(ctx, "holo.session.set_last_whispered: update failed",
			"session_id", sessionID,
			"name", name,
			"error", err)
	}

	return 0
}
