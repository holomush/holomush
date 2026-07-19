// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/eventbus"
	"github.com/holomush/holomush/internal/eventbus/cursor"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/session"
	corecomm "github.com/holomush/holomush/plugins/core-communication"
)

type mockFocusOps struct {
	joinCalls           []focusOpCall
	leaveCalls          []focusOpCall
	leaveByTargetCalls  []session.FocusKey
	leaveByTargetResult session.LeaveByTargetResult
	leaveByTargetErr    error
	presentCalls        []focusOpCall
	setConnFocusCalls   []setConnFocusCall
	joinErr             error
	leaveErr            error
	presentErr          error
	getConnFocusResult  *session.FocusKey
	getConnFocusErr     error
}

// setConnFocusCall captures inputs to SetConnectionFocus so tests
// can assert the Lua hostfunc parsed and forwarded them correctly.
// (CodeRabbit PR #4191 round 6)
type setConnFocusCall struct {
	connectionID ulid.ULID
	focusKey     *session.FocusKey
	isSceneGrid  bool
}

type focusOpCall struct {
	sessionID string
	key       session.FocusKey
}

func (m *mockFocusOps) JoinFocus(_ context.Context, sid string, key session.FocusKey) error {
	m.joinCalls = append(m.joinCalls, focusOpCall{sid, key})
	return m.joinErr
}

func (m *mockFocusOps) LeaveFocus(_ context.Context, sid string, key session.FocusKey) error {
	m.leaveCalls = append(m.leaveCalls, focusOpCall{sid, key})
	return m.leaveErr
}

func (m *mockFocusOps) LeaveFocusByTarget(_ context.Context, key session.FocusKey) (session.LeaveByTargetResult, error) {
	m.leaveByTargetCalls = append(m.leaveByTargetCalls, key)
	return m.leaveByTargetResult, m.leaveByTargetErr
}

func (m *mockFocusOps) PresentFocus(_ context.Context, sid string, key session.FocusKey) error {
	m.presentCalls = append(m.presentCalls, focusOpCall{sid, key})
	return m.presentErr
}

func (m *mockFocusOps) SetConnectionFocus(_ context.Context, connID ulid.ULID, focusKey *session.FocusKey, isSceneGrid bool) error {
	m.setConnFocusCalls = append(m.setConnFocusCalls, setConnFocusCall{connID, focusKey, isSceneGrid})
	return nil
}

func (m *mockFocusOps) AutoFocusOnJoin(_ context.Context, _, _ ulid.ULID) ([]ulid.ULID, []ulid.ULID, []hostfunc.FocusFailure, uint32, error) {
	return nil, nil, nil, 0, nil
}

func (m *mockFocusOps) IsAnyConnFocused(_ context.Context, _, _ ulid.ULID) (bool, error) {
	return false, nil
}

func (m *mockFocusOps) GetConnectionFocus(_ context.Context, _ ulid.ULID) (*session.FocusKey, error) {
	return m.getConnFocusResult, m.getConnFocusErr
}

type mockHistoryReader struct {
	calls  []historyCall
	result []eventbus.Event
	err    error
}

type historyCall struct {
	stream    string
	count     int
	notBefore time.Time
	beforeSeq uint64
	beforeID  ulid.ULID
}

func (m *mockHistoryReader) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time, beforeSeq uint64, beforeID ulid.ULID) ([]eventbus.Event, error) {
	m.calls = append(m.calls, historyCall{stream, count, notBefore, beforeSeq, beforeID})
	return m.result, m.err
}

func newFocusTestState(t *testing.T, fo hostfunc.FocusOps, hr hostfunc.HistoryReader) *lua.LState {
	t.Helper()
	L := lua.NewState()
	t.Cleanup(L.Close)
	hf := hostfunc.New(
		nil,
		hostfunc.WithFocusOps(fo),
		hostfunc.WithHistoryReader(hr),
	)
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")
	return L
}

func TestJoinFocusCallsCoordinatorWithCorrectArgs(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	targetID := ulid.Make()
	err := L.DoString(`holomush.join_focus("sess-1", "scene", "` + targetID.String() + `")`)
	require.NoError(t, err)

	require.Len(t, fo.joinCalls, 1)
	assert.Equal(t, "sess-1", fo.joinCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fo.joinCalls[0].key.Kind)
	assert.Equal(t, targetID, fo.joinCalls[0].key.TargetID)
}

