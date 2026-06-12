// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	hostv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/host/v1"
)

type commandRegistryTestServer struct {
	hostv1.UnimplementedCommandRegistryServiceServer
	listResp *hostv1.ListCommandsResponse
	listErr  error
}

func (s *commandRegistryTestServer) ListCommands(_ context.Context, _ *hostv1.ListCommandsRequest) (*hostv1.ListCommandsResponse, error) {
	return s.listResp, s.listErr
}

// startCommandRegistryServiceTestServer starts an in-process gRPC server that
// registers CommandRegistryService from the given test double. The returned
// *grpc.ClientConn is ready to use and cleaned up via t.Cleanup.
func startCommandRegistryServiceTestServer(t *testing.T, srv *commandRegistryTestServer) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- bufconn test server
	hostv1.RegisterCommandRegistryServiceServer(server, srv)
	go func() {
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})
	conn, err := grpc.NewClient(
		"passthrough:///command-registry-service-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection -- bufconn test client
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestHostCommandListerNilClientFailsClosed(t *testing.T) {
	c := &hostCommandClient{client: nil}
	_, err := c.ListCommands(context.Background(), "01HCHAR0000000000000000AAA")
	require.Error(t, err)
}

func TestHostCommandListerMapsResponse(t *testing.T) {
	srv := &commandRegistryTestServer{
		listResp: &hostv1.ListCommandsResponse{
			Commands: []*hostv1.CommandInfo{
				{Name: "look", Help: "look around", Usage: "look", Source: "core"},
			},
			Incomplete: true,
		},
	}
	conn := startCommandRegistryServiceTestServer(t, srv)
	c := &hostCommandClient{client: hostv1.NewCommandRegistryServiceClient(conn)}
	got, err := c.ListCommands(context.Background(), "01HCHAR0000000000000000AAA")
	require.NoError(t, err)
	require.Len(t, got.Commands, 1)
	assert.Equal(t, "look", got.Commands[0].Name)
	assert.Equal(t, "look around", got.Commands[0].Help)
	assert.Equal(t, "look", got.Commands[0].Usage)
	assert.Equal(t, "core", got.Commands[0].Source)
	assert.True(t, got.Incomplete)
}
