// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/cursor"
	"github.com/holomush/holomush/internal/session"
)

const (
	focusOpsKey      = "__holo_focus_ops"
	historyReaderKey = "__holo_history_reader"
)

// FocusFailure mirrors the proto FocusFailure shape for Lua serialization.
type FocusFailure struct {
	ConnectionID ulid.ULID
	Reason       string // "membership_absent" | "connection_not_found"
}

// FocusOps is a narrow interface for focus coordinator operations exposed to Lua plugins.
type FocusOps interface {
	JoinFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	LeaveFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	LeaveFocusByTarget(ctx context.Context, target session.FocusKey) (session.LeaveByTargetResult, error)
	PresentFocus(ctx context.Context, sessionID string, target session.FocusKey) error
	// Phase 5 additions (INV-SCENE-19, D6):
	SetConnectionFocus(ctx context.Context, connectionID ulid.ULID, focusKey *session.FocusKey, isSceneGrid bool) error
	AutoFocusOnJoin(ctx context.Context, characterID, sceneID ulid.ULID) (focused, skipped []ulid.ULID, failed []FocusFailure, totalConnCount uint32, err error)
	IsAnyConnFocused(ctx context.Context, characterID, sceneID ulid.ULID) (bool, error)
	// GetConnectionFocus returns the current FocusKey for the given connection,
	// or nil when grid-focused or unknown. Ships paired with the Go SDK method
	// (runtime-symmetry parity with SetConnectionFocus).
	GetConnectionFocus(ctx context.Context, connectionID ulid.ULID) (*session.FocusKey, error)
}

// HistoryReader provides read-only event history access for Lua plugins.
type HistoryReader interface {
	ReplayTail(ctx context.Context, stream string, count int, notBefore time.Time, beforeID ulid.ULID) ([]eventbus.Event, error)
}

// actorIDString renders an event actor's ULID for the plugin-facing Lua
// table, mapping the zero ULID (anonymous/system-unset) to the empty string
// rather than its 26-char Crockford text. Mirrors the deleted
// busEventToCoreEvent's deliberate zero→"" mapping
// (cmd/holomush/sub_grpc.go, pre-ARCH-04) and the precedent at
// internal/grpc/server.go's actorIDString — a bare ulid.ULID.String() would
// surface the zero actor as "00000000000000000000000000", an observable
// behavior change plugins do not expect (cross-AI round 7, MEDIUM).
func actorIDString(id ulid.ULID) string {
	if id == (ulid.ULID{}) {
		return ""
	}
	return id.String()
}

// RegisterStreamHistoryFunc adds ONLY holomush.query_stream_history (the
// stream.history read-back) to an existing holomush module table, wiring the
// history reader global it reads. It is the narrow slice of the former
// RegisterFocusFuncs that survives the atomic capability cutover
// (holomush-eykuh.4): the `focus` capability functions are retired to the
// host-brokered path (spec R1 / ADR holomush-05f3v), but `stream.history` is
// NOT one of the ten retired capabilities and stays ambient.
func RegisterStreamHistoryFunc(ls *lua.LState, mod *lua.LTable, hr HistoryReader) {
	if hr != nil {
		ud := ls.NewUserData()
		ud.Value = hr
		ls.SetGlobal(historyReaderKey, ud)
	}
	ls.SetField(mod, "query_stream_history", ls.NewFunction(queryStreamHistoryFn))
}

// RegisterFocusFuncs adds holomush.join_focus, leave_focus, present_focus,
// and query_stream_history to an existing holomush module table.
//
// Deprecated for the per-delivery Register path after the atomic capability
// cutover (holomush-eykuh.4): the `focus` capability functions are now host-
// brokered (spec R1). Retained for direct unit-test coverage of the focus
// hostfuncs; production Register installs only RegisterStreamHistoryFunc.
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
	ls.SetField(mod, "leave_focus_by_target", ls.NewFunction(leaveFocusByTargetFn))
	ls.SetField(mod, "present_focus", ls.NewFunction(presentFocusFn))
	ls.SetField(mod, "query_stream_history", ls.NewFunction(queryStreamHistoryFn))
	// Phase 5 additions:
	ls.SetField(mod, "set_connection_focus", ls.NewFunction(setConnectionFocusFn))
	ls.SetField(mod, "auto_focus_on_join", ls.NewFunction(autoFocusOnJoinFn))
	ls.SetField(mod, "is_any_conn_focused", ls.NewFunction(isAnyConnFocusedFn))
	ls.SetField(mod, "get_connection_focus", ls.NewFunction(getConnectionFocusFn))
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
		slog.WarnContext(luaContext(ls), "holomush.join_focus: focus ops not initialized")
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
		slog.WarnContext(luaContext(ls), "holomush.leave_focus: focus ops not initialized")
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

