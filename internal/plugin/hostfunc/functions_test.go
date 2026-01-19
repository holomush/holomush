// Package hostfunc_test tests host function implementations.
package hostfunc_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	lua "github.com/yuin/gopher-lua"
)

func TestNew_NilEnforcerPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for nil enforcer")
		}
	}()

	hostfunc.New(nil, nil)
}

func TestHostFunctions_Log(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil, capability.NewEnforcer())
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.log("info", "test message")`)
	if err != nil {
		t.Errorf("log() failed: %v", err)
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

			hf := hostfunc.New(nil, capability.NewEnforcer())
			hf.Register(L, "test-plugin")

			err := L.DoString(`holomush.log("` + tt.level + `", "test message")`)
			if err != nil {
				t.Errorf("log(%q) failed: %v", tt.level, err)
			}
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
	if err == nil {
		t.Error("expected error for invalid log level")
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
			if err == nil {
				t.Errorf("expected error for invalid log level %q", tt.level)
			}
		})
	}
}

func TestHostFunctions_NewRequestID(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	hf := hostfunc.New(nil, capability.NewEnforcer())
	hf.Register(L, "test-plugin")

	err := L.DoString(`id = holomush.new_request_id()`)
	if err != nil {
		t.Fatalf("new_request_id() failed: %v", err)
	}

	id := L.GetGlobal("id").String()
	if len(id) != 26 { // ULID length
		t.Errorf("id length = %d, want 26", len(id))
	}
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
	if err != nil {
		t.Fatalf("new_request_id() failed: %v", err)
	}

	id1 := L.GetGlobal("id1").String()
	id2 := L.GetGlobal("id2").String()
	if id1 == id2 {
		t.Errorf("IDs should be unique, got %q twice", id1)
	}
}

func TestHostFunctions_KV_RequiresCapability(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	// No capabilities granted

	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.kv_get("key")`)
	if err == nil {
		t.Error("expected capability error for kv_get without kv.read")
	}
}

func TestHostFunctions_KVSet_RequiresWriteCapability(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	// Only kv.read, not kv.write
	if err := enforcer.SetGrants("test-plugin", []string{"kv.read"}); err != nil {
		t.Fatal(err)
	}

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.kv_set("key", "value")`)
	if err == nil {
		t.Error("expected capability error for kv_set without kv.write")
	}
}

func TestHostFunctions_KVDelete_RequiresWriteCapability(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	// Only kv.read, not kv.write
	if err := enforcer.SetGrants("test-plugin", []string{"kv.read"}); err != nil {
		t.Fatal(err)
	}

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`holomush.kv_delete("key")`)
	if err == nil {
		t.Error("expected capability error for kv_delete without kv.write")
	}
}

func TestHostFunctions_KV_WithCapability(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.read", "kv.write"}); err != nil {
		t.Fatal(err)
	}

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	// Set returns (nil, nil) on success
	err := L.DoString(`result, err = holomush.kv_set("mykey", "myvalue")`)
	if err != nil {
		t.Fatalf("kv_set failed: %v", err)
	}

	setResult := L.GetGlobal("result")
	setErr := L.GetGlobal("err")
	if setResult.Type() != lua.LTNil {
		t.Errorf("kv_set result = %v, want nil", setResult)
	}
	if setErr.Type() != lua.LTNil {
		t.Errorf("kv_set err = %v, want nil", setErr)
	}

	// Get returns (value, nil) on success
	err = L.DoString(`result, err = holomush.kv_get("mykey")`)
	if err != nil {
		t.Fatalf("kv_get failed: %v", err)
	}

	result := L.GetGlobal("result").String()
	if result != "myvalue" {
		t.Errorf("result = %q, want %q", result, "myvalue")
	}
}

func TestHostFunctions_KVGet_ReturnsNilForMissingKey(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.read"}); err != nil {
		t.Fatal(err)
	}

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`
		val, err = holomush.kv_get("nonexistent")
	`)
	if err != nil {
		t.Fatalf("kv_get failed: %v", err)
	}

	val := L.GetGlobal("val")
	errVal := L.GetGlobal("err")

	if val.Type() != lua.LTNil {
		t.Errorf("expected nil for missing key, got %v", val)
	}
	if errVal.Type() != lua.LTNil {
		t.Errorf("expected nil error for missing key, got %v", errVal)
	}
}

func TestHostFunctions_KVGet_NoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.read"}); err != nil {
		t.Fatal(err)
	}

	// nil kv store
	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`
		val, err = holomush.kv_get("key")
	`)
	if err != nil {
		t.Fatalf("kv_get failed: %v", err)
	}

	errVal := L.GetGlobal("err")
	if errVal.Type() != lua.LTString {
		t.Errorf("expected error string when kv store unavailable, got %v", errVal.Type())
	}
}

func TestHostFunctions_KVSet_NoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.write"}); err != nil {
		t.Fatal(err)
	}

	// nil kv store
	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_set("key", "value")`)
	if err != nil {
		t.Fatalf("kv_set failed: %v", err)
	}

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	if result.Type() != lua.LTNil {
		t.Errorf("expected nil result, got %v", result.Type())
	}
	if errVal.Type() != lua.LTString {
		t.Errorf("expected error string when kv store unavailable, got %v", errVal.Type())
	}
}

