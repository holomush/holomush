// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corecomm_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	commv1 "github.com/holomush/holomush/pkg/proto/holomush/comm/v1"
)

// stubHolomush installs a minimal `holomush` global carrying just what
// main.lua's say/pose/ooc/emit path needs: register_emit_type (called at
// module-load time for the INV-PLUGIN-32 emit-type registration block) and log
// (used by handlers outside this harness's scope, e.g. page/whisper/wall) —
// mirroring plugins/core-help/help_lua_test.go's registerHolomushStub.
func stubHolomush(L *lua.LState) {
	holomush := L.NewTable()
	L.SetField(holomush, "log", L.NewFunction(func(_ *lua.LState) int { return 0 }))
	L.SetField(holomush, "register_emit_type", L.NewFunction(func(_ *lua.LState) int { return 0 }))
	L.SetGlobal("holomush", holomush)
}

// runCommand loads the real plugins/core-communication/main.lua into a fresh
// gopher-lua state wired with the real holo.comm.* stdlib
// (hostfunc.RegisterStdlib) and a stubbed holomush global, then invokes the
// host's real on_command dispatch entry (main.lua:563) — NOT the file-local
// handle_* functions, which are unreachable from outside the file. Returns the
// table on_command returned.
func runCommand(t *testing.T, ctx map[string]string) *lua.LTable {
	t.Helper()

	L := lua.NewState()
	defer L.Close()

	hostfunc.RegisterStdlib(L) // provides holo.comm
	stubHolomush(L)

	require.NoError(t, L.DoFile("main.lua"), "load core-communication main.lua")

	ctxTbl := L.NewTable()
	for k, v := range ctx {
		L.SetField(ctxTbl, k, lua.LString(v))
	}

	require.NoError(t, L.CallByParam(lua.P{
		Fn:      L.GetGlobal("on_command"),
		NRet:    1,
		Protect: true,
	}, ctxTbl), "call on_command")

	ret := L.Get(-1)
	L.Pop(1)

	tbl, ok := ret.(*lua.LTable)
	require.True(t, ok, "on_command must return a table, got %T", ret)
	return tbl
}

// firstEventPayload digs events[1].payload out of an on_command response
// table (the ok_events shape: {status=0, output=..., events={...}}).
func firstEventPayload(t *testing.T, resp *lua.LTable) string {
	t.Helper()

	events, ok := resp.RawGetString("events").(*lua.LTable)
	require.True(t, ok, "response must carry an events table")
	require.Greater(t, events.Len(), 0, "events table must be non-empty")

	first, ok := events.RawGetInt(1).(*lua.LTable)
	require.True(t, ok, "events[1] must be a table")

	return lua.LVAsString(first.RawGetString("payload"))
}

// responseOutput extracts the output field from an on_command response table
// (the error_response{status=1, output=msg} shape).
func responseOutput(resp *lua.LTable) string {
	return lua.LVAsString(resp.RawGetString("output"))
}

// assertRejected verifies an on_command response is a guard rejection: the
// expected error message, status 1, and — critically — NO events table, proving
// the empty-input guard fired BEFORE any payload was built (the builders always
// return a non-empty JSON string, so a guard could not rely on an empty payload).
func assertRejected(t *testing.T, resp *lua.LTable, wantMsg string) {
	t.Helper()
	require.Equal(t, wantMsg, responseOutput(resp))
	require.Equal(t, 1, int(lua.LVAsNumber(resp.RawGetString("status"))), "rejection status must be 1")
	require.Equal(t, lua.LNil, resp.RawGetString("events"), "a rejected command must emit no events")
}

func TestPoseHandlerEmitsCommunicationContent(t *testing.T) {
	resp := runCommand(t, map[string]string{
		"command": "pose", "character_id": "01HZX0000000000000000TURQ", "character_name": "Alaric",
		"args": "waves", "invoked_as": ";", "location_id": "01LOC00000000000000000000",
	})

	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(firstEventPayload(t, resp)), &got))
	require.Equal(t, "waves", got.GetText())
	require.True(t, got.GetNoSpace())
	require.Equal(t, "01HZX0000000000000000TURQ", got.GetActorId())
	require.Equal(t, "Alaric", got.GetActorDisplayName())
}

func TestPoseHandlerRejectsEmptyAction(t *testing.T) {
	resp := runCommand(t, map[string]string{
		"command": "pose", "character_id": "01H", "character_name": "Alaric",
		"args": "", "invoked_as": ";", "location_id": "01LOC",
	})

	assertRejected(t, resp, "What do you want to pose?")
}

func TestSayHandlerEmitsCommunicationContent(t *testing.T) {
	resp := runCommand(t, map[string]string{
		"command": "say", "character_id": "01H", "character_name": "Alaric",
		"args": "hello there", "location_id": "01LOC",
	})

	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(firstEventPayload(t, resp)), &got))
	require.Equal(t, "hello there", got.GetText())
	require.Equal(t, "01H", got.GetActorId())
	require.Equal(t, "Alaric", got.GetActorDisplayName())
}

func TestSayHandlerRejectsEmptyMessage(t *testing.T) {
	resp := runCommand(t, map[string]string{
		"command": "say", "character_id": "01H", "character_name": "Alaric",
		"args": "   ", "location_id": "01LOC",
	})

	assertRejected(t, resp, "What do you want to say?")
}

func TestOOCHandlerEmitsCommunicationContent(t *testing.T) {
	resp := runCommand(t, map[string]string{
		"command": "ooc", "character_id": "01H", "character_name": "Alaric",
		"args": ":laughs", "location_id": "01LOC",
	})

	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(firstEventPayload(t, resp)), &got))
	require.Equal(t, "laughs", got.GetText())
	require.Equal(t, "pose", got.GetOocStyle())
	require.Equal(t, "01H", got.GetActorId())
}

func TestOOCHandlerRejectsEmptyMessage(t *testing.T) {
	resp := runCommand(t, map[string]string{
		"command": "ooc", "character_id": "01H", "character_name": "Alaric",
		"args": "", "location_id": "01LOC",
	})

	assertRejected(t, resp, "Usage: ooc <message>")
}

func TestEmitHandlerEmitsCommunicationContentWithoutActor(t *testing.T) {
	resp := runCommand(t, map[string]string{
		"command": "emit", "location_id": "01LOC",
		"args": "the ground trembles",
	})

	var got commv1.CommunicationContent
	require.NoError(t, protojson.Unmarshal([]byte(firstEventPayload(t, resp)), &got))
	require.Equal(t, "the ground trembles", got.GetText())
	require.Equal(t, "", got.GetActorId())
	require.Equal(t, "", got.GetActorDisplayName())
}

func TestEmitHandlerRejectsEmptyMessage(t *testing.T) {
	resp := runCommand(t, map[string]string{
		"command": "emit", "location_id": "01LOC",
		"args": "   ",
	})

	assertRejected(t, resp, "What do you want to emit?")
}
