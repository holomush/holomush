// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSentryLogsTarget(t *testing.T) { // INV-L8
	dsn := "https://abc123@o4509.ingest.us.sentry.io/4510"
	url, header, err := sentryLogsTarget(dsn)
	require.NoError(t, err)
	require.Equal(t, "https://o4509.ingest.us.sentry.io/api/4510/integration/otlp/v1/logs", url)
	require.Equal(t, "sentry sentry_key=abc123", header)
}

func TestSentryLogsTarget_Invalid(t *testing.T) {
	_, _, err := sentryLogsTarget("not a dsn")
	require.Error(t, err)
}

// TestNewCollectorLogExporter_HappyPath verifies the gRPC exporter constructor
// succeeds without dialling (otlploggrpc.New is lazy — it does not connect on
// construction, so an unreachable endpoint is fine here).
func TestNewCollectorLogExporter_HappyPath(t *testing.T) {
	// Point at an unreachable address so no real network call is made.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://192.0.2.1:4317")
	exp, err := newCollectorLogExporter(context.Background())
	require.NoError(t, err)
	require.NotNil(t, exp)
	// Cleanup: shutdown is best-effort; we just assert it doesn't panic.
	_ = exp.Shutdown(context.Background())
}

// TestNewSentryLogExporter_HappyPath_WithEndpointSet exercises the
// OTEL_EXPORTER_OTLP_ENDPOINT unset guard: the env var is present when the
// constructor runs, so the guard branch (unset + deferred restore) fires.
func TestNewSentryLogExporter_HappyPath_WithEndpointSet(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://192.0.2.1:4317")
	dsn := "https://abc123@o4509.ingest.us.sentry.io/4510"
	exp, err := newSentryLogExporter(context.Background(), dsn)
	require.NoError(t, err)
	require.NotNil(t, exp)
	_ = exp.Shutdown(context.Background())
}

// TestNewSentryLogExporter_HappyPath_WithoutEndpointSet exercises the path
// where OTEL_EXPORTER_OTLP_ENDPOINT is absent (no unset-guard branch taken).
func TestNewSentryLogExporter_HappyPath_WithoutEndpointSet(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	dsn := "https://abc123@o4509.ingest.us.sentry.io/4510"
	exp, err := newSentryLogExporter(context.Background(), dsn)
	require.NoError(t, err)
	require.NotNil(t, exp)
	_ = exp.Shutdown(context.Background())
}

// TestNewSentryLogExporter_InvalidDSN asserts that an unparseable DSN returns
// an error before any exporter is constructed.
func TestNewSentryLogExporter_InvalidDSN(t *testing.T) {
	exp, err := newSentryLogExporter(context.Background(), "not-a-dsn")
	require.Error(t, err)
	require.Nil(t, exp)
}
