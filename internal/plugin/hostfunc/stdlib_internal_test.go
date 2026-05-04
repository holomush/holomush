// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
)

// logEntry represents a parsed JSON log entry for test verification.
type logEntry struct {
	Level       string `json:"level"`
	Msg         string `json:"msg"`
	RegistryKey string `json:"registry_key"`
	Hint        string `json:"hint"`
}

func TestGetEmitterReturnsNilWhenEmitterMissing(t *testing.T) {
	// Create a Lua state WITHOUT calling RegisterStdlib
	// This simulates the bug where RegisterStdlib wasn't called
	L := lua.NewState()
	defer L.Close()

	// Capture logs
	var buf bytes.Buffer
	oldDefault := slog.Default()
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	// Call getEmitter - it should return nil and log an error
	emitter := getEmitter(L)

	// Should return nil (fail fast instead of fallback)
	assert.Nil(t, emitter, "getEmitter should return nil when RegisterStdlib not called")

	// Parse and verify log output
	var entry logEntry
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "should have logged JSON entry, got: %s", buf.String())

	assert.Equal(t, "ERROR", entry.Level, "should log at ERROR level")
	assert.Contains(t, entry.Msg, "emitter not found", "should mention emitter not found")
	assert.Equal(t, emitterRegistryKey, entry.RegistryKey, "should include registry key")
	assert.Contains(t, entry.Hint, "RegisterStdlib must be called", "should include hint about RegisterStdlib")
}

func TestGetEmitterReturnsEmitterWhenRegistered(t *testing.T) {
	// Create a Lua state WITH RegisterStdlib called
	L := lua.NewState()
	defer L.Close()

	RegisterStdlib(L)

	// Capture logs
	var buf bytes.Buffer
	oldDefault := slog.Default()
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	slog.SetDefault(logger)
	defer slog.SetDefault(oldDefault)

	// Call getEmitter - should succeed without logging
	emitter := getEmitter(L)

	require.NotNil(t, emitter, "getEmitter should return the registered emitter")
	assert.Empty(t, buf.String(), "should not log anything when emitter is properly registered")
}

// =============================================================================
// Emit functions fail when RegisterStdlib not called
// =============================================================================

func TestEmitLocationRaisesLuaErrorWithoutRegisterStdlib(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Manually set up just the emit.location function without the emitter
	holoTable := L.NewTable()
	emitMod := L.NewTable()
	L.SetField(emitMod, "location", L.NewFunction(emitLocation))
	L.SetField(holoTable, "emit", emitMod)
	L.SetGlobal("holo", holoTable)

	err := L.DoString(`holo.emit.location("01ABC", "say", {message = "test"})`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "emitter not initialized")
	assert.Contains(t, err.Error(), "RegisterStdlib not called")
}

func TestEmitCharacterRaisesLuaErrorWithoutRegisterStdlib(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	holoTable := L.NewTable()
	emitMod := L.NewTable()
	L.SetField(emitMod, "character", L.NewFunction(emitCharacter))
	L.SetField(holoTable, "emit", emitMod)
	L.SetGlobal("holo", holoTable)

	err := L.DoString(`holo.emit.character("01DEF", "tell", {message = "test"})`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "emitter not initialized")
	assert.Contains(t, err.Error(), "RegisterStdlib not called")
}

func TestEmitGlobalRaisesLuaErrorWithoutRegisterStdlib(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	holoTable := L.NewTable()
	emitMod := L.NewTable()
	L.SetField(emitMod, "global", L.NewFunction(emitGlobal))
	L.SetField(holoTable, "emit", emitMod)
	L.SetGlobal("holo", holoTable)

	err := L.DoString(`holo.emit.global("system", {message = "test"})`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "emitter not initialized")
	assert.Contains(t, err.Error(), "RegisterStdlib not called")
}

func TestEmitFlushRaisesLuaErrorWithoutRegisterStdlib(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	holoTable := L.NewTable()
	emitMod := L.NewTable()
	L.SetField(emitMod, "flush", L.NewFunction(emitFlush))
	L.SetField(holoTable, "emit", emitMod)
	L.SetGlobal("holo", holoTable)

	err := L.DoString(`holo.emit.flush()`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "emitter not initialized")
	assert.Contains(t, err.Error(), "RegisterStdlib not called")
}

// =============================================================================
// Phase 3d Task 8: Lua-side `sensitive` opts-table key plumbing
// =============================================================================

// TestEmitLocationReadsSensitiveTrue asserts holo.emit.location with
// {sensitive=true} opts table sets EmitEvent.Sensitive=true on the
// accumulated buffer.
func TestEmitLocationReadsSensitiveTrue(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	RegisterStdlib(ls)

	err := ls.DoString(`
        holo.emit.location("loc-01ABC", "core-test:hello",
            { msg = "private" }, { sensitive = true })
    `)
	require.NoError(t, err)

	emitter := getEmitter(ls)
	require.NotNil(t, emitter)
	events, _ := emitter.Flush()
	require.Len(t, events, 1)
	assert.True(t, events[0].Sensitive,
		"opts.sensitive=true MUST set EmitEvent.Sensitive=true on the buffer")
}

// TestEmitLocationDefaultsSensitiveFalse asserts that omitting opts
// keeps EmitEvent.Sensitive=false (backwards compat).
func TestEmitLocationDefaultsSensitiveFalse(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	RegisterStdlib(ls)

	err := ls.DoString(`holo.emit.location("loc-01ABC", "core-test:hello", { msg = "public" })`)
	require.NoError(t, err)

	events, _ := getEmitter(ls).Flush()
	require.Len(t, events, 1)
	assert.False(t, events[0].Sensitive,
		"opts absent MUST keep EmitEvent.Sensitive=false")
}

// TestEmitLocationSensitiveWrongTypeRejected asserts non-bool sensitive
// raises LUA_EMIT_SENSITIVE_TYPE.
func TestEmitLocationSensitiveWrongTypeRejected(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	RegisterStdlib(ls)

	err := ls.DoString(`
        holo.emit.location("loc-01ABC", "core-test:hello",
            { msg = "x" }, { sensitive = "true" })
    `)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LUA_EMIT_SENSITIVE_TYPE",
		"wrong type MUST raise LUA_EMIT_SENSITIVE_TYPE error")
}
