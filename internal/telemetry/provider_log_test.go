// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
	"context"
	"log/slog"
	"testing"

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
