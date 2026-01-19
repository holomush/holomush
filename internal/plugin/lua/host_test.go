// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// writeMainLua creates a main.lua plugin file in the given directory.
func writeMainLua(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// closeHost closes the host and fails the test if an error occurs.
func closeHost(t *testing.T, host *pluginlua.Host) {
	t.Helper()
	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestLuaHost_Load(t *testing.T) {
	dir := t.TempDir()

	mainLua := `
function on_event(event)
    return nil
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{
			Entry: "main.lua",
		},
	}

	err := host.Load(context.Background(), manifest, dir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	plugins := host.Plugins()
	if len(plugins) != 1 || plugins[0] != "test-plugin" {
		t.Errorf("Plugins() = %v, want [test-plugin]", plugins)
	}
}

func TestLuaHost_DeliverEvent_ReturnsEmitEvents(t *testing.T) {
	dir := t.TempDir()

	mainLua := `
function on_event(event)
    if event.type == "say" then
        return {
            {
                stream = event.stream,
                type = "say",
                payload = '{"message":"Echo: ' .. event.payload .. '"}'
            }
        }
    end
    return nil
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "echo",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{
		ID:        "01ABC",
		Stream:    "location:123",
		Type:      "say",
		Timestamp: 1705591234000,
		ActorKind: pluginpkg.ActorCharacter,
		ActorID:   "char_1",
		Payload:   "Hello",
	}

	emits, err := host.DeliverEvent(context.Background(), "echo", event)
	if err != nil {
		t.Fatalf("DeliverEvent() error = %v", err)
	}

	if len(emits) != 1 {
		t.Fatalf("len(emits) = %d, want 1", len(emits))
	}

	// Verify all fields of the emitted event
	if emits[0].Stream != "location:123" {
		t.Errorf("emit.Stream = %q, want %q", emits[0].Stream, "location:123")
	}
	if emits[0].Type != "say" {
		t.Errorf("emit.Type = %q, want %q", emits[0].Type, "say")
	}
	if !strings.Contains(emits[0].Payload, "Echo:") {
		t.Errorf("emit.Payload = %q, want to contain %q", emits[0].Payload, "Echo:")
	}
}

func TestLuaHost_DeliverEvent_NoHandler(t *testing.T) {
	dir := t.TempDir()

	// Plugin without on_event function
	writeMainLua(t, dir, `x = 1`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "no-handler",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "no-handler", event)
	if err != nil {
		t.Fatalf("DeliverEvent() error = %v", err)
	}

	if len(emits) != 0 {
		t.Errorf("expected no emits for plugin without handler")
	}
}

func TestLuaHost_Unload(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_event(event) return nil end`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	if len(host.Plugins()) != 1 {
		t.Fatalf("expected 1 plugin after load")
	}

	if err := host.Unload(context.Background(), "test-plugin"); err != nil {
		t.Fatalf("Unload() error = %v", err)
	}

	if len(host.Plugins()) != 0 {
		t.Errorf("expected 0 plugins after unload, got %d", len(host.Plugins()))
	}
}

func TestLuaHost_Unload_NotFound(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	err := host.Unload(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error when unloading nonexistent plugin")
	}
}

func TestLuaHost_DeliverEvent_NotLoaded(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	_, err := host.DeliverEvent(context.Background(), "nonexistent", event)
	if err == nil {
		t.Error("expected error when delivering to nonexistent plugin")
	}
}

func TestLuaHost_Load_SyntaxError(t *testing.T) {
	dir := t.TempDir()

	// Invalid Lua syntax
	writeMainLua(t, dir, `function on_event(event return nil end`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "bad-syntax",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	if err == nil {
		t.Error("expected error when loading plugin with syntax error")
	}
}

func TestLuaHost_Load_MissingFile(t *testing.T) {
	dir := t.TempDir()

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "missing-file",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "nonexistent.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	if err == nil {
		t.Error("expected error when loading plugin with missing file")
	}
}

func TestLuaHost_Close(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_event(event) return nil end`)

	host := pluginlua.NewHost()

	manifest := &plugin.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// Should error when loading after close
	err := host.Load(context.Background(), manifest, dir)
	if err == nil {
		t.Error("expected error when loading after close")
	}
}

func TestLuaHost_DeliverEvent_RuntimeError(t *testing.T) {
	dir := t.TempDir()

	// Plugin that throws a runtime error
	writeMainLua(t, dir, `
function on_event(event)
    error("intentional failure")
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "error-plugin",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	_, err := host.DeliverEvent(context.Background(), "error-plugin", event)
	if err == nil {
		t.Error("expected error when plugin throws runtime error")
	}
	// Error should include plugin name for debugging
	if !strings.Contains(err.Error(), "error-plugin") {
		t.Errorf("error should contain plugin name 'error-plugin', got: %v", err)
	}
	if !strings.Contains(err.Error(), "on_event failed") {
		t.Errorf("error should contain 'on_event failed', got: %v", err)
	}
}

func TestLuaHost_DeliverEvent_ActorKinds(t *testing.T) {
	dir := t.TempDir()

	// Plugin that returns the actor_kind it received
	writeMainLua(t, dir, `
function on_event(event)
    return {
        {
            stream = "test:1",
            type = "echo",
            payload = event.actor_kind
        }
    }
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "actor-test",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		kind     pluginpkg.ActorKind
		expected string
	}{
		{pluginpkg.ActorCharacter, "character"},
		{pluginpkg.ActorSystem, "system"},
		{pluginpkg.ActorPlugin, "plugin"},
		{pluginpkg.ActorKind(99), "unknown"}, // Unknown kind
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			event := pluginpkg.Event{
				ID:        "01ABC",
				Type:      "say",
				ActorKind: tt.kind,
			}

			emits, err := host.DeliverEvent(context.Background(), "actor-test", event)
			if err != nil {
				t.Fatalf("DeliverEvent() error = %v", err)
			}

			if len(emits) != 1 {
				t.Fatalf("len(emits) = %d, want 1", len(emits))
			}

			if emits[0].Payload != tt.expected {
				t.Errorf("actor_kind = %q, want %q", emits[0].Payload, tt.expected)
			}
		})
	}
}

