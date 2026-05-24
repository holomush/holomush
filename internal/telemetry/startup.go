// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// sentryActive tracks whether Sentry export was successfully wired during
// the most recent Init call. EmitStartupSpan reads it so callers don't
// need to thread the bool through their bootstrap code.
var sentryActive atomic.Bool

// EmitStartupSpan emits a one-off "process.startup" span whose duration
// equals the elapsed bootstrap time (now − bootStart). It is intended to
// be called once per binary at the "I'm ready" point, after telemetry
// initialization and all subsystems have started.
//
// The span's start timestamp is set explicitly via trace.WithTimestamp,
// allowing the span to retroactively cover the bootstrap interval without
// requiring an open-then-close pattern threaded through all of startup.
//
// When telemetry is disabled (OTEL_EXPORTER_OTLP_ENDPOINT and SENTRY_DSN
// both unset), otel.Tracer returns a no-op tracer and this function emits
// nothing. Safe to call unconditionally.
func EmitStartupSpan(ctx context.Context, serviceName, serviceVersion string, bootStart time.Time) {
	tracer := otel.Tracer(serviceName)
	now := time.Now()
	_, span := tracer.Start(
		ctx, "process.startup",
		trace.WithTimestamp(bootStart),
		trace.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.version", serviceVersion),
			attribute.Bool("sentry.enabled", sentryActive.Load()),
			attribute.Int64("bootstrap.duration_ms", now.Sub(bootStart).Milliseconds()),
		),
	)
	span.End(trace.WithTimestamp(now))
}
