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

func TestGetEmitter_ReturnsNilWhenEmitterMissing(t *testing.T) {
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

func TestGetEmitter_ReturnsEmitterWhenRegistered(t *testing.T) {
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

func TestEmitLocation_RaisesLuaErrorWithoutRegisterStdlib(t *testing.T) {
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

func TestEmitCharacter_RaisesLuaErrorWithoutRegisterStdlib(t *testing.T) {
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

func TestEmitGlobal_RaisesLuaErrorWithoutRegisterStdlib(t *testing.T) {
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

func TestEmitFlush_RaisesLuaErrorWithoutRegisterStdlib(t *testing.T) {
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
