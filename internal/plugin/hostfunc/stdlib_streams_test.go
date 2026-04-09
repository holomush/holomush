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

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
)

// mockStreamRegistry records calls for assertion.
type mockStreamRegistry struct {
	addCalls    []struct{ sessionID, stream string }
	removeCalls []struct{ sessionID, stream string }
	addErr      error
	removeErr   error
}

func (m *mockStreamRegistry) AddStream(_ context.Context, sessionID, stream string) error {
	m.addCalls = append(m.addCalls, struct{ sessionID, stream string }{sessionID, stream})
	return m.addErr
}

func (m *mockStreamRegistry) RemoveStream(_ context.Context, sessionID, stream string) error {
	m.removeCalls = append(m.removeCalls, struct{ sessionID, stream string }{sessionID, stream})
	return m.removeErr
}

// Ensure mockStreamRegistry implements plugins.StreamRegistry.
var _ plugins.StreamRegistry = (*mockStreamRegistry)(nil)

func newTestLuaState(t *testing.T, reg plugins.StreamRegistry) *lua.LState {
	t.Helper()
	L := lua.NewState()
	t.Cleanup(L.Close)
	hf := hostfunc.New(nil, hostfunc.WithStreamRegistry(reg))
	hf.Register(L, "test-plugin")
	return L
}

func TestAddSessionStreamCallsRegistryWithCorrectArgs(t *testing.T) {
	reg := &mockStreamRegistry{}
	L := newTestLuaState(t, reg)

	err := L.DoString(`holomush.add_session_stream("sess-1", "channel:abc")`)
	require.NoError(t, err)

	require.Len(t, reg.addCalls, 1)
	assert.Equal(t, "sess-1", reg.addCalls[0].sessionID)
	assert.Equal(t, "channel:abc", reg.addCalls[0].stream)
}

func TestRemoveSessionStreamCallsRegistryWithCorrectArgs(t *testing.T) {
	reg := &mockStreamRegistry{}
	L := newTestLuaState(t, reg)

	err := L.DoString(`holomush.remove_session_stream("sess-1", "channel:abc")`)
	require.NoError(t, err)

	require.Len(t, reg.removeCalls, 1)
	assert.Equal(t, "sess-1", reg.removeCalls[0].sessionID)
	assert.Equal(t, "channel:abc", reg.removeCalls[0].stream)
}

func TestAddSessionStreamReturnsErrorToLuaOnRegistryFailure(t *testing.T) {
	reg := &mockStreamRegistry{addErr: errors.New("registry error")}
	L := newTestLuaState(t, reg)

	// Error is returned to Lua as (nil, error_string)
	err := L.DoString(`
local ok, errmsg = holomush.add_session_stream("sess-1", "channel:abc")
assert(ok == nil, "expected nil on error, got: " .. tostring(ok))
assert(errmsg == "registry error", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
	require.Len(t, reg.addCalls, 1)
}

func TestRemoveSessionStreamReturnsErrorToLuaOnRegistryFailure(t *testing.T) {
	reg := &mockStreamRegistry{removeErr: errors.New("registry error")}
	L := newTestLuaState(t, reg)

	err := L.DoString(`
local ok, errmsg = holomush.remove_session_stream("sess-1", "channel:abc")
assert(ok == nil, "expected nil on error, got: " .. tostring(ok))
assert(errmsg == "registry error", "expected error message, got: " .. tostring(errmsg))
`)
	require.NoError(t, err)
	require.Len(t, reg.removeCalls, 1)
}

func TestAddSessionStreamReturnsTrueOnSuccess(t *testing.T) {
	reg := &mockStreamRegistry{}
	L := newTestLuaState(t, reg)

	err := L.DoString(`
local ok = holomush.add_session_stream("sess-1", "channel:abc")
assert(ok == true, "expected true on success, got: " .. tostring(ok))
`)
	require.NoError(t, err)
}

func TestRemoveSessionStreamReturnsTrueOnSuccess(t *testing.T) {
	reg := &mockStreamRegistry{}
	L := newTestLuaState(t, reg)

	err := L.DoString(`
local ok = holomush.remove_session_stream("sess-1", "channel:abc")
assert(ok == true, "expected true on success, got: " .. tostring(ok))
`)
	require.NoError(t, err)
}

func TestAddSessionStreamWithNilRegistryIsNoOp(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.add_session_stream("sess-1", "channel:abc")`)
	require.NoError(t, err)
}

func TestRemoveSessionStreamWithNilRegistryIsNoOp(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.remove_session_stream("sess-1", "channel:abc")`)
	require.NoError(t, err)
}
