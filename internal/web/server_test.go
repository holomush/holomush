// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"bytes"
	"context"
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
