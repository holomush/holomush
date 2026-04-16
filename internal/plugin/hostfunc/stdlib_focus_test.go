// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/session"
)

type mockFocusOps struct {
	joinCalls    []focusOpCall
	leaveCalls   []focusOpCall
	presentCalls []focusOpCall
	joinErr      error
	leaveErr     error
	presentErr   error
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

func (m *mockFocusOps) PresentFocus(_ context.Context, sid string, key session.FocusKey) error {
	m.presentCalls = append(m.presentCalls, focusOpCall{sid, key})
	return m.presentErr
}

type mockHistoryReader struct {
	calls  []historyCall
	result []core.Event
	err    error
}

type historyCall struct {
	stream    string
	count     int
	notBefore time.Time
}

func (m *mockHistoryReader) ReplayTail(_ context.Context, stream string, count int, notBefore time.Time, _ ulid.ULID) ([]core.Event, error) {
	m.calls = append(m.calls, historyCall{stream, count, notBefore})
	return m.result, m.err
}

func newFocusTestState(t *testing.T, fo hostfunc.FocusOps, hr hostfunc.HistoryReader) *lua.LState {
	t.Helper()
	L := lua.NewState()
	t.Cleanup(L.Close)
	hf := hostfunc.New(nil,
		hostfunc.WithFocusOps(fo),
		hostfunc.WithHistoryReader(hr),
	)
	hf.Register(L, "test-plugin")
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

func TestQueryStreamHistoryCallsReaderWithCorrectArgs(t *testing.T) {
	hr := &mockHistoryReader{result: []core.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history("scene:01ABC:ic", 10, 1700000000000)`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.Equal(t, "scene:01ABC:ic", hr.calls[0].stream)
	assert.Equal(t, 10, hr.calls[0].count)
	assert.Equal(t, time.UnixMilli(1700000000000).UTC(), hr.calls[0].notBefore)
}

func TestQueryStreamHistoryReturnsEventTableOnSuccess(t *testing.T) {
	targetID := ulid.Make()
	ev := core.Event{
		ID:        targetID,
		Stream:    "scene:abc:ic",
		Type:      core.EventTypeSay,
		Timestamp: time.UnixMilli(1700000000000).UTC(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: "char-1"},
		Payload:   []byte(`{"msg":"hello"}`),
	}
	hr := &mockHistoryReader{result: []core.Event{ev}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local events = holomush.query_stream_history("scene:abc:ic", 5)
assert(type(events) == "table", "expected table, got: " .. type(events))
assert(#events == 1, "expected 1 event, got: " .. #events)
local e = events[1]
assert(e.stream == "scene:abc:ic", "wrong stream: " .. tostring(e.stream))
assert(e.type == "say", "wrong type: " .. tostring(e.type))
assert(e.actor_kind == "character", "wrong actor_kind: " .. tostring(e.actor_kind))
assert(e.actor_id == "char-1", "wrong actor_id: " .. tostring(e.actor_id))
assert(e.payload == '{"msg":"hello"}', "wrong payload: " .. tostring(e.payload))
`)
	require.NoError(t, err)
}

func TestQueryStreamHistoryReturnsNilAndErrorOnFailure(t *testing.T) {
	hr := &mockHistoryReader{err: errors.New("store unavailable")}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`
local events, errmsg = holomush.query_stream_history("scene:abc:ic", 5)
assert(events == nil, "expected nil on error")
assert(errmsg == "store unavailable", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
}

func TestQueryStreamHistoryWithZeroNotBeforePassesZeroTime(t *testing.T) {
	hr := &mockHistoryReader{result: []core.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history("scene:abc:ic", 5)`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.True(t, hr.calls[0].notBefore.IsZero(), "expected zero time when not_before_ms omitted")
}

func TestQueryStreamHistoryWithNilReaderIsNoOp(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.query_stream_history("scene:abc:ic", 10)`)
	require.NoError(t, err)
}

func TestFocusFuncsWithNilOpsAreNoOps(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")

	require.NoError(t, L.DoString(`holomush.join_focus("s", "scene", "`+ulid.Make().String()+`")`))
	require.NoError(t, L.DoString(`holomush.leave_focus("s", "scene", "`+ulid.Make().String()+`")`))
	require.NoError(t, L.DoString(`holomush.present_focus("s", "scene", "`+ulid.Make().String()+`")`))
}

func TestQueryStreamHistoryClampsCountAbove500ToMax(t *testing.T) {
	hr := &mockHistoryReader{result: []core.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history("scene:abc:ic", 1000)`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.Equal(t, 500, hr.calls[0].count)
}

func TestQueryStreamHistoryClampsNegativeCountToZero(t *testing.T) {
	hr := &mockHistoryReader{result: []core.Event{}}
	L := newFocusTestState(t, nil, hr)

	err := L.DoString(`holomush.query_stream_history("scene:abc:ic", -5)`)
	require.NoError(t, err)

	require.Len(t, hr.calls, 1)
	assert.Equal(t, 0, hr.calls[0].count)
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
