// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/internal/telemetry"
)

func TestInitReturnsNoopProviderWhenEndpointIsEmpty(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	res, err := telemetry.Init(context.Background(), "test-svc", "1.0.0", config.DefaultLoggingConfig(), slog.LevelInfo)
	require.NoError(t, err)
	require.NotNil(t, res.Shutdown)
	require.NoError(t, res.Shutdown(context.Background()))
}

func TestInitReturnsOTLPProviderWhenEndpointIsSet(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://192.0.2.1:4317")
	res, err := telemetry.Init(context.Background(), "test-svc", "1.0.0", config.DefaultLoggingConfig(), slog.LevelInfo)
	require.NoError(t, err)
	require.NotNil(t, res.Shutdown)

	tp := otel.GetTracerProvider()
	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	assert.True(t, span.SpanContext().IsValid())
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// The endpoint is deliberately unreachable, so Shutdown may return a
	// flush/deadline error — assert it completes without panicking rather than
	// requiring NoError (which would be flaky against an unreachable collector).
	require.NotPanics(t, func() { _ = res.Shutdown(ctx) })
}
