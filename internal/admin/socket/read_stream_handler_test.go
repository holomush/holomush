// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/admin/socket"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// fakeReadStreamRPCHandler records invocations and delegates to the
// configured handle func.
type fakeReadStreamRPCHandler struct {
	invoked bool
	handle  func(
		ctx context.Context,
		req *connect.Request[adminv1.AdminReadStreamRequest],
		stream *connect.ServerStream[adminv1.AdminReadStreamResponse],
	) error
}

func (f *fakeReadStreamRPCHandler) HandleAdminReadStream(
	ctx context.Context,
	req *connect.Request[adminv1.AdminReadStreamRequest],
	stream *connect.ServerStream[adminv1.AdminReadStreamResponse],
) error {
	f.invoked = true
	return f.handle(ctx, req, stream)
}

// newReadStreamTestServer wires an AdminReadStreamConnectHandler into a real
// ConnectRPC mux and returns a ConnectRPC client pointed at the test server.
func newReadStreamTestServer(t *testing.T, h socket.ReadStreamRPCHandler) adminv1connect.AdminServiceClient {
	t.Helper()
	adapter := socket.NewAdminReadStreamConnectHandler(h)
	mux := http.NewServeMux()
	path, handler := adminv1connect.NewAdminServiceHandler(adapter)
	mux.Handle(path, handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return adminv1connect.NewAdminServiceClient(srv.Client(), srv.URL)
}

// TestAdminReadStreamConnectHandler_Delegates verifies that a call to
// AdminReadStream over a real ConnectRPC mux reaches the injected
// ReadStreamRPCHandler (delegation not swallowed by the adapter).
func TestAdminReadStreamConnectHandler_Delegates(t *testing.T) {
	stub := &fakeReadStreamRPCHandler{
		handle: func(
			_ context.Context,
			_ *connect.Request[adminv1.AdminReadStreamRequest],
			stream *connect.ServerStream[adminv1.AdminReadStreamResponse],
		) error {
			return stream.Send(&adminv1.AdminReadStreamResponse{
				Payload: &adminv1.AdminReadStreamResponse_Finished{
					Finished: &adminv1.ReadFinished{
						TerminatedBy: adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF,
					},
				},
			})
		},
	}

	client := newReadStreamTestServer(t, stub)

	stream, err := client.AdminReadStream(
		context.Background(),
		connect.NewRequest(&adminv1.AdminReadStreamRequest{
			SessionToken:  "test-token",
			Justification: "test",
		}),
	)
	require.NoError(t, err)

	var gotFinished bool
	for stream.Receive() {
		msg := stream.Msg()
		if msg.GetFinished() != nil {
			gotFinished = true
			assert.Equal(t,
				adminv1.ReadFinished_TERMINATED_BY_CLIENT_EOF,
				msg.GetFinished().GetTerminatedBy(),
			)
			break
		}
	}
	require.NoError(t, stream.Err())
	require.True(t, gotFinished, "stream must deliver the Finished frame sent by the handler")
	require.True(t, stub.invoked, "real delegation must reach the underlying ReadStreamRPCHandler")
}

// TestAdminReadStreamConnectHandler_NilInner verifies that when the adapter
// is constructed with a nil ReadStreamRPCHandler the RPC returns
// connect.CodeUnimplemented.
func TestAdminReadStreamConnectHandler_NilInner(t *testing.T) {
	client := newReadStreamTestServer(t, nil)

	stream, err := client.AdminReadStream(
		context.Background(),
		connect.NewRequest(&adminv1.AdminReadStreamRequest{
			SessionToken:  "test-token",
			Justification: "test",
		}),
	)
	require.NoError(t, err) // error arrives on stream, not on the initial call

	for stream.Receive() {
	}
	require.Error(t, stream.Err())
	var ce *connect.Error
	require.ErrorAs(t, stream.Err(), &ce)
	assert.Equal(t, connect.CodeUnimplemented, ce.Code())
}
