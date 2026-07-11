// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_StartsAndServes(t *testing.T) {
	mock := &mockCoreClient{} // Defined in handler_test.go (same package)
	handler := NewHandler(mock)

	srv, err := NewServer(Config{
		Addr:    "127.0.0.1:0",
		Handler: handler,
		WebDir:  "",
	})
	require.NoError(t, err)

	errCh, err := srv.Start()
	require.NoError(t, err)
	require.NotEmpty(t, srv.Addr())

	resp, err := http.Get("http://" + srv.Addr() + "/")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Stop(ctx))

	for err := range errCh {
		t.Errorf("unexpected server error: %v", err)
	}
}

func TestNewServerLogsWarningWhenSecureIsFalse(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	mock := &mockCoreClient{}
	handler := NewHandler(mock)

	_, err := NewServer(Config{
		Addr:    "127.0.0.1:0",
		Handler: handler,
		Secure:  false,
	})
	require.NoError(t, err)

	out := buf.String()
	assert.Contains(t, out, "level=WARN")
	assert.Contains(t, out, "WITHOUT Secure flag")
}

func TestNewServerDoesNotWarnWhenSecureIsTrue(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	mock := &mockCoreClient{}
	handler := NewHandler(mock)

	_, err := NewServer(Config{
		Addr:    "127.0.0.1:0",
		Handler: handler,
		Secure:  true,
	})
	require.NoError(t, err)

	assert.False(t, strings.Contains(buf.String(), "WITHOUT Secure flag"),
		"server should not warn about insecure cookies when Secure=true")
}

func TestServer_ConnectRPCRouting(t *testing.T) {
	mock := &mockCoreClient{}
	handler := NewHandler(mock)

	srv, err := NewServer(Config{
		Addr:    "127.0.0.1:0",
		Handler: handler,
	})
	require.NoError(t, err)

	errCh, err := srv.Start()
	require.NoError(t, err)

	resp, err := http.Post(
		"http://"+srv.Addr()+"/holomush.web.v1.WebService/WebCreateGuest",
		"application/json",
		nil,
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.NotEqual(t, http.StatusNotFound, resp.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Stop(ctx))

	// Drain channel to confirm clean shutdown (no errors).
	for err := range errCh {
		t.Errorf("unexpected server error after stop: %v", err)
	}
}

// Verifies GH-4785: the public ConnectRPC handler must cap inbound request
// bodies so an unauthenticated POST cannot buffer an arbitrarily large body
// into memory and OOM the gateway.
func TestServerRejectsOversizedRequestBody(t *testing.T) {
	mock := &mockCoreClient{}
	handler := NewHandler(mock)

	srv, err := NewServer(Config{
		Addr:    "127.0.0.1:0",
		Handler: handler,
	})
	require.NoError(t, err)

	errCh, err := srv.Start()
	require.NoError(t, err)

	// A body far larger than the 4 MiB cap. Without connect.WithReadMaxBytes
	// the gateway would read the whole thing into memory (the OOM vector);
	// with the cap connect rejects it as resource_exhausted before buffering.
	oversized := strings.Repeat("A", 5*1024*1024)
	resp, err := http.Post(
		"http://"+srv.Addr()+"/holomush.web.v1.WebService/WebCreateGuest",
		"application/json",
		strings.NewReader(oversized),
	)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	// Connect maps CodeResourceExhausted to HTTP 429 and carries the code
	// string in its JSON error envelope.
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode,
		"oversized body should be rejected, not buffered; got body: %s", body)
	assert.Contains(t, string(body), "resource_exhausted")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Stop(ctx))

	for err := range errCh {
		t.Errorf("unexpected server error after stop: %v", err)
	}
}
