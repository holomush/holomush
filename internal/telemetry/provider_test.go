// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/holomush/holomush/internal/telemetry"
)

func TestInit_NoEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := telemetry.Init(context.Background(), "test-svc", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestInit_WithEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://192.0.2.1:4317")
	shutdown, err := telemetry.Init(context.Background(), "test-svc", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, shutdown)

	tp := otel.GetTracerProvider()
	tracer := tp.Tracer("test")
	_, span := tracer.Start(context.Background(), "test-span")
	assert.True(t, span.SpanContext().IsValid())
	span.End()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = shutdown(ctx)
}
