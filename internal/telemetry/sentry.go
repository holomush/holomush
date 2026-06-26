// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/getsentry/sentry-go"
	sentryotlp "github.com/getsentry/sentry-go/otel/otlp"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/holomush/holomush/pkg/errutil"
)

// SentryFlushTimeout bounds how long the shutdown chain waits for buffered
// Sentry events (errors + logs) to drain over the network. Sentry's own
// example uses 2s; we match it to avoid blocking SIGTERM on a slow ingest.
const SentryFlushTimeout = 2 * time.Second

// sentryEnv holds Sentry configuration resolved from the process environment.
// All fields are optional except DSN — an empty DSN disables Sentry entirely.
type sentryEnv struct {
	DSN              string
	Environment      string
	Release          string
	TracesSampleRate float64
}

// readSentryEnv resolves Sentry configuration from environment variables.
// Returns ok=false when SENTRY_DSN is unset, signalling Sentry should remain
// disabled. The sample rate defaults to 1.0 (capture everything) when
// SENTRY_TRACES_SAMPLE_RATE is unset or unparseable — appropriate for the
// trial; tune down once production volume is known.
func readSentryEnv() (sentryEnv, bool) {
	dsn := os.Getenv("SENTRY_DSN")
	if dsn == "" {
		return sentryEnv{}, false
	}
	rate := 1.0
	if raw := os.Getenv("SENTRY_TRACES_SAMPLE_RATE"); raw != "" {
		if parsed, perr := strconv.ParseFloat(raw, 64); perr == nil && parsed >= 0 && parsed <= 1 {
			rate = parsed
		}
	}
	return sentryEnv{
		DSN:              dsn,
		Environment:      os.Getenv("SENTRY_ENVIRONMENT"),
		Release:          os.Getenv("SENTRY_RELEASE"),
		TracesSampleRate: rate,
	}, true
}

// initSentry initializes the Sentry SDK and builds an OTLP HTTP span exporter
// targeting Sentry's ingest. The exporter is returned so the caller can wire
// it as an additional sdktrace.WithBatcher alongside any existing OTel
// exporter — this preserves the existing collector pipeline while adding
// Sentry as a parallel consumer.
//
// The returned flush function MUST be called during shutdown before the
// process exits, to drain buffered errors and logs. Trace spans drain via
// the tracer provider's own Shutdown.
func initSentry(ctx context.Context, cfg sentryEnv, serviceName, serviceVersion string) (sdktrace.SpanExporter, func(), error) {
	release := cfg.Release
	if release == "" {
		release = serviceName + "@" + serviceVersion
	}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          release,
		EnableTracing:    true,
		TracesSampleRate: cfg.TracesSampleRate,
		ServerName:       serviceName,
	}); err != nil {
		return nil, nil, oops.With("service", serviceName).Wrap(err)
	}

	// Temporarily unset OTEL_EXPORTER_OTLP_ENDPOINT for the duration of the
	// Sentry exporter construction. The OTel Go SDK's HTTP exporter reads
	// that env var at construction and, if it contains an `http://` scheme
	// (as it does here — it's the collector's gRPC endpoint), unconditionally
	// sets Insecure=true on the HTTP transport. There is no public
	// `WithInsecure(false)` to override this from option code, so the only
	// way to keep the Sentry POSTs on HTTPS is to hide the env during
	// construction. The collector exporter has already been built above with
	// the original env in place, so this isolation is safe.
	prevOTELEndpoint, hadOTELEndpoint := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if hadOTELEndpoint {
		if uerr := os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT"); uerr != nil {
			return nil, nil, oops.With("service", serviceName).Wrap(uerr)
		}
		defer func() {
			if rerr := os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", prevOTELEndpoint); rerr != nil {
				errutil.LogError(slog.Default().With("service", serviceName),
					"failed to restore OTEL_EXPORTER_OTLP_ENDPOINT after Sentry init", rerr)
			}
		}()
	}

	// Gzip the OTLP payload — Sentry's reference collector config recommends
	// this, and span batches compress well. Trades a small CPU cost for much
	// less network traffic.
	exporter, err := sentryotlp.NewTraceExporter(
		ctx, cfg.DSN,
		sentryotlp.WithCompression(otlptracehttp.GzipCompression),
	)
	if err != nil {
		// Roll back sentry.Init's global state by flushing immediately —
		// nothing's been emitted yet so this is just defensive cleanup.
		sentry.Flush(SentryFlushTimeout)
		return nil, nil, oops.With("service", serviceName).Wrap(err)
	}

	flush := func() { sentry.Flush(SentryFlushTimeout) }
	return exporter, flush, nil
}
