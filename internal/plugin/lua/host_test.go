// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package lua_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/grpc/focus"
	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/hostfunc"
	pluginlua "github.com/holomush/holomush/internal/plugin/lua"
	"github.com/holomush/holomush/internal/session"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
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

func TestLuaHostLoad(t *testing.T) {
	dir := t.TempDir()

	mainLua := `
function on_event(event)
    return nil
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:    "test-plugin",
		Version: "1.0.0",
		Type:    plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{
			Entry: "main.lua",
		},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err, "Load() failed")

	plugins := host.Plugins()
	require.Len(t, plugins, 1, "expected 1 plugin")
	assert.Equal(t, "test-plugin", plugins[0])
}

func TestLuaHostDeliverEventReturnsEmitEvents(t *testing.T) {
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

	manifest := &plugins.Manifest{
		Name:      "echo",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{
		ID:        "01ABC",
		Stream:    "location:123",
		Type:      "say",
		Timestamp: 1705591234000,
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   "char_1",
		Payload:   "Hello",
	}

	emits, err := host.DeliverEvent(context.Background(), "echo", event)
	require.NoError(t, err, "DeliverEvent() failed")
	require.Len(t, emits, 1, "expected 1 emit event")

	// Verify all fields of the emitted event
	assert.Equal(t, "location:123", emits[0].Stream)
	assert.Equal(t, pluginsdk.EventType("say"), emits[0].Type)
	assert.Contains(t, emits[0].Payload, "Echo:")
}

func TestLuaHostDeliverEventNoHandler(t *testing.T) {
	dir := t.TempDir()

	// Plugin without on_event function
	writeMainLua(t, dir, `x = 1`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "no-handler",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "no-handler", event)
	require.NoError(t, err, "DeliverEvent() failed")
	assert.Empty(t, emits, "expected no emits for plugin without handler")
}

func TestLuaHostDeliverEventNoHandlerLogsDebug(t *testing.T) {
	dir := t.TempDir()

	// Plugin without on_event or on_command function
	writeMainLua(t, dir, `x = 1`)

	// Capture log output at DEBUG level
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "no-handler-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "no-handler-plugin", event)
	require.NoError(t, err, "DeliverEvent() failed")
	assert.Empty(t, emits, "expected no emits for plugin without handler")

	// Verify DEBUG log was emitted with plugin name
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "no handler", "expected debug log about missing handler")
	assert.Contains(t, logOutput, "no-handler-plugin", "expected plugin name in debug log")
}

func TestLuaHostUnload(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_event(event) return nil end`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)
	require.Len(t, host.Plugins(), 1, "expected 1 plugin after load")

	err = host.Unload(context.Background(), "test-plugin")
	require.NoError(t, err, "Unload() failed")
	assert.Empty(t, host.Plugins(), "expected 0 plugins after unload")
}

func TestLuaHostUnloadNotFound(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	err := host.Unload(context.Background(), "nonexistent")
	assert.Error(t, err, "expected error when unloading nonexistent plugin")
}

func TestLuaHostDeliverEventNotLoaded(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	_, err := host.DeliverEvent(context.Background(), "nonexistent", event)
	assert.Error(t, err, "expected error when delivering to nonexistent plugin")
}

