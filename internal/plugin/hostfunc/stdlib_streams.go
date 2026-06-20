// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"log/slog"

	lua "github.com/yuin/gopher-lua"

	plugins "github.com/holomush/holomush/internal/plugin"
)

const streamRegistryKey = "__holo_stream_registry"

// RegisterStreamFuncs adds holomush.add_session_stream and holomush.remove_session_stream
// to an existing holomush module table. r may be nil; calls will be no-ops in that case.
func RegisterStreamFuncs(ls *lua.LState, holoMushTable *lua.LTable, r plugins.StreamRegistry) {
	ud := ls.NewUserData()
	ud.Value = r
	ls.SetGlobal(streamRegistryKey, ud)

	ls.SetField(holoMushTable, "add_session_stream", ls.NewFunction(addSessionStreamFn))
	ls.SetField(holoMushTable, "remove_session_stream", ls.NewFunction(removeSessionStreamFn))
}

func getStreamRegistry(ls *lua.LState) plugins.StreamRegistry {
	ud := ls.GetGlobal(streamRegistryKey)
	if ud.Type() == lua.LTUserData {
		if userData, ok := ud.(*lua.LUserData); ok {
			if r, ok := userData.Value.(plugins.StreamRegistry); ok {
				return r
			}
		}
	}
	return nil
}

// addSessionStreamFn implements holomush.add_session_stream(session_id, stream).
// Returns true on success; returns (nil, error_message) on failure.
func addSessionStreamFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	stream := ls.CheckString(2)

	r := getStreamRegistry(ls)
	if r == nil {
		slog.WarnContext(luaContext(ls), "holomush.add_session_stream: stream registry not initialized")
		return 0
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := r.AddStream(ctx, sessionID, stream); err != nil {
		slog.WarnContext(ctx, "holomush.add_session_stream failed",
			"session_id", sessionID, "stream", stream, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}

// removeSessionStreamFn implements holomush.remove_session_stream(session_id, stream).
// Returns true on success; returns (nil, error_message) on failure.
func removeSessionStreamFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	stream := ls.CheckString(2)

	r := getStreamRegistry(ls)
	if r == nil {
		slog.WarnContext(luaContext(ls), "holomush.remove_session_stream: stream registry not initialized")
		return 0
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if err := r.RemoveStream(ctx, sessionID, stream); err != nil {
		slog.WarnContext(ctx, "holomush.remove_session_stream failed",
			"session_id", sessionID, "stream", stream, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}
