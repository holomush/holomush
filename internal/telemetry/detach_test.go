// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/holomush/holomush/internal/telemetry"
)

func TestDetachTraceStartsNewRootLinkedToParentSpan(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("test")

	parentCtx, parent := tracer.Start(context.Background(), "connection")
	parentSC := parent.SpanContext()

	_, detached := telemetry.DetachTrace(parentCtx, tracer, "gateway.lease_refresh")
	detached.End()
	parent.End()

	var refresh sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.Name() == "gateway.lease_refresh" {
			refresh = s
		}
	}
	require.NotNil(t, refresh, "detached span was not recorded")

	assert.NotEqual(t, parentSC.TraceID(), refresh.SpanContext().TraceID(),
		"detached span must start a new trace, not inherit the connection trace")
	assert.False(t, refresh.Parent().IsValid(),
		"detached span must be a root (no parent span)")

	links := refresh.Links()
	require.Len(t, links, 1, "detached span must carry exactly one link to the originating span")
	assert.Equal(t, parentSC.TraceID(), links[0].SpanContext.TraceID())
	assert.Equal(t, parentSC.SpanID(), links[0].SpanContext.SpanID())
}

func TestDetachTraceWithoutParentSpanIsRootWithNoLink(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	tracer := tp.Tracer("test")

	_, detached := telemetry.DetachTrace(context.Background(), tracer, "gateway.lease_refresh")
	detached.End()

	ended := sr.Ended()
	require.Len(t, ended, 1)
	assert.False(t, ended[0].Parent().IsValid())
	assert.Empty(t, ended[0].Links(), "no parent span ⇒ no link recorded")
}
