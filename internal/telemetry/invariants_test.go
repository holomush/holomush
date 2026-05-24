// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/config"
)

// TestINV_L3_L5_BuildLogProcessors_GatesAndSkips asserts that buildLogProcessors
// gates each sink on its config toggle AND endpoint presence (INV-L3), and that
// a construction failure for one sink is skipped without panic/fatal (INV-L5).
func TestINV_L3_L5_BuildLogProcessors_GatesAndSkips(t *testing.T) { // INV-L3 INV-L5
	global := slog.LevelInfo

	t.Run("no_endpoint_and_sentry_disabled_yields_zero_processors", func(t *testing.T) {
		// INV-L3: both sinks need endpoint AND config.Enabled; here endpoint=""
		// so collector is off, and sentryEnabled=false disables Sentry.
		cfg := config.DefaultLoggingConfig()
		procs := buildLogProcessors(context.Background(), cfg, global, "", "", false)
		require.Empty(t, procs, "no active sinks → zero processors (INV-L3)")
	})

	t.Run("otel_config_disabled_skips_collector_even_with_endpoint", func(t *testing.T) {
		// INV-L3: config toggle must be true AND endpoint present; toggle=false → skip.
		cfg := config.DefaultLoggingConfig()
		cfg.OTel.Enabled = false
		procs := buildLogProcessors(context.Background(), cfg, global, "http://127.0.0.1:4317", "", false)
		require.Empty(t, procs, "OTel.Enabled=false gates collector sink (INV-L3)")
	})

	t.Run("invalid_sentry_dsn_skips_sink_no_panic", func(t *testing.T) {
		// INV-L5: newSentryLogExporter errors on "not-a-dsn"; must be skipped,
		// not fatal. Collector is also disabled (no endpoint), so result is
		// zero processors — but the key assertion is no panic/fatal.
		cfg := config.DefaultLoggingConfig()
		require.NotPanics(t, func() {
			procs := buildLogProcessors(context.Background(), cfg, global, "", "not-a-dsn", true)
			require.Empty(t, procs, "invalid DSN → Sentry sink skipped (INV-L5)")
		})
	})
}

// TestINV_L4_BridgeFloorIsMinEnabledSink asserts that the bridge gate floor is
// the minimum effective level across the enabled OTel sinks, so a per-sink level
// set below the global level still reaches its sink's filter (INV-L4).
func TestINV_L4_BridgeFloorIsMinEnabledSink(t *testing.T) { // INV-L4
	cfg := config.DefaultLoggingConfig()
	cfg.Sentry.Level = "debug" // below global info
	floor, anyEnabled := enabledLogFloor(cfg, slog.LevelInfo, "collector:4317", true)
	require.True(t, anyEnabled)
	require.Equal(t, slog.LevelDebug, floor) // min(info collector, debug sentry) = debug
}

// TestINV_L6_ShutdownRunsCleanly asserts that Result.Shutdown flushes log
// batches before exit without panicking, even when the collector endpoint is
// unreachable. The shutdown path exercises lp.Shutdown → tp.Shutdown → Sentry
// flush. A short timeout ensures the batchers drain/time-out cleanly. (INV-L6)
func TestINV_L6_ShutdownRunsCleanly(t *testing.T) { // INV-L6
	// Point at an unreachable address to exercise the drain-and-timeout path.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:19317")
	t.Setenv("SENTRY_DSN", "")

	ctx := context.Background()
	res, err := Init(ctx, "test-svc", "v0.0.0", config.DefaultLoggingConfig(), slog.LevelInfo)
	require.NoError(t, err)
	require.NotNil(t, res.Shutdown)

	// Shutdown with a short timeout — the batchers must drain (or time out)
	// cleanly without panicking. We do not require a nil error because the
	// exporter may return a context deadline exceeded when the collector is
	// unreachable; that is acceptable, the key invariant is no panic/fatal.
	shutCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	require.NotPanics(t, func() {
		_ = res.Shutdown(shutCtx)
	})
}