func TestLuaHost_DeliverEvent_NonTableReturn(t *testing.T) {
	dir := t.TempDir()

	// Plugin that returns a string instead of a table
	writeMainLua(t, dir, `
function on_event(event)
    return "not a table"
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "bad-return",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "bad-return", event)
	if err != nil {
		t.Fatalf("DeliverEvent() error = %v", err)
	}

	// Non-table returns should be gracefully ignored (empty emits)
	if len(emits) != 0 {
		t.Errorf("expected empty emits for non-table return, got %d", len(emits))
	}
}

func TestLuaHost_DeliverEvent_MalformedEmitEvents(t *testing.T) {
	dir := t.TempDir()

	// Plugin that returns a mix of valid and invalid emit events
	writeMainLua(t, dir, `
function on_event(event)
    return {
        {
            stream = "valid:1",
            type = "test",
            payload = "valid"
        },
        "not a table",  -- Should be skipped
        123,            -- Should be skipped
        {
            stream = "valid:2",
            type = "test",
            payload = "also valid"
        }
    }
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "mixed-return",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "mixed-return", event)
	if err != nil {
		t.Fatalf("DeliverEvent() error = %v", err)
	}

	// Should only get the 2 valid emit events
	if len(emits) != 2 {
		t.Fatalf("len(emits) = %d, want 2 (valid events only)", len(emits))
	}

	if emits[0].Stream != "valid:1" {
		t.Errorf("emits[0].Stream = %q, want %q", emits[0].Stream, "valid:1")
	}
	if emits[1].Stream != "valid:2" {
		t.Errorf("emits[1].Stream = %q, want %q", emits[1].Stream, "valid:2")
	}
}