func TestJoinFocusReturnsTrueOnSuccess(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok = holomush.join_focus("sess-1", "scene", "` + ulid.Make().String() + `")
assert(ok == true, "expected true, got: " .. tostring(ok))
`)
	require.NoError(t, err)
}

func TestJoinFocusReturnsNilAndErrorOnFailure(t *testing.T) {
	fo := &mockFocusOps{joinErr: errors.New("already member")}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.join_focus("sess-1", "scene", "` + ulid.Make().String() + `")
assert(ok == nil, "expected nil on error")
assert(errmsg == "already member", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestLeaveFocusCallsCoordinatorWithCorrectArgs(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	targetID := ulid.Make()
	err := L.DoString(`holomush.leave_focus("sess-2", "scene", "` + targetID.String() + `")`)
	require.NoError(t, err)

	require.Len(t, fo.leaveCalls, 1)
	assert.Equal(t, "sess-2", fo.leaveCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fo.leaveCalls[0].key.Kind)
	assert.Equal(t, targetID, fo.leaveCalls[0].key.TargetID)
}

func TestLeaveFocusReturnsTrueOnSuccess(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok = holomush.leave_focus("sess-2", "scene", "` + ulid.Make().String() + `")
assert(ok == true, "expected true, got: " .. tostring(ok))
`)
	require.NoError(t, err)
}

