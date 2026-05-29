// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOTLPCollectorEndpointBuildsTracesURL(t *testing.T) {
	got, err := parseOTLPCollectorEndpoint("http://otel-collector:4318")
	require.NoError(t, err)
	assert.Equal(t, "http://otel-collector:4318/v1/traces", got)
}

func TestParseOTLPCollectorEndpointTrimsTrailingSlash(t *testing.T) {
	got, err := parseOTLPCollectorEndpoint("http://otel-collector:4318/")
	require.NoError(t, err)
	assert.Equal(t, "http://otel-collector:4318/v1/traces", got)
}

func TestParseOTLPCollectorEndpointRejectsMissingScheme(t *testing.T) {
	_, err := parseOTLPCollectorEndpoint("otel-collector:4318")
	require.Error(t, err)
}

func TestParseOTLPCollectorEndpointRejectsMissingHost(t *testing.T) {
	_, err := parseOTLPCollectorEndpoint("http:///v1/traces")
	require.Error(t, err)
}

func TestNewOTLPRelayHandlerRejectsInvalidEndpoint(t *testing.T) {
	_, err := NewOTLPRelayHandler("not a url with spaces")
	require.Error(t, err)
}

func TestOTLPRelayHandlerRejectsNonPost(t *testing.T) {
	handler, err := NewOTLPRelayHandler("http://otel-collector:4318")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/otlp/v1/traces", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, http.MethodPost, rec.Header().Get("Allow"))
}

func TestOTLPRelayHandlerRejectsOversizeBody(t *testing.T) {
	handler, err := NewOTLPRelayHandler("http://otel-collector:4318")
	require.NoError(t, err)

	body := bytes.Repeat([]byte("a"), otlpRelayMaxBody+1)
	req := httptest.NewRequest(http.MethodPost, "/api/otlp/v1/traces", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

func TestOTLPRelayHandlerForwardsToCollectorTracesPath(t *testing.T) {
	var (
		receivedPath     string
		receivedBody     []byte
		receivedType     string
		receivedEncoding string
	)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		receivedBody, _ = io.ReadAll(r.Body)
		receivedType = r.Header.Get("Content-Type")
		receivedEncoding = r.Header.Get("Content-Encoding")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"partialSuccess":{}}`))
	}))
	defer collector.Close()

	handler, err := NewOTLPRelayHandler(collector.URL)
	require.NoError(t, err)

	payload := `{"resourceSpans":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/otlp/v1/traces", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "/v1/traces", receivedPath)
	assert.Equal(t, payload, string(receivedBody))
	assert.Equal(t, "application/json", receivedType)
	assert.Equal(t, "gzip", receivedEncoding, "Content-Encoding must be forwarded so the collector can decode")
	assert.Equal(t, `{"partialSuccess":{}}`, rec.Body.String(), "upstream response body is mirrored back")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"), "upstream response headers are mirrored back")
}

func TestOTLPRelayHandlerReturnsBadGatewayWhenCollectorUnreachable(t *testing.T) {
	// Point at a closed server so the forward POST fails at dial time.
	collector := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachableURL := collector.URL
	collector.Close()

	handler, err := NewOTLPRelayHandler(unreachableURL)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/otlp/v1/traces", strings.NewReader(`{"resourceSpans":[]}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadGateway, rec.Code)
}
