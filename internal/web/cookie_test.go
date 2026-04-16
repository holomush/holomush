// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "test-token", true, 86400)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "holomush_session", cookies[0].Name)
	assert.Equal(t, "test-token", cookies[0].Value)
	assert.True(t, cookies[0].HttpOnly)
	assert.True(t, cookies[0].Secure)
	assert.Equal(t, 86400, cookies[0].MaxAge)
}

func TestSetSessionCookie_Insecure(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "test-token", false, 86400)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.False(t, cookies[0].Secure)
}

func TestSetSessionCookieUsesGuestTTLForTwoHourSession(t *testing.T) {
	w := httptest.NewRecorder()
	const guestTTL = 7200 // 2h
	SetSessionCookie(w, "guest-token", true, guestTTL)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, guestTTL, cookies[0].MaxAge,
		"guest cookie MaxAge MUST match session TTL; otherwise cookie outlives session")
}

func TestSetSessionCookieFallsBackToDefaultWhenMaxAgeNonPositive(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "test-token", true, 0)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, cookieMaxAge, cookies[0].MaxAge,
		"zero/negative maxAge MUST fall back to the default 24h TTL")
}

func TestClearSessionCookieSecureSetsSecureAndStrictSameSite(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w, true)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "holomush_session", cookies[0].Name)
	assert.Equal(t, -1, cookies[0].MaxAge)
	assert.True(t, cookies[0].HttpOnly)
	assert.True(t, cookies[0].Secure, "Secure flag MUST match SetSessionCookie so browsers reliably clear the cookie")
	assert.Equal(t, http.SameSiteStrictMode, cookies[0].SameSite)
}

func TestClearSessionCookieInsecureSetsLaxSameSiteAndNoSecure(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w, false)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, -1, cookies[0].MaxAge)
	assert.True(t, cookies[0].HttpOnly)
	assert.False(t, cookies[0].Secure)
	assert.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
}

func TestGetSessionToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{
		Name:     "holomush_session",
		Value:    "my-token",
		HttpOnly: true,
		Secure:   true,
	})
	assert.Equal(t, "my-token", GetSessionToken(req))
}

func TestGetSessionToken_Missing(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	assert.Equal(t, "", GetSessionToken(req))
}

func TestCookieMiddlewareUsesMaxAgeHeaderForGuestSession(t *testing.T) {
	// Given a handler that signals a 2h guest session via both the token and
	// max-age headers, When the middleware runs, Then the Set-Cookie MaxAge
	// matches the signalled TTL (not the default 24h).
	const guestTTL = 7200
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerSetSessionToken, "guest-token")
		w.Header().Set(headerSetSessionMaxAge, strconv.Itoa(guestTTL))
		w.WriteHeader(http.StatusOK)
	})

	mw := CookieMiddleware(true, handler)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

	cookies := rr.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, guestTTL, cookies[0].MaxAge)
	// Signal headers MUST be stripped from the outbound response.
	assert.Empty(t, rr.Header().Get(headerSetSessionToken))
	assert.Empty(t, rr.Header().Get(headerSetSessionMaxAge))
}

func TestCookieMiddlewareDefaultsMaxAgeWhenHeaderAbsent(t *testing.T) {
	// Legacy callers that do not set the max-age header get the default 24h.
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerSetSessionToken, "player-token")
		w.WriteHeader(http.StatusOK)
	})

	mw := CookieMiddleware(true, handler)
	rr := httptest.NewRecorder()
	mw.ServeHTTP(rr, httptest.NewRequest("GET", "/", nil))

	cookies := rr.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, cookieMaxAge, cookies[0].MaxAge)
}
