// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"fmt"
	"net/http"
	"strconv"
)

const (
	cookieName   = "holomush_session"
	cookieMaxAge = 86400 // 24 hours (default, used when caller supplies no TTL)
)

// SetSessionCookie writes an HTTP session cookie to the response. The cookie's
// MaxAge matches the session's TTL (in seconds) so the cookie expires when the
// underlying session does — guest sessions (2h TTL) must not get a 24h cookie,
// or the cookie outlives the session and users see stale-session errors for
// hours. A non-positive maxAge falls back to the 24h default so legacy callers
// that don't thread the TTL still get a reasonable value.
//
// The Secure flag and SameSite policy are adjusted based on the secure param.
func SetSessionCookie(w http.ResponseWriter, token string, secure bool, maxAge int) {
	if maxAge <= 0 {
		maxAge = cookieMaxAge
	}
	http.SetCookie(w, sessionCookie(token, maxAge, secure))
}

// ClearSessionCookie expires the session cookie immediately. The Secure flag
// and SameSite policy MUST match the original SetSessionCookie call so that
// browsers consistently remove the cookie; mismatched attributes can leave
// stale cookies in place.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, sessionCookie("", -1, secure))
}

// sessionCookie builds the session cookie with Secure and SameSite attributes
// derived from the secure flag. Constructed with Secure=true by default;
// dev-mode (secure=false) downgrades the flag after construction so browsers
// accept the cookie over plain HTTP on localhost. A startup WARN (see server
// init) makes the misconfiguration obvious if this path is hit in production.
func sessionCookie(value string, maxAge int, secure bool) *http.Cookie {
	c := &http.Cookie{
		Name:     cookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
	if !secure {
		c.Secure = false
		c.SameSite = http.SameSiteLaxMode
	}
	return c
}

// GetSessionToken extracts the session token from the request cookie.
// Returns an empty string if no cookie is present.
func GetSessionToken(r *http.Request) string {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// CookieMiddleware translates between internal signal headers and HTTP cookies.
// On inbound requests, it copies the session cookie value into the
// X-Session-Token header so handlers can read it. On outbound responses, it
// converts X-Set-Session-Token and X-Clear-Session headers into Set-Cookie
// headers, keeping cookie management out of the ConnectRPC handlers.
func CookieMiddleware(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// SECURITY: Remove any client-supplied session header to prevent spoofing.
		r.Header.Del(headerInjectSessionToken)
		if token := GetSessionToken(r); token != "" {
			r.Header.Set(headerInjectSessionToken, token)
		}
		cw := &cookieWriter{ResponseWriter: w, secure: secure}
		next.ServeHTTP(cw, r)
	})
}

// cookieWriter wraps http.ResponseWriter to intercept signal headers and
// convert them to Set-Cookie before writing the response status line.
type cookieWriter struct {
	http.ResponseWriter
	secure      bool
	wroteHeader bool
}

func (cw *cookieWriter) WriteHeader(code int) {
	if !cw.wroteHeader {
		cw.wroteHeader = true
		cw.applyCookieHeaders()
	}
	cw.ResponseWriter.WriteHeader(code)
}

func (cw *cookieWriter) Write(b []byte) (int, error) {
	if !cw.wroteHeader {
		cw.wroteHeader = true
		cw.applyCookieHeaders()
	}
	n, err := cw.ResponseWriter.Write(b)
	if err != nil {
		return n, fmt.Errorf("cookie writer: %w", err)
	}
	return n, nil
}

// Flush implements http.Flusher by delegating to the underlying ResponseWriter.
// This is required for ConnectRPC server-streaming, which calls Flush after
// each frame. Apply cookie headers before flushing in case they were set but
// WriteHeader/Write weren't called yet.
func (cw *cookieWriter) Flush() {
	if !cw.wroteHeader {
		cw.wroteHeader = true
		cw.applyCookieHeaders()
	}
	if f, ok := cw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (cw *cookieWriter) Unwrap() http.ResponseWriter {
	return cw.ResponseWriter
}

// applyCookieHeaders reads signal headers, sets cookies, then removes the signal headers.
func (cw *cookieWriter) applyCookieHeaders() {
	h := cw.Header()

	if token := h.Get(headerSetSessionToken); token != "" {
		maxAge := parseMaxAgeHeader(h.Get(headerSetSessionMaxAge))
		SetSessionCookie(cw.ResponseWriter, token, cw.secure, maxAge)
		h.Del(headerSetSessionToken)
		h.Del(headerSetSessionMaxAge)
	}

	if h.Get(headerClearSession) == "true" {
		ClearSessionCookie(cw.ResponseWriter, cw.secure)
		h.Del(headerClearSession)
	}
}

// parseMaxAgeHeader returns the integer MaxAge signalled by the handler, or 0
// if the header is missing/invalid. SetSessionCookie falls back to the 24h
// default on non-positive values.
func parseMaxAgeHeader(raw string) int {
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
