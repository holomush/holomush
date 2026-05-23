// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
)

// sentryRelayMaxBody bounds the inbound envelope size. Real Sentry envelopes
// for errors with large extras can run to a few hundred KB; 1 MiB is generous
// without inviting amplification abuse via the relay.
const sentryRelayMaxBody = 1 << 20

// sentryRelayTimeout caps the outbound POST to Sentry's ingest. Sentry's
// own JS SDK uses 30s for its envelope transport; we match that.
const sentryRelayTimeout = 30 * time.Second

// SentryRelayConfig captures the configured DSN's identity so inbound
// envelopes can be validated against it before forwarding. Validation
// prevents the relay from being abused as an open forwarder to arbitrary
// Sentry projects.
type SentryRelayConfig struct {
	// Host is the DSN's hostname, e.g. "o4511439862824960.ingest.us.sentry.io".
	Host string
	// ProjectID is the trailing numeric segment of the DSN path.
	ProjectID string
}

// parseSentryDSN extracts the relay-validation fields from a Sentry DSN.
// The expected DSN shape is scheme://publicKey@host/projectID.
func parseSentryDSN(dsn string) (SentryRelayConfig, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return SentryRelayConfig{}, oops.Code("SENTRY_DSN_INVALID").Wrap(err)
	}
	if parsed.Host == "" {
		return SentryRelayConfig{}, oops.Code("SENTRY_DSN_INVALID").Errorf("DSN missing host")
	}
	projectID := strings.Trim(parsed.Path, "/")
	if projectID == "" {
		return SentryRelayConfig{}, oops.Code("SENTRY_DSN_INVALID").Errorf("DSN missing project ID")
	}
	return SentryRelayConfig{Host: parsed.Host, ProjectID: projectID}, nil
}

// envelopeHeader is the subset of fields we read from the Sentry envelope's
// first-line JSON header. Sentry's JS SDK includes the DSN in this header
// when configured with a `tunnel` option, which lets the relay verify the
// inbound envelope is destined for the same project the relay is configured
// to forward to.
type envelopeHeader struct {
	DSN string `json:"dsn"`
}

// NewSentryRelayHandler returns an http.Handler that accepts Sentry SDK
// envelope POSTs, validates the embedded DSN matches the configured one,
// and forwards the body to Sentry's ingest. Used as an ad-blocker bypass —
// the browser SDK sees a same-origin POST instead of one to a known
// telemetry domain.
//
// Returns an error if the configured DSN cannot be parsed; callers MUST
// fall back to leaving the route unregistered in that case rather than
// installing a non-functional endpoint.
func NewSentryRelayHandler(dsn string) (http.Handler, error) {
	allowed, err := parseSentryDSN(dsn)
	if err != nil {
		return nil, err
	}

	ingestURL := fmt.Sprintf("https://%s/api/%s/envelope/", allowed.Host, allowed.ProjectID)

	client := &http.Client{Timeout: sentryRelayTimeout}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Bound the inbound size to prevent the relay being used as an
		// amplification vector. MaxBytesReader closes the body and returns
		// http.MaxBytesError on overflow, which we surface as 413.
		body, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, sentryRelayMaxBody))
		if readErr != nil {
			var maxErr *http.MaxBytesError
			if errors.As(readErr, &maxErr) {
				http.Error(w, "envelope too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "could not read envelope", http.StatusBadRequest)
			return
		}

		// Validate the envelope header's DSN matches the configured one
		// before forwarding. The header is the first newline-delimited line
		// of the envelope body (JSON).
		if validateErr := validateEnvelopeDSN(body, allowed); validateErr != nil {
			errutil.LogErrorContext(r.Context(), "sentry relay: rejecting envelope",
				validateErr, "remote_addr", r.RemoteAddr)
			http.Error(w, "envelope DSN does not match configured project", http.StatusForbidden)
			return
		}

		// Forward to Sentry's ingest. Use the request's context so client
		// disconnects propagate.
		ctx, cancel := context.WithTimeout(r.Context(), sentryRelayTimeout)
		defer cancel()

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, ingestURL, bytes.NewReader(body))
		if reqErr != nil {
			http.Error(w, "could not build upstream request", http.StatusInternalServerError)
			return
		}
		// Sentry expects application/x-sentry-envelope; the browser SDK
		// sets it, but we forward whatever the client sent to keep the
		// relay transparent.
		if ct := r.Header.Get("Content-Type"); ct != "" {
			req.Header.Set("Content-Type", ct)
		}

		resp, respErr := client.Do(req)
		if respErr != nil {
			errutil.LogErrorContext(r.Context(), "sentry relay: upstream POST failed", respErr)
			http.Error(w, "upstream Sentry POST failed", http.StatusBadGateway)
			return
		}
		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				slog.Debug("sentry relay: close upstream body", "error", closeErr.Error())
			}
		}()

		// Mirror Sentry's response headers + status + body back to the
		// SDK so it can drive its own retry/back-off behaviour on 429s.
		// In particular, Retry-After and X-Sentry-Rate-Limits are how
		// Sentry tells the SDK to slow down; dropping them would cause
		// the SDK to keep hammering during throttling.
		for k, values := range resp.Header {
			for _, v := range values {
				w.Header().Add(k, v)
			}
		}
		w.WriteHeader(resp.StatusCode)
		if _, copyErr := io.Copy(w, resp.Body); copyErr != nil {
			errutil.LogErrorContext(r.Context(), "sentry relay: failed to copy upstream body", copyErr)
		}
	}), nil
}

// validateEnvelopeDSN parses the envelope's first line as JSON and checks
// the embedded `dsn` field's host + project ID against the configured
// values. Returns nil if the envelope is acceptable, an error otherwise.
func validateEnvelopeDSN(body []byte, allowed SentryRelayConfig) error {
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 4096), 64*1024)
	if !scanner.Scan() {
		return errors.New("envelope header missing")
	}
	var hdr envelopeHeader
	if err := json.Unmarshal(scanner.Bytes(), &hdr); err != nil {
		return fmt.Errorf("envelope header is not valid JSON: %w", err)
	}
	if hdr.DSN == "" {
		// Browser SDK with tunnel configured always emits the DSN; an
		// absent DSN means either a misconfigured SDK or a probe attempt.
		return errors.New("envelope header missing dsn")
	}
	inbound, err := parseSentryDSN(hdr.DSN)
	if err != nil {
		return fmt.Errorf("envelope DSN unparseable: %w", err)
	}
	if inbound.Host != allowed.Host || inbound.ProjectID != allowed.ProjectID {
		return fmt.Errorf("envelope DSN %s/%s does not match configured %s/%s",
			inbound.Host, inbound.ProjectID, allowed.Host, allowed.ProjectID)
	}
	return nil
}
