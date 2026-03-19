// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServer_StartsAndServes(t *testing.T) {
	mock := &mockCoreClient{} // Defined in handler_test.go (same package)
	handler := NewHandler(mock)

	srv := NewServer(Config{
		Addr:    "127.0.0.1:0",
		Handler: handler,
		WebDir:  "",
	})

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

func TestServer_ConnectRPCRouting(t *testing.T) {
	mock := &mockCoreClient{}
	handler := NewHandler(mock)

	srv := NewServer(Config{
		Addr:    "127.0.0.1:0",
		Handler: handler,
	})

	errCh, err := srv.Start()
	require.NoError(t, err)

	resp, err := http.Post(
		"http://"+srv.Addr()+"/holomush.web.v1.WebService/Login",
		"application/json",
		nil,
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.NotEqual(t, http.StatusNotFound, resp.StatusCode)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Stop(ctx))

	for range errCh {
	}
}
