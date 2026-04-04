// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/session"
)

// mockSessionAccess is a minimal session.Access implementation for hostfunc tests.
type mockSessionAccess struct {
	mu                    sync.Mutex
	sessions              []*session.Info
	updateLastWhisperedFn func(ctx context.Context, sessionID, name string) error
	lastWhisperedCalls    []whisperCall
}

type whisperCall struct {
	SessionID string
	Name      string
}

func newMockSessionAccess(infos ...*session.Info) *mockSessionAccess {
	return &mockSessionAccess{sessions: infos}
}

func (m *mockSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*session.Info
	for _, s := range m.sessions {
		if s.Status == session.StatusActive {
			result = append(result, s)
		}
	}
	return result, nil
}

func (m *mockSessionAccess) FindByCharacter(_ context.Context, charID ulid.ULID) (*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.CharacterID == charID {
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockSessionAccess) FindByCharacterName(_ context.Context, name string) (*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if strings.EqualFold(s.CharacterName, name) {
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockSessionAccess) DeleteByCharacter(_ context.Context, charID ulid.ULID, _ string) (*session.Info, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.sessions {
		if s.CharacterID == charID {
			m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
			return s, nil
		}
	}
	return nil, nil
}

func (m *mockSessionAccess) UpdateActivity(_ context.Context, _ string) error { return nil }

func (m *mockSessionAccess) UpdateLastPaged(_ context.Context, _, _ string) error { return nil }

func (m *mockSessionAccess) UpdateLastWhispered(ctx context.Context, sessionID, name string) error {
	m.mu.Lock()
	m.lastWhisperedCalls = append(m.lastWhisperedCalls, whisperCall{SessionID: sessionID, Name: name})
	fn := m.updateLastWhisperedFn
	m.mu.Unlock()
	if fn != nil {
		return fn(ctx, sessionID, name)
	}
	return nil
}

func (m *mockSessionAccess) whisperCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.lastWhisperedCalls)
}

func (m *mockSessionAccess) lastWhisperCall() (whisperCall, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.lastWhisperedCalls) == 0 {
		return whisperCall{}, false
	}
	return m.lastWhisperedCalls[len(m.lastWhisperedCalls)-1], true
}

// newLuaStateWithSession creates a Lua state with stdlib registered and holo.session configured.
func newLuaStateWithSession(t *testing.T, sa session.Access) *lua.LState {
	t.Helper()
	L := lua.NewState()
	hostfunc.RegisterStdlib(L)
	holoTable, ok := L.GetGlobal("holo").(*lua.LTable)
	require.True(t, ok, "holo table must be set after RegisterStdlib")
	hostfunc.RegisterSessionFuncs(L, holoTable, sa)
	return L
}

