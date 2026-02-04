// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package hostfunc_test tests host function implementations.
package hostfunc_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/oklog/ulid/v2"
	lua "github.com/yuin/gopher-lua"
)

func TestNew_NilEnforcerPanics(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic for nil enforcer")
	}()

	hostfunc.New(nil, nil)
}

func TestHostFunctions_Log(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil, capability.NewEnforcer())
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.log("info", "test message")`)
	assert.NoError(t, err, "log() failed")
}

func TestHostFunctions_Log_Levels(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{"debug level", "debug"},
		{"info level", "info"},
		{"warn level", "warn"},
		{"error level", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			hf := hostfunc.New(nil, capability.NewEnforcer())
			hf.Register(L, "test-plugin")

			err := L.DoString(`holomush.log("` + tt.level + `", "test message")`)
			assert.NoError(t, err, "log(%q) failed", tt.level)
		})
	}
}

func TestHostFunctions_Log_InvalidLevel(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil, capability.NewEnforcer())
	hf.Register(L, "test-plugin")

	// Invalid log level should raise an error so plugin developers know their code is wrong
	err := L.DoString(`holomush.log("invalid_level", "test message")`)
	assert.Error(t, err, "expected error for invalid log level")
}

func TestHostFunctions_Log_MissingArguments(t *testing.T) {
	tests := []struct {
		name string
		code string
	}{
		{"no arguments", `holomush.log()`},
		{"only level", `holomush.log("info")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			hf := hostfunc.New(nil, capability.NewEnforcer())
			hf.Register(L, "test-plugin")

			err := L.DoString(tt.code)
			assert.Error(t, err, "expected error for %s", tt.name)
		})
	}
}

func TestHostFunctions_Log_InvalidLevel_ErrorMessage(t *testing.T) {
	tests := []struct {
		name  string
		level string
	}{
		{"typo warn", "warning"},
		{"typo error", "erro"},
		{"uppercase", "INFO"},
		{"unknown", "trace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			hf := hostfunc.New(nil, capability.NewEnforcer())
			hf.Register(L, "test-plugin")

			err := L.DoString(`holomush.log("` + tt.level + `", "test message")`)
			assert.Error(t, err, "expected error for invalid log level %q", tt.level)
		})
	}
}

func TestHostFunctions_NewRequestID(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil, capability.NewEnforcer())
	hf.Register(L, "test-plugin")

	err := L.DoString(`id = holomush.new_request_id()`)
	require.NoError(t, err, "new_request_id() failed")

	id := L.GetGlobal("id").String()
	assert.Len(t, id, 26, "ULID should be 26 characters")

	// Verify it's a valid ULID (not just 26 random characters)
	_, err = ulid.Parse(id)
	assert.NoError(t, err, "id %q is not a valid ULID", id)
}

func TestHostFunctions_NewRequestID_Unique(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil, capability.NewEnforcer())
	hf.Register(L, "test-plugin")

	err := L.DoString(`
		id1 = holomush.new_request_id()
		id2 = holomush.new_request_id()
	`)
	require.NoError(t, err, "new_request_id() failed")

	id1 := L.GetGlobal("id1").String()
	id2 := L.GetGlobal("id2").String()
	assert.NotEqual(t, id1, id2, "IDs should be unique")
}

