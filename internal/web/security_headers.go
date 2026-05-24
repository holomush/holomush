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

	// hdrCSPValue — the header half of the SPA's Content-Security-Policy.
	//
	// CSP for this SPA is deliberately split across two policies:
	//
	//   1. This response header carries only the directives that *cannot* be
	//      expressed in a <meta> tag or that benefit from header-level enforcement:
	//        - frame-ancestors 'none'  clickjacking protection (browsers IGNORE
	//                                  frame-ancestors when set via <meta>, so it
	//                                  MUST live on the header)
	//        - base-uri 'self'         block <base> tag injection
	//        - object-src 'none'       no plugins/embeds
	//
	//   2. SvelteKit emits a <meta http-equiv="content-security-policy"> tag in the
	//      prerendered HTML (configured via `csp: { mode: 'hash' }` in
	//      web/svelte.config.js) carrying default-src/script-src/style-src/img-src/
	//      connect-src, where script-src includes the per-build sha256 hashes of
	//      SvelteKit's inline hydration bootstrap.
	//
	// The split is load-bearing: two CSPs are enforced independently and a resource
	// must satisfy ALL of them. If this header also set `script-src 'self'` (or a
	// `default-src 'self'` that scripts fall back to), it would block SvelteKit's
	// hashed inline bootstrap — the adapter-static build mounts the app from an
	// inline <script>, and a static (frozen) document cannot use a per-request
	// nonce, so the hash in the meta tag is the only way to allow it. Setting the
	// document's script policy here would re-introduce the blank-page regression
	// (holomush-11ape).
	hdrCSPValue = "frame-ancestors 'none'; " +
		"base-uri 'self'; " +
		"object-src 'none'"
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
//   - Content-Security-Policy: header half of the split SPA policy (see
//     hdrCSPValue); the script/style/connect half ships as a <meta> tag in the
//     SvelteKit build
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
