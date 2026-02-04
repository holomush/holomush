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
}

func TestGetEmitter_LogsErrorWhenEmitterMissing(t *testing.T) {
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

	// Call getEmitter - it should log an error since no emitter was registered
	emitter := getEmitter(L)

	// Should still return a valid emitter (fallback behavior)
	require.NotNil(t, emitter, "getEmitter should return a fallback emitter")

	// Parse and verify log output
	var entry logEntry
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "should have logged JSON entry, got: %s", buf.String())

	assert.Equal(t, "ERROR", entry.Level, "should log at ERROR level")
	assert.Contains(t, entry.Msg, "emitter not found", "should mention emitter not found")
	assert.Equal(t, emitterRegistryKey, entry.RegistryKey, "should include registry key")
}

func TestGetEmitter_NoLogWhenEmitterExists(t *testing.T) {
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