// leaveFocusByTargetFn implements holomush.leave_focus_by_target(kind, target_id).
// Sweeps every non-expired session holding the given focus membership.
//
// On enumeration success, returns a single Lua table:
//
//	{ succeeded = N, total_scanned = M, failed = { {session_id=..., error=...}, ... } }
//
// Partial sweep outcomes (some sessions failed) are represented on the
// table via a non-empty failed array — this removes the ambiguity of a
// scalar (count, err) shape where Lua callers could confuse "nothing
// matched" with "everything failed."
//
// On enumeration failure (store degraded, e.g.), returns (nil, error_string)
// as with the other leave_* hostfuncs.
func leaveFocusByTargetFn(ls *lua.LState) int {
	kind := ls.CheckString(1)
	targetID := ls.CheckString(2)

	fo := getFocusOps(ls)
	if fo == nil {
		slog.WarnContext(luaContext(ls), "holomush.leave_focus_by_target: focus ops not initialized")
		ls.Push(lua.LNil)
		ls.Push(lua.LString("focus ops not initialized"))
		return 2
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

	result, err := fo.LeaveFocusByTarget(ctx, key)
	if err != nil {
		slog.WarnContext(ctx, "holomush.leave_focus_by_target failed",
			"kind", kind, "target_id", targetID, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(leaveByTargetResultToLuaTable(ls, result))
	return 1
}

// leaveByTargetResultToLuaTable converts a session.LeaveByTargetResult
// into a Lua table with succeeded / total_scanned / failed fields.
func leaveByTargetResultToLuaTable(ls *lua.LState, r session.LeaveByTargetResult) *lua.LTable {
	tbl := ls.NewTable()
	ls.SetField(tbl, "succeeded", lua.LNumber(r.Succeeded))
	ls.SetField(tbl, "total_scanned", lua.LNumber(r.TotalScanned))
	failed := ls.NewTable()
	for i, f := range r.Failed {
		entry := ls.NewTable()
		ls.SetField(entry, "session_id", lua.LString(f.SessionID))
		if f.Err != nil {
			ls.SetField(entry, "error", lua.LString(f.Err.Error()))
		}
		failed.RawSetInt(i+1, entry)
	}
	ls.SetField(tbl, "failed", failed)
	return tbl
}

// presentFocusFn implements holomush.present_focus(session_id, kind, target_id).
// Returns true on success; returns (nil, error_string) on failure.
func presentFocusFn(ls *lua.LState) int {
	sessionID := ls.CheckString(1)
	kind := ls.CheckString(2)
	targetID := ls.CheckString(3)

	fo := getFocusOps(ls)
	if fo == nil {
		slog.WarnContext(luaContext(ls), "holomush.present_focus: focus ops not initialized")
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

// queryStreamHistoryFn implements holomush.query_stream_history({stream, count, cursor, not_before_ms}).
// The single argument is a Lua table with fields:
//
//	stream       string  (required) — stream name
//	count        int     (required) — maximum events to return (server clamps to 500)
//	cursor       string  (optional) — opaque base64-encoded pagination cursor from a
//	                                  previous result.next_cursor; nil/absent for first page
//	not_before_ms int64  (optional) — Unix millisecond floor; events before this time
//	                                  are excluded; 0 or absent means no floor
//
// On success returns a table:
//
//	{
//	  events     = { { id, stream, type, timestamp, actor_kind, actor_id, payload, cursor }, ... },
//	  has_more   = bool,
//	  next_cursor = string|nil,  -- base64-encoded; nil when no further pages exist
//	}
//
// Each event's cursor field is base64-encoded and may be passed as cursor on the
// next call to page backward. On failure returns (nil, error_string).
const maxHistoryCount = 500

func queryStreamHistoryFn(ls *lua.LState) int {
	args := ls.CheckTable(1)

	streamVal := ls.GetField(args, "stream")
	if streamVal == lua.LNil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("query_stream_history: missing required field 'stream'"))
		return 2
	}
	stream := lua.LVAsString(streamVal)

	countVal := ls.GetField(args, "count")
	if countVal == lua.LNil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("query_stream_history: missing required field 'count'"))
		return 2
	}
	count := int(lua.LVAsNumber(countVal))
	if count > maxHistoryCount {
		count = maxHistoryCount
	}
	if count < 0 {
		count = 0
	}

	// Decode optional cursor from base64.
	var beforeID ulid.ULID
	cursorVal := ls.GetField(args, "cursor")
	if cursorVal != lua.LNil && cursorVal != lua.LFalse {
		cursorB64 := lua.LVAsString(cursorVal)
		if cursorB64 != "" {
			cursorBytes, decErr := base64.StdEncoding.DecodeString(cursorB64)
			if decErr != nil {
				ls.Push(lua.LNil)
				ls.Push(lua.LString("query_stream_history: invalid cursor (base64 decode failed): " + decErr.Error()))
				return 2
			}
			if len(cursorBytes) > 0 {
				c, decErr := cursor.Decode(cursorBytes)
				if decErr != nil {
					ls.Push(lua.LNil)
					ls.Push(lua.LString("query_stream_history: invalid cursor: " + decErr.Error()))
					return 2
				}
				if c.Host != nil {
					beforeID = c.Host.ID
				}
			}
		}
	}

	notBeforeMsVal := ls.GetField(args, "not_before_ms")
	notBeforeMs := int64(0)
	if notBeforeMsVal != lua.LNil {
		notBeforeMs = int64(lua.LVAsNumber(notBeforeMsVal))
	}

	hr := getHistoryReader(ls)
	if hr == nil {
		slog.WarnContext(luaContext(ls), "holomush.query_stream_history: history reader not initialized")
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

	events, err := hr.ReplayTail(ctx, stream, count, notBefore, beforeID)
	if err != nil {
		slog.WarnContext(ctx, "holomush.query_stream_history failed",
			"stream", stream, "error", err)
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}

	eventsTable := ls.NewTable()
	for i := range events {
		e := &events[i]
		et := ls.NewTable()
		ls.SetField(et, "id", lua.LString(e.ID.String()))
		ls.SetField(et, "stream", lua.LString(string(e.Subject)))
		ls.SetField(et, "type", lua.LString(string(e.Type)))
		ls.SetField(et, "timestamp", lua.LNumber(e.Timestamp.UnixMilli()))
		ls.SetField(et, "actor_kind", lua.LString(e.Actor.Kind.String()))
		ls.SetField(et, "actor_id", lua.LString(actorIDString(e.Actor.ID)))
		ls.SetField(et, "payload", lua.LString(string(e.Payload)))
		// Encode a per-event cursor so callers can paginate from this event.
		evtCursorBytes, encErr := cursor.Encode(cursor.Cursor{
			Version: cursor.CurrentVersion,
			Epoch:   cursor.CurrentEpoch(),
			Owner:   cursor.Owner{Kind: cursor.OwnerHost},
			Host:    &cursor.HostCursor{Seq: 0, ID: e.ID},
		})
		if encErr == nil {
			ls.SetField(et, "cursor", lua.LString(base64.StdEncoding.EncodeToString(evtCursorBytes)))
		} else {
			slog.DebugContext(ctx, "holomush.query_stream_history: failed to encode per-event cursor",
				"event_id", e.ID, "cursor_kind", "event", "error", encErr)
		}
		eventsTable.RawSetInt(i+1, et)
	}

	// next_cursor: non-empty when a full page was returned (indicating more pages exist).
	// The oldest event (index 0, ascending order) is the backward-pagination anchor.
	hasMore := len(events) == count && count > 0
	result := ls.NewTable()
	ls.SetField(result, "events", eventsTable)
	ls.SetField(result, "has_more", lua.LBool(hasMore))
	if hasMore && len(events) > 0 {
		nextCursorBytes, encErr := cursor.Encode(cursor.Cursor{
			Version: cursor.CurrentVersion,
			Epoch:   cursor.CurrentEpoch(),
			Owner:   cursor.Owner{Kind: cursor.OwnerHost},
			Host:    &cursor.HostCursor{Seq: 0, ID: events[0].ID},
		})
		if encErr == nil {
			ls.SetField(result, "next_cursor", lua.LString(base64.StdEncoding.EncodeToString(nextCursorBytes)))
		} else {
			slog.DebugContext(ctx, "holomush.query_stream_history: failed to encode next_cursor",
				"event_id", events[0].ID, "cursor_kind", "next", "error", encErr)
		}
	}
	ls.Push(result)
	return 1
}

// setConnectionFocusFn implements holomush.set_connection_focus(connection_id_str, {kind, target_id}|nil, is_scene_grid_bool).
// Returns true on success; returns (nil, error_string) on failure.
func setConnectionFocusFn(ls *lua.LState) int {
	fo := getFocusOps(ls)
	if fo == nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("focus_ops not registered"))
		return 2
	}
	connIDStr := ls.CheckString(1)
	connID, err := ulid.Parse(connIDStr)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
		return 2
	}
	var fk *session.FocusKey
	if ls.Get(2) != lua.LNil {
		// Lua table {kind="scene", target_id="..."}
		tbl := ls.CheckTable(2)
		kind := tbl.RawGetString("kind").String()
		targetIDStr := tbl.RawGetString("target_id").String()
		targetID, parseErr := ulid.Parse(targetIDStr)
		if parseErr != nil {
			ls.Push(lua.LNil)
			ls.Push(lua.LString("INVALID_ULID: " + parseErr.Error()))
			return 2
		}
		fk = &session.FocusKey{Kind: session.FocusKind(kind), TargetID: targetID}
	}
	isSceneGrid := ls.OptBool(3, false)
	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()
	if err := fo.SetConnectionFocus(ctx, connID, fk, isSceneGrid); err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LTrue)
	return 1
}