func TestHostFunctions_KVDelete(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.read", "kv.write"}); err != nil {
		t.Fatal(err)
	}

	kvStore := &mockKVStore{data: make(map[string][]byte)}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	// Set a key
	err := L.DoString(`holomush.kv_set("deletekey", "somevalue")`)
	if err != nil {
		t.Fatalf("kv_set failed: %v", err)
	}

	// Delete it
	err = L.DoString(`holomush.kv_delete("deletekey")`)
	if err != nil {
		t.Fatalf("kv_delete failed: %v", err)
	}

	// Verify it's gone
	err = L.DoString(`result, err = holomush.kv_get("deletekey")`)
	if err != nil {
		t.Fatalf("kv_get failed: %v", err)
	}

	result := L.GetGlobal("result")
	if result.Type() != lua.LTNil {
		t.Errorf("expected nil after delete, got %v", result)
	}
}

func TestHostFunctions_KVDelete_NoStoreAvailable(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.write"}); err != nil {
		t.Fatal(err)
	}

	// nil kv store
	hf := hostfunc.New(nil, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_delete("key")`)
	if err != nil {
		t.Fatalf("kv_delete failed: %v", err)
	}

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	if result.Type() != lua.LTNil {
		t.Errorf("expected nil result, got %v", result.Type())
	}
	if errVal.Type() != lua.LTString {
		t.Errorf("expected error string when kv store unavailable, got %v", errVal.Type())
	}
}

func TestHostFunctions_KVGet_StoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.read"}); err != nil {
		t.Fatal(err)
	}

	kvStore := &mockKVStore{
		data:   make(map[string][]byte),
		getErr: errors.New("database connection failed"),
	}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`val, err = holomush.kv_get("key")`)
	if err != nil {
		t.Fatalf("kv_get failed: %v", err)
	}

	errVal := L.GetGlobal("err")
	if errVal.Type() != lua.LTString {
		t.Fatalf("expected error string, got %v", errVal.Type())
	}
	if errVal.String() != "database connection failed" {
		t.Errorf("error = %q, want %q", errVal.String(), "database connection failed")
	}
}

func TestHostFunctions_KVSet_StoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.write"}); err != nil {
		t.Fatal(err)
	}

	kvStore := &mockKVStore{
		data:   make(map[string][]byte),
		setErr: errors.New("write failed: disk full"),
	}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_set("key", "value")`)
	if err != nil {
		t.Fatalf("kv_set failed: %v", err)
	}

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	if result.Type() != lua.LTNil {
		t.Errorf("expected nil result, got %v", result.Type())
	}
	if errVal.Type() != lua.LTString {
		t.Fatalf("expected error string, got %v", errVal.Type())
	}
	if errVal.String() != "write failed: disk full" {
		t.Errorf("error = %q, want %q", errVal.String(), "write failed: disk full")
	}
}

func TestHostFunctions_KVDelete_StoreError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("test-plugin", []string{"kv.write"}); err != nil {
		t.Fatal(err)
	}

	kvStore := &mockKVStore{
		data:      make(map[string][]byte),
		deleteErr: errors.New("delete failed: permission denied"),
	}
	hf := hostfunc.New(kvStore, enforcer)
	hf.Register(L, "test-plugin")

	err := L.DoString(`result, err = holomush.kv_delete("key")`)
	if err != nil {
		t.Fatalf("kv_delete failed: %v", err)
	}

	result := L.GetGlobal("result")
	errVal := L.GetGlobal("err")
	if result.Type() != lua.LTNil {
		t.Errorf("expected nil result, got %v", result.Type())
	}
	if errVal.Type() != lua.LTString {
		t.Fatalf("expected error string, got %v", errVal.Type())
	}
	if errVal.String() != "delete failed: permission denied" {
		t.Errorf("error = %q, want %q", errVal.String(), "delete failed: permission denied")
	}
}

func TestHostFunctions_KV_NamespaceIsolation(t *testing.T) {
	kvStore := &mockKVStore{data: make(map[string][]byte)}

	// Plugin A writes
	L1 := lua.NewState()
	defer L1.Close()
	enforcer := capability.NewEnforcer()
	if err := enforcer.SetGrants("plugin-a", []string{"kv.read", "kv.write"}); err != nil {
		t.Fatal(err)
	}
	if err := enforcer.SetGrants("plugin-b", []string{"kv.read"}); err != nil {
		t.Fatal(err)
	}

	hfA := hostfunc.New(kvStore, enforcer)
	hfA.Register(L1, "plugin-a")

	err := L1.DoString(`holomush.kv_set("secret", "plugin-a-data")`)
	if err != nil {
		t.Fatalf("plugin-a kv_set failed: %v", err)
	}

	// Plugin B tries to read - should get nil (different namespace)
	L2 := lua.NewState()
	defer L2.Close()
	hfB := hostfunc.New(kvStore, enforcer)
	hfB.Register(L2, "plugin-b")

	err = L2.DoString(`val, err = holomush.kv_get("secret")`)
	if err != nil {
		t.Fatalf("plugin-b kv_get failed: %v", err)
	}

	val := L2.GetGlobal("val")
	if val.Type() != lua.LTNil {
		t.Errorf("plugin-b should not see plugin-a's data, got %v", val)
	}
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
