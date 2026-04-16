// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import "net/http"

// Security header values. Kept as package-level constants so tests and ops
// tooling can reference the exact policy strings without drift.
const (
	// headerXContentTypeOptions disables MIME-sniffing by browsers. Always on.
	hdrContentTypeOptionsValue = "nosniff"

	// headerXFrameOptions blocks all framing, mitigating clickjacking against
	// the SPA. frame-ancestors in CSP supersedes this in modern browsers, but
	// X-Frame-Options is still honored by older clients.
	hdrFrameOptionsValue = "DENY"

	// headerReferrerPolicy trims referrers to same-origin, stripping path/query
	// on cross-origin requests while allowing full referrer for same-site nav.
	hdrReferrerPolicyValue = "strict-origin-when-cross-origin"

	// hdrHSTSValue — 1 year, include subdomains. Only sent when Secure=true to
	// avoid trapping dev-mode plain-HTTP clients into HTTPS-only for a year.
	hdrHSTSValue = "max-age=31536000; includeSubDomains"

	// hdrCSPValue — conservative CSP for the SvelteKit SPA in production:
	//   - default-src 'self'              all fetches default to same-origin
	//   - connect-src 'self' ws: wss:     allow WebSocket + ConnectRPC to origin
	//   - img-src 'self' data:            allow inline data-URI images (icons)
	//   - style-src 'self' 'unsafe-inline' Svelte component styles need inline
	//   - script-src 'self'               no inline scripts in prod builds
	//   - frame-ancestors 'none'          modern clickjacking protection
	//
	// Only emitted when Secure=true because Vite dev-mode injects inline
	// <script> tags that violate script-src 'self'. If this breaks the SPA
	// even in production, promote to a Config option (opt-in) rather than
	// relaxing the policy for all deployments.
	hdrCSPValue = "default-src 'self'; " +
		"connect-src 'self' ws: wss:; " +
		"img-src 'self' data:; " +
		"style-src 'self' 'unsafe-inline'; " +
		"script-src 'self'; " +
		"frame-ancestors 'none'"
)

// SecurityHeadersMiddleware wraps next with a standard set of HTTP security
// headers applied to every response (static files and ConnectRPC alike).
//
// Always-on headers:
//   - X-Content-Type-Options: nosniff
//   - X-Frame-Options: DENY
//   - Referrer-Policy: strict-origin-when-cross-origin
//
// Secure-only headers (set only when secure=true, which tracks TLS deployment):
//   - Strict-Transport-Security: max-age=31536000; includeSubDomains
//   - Content-Security-Policy: conservative SPA policy (see hdrCSPValue)
//
// Headers are set before delegating to next, so they appear on any response
// the inner handler emits — including errors, redirects, and streaming frames.
func SecurityHeadersMiddleware(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", hdrContentTypeOptionsValue)
		h.Set("X-Frame-Options", hdrFrameOptionsValue)
		h.Set("Referrer-Policy", hdrReferrerPolicyValue)
		if secure {
			h.Set("Strict-Transport-Security", hdrHSTSValue)
			h.Set("Content-Security-Policy", hdrCSPValue)
		}
		next.ServeHTTP(w, r)
	})
}
