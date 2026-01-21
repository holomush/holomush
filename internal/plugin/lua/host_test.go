// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/capability"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	pluginpkg "github.com/holomush/holomush/pkg/plugin"
)

// writeMainLua creates a main.lua plugin file in the given directory.
func writeMainLua(t *testing.T, dir, content string) {
	t.Helper()
	err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(content), 0o600)
	require.NoError(t, err, "failed to write main.lua")
}

// closeHost closes the host and fails the test if an error occurs.
func closeHost(t *testing.T, host *pluginlua.Host) {
	t.Helper()
	err := host.Close(context.Background())
	require.NoError(t, err, "Close() failed")
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
	require.NoError(t, err, "Load() failed")

	plugins := host.Plugins()
	require.Len(t, plugins, 1, "expected 1 plugin")
	assert.Equal(t, "test-plugin", plugins[0])
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

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
	require.NoError(t, err, "DeliverEvent() failed")
	require.Len(t, emits, 1, "expected 1 emit event")

	// Verify all fields of the emitted event
	assert.Equal(t, "location:123", emits[0].Stream)
	assert.Equal(t, pluginpkg.EventType("say"), emits[0].Type)
	assert.Contains(t, emits[0].Payload, "Echo:")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "no-handler", event)
	require.NoError(t, err, "DeliverEvent() failed")
	assert.Empty(t, emits, "expected no emits for plugin without handler")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)
	require.Len(t, host.Plugins(), 1, "expected 1 plugin after load")

	err = host.Unload(context.Background(), "test-plugin")
	require.NoError(t, err, "Unload() failed")
	assert.Empty(t, host.Plugins(), "expected 0 plugins after unload")
}

func TestLuaHost_Unload_NotFound(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	err := host.Unload(context.Background(), "nonexistent")
	assert.Error(t, err, "expected error when unloading nonexistent plugin")
}

func TestLuaHost_DeliverEvent_NotLoaded(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	_, err := host.DeliverEvent(context.Background(), "nonexistent", event)
	assert.Error(t, err, "expected error when delivering to nonexistent plugin")
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
	assert.Error(t, err, "expected error when loading plugin with syntax error")
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
	assert.Error(t, err, "expected error when loading plugin with missing file")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	err = host.Close(context.Background())
	require.NoError(t, err, "Close() failed")

	// Should error when loading after close
	err = host.Load(context.Background(), manifest, dir)
	assert.Error(t, err, "expected error when loading after close")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	_, err = host.DeliverEvent(context.Background(), "error-plugin", event)
	require.Error(t, err, "expected error when plugin throws runtime error")
	// Error should include plugin name for debugging
	assert.Contains(t, err.Error(), "error-plugin", "error should contain plugin name")
	assert.Contains(t, err.Error(), "on_event failed", "error should contain 'on_event failed'")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

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
			require.NoError(t, err, "DeliverEvent() failed")
			require.Len(t, emits, 1, "expected 1 emit event")
			assert.Equal(t, tt.expected, emits[0].Payload, "actor_kind mismatch")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "bad-return", event)
	require.NoError(t, err, "DeliverEvent() failed")

	// Non-table returns should be gracefully ignored (empty emits)
	assert.Empty(t, emits, "expected empty emits for non-table return")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "mixed-return", event)
	require.NoError(t, err, "DeliverEvent() failed")

	// Should only get the 2 valid emit events
	require.Len(t, emits, 2, "expected 2 valid events only")
	assert.Equal(t, "valid:1", emits[0].Stream)
	assert.Equal(t, "valid:2", emits[1].Stream)
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	err = host.Close(context.Background())
	require.NoError(t, err, "Close() failed")

	// DeliverEvent should error after close
	event := pluginpkg.Event{ID: "01ABC", Type: "say"}
	_, err = host.DeliverEvent(context.Background(), "test-plugin", event)
	assert.Error(t, err, "expected error when delivering after close")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

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
	require.NoError(t, err, "DeliverEvent() failed")
	require.Len(t, emits, 1, "expected 1 emit event")

	expected := "01ABC|location:123|say|1705591234000|character|char_1"
	assert.Equal(t, expected, emits[0].Payload)
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginpkg.Event{
		ID:     "01ABC",
		Stream: "location:123",
		Type:   "say",
	}

	emits, err := host.DeliverEvent(context.Background(), "test", event)
	require.NoError(t, err, "DeliverEvent() failed")
	assert.Len(t, emits, 1, "expected 1 emit event")

	// Verify the payload contains a request_id (ULID format: 26 chars)
	if len(emits) > 0 {
		assert.Contains(t, emits[0].Payload, "request_id", "payload should contain request_id")
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

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginpkg.Event{
		ID:     "01ABC",
		Stream: "location:123",
		Type:   "say",
	}

	// Should fail because plugin lacks kv.read capability
	_, err = host.DeliverEvent(context.Background(), "no-kv-cap", event)
	require.Error(t, err, "expected error due to capability denial")
	assert.Contains(t, err.Error(), "capability denied", "error should mention capability denial")
}

func TestLuaHost_NewHostWithFunctions_NilPanics(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic when hostFuncs is nil")
		// Verify panic message is descriptive
		msg, ok := r.(string)
		require.True(t, ok, "panic should be a string")
		assert.Contains(t, msg, "hostFuncs cannot be nil", "panic message should mention 'hostFuncs cannot be nil'")
	}()

	_ = pluginlua.NewHostWithFunctions(nil)
}
