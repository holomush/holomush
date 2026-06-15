// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"net"

	"github.com/samber/oops"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const inProcessBufSize = 1 << 20 // 1 MiB

// InProcessConn wraps a gRPC server as a grpc.ClientConnInterface using an
// in-memory bufconn listener. This allows server-internal services to be
// registered in the service registry using the same interface as
// plugin-provided services, without requiring a real network connection.
type InProcessConn struct {
	conn     *grpc.ClientConn
	listener *bufconn.Listener
	server   *grpc.Server
}

// NewInProcessConn starts srv on an in-memory bufconn listener and returns a
// client connection to it. The caller must call Close when done.
//
// Extra dialOpts are appended after the fixed context-dialer and insecure
// transport credentials, letting callers attach client-side interceptors (e.g.
// the Lua dispatch-propagation outgoing interceptor, which marshals the
// host-vouched dispatch context into outgoing metadata across this bufconn —
// holomush-eykuh.4.13). The bufconn carries no wire to encrypt, so the insecure
// transport credentials are always correct here regardless of dialOpts.
func NewInProcessConn(srv *grpc.Server, dialOpts ...grpc.DialOption) (*InProcessConn, error) {
	if srv == nil {
		return nil, oops.Errorf("grpc server must not be nil")
	}

	lis := bufconn.Listen(inProcessBufSize)

	go func() {
		// Serve returns when the server is stopped. Ignore the error — it is
		// always non-nil (typically "use of closed network connection") after
		// lis.Close().
		_ = srv.Serve(lis) //nolint:errcheck // always non-nil after lis.Close(); no meaningful action
	}()

	dialer := func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}

	opts := append([]grpc.DialOption{
		grpc.WithContextDialer(dialer),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection
	}, dialOpts...)
	conn, err := grpc.NewClient("passthrough:///bufconn", opts...)
	if err != nil {
		_ = lis.Close() //nolint:errcheck // best-effort cleanup on dial failure
		return nil, oops.Wrap(err)
	}

	return &InProcessConn{conn: conn, listener: lis, server: srv}, nil
}

// Invoke delegates to the underlying ClientConn, satisfying grpc.ClientConnInterface.
func (c *InProcessConn) Invoke(ctx context.Context, method string, args, reply any, opts ...grpc.CallOption) error {
	return c.conn.Invoke(ctx, method, args, reply, opts...) //nolint:wrapcheck // pass-through delegation to underlying ClientConn
}

// NewStream delegates to the underlying ClientConn, satisfying grpc.ClientConnInterface.
func (c *InProcessConn) NewStream(ctx context.Context, desc *grpc.StreamDesc, method string, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return c.conn.NewStream(ctx, desc, method, opts...) //nolint:wrapcheck // pass-through delegation to underlying ClientConn
}

// Close gracefully stops the gRPC server, then shuts down the client
// connection and the in-memory listener.
func (c *InProcessConn) Close() error {
	c.server.Stop()
	connErr := c.conn.Close()
	lisErr := c.listener.Close()
	if connErr != nil {
		return oops.Wrap(connErr)
	}
	if lisErr != nil {
		return oops.Wrap(lisErr)
	}
	return nil
}
