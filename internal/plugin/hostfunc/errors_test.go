// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/holomush/holomush/internal/world"
)

func TestSanitizeErrorForPluginReturnsNilMessageForNilError(t *testing.T) {
	ctx := PluginErrorContext{
		Plugin:    "test-plugin",
		Operation: "get",
		Subject:   "location",
		SubjectID: "01J8X2Y3Z4A5B6C7D8E9F0G1H2",
	}
	result := SanitizeErrorForPlugin(ctx, nil)
	assert.Empty(t, result, "nil error should return empty string")
}

func TestSanitizeErrorForPluginReturnsNotFoundForErrNotFound(t *testing.T) {
	ctx := PluginErrorContext{
		Plugin:    "test-plugin",
		Operation: "get",
		Subject:   "location",
		SubjectID: "01J8X2Y3Z4A5B6C7D8E9F0G1H2",
	}
	result := SanitizeErrorForPlugin(ctx, world.ErrNotFound)
	assert.Equal(t, "location not found", result)
}

func TestSanitizeErrorForPluginReturnsNotFoundForWrappedErrNotFound(t *testing.T) {
	ctx := PluginErrorContext{
		Plugin:    "test-plugin",
		Operation: "get",
		Subject:   "character",
		SubjectID: "01J8X2Y3Z4A5B6C7D8E9F0G1H3",
	}
	wrapped := fmt.Errorf("query failed: %w", world.ErrNotFound)
	result := SanitizeErrorForPlugin(ctx, wrapped)
	assert.Equal(t, "character not found", result)
}

func TestSanitizeErrorForPluginReturnsPermissionDeniedForErrPermissionDenied(t *testing.T) {
	ctx := PluginErrorContext{
		Plugin:    "test-plugin",
		Operation: "write",
		Subject:   "location",
		SubjectID: "01J8X2Y3Z4A5B6C7D8E9F0G1H2",
	}
	result := SanitizeErrorForPlugin(ctx, world.ErrPermissionDenied)
	assert.Equal(t, "access denied", result)
}

func TestSanitizeErrorForPluginReturnsTimeoutForDeadlineExceeded(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	origLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(origLogger)

	ctx := PluginErrorContext{
		Plugin:    "test-plugin",
		Operation: "query",
		Subject:   "location",
		SubjectID: "01J8X2Y3Z4A5B6C7D8E9F0G1H2",
	}
	result := SanitizeErrorForPlugin(ctx, context.DeadlineExceeded)
	assert.Equal(t, "operation timed out", result)
	assert.Contains(t, buf.String(), "test-plugin", "timeout should be logged with plugin name")
}

func TestSanitizeErrorForPluginReturnsCorrelationIDForUnknownError(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	origLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(origLogger)

	ctx := PluginErrorContext{
		Plugin:    "test-plugin",
		Operation: "get",
		Subject:   "location",
		SubjectID: "01J8X2Y3Z4A5B6C7D8E9F0G1H2",
	}
	result := SanitizeErrorForPlugin(ctx, errors.New("pq: connection refused"))
	assert.Contains(t, result, "internal error (ref: ")
	assert.NotContains(t, result, "pq", "raw error must not leak to plugin")
}

func TestSanitizeErrorForPluginDoesNotLeakInternalDetailsForUnknownError(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	origLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(origLogger)

	ctx := PluginErrorContext{
		Plugin:    "my-plugin",
		Operation: "get",
		Subject:   "location",
		SubjectID: "01J8X2Y3Z4A5B6C7D8E9F0G1H2",
	}
	dbErr := errors.New("FATAL: password authentication failed for user \"holomush\"")
	result := SanitizeErrorForPlugin(ctx, dbErr)
	assert.NotContains(t, result, "password", "credential details must not leak to plugin")
	assert.Contains(t, result, "internal error (ref: ")
}

func TestSanitizeErrorForPluginLogsFullErrorContextForUnknownError(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	origLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(origLogger)

	ctx := PluginErrorContext{
		Plugin:    "my-plugin",
		Operation: "set",
		Subject:   "key",
		SubjectID: "user-pref",
	}
	dbErr := errors.New("pq: connection refused to 10.0.0.5:5432")
	SanitizeErrorForPlugin(ctx, dbErr)

	logOutput := buf.String()
	assert.Contains(t, logOutput, "my-plugin", "log must include plugin name")
	assert.Contains(t, logOutput, "set", "log must include operation")
	assert.Contains(t, logOutput, "pq: connection refused", "log must include full error")
	assert.Contains(t, logOutput, "error_id", "log must include correlation ID")
}

func TestSanitizeErrorForPluginProducesUniqueCorrelationIDsForRepeatedErrors(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	origLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	defer slog.SetDefault(origLogger)

	ctx := PluginErrorContext{
		Plugin:    "plugin",
		Operation: "get",
		Subject:   "location",
		SubjectID: "id",
	}
	err := errors.New("internal failure")
	msg1 := SanitizeErrorForPlugin(ctx, err)
	msg2 := SanitizeErrorForPlugin(ctx, err)

	assert.NotEqual(t, msg1, msg2, "each error instance should have a unique correlation ID")
	assert.True(t, strings.HasPrefix(msg1, "internal error (ref: "))
	assert.True(t, strings.HasPrefix(msg2, "internal error (ref: "))
}

func TestSanitizeErrorForPluginUsesSubjectInNotFoundMessage(t *testing.T) {
	tests := []struct {
		subject string
		wantMsg string
	}{
		{"location", "location not found"},
		{"character", "character not found"},
		{"exit", "exit not found"},
		{"object", "object not found"},
	}
	for _, tt := range tests {
		t.Run("returns '"+tt.wantMsg+"' for ErrNotFound", func(t *testing.T) {
			ctx := PluginErrorContext{
				Plugin:    "test-plugin",
				Operation: "get",
				Subject:   tt.subject,
				SubjectID: "some-id",
			}
			result := SanitizeErrorForPlugin(ctx, world.ErrNotFound)
			assert.Equal(t, tt.wantMsg, result)
		})
	}
}
