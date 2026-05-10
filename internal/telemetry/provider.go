// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package telemetry provides conditional OpenTelemetry SDK initialization.
// When OTEL_EXPORTER_OTLP_ENDPOINT is unset, Init returns a no-op shutdown
// so the application runs without telemetry overhead. When the endpoint is
// set, a full SDK TracerProvider with OTLP gRPC export is configured and
// registered as the global provider.
package telemetry

import (
	"context"
	"os"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init initializes the OpenTelemetry SDK.
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is empty, Init registers no-op global
// providers and returns a no-op shutdown function. When the endpoint is set,
// Init creates an OTLP gRPC exporter and a TracerProvider backed by a
// BatchSpanProcessor, registers it as the global provider along with a W3C
// TraceContext propagator, and returns a shutdown function that flushes and
// stops the provider.
//
// The caller MUST call the returned shutdown function before the process
// exits to ensure pending spans are flushed.
func Init(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		return func(_ context.Context) error { return nil }, nil
	}

	// Suppress SDK-internal retry/transport error logs to avoid noise when
	// the collector is temporarily unreachable.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(_ error) {}))

	res, err := resource.New(
		ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(serviceVersion),
		),
	)
	if err != nil {
		return nil, oops.With("service", serviceName).Wrap(err)
	}

	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, oops.With("endpoint", endpoint).Wrap(err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}
