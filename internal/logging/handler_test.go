// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestSetup_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("core", "1.0.0", "json", &buf)

	logger.Info("test message")

	var entry map[string]any
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "Failed to parse JSON: %s", buf.String())

	assert.Equal(t, "test message", entry["msg"])
	assert.Equal(t, "core", entry["service"])
	assert.Equal(t, "1.0.0", entry["version"])
	assert.Contains(t, entry, "time", "time field missing")
	assert.Contains(t, entry, "level", "level field missing")
}

func TestSetup_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("gateway", "1.0.0", "text", &buf)

	logger.Info("test message")

	output := buf.String()
	assert.Contains(t, output, "test message", "Output missing message")
	assert.Contains(t, output, "gateway", "Output missing service")
}

func TestHandler_TraceContext(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("core", "1.0.0", "json", &buf)

	// Create a mock span context
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), spanCtx)

	logger.InfoContext(ctx, "traced message")

	var entry map[string]any
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "Failed to parse JSON")

	assert.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", entry["trace_id"])
	assert.Equal(t, "00f067aa0ba902b7", entry["span_id"])
}

func TestHandler_NoTraceContext(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("core", "1.0.0", "json", &buf)

	logger.Info("no trace message")

	var entry map[string]any
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "Failed to parse JSON")

	// trace_id and span_id should be empty strings or missing
	if tid, ok := entry["trace_id"]; ok {
		assert.Empty(t, tid, "trace_id should be empty")
	}
	if sid, ok := entry["span_id"]; ok {
		assert.Empty(t, sid, "span_id should be empty")
	}
}

func TestSetup_DefaultFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("core", "1.0.0", "", &buf)

	logger.Info("test message")

	// Default should be JSON
	var entry map[string]any
	err := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "Default format should be JSON")
}

func TestSetDefault(t *testing.T) {
	// Capture original default logger
	original := slog.Default()
	defer slog.SetDefault(original)

	SetDefault("test-service", "2.0.0", "json")

	// Verify the default was set (we can't easily test the output without more setup)
	assert.NotEqual(t, original, slog.Default(), "SetDefault did not change the default logger")
}
