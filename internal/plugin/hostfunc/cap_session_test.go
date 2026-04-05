// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// =============================================================================
// SessionCapability — compile-time interface satisfaction
// =============================================================================

// Verify SessionCapability satisfies the Capability interface at compile time.
var _ hostfunc.Capability = (*hostfunc.SessionCapability)(nil)

// =============================================================================
// mockCapSessionAccess — test double for SessionAccess
// =============================================================================

type mockCapSessionAccess struct {
	findResult      *hostfunc.SessionInfo
	findErr         error
	listResult      []hostfunc.SessionInfo
	listErr         error
	broadcastErr    error
	disconnectErr   error
	setWhisperErr   error
	broadcastCalled bool
	disconnectCalls []disconnectCall
	whisperCalls    []capWhisperCall
}

type disconnectCall struct {
	SessionID string
	Reason    string
}

type capWhisperCall struct {
	SessionID string
	Name      string
}

func (m *mockCapSessionAccess) FindSessionByName(_ context.Context, _ string) (*hostfunc.SessionInfo, error) {
	return m.findResult, m.findErr
}

func (m *mockCapSessionAccess) ListActiveSessions(_ context.Context) ([]hostfunc.SessionInfo, error) {
	return m.listResult, m.listErr
}

func (m *mockCapSessionAccess) BroadcastSystemMessage(_ context.Context, _ string) error {
	m.broadcastCalled = true
	return m.broadcastErr
}

func (m *mockCapSessionAccess) SetLastWhispered(_ context.Context, sessionID, name string) error {
	m.whisperCalls = append(m.whisperCalls, capWhisperCall{SessionID: sessionID, Name: name})
	return m.setWhisperErr
}

func (m *mockCapSessionAccess) DisconnectSession(_ context.Context, sessionID, reason string) error {
	m.disconnectCalls = append(m.disconnectCalls, disconnectCall{SessionID: sessionID, Reason: reason})
	return m.disconnectErr
}

// =============================================================================
// helpers
// =============================================================================

func newCapSessionState(t *testing.T, sa hostfunc.SessionAccess) *lua.LState {
	t.Helper()
	cap := hostfunc.NewSessionCapability(sa)
	L := lua.NewState()
	t.Cleanup(func() { L.Close() })
	cap.Register(L, "test-plugin")
	return L
}

// =============================================================================
// Namespace
// =============================================================================

func TestSessionCapabilityNamespaceReturnsSession(t *testing.T) {
	cap := hostfunc.NewSessionCapability(nil)
	assert.Equal(t, "session", cap.Namespace())
}

// =============================================================================
// Register — table structure
// =============================================================================

func TestSessionCapabilityRegisterCreatesFunctionTable(t *testing.T) {
	sa := &mockCapSessionAccess{}
	L := newCapSessionState(t, sa)

	tbl := L.GetGlobal("session")
	require.Equal(t, lua.LTTable, tbl.Type(), "session global should be a table")

	for _, fn := range []string{"find_by_name", "set_last_whispered", "list_active", "broadcast", "disconnect"} {
		field := L.GetField(tbl.(*lua.LTable), fn)
		assert.Equal(t, lua.LTFunction, field.Type(), "session.%s should be a function", fn)
	}
}

// =============================================================================
// session.find_by_name
// =============================================================================

