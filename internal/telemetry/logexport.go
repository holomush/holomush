// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"fmt"
	"os"

	"github.com/getsentry/sentry-go"
	"github.com/samber/oops"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// sentryLogsTarget derives Sentry's OTLP logs endpoint URL and the
// x-sentry-auth header value from a DSN. DSN parsing stays inside
// internal/telemetry (the only package permitted to import sentry-go);
// internal/logging never imports sentry-go (INV-L1).
func sentryLogsTarget(dsn string) (url, authHeader string, err error) {
	d, perr := sentry.NewDsn(dsn)
	if perr != nil {
		return "", "", oops.Code("SENTRY_DSN_INVALID").Wrap(perr)
	}
	host := d.GetHost()
	projectID := d.GetProjectID()
	publicKey := d.GetPublicKey()
	url = fmt.Sprintf("https://%s/api/%s/integration/otlp/v1/logs", host, projectID)
	authHeader = fmt.Sprintf("sentry sentry_key=%s", publicKey)
	return url, authHeader, nil
}

// newCollectorLogExporter builds the OTLP-gRPC log exporter targeting the
// shared collector endpoint (OTEL_EXPORTER_OTLP_ENDPOINT, env-driven).
func newCollectorLogExporter(ctx context.Context) (sdklog.Exporter, error) {
	exp, err := otlploggrpc.New(ctx)
	if err != nil {
		return nil, oops.Code("OTEL_LOG_EXPORTER_FAILED").Wrap(err)
	}
	return exp, nil
}

// newSentryLogExporter builds the OTLP-HTTP log exporter targeting Sentry.
// Reuses the OTEL_EXPORTER_OTLP_ENDPOINT unset guard from initSentry so the
// otlploghttp transport stays on HTTPS (INV-L8): the SDK forces Insecure=true
// when that env var carries an http:// scheme.
func newSentryLogExporter(ctx context.Context, dsn string) (sdklog.Exporter, error) {
	url, authHeader, err := sentryLogsTarget(dsn)
	if err != nil {
		return nil, err
	}
	// NOTE: this mutates process-global env (unset + deferred restore). It
	// assumes telemetry.Init runs once at startup, mirroring sentry.go's
	// trace-path precedent; concurrent callers would race the env var.
	prev, had := os.LookupEnv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if had {
		if uerr := os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT"); uerr != nil {
			return nil, oops.Wrap(uerr)
		}
		defer func() { _ = os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", prev) }() //nolint:errcheck // best-effort restore; failure is non-fatal
	}
	exp, err := otlploghttp.New(
		ctx,
		otlploghttp.WithEndpointURL(url),
		otlploghttp.WithHeaders(map[string]string{"x-sentry-auth": authHeader}),
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
	)
	if err != nil {
		return nil, oops.Code("SENTRY_LOG_EXPORTER_FAILED").Wrap(err)
	}
	return exp, nil
}