func TestHostFunctions_KV_RequiresCapability(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	// No capabilities granted

	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.kv_get("key")`)
	assert.Error(t, err, "expected capability error for kv_get without kv.read")
}

func TestHostFunctions_KVSet_RequiresWriteCapability(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	// Only kv.read, not kv.write
	err := enforcer.SetGrants("test-plugin", []string{"kv.read"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`holomush.kv_set("key", "value")`)
	assert.Error(t, err, "expected capability error for kv_set without kv.write")
}

func TestHostFunctions_KVDelete_RequiresWriteCapability(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	// Only kv.read, not kv.write
	err := enforcer.SetGrants("test-plugin", []string{"kv.read"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`holomush.kv_delete("key")`)
	assert.Error(t, err, "expected capability error for kv_delete without kv.write")
}

func TestHostFunctions_KV_WithCapability(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.read", "kv.write"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	// Set returns (nil, nil) on success
	err = L.DoString(`result, err = holomush.kv_set("mykey", "myvalue")`)
	require.NoError(t, err, "kv_set failed")

	setResult := L.GetGlobal("result")
	setErr := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, setResult.Type(), "kv_set result should be nil")
	assert.Equal(t, lua.LTNil, setErr.Type(), "kv_set err should be nil")

	// Get returns (value, nil) on success
	err = L.DoString(`result, err = holomush.kv_get("mykey")`)
	require.NoError(t, err, "kv_get failed")

	result := L.GetGlobal("result").String()
	assert.Equal(t, "myvalue", result)
}

func TestHostFunctions_KVGet_ReturnsNilForMissingKey(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.read"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`
		val, err = holomush.kv_get("nonexistent")
	`)
	require.NoError(t, err, "kv_get failed")

	val := L.GetGlobal("val")
	errVal := L.GetGlobal("err")

	assert.Equal(t, lua.LTNil, val.Type(), "expected nil for missing key")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error for missing key")
}

func TestHostFunctions_KVGet_NoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.read"})
	require.NoError(t, err)

	// nil kv store
	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`
		val, err = holomush.kv_get("key")
	`)
	require.NoError(t, err, "kv_get failed")

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string when kv store unavailable")
}

func TestHostFunctions_KVSet_NoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.write"})
	require.NoError(t, err)

	// nil kv store
	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`result, err = holomush.kv_set("key", "value")`)
	require.NoError(t, err, "kv_set failed")

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string when kv store unavailable")
}

func TestHostFunctions_KVDelete(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.read", "kv.write"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	// Set a key
	err = L.DoString(`holomush.kv_set("deletekey", "somevalue")`)
	require.NoError(t, err, "kv_set failed")

	// Delete it
	err = L.DoString(`holomush.kv_delete("deletekey")`)
	require.NoError(t, err, "kv_delete failed")

	// Verify it's gone
	err = L.DoString(`result, err = holomush.kv_get("deletekey")`)
	require.NoError(t, err, "kv_get failed")

	result := L.GetGlobal("result")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil after delete")
}

func TestHostFunctions_KVDelete_NoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.write"})
	require.NoError(t, err)

	// nil kv store
	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`result, err = holomush.kv_delete("key")`)
	require.NoError(t, err, "kv_delete failed")

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string when kv store unavailable")
}

func TestHostFunctions_KVGet_StoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.read"})
	require.NoError(t, err)

	kvStore := &mockKVStore{
		data:   make(map[string][]byte),
		getErr: errors.New("database connection failed"),
	}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`val, err = holomush.kv_get("key")`)
	require.NoError(t, err, "kv_get failed")

	errVal := L.GetGlobal("err")
	require.Equal(t, lua.LTString, errVal.Type(), "expected error string")

	// Error should be sanitized with correlation ID, not raw database error
	errMsg := errVal.String()
	assert.Contains(t, errMsg, "internal error (ref: ")
	assert.NotContains(t, errMsg, "database connection failed",
		"raw error should not leak to plugin")
}

func TestHostFunctions_KVSet_StoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.write"})
	require.NoError(t, err)

	kvStore := &mockKVStore{
		data:   make(map[string][]byte),
		setErr: errors.New("write failed: disk full"),
	}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`result, err = holomush.kv_set("key", "value")`)
	require.NoError(t, err, "kv_set failed")

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	require.Equal(t, lua.LTString, errVal.Type(), "expected error string")

	// Error should be sanitized with correlation ID, not raw database error
	errMsg := errVal.String()
	assert.Contains(t, errMsg, "internal error (ref: ")
	assert.NotContains(t, errMsg, "write failed",
		"raw error should not leak to plugin")
}

func TestHostFunctions_KVDelete_StoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.write"})
	require.NoError(t, err)

	kvStore := &mockKVStore{
		data:      make(map[string][]byte),
		deleteErr: errors.New("delete failed: permission denied"),
	}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`result, err = holomush.kv_delete("key")`)
	require.NoError(t, err, "kv_delete failed")

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	require.Equal(t, lua.LTString, errVal.Type(), "expected error string")

	// Error should be sanitized with correlation ID, not raw database error
	errMsg := errVal.String()
	assert.Contains(t, errMsg, "internal error (ref: ")
	assert.NotContains(t, errMsg, "delete failed",
		"raw error should not leak to plugin")
}

func TestHostFunctions_KV_MissingArguments(t *testing.T) {
	tests := []struct {
		name string
		code string
		caps []string
	}{
		{"kv_get no args", `holomush.kv_get()`, []string{"kv.read"}},
		{"kv_set no args", `holomush.kv_set()`, []string{"kv.write"}},
		{"kv_set only key", `holomush.kv_set("key")`, []string{"kv.write"}},
		{"kv_delete no args", `holomush.kv_delete()`, []string{"kv.write"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			enforcer := capability.NewEnforcer()
			err := enforcer.SetGrants("test-plugin", tt.caps)
			require.NoError(t, err)

			kvStore := &mockKVStore{data: make(map[string][]byte)}
			hf := hostfunc.New(kvStore, enforcer)
			hf.Register(L, "test-plugin")

			err = L.DoString(tt.code)
			assert.Error(t, err, "expected error for %s", tt.name)
		})
	}
}

func TestHostFunctions_KVGet_EmptyKeyRejected(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.read"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`holomush.kv_get("")`)
	assert.Error(t, err, "expected error for empty key")
}

func TestHostFunctions_KVSet_EmptyKeyRejected(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.write"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`holomush.kv_set("", "value")`)
	assert.Error(t, err, "expected error for empty key")
}

func TestHostFunctions_KVDelete_EmptyKeyRejected(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.write"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`holomush.kv_delete("")`)
	assert.Error(t, err, "expected error for empty key")
}

func TestHostFunctions_KVSet_EmptyValueAllowed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.read", "kv.write"})
	require.NoError(t, err)

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	// Empty values should be allowed (useful for clearing/resetting)
	err = L.DoString(`result, err = holomush.kv_set("mykey", "")`)
	require.NoError(t, err, "kv_set with empty value failed")

	setErr := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, setErr.Type(), "expected nil error for empty value")

	// Verify we can read it back
	err = L.DoString(`result, err = holomush.kv_get("mykey")`)
	require.NoError(t, err, "kv_get failed")

	result := L.GetGlobal("result")
	assert.Equal(t, lua.LTString, result.Type(), "expected string result")
	assert.Equal(t, "", result.String(), "expected empty string")
}

func TestHostFunctions_KV_CapabilityDenied_ErrorMessage(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	// No capabilities granted

	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.kv_get("key")`)
	require.Error(t, err, "expected capability error")

	// Error message should contain plugin name and capability for debugging
	errMsg := err.Error()
	assert.Contains(t, errMsg, "test-plugin", "error message should contain plugin name")
	assert.Contains(t, errMsg, "kv.read", "error message should contain capability name")
}