func TestSessionCapabilityFindByNameReturnsTableWhenFound(t *testing.T) {
	sa := &mockCapSessionAccess{
		findResult: &hostfunc.SessionInfo{
			ID:            testSessID1,
			CharacterID:   testCharID1,
			CharacterName: "Alice",
			LocationID:    testLocID1,
			GridPresent:   true,
			LastWhispered: "Bob",
		},
	}
	L := newCapSessionState(t, sa)

	err := L.DoString(`result = session.find_by_name("Alice")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	assert.Equal(t, testSessID1, tbl.RawGetString("id").String())
	assert.Equal(t, testCharID1, tbl.RawGetString("character_id").String())
	assert.Equal(t, "Alice", tbl.RawGetString("character_name").String())
	assert.Equal(t, testLocID1, tbl.RawGetString("location_id").String())
	assert.Equal(t, lua.LTrue, tbl.RawGetString("grid_present"))
	assert.Equal(t, "Bob", tbl.RawGetString("last_whispered").String())
}

func TestSessionCapabilityFindByNameReturnsNilWhenNotFound(t *testing.T) {
	sa := &mockCapSessionAccess{findResult: nil}
	L := newCapSessionState(t, sa)

	err := L.DoString(`result = session.find_by_name("Ghost")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	assert.Equal(t, lua.LTNil, result.Type())
}

func TestSessionCapabilityFindByNameReturnsNilAndErrorOnFailure(t *testing.T) {
	sa := &mockCapSessionAccess{findErr: errors.New("store failure")}
	L := newCapSessionState(t, sa)

	err := L.DoString(`result, find_err = session.find_by_name("Alice")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("find_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// session.list_active
// =============================================================================

func TestSessionCapabilityListActiveReturnsTable(t *testing.T) {
	sa := &mockCapSessionAccess{
		listResult: []hostfunc.SessionInfo{
			{ID: testSessID1, CharacterID: testCharID1, CharacterName: "Alice", LocationID: testLocID1},
			{ID: "01BX5ZZKBKACTAV9WEVGEMMVS2", CharacterID: testCharID2, CharacterName: "Bob", LocationID: testLocID1},
		},
	}
	L := newCapSessionState(t, sa)

	err := L.DoString(`result = session.list_active()`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())

	tbl := result.(*lua.LTable)
	assert.Equal(t, 2, tbl.Len())
}

func TestSessionCapabilityListActiveReturnsEmptyTableWhenNone(t *testing.T) {
	sa := &mockCapSessionAccess{listResult: nil}
	L := newCapSessionState(t, sa)

	err := L.DoString(`result = session.list_active()`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type())
	assert.Equal(t, 0, result.(*lua.LTable).Len())
}

func TestSessionCapabilityListActiveReturnsNilAndErrorOnFailure(t *testing.T) {
	sa := &mockCapSessionAccess{listErr: errors.New("list error")}
	L := newCapSessionState(t, sa)

	err := L.DoString(`result, list_err = session.list_active()`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("list_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// session.broadcast
// =============================================================================

func TestSessionCapabilityBroadcastCallsService(t *testing.T) {
	sa := &mockCapSessionAccess{}
	L := newCapSessionState(t, sa)

	err := L.DoString(`session.broadcast("hello world")`)
	require.NoError(t, err)
	assert.True(t, sa.broadcastCalled)
}

func TestSessionCapabilityBroadcastReturnsErrorOnFailure(t *testing.T) {
	sa := &mockCapSessionAccess{broadcastErr: errors.New("broadcast failed")}
	L := newCapSessionState(t, sa)

	err := L.DoString(`result, bc_err = session.broadcast("msg")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("bc_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// session.disconnect
// =============================================================================

func TestSessionCapabilityDisconnectCallsService(t *testing.T) {
	sa := &mockCapSessionAccess{}
	L := newCapSessionState(t, sa)

	err := L.DoString(`session.disconnect("` + testSessID1 + `", "kicked")`)
	require.NoError(t, err)

	require.Len(t, sa.disconnectCalls, 1)
	assert.Equal(t, testSessID1, sa.disconnectCalls[0].SessionID)
	assert.Equal(t, "kicked", sa.disconnectCalls[0].Reason)
}

func TestSessionCapabilityDisconnectReturnsErrorOnFailure(t *testing.T) {
	sa := &mockCapSessionAccess{disconnectErr: errors.New("disconnect failed")}
	L := newCapSessionState(t, sa)

	err := L.DoString(`result, dc_err = session.disconnect("` + testSessID1 + `", "reason")`)
	require.NoError(t, err)

	assert.Equal(t, lua.LTNil, L.GetGlobal("result").Type())
	errVal := L.GetGlobal("dc_err")
	require.Equal(t, lua.LTString, errVal.Type())
	assert.NotEmpty(t, errVal.String())
}

// =============================================================================
// session.set_last_whispered
// =============================================================================

func TestSessionCapabilitySetLastWhisperedCallsService(t *testing.T) {
	sa := &mockCapSessionAccess{}
	L := newCapSessionState(t, sa)

	err := L.DoString(`session.set_last_whispered("` + testSessID1 + `", "Alice")`)
	require.NoError(t, err)

	require.Len(t, sa.whisperCalls, 1)
	assert.Equal(t, testSessID1, sa.whisperCalls[0].SessionID)
	assert.Equal(t, "Alice", sa.whisperCalls[0].Name)
}

func TestSessionCapabilitySetLastWhisperedSilentOnError(t *testing.T) {
	// set_last_whispered swallows errors (matches stdlib_session.go behavior).
	sa := &mockCapSessionAccess{setWhisperErr: errors.New("write failed")}
	L := newCapSessionState(t, sa)

	err := L.DoString(`session.set_last_whispered("` + testSessID1 + `", "Alice")`)
	require.NoError(t, err, "set_last_whispered should not raise a Lua error on failure")
}
