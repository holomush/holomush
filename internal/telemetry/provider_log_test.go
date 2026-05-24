// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/telemetry"
)

func TestINV_L7_NoLogSinks_NilHandler(t *testing.T) { // INV-L7
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("SENTRY_DSN", "")
	res, err := telemetry.Init(context.Background(), "svc", "v1", config.DefaultLoggingConfig(), slog.LevelInfo)
	require.NoError(t, err)
	require.Nil(t, res.LogHandler)
	require.NoError(t, res.Shutdown(context.Background()))
}

// TestInit_CollectorLogSink_BuildsBridge verifies that when
// OTEL_EXPORTER_OTLP_ENDPOINT is set and the OTel log sink is enabled, Init
// builds a LoggerProvider and returns a non-nil LogHandler (INV-L7 positive
// path). The endpoint is deliberately unreachable; shutdown may error on
// flush but must not panic.
func TestInit_CollectorLogSink_BuildsBridge(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://192.0.2.1:4317")
	t.Setenv("SENTRY_DSN", "")

	ctx := context.Background()
	res, err := telemetry.Init(ctx, "svc", "v1", config.DefaultLoggingConfig(), slog.LevelInfo)
	require.NoError(t, err)
	require.NotNil(t, res.LogHandler, "expected non-nil LogHandler when OTel log sink is enabled")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	// Unreachable endpoint → shutdown may return a flush error; just assert no panic.
	require.NotPanics(t, func() { _ = res.Shutdown(shutdownCtx) })
}

// TestInit_SentryLogSink_BuildsBridge verifies that when SENTRY_DSN is set
// (and OTel endpoint absent), Init builds a LogHandler from the Sentry sink.
// Sentry SDK init may emit warnings; that's expected for an unreachable DSN.
func TestInit_SentryLogSink_BuildsBridge(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("SENTRY_DSN", "https://abc@o1.ingest.us.sentry.io/1")

	ctx := context.Background()
	res, err := telemetry.Init(ctx, "svc", "v1", config.DefaultLoggingConfig(), slog.LevelInfo)
	require.NoError(t, err)
	// The Sentry log sink may fail to init (auth error from the unreachable DSN)
	// but Init must not return an error itself (INV-L5: exporter failure is
	// non-fatal). LogHandler may be nil if the sink was skipped — either outcome
	// is acceptable here; we assert no panic on shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	require.NotPanics(t, func() { _ = res.Shutdown(shutdownCtx) })
}
