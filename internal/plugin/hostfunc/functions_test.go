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

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/access/policy/policytest"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	"github.com/holomush/holomush/internal/world"
)

func TestWithWorldServiceAcceptsWorldMutator(t *testing.T) {
	// This test verifies that WithWorldService accepts a WorldMutator at construction time
	// The compile-time type check ensures only WorldMutator implementations can be passed
	// If this compiles, the interface enforcement is working

	// Create a simple mock that implements WorldMutator
	mutator := &mockWorldMutatorForConstructorTest{}

	// This should work without panicking
	hf := hostfunc.New(nil, hostfunc.WithWorldService(mutator))
	require.NotNil(t, hf, "hostfunc.New should return a Functions instance")
}

// mockWorldMutatorForConstructorTest implements WorldMutator for testing constructor behavior.
type mockWorldMutatorForConstructorTest struct{}

func (m *mockWorldMutatorForConstructorTest) GetLocation(_ context.Context, _ string, _ ulid.ULID) (*world.Location, error) {
	return nil, nil
}

func (m *mockWorldMutatorForConstructorTest) GetCharacter(_ context.Context, _ string, _ ulid.ULID) (*world.Character, error) {
	return nil, nil
}

func (m *mockWorldMutatorForConstructorTest) GetCharactersByLocation(_ context.Context, _ string, _ ulid.ULID, _ world.ListOptions) ([]*world.Character, error) {
	return nil, nil
}

func (m *mockWorldMutatorForConstructorTest) GetObject(_ context.Context, _ string, _ ulid.ULID) (*world.Object, error) {
	return nil, nil
}

func (m *mockWorldMutatorForConstructorTest) CreateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *mockWorldMutatorForConstructorTest) CreateExit(_ context.Context, _ string, _ *world.Exit) error {
	return nil
}

func (m *mockWorldMutatorForConstructorTest) CreateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *mockWorldMutatorForConstructorTest) UpdateLocation(_ context.Context, _ string, _ *world.Location) error {
	return nil
}

func (m *mockWorldMutatorForConstructorTest) UpdateObject(_ context.Context, _ string, _ *world.Object) error {
	return nil
}

func (m *mockWorldMutatorForConstructorTest) FindLocationByName(_ context.Context, _, _ string) (*world.Location, error) {
	return nil, nil
}

// Compile-time interface check.
var _ hostfunc.WorldMutator = (*mockWorldMutatorForConstructorTest)(nil)

func TestHostFunctionsLog(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`holomush.log("info", "test message")`)
	assert.NoError(t, err, "log() failed")
}

// TestHostfuncRegisterOmitsCapabilityFunctions asserts the atomic-cutover
// contract (holomush-eykuh.4, spec R1 / ADR holomush-05f3v): after the flip,
// hostfunc.Register installs the language stdlib ONLY. The ten capability host
// functions (kv, world.query, world.mutation, property, session, session.admin,
// focus, eval, settings, emit) MUST NOT be present on the holomush module — they
// flow exclusively through the host-brokered RegisterHostCaps path, gated by the
// resolver grant set. The retained stdlib (log, new_request_id, register_emit_type)
// MUST still be present.
func TestHostfuncRegisterOmitsCapabilityFunctions(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hostfunc.New(nil).Register(L, "core-help")

	mod, ok := L.GetGlobal("holomush").(*lua.LTable)
	require.True(t, ok, "holomush module must be a table")

	// The ten capability host functions MUST be gone from the legacy surface.
	stripped := []string{
		"kv_get", "kv_set", "kv_delete",
		"query_location", "query_character", "query_location_characters", "query_object",
		"create_location", "create_exit", "create_object", "find_location",
		"set_property", "get_property",
		"evaluate",
		"get_setting", "set_setting",
		"join_focus", "leave_focus", "present_focus",
	}
	for _, name := range stripped {
		assert.Equal(t, lua.LNil, mod.RawGetString(name),
			"capability fn %q must NOT be on the legacy hostfunc surface after the cutover", name)
	}

	// The language stdlib MUST remain.
	for _, name := range []string{"log", "new_request_id", "register_emit_type"} {
		assert.NotEqual(t, lua.LNil, mod.RawGetString(name),
			"stdlib fn %q must remain on the legacy hostfunc surface", name)
	}
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

			hf := hostfunc.New(nil)
			hf.Register(L, "test-plugin")
			hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

			err := L.DoString(`holomush.log("` + tt.level + `", "test message")`)
			assert.NoError(t, err, "log(%q) failed", tt.level)
		})
	}
}

