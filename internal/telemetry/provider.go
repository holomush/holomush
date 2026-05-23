// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package telemetry provides conditional OpenTelemetry SDK initialization.
// When OTEL_EXPORTER_OTLP_ENDPOINT is unset, Init returns a no-op shutdown
// so the application runs without telemetry overhead. When the endpoint is
// set, a full SDK TracerProvider with OTLP gRPC export is configured and
// registered as the global provider.
//
// When SENTRY_DSN is additionally set, Init wires Sentry's OTLP HTTP
// exporter as a second BatchSpanProcessor on the same TracerProvider —
// the existing collector pipeline is preserved and Sentry receives a
// copy of every span. Sentry's error and log channels are flushed via
// sentry.Flush in the returned shutdown function.
package telemetry

import (
	"context"
	"log/slog"
	"os"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"github.com/holomush/holomush/pkg/errutil"
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
// When SENTRY_DSN is additionally set, a Sentry OTLP HTTP exporter is wired
// as a second batcher on the same TracerProvider, and Sentry's own
// error/log buffers are flushed during shutdown.
//
// The caller MUST call the returned shutdown function before the process
// exits to ensure pending spans, errors, and logs are flushed.
func Init(ctx context.Context, serviceName, serviceVersion string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	sentryCfg, sentryEnabled := readSentryEnv()

	// Fast path: nothing configured, return a no-op shutdown. This keeps
	// tests and ad-hoc invocations free of telemetry overhead.
	if endpoint == "" && !sentryEnabled {
		return func(_ context.Context) error { return nil }, nil
	}

	// Log SDK-internal errors at WARN. The previous behaviour silently
	// dropped them (to avoid retry-noise when the collector was unreachable
	// in dev). With Sentry as a second backend that silenced auth/quota
	// failures invisibly, so operators couldn't see why Sentry-side data
	// was missing. WARN-level surfaces both kinds of issue while staying
	// out of INFO log streams.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		errutil.LogError(slog.Default().With("service", serviceName),
			"telemetry SDK error", err)
	}))

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

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	}

	if endpoint != "" {
		exporter, expErr := otlptracegrpc.New(ctx)
		if expErr != nil {
			return nil, oops.With("endpoint", endpoint).Wrap(expErr)
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(exporter))
	}

	// Sentry as a parallel consumer. If sentry.Init fails, log and continue
	// without Sentry rather than blocking the entire telemetry pipeline —
	// the primary OTel path is more important to the operator than the
	// optional trial backend.
	var sentryFlush func()
	sentryActive.Store(false)
	if sentryEnabled {
		sentryExporter, flush, sErr := initSentry(ctx, sentryCfg, serviceName, serviceVersion)
		if sErr != nil {
			errutil.LogError(slog.Default().With("service", serviceName),
				"sentry init failed; continuing without Sentry", sErr)
		} else {
			tpOpts = append(tpOpts, sdktrace.WithBatcher(sentryExporter))
			sentryFlush = flush
			sentryActive.Store(true)
		}
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info( //nolint:gosec // G706: values from env vars / caller-controlled constants, not untrusted user input
		"telemetry initialized",
		"service", serviceName,
		"otel_endpoint", endpoint,
		"sentry_enabled", sentryFlush != nil,
	)

	return func(shutdownCtx context.Context) error {
		// Flush Sentry's own buffers (errors + logs) first; tracer-provider
		// shutdown drains the span batchers including the Sentry OTLP one.
		if sentryFlush != nil {
			sentryFlush()
		}
		return tp.Shutdown(shutdownCtx)
	}, nil
}
