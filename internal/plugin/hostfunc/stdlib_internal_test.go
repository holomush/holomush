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
	events, errs := emitter.Flush()
	require.Empty(t, errs, "Flush MUST NOT report JSON marshal errors for valid payloads")
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

	events, errs := getEmitter(ls).Flush()
	require.Empty(t, errs, "Flush MUST NOT report JSON marshal errors for valid payloads")
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

// TestEmitFlushWritesSensitiveToLuaTable locks the write-side of the
// Phase 3d Lua sensitive plumbing chain on the canonical
// `return holo.emit.flush()` round-trip path. emitFlush MUST serialize
// the Sensitive flag from the Go-side pluginsdk.EmitEvent buffer into
// the `sensitive` key on each Lua eventTable so the downstream
// internal/plugin/lua/host.go::parseEmitEvents can read it back.
//
// Refs: docs/superpowers/specs/2026-05-03-event-payload-crypto-phase3d-grounding.md (Decision 5)
// Refs: code-reviewer Pass 1 finding 2026-05-04 (full plumbing chain regression).
func TestEmitFlushWritesSensitiveToLuaTable(t *testing.T) {
	ls := lua.NewState()
	defer ls.Close()
	RegisterStdlib(ls)

	// Emit two events: one sensitive, one not.
	err := ls.DoString(`
        holo.emit.character("char_1", "core-test:secret",
            { msg = "private" }, { sensitive = true })
        holo.emit.character("char_2", "core-test:public",
            { msg = "public" })
        result = holo.emit.flush()
    `)
	require.NoError(t, err)

	// Pull the result table back from the Lua state.
	resultLV := ls.GetGlobal("result")
	resultTable, ok := resultLV.(*lua.LTable)
	require.True(t, ok, "expected result to be a table")
	require.Equal(t, 2, resultTable.Len())

	// Event 1: sensitive=true.
	ev1, ok := resultTable.RawGetInt(1).(*lua.LTable)
	require.True(t, ok)
	sensitive1, ok := ev1.RawGetString("sensitive").(lua.LBool)
	require.True(t, ok, "sensitive key MUST be a boolean")
	assert.True(t, bool(sensitive1),
		"first event was emitted with sensitive=true; emitFlush MUST serialize that on the Lua table")

	// Event 2: sensitive=false (default).
	ev2, ok := resultTable.RawGetInt(2).(*lua.LTable)
	require.True(t, ok)
	sensitive2, ok := ev2.RawGetString("sensitive").(lua.LBool)
	require.True(t, ok, "sensitive key MUST be a boolean even when false")
	assert.False(t, bool(sensitive2),
		"second event was emitted without sensitive opts; emitFlush MUST serialize sensitive=false")
}
