// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/eventbus"
)

type recordingInnerReader struct{ called bool }

func (r *recordingInnerReader) ReplayTail(_ context.Context, _ string, _ int, _ time.Time, _ ulid.ULID) ([]eventbus.Event, error) {
	r.called = true
	return nil, nil
}

// Verifies: INV-PLUGIN-50
func TestAuthorizingHistoryReaderReplayTail(t *testing.T) {
	// The ambient Lua query_stream_history path is gated by wrapping the reader.
	// Each case asserts that the wrapped ReplayTail delegates to the inner reader
	// ONLY when the gate permits — a denied/misconfigured/wildcard read must never
	// reach the inner ReplayTail (holomush-xakba, plugin-runtime-symmetry with the
	// host.v1 handler).
	tests := []struct {
		name          string
		engine        types.AccessPolicyEngine
		stream        string
		wantDelegated bool
	}{
		{"policy-denied stream is not delegated", policytest.DenyAllEngine(), "system.rekey.01CT000.01CID00", false},
		{"permitted stream is delegated", policytest.AllowAllEngine(), "location.01LOCAAAAAAAAAAAAAAAAAA", true},
		{"nil engine fails closed", nil, "location.01LOCAAAAAAAAAAAAAAAAAA", false},
		{"wildcard is rejected even under allow-all", policytest.AllowAllEngine(), "location.>", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inner := &recordingInnerReader{}
			hr := newAuthorizingHistoryReader(inner, tc.engine, nil, "main", "echo-bot")

			_, err := hr.ReplayTail(context.Background(), tc.stream, 10, time.Time{}, ulid.ULID{})
			if tc.wantDelegated {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			assert.Equal(t, tc.wantDelegated, inner.called)
		})
	}
}

// Verifies: INV-PLUGIN-50
func TestQueryStreamHistoryLuaPathEnforcesGate(t *testing.T) {
	// End-to-end Lua call chain: holomush.query_stream_history → Register-wrapped
	// reader → AuthorizeStreamRead. A denied system stream must return nil + an
	// error to Lua and never reach the inner reader (closes the fw118.6 gap where
	// the ambient hostfunc tests wired the raw reader, bypassing the gate).
	inner := &recordingInnerReader{}
	f := New(nil,
		WithEngine(policytest.DenyAllEngine()),
		WithGameID("main"),
		WithHistoryReader(inner))
	L := lua.NewState()
	defer L.Close()
	f.Register(L, "echo-bot")

	require.NoError(t, L.DoString(`result, errmsg = holomush.query_stream_history({stream="system.rekey.01CT000.01CID00", count=5})`))
	assert.Equal(t, lua.LNil, L.GetGlobal("result"), "a denied stream read must return nil")
	assert.NotEqual(t, lua.LNil, L.GetGlobal("errmsg"), "a denied stream read must return an error message")
	assert.False(t, inner.called, "the ABAC gate must deny before the inner reader is reached")
}

func TestNewAuthorizingHistoryReaderNilInnerStaysNil(t *testing.T) {
	// Preserves the caller's nil-reader no-op path (RegisterStreamHistoryFunc skips
	// stashing a nil reader).
	require.Nil(t, newAuthorizingHistoryReader(nil, policytest.AllowAllEngine(), nil, "main", "echo-bot"))
}
