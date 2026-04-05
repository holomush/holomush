// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	lua "github.com/yuin/gopher-lua"
)

// SessionInfo carries session data from the host to Lua plugins.
// It is a flat, string-keyed struct to avoid coupling plugins to internal types.
type SessionInfo struct {
	ID            string
	CharacterID   string
	CharacterName string
	LocationID    string
	GridPresent   bool
	LastWhispered string
}

// SessionAccess is the narrow interface required by SessionCapability.
// It covers the session operations needed by the capability module without
// exposing the full session or host service surface.
type SessionAccess interface {
	// FindSessionByName returns the active session for a character by name (case-insensitive).
	// Returns nil, nil when no session is found.
	FindSessionByName(ctx context.Context, name string) (*SessionInfo, error)

	// ListActiveSessions returns all currently active sessions.
	ListActiveSessions(ctx context.Context) ([]SessionInfo, error)

	// BroadcastSystemMessage sends a system message to all active sessions.
	BroadcastSystemMessage(ctx context.Context, message string) error

	// SetLastWhispered records the last whisper target on a session.
	SetLastWhispered(ctx context.Context, sessionID, name string) error

	// DisconnectSession forcibly disconnects a session with a reason.
	DisconnectSession(ctx context.Context, sessionID, reason string) error
}

// SessionCapability implements the Capability interface for the session namespace.
// It registers Lua host functions under the global "session" table.
type SessionCapability struct {
	sessions SessionAccess
}

// NewSessionCapability creates a SessionCapability backed by the given SessionAccess.
func NewSessionCapability(sessions SessionAccess) *SessionCapability {
	return &SessionCapability{sessions: sessions}
}

// Namespace returns "session", the Lua global table name for this capability.
func (c *SessionCapability) Namespace() string {
	return "session"
}

// Register injects the session.* functions into the Lua state as a global table.
func (c *SessionCapability) Register(L *lua.LState, pluginName string) { //nolint:gocritic // L is conventional gopher-lua parameter name
	tbl := L.NewTable()
	L.SetField(tbl, "find_by_name", L.NewFunction(c.findByNameFn(pluginName)))
	L.SetField(tbl, "set_last_whispered", L.NewFunction(c.setLastWhisperedFn(pluginName)))
	L.SetField(tbl, "list_active", L.NewFunction(c.listActiveFn(pluginName)))
	L.SetField(tbl, "broadcast", L.NewFunction(c.broadcastFn(pluginName)))
	L.SetField(tbl, "disconnect", L.NewFunction(c.disconnectFn(pluginName)))
	L.SetGlobal("session", tbl)
}

// findByNameFn returns a Lua function implementing session.find_by_name(name).
// Returns a table on success, nil on not-found, or nil + error string on failure.
func (c *SessionCapability) findByNameFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		name := L.CheckString(1)

		ctx := luaContext(L)
		info, err := c.sessions.FindSessionByName(ctx, name)
		if err != nil {
			msg := SanitizeErrorForPlugin(PluginErrorContext{
				Plugin:    pluginName,
				Operation: "find_by_name",
				Subject:   "session",
				SubjectID: name,
			}, err)
			L.Push(lua.LNil)
			L.Push(lua.LString(msg))
			return 2
		}
		if info == nil {
			L.Push(lua.LNil)
			return 1
		}

		tbl := L.NewTable()
		L.SetField(tbl, "id", lua.LString(info.ID))
		L.SetField(tbl, "character_id", lua.LString(info.CharacterID))
		L.SetField(tbl, "character_name", lua.LString(info.CharacterName))
		L.SetField(tbl, "location_id", lua.LString(info.LocationID))
		L.SetField(tbl, "grid_present", lua.LBool(info.GridPresent))
		L.SetField(tbl, "last_whispered", lua.LString(info.LastWhispered))
		L.Push(tbl)
		return 1
	}
}

// setLastWhisperedFn returns a Lua function implementing session.set_last_whispered(session_id, name).
// Errors are logged and swallowed — consistent with the existing stdlib_session.go behaviour.
func (c *SessionCapability) setLastWhisperedFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		sessionID := L.CheckString(1)
		name := L.CheckString(2)

		ctx := luaContext(L)
		if err := c.sessions.SetLastWhispered(ctx, sessionID, name); err != nil {
			slog.WarnContext(ctx, "session.set_last_whispered: update failed",
				"plugin", pluginName,
				"session_id", sessionID,
				"name", name,
				"error", err)
		}
		return 0
	}
}

// listActiveFn returns a Lua function implementing session.list_active().
// Returns an array table of session tables, or nil + error string on failure.
func (c *SessionCapability) listActiveFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		ctx := luaContext(L)
		sessions, err := c.sessions.ListActiveSessions(ctx)
		if err != nil {
			msg := SanitizeErrorForPlugin(PluginErrorContext{
				Plugin:    pluginName,
				Operation: "list_active",
				Subject:   "session",
			}, err)
			L.Push(lua.LNil)
			L.Push(lua.LString(msg))
			return 2
		}

		arr := L.NewTable()
		for _, s := range sessions {
			row := L.NewTable()
			L.SetField(row, "id", lua.LString(s.ID))
			L.SetField(row, "character_id", lua.LString(s.CharacterID))
			L.SetField(row, "character_name", lua.LString(s.CharacterName))
			L.SetField(row, "location_id", lua.LString(s.LocationID))
			L.SetField(row, "grid_present", lua.LBool(s.GridPresent))
			L.SetField(row, "last_whispered", lua.LString(s.LastWhispered))
			arr.Append(row)
		}
		L.Push(arr)
		return 1
	}
}

// broadcastFn returns a Lua function implementing session.broadcast(message).
// Returns nil on success, or nil + error string on failure.
func (c *SessionCapability) broadcastFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		message := L.CheckString(1)

		ctx := luaContext(L)
		if err := c.sessions.BroadcastSystemMessage(ctx, message); err != nil {
			msg := SanitizeErrorForPlugin(PluginErrorContext{
				Plugin:    pluginName,
				Operation: "broadcast",
				Subject:   "session",
			}, err)
			L.Push(lua.LNil)
			L.Push(lua.LString(msg))
			return 2
		}
		return 0
	}
}

// disconnectFn returns a Lua function implementing session.disconnect(session_id, reason).
// Returns nil on success, or nil + error string on failure.
func (c *SessionCapability) disconnectFn(pluginName string) lua.LGFunction {
	return func(L *lua.LState) int {
		sessionID := L.CheckString(1)
		reason := L.CheckString(2)

		ctx := luaContext(L)
		if err := c.sessions.DisconnectSession(ctx, sessionID, reason); err != nil {
			msg := SanitizeErrorForPlugin(PluginErrorContext{
				Plugin:    pluginName,
				Operation: "disconnect",
				Subject:   "session",
				SubjectID: sessionID,
			}, err)
			L.Push(lua.LNil)
			L.Push(lua.LString(msg))
			return 2
		}
		return 0
	}
}

// luaContext returns the context from the Lua state, falling back to Background.
func luaContext(L *lua.LState) context.Context { //nolint:gocritic // L is conventional gopher-lua parameter name
	if ctx := L.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