func TestHostFunctionsLogInvalidLevel(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

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

			hf := hostfunc.New(nil)
			hf.Register(L, "test-plugin")
			hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

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

			hf := hostfunc.New(nil)
			hf.Register(L, "test-plugin")
			hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

			err := L.DoString(`holomush.log("` + tt.level + `", "test message")`)
			assert.Error(t, err, "expected error for invalid log level %q", tt.level)
		})
	}
}

func TestHostFunctionsNewRequestID(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`id = holomush.new_request_id()`)
	require.NoError(t, err, "new_request_id() failed")

	id := L.GetGlobal("id").String()
	assert.Len(t, id, 26, "ULID should be 26 characters")

	// Verify it's a valid ULID (not just 26 random characters)
	_, err = ulid.Parse(id)
	assert.NoError(t, err, "id %q is not a valid ULID", id)
}

func TestHostFunctionsNewRequestIDUnique(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil)
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`
		id1 = holomush.new_request_id()
		id2 = holomush.new_request_id()
	`)
	require.NoError(t, err, "new_request_id() failed")

	id1 := L.GetGlobal("id1").String()
	id2 := L.GetGlobal("id2").String()
	assert.NotEqual(t, id1, id2, "IDs should be unique")
}

func TestHostFunctionsKVWithKVStore(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	// Set returns (nil, nil) on success
	err := L.DoString(`result, err = holomush.kv_set("mykey", "myvalue")`)
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

func TestHostFunctionsKVGetReturnsNilForMissingKey(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`
		val, err = holomush.kv_get("nonexistent")
	`)
	require.NoError(t, err, "kv_get failed")

	val := L.GetGlobal("val")
	errVal := L.GetGlobal("err")

	assert.Equal(t, lua.LTNil, val.Type(), "expected nil for missing key")
	assert.Equal(t, lua.LTNil, errVal.Type(), "expected nil error for missing key")
}

func TestHostFunctionsKVGetNoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// nil kv store, engine allows so we reach the nil-store guard
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`
		val, err = holomush.kv_get("key")
	`)
	require.NoError(t, err, "kv_get failed")

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string when kv store unavailable")
}

func TestHostFunctionsKVSetNoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// nil kv store, engine allows so we reach the nil-store guard
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_set("key", "value")`)
	require.NoError(t, err, "kv_set failed")

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string when kv store unavailable")
}

func TestHostFunctionsKVDelete(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	// Set a key
	err := L.DoString(`holomush.kv_set("deletekey", "somevalue")`)
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

func TestHostFunctionsKVDeleteNoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// nil kv store, engine allows so we reach the nil-store guard
	hf := hostfunc.New(nil, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_delete("key")`)
	require.NoError(t, err, "kv_delete failed")

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTNil, result.Type(), "expected nil result")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string when kv store unavailable")
}

func TestHostFunctionsKVGetStoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{
		data:   make(map[string][]byte),
		getErr: errors.New("database connection failed"),
	}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`val, err = holomush.kv_get("key")`)
	require.NoError(t, err, "kv_get failed")

	errVal := L.GetGlobal("err")
	require.Equal(t, lua.LTString, errVal.Type(), "expected error string")

	// Error should be sanitized with correlation ID, not raw database error
	errMsg := errVal.String()
	assert.Contains(t, errMsg, "internal error (ref: ")
	assert.NotContains(t, errMsg, "database connection failed",
		"raw error should not leak to plugin")
}

func TestHostFunctionsKVSetStoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{
		data:   make(map[string][]byte),
		setErr: errors.New("write failed: disk full"),
	}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_set("key", "value")`)
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

func TestHostFunctionsKVDeleteStoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{
		data:      make(map[string][]byte),
		deleteErr: errors.New("delete failed: permission denied"),
	}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_delete("key")`)
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
	}{
		{"kv_get no args", `holomush.kv_get()`},
		{"kv_set no args", `holomush.kv_set()`},
		{"kv_set only key", `holomush.kv_set("key")`},
		{"kv_delete no args", `holomush.kv_delete()`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			L := lua.NewState()
			defer L.Close()

			kvStore := &mockKVStore{data: make(map[string][]byte)}
			hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
			hf.Register(L, "test-plugin")
			hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

			err := L.DoString(tt.code)
			assert.Error(t, err, "expected error for %s", tt.name)
		})
	}
}

func TestHostFunctionsKVGetEmptyKeyRejected(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`holomush.kv_get("")`)
	assert.Error(t, err, "expected error for empty key")
}

func TestHostFunctionsKVSetEmptyKeyRejected(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`holomush.kv_set("", "value")`)
	assert.Error(t, err, "expected error for empty key")
}

func TestHostFunctionsKVDeleteEmptyKeyRejected(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`holomush.kv_delete("")`)
	assert.Error(t, err, "expected error for empty key")
}

func TestHostFunctionsKVSetEmptyValueAllowed(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	// Empty values should be allowed (useful for clearing/resetting)
	err := L.DoString(`result, err = holomush.kv_set("mykey", "")`)
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

func TestHostFunctionsKVGetTimeout(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Create a slow store that exceeds the 5-second timeout
	kvStore := &slowKVStore{delay: 10 * time.Second}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`val, err = holomush.kv_get("key")`)
	require.NoError(t, err, "kv_get raised error instead of returning error tuple")

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string for timeout")

	// Sanitized timeout message (no raw context.DeadlineExceeded details)
	assert.Equal(t, "operation timed out", errVal.String())
}

func TestHostFunctionsKVSetTimeout(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &slowKVStore{delay: 10 * time.Second}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_set("key", "value")`)
	require.NoError(t, err, "kv_set raised error instead of returning error tuple")

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string for timeout")

	// Sanitized timeout message
	assert.Equal(t, "operation timed out", errVal.String())
}

func TestHostFunctionsKVDeleteTimeout(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	kvStore := &slowKVStore{delay: 10 * time.Second}
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_delete("key")`)
	require.NoError(t, err, "kv_delete raised error instead of returning error tuple")

	errVal := L.GetGlobal("err")
	assert.Equal(t, lua.LTString, errVal.Type(), "expected error string for timeout")

	// Sanitized timeout message
	assert.Equal(t, "operation timed out", errVal.String())
}

func TestHostFunctionsKVNamespaceIsolation(t *testing.T) {
	kvStore := &mockKVStore{data: make(map[string][]byte)}

	// Plugin A writes
	L1 := lua.NewState()
	defer L1.Close()

	hfA := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hfA.Register(L1, "plugin-a")
	hfA.RegisterCapabilityFuncsForTest(L1, "plugin-a")

	err := L1.DoString(`holomush.kv_set("secret", "plugin-a-data")`)
	require.NoError(t, err, "plugin-a kv_set failed")

	// Plugin B tries to read - should get nil (different namespace)
	L2 := lua.NewState()
	defer L2.Close()
	hfB := hostfunc.New(kvStore, hostfunc.WithEngine(policytest.AllowAllEngine()))
	hfB.Register(L2, "plugin-b")
	hfB.RegisterCapabilityFuncsForTest(L2, "plugin-b")

	err = L2.DoString(`val, err = holomush.kv_get("secret")`)
	require.NoError(t, err, "plugin-b kv_get failed")

	val := L2.GetGlobal("val")
	assert.Equal(t, lua.LTNil, val.Type(), "plugin-b should not see plugin-a's data")
}

func TestKVGetDeniedByEngine(t *testing.T) {
	kvStore := &mockKVStore{data: make(map[string][]byte)}
	engine := policytest.DenyAllEngine()
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`
		local val, err = holomush.kv_get("mykey")
		result_val = val
		result_err = err
	`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("result_val"))
	errStr := L.GetGlobal("result_err")
	assert.Contains(t, errStr.String(), "access denied")
}

