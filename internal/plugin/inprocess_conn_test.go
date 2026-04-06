// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestInProcessConnImplementsClientConnInterface(t *testing.T) {
	t.Run("satisfies grpc.ClientConnInterface", func(t *testing.T) {
		srv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection
		defer srv.Stop()

		conn, err := NewInProcessConn(srv)
		require.NoError(t, err)
		defer conn.Close()

		var _ grpc.ClientConnInterface = conn
		assert.NotNil(t, conn)
	})
}

func TestInProcessConnInvokeReturnsErrorForUnknownMethod(t *testing.T) {
	t.Run("returns error for unregistered service method", func(t *testing.T) {
		srv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection
		defer srv.Stop()

		conn, err := NewInProcessConn(srv)
		require.NoError(t, err)
		defer conn.Close()

		err = conn.Invoke(context.Background(), "/test.Service/Method", nil, nil)
		assert.Error(t, err)
	})
}

func TestInProcessConnCloseIsIdempotent(t *testing.T) {
	t.Run("close can be called multiple times without error", func(t *testing.T) {
		srv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection
		defer srv.Stop()

		conn, err := NewInProcessConn(srv)
		require.NoError(t, err)

		assert.NoError(t, conn.Close())
		// Second close may error but should not panic
		_ = conn.Close()
	})
}
