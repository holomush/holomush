// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import "net/http"

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
