// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the OTel instrumentation name for the core-channels plugin.
// All spans are created on the tracer obtained from the global TracerProvider
// using this name. If no provider is configured at process startup, the global
// no-op tracer is returned and span operations become no-ops.
const tracerName = "github.com/holomush/holomush/plugins/core-channels"

// startSpan starts a span on the core-channels tracer with the given name and
// attributes. The returned context carries the span; the caller MUST defer
// span.End(). This helper is the only path to a span in the plugin so the
// instrumentation name and attribute conventions stay consistent.
func startSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	tracer := otel.GetTracerProvider().Tracer(tracerName)
	return tracer.Start(ctx, name, trace.WithAttributes(attrs...))
}

// recordError marks the span as errored with the given message and sets its
// status to codes.Error. Use at the call site instead of inlining repeated
// span.RecordError + span.SetStatus pairs.
func recordError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