func TestHostFunctions_KVGet_Timeout(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.read"})
	require.NoError(t, err)

	// Create a slow store that exceeds the 5-second timeout
	kvStore := &slowKVStore{delay: 10 * time.Second}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`val, err = holomush.kv_get("key")`)
	require.NoError(t, err, "kv_get raised error instead of returning error tuple")

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string for timeout")

	// Sanitized timeout message (no raw context.DeadlineExceeded details)
	assert.Equal(t, "operation timed out", errVal.String())
}

func TestHostFunctions_KVSet_Timeout(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.write"})
	require.NoError(t, err)

	kvStore := &slowKVStore{delay: 10 * time.Second}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`result, err = holomush.kv_set("key", "value")`)
	require.NoError(t, err, "kv_set raised error instead of returning error tuple")

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string for timeout")

	// Sanitized timeout message
	assert.Equal(t, "operation timed out", errVal.String())
}

func TestHostFunctions_KVDelete_Timeout(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("test-plugin", []string{"kv.write"})
	require.NoError(t, err)

	kvStore := &slowKVStore{delay: 10 * time.Second}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err = L.DoString(`result, err = holomush.kv_delete("key")`)
	require.NoError(t, err, "kv_delete raised error instead of returning error tuple")

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string for timeout")

	// Sanitized timeout message
	assert.Equal(t, "operation timed out", errVal.String())
}

func TestHostFunctions_KV_NamespaceIsolation(t *testing.T) {
	kvStore := &mockKVStore{data: make(map[string][]byte)}

	// Plugin A writes
	L1 := lua.NewState()
	defer L1.Close()
	enforcer := capability.NewEnforcer()
	err := enforcer.SetGrants("plugin-a", []string{"kv.read", "kv.write"})
	require.NoError(t, err)
	err = enforcer.SetGrants("plugin-b", []string{"kv.read"})
	require.NoError(t, err)

	hfA := hostfunc.New(kvStore, enforcer)
	hfA.Register(L1, "plugin-a")

	err = L1.DoString(`holomush.kv_set("secret", "plugin-a-data")`)
	require.NoError(t, err, "plugin-a kv_set failed")

	// Plugin B tries to read - should get nil (different namespace)
	L2 := lua.NewState()
	defer L2.Close()
	hfB := hostfunc.New(kvStore, enforcer)
	hfB.Register(L2, "plugin-b")

	err = L2.DoString(`val, err = holomush.kv_get("secret")`)
	require.NoError(t, err, "plugin-b kv_get failed")

	val := L2.GetGlobal("val")
	assert.Equal(t, lua.LTNil, val.Type(), "plugin-b should not see plugin-a's data")
}

type mockKVStore struct {
	data      map[string][]byte
	getErr    error
	setErr    error
	deleteErr error
	mu        sync.RWMutex
}

func (m *mockKVStore) Get(_ context.Context, namespace, key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.data[namespace+":"+key], nil
}

func (m *mockKVStore) Set(_ context.Context, namespace, key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.setErr != nil {
		return m.setErr
	}
	m.data[namespace+":"+key] = value
	return nil
}

func (m *mockKVStore) Delete(_ context.Context, namespace, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.data, namespace+":"+key)
	return nil
}

// slowKVStore simulates a slow KV store that exceeds timeouts.
type slowKVStore struct {
	delay time.Duration
}

func (s *slowKVStore) Get(ctx context.Context, _, _ string) ([]byte, error) {
	select {
	case <-time.After(s.delay):
		return []byte("value"), nil
	case <-ctx.Done():
		return nil, fmt.Errorf("slow kv get: %w", ctx.Err())
	}
}

func (s *slowKVStore) Set(ctx context.Context, _, _ string, _ []byte) error {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return fmt.Errorf("slow kv set: %w", ctx.Err())
	}
}

func (s *slowKVStore) Delete(ctx context.Context, _, _ string) error {
	select {
	case <-time.After(s.delay):
		return nil
	case <-ctx.Done():
		return fmt.Errorf("slow kv delete: %w", ctx.Err())
	}
}
