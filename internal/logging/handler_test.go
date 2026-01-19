// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestSetup_JSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("core", "1.0.0", "json", &buf)

	logger.Info("test message")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v\nOutput: %s", err, buf.String())
	}

	if entry["msg"] != "test message" {
		t.Errorf("msg = %v, want 'test message'", entry["msg"])
	}
	if entry["service"] != "core" {
		t.Errorf("service = %v, want 'core'", entry["service"])
	}
	if entry["version"] != "1.0.0" {
		t.Errorf("version = %v, want '1.0.0'", entry["version"])
	}
	if _, ok := entry["time"]; !ok {
		t.Error("time field missing")
	}
	if _, ok := entry["level"]; !ok {
		t.Error("level field missing")
	}
}

func TestSetup_TextFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("gateway", "1.0.0", "text", &buf)

	logger.Info("test message")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("Output missing message: %s", output)
	}
	if !strings.Contains(output, "gateway") {
		t.Errorf("Output missing service: %s", output)
	}
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
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Errorf("trace_id = %v, want '4bf92f3577b34da6a3ce929d0e0e4736'", entry["trace_id"])
	}
	if entry["span_id"] != "00f067aa0ba902b7" {
		t.Errorf("span_id = %v, want '00f067aa0ba902b7'", entry["span_id"])
	}
}

func TestHandler_NoTraceContext(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("core", "1.0.0", "json", &buf)

	logger.Info("no trace message")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// trace_id and span_id should be empty strings or missing
	if tid, ok := entry["trace_id"]; ok && tid != "" {
		t.Errorf("trace_id should be empty, got %v", tid)
	}
	if sid, ok := entry["span_id"]; ok && sid != "" {
		t.Errorf("span_id should be empty, got %v", sid)
	}
}

func TestSetup_DefaultFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := Setup("core", "1.0.0", "", &buf)

	logger.Info("test message")

	// Default should be JSON
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Default format should be JSON, failed to parse: %v", err)
	}
}

func TestSetDefault(t *testing.T) {
	// Capture original default logger
	original := slog.Default()
	defer slog.SetDefault(original)

	SetDefault("test-service", "2.0.0", "json")

	// Verify the default was set (we can't easily test the output without more setup)
	if slog.Default() == original {
		t.Error("SetDefault did not change the default logger")
	}
}
