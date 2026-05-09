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

// TestStatusHandlerStatus verifies AC-C3: Status returns the configured version
// string and healthy=true across all version inputs.
func TestStatusHandlerStatus(t *testing.T) {
	tests := []struct {
		name    string
		version string
	}{
		{"returns version and healthy when version is set", "v1.2.3-test"},
		{"returns empty version and healthy when version is unset", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &statusHandler{version: tt.version}

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