func TestLuaHostLoadSyntaxError(t *testing.T) {
	dir := t.TempDir()

	// Invalid Lua syntax
	writeMainLua(t, dir, `function on_event(event return nil end`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "bad-syntax",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	assert.Error(t, err, "expected error when loading plugin with syntax error")
}

func TestLuaHostLoadMissingFile(t *testing.T) {
	dir := t.TempDir()

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "missing-file",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "nonexistent.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	assert.Error(t, err, "expected error when loading plugin with missing file")
}

func TestLuaHostClose(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_event(event) return nil end`)

	host := pluginlua.NewHost()

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	err = host.Close(context.Background())
	require.NoError(t, err, "Close() failed")

	// Should error when loading after close
	err = host.Load(context.Background(), manifest, dir)
	assert.Error(t, err, "expected error when loading after close")
}

func TestLuaHostDeliverEventRuntimeError(t *testing.T) {
	dir := t.TempDir()

	// Plugin that throws a runtime error
	writeMainLua(t, dir, `
function on_event(event)
    error("intentional failure")
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "error-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	_, err = host.DeliverEvent(context.Background(), "error-plugin", event)
	require.Error(t, err, "expected error when plugin throws runtime error")
	// oops.Error() returns the underlying Lua error message
	assert.Contains(t, err.Error(), "intentional failure", "error should contain Lua error message")
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

	manifest := &plugins.Manifest{
		Name:      "actor-test",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	tests := []struct {
		kind     pluginsdk.ActorKind
		expected string
	}{
		{pluginsdk.ActorCharacter, "character"},
		{pluginsdk.ActorSystem, "system"},
		{pluginsdk.ActorPlugin, "plugin"},
		{pluginsdk.ActorKind(99), "unknown"}, // Unknown kind
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			event := pluginsdk.Event{
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

func TestLuaHost_DeliverEvent_NonTableReturn_Logs_Warning(t *testing.T) {
	tests := []struct {
		name         string
		luaCode      string
		expectWarn   bool
		expectedType string
	}{
		{
			name: "string return logs warning",
			luaCode: `
function on_event(event)
    return "not a table"
end
`,
			expectWarn:   true,
			expectedType: "string",
		},
		{
			name: "number return logs warning",
			luaCode: `
function on_event(event)
    return 42
end
`,
			expectWarn:   true,
			expectedType: "number",
		},
		{
			name: "boolean return logs warning",
			luaCode: `
function on_event(event)
    return true
end
`,
			expectWarn:   true,
			expectedType: "bool",
		},
		{
			name: "nil return does NOT log warning",
			luaCode: `
function on_event(event)
    return nil
end
`,
			expectWarn: false,
		},
		{
			name: "table return does NOT log warning",
			luaCode: `
function on_event(event)
    return {}
end
`,
			expectWarn: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeMainLua(t, dir, tt.luaCode)

			// Capture log output
			var logBuf bytes.Buffer
			handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
			oldLogger := slog.Default()
			slog.SetDefault(slog.New(handler))
			defer slog.SetDefault(oldLogger)

			host := pluginlua.NewHost()
			defer closeHost(t, host)

			manifest := &plugins.Manifest{
				Name:      "bad-return",
				Version:   "1.0.0",
				Type:      plugins.TypeLua,
				LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
			}

			err := host.Load(context.Background(), manifest, dir)
			require.NoError(t, err)

			event := pluginsdk.Event{ID: "01ABC", Type: "say"}
			emits, err := host.DeliverEvent(context.Background(), "bad-return", event)
			require.NoError(t, err, "DeliverEvent() failed")

			// Non-table returns should be gracefully ignored (empty emits)
			assert.Empty(t, emits, "expected empty emits for non-table return")

			logOutput := logBuf.String()
			if tt.expectWarn {
				assert.Contains(t, logOutput, "non-table", "expected warning about non-table return")
				assert.Contains(t, logOutput, "bad-return", "expected plugin name in warning")
				assert.Contains(t, logOutput, tt.expectedType, "expected return type in warning")
			} else {
				assert.NotContains(t, logOutput, "non-table", "expected no warning for nil/table return")
			}
		})
	}
}

func TestLuaHostDeliverEventMalformedEmitEvents(t *testing.T) {
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

	manifest := &plugins.Manifest{
		Name:      "mixed-return",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "mixed-return", event)
	require.NoError(t, err, "DeliverEvent() failed")

	// Should only get the 2 valid emit events
	require.Len(t, emits, 2, "expected 2 valid events only")
	assert.Equal(t, "valid:1", emits[0].Stream)
	assert.Equal(t, "valid:2", emits[1].Stream)
}

func TestLuaHostDeliverEventAfterClose(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_event(event) return nil end`)

	host := pluginlua.NewHost()

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	err = host.Close(context.Background())
	require.NoError(t, err, "Close() failed")

	// DeliverEvent should error after close
	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	_, err = host.DeliverEvent(context.Background(), "test-plugin", event)
	assert.Error(t, err, "expected error when delivering after close")
}

func TestLuaHostDeliverEventAllFields(t *testing.T) {
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

	manifest := &plugins.Manifest{
		Name:      "field-test",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{
		ID:        "01ABC",
		Stream:    "location:123",
		Type:      "say",
		Timestamp: 1705591234000,
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   "char_1",
		Payload:   "Hello",
	}

	emits, err := host.DeliverEvent(context.Background(), "field-test", event)
	require.NoError(t, err, "DeliverEvent() failed")
	require.Len(t, emits, 1, "expected 1 emit event")

	expected := "01ABC|location:123|say|1705591234000|character|char_1"
	assert.Equal(t, expected, emits[0].Payload)
}

func TestLuaHostWithHostFunctions(t *testing.T) {
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

	hostFuncs := hostfunc.New(nil)
	host := pluginlua.NewHostWithFunctions(hostFuncs)
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "test",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{
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

func TestLuaHostNewHostWithFunctionsNilPanics(t *testing.T) {
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

func TestLuaHostDeliverEventOnCommandCommandEvent(t *testing.T) {
	dir := t.TempDir()

	// Plugin with on_command handler that echoes context fields
	mainLua := `
function on_command(ctx)
    return {
        {
            stream = "location:" .. ctx.location_id,
            type = "echo",
            payload = ctx.command .. "|" .. ctx.args .. "|" .. ctx.invoked_as .. "|" ..
                      ctx.character_name .. "|" .. ctx.character_id .. "|" ..
                      ctx.location_id .. "|" .. ctx.player_id
        }
    }
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "on-command-test",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{
		ID:        "01ABC",
		Stream:    "character:char123",
		Type:      pluginsdk.EventType("command"),
		Timestamp: 1705591234000,
		ActorKind: pluginsdk.ActorCharacter,
		ActorID:   "char123",
		Payload:   `{"name":"say","args":"Hello everyone!","invoked_as":";","character_name":"Alice","character_id":"01CHAR","location_id":"01LOC","player_id":"01PLAYER"}`,
	}

	emits, err := host.DeliverEvent(context.Background(), "on-command-test", event)
	require.NoError(t, err)
	require.Len(t, emits, 1)

	// Verify all context fields were received correctly
	expected := "say|Hello everyone!|;|Alice|01CHAR|01LOC|01PLAYER"
	assert.Equal(t, expected, emits[0].Payload)
	assert.Equal(t, "location:01LOC", emits[0].Stream)
}

func TestLuaHostDeliverEventOnCommandFallbackToOnEvent(t *testing.T) {
	dir := t.TempDir()

	// Plugin with only on_event (no on_command)
	mainLua := `
function on_event(event)
    if event.type == "command" then
        return {
            {
                stream = "fallback:1",
                type = "echo",
                payload = "fell_back_to_on_event"
            }
        }
    end
    return nil
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "fallback-test",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	// Command event should fall back to on_event
	event := pluginsdk.Event{
		ID:      "01ABC",
		Stream:  "character:char123",
		Type:    pluginsdk.EventType("command"),
		Payload: `{"name":"say","args":"test"}`,
	}

	emits, err := host.DeliverEvent(context.Background(), "fallback-test", event)
	require.NoError(t, err)
	require.Len(t, emits, 1)
	assert.Equal(t, "fell_back_to_on_event", emits[0].Payload)
}

func TestLuaHostDeliverEventOnCommandNonCommandEventUsesOnEvent(t *testing.T) {
	dir := t.TempDir()

	// Plugin with both on_command and on_event
	mainLua := `
function on_command(ctx)
    return {
        {
            stream = "on_command:1",
            type = "echo",
            payload = "on_command_called"
        }
    }
end

function on_event(event)
    return {
        {
            stream = "on_event:1",
            type = "echo",
            payload = "on_event_called"
        }
    }
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "both-handlers",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	// Non-command event should use on_event, not on_command
	event := pluginsdk.Event{
		ID:     "01ABC",
		Stream: "location:123",
		Type:   pluginsdk.EventTypeSay,
	}

	emits, err := host.DeliverEvent(context.Background(), "both-handlers", event)
	require.NoError(t, err)
	require.Len(t, emits, 1)
	assert.Equal(t, "on_event_called", emits[0].Payload)
}

func TestLuaHostDeliverEventOnCommandEmptyArgs(t *testing.T) {
	dir := t.TempDir()

	mainLua := `
function on_command(ctx)
    local args_value = ctx.args
    if args_value == "" then
        args_value = "EMPTY"
    end
    return {
        {
            stream = "test:1",
            type = "echo",
            payload = "args=" .. args_value
        }
    }
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "empty-args-test",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{
		ID:      "01ABC",
		Type:    pluginsdk.EventType("command"),
		Payload: `{"name":"look","args":"","character_name":"Bob","location_id":"loc1"}`,
	}

	emits, err := host.DeliverEvent(context.Background(), "empty-args-test", event)
	require.NoError(t, err)
	require.Len(t, emits, 1)
	assert.Equal(t, "args=EMPTY", emits[0].Payload)
}

func TestLuaHostDeliverEventOnCommandInvalidPayload(t *testing.T) {
	dir := t.TempDir()

	mainLua := `
function on_command(ctx)
    -- Even with invalid payload, ctx should have empty strings
    return {
        {
            stream = "test:1",
            type = "echo",
            payload = "name=" .. (ctx.command or "nil")
        }
    }
end
`
	writeMainLua(t, dir, mainLua)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "invalid-payload-test",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{
		ID:      "01ABC",
		Type:    pluginsdk.EventType("command"),
		Payload: "not valid json",
	}

	emits, err := host.DeliverEvent(context.Background(), "invalid-payload-test", event)
	require.NoError(t, err)
	require.Len(t, emits, 1)
	// With invalid JSON, ctx.command should be empty string
	assert.Equal(t, "name=", emits[0].Payload)
}

func TestLuaHostDeliverEventMalformedEmitEventsWarnsOnNonTableEntry(t *testing.T) {
	dir := t.TempDir()

	// Plugin returns an array with non-table entries
	mainLua := `
function on_event(event)
    return {
        "string entry",
        123,
        {
            stream = "valid:1",
            type = "test",
            payload = "valid"
        }
    }
end
`
	writeMainLua(t, dir, mainLua)

	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "warn-non-table",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "warn-non-table", event)
	require.NoError(t, err)

	// Only the valid table entry should be returned
	require.Len(t, emits, 1)
	assert.Equal(t, "valid:1", emits[0].Stream)

	// Verify warnings were logged for non-table entries
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "non-table", "expected warning about non-table entry")
	assert.Contains(t, logOutput, "warn-non-table", "expected plugin name in warning")
	assert.Contains(t, logOutput, "string", "expected type name in warning")
}

func TestLuaHostDeliverEventMalformedEmitEventsWarnsOnMissingStream(t *testing.T) {
	dir := t.TempDir()

	// Plugin returns event without stream field
	mainLua := `
function on_event(event)
    return {
        {
            type = "test",
            payload = "missing stream"
        },
        {
            stream = "valid:1",
            type = "test",
            payload = "valid"
        }
    }
end
`
	writeMainLua(t, dir, mainLua)

	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "warn-missing-stream",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "warn-missing-stream", event)
	require.NoError(t, err)

	// Only the valid entry should be returned
	require.Len(t, emits, 1)
	assert.Equal(t, "valid:1", emits[0].Stream)

	// Verify warning was logged for missing stream
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "stream", "expected warning about missing stream field")
	assert.Contains(t, logOutput, "warn-missing-stream", "expected plugin name in warning")
}

func TestLuaHostDeliverEventMalformedEmitEventsWarnsOnMissingType(t *testing.T) {
	dir := t.TempDir()

	// Plugin returns event without type field
	mainLua := `
function on_event(event)
    return {
        {
            stream = "test:1",
            payload = "missing type"
        },
        {
            stream = "valid:1",
            type = "test",
            payload = "valid"
        }
    }
end
`
	writeMainLua(t, dir, mainLua)

	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "warn-missing-type",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "warn-missing-type", event)
	require.NoError(t, err)

	// Only the valid entry should be returned
	require.Len(t, emits, 1)
	assert.Equal(t, "valid:1", emits[0].Stream)

	// Verify warning was logged for missing type
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "type", "expected warning about missing type field")
	assert.Contains(t, logOutput, "warn-missing-type", "expected plugin name in warning")
}

func TestLuaHostDeliverEventValidationErrorsLogged(t *testing.T) {
	dir := t.TempDir()

	// Plugin with multiple validation failures
	mainLua := `
function on_event(event)
    return {
        "not a table",
        {
            -- missing stream
            type = "test",
            payload = "missing stream"
        },
        {
            stream = "valid:1",
            type = "test",
            payload = "valid"
        },
        {
            stream = "test:2",
            -- missing type
            payload = "missing type"
        }
    }
end
`
	writeMainLua(t, dir, mainLua)

	// Capture log output
	var logBuf bytes.Buffer
	handler := slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(oldLogger)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "multi-validation",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	event := pluginsdk.Event{ID: "01ABC", Type: "say"}
	emits, err := host.DeliverEvent(context.Background(), "multi-validation", event)
	require.NoError(t, err, "DeliverEvent should not fail on validation errors")

	// Only the valid entry should be returned (partial success)
	require.Len(t, emits, 1)
	assert.Equal(t, "valid:1", emits[0].Stream)

	// Verify consolidated validation errors were logged in single warning
	logOutput := logBuf.String()
	assert.Contains(t, logOutput, "emit validation errors", "expected consolidated warning")
	assert.Contains(t, logOutput, "multi-validation", "expected plugin name in warning")
	assert.Contains(t, logOutput, "error_count", "expected error count")
	// Individual error messages should be in the errors array
	assert.Contains(t, logOutput, "entry[1]", "expected entry 1 error")
	assert.Contains(t, logOutput, "entry[2]", "expected entry 2 error")
	assert.Contains(t, logOutput, "entry[4]", "expected entry 4 error")
}

func TestLuaHostDeliverCommandStringReturn(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `
function on_command(ctx)
    return "Hello from " .. ctx.command .. " " .. ctx.args
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "cmd-string",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	resp, err := host.DeliverCommand(context.Background(), "cmd-string", pluginsdk.CommandRequest{
		Command:       "say",
		Args:          "Hello everyone!",
		CharacterID:   "01CHAR",
		CharacterName: "Alice",
		LocationID:    "01LOC",
		SessionID:     "01SESS",
		InvokedAs:     "say",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "Hello from say Hello everyone!", resp.Output)
}

func TestLuaHostDeliverCommandTableReturn(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `
function on_command(ctx)
    return {
        status = 0,
        output = "You say: " .. ctx.args,
        events = {
            {
                stream = "location:" .. ctx.location_id,
                type = "say",
                payload = ctx.args,
            },
        },
    }
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "cmd-table",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	resp, err := host.DeliverCommand(context.Background(), "cmd-table", pluginsdk.CommandRequest{
		Command:       "say",
		Args:          "Hello!",
		CharacterID:   "01CHAR",
		CharacterName: "Alice",
		LocationID:    "01LOC",
		SessionID:     "01SESS",
		InvokedAs:     "say",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Equal(t, "You say: Hello!", resp.Output)
	require.Len(t, resp.Events, 1)
	assert.Equal(t, "location:01LOC", resp.Events[0].Stream)
	assert.Equal(t, pluginsdk.EventType("say"), resp.Events[0].Type)
	assert.Equal(t, "Hello!", resp.Events[0].Payload)
}

func TestLuaHostDeliverCommandPluginNotFound(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	_, err := host.DeliverCommand(context.Background(), "nonexistent", pluginsdk.CommandRequest{
		Command: "say",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestLuaHostDeliverCommandNoHandler(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `x = 1`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "cmd-no-handler",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	resp, err := host.DeliverCommand(context.Background(), "cmd-no-handler", pluginsdk.CommandRequest{
		Command: "say",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Empty(t, resp.Output)
}

func TestLuaHostDeliverCommandAfterClose(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `function on_command(ctx) return "ok" end`)

	host := pluginlua.NewHost()

	manifest := &plugins.Manifest{
		Name:      "cmd-closed",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	err = host.Close(context.Background())
	require.NoError(t, err)

	_, err = host.DeliverCommand(context.Background(), "cmd-closed", pluginsdk.CommandRequest{
		Command: "say",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "host is closed")
}

func TestLuaHostDeliverCommandNilReturn(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `
function on_command(ctx)
    return nil
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "cmd-nil",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	resp, err := host.DeliverCommand(context.Background(), "cmd-nil", pluginsdk.CommandRequest{
		Command: "say",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandOK, resp.Status)
	assert.Empty(t, resp.Output)
}

func TestLuaHostDeliverCommandErrorStatus(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `
function on_command(ctx)
    return {
        status = 1,
        output = "Unknown command: " .. ctx.command,
    }
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "cmd-error",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	resp, err := host.DeliverCommand(context.Background(), "cmd-error", pluginsdk.CommandRequest{
		Command: "bogus",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, pluginsdk.CommandError, resp.Status)
	assert.Equal(t, "Unknown command: bogus", resp.Output)
}

func TestLuaHost_DeliverCommand_FailureModes(t *testing.T) {
	tests := []struct {
		name       string
		luaSource  string
		wantErr    bool
		wantStatus pluginsdk.CommandStatus
	}{
		{
			name:      "runtime error in handler",
			luaSource: `function on_command(ctx) error("kaboom") end`,
			wantErr:   true,
		},
		{
			name:      "undefined variable reference",
			luaSource: `function on_command(ctx) return undefined_var .. "x" end`,
			wantErr:   true,
		},
		{
			name:       "out-of-range status clamps to OK",
			luaSource:  `function on_command(ctx) return { status = 999, output = "bad" } end`,
			wantStatus: pluginsdk.CommandOK,
		},
		{
			name:       "negative status clamps to OK",
			luaSource:  `function on_command(ctx) return { status = -1, output = "neg" } end`,
			wantStatus: pluginsdk.CommandOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeMainLua(t, dir, tt.luaSource)

			host := pluginlua.NewHost()
			defer closeHost(t, host)

			manifest := &plugins.Manifest{
				Name:      "fail-" + tt.name,
				Version:   "1.0.0",
				Type:      plugins.TypeLua,
				LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
			}
			require.NoError(t, host.Load(context.Background(), manifest, dir))

			resp, err := host.DeliverCommand(context.Background(), manifest.Name, pluginsdk.CommandRequest{
				Command: "test",
			})

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				assert.Equal(t, tt.wantStatus, resp.Status)
			}
		})
	}
}

func TestLuaHostDeliverCommandAllContextFields(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `
function on_command(ctx)
    return ctx.command .. "|" .. ctx.args .. "|" ..
           ctx.character_id .. "|" .. ctx.character_name .. "|" ..
           ctx.location_id .. "|" .. ctx.session_id .. "|" ..
           ctx.player_id .. "|" .. ctx.invoked_as
end
`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "cmd-fields",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	resp, err := host.DeliverCommand(context.Background(), "cmd-fields", pluginsdk.CommandRequest{
		Command:       "say",
		Args:          "Hello!",
		CharacterID:   "01CHAR",
		CharacterName: "Alice",
		LocationID:    "01LOC",
		SessionID:     "01SESS",
		PlayerID:      "01PLAYER",
		InvokedAs:     ";",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "say|Hello!|01CHAR|Alice|01LOC|01SESS|01PLAYER|;", resp.Output)
}

func TestLuaHostDeliverCommandWithHostFunctions(t *testing.T) {
	dir := t.TempDir()

	writeMainLua(t, dir, `
function on_command(ctx)
    local id = holomush.new_request_id()
    holomush.log("info", "Command: " .. ctx.command)
    return "request_id=" .. id
end
`)

	hostFuncs := hostfunc.New(nil)
	host := pluginlua.NewHostWithFunctions(hostFuncs)
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "cmd-hostfuncs",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}

	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	resp, err := host.DeliverCommand(context.Background(), "cmd-hostfuncs", pluginsdk.CommandRequest{
		Command: "test",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Contains(t, resp.Output, "request_id=")
}

func TestLuaHostQuerySessionStreamsReturnsErrorWhenHostClosed(t *testing.T) {
	dir := t.TempDir()
	writeMainLua(t, dir, `function on_session_subscribe() return {} end`)
	host := pluginlua.NewHost()

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
	require.NoError(t, host.Load(context.Background(), manifest, dir))
	closeHost(t, host)

	_, err := host.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "host is closed")
}

func TestLuaHostQuerySessionStreamsReturnsErrorForUnknownPlugin(t *testing.T) {
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	_, err := host.QuerySessionStreams(context.Background(), "nonexistent", plugins.SessionStreamsRequest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin not loaded")
}

func TestLuaHostQuerySessionStreamsRegistersHostFuncsWhenAvailable(t *testing.T) {
	dir := t.TempDir()
	writeMainLua(t, dir, `
function on_session_subscribe(character_id, player_id, session_id)
    return {"channel:test"}
end
`)
	hf := hostfunc.New(nil)
	host := pluginlua.NewHostWithFunctions(hf)
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
		Requires:  []string{"test-svc"},
	}
	require.NoError(t, host.Load(context.Background(), manifest, dir))

	streams, err := host.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{
		CharacterID: "char-1", PlayerID: "player-1", SessionID: "sess-1",
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"channel:test"}, streams)
}

func TestLuaHostQuerySessionStreamsCallsOnSessionSubscribeWhenDefined(t *testing.T) {
	dir := t.TempDir()
	writeMainLua(t, dir, `
function on_session_subscribe(character_id, player_id, session_id)
    return {"channel:" .. character_id, "channel:general"}
end
`)
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	req := plugins.SessionStreamsRequest{
		CharacterID: "char-abc",
		PlayerID:    "player-xyz",
		SessionID:   "sess-123",
	}
	streams, err := host.QuerySessionStreams(context.Background(), "test-plugin", req)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"channel:char-abc", "channel:general"}, streams)
}

func TestLuaHostQuerySessionStreamsReturnsNilWhenHandlerNotDefined(t *testing.T) {
	dir := t.TempDir()
	writeMainLua(t, dir, `-- no on_session_subscribe defined`)

	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	streams, err := host.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{
		CharacterID: "char-abc", PlayerID: "player-xyz", SessionID: "sess-123",
	})
	require.NoError(t, err)
	assert.Nil(t, streams)
}

func TestLuaHostQuerySessionStreamsReturnsErrorWhenHandlerFails(t *testing.T) {
	dir := t.TempDir()
	writeMainLua(t, dir, `
function on_session_subscribe(character_id, player_id, session_id)
    error("db connection failed")
end
`)
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	_, err = host.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{
		CharacterID: "char-abc", PlayerID: "player-xyz", SessionID: "sess-123",
	})
	require.Error(t, err)
}

func TestLuaHostQuerySessionStreamsReturnsErrorWhenHandlerReturnsNonTable(t *testing.T) {
	dir := t.TempDir()
	writeMainLua(t, dir, `
function on_session_subscribe(character_id, player_id, session_id)
    return "not a table"
end
`)
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	_, err = host.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{
		CharacterID: "char-abc", PlayerID: "player-xyz", SessionID: "sess-123",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected table return")
}

func TestLuaHostQuerySessionStreamsReturnsNilWhenHandlerReturnsNil(t *testing.T) {
	dir := t.TempDir()
	writeMainLua(t, dir, `
function on_session_subscribe(character_id, player_id, session_id)
    return nil
end
`)
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	manifest := &plugins.Manifest{
		Name:      "test-plugin",
		Version:   "1.0.0",
		Type:      plugins.TypeLua,
		LuaPlugin: &plugins.LuaConfig{Entry: "main.lua"},
	}
	err := host.Load(context.Background(), manifest, dir)
	require.NoError(t, err)

	streams, err := host.QuerySessionStreams(context.Background(), "test-plugin", plugins.SessionStreamsRequest{
		CharacterID: "char-abc", PlayerID: "player-xyz", SessionID: "sess-123",
	})
	require.NoError(t, err)
	assert.Nil(t, streams)
}

// stubLuaTestCoordinator implements focus.Coordinator for lua host tests.
type stubLuaTestCoordinator struct{}

func (s *stubLuaTestCoordinator) JoinFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
}

func (s *stubLuaTestCoordinator) LeaveFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
}

func (s *stubLuaTestCoordinator) PresentFocus(_ context.Context, _ string, _ session.FocusKey) error {
	return nil
}

func (s *stubLuaTestCoordinator) RestoreFocus(_ context.Context, _ string) (focus.RestorePlan, error) {
	return focus.RestorePlan{}, nil
}

var _ focus.Coordinator = (*stubLuaTestCoordinator)(nil)

func TestLuaHostSetFocusCoordinatorWithNilHostFuncsIsNoOp(t *testing.T) {
	// NewHost() creates a host without hostFuncs — SetFocusCoordinator must not panic.
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	require.NotPanics(t, func() {
		host.SetFocusCoordinator(&stubLuaTestCoordinator{})
	})
}

func TestLuaHostSetEventStoreWithNilHostFuncsIsNoOp(t *testing.T) {
	// NewHost() creates a host without hostFuncs — SetEventStore must not panic.
	host := pluginlua.NewHost()
	defer closeHost(t, host)

	require.NotPanics(t, func() {
		host.SetEventStore(nil)
	})
}

func TestLuaHostSetFocusCoordinatorWithHostFuncsInjectsOps(t *testing.T) {
	hf := hostfunc.New(nil)
	host := pluginlua.NewHostWithFunctions(hf)
	defer closeHost(t, host)

	fc := &stubLuaTestCoordinator{}
	require.NotPanics(t, func() {
		host.SetFocusCoordinator(fc)
	})
}

func TestLuaHostSetEventStoreWithHostFuncsInjectsReader(t *testing.T) {
	hf := hostfunc.New(nil)
	host := pluginlua.NewHostWithFunctions(hf)
	defer closeHost(t, host)

	// nil event store is valid (late-binding to clear or defer injection).
	require.NotPanics(t, func() {
		host.SetEventStore(nil)
	})
}
