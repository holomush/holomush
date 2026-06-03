// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// mockReadbackDecryptor implements hostfunc.AuditDecryptor for tests.
type mockReadbackDecryptor struct {
	calls  []mockDecryptCall
	result func(pluginName string, rows []*pluginv1.AuditRow) (*pluginv1.DecryptOwnAuditRowsResponse, error)
}

type mockDecryptCall struct {
	pluginName string
	rowCount   int
}

func (m *mockReadbackDecryptor) DecryptOwnAuditRows(_ context.Context, pluginName string, rows []*pluginv1.AuditRow) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
	m.calls = append(m.calls, mockDecryptCall{pluginName: pluginName, rowCount: len(rows)})
	if m.result != nil {
		return m.result(pluginName, rows)
	}
	results := make([]*pluginv1.RowResult, 0, len(rows))
	for _, r := range rows {
		results = append(results, &pluginv1.RowResult{Id: r.GetId()})
	}
	return &pluginv1.DecryptOwnAuditRowsResponse{Results: results}, nil
}

// newAuditTestState constructs a Lua state with holomush.decrypt_own_audit_rows registered.
func newAuditTestState(t *testing.T, dec hostfunc.AuditDecryptor) (*lua.LState, *mockReadbackDecryptor) {
	t.Helper()
	var mock *mockReadbackDecryptor
	if dec == nil {
		mock = &mockReadbackDecryptor{}
		dec = mock
	} else {
		mock, _ = dec.(*mockReadbackDecryptor)
	}
	L := lua.NewState()
	t.Cleanup(L.Close)
	hf := hostfunc.New(nil, hostfunc.WithAuditDecryptor(dec))
	hf.Register(L, "test-plugin")
	return L, mock
}

func TestDecryptOwnAuditRowsHostfuncReachesAuditDecryptor(t *testing.T) {
	t.Parallel()
	L, mock := newAuditTestState(t, nil)

	err := L.DoString(`
local rows = {
	{id = "row-1", subject = "events.main.scene.01ABC.ic", type = "scene_pose",
	 payload = "cipher", codec = "xchacha20poly1305-v1", dek_ref = 42, dek_version = 7}
}
local results, errmsg = holomush.decrypt_own_audit_rows(rows)
assert(results ~= nil, "expected results, got nil; err=" .. tostring(errmsg))
assert(#results == 1, "expected 1 result, got " .. tostring(#results))
`)
	require.NoError(t, err)
	require.Len(t, mock.calls, 1)
	assert.Equal(t, "test-plugin", mock.calls[0].pluginName)
	assert.Equal(t, 1, mock.calls[0].rowCount)
}

func TestDecryptOwnAuditRowsHostfuncReturnsPlaintextFromDecryptor(t *testing.T) {
	t.Parallel()
	dec := &mockReadbackDecryptor{
		result: func(_ string, rows []*pluginv1.AuditRow) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
			return &pluginv1.DecryptOwnAuditRowsResponse{
				Results: []*pluginv1.RowResult{
					{Id: rows[0].GetId(), Outcome: &pluginv1.RowResult_Plaintext{Plaintext: []byte("hello world")}},
				},
			}, nil
		},
	}
	L, _ := newAuditTestState(t, dec)

	err := L.DoString(`
local rows = {{id = "r1", subject = "events.main.scene.x.ic", type = "pose",
               payload = "c", codec = "xchacha20poly1305-v1"}}
local results, errmsg = holomush.decrypt_own_audit_rows(rows)
assert(results ~= nil, "expected results; err=" .. tostring(errmsg))
assert(#results == 1, "expected 1 result")
assert(results[1].plaintext == "hello world",
       "expected plaintext 'hello world', got: " .. tostring(results[1].plaintext))
assert(results[1].no_plaintext_reason == nil or results[1].no_plaintext_reason == "",
       "expected no refusal reason, got: " .. tostring(results[1].no_plaintext_reason))
`)
	require.NoError(t, err)
}

func TestDecryptOwnAuditRowsHostfuncReturnsRefusalReason(t *testing.T) {
	t.Parallel()
	dec := &mockReadbackDecryptor{
		result: func(_ string, rows []*pluginv1.AuditRow) (*pluginv1.DecryptOwnAuditRowsResponse, error) {
			return &pluginv1.DecryptOwnAuditRowsResponse{
				Results: []*pluginv1.RowResult{
					{Id: rows[0].GetId(), Outcome: &pluginv1.RowResult_NoPlaintextReason{NoPlaintextReason: "not_owner"}},
				},
			}, nil
		},
	}
	L, _ := newAuditTestState(t, dec)

	err := L.DoString(`
local rows = {{id = "r2", subject = "events.main.channel.x.msg", type = "msg",
               payload = "c", codec = "xchacha20poly1305-v1"}}
local results, errmsg = holomush.decrypt_own_audit_rows(rows)
assert(results ~= nil, "expected results; err=" .. tostring(errmsg))
assert(#results == 1, "expected 1 result")
assert(results[1].no_plaintext_reason == "not_owner",
       "expected 'not_owner', got: " .. tostring(results[1].no_plaintext_reason))
assert(results[1].plaintext == nil or results[1].plaintext == "",
       "refused row must have no plaintext")
`)
	require.NoError(t, err)
}

func TestDecryptOwnAuditRowsHostfuncRejectsMalformedRowEntry(t *testing.T) {
	t.Parallel()
	// A non-table entry in the rows array MUST reject the whole call (nil +
	// error), NOT silently shorten the batch — silent skips misalign results
	// with input indices and break INV-CRYPTO-37 positional correlation.
	L, mock := newAuditTestState(t, nil)

	err := L.DoString(`
local rows = {
	{id = "r1", subject = "events.main.scene.x.ic", type = "pose",
	 payload = "c", codec = "xchacha20poly1305-v1"},
	"not-a-table",
	{id = "r3", subject = "events.main.scene.x.ic", type = "pose",
	 payload = "c", codec = "xchacha20poly1305-v1"},
}
local results, errmsg = holomush.decrypt_own_audit_rows(rows)
assert(results == nil, "malformed batch must yield nil results, got a table")
assert(errmsg ~= nil and errmsg ~= "", "expected an error message for the malformed entry")
`)
	require.NoError(t, err)
	assert.Empty(t, mock.calls, "decryptor must NOT be invoked when the batch is malformed")
}

func TestDecryptOwnAuditRowsHostfuncWithNilDecryptorIsNoOp(t *testing.T) {
	t.Parallel()
	// A nil decryptor (unconfigured) must not panic — returns nil result.
	L := lua.NewState()
	t.Cleanup(L.Close)
	hf := hostfunc.New(nil) // no WithAuditDecryptor
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.decrypt_own_audit_rows({})`)
	require.NoError(t, err)
}

func TestDecryptOwnAuditRowsHostfuncIsRegisteredInAuditList(t *testing.T) {
	// INV: plugin-runtime-symmetry — the Lua hostfunc MUST appear in the audit
	// list so the context_audit meta-test covers it.
	hf := hostfunc.New(nil)
	entries := hf.RegisteredFunctionsForAudit()
	var found bool
	for _, e := range entries {
		if e.Name == "holomush.decrypt_own_audit_rows" {
			found = true
			break
		}
	}
	assert.True(t, found, "holomush.decrypt_own_audit_rows must be in RegisteredFunctionsForAudit")
}
