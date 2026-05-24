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
// copy of every span. Sentry's error channel is flushed via sentry.Flush
// in the returned shutdown function.
//
// Init also builds a LoggerProvider when any OTel log sink is enabled
// (collector and/or Sentry), gating each sink behind its config toggle and
// endpoint presence (INV-L3) with an independent per-sink severity floor
// (INV-L4). Exporter-construction failure is logged and skipped, never
// fatal (INV-L5). When at least one sink is active, Init returns an
// otelslog bridge handler in Result.LogHandler; otherwise LogHandler is nil
// and the caller keeps its stderr-only logger (INV-L7).
package telemetry

import (
	"context"
	"log/slog"
	"os"

	"github.com/samber/oops"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"

	"go.opentelemetry.io/contrib/bridges/otelslog"

	"github.com/holomush/holomush/internal/config"
	"github.com/holomush/holomush/pkg/errutil"
)

// Result carries the shutdown closure and the optional OTel log bridge
// handler back to the caller. LogHandler is nil when no OTel log sink is
// enabled (INV-L7) — the caller then keeps its stderr-only logger.
type Result struct {
	Shutdown   func(context.Context) error
	LogHandler slog.Handler
	// LogBridgeLevel is the floor for the OTel bridge handler = the minimum
	// effective level across the enabled OTel log sinks (spec §3). Only
	// meaningful when LogHandler != nil. The caller passes it as the bridge
	// gate level so a per-sink level below the global level still reaches its
	// sink's filter (INV-L4).
	LogBridgeLevel slog.Level
}

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
// as a second batcher on the same TracerProvider, and Sentry's own error
// buffers are flushed during shutdown.
//
// Init builds a LoggerProvider from the enabled OTel log sinks (logCfg gated
// against the same endpoints) and, when at least one is active, returns an
// otelslog bridge handler in Result.LogHandler (otherwise nil per INV-L7).
// global is the process-wide slog level used as the per-sink fallback.
//
// The caller MUST call Result.Shutdown before the process exits to ensure
// pending spans, log records, and errors are flushed.
func Init(ctx context.Context, serviceName, serviceVersion string, logCfg config.LoggingConfig, global slog.Level) (Result, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	sentryCfg, sentryEnabled := readSentryEnv()

	// Fast path: nothing configured, return a no-op shutdown. This keeps
	// tests and ad-hoc invocations free of telemetry overhead.
	if endpoint == "" && !sentryEnabled {
		return Result{Shutdown: func(_ context.Context) error { return nil }}, nil
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
		return Result{}, oops.With("service", serviceName).Wrap(err)
	}

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.AlwaysSample())),
	}

	if endpoint != "" {
		exporter, expErr := otlptracegrpc.New(ctx)
		if expErr != nil {
			return Result{}, oops.With("endpoint", endpoint).Wrap(expErr)
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

	// Build the LoggerProvider from the enabled OTel log sinks. When none are
	// active, lp stays nil and LogHandler is nil (INV-L7) — the caller keeps
	// its stderr-only logger.
	var (
		lp          *sdklog.LoggerProvider
		logHandler  slog.Handler
		bridgeLevel slog.Level
	)
	if procs := buildLogProcessors(ctx, logCfg, global, endpoint, sentryCfg.DSN, sentryEnabled); len(procs) > 0 {
		lpOpts := []sdklog.LoggerProviderOption{sdklog.WithResource(res)}
		for _, p := range procs {
			lpOpts = append(lpOpts, sdklog.WithProcessor(p))
		}
		lp = sdklog.NewLoggerProvider(lpOpts...)
		logglobal.SetLoggerProvider(lp)
		logHandler = otelslog.NewHandler(serviceName, otelslog.WithLoggerProvider(lp))
		// The bridge gate floor = min effective level across the enabled OTel
		// sinks (spec §3), so a per-sink level below global still reaches its
		// sink's filter (INV-L4).
		bridgeLevel, _ = enabledLogFloor(logCfg, global, endpoint, sentryEnabled)
	}

	slog.Info( //nolint:gosec // G706: values from env vars / caller-controlled constants, not untrusted user input
		"telemetry initialized",
		"service", serviceName,
		"otel_endpoint", endpoint,
		"sentry_enabled", sentryFlush != nil,
		"log_bridge_enabled", logHandler != nil,
	)

	return Result{
		LogHandler:     logHandler,
		LogBridgeLevel: bridgeLevel,
		Shutdown: func(shutdownCtx context.Context) error {
			// INV-L6 shutdown order: drain the LoggerProvider first so log
			// batches flush over their transports (collector / Sentry OTLP),
			// then the TracerProvider's span batchers, then Sentry's own SDK
			// error buffers. Logs no longer route through sentry.Flush.
			var err error
			if lp != nil {
				err = lp.Shutdown(shutdownCtx)
			}
			if tpErr := tp.Shutdown(shutdownCtx); tpErr != nil && err == nil {
				err = tpErr
			}
			if sentryFlush != nil {
				sentryFlush()
			}
			return err
		},
	}, nil
}

// slogToOTel maps a slog.Level to the closest OpenTelemetry log severity for
// per-sink level-floor comparison.
func slogToOTel(l slog.Level) otellog.Severity {
	switch {
	case l <= slog.LevelDebug:
		return otellog.SeverityDebug
	case l < slog.LevelWarn:
		return otellog.SeverityInfo
	case l < slog.LevelError:
		return otellog.SeverityWarn
	default:
		return otellog.SeverityError
	}
}

// enabledLogFloor returns the minimum effective slog level across the
// enabled OTel log sinks and whether any sink is enabled. Gating MUST match
// buildLogProcessors (INV-L3): collector on endpoint!=""&&cfg.OTel.Enabled,
// Sentry on sentryEnabled&&cfg.Sentry.Enabled.
func enabledLogFloor(cfg config.LoggingConfig, global slog.Level, endpoint string, sentryEnabled bool) (slog.Level, bool) {
	floor := global
	anyEnabled := false
	if endpoint != "" && cfg.OTel.Enabled {
		l := cfg.OTel.EffectiveLevel(global)
		if !anyEnabled || l < floor {
			floor = l
		}
		anyEnabled = true
	}
	if sentryEnabled && cfg.Sentry.Enabled {
		l := cfg.Sentry.EffectiveLevel(global)
		if !anyEnabled || l < floor {
			floor = l
		}
		anyEnabled = true
	}
	return floor, anyEnabled
}

// buildLogProcessors returns the gated log processors for the enabled OTel
// log sinks. Returns nil when none are enabled. Each sink is gated on its
// config toggle AND its endpoint being present (INV-L3) and wrapped in a
// level filter at its effective severity floor (INV-L4). Exporter-construction
// failure is logged and skipped, never fatal (INV-L5).
func buildLogProcessors(ctx context.Context, cfg config.LoggingConfig, global slog.Level, endpoint, sentryDSN string, sentryEnabled bool) []sdklog.Processor {
	var procs []sdklog.Processor
	if endpoint != "" && cfg.OTel.Enabled {
		if exp, err := newCollectorLogExporter(ctx); err != nil {
			errutil.LogError(slog.Default().With("service_component", "log-collector-exporter"), "collector log exporter init failed; skipping", err)
		} else {
			procs = append(procs, newLevelFilter(sdklog.NewBatchProcessor(exp), slogToOTel(cfg.OTel.EffectiveLevel(global))))
		}
	}
	if sentryEnabled && cfg.Sentry.Enabled {
		if exp, err := newSentryLogExporter(ctx, sentryDSN); err != nil {
			errutil.LogError(slog.Default().With("service_component", "log-sentry-exporter"), "sentry log exporter init failed; skipping", err)
		} else {
			procs = append(procs, newLevelFilter(sdklog.NewBatchProcessor(exp), slogToOTel(cfg.Sentry.EffectiveLevel(global))))
		}
	}
	return procs
}
