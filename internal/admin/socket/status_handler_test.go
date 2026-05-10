// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// TestCompositeHandlerStatus verifies AC-C3: Status returns the configured
// version string and healthy=true across all version inputs.
func TestCompositeHandlerStatus(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"returns version and healthy when version is set", "v1.2.3-test"},
		{"returns empty version and healthy when version is unset", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &compositeHandler{version: tt.version}

			mux := http.NewServeMux()
			path, handler := adminv1connect.NewAdminServiceHandler(h)
			mux.Handle(path, handler)

			srv := httptest.NewServer(mux)
			defer srv.Close()

			client := adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL)
			resp, err := client.Status(context.Background(), connect.NewRequest(&adminv1.StatusRequest{}))
			require.NoError(t, err)
			assert.Equal(t, tt.version, resp.Msg.Version)
			assert.True(t, resp.Msg.Healthy)
		})
	}
}

// TestCompositeHandlerReturnsUnimplementedForNilAuthenticate verifies that
// calling Authenticate when no AuthenticateHandler is registered returns
// connect.CodeUnimplemented.
func TestCompositeHandlerReturnsUnimplementedForNilAuthenticate(t *testing.T) {
	dir, err := os.MkdirTemp("", "hm-adm-")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	cfg := Config{
		SocketPath: filepath.Join(dir, "a.sock"),
		LockPath:   filepath.Join(dir, "a.lock"),
		Version:    "test-v0",
		// AuthenticateHandler intentionally nil
	}
	s := NewServer(cfg)
	_, err = s.Start()
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{Timeout: 2 * time.Second}).DialContext(ctx, "unix", cfg.SocketPath)
			},
		},
	}
	client := adminv1connect.NewAdminServiceClient(httpClient, "http://localhost")

	_, err = client.Authenticate(context.Background(), connect.NewRequest(&adminv1.AuthenticateRequest{}))
	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeUnimplemented, ce.Code())
}

// TestCompositeHandlerReturnsUnimplementedForNilApprove verifies that calling
// Approve when no ApproveHandler is registered returns connect.CodeUnimplemented.
func TestCompositeHandlerReturnsUnimplementedForNilApprove(t *testing.T) {
	h := &compositeHandler{} // all handlers nil

	mux := http.NewServeMux()
	path, handler := adminv1connect.NewAdminServiceHandler(h)
	mux.Handle(path, handler)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL)
	_, err := client.Approve(context.Background(), connect.NewRequest(&adminv1.ApproveRequest{}))
	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeUnimplemented, ce.Code())
}

// TestCompositeHandlerReturnsUnimplementedForNilResetTOTP verifies that calling
// ResetTOTP when no ResetTOTPHandler is registered returns connect.CodeUnimplemented.
func TestCompositeHandlerReturnsUnimplementedForNilResetTOTP(t *testing.T) {
	h := &compositeHandler{} // all handlers nil

	mux := http.NewServeMux()
	path, handler := adminv1connect.NewAdminServiceHandler(h)
	mux.Handle(path, handler)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL)
	_, err := client.ResetTOTP(context.Background(), connect.NewRequest(&adminv1.ResetTOTPRequest{}))
	require.Error(t, err)
	var ce *connect.Error
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, connect.CodeUnimplemented, ce.Code())
}
