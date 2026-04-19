// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package telemetry provides OTEL header propagation and an in-process
// Prometheus exporter wiring for the JetStream event bus.
//
// Two responsibilities:
//
//   - Trace context propagation: InjectHeaders / ExtractContext wrap a
//     nats.Header as a propagation.TextMapCarrier so publishers and
//     subscribers can preserve W3C traceparent / tracestate across the
//     JetStream hop.
//   - Prometheus exporter lifecycle: StartNATSExporter spins up the
//     prometheus-nats-exporter against the embedded NATS monitoring port
//     and returns a handle the caller Stops on shutdown.
package telemetry

import (
	"context"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// InjectHeaders writes W3C traceparent / tracestate headers from ctx into h
// using the global TextMapPropagator. Use on the publish path before
// js.PublishMsg so the downstream consumer can link its span to the
// upstream producer.
//
// A nil input header is initialized automatically — nats.Header is a map,
// and the underlying Inject → Set path would panic on a nil map. Callers
// can safely do `msg.Header = telemetry.InjectHeaders(ctx, msg.Header)`
// whether msg.Header starts nil or already-initialized.
func InjectHeaders(ctx context.Context, h nats.Header) nats.Header {
	if h == nil {
		h = nats.Header{}
	}
	otel.GetTextMapPropagator().Inject(ctx, natsHeaderCarrier(h))
	return h
}

// ExtractContext reads W3C trace headers from h and returns a context linked
// to the upstream span. Use on the subscribe / audit-projection receive path
// before starting the consumer span.
func ExtractContext(ctx context.Context, h nats.Header) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, natsHeaderCarrier(h))
}

// natsHeaderCarrier adapts nats.Header to propagation.TextMapCarrier.
// nats.Header is a type alias for textproto.MIMEHeader (map[string][]string),
// so Get / Set / Keys are trivial wrappers.
type natsHeaderCarrier nats.Header

func (c natsHeaderCarrier) Get(key string) string { return nats.Header(c).Get(key) }

func (c natsHeaderCarrier) Set(key, value string) { nats.Header(c).Set(key, value) }

// Keys returns the header keys as required by propagation.TextMapCarrier.
func (c natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

var _ propagation.TextMapCarrier = natsHeaderCarrier{}
