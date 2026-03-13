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

	"github.com/holomush/holomush/internal/world"
)

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

			result := sanitizeKVErrorForPlugin("test-plugin", "get", "test-key", tt.inputErr)

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

func TestSanitizeKVErrorForPlugin_CorrelationIDUnique(t *testing.T) {
	// Each call should produce a unique correlation ID
	err := errors.New("internal failure")
	msg1 := sanitizeKVErrorForPlugin("plugin", "get", "key", err)
	msg2 := sanitizeKVErrorForPlugin("plugin", "get", "key", err)

	assert.NotEqual(t, msg1, msg2, "each error should have a unique correlation ID")
	assert.Contains(t, msg1, "internal error (ref: ")
	assert.Contains(t, msg2, "internal error (ref: ")
}

func TestSanitizeKVErrorForPlugin_LogsFullContext(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	origLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(origLogger)

	dbErr := errors.New("pq: connection refused to 10.0.0.5:5432")
	sanitizeKVErrorForPlugin("my-plugin", "set", "user-pref", dbErr)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "my-plugin", "log should include plugin name")
	assert.Contains(t, logOutput, "set", "log should include operation")
	assert.Contains(t, logOutput, "user-pref", "log should include key")
	assert.Contains(t, logOutput, "pq: connection refused", "log should include full error")
	assert.Contains(t, logOutput, "error_id", "log should include correlation ID")
}