// autoFocusOnJoinFn implements holomush.auto_focus_on_join(character_id_str, scene_id_str).
// Returns a Lua table {focused_connection_ids, skipped_connection_ids, failed_connection_ids, total_connection_count}.
// Returns (nil, error_string) on failure.
func autoFocusOnJoinFn(ls *lua.LState) int {
	fo := getFocusOps(ls)
	if fo == nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("focus_ops not registered"))
		return 2
	}
	charIDStr := ls.CheckString(1)
	sceneIDStr := ls.CheckString(2)
	charID, err := ulid.Parse(charIDStr)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
		return 2
	}
	sceneID, err := ulid.Parse(sceneIDStr)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
		return 2
	}
	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()
	focused, skipped, failed, total, err := fo.AutoFocusOnJoin(ctx, charID, sceneID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	// Return a Lua table mirroring the proto response shape so plugin
	// authors can render the 4 branches in §7.4.
	resp := ls.NewTable()
	focusedTbl := ls.NewTable()
	for i, id := range focused {
		focusedTbl.RawSetInt(i+1, lua.LString(id.String()))
	}
	resp.RawSetString("focused_connection_ids", focusedTbl)

	skippedTbl := ls.NewTable()
	for i, id := range skipped {
		skippedTbl.RawSetInt(i+1, lua.LString(id.String()))
	}
	resp.RawSetString("skipped_connection_ids", skippedTbl)

	failedTbl := ls.NewTable()
	for i, f := range failed {
		entry := ls.NewTable()
		entry.RawSetString("connection_id", lua.LString(f.ConnectionID.String()))
		entry.RawSetString("reason", lua.LString(f.Reason))
		failedTbl.RawSetInt(i+1, entry)
	}
	resp.RawSetString("failed_connection_ids", failedTbl)

	resp.RawSetString("total_connection_count", lua.LNumber(total))

	ls.Push(resp)
	return 1
}

