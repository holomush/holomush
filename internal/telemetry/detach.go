// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"

	"go.opentelemetry.io/otel/trace"
)

// DetachTrace starts a NEW-ROOT span for periodic or background work that must
// not be parented under a long-lived operation's span.
//
// Parenting periodic work (e.g. a 15s connection-lease keepalive) under a
// connection-lifetime span orphans the trace: the long-lived parent is not
// exported until the operation finally ends, so every child renders as a
// rootless span carrying an "invalid parent span IDs" warning, and the trace
// grows unbounded for the connection's whole life. Starting a new root per call
// keeps each unit of work in its own complete, bounded trace.
//
// When ctx carries a span, a Link back to it is added so the detached trace can
// still be correlated to the originating operation. The caller MUST End the
// returned span.
func DetachTrace(ctx context.Context, tracer trace.Tracer, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	startOpts := []trace.SpanStartOption{trace.WithNewRoot()}
	if parent := trace.SpanContextFromContext(ctx); parent.IsValid() {
		startOpts = append(startOpts, trace.WithLinks(trace.Link{SpanContext: parent}))
	}
	startOpts = append(startOpts, opts...)
	return tracer.Start(ctx, spanName, startOpts...)
}