func TestLuaHost_DeliverEvent_AfterClose(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_event(event) return nil end`)

	host := pluginlua.NewHost()

	manifest := &plugin.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	if err := host.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// DeliverEvent should error after close
	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	_, err := host.DeliverEvent(context.Background(), "test-plugin", event)
	if err == nil {
		t.Error("expected error when delivering after close")
	}
}

func TestLuaHost_DeliverEvent_AllFields(t *testing.T) {
	dir := t.TempDir()

	// Plugin that verifies all event fields are accessible
	writeMainLua(t, dir, `
function on_event(event)
    return {
        {
            stream = "test:1",
            type = "echo",
            payload = event.id .. "|" .. event.stream .. "|" .. event.type .. "|" ..
                      tostring(event.timestamp) .. "|" .. event.actor_kind .. "|" .. event.actor_id
        }
    }
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "field-test",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{
		ID:        "01ABC",
		Stream:    "location:123",
		Type:      "say",
		Timestamp: 1705591234000,
		ActorKind: pluginpkg.ActorCharacter,
		ActorID:   "char_1",
		Payload:   "Hello",
	}

	emits, err := host.DeliverEvent(context.Background(), "field-test", event)
	if err != nil {
		t.Fatalf("DeliverEvent() error = %v", err)
	}

	if len(emits) != 1 {
		t.Fatalf("len(emits) = %d, want 1", len(emits))
	}

	expected := "01ABC|location:123|say|1705591234000|character|char_1"
	if emits[0].Payload != expected {
		t.Errorf("payload = %q, want %q", emits[0].Payload, expected)
	}
}

func TestLuaHost_WithHostFunctions(t *testing.T) {
	dir := t.TempDir()

	mainLua := `
function on_event(event)
    local id = holomush.new_request_id()
    holomush.log("info", "Got event: " .. event.type)
    return {{
        stream = event.stream,
        type = "say",
        payload = '{"request_id":"' .. id .. '"}'
    }}
end
`
	writeMainLua(t, dir, mainLua)

	enforcer := capability.NewEnforcer()

	hostFuncs := hostfunc.New(nil, enforcer)
	host := pluginlua.NewHostWithFunctions(hostFuncs)
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "test",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{
		ID:     "01ABC",
		Stream: "location:123",
		Type:   "say",
	}

	emits, err := host.DeliverEvent(context.Background(), "test", event)
	if err != nil {
		t.Fatalf("DeliverEvent() error = %v", err)
	}

	if len(emits) != 1 {
		t.Errorf("len(emits) = %d, want 1", len(emits))
	}

	// Verify the payload contains a request_id (ULID format: 26 chars)
	if len(emits) > 0 && !strings.Contains(emits[0].Payload, "request_id") {
		t.Errorf("payload should contain request_id, got %q", emits[0].Payload)
	}
}

func TestLuaHost_WithHostFunctions_CapabilityEnforcement(t *testing.T) {
	dir := t.TempDir()

	// Plugin that tries to use kv_get without capability
	mainLua := `
function on_event(event)
    local val, err = holomush.kv_get("mykey")
    if err then
        return {{
            stream = event.stream,
            type = "error",
            payload = err
        }}
    end
    return nil
end
`
	writeMainLua(t, dir, mainLua)

	enforcer := capability.NewEnforcer()
	// Note: NOT granting kv.read capability to trigger denial

	hostFuncs := hostfunc.New(nil, enforcer)
	host := pluginlua.NewHostWithFunctions(hostFuncs)
	defer closeHost(t, host)

	manifest := &plugin.Manifest{
		Name:      "no-kv-cap",
		Version:   "1.0.0",
		Type:      plugin.TypeLua,
		LuaPlugin: &plugin.LuaConfig{Entry: "main.lua"},
	}

	if err := host.Load(context.Background(), manifest, dir); err != nil {
		t.Fatal(err)
	}

	event := pluginpkg.Event{
		ID:     "01ABC",
		Stream: "location:123",
		Type:   "say",
	}

	// Should fail because plugin lacks kv.read capability
	_, err := host.DeliverEvent(context.Background(), "no-kv-cap", event)
	if err == nil {
		t.Fatal("expected error due to capability denial")
	}
	if !strings.Contains(err.Error(), "capability denied") {
		t.Errorf("expected capability denied error, got: %v", err)
	}
}

func TestLuaHost_NewHostWithFunctions_NilPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("expected panic when hostFuncs is nil")
		}
		// Verify panic message is descriptive
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "hostFuncs cannot be nil") {
			t.Errorf("panic message should mention 'hostFuncs cannot be nil', got: %v", r)
		}
	}()

	_ = pluginlua.NewHostWithFunctions(nil)
}
