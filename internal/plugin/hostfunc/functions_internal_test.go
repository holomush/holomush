// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	"github.com/holomush/holomush/internal/world"
)

// kvErrCtx is a helper that builds a PluginErrorContext for KV operation tests.
func kvErrCtx(pluginName, operation, key string) PluginErrorContext {
	return PluginErrorContext{Plugin: pluginName, Operation: operation, Subject: "key", SubjectID: key}
}

func TestSanitizeKVErrorForPlugin(t *testing.T) {
	tests := []struct {
		name           string
		inputErr       error
		wantContains   string
		wantNotContain string
		wantLogged     bool
	}{
		{
			name:         "deadline exceeded returns sanitized timeout",
			inputErr:     context.DeadlineExceeded,
			wantContains: "operation timed out",
			wantLogged:   true,
		},
		{
			name:         "wrapped deadline exceeded returns sanitized timeout",
			inputErr:     errors.Join(errors.New("slow kv get"), context.DeadlineExceeded),
			wantContains: "operation timed out",
			wantLogged:   true,
		},
		{
			name:         "not found returns key not found",
			inputErr:     world.ErrNotFound,
			wantContains: "key not found",
		},
		{
			name:         "permission denied returns access denied",
			inputErr:     world.ErrPermissionDenied,
			wantContains: "access denied",
		},
		{
			name:           "internal error returns correlation ID",
			inputErr:       errors.New("pq: connection refused"),
			wantContains:   "internal error (ref: ",
			wantNotContain: "pq: connection refused",
			wantLogged:     true,
		},
		{
			name:           "database error does not leak details",
			inputErr:       errors.New("FATAL: password authentication failed for user \"holomush\""),
			wantContains:   "internal error (ref: ",
			wantNotContain: "password",
			wantLogged:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Capture log output to verify server-side logging
			var buf bytes.Buffer
			handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
			origLogger := slog.Default()
			slog.SetDefault(slog.New(handler))
			defer slog.SetDefault(origLogger)

			result := SanitizeErrorForPlugin(kvErrCtx("test-plugin", "get", "test-key"), tt.inputErr)

			assert.Contains(t, result, tt.wantContains)
			if tt.wantNotContain != "" {
				assert.NotContains(t, result, tt.wantNotContain,
					"sanitized message must not contain raw error details")
			}

			if tt.wantLogged {
				logOutput := buf.String()
				require.NotEmpty(t, logOutput, "expected server-side log entry")
			}
		})
	}
}

func TestSanitizeKVErrorForPluginCorrelationIDUnique(t *testing.T) {
	// Each call should produce a unique correlation ID
	err := errors.New("internal failure")
	msg1 := SanitizeErrorForPlugin(kvErrCtx("plugin", "get", "key"), err)
	msg2 := SanitizeErrorForPlugin(kvErrCtx("plugin", "get", "key"), err)

	assert.NotEqual(t, msg1, msg2, "each error should have a unique correlation ID")
	assert.Contains(t, msg1, "internal error (ref: ")
	assert.Contains(t, msg2, "internal error (ref: ")
}

func TestSanitizeKVErrorForPluginLogsFullContext(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	origLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(origLogger)

	dbErr := errors.New("pq: connection refused to 10.0.0.5:5432")
	SanitizeErrorForPlugin(kvErrCtx("my-plugin", "set", "user-pref"), dbErr)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "my-plugin", "log should include plugin name")
	assert.Contains(t, logOutput, "set", "log should include operation")
	assert.Contains(t, logOutput, "user-pref", "log should include key")
	assert.Contains(t, logOutput, "pq: connection refused", "log should include full error")
	assert.Contains(t, logOutput, "error_id", "log should include correlation ID")
}

// TestRegisterWithEmitCapture_LuaCallsAccumulate verifies that after
// RegisterWithEmitCapture, a Lua script calling holomush.register_emit_type
// adds the type to the passed registry.
func TestRegisterWithEmitCapture_LuaCallsAccumulate(t *testing.T) {
	t.Parallel()

	f := New(nil)
	L := lua.NewState()
	t.Cleanup(L.Close)

	reg := NewLuaEmitRegistry()
	f.RegisterWithEmitCapture(L, "test-plugin", reg)

	err := L.DoString(`holomush.register_emit_type("alpha")`)
	require.NoError(t, err)

	require.Equal(t, []string{"alpha"}, reg.Types())
}

// TestRegisterWithEmitCapture_DuplicateCallsIdempotent verifies that
// repeated register_emit_type calls remain idempotent through the
// RegisterWithEmitCapture entry point.
func TestRegisterWithEmitCapture_DuplicateCallsIdempotent(t *testing.T) {
	t.Parallel()

	f := New(nil)
	L := lua.NewState()
	t.Cleanup(L.Close)

	reg := NewLuaEmitRegistry()
	f.RegisterWithEmitCapture(L, "test-plugin", reg)

	err := L.DoString(`
holomush.register_emit_type("x")
holomush.register_emit_type("x")
`)
	require.NoError(t, err)

	require.Equal(t, []string{"x"}, reg.Types())
}

// TestRegisterWithEmitCapture_PreservesStandardNamespace verifies that the
// standard holomush.* namespace is also installed (log, new_request_id,
// etc.) — confirming RegisterWithEmitCapture wraps Register, not replaces.
func TestRegisterWithEmitCapture_PreservesStandardNamespace(t *testing.T) {
	t.Parallel()

	f := New(nil)
	L := lua.NewState()
	t.Cleanup(L.Close)

	reg := NewLuaEmitRegistry()
	f.RegisterWithEmitCapture(L, "test-plugin", reg)

	// holomush.log and holomush.new_request_id are part of the standard
	// stdlib; both should be present alongside register_emit_type.
	err := L.DoString(`
assert(type(holomush.log) == "function", "holomush.log should be a function")
assert(type(holomush.new_request_id) == "function", "holomush.new_request_id should be a function")
assert(type(holomush.register_emit_type) == "function", "holomush.register_emit_type should be a function")
`)
	require.NoError(t, err)
}
