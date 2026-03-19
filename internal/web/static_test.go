// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileServer_Embedded_ServesIndex(t *testing.T) {
	handler := FileServer("")
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	// SvelteKit production builds render "HoloMUSH" client-side via JS hydration.
	// The static HTML shell always contains the sveltekit bootstrap marker.
	assert.Contains(t, rec.Body.String(), "sveltekit")
}

func TestFileServer_Embedded_SPAFallback(t *testing.T) {
	handler := FileServer("")
	req := httptest.NewRequest(http.MethodGet, "/some/client/route", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "sveltekit")
}

func TestFileServer_Override(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "index.html", "<h1>Custom Build</h1>"))
	handler := FileServer(dir)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Custom Build")
}

func TestFileServer_Override_SPAFallback(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, writeTestFile(dir, "index.html", "<h1>Custom SPA</h1>"))
	handler := FileServer(dir)
	req := httptest.NewRequest(http.MethodGet, "/nonexistent/route", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Custom SPA")
}

func TestFileServer_MissingAsset_Returns404(t *testing.T) {
	handler := FileServer("")
	req := httptest.NewRequest(http.MethodGet, "/missing.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func writeTestFile(dir, name, content string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
}