// isAnyConnFocusedFn implements holomush.is_any_conn_focused(character_id_str, scene_id_str).
// Returns bool on success; returns (nil, error_string) on failure.
func isAnyConnFocusedFn(ls *lua.LState) int {
	fo := getFocusOps(ls)
	if fo == nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("focus_ops not registered"))
		return 2
	}
	charIDStr := ls.CheckString(1)
	sceneIDStr := ls.CheckString(2)
	charID, err := ulid.Parse(charIDStr)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
		return 2
	}
	sceneID, err := ulid.Parse(sceneIDStr)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
		return 2
	}
	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()
	isFocused, err := fo.IsAnyConnFocused(ctx, charID, sceneID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	ls.Push(lua.LBool(isFocused))
	return 1
}

// getConnectionFocusFn implements holomush.get_connection_focus(connection_id_str).
// Returns {kind=..., target_id=...} for a focused connection, or nil for
// grid-focused or unknown connections (absent = grid). On error returns (nil, error_string).
func getConnectionFocusFn(ls *lua.LState) int {
	fo := getFocusOps(ls)
	if fo == nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("focus_ops not registered"))
		return 2
	}
	connIDStr := ls.CheckString(1)
	connID, err := ulid.Parse(connIDStr)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString("INVALID_ULID: " + err.Error()))
		return 2
	}
	ctx := ls.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, defaultPluginQueryTimeout)
	defer cancel()
	fk, err := fo.GetConnectionFocus(ctx, connID)
	if err != nil {
		ls.Push(lua.LNil)
		ls.Push(lua.LString(err.Error()))
		return 2
	}
	if fk == nil {
		// Grid-focused or unknown connection — return nil (no focus key).
		ls.Push(lua.LNil)
		return 1
	}
	result := ls.NewTable()
	ls.SetField(result, "kind", lua.LString(string(fk.Kind)))
	ls.SetField(result, "target_id", lua.LString(fk.TargetID.String()))
	ls.Push(result)
	return 1
}
