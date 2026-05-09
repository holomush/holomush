// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// TestStatusHandlerReturnsVersionAndHealthy verifies AC-C3.
func TestStatusHandlerReturnsVersionAndHealthy(t *testing.T) {
	h := &statusHandler{version: "v1.2.3-test"}

	mux := http.NewServeMux()
	path, handler := adminv1connect.NewAdminServiceHandler(h)
	mux.Handle(path, handler)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL)
	resp, err := client.Status(context.Background(), connect.NewRequest(&adminv1.StatusRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "v1.2.3-test", resp.Msg.Version)
	assert.True(t, resp.Msg.Healthy)
}

// TestStatusHandlerReturnsEmptyVersionWhenUnset verifies that empty version
// passes through unchanged.
func TestStatusHandlerReturnsEmptyVersionWhenUnset(t *testing.T) {
	h := &statusHandler{version: ""}

	mux := http.NewServeMux()
	path, handler := adminv1connect.NewAdminServiceHandler(h)
	mux.Handle(path, handler)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL)
	resp, err := client.Status(context.Background(), connect.NewRequest(&adminv1.StatusRequest{}))
	require.NoError(t, err)
	assert.Equal(t, "", resp.Msg.Version)
	assert.True(t, resp.Msg.Healthy)
}
