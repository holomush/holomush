// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"fmt"
	"net/http"
)

const (
	cookieName       = "holomush_session"
	cookieMaxAge     = 86400   // 24 hours
	cookieMaxAgeLong = 2592000 // 30 days
)

// SetSessionCookie writes an HTTP session cookie to the response. When
// rememberMe is true the cookie persists for 30 days; otherwise 24 hours.
// The Secure flag and SameSite policy are adjusted based on the secure param.
func SetSessionCookie(w http.ResponseWriter, token string, rememberMe, secure bool) {
	maxAge := cookieMaxAge
	if rememberMe {
		maxAge = cookieMaxAgeLong
	}
	sameSite := http.SameSiteStrictMode
	if !secure {
		sameSite = http.SameSiteLaxMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
	})
}

// ClearSessionCookie expires the session cookie immediately.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
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

// applyCookieHeaders reads signal headers, sets cookies, then removes the signal headers.
func (cw *cookieWriter) applyCookieHeaders() {
	h := cw.Header()

	if token := h.Get(headerSetSessionToken); token != "" {
		rememberMe := h.Get(headerRememberMe) == "true"
		SetSessionCookie(cw.ResponseWriter, token, rememberMe, cw.secure)
		h.Del(headerSetSessionToken)
		h.Del(headerRememberMe)
	}

	if h.Get(headerClearSession) == "true" {
		ClearSessionCookie(cw.ResponseWriter)
		h.Del(headerClearSession)
	}
}