func TestLeaveFocusReturnsNilAndErrorOnFailure(t *testing.T) {
	fo := &mockFocusOps{leaveErr: errors.New("not a member")}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.leave_focus("sess-2", "scene", "` + ulid.Make().String() + `")
assert(ok == nil, "expected nil on error")
assert(errmsg == "not a member", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestLeaveFocusByTargetCallsCoordinatorWithParsedFocusKey(t *testing.T) {
	fo := &mockFocusOps{leaveByTargetResult: session.LeaveByTargetResult{Succeeded: 2, TotalScanned: 2}}
	L := newFocusTestState(t, fo, nil)

	targetID := ulid.Make()
	err := L.DoString(`holomush.leave_focus_by_target("scene", "` + targetID.String() + `")`)
	require.NoError(t, err)

	require.Len(t, fo.leaveByTargetCalls, 1)
	assert.Equal(t, session.FocusKindScene, fo.leaveByTargetCalls[0].Kind)
	assert.Equal(t, targetID, fo.leaveByTargetCalls[0].TargetID)
}

func TestLeaveFocusByTargetReturnsResultTableOnSuccess(t *testing.T) {
	fo := &mockFocusOps{leaveByTargetResult: session.LeaveByTargetResult{Succeeded: 3, TotalScanned: 3}}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local result = holomush.leave_focus_by_target("scene", "` + ulid.Make().String() + `")
assert(type(result) == "table", "expected table, got: " .. type(result))
assert(result.succeeded == 3, "expected succeeded=3, got: " .. tostring(result.succeeded))
assert(result.total_scanned == 3, "expected total_scanned=3, got: " .. tostring(result.total_scanned))
assert(#result.failed == 0, "expected empty failed list, got: " .. tostring(#result.failed))
`)
	require.NoError(t, err)
}

func TestLeaveFocusByTargetExposesPartialFailureInLuaTable(t *testing.T) {
	fo := &mockFocusOps{leaveByTargetResult: session.LeaveByTargetResult{
		Succeeded:    1,
		TotalScanned: 2,
		Failed:       []session.FailedLeave{{SessionID: "sess-bad", Err: errors.New("host blip")}},
	}}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local result = holomush.leave_focus_by_target("scene", "` + ulid.Make().String() + `")
assert(result.succeeded == 1, "expected succeeded=1")
assert(result.total_scanned == 2, "expected total_scanned=2")
assert(#result.failed == 1, "expected 1 failed entry")
assert(result.failed[1].session_id == "sess-bad", "wrong session id: " .. tostring(result.failed[1].session_id))
assert(result.failed[1].error == "host blip", "wrong error: " .. tostring(result.failed[1].error))
`)
	require.NoError(t, err)
}

func TestLeaveFocusByTargetReturnsEmptyTableWhenNoMembers(t *testing.T) {
	fo := &mockFocusOps{leaveByTargetResult: session.LeaveByTargetResult{}}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local result = holomush.leave_focus_by_target("scene", "` + ulid.Make().String() + `")
assert(result.succeeded == 0, "expected succeeded=0")
assert(result.total_scanned == 0, "expected total_scanned=0")
assert(#result.failed == 0, "expected empty failed list")
`)
	require.NoError(t, err)
}

func TestLeaveFocusByTargetReturnsNilAndErrorOnEnumerationFailure(t *testing.T) {
	fo := &mockFocusOps{leaveByTargetErr: errors.New("store down")}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local result, errmsg = holomush.leave_focus_by_target("scene", "` + ulid.Make().String() + `")
assert(result == nil, "expected nil on enumeration failure")
assert(errmsg == "store down", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestLeaveFocusByTargetReturnsErrorWhenFocusOpsNotInitialized(t *testing.T) {
	L := newFocusTestState(t, nil, nil)

	err := L.DoString(`
local result, errmsg = holomush.leave_focus_by_target("scene", "` + ulid.Make().String() + `")
assert(result == nil, "expected nil when focus ops missing")
assert(errmsg == "focus ops not initialized", "expected init error, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestLeaveFocusByTargetReturnsErrorForInvalidULID(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local result, errmsg = holomush.leave_focus_by_target("scene", "not-a-ulid")
assert(result == nil, "expected nil on invalid ulid")
assert(string.find(errmsg, "invalid target_id"), "expected parse error, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
	assert.Empty(t, fo.leaveByTargetCalls)
}

func TestPresentFocusCallsCoordinatorWithCorrectArgs(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	targetID := ulid.Make()
	err := L.DoString(`holomush.present_focus("sess-3", "scene", "` + targetID.String() + `")`)
	require.NoError(t, err)

	require.Len(t, fo.presentCalls, 1)
	assert.Equal(t, "sess-3", fo.presentCalls[0].sessionID)
	assert.Equal(t, session.FocusKindScene, fo.presentCalls[0].key.Kind)
	assert.Equal(t, targetID, fo.presentCalls[0].key.TargetID)
}

func TestPresentFocusReturnsTrueOnSuccess(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok = holomush.present_focus("sess-3", "scene", "` + ulid.Make().String() + `")
assert(ok == true, "expected true, got: " .. tostring(ok))
`)
	require.NoError(t, err)
}

func TestPresentFocusReturnsNilAndErrorOnFailure(t *testing.T) {
	fo := &mockFocusOps{presentErr: errors.New("focus not found")}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.present_focus("sess-3", "scene", "` + ulid.Make().String() + `")
assert(ok == nil, "expected nil on error")
assert(errmsg == "focus not found", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

// encodeTestCursor builds a base64 cursor string for use in test assertions.
func encodeTestCursor(t *testing.T, seq uint64, id ulid.ULID) string {
	t.Helper()
	b, err := cursor.Encode(cursor.Cursor{
		Version: cursor.CurrentVersion,
		Epoch:   cursor.CurrentEpoch(),
		Owner:   cursor.Owner{Kind: cursor.OwnerHost},
		Host:    &cursor.HostCursor{Seq: seq, ID: id},
	})
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(b)
}

func TestQueryStreamHistoryCallsReaderWithCorrectArgs(t *testing.T) {
	hr := &mockHistoryReader{result: []eventbus.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history({stream="scene:01ABC:ic", count=10, not_before_ms=1700000000000})`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.Equal(t, "scene:01ABC:ic", hr.calls[0].stream)
	assert.Equal(t, 10, hr.calls[0].count)
	assert.Equal(t, time.UnixMilli(1700000000000).UTC(), hr.calls[0].notBefore)
}

func TestQueryStreamHistoryReturnsResultTableWithEventsAndMeta(t *testing.T) {
	targetID := ulid.Make()
	actorID := ulid.Make()
	ev := eventbus.Event{
		ID:        targetID,
		Subject:   "scene:abc:ic",
		Type:      eventbus.Type(corecomm.EventTypeSay),
		Timestamp: time.UnixMilli(1700000000000).UTC(),
		Actor:     eventbus.Actor{Kind: eventbus.ActorKindCharacter, ID: actorID},
		Payload:   []byte(`{"msg":"hello"}`),
	}
	hr := &mockHistoryReader{result: []eventbus.Event{ev}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local result = holomush.query_stream_history({stream="scene:abc:ic", count=5})
assert(type(result) == "table", "expected table, got: " .. type(result))
assert(type(result.events) == "table", "expected events table, got: " .. type(result.events))
assert(#result.events == 1, "expected 1 event, got: " .. #result.events)
local e = result.events[1]
assert(e.stream == "scene:abc:ic", "wrong stream: " .. tostring(e.stream))
assert(e.type == "core-communication:say", "wrong type: " .. tostring(e.type))
assert(e.actor_kind == "character", "wrong actor_kind: " .. tostring(e.actor_kind))
assert(e.actor_id == "` + actorID.String() + `", "wrong actor_id: " .. tostring(e.actor_id))
assert(e.payload == '{"msg":"hello"}', "wrong payload: " .. tostring(e.payload))
assert(type(e.cursor) == "string", "expected cursor string, got: " .. type(e.cursor))
assert(result.has_more == false, "expected has_more=false for partial page")
assert(result.next_cursor == nil, "expected next_cursor=nil for partial page")
`)
	require.NoError(t, err)
}

// TestQueryStreamHistoryZeroActorIDRendersAsEmptyString locks in the
// zero-ULID → "" mapping (actorIDString) so a system/anonymous actor renders
// as the empty string plugins already observe, not the 26-char all-zeros
// ULID text (cross-AI round 7, MEDIUM).
func TestQueryStreamHistoryZeroActorIDRendersAsEmptyString(t *testing.T) {
	ev := eventbus.Event{
		ID:      ulid.Make(),
		Subject: "scene:abc:ic",
		Type:    eventbus.Type(corecomm.EventTypeSay),
		Actor:   eventbus.Actor{Kind: eventbus.ActorKindSystem},
		Payload: []byte(`{}`),
	}
	hr := &mockHistoryReader{result: []eventbus.Event{ev}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local result = holomush.query_stream_history({stream="scene:abc:ic", count=5})
local e = result.events[1]
assert(e.actor_id == "", "expected zero actor ULID to render as empty string, got: " .. tostring(e.actor_id))
`)
	require.NoError(t, err)
}

func TestQueryStreamHistoryReturnsNilAndErrorOnFailure(t *testing.T) {
	hr := &mockHistoryReader{err: errors.New("store unavailable")}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local result, errmsg = holomush.query_stream_history({stream="scene:abc:ic", count=5})
assert(result == nil, "expected nil on error")
assert(errmsg == "store unavailable", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestQueryStreamHistoryWithZeroNotBeforePassesZeroTime(t *testing.T) {
	hr := &mockHistoryReader{result: []eventbus.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history({stream="scene:abc:ic", count=5})`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.True(t, hr.calls[0].notBefore.IsZero(), "expected zero time when not_before_ms omitted")
}

func TestQueryStreamHistoryWithNilReaderIsNoOp(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`holomush.query_stream_history({stream="scene:abc:ic", count=10})`)
	require.NoError(t, err)
}

func TestFocusFuncsWithNilOpsAreNoOps(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	require.NoError(t, L.DoString(`holomush.join_focus("s", "scene", "`+ulid.Make().String()+`")`))
	require.NoError(t, L.DoString(`holomush.leave_focus("s", "scene", "`+ulid.Make().String()+`")`))
	require.NoError(t, L.DoString(`holomush.present_focus("s", "scene", "`+ulid.Make().String()+`")`))
}

func TestQueryStreamHistoryClampsCountAbove500ToMax(t *testing.T) {
	hr := &mockHistoryReader{result: []eventbus.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history({stream="scene:abc:ic", count=1000})`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.Equal(t, 500, hr.calls[0].count)
}

func TestQueryStreamHistoryClampsNegativeCountToZero(t *testing.T) {
	hr := &mockHistoryReader{result: []eventbus.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history({stream="scene:abc:ic", count=-5})`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.Equal(t, 0, hr.calls[0].count)
}

func TestQueryStreamHistoryHasMoreTrueWhenFullPageReturned(t *testing.T) {
	// 2 events returned for count=2 → has_more=true, next_cursor is set.
	e1 := eventbus.Event{ID: ulid.Make(), Subject: "scene:abc:ic", Type: eventbus.Type(corecomm.EventTypeSay)}
	e2 := eventbus.Event{ID: ulid.Make(), Subject: "scene:abc:ic", Type: eventbus.Type(corecomm.EventTypeSay)}
	hr := &mockHistoryReader{result: []eventbus.Event{e1, e2}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local result = holomush.query_stream_history({stream="scene:abc:ic", count=2})
assert(result.has_more == true, "expected has_more=true for full page")
assert(type(result.next_cursor) == "string", "expected next_cursor string, got: " .. type(result.next_cursor))
assert(#result.next_cursor > 0, "expected non-empty next_cursor")
`)
	require.NoError(t, err)
}

// TestQueryStreamHistoryMultipageWalkThreadsRealSeqIntoNextCursor is a
// genuine MULTIPAGE walk — not just a single-page encode assertion — because
// a regression that fixes only the per-event cursor literal
// (stdlib_focus.go's per-event encode site) while leaving the SEPARATE
// next_cursor literal hardcoded at Seq:0 would pass every single-page test in
// this file while still reproducing D-07's repeat bug in production (the
// next_cursor is the token the plugin actually feeds back). This test drives
// two query_stream_history calls, feeding page 1's next_cursor into page 2's
// request, and asserts the SECOND ReplayTail call carries page 1's oldest
// event's (index 0) REAL Seq — not a hardcoded 0 — as beforeSeq.
func TestQueryStreamHistoryMultipageWalkThreadsRealSeqIntoNextCursor(t *testing.T) {
	page1e1 := eventbus.Event{ID: ulid.Make(), Seq: 100, Subject: "scene:abc:ic", Type: eventbus.Type(corecomm.EventTypeSay)}
	page1e2 := eventbus.Event{ID: ulid.Make(), Seq: 101, Subject: "scene:abc:ic", Type: eventbus.Type(corecomm.EventTypeSay)}
	hr := &mockHistoryReader{result: []eventbus.Event{page1e1, page1e2}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local result = holomush.query_stream_history({stream="scene:abc:ic", count=2})
assert(result.has_more == true, "expected has_more=true for full page")
_G.next_cursor = result.next_cursor
_G.page1_id_1 = result.events[1].id
_G.page1_id_2 = result.events[2].id
`)
	require.NoError(t, err)

	page2e1 := eventbus.Event{ID: ulid.Make(), Seq: 50, Subject: "scene:abc:ic", Type: eventbus.Type(corecomm.EventTypeSay)}
	hr.result = []eventbus.Event{page2e1}
	err = L.DoString(`
local result2 = holomush.query_stream_history({stream="scene:abc:ic", count=2, cursor=_G.next_cursor})
assert(result2.events[1].id ~= _G.page1_id_1, "page 2 must not repeat page 1's first event (D-07)")
assert(result2.events[1].id ~= _G.page1_id_2, "page 2 must not repeat page 1's second event (D-07)")
`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 2)
	// The next_cursor anchor is page1e1 (index 0 of the ascending page,
	// per <page_advance_anchor>) — its REAL Seq, not a hardcoded 0.
	assert.Equal(t, page1e1.Seq, hr.calls[1].beforeSeq, "next_cursor must carry the oldest event's real Seq, not a hardcoded 0")
	assert.Equal(t, page1e1.ID, hr.calls[1].beforeID)
}

func TestQueryStreamHistoryCursorRoundTripsAsBase64(t *testing.T) {
	// Encode a cursor, pass it back in the next call, verify beforeSeq and
	// beforeID both survive the encode→decode round trip (D-07/ARCH-04:
	// the per-event cursor must carry the event's real Seq, not a
	// hardcoded 0).
	eventID := ulid.Make()
	const eventSeq = uint64(9001)
	ev := eventbus.Event{
		ID:      eventID,
		Seq:     eventSeq,
		Subject: "scene:abc:ic",
		Type:    eventbus.Type(corecomm.EventTypeSay),
	}
	hr := &mockHistoryReader{result: []eventbus.Event{ev}}
	L := newFocusTestState(t, nil, hr)

	// First page — get cursor from first event.
	err := L.DoString(`
local result = holomush.query_stream_history({stream="scene:abc:ic", count=5})
assert(type(result.events[1].cursor) == "string", "expected cursor string on event")
_G.captured_cursor = result.events[1].cursor
`)
	require.NoError(t, err)

	// Second call using the cursor — verify the call was made with the decoded
	// (beforeSeq, beforeID) pair.
	hr.result = []eventbus.Event{}
	err = L.DoString(`
local result2 = holomush.query_stream_history({stream="scene:abc:ic", count=5, cursor=_G.captured_cursor})
assert(type(result2) == "table", "expected table on second call")
`)
	require.NoError(t, err)

	// The second call should have passed the decoded (beforeSeq, beforeID)
	// pair (eventSeq, eventID) to ReplayTail.
	require.Len(t, hr.calls, 2)
	assert.Equal(t, eventID, hr.calls[1].beforeID, "cursor should decode to the event's ULID as beforeID")
	assert.Equal(t, eventSeq, hr.calls[1].beforeSeq, "cursor should decode to the event's real Seq as beforeSeq, not a hardcoded 0")
}

func TestQueryStreamHistoryPassesCursorToReaderAsBeforeID(t *testing.T) {
	// Build a cursor externally and verify it decodes correctly when passed to the hostfunc.
	const anchorSeq = uint64(77)
	anchorID := ulid.Make()
	cursorStr := encodeTestCursor(t, anchorSeq, anchorID)

	hr := &mockHistoryReader{result: []eventbus.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history({stream="scene:abc:ic", count=5, cursor="` + cursorStr + `"})`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.Equal(t, anchorID, hr.calls[0].beforeID, "decoded cursor should match anchor ULID")
	assert.Equal(t, anchorSeq, hr.calls[0].beforeSeq, "decoded cursor should match anchor Seq")
}

func TestQueryStreamHistoryReturnsErrorForInvalidBase64Cursor(t *testing.T) {
	hr := &mockHistoryReader{result: []eventbus.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local result, errmsg = holomush.query_stream_history({stream="scene:abc:ic", count=5, cursor="!!!not-base64!!!"})
assert(result == nil, "expected nil on invalid cursor")
assert(errmsg ~= nil, "expected error message")
assert(string.find(errmsg, "base64"), "expected base64 error, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestQueryStreamHistoryReturnsErrorWhenStreamMissing(t *testing.T) {
	hr := &mockHistoryReader{result: []eventbus.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local result, errmsg = holomush.query_stream_history({count=5})
assert(result == nil, "expected nil when stream missing")
assert(errmsg ~= nil, "expected error message")
`)
	require.NoError(t, err)
}

func TestQueryStreamHistoryReturnsErrorWhenCountMissing(t *testing.T) {
	hr := &mockHistoryReader{result: []eventbus.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local result, errmsg = holomush.query_stream_history({stream="scene:abc:ic"})
assert(result == nil, "expected nil when count missing")
assert(errmsg ~= nil, "expected error message")
`)
	require.NoError(t, err)
}

func TestJoinFocusReturnsErrorForInvalidULID(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.join_focus("sess-1", "scene", "not-a-ulid")
assert(ok == nil, "expected nil on error")
assert(errmsg ~= nil, "expected error message")
`)
	require.NoError(t, err)
	assert.Empty(t, fo.joinCalls)
}

func TestLeaveFocusReturnsErrorForInvalidULID(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.leave_focus("sess-1", "scene", "not-a-ulid")
assert(ok == nil, "expected nil on error")
assert(errmsg ~= nil, "expected error message")
`)
	require.NoError(t, err)
	assert.Empty(t, fo.leaveCalls)
}

func TestPresentFocusReturnsErrorForInvalidULID(t *testing.T) {
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local ok, errmsg = holomush.present_focus("sess-1", "scene", "not-a-ulid")
assert(ok == nil, "expected nil on error")
assert(errmsg ~= nil, "expected error message")
`)
	require.NoError(t, err)
	assert.Empty(t, fo.presentCalls)
}

func TestFocusHostfunc_PhaseFive_LuaParity(t *testing.T) {
	t.Parallel()
	// INV-SCENE-19: the 3 new RPCs ship Go SDK + Lua hostfunc together.
	// Test that each is registered in the holomush module table.
	ls := lua.NewState()
	defer ls.Close()
	mod := ls.NewTable()
	hostfunc.RegisterFocusFuncs(ls, mod /* mocks */, nil, nil)

	for _, name := range []string{"set_connection_focus", "auto_focus_on_join", "is_any_conn_focused", "get_connection_focus"} {
		fn := ls.GetField(mod, name)
		require.NotEqual(t, lua.LNil, fn, "hostfunc %q MUST be registered for INV-SCENE-19 parity", name)
	}
}

func TestFocusHostfunc_ULIDRoundTrip(t *testing.T) {
	t.Parallel()
	// INV-SCENE-22: Lua hostfunc accepts 26-char base32 string ULIDs; proto
	// wire takes bytes; the boundary converts. This MUST drive the
	// hostfunc end-to-end (Lua → Go binding → FocusOps mock) rather
	// than only calling ulid.Parse in Go — otherwise a regression in
	// the Lua binding's parser would still pass. (CodeRabbit PR #4191 round 6)
	connID := ulid.Make()
	sceneID := ulid.Make()
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`holomush.set_connection_focus("` + connID.String() + `", { kind = "scene", target_id = "` + sceneID.String() + `" }, false)`)
	require.NoError(t, err)

	require.Len(t, fo.setConnFocusCalls, 1, "Lua hostfunc MUST forward to FocusOps.SetConnectionFocus")
	call := fo.setConnFocusCalls[0]
	assert.Equal(t, connID, call.connectionID, "connection_id MUST round-trip Lua string → ulid.ULID")
	require.NotNil(t, call.focusKey)
	assert.Equal(t, sceneID, call.focusKey.TargetID, "target_id MUST round-trip Lua string → ulid.ULID")
	assert.Equal(t, session.FocusKindScene, call.focusKey.Kind)
	assert.False(t, call.isSceneGrid)
}

func TestGetConnectionFocusReturnsFocusKeyTableForFocusedConnection(t *testing.T) {
	t.Parallel()
	connID := ulid.Make()
	sceneID := ulid.Make()
	fo := &mockFocusOps{
		getConnFocusResult: &session.FocusKey{
			Kind:     session.FocusKindScene,
			TargetID: sceneID,
		},
	}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`local fk, err = holomush.get_connection_focus("` + connID.String() + `")
assert(err == nil, "expected no error, got: " .. tostring(err))
assert(type(fk) == "table", "expected table for focused connection")
assert(fk.kind == "scene", "expected kind=scene, got: " .. tostring(fk.kind))
assert(fk.target_id == "` + sceneID.String() + `", "wrong target_id: " .. tostring(fk.target_id))`)
	require.NoError(t, err)
}

func TestGetConnectionFocusReturnsNilForGridFocusedConnection(t *testing.T) {
	t.Parallel()
	connID := ulid.Make()
	fo := &mockFocusOps{getConnFocusResult: nil} // nil = grid focus / no scene focus
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local fk, err = holomush.get_connection_focus("` + connID.String() + `")
assert(err == nil, "expected no error for grid-focused conn, got: " .. tostring(err))
assert(fk == nil, "expected nil for grid-focused connection, got: " .. tostring(fk))
`)
	require.NoError(t, err)
}

func TestGetConnectionFocusReturnsErrorForInvalidULID(t *testing.T) {
	t.Parallel()
	fo := &mockFocusOps{}
	L := newFocusTestState(t, fo, nil)

	err := L.DoString(`
local fk, err = holomush.get_connection_focus("not-a-ulid")
assert(fk == nil, "expected nil on invalid ULID")
assert(err ~= nil, "expected error for invalid ULID")
`)
	require.NoError(t, err)
}
