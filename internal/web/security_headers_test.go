// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestSecurityHeadersSetsAlwaysOnHeadersInInsecureMode(t *testing.T) {
	handler := SecurityHeadersMiddleware(false, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
}

func TestSecurityHeadersOmitsHSTSAndCSPInInsecureMode(t *testing.T) {
	handler := SecurityHeadersMiddleware(false, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Strict-Transport-Security"),
		"HSTS must not be set in insecure mode (dev-mode over plain HTTP)")
	assert.Empty(t, rec.Header().Get("Content-Security-Policy"),
		"CSP must not be set in insecure mode to avoid breaking Vite dev-mode inline scripts")
}

func TestSecurityHeadersSetsHSTSInSecureMode(t *testing.T) {
	handler := SecurityHeadersMiddleware(true, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "max-age=31536000; includeSubDomains",
		rec.Header().Get("Strict-Transport-Security"))
}

func TestSecurityHeadersSetsCSPInSecureMode(t *testing.T) {
	handler := SecurityHeadersMiddleware(true, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	csp := rec.Header().Get("Content-Security-Policy")
	assert.Contains(t, csp, "default-src 'self'")
	assert.Contains(t, csp, "connect-src 'self' ws: wss:")
	assert.Contains(t, csp, "img-src 'self' data:")
	assert.Contains(t, csp, "style-src 'self' 'unsafe-inline'")
	assert.Contains(t, csp, "script-src 'self'")
	assert.Contains(t, csp, "frame-ancestors 'none'")
}

func TestSecurityHeadersSetsAlwaysOnHeadersInSecureMode(t *testing.T) {
	handler := SecurityHeadersMiddleware(true, okHandler())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rec.Header().Get("Referrer-Policy"))
}

func TestSecurityHeadersDelegatesToWrappedHandler(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})
	handler := SecurityHeadersMiddleware(false, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.True(t, called, "wrapped handler must be invoked")
	assert.Equal(t, http.StatusTeapot, rec.Code)
}

func TestSecurityHeadersAppliedBeforeHandlerWritesStatus(t *testing.T) {
	// Ensures headers are set prior to WriteHeader, so they appear on all responses
	// (including early returns or error responses from the wrapped handler).
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Handler writes an error status immediately without touching headers.
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	handler := SecurityHeadersMiddleware(true, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rec.Header().Get("X-Frame-Options"))
	assert.NotEmpty(t, rec.Header().Get("Strict-Transport-Security"))
	assert.NotEmpty(t, rec.Header().Get("Content-Security-Policy"))
}
