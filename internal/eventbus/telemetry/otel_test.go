// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry_test

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/holomush/holomush/internal/eventbus/telemetry"
)

// setTraceContextPropagator installs a W3C TraceContext propagator for the
// duration of the test. Because otel propagators are process-global, we set
// and restore so tests remain order-independent.
func setTraceContextPropagator(t *testing.T) {
	t.Helper()
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
}

func TestInjectExtractRoundTripPreservesTraceContext(t *testing.T) {
	setTraceContextPropagator(t)

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	tr := tp.Tracer("test")
	ctx, span := tr.Start(context.Background(), "publish")
	defer span.End()

	h := nats.Header{}
	telemetry.InjectHeaders(ctx, h)
	require.NotEmpty(t, h.Get("traceparent"), "InjectHeaders must write the W3C traceparent header")

	extractedCtx := telemetry.ExtractContext(context.Background(), h)
	_, child := tr.Start(extractedCtx, "consume")
	defer child.End()

	assert.Equal(t, span.SpanContext().TraceID(), child.SpanContext().TraceID(),
		"child span must inherit parent trace ID across the header round-trip")
}

func TestInjectHeadersIsNoopWithoutActiveSpan(t *testing.T) {
	setTraceContextPropagator(t)

	h := nats.Header{}
	telemetry.InjectHeaders(context.Background(), h)

	assert.Empty(t, h.Get("traceparent"),
		"no active span means no traceparent header should be written")
}

// carrierKeysAccessor lets the test reach the unexported natsHeaderCarrier
// type's Keys() method via the exported TextMapCarrier contract. We build a
// minimal propagator whose Extract calls Keys() to verify our adapter
// satisfies propagation.TextMapCarrier end-to-end.
type keysReadingPropagator struct {
	got *[]string
}

func (p keysReadingPropagator) Inject(_ context.Context, carrier propagation.TextMapCarrier) {
	*p.got = carrier.Keys()
}
func (p keysReadingPropagator) Extract(ctx context.Context, _ propagation.TextMapCarrier) context.Context {
	return ctx
}
func (p keysReadingPropagator) Fields() []string { return []string{"traceparent"} }

func TestInjectedHeadersExposeKeysForCarrierContract(t *testing.T) {
	// Keys() is part of the propagation.TextMapCarrier contract. Some
	// propagators iterate it (for example a future Baggage composite or a
	// custom propagator). We swap in a test propagator whose Inject reads
	// Keys() directly against the adapter, proving our Keys() implementation
	// is reachable via the interface.
	prev := otel.GetTextMapPropagator()
	var captured []string
	otel.SetTextMapPropagator(keysReadingPropagator{got: &captured})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })

	h := nats.Header{}
	h.Set("traceparent", "00-00000000000000000000000000000001-0000000000000001-01")
	h.Set("custom-header", "value")

	telemetry.InjectHeaders(context.Background(), h)

	// nats.Header.Set stores keys as-written (MIMEHeader-style canonicalization
	// is not applied by nats.Header.Set) — our Keys() reflects the map's
	// literal keys, which is what TextMapCarrier requires.
	assert.ElementsMatch(t, []string{"traceparent", "custom-header"}, captured,
		"natsHeaderCarrier.Keys() must return every header key")
}

func TestExtractContextReturnsBaseCtxWhenHeadersEmpty(t *testing.T) {
	setTraceContextPropagator(t)

	base := context.Background()
	got := telemetry.ExtractContext(base, nats.Header{})

	// Assert on the EXTRACTED context's span directly, not a child we
	// spin off it — a child's own SpanContext always reports IsRemote=false
	// regardless of whether the parent was remote, so the previous child-based
	// assertion would pass even if empty headers accidentally produced a
	// remote parent.
	parent := trace.SpanContextFromContext(got)
	assert.False(t, parent.IsValid(),
		"empty headers must produce an invalid (no-remote) SpanContext")
	assert.False(t, parent.IsRemote(),
		"empty headers must not produce a remote parent SpanContext")
}