func TestKVGetAllowedByEngine(t *testing.T) {
	kvStore := &mockKVStore{data: make(map[string][]byte)}
	kvStore.data["test-plugin:mykey"] = []byte("hello")
	engine := policytest.AllowAllEngine()
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`
		local val, err = holomush.kv_get("mykey")
		result_val = val
		result_err = err
	`)
	require.NoError(t, err)

	assert.Equal(t, "hello", L.GetGlobal("result_val").String())
	assert.Equal(t, lua.LNil, L.GetGlobal("result_err"))
}

func TestKVGetNilEngineDenied(t *testing.T) {
	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore) // No WithEngine

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`
		local val, err = holomush.kv_get("mykey")
		result_val = val
		result_err = err
	`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("result_val"))
	errStr := L.GetGlobal("result_err")
	assert.Contains(t, errStr.String(), "access engine not available")
}

func TestKVSetDeniedByEngine(t *testing.T) {
	kvStore := &mockKVStore{data: make(map[string][]byte)}
	engine := policytest.DenyAllEngine()
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`
		local result, err = holomush.kv_set("mykey", "myvalue")
		result_val = result
		result_err = err
	`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("result_val"))
	errStr := L.GetGlobal("result_err")
	assert.Contains(t, errStr.String(), "access denied")

	// Verify store was NOT called (data map should be empty)
	assert.Empty(t, kvStore.data, "kvStore should not have been called when access denied")
}

func TestKVDeleteDeniedByEngine(t *testing.T) {
	kvStore := &mockKVStore{data: make(map[string][]byte)}
	kvStore.data["test-plugin:mykey"] = []byte("value")
	engine := policytest.DenyAllEngine()
	hf := hostfunc.New(kvStore, hostfunc.WithEngine(engine))

	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")

	err := L.DoString(`
		local result, err = holomush.kv_delete("mykey")
		result_val = result
		result_err = err
	`)
	require.NoError(t, err)

	assert.Equal(t, lua.LNil, L.GetGlobal("result_val"))
	errStr := L.GetGlobal("result_err")
	assert.Contains(t, errStr.String(), "access denied")

	// Verify store was NOT called (data should still contain the key)
	assert.Contains(t, kvStore.data, "test-plugin:mykey", "kvStore should not have been called when access denied")
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

func TestSetFocusOpsLateBindingMakesCallsAvailable(t *testing.T) {
	hf := hostfunc.New(nil)

	// Before SetFocusOps, calls are no-ops (nil focus ops).
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")
	targetID := ulid.Make()
	require.NoError(t, L.DoString(`holomush.join_focus("s", "scene", "`+targetID.String()+`")`))

	// After SetFocusOps, calls route to the new ops.
	fo := &mockFocusOps{}
	hf.SetFocusOps(fo)

	L2 := lua.NewState()
	defer L2.Close()
	hf.Register(L2, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L2, "test-plugin")
	targetID2 := ulid.Make()
	require.NoError(t, L2.DoString(`holomush.join_focus("sess-1", "scene", "`+targetID2.String()+`")`))
	require.Len(t, fo.joinCalls, 1)
	assert.Equal(t, "sess-1", fo.joinCalls[0].sessionID)
}

func TestSetHistoryReaderLateBindingMakesCallsAvailable(t *testing.T) {
	hf := hostfunc.New(nil)

	// Before SetHistoryReader, calls are no-ops (nil reader).
	L := lua.NewState()
	defer L.Close()
	hf.Register(L, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L, "test-plugin")
	require.NoError(t, L.DoString(`holomush.query_stream_history({stream="scene:abc:ic", count=10})`))

	// After SetHistoryReader, calls route to the new reader.
	hr := &mockHistoryReader{}
	hf.SetHistoryReader(hr)

	L2 := lua.NewState()
	defer L2.Close()
	hf.Register(L2, "test-plugin")
	hf.RegisterCapabilityFuncsForTest(L2, "test-plugin")
	require.NoError(t, L2.DoString(`holomush.query_stream_history({stream="scene:abc:ic", count=10})`))
	require.Len(t, hr.calls, 1)
	assert.Equal(t, "scene:abc:ic", hr.calls[0].stream)
}
