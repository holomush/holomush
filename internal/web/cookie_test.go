// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "test-token", false, true)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "holomush_session", cookies[0].Name)
	assert.Equal(t, "test-token", cookies[0].Value)
	assert.True(t, cookies[0].HttpOnly)
	assert.True(t, cookies[0].Secure)
	assert.Equal(t, 86400, cookies[0].MaxAge)
}

func TestSetSessionCookie_RememberMe(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "test-token", true, true)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, 2592000, cookies[0].MaxAge)
}

func TestSetSessionCookie_Insecure(t *testing.T) {
	w := httptest.NewRecorder()
	SetSessionCookie(w, "test-token", false, false)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.False(t, cookies[0].Secure)
}

func TestClearSessionCookie(t *testing.T) {
	w := httptest.NewRecorder()
	ClearSessionCookie(w)
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, -1, cookies[0].MaxAge)
}

func TestGetSessionToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(&http.Cookie{Name: "holomush_session", Value: "my-token"})
	assert.Equal(t, "my-token", GetSessionToken(req))
}

func TestGetSessionToken_Missing(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	assert.Equal(t, "", GetSessionToken(req))
}
