// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
)

// otlpRelayMaxBody bounds the inbound OTLP trace export size. The browser
// tracer batches spans on a 1s timer (web/src/lib/telemetry.ts), so a single
// POST carries a small burst of spans — typically tens of KB. 4 MiB is
// generous headroom for legitimate batches while still bounding the relay as
// an amplification vector into the collector.
//
// Note: the cap is on the ENCODED bytes. The relay forwards Content-Encoding
// without decompressing, so a non-SDK client could POST a small gzip body that
// expands past 4 MiB at the collector. The configured browser path never hits
// this — the OTel-JS http exporter sends uncompressed JSON (gzip unimplemented
// upstream) — and the collector enforces its own limits, so we accept the
// residual surface rather than buffer-and-inflate on every request.
const otlpRelayMaxBody = 4 << 20

// otlpRelayTimeout caps the outbound POST to the collector. The collector is
// in-cluster and responds promptly; 10s tolerates a slow/contended collector
// without leaving relay goroutines hung on a wedged upstream.
const otlpRelayTimeout = 10 * time.Second

// parseOTLPCollectorEndpoint validates the configured collector base URL and
// derives the OTLP/HTTP traces ingest URL (<base>/v1/traces). The collector's
// HTTP receiver listens on its own port (4318 by default) — distinct from the
// gRPC receiver (4317) the Go SDK exports to via OTEL_EXPORTER_OTLP_ENDPOINT —
// so the relay target is configured independently.
func parseOTLPCollectorEndpoint(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", oops.Code("OTLP_RELAY_ENDPOINT_INVALID").Wrap(err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", oops.Code("OTLP_RELAY_ENDPOINT_INVALID").Errorf("endpoint scheme must be http or https, got %q", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", oops.Code("OTLP_RELAY_ENDPOINT_INVALID").Errorf("endpoint missing host")
	}
	base := strings.TrimRight(endpoint, "/")
	return base + "/v1/traces", nil
}

// NewOTLPRelayHandler returns an http.Handler that accepts browser OTLP/HTTP
// trace exports and forwards them to the configured collector's /v1/traces
// receiver. It is the OpenTelemetry analogue of the Sentry envelope relay
// (sentry_relay.go): the browser sees a same-origin POST instead of one to an
// external ingest origin, which ad-blockers and CORS would otherwise block.
//
// Unlike the Sentry relay, the forward target is fixed by server config rather
// than derived from the request, so the relay cannot be turned into an open
// forwarder. It is, however, an unauthenticated ingest into the collector;
// the body size is bounded to limit amplification abuse.
//
// Returns an error if the configured endpoint cannot be parsed; callers MUST
// leave the route unregistered in that case rather than installing a
// non-functional endpoint.
func NewOTLPRelayHandler(collectorEndpoint string) (http.Handler, error) {
	forwardURL, err := parseOTLPCollectorEndpoint(collectorEndpoint)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: otlpRelayTimeout}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Bound the inbound size to prevent the relay being used as an
		// amplification vector. MaxBytesReader closes the body and returns
		// http.MaxBytesError on overflow, which we surface as 413.
		body, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, otlpRelayMaxBody))
		if readErr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(readErr, &maxErr) {
				http.Error(w, "trace export too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "could not read trace export", http.StatusBadRequest)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), otlpRelayTimeout)
		defer cancel()

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, forwardURL, bytes.NewReader(body))
		if reqErr != nil {
			http.Error(w, "could not build upstream request", http.StatusInternalServerError)
			return
		}
		// Forward the encoding-relevant headers so the collector can decode
		// the payload. The browser OTLP/HTTP exporter sends JSON or protobuf
		// (Content-Type) and MAY compress (Content-Encoding); both must reach
		// the collector unchanged.
		if ct := r.Header.Get("Content-Type"); ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if ce := r.Header.Get("Content-Encoding"); ce != "" {
			req.Header.Set("Content-Encoding", ce)
		}

		resp, respErr := client.Do(req)
		if respErr != nil {
			errutil.LogErrorContext(r.Context(), "otlp relay: upstream POST failed", respErr)
			http.Error(w, "upstream collector POST failed", http.StatusBadGateway)
			return
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				slog.DebugContext(r.Context(), "otlp relay: close upstream body", "error", closeErr.Error())
			}
		}()

		// Mirror the collector's response (status + headers + body) back to
		// the browser exporter so it can drive its own retry/back-off on 429s
		// and surface partial-success bodies.
		for k, values := range resp.Header {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if _, copyErr := io.Copy(w, resp.Body); copyErr != nil {
			errutil.LogErrorContext(r.Context(), "otlp relay: failed to copy upstream body", copyErr)
		}
	}), nil
}
