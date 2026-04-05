// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package setup

import (
	"fmt"

	"google.golang.org/grpc"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/world"
	worldv1 "github.com/holomush/holomush/pkg/proto/holomush/world/v1"
)

// newWorldInProcessConn creates an in-memory gRPC server with WorldServiceServer
// registered and returns an InProcessConn wrapping it. The connection is suitable
// for registering in the service registry as a server-internal service.
func newWorldInProcessConn(svc *world.Service) (*plugins.InProcessConn, error) {
	srv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn only
	worldv1.RegisterWorldServiceServer(srv, world.NewGRPCServer(svc))
	conn, err := plugins.NewInProcessConn(srv)
	if err != nil {
		return nil, fmt.Errorf("world in-process conn: %w", err)
	}
	return conn, nil
}
