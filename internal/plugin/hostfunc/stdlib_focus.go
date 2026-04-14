// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/session"
)

const (
	focusOpsKey      = "__holo_focus_ops"
	historyReaderKey = "__holo_history_reader"
)

// FocusOps is a narrow interface for focus coordinator operations exposed to Lua plugins.
type FocusOps interface {
	JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error
}

// HistoryReader provides read-only event history access for Lua plugins.
type HistoryReader interface {
	ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time) ([]core.Event, error)
}

// RegisterFocusFuncs adds holomush.join_focus, leave_focus, present_focus,
// and query_stream_history to an existing holomush module table.
func RegisterFocusFuncs(ls *lua.LState, mod *lua.LTable, fo FocusOps, hr HistoryReader) {
	if fo != nil {
		ud := ls.NewUserData()
		ud.Value = fo
		ls.SetGlobal(focusOpsKey, ud)
	}
	if hr != nil {
		ud := ls.NewUserData()
		ud.Value = hr
		ls.SetGlobal(historyReaderKey, ud)
	}

	ls.SetField(mod, "join_focus", ls.NewFunction(joinFocusFn))
	ls.SetField(mod, "leave_focus", ls.NewFunction(leaveFocusFn))
	ls.SetField(mod, "present_focus", ls.NewFunction(presentFocusFn))
	ls.SetField(mod, "query_stream_history", ls.NewFunction(queryStreamHistoryFn))
}

func getFocusOps(ls *lua.LState) FocusOps {
	ud := ls.GetGlobal(focusOpsKey)
	if ud.Type() == lua.LTUserData {
		if userData, ok := ud.(*lua.LUserData); ok {
			if fo, ok := userData.Value.(FocusOps); ok {
				return fo
			}
		}
	}
	return nil
}

func getHistoryReader(ls *lua.LState) HistoryReader {
	ud := ls.GetGlobal(historyReaderKey)
	if ud.Type() == lua.LTUserData {
		if userData, ok := ud.(*lua.LUserData); ok {
			if hr, ok := userData.Value.(HistoryReader); ok {
				return hr
			}
		}
	}
	return nil
}

func parseFocusKey(kindStr, targetIDStr string) (session.FocusKey, error) {
	targetID, err := ulid.Parse(targetIDStr)
	if err != nil {
		return session.FocusKey{}, fmt.Errorf("invalid target_id %q: %w", targetIDStr, err)
	}
	return session.FocusKey{
		Kind:     session.FocusKind(kindStr),
		TargetID: targetID,
	}, nil
}

// joinFocusFn implements holomush.join_focus(session_id, kind, target_id).
// Returns true on success; returns (nil, error_string) on failure.
func joinFocusFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	kind := ls.CheckString(2)
	targetID := ls.CheckString(3)

	fo := getFocusOps(ls)
	if fo == nil {
		slog.Warn("holomush.join_focus: focus ops not initialized")
		return 0
	}

	key, err := parseFocusKey(kind, targetID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()

	if err := fo.JoinFocus(ctx, sessionID, key); err != nil {
		slog.WarnContext(ctx, "holomush.join_focus failed",
			"session_id", sessionID, "kind", kind, "target_id", targetID, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}

// leaveFocusFn implements holomush.leave_focus(session_id, kind, target_id).
// Returns true on success; returns (nil, error_string) on failure.
func leaveFocusFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	kind := ls.CheckString(2)
	targetID := ls.CheckString(3)

	fo := getFocusOps(ls)
	if fo == nil {
		slog.Warn("holomush.leave_focus: focus ops not initialized")
		return 0
	}

	key, err := parseFocusKey(kind, targetID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()

	if err := fo.LeaveFocus(ctx, sessionID, key); err != nil {
		slog.WarnContext(ctx, "holomush.leave_focus failed",
			"session_id", sessionID, "kind", kind, "target_id", targetID, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}

// presentFocusFn implements holomush.present_focus(session_id, kind, target_id).
// Returns true on success; returns (nil, error_string) on failure.
func presentFocusFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	kind := ls.CheckString(2)
	targetID := ls.CheckString(3)

	fo := getFocusOps(ls)
	if fo == nil {
		slog.Warn("holomush.present_focus: focus ops not initialized")
		return 0
	}

	key, err := parseFocusKey(kind, targetID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()

	if err := fo.PresentFocus(ctx, sessionID, key); err != nil {
		slog.WarnContext(ctx, "holomush.present_focus failed",
			"session_id", sessionID, "kind", kind, "target_id", targetID, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}

// queryStreamHistoryFn implements holomush.query_stream_history(stream, count, [not_before_ms]).
// Returns a table of event tables on success; returns (nil, error_string) on failure.
const maxHistoryCount = 500

func queryStreamHistoryFn(ls *lua.LState) int {
	stream := ls.CheckString(1)
	count := ls.CheckInt(2)
	if count > maxHistoryCount {
		count = maxHistoryCount
	}
	if count < 0 {
		count = 0
	}
	notBeforeMs := ls.OptInt64(3, 0)

	hr := getHistoryReader(ls)
	if hr == nil {
		slog.Warn("holomush.query_stream_history: history reader not initialized")
		return 0
	}

	var notBefore time.Time
	if notBeforeMs > 0 {
		notBefore = time.UnixMilli(notBeforeMs).UTC()
	}

	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()

	events, err := hr.ReplayTail(ctx, stream, count, notBefore)
	if err != nil {
		slog.WarnContext(ctx, "holomush.query_stream_history failed",
			"stream", stream, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	result := ls.NewTable()
	for i, e := range events {
		et := ls.NewTable()
		ls.SetField(et, "id", lua.LString(e.ID.String()))
		ls.SetField(et, "stream", lua.LString(e.Stream))
		ls.SetField(et, "type", lua.LString(string(e.Type)))
		ls.SetField(et, "timestamp", lua.LNumber(e.Timestamp.UnixMilli()))
		ls.SetField(et, "actor_kind", lua.LString(e.Actor.Kind.String()))
		ls.SetField(et, "actor_id", lua.LString(e.Actor.ID))
		ls.SetField(et, "payload", lua.LString(string(e.Payload)))
		result.RawSetInt(i+1, et)
	}
	ls.Push(result)
	return 1
}