// makeSessionInfo builds a minimal session.Info for test scenarios.
func makeSessionInfo(charID, charName, locID string) *session.Info {
	cid := ulid.MustParse(charID)
	lid := ulid.MustParse(locID)
	return &session.Info{
		ID:            ulid.Make().String(),
		CharacterID:   cid,
		CharacterName: charName,
		LocationID:    lid,
		Status:        session.StatusActive,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
}

// Valid Crockford base32 ULIDs for use in tests.
// ULID alphabet: 0123456789ABCDEFGHJKMNPQRSTVWXYZ (no I, L, O, U)
const (
	testCharID1 = "01BX5ZZKBKACTAV9WEVGEMMVRY" // valid ULID
	testCharID2 = "01BX5ZZKBKACTAV9WEVGEMMVRZ" // valid ULID
	testLocID1  = "01BX5ZZKBKACTAV9WEVGEMMVS0" // valid ULID
	testSessID1 = "01BX5ZZKBKACTAV9WEVGEMMVS1" // valid ULID
)

// =============================================================================
// holo.session.find_by_name()
// =============================================================================

func TestSessionFindByNameFound(t *testing.T) {
	info := makeSessionInfo(testCharID1, "Alice", testLocID1)
	sa := newMockSessionAccess(info)
	L := newLuaStateWithSession(t, sa)
	defer L.Close()

	err := L.DoString(`result = holo.session.find_by_name("Alice")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type(), "find_by_name should return a table for existing session")

	tbl := result.(*lua.LTable)
	assert.Equal(t, testCharID1, tbl.RawGetString("character_id").String())
	assert.Equal(t, "Alice", tbl.RawGetString("character_name").String())
	assert.Equal(t, testLocID1, tbl.RawGetString("location_id").String())
}

func TestSessionFindByNameCaseInsensitive(t *testing.T) {
	info := makeSessionInfo(testCharID2, "Bob", testLocID1)
	sa := newMockSessionAccess(info)
	L := newLuaStateWithSession(t, sa)
	defer L.Close()

	err := L.DoString(`result = holo.session.find_by_name("bob")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	require.Equal(t, lua.LTTable, result.Type(), "case-insensitive lookup should find session")

	tbl := result.(*lua.LTable)
	assert.Equal(t, "Bob", tbl.RawGetString("character_name").String())
}

func TestSessionFindByNameNotFound(t *testing.T) {
	sa := newMockSessionAccess()
	L := newLuaStateWithSession(t, sa)
	defer L.Close()

	err := L.DoString(`result = holo.session.find_by_name("Nonexistent")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	assert.Equal(t, lua.LTNil, result.Type(), "find_by_name should return nil for missing session")
}

func TestSessionFindByNameStoreError(t *testing.T) {
	sa := newMockSessionAccess()
	sa.updateLastWhisperedFn = nil // doesn't apply here, test find returning error
	// Override FindByCharacterName to return an error via a session that panics — instead, build a
	// custom access that returns an error for find.
	errSA := &errorSessionAccess{findErr: errors.New("db unavailable")}
	L := newLuaStateWithSession(t, errSA)
	defer L.Close()

	// Should return nil + error string on error (no panic).
	err := L.DoString(`result, find_err = holo.session.find_by_name("Alex")`)
	require.NoError(t, err)

	result := L.GetGlobal("result")
	assert.Equal(t, lua.LTNil, result.Type(), "find_by_name should return nil on store error")

	findErr := L.GetGlobal("find_err")
	require.Equal(t, lua.LTString, findErr.Type(), "find_by_name should return error string as second value")
	assert.Contains(t, findErr.String(), "db unavailable")
}

// =============================================================================
// holo.session.set_last_whispered()
// =============================================================================

func TestSessionSetLastWhisperedCallsUpdate(t *testing.T) {
	sa := newMockSessionAccess()
	L := newLuaStateWithSession(t, sa)
	defer L.Close()

	err := L.DoString(`holo.session.set_last_whispered("` + testSessID1 + `", "Alice")`)
	require.NoError(t, err)

	assert.Equal(t, 1, sa.whisperCallCount(), "UpdateLastWhispered should be called once")
	call, ok := sa.lastWhisperCall()
	require.True(t, ok)
	assert.Equal(t, testSessID1, call.SessionID)
	assert.Equal(t, "Alice", call.Name)
}

func TestSessionSetLastWhisperedStoreErrorNoLuaError(t *testing.T) {
	// Even if UpdateLastWhispered returns an error, the Lua call should not raise an error.
	sa := newMockSessionAccess()
	sa.updateLastWhisperedFn = func(_ context.Context, _, _ string) error {
		return errors.New("db write failed")
	}
	L := newLuaStateWithSession(t, sa)
	defer L.Close()

	// Should complete without error — errors are logged, not propagated.
	err := L.DoString(`holo.session.set_last_whispered("` + testSessID1 + `", "Alice")`)
	require.NoError(t, err, "set_last_whispered should not raise a Lua error on store failure")
}

// =============================================================================
// errorSessionAccess — test helper for error path coverage
// =============================================================================

type errorSessionAccess struct {
	findErr error
}

func (e *errorSessionAccess) ListActive(_ context.Context) ([]*session.Info, error) {
	return nil, nil
}

func (e *errorSessionAccess) FindByCharacter(_ context.Context, _ ulid.ULID) (*session.Info, error) {
	return nil, e.findErr
}

func (e *errorSessionAccess) FindByCharacterName(_ context.Context, _ string) (*session.Info, error) {
	return nil, e.findErr
}

func (e *errorSessionAccess) DeleteByCharacter(_ context.Context, _ ulid.ULID, _ string) (*session.Info, error) {
	return nil, nil
}

func (e *errorSessionAccess) UpdateActivity(_ context.Context, _ string) error     { return nil }
func (e *errorSessionAccess) UpdateLastPaged(_ context.Context, _, _ string) error { return nil }
func (e *errorSessionAccess) UpdateLastWhispered(_ context.Context, _, _ string) error {
	return nil
}
