// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// echoServiceName is the fully qualified gRPC service name used in proxy tests.
const echoServiceName = "test.proxy.v1.EchoService"

// echoServiceIface is the interface the gRPC service descriptor requires for HandlerType.
type echoServiceIface interface {
	Echo(ctx context.Context, req *RawMessage) (*RawMessage, error)
}

// echoServiceDesc is a minimal gRPC service descriptor for testing proxy forwarding.
// It registers a single unary method that echoes the raw request bytes back.
var echoServiceDesc = grpc.ServiceDesc{
	ServiceName: echoServiceName,
	HandlerType: (*echoServiceIface)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Echo",
			Handler:    echoHandler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "test/proxy/v1/echo.proto",
}

type echoServer struct{}

func (e *echoServer) Echo(_ context.Context, req *RawMessage) (*RawMessage, error) {
	return req, nil
}

func echoHandler(srv any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) { //nolint:revive // ctx position required by grpc.methodHandler signature
	msg := &RawMessage{}
	if err := dec(msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// unhealthyReporter always returns false from Healthy().
type unhealthyReporter struct{}

func (u *unhealthyReporter) Healthy() bool { return false }

// startBackendServer starts a bufconn gRPC server hosting the echo service
// and returns a ClientConn connected to it.
func startBackendServer(t *testing.T) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer() // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn for tests
	srv.RegisterService(&echoServiceDesc, &echoServer{})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop(); _ = lis.Close() })

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// startFrontendWithProxy starts a bufconn gRPC server with the given proxy
// installed as UnknownServiceHandler and returns a ClientConn connected to it.
func startFrontendWithProxy(t *testing.T, proxy *GRPCServiceProxy) *grpc.ClientConn {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(proxy.Handler()) // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn for tests
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(func() { srv.Stop(); _ = lis.Close() })

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // nosemgrep: go.grpc.tls.grpc-client-new-insecure-connection.grpc-client-new-insecure-connection
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestNewGRPCServiceProxy(t *testing.T) {
	t.Run("creates proxy with registry", func(t *testing.T) {
		reg := NewServiceRegistry()
		proxy := NewGRPCServiceProxy(reg)
		assert.NotNil(t, proxy)
	})

	t.Run("returns a grpc.ServerOption from Handler()", func(t *testing.T) {
		reg := NewServiceRegistry()
		proxy := NewGRPCServiceProxy(reg)
		opt := proxy.Handler()
		assert.NotNil(t, opt)
	})
}

func TestExtractServiceName(t *testing.T) {
	t.Run("extracts service from standard gRPC method", func(t *testing.T) {
		assert.Equal(t, "holomush.scene.v1.SceneService", extractServiceName("/holomush.scene.v1.SceneService/CreateScene"))
	})

	t.Run("returns empty for malformed method without leading slash", func(t *testing.T) {
		assert.Equal(t, "", extractServiceName("holomush.scene.v1.SceneService/CreateScene"))
	})

	t.Run("returns empty for method without service separator", func(t *testing.T) {
		assert.Equal(t, "", extractServiceName("/nomethod"))
	})

	t.Run("handles simple service name", func(t *testing.T) {
		assert.Equal(t, "MyService", extractServiceName("/MyService/MyMethod"))
	})
}

func TestRawMessage(t *testing.T) {
	t.Run("round-trips data through marshal/unmarshal", func(t *testing.T) {
		msg := &RawMessage{}
		require.NoError(t, msg.Unmarshal([]byte("hello")))
		data, err := msg.Marshal()
		require.NoError(t, err)
		assert.Equal(t, []byte("hello"), data)
	})

	t.Run("reset clears data", func(t *testing.T) {
		msg := &RawMessage{data: []byte("hello")}
		msg.Reset()
		assert.Nil(t, msg.data)
	})
}

func TestGRPCServiceProxy(t *testing.T) {
	t.Run("forwards call to backend and returns response", func(t *testing.T) {
		backendConn := startBackendServer(t)

		reg := NewServiceRegistry()
		require.NoError(t, reg.Register(RegisteredService{
			Name:       echoServiceName,
			Conn:       backendConn,
			PluginName: "test-plugin",
			PluginType: "binary",
		}))

		proxy := NewGRPCServiceProxy(reg)
		frontConn := startFrontendWithProxy(t, proxy)

		reqPayload := []byte("ping")
		resp := &RawMessage{}
		err := frontConn.Invoke(
			context.Background(),
			"/"+echoServiceName+"/Echo",
			&RawMessage{data: reqPayload},
			resp,
		)
		require.NoError(t, err)
		assert.Equal(t, reqPayload, resp.data)
	})

	t.Run("returns Unimplemented for unknown service", func(t *testing.T) {
		reg := NewServiceRegistry()
		proxy := NewGRPCServiceProxy(reg)
		frontConn := startFrontendWithProxy(t, proxy)

		err := frontConn.Invoke(
			context.Background(),
			"/no.such.v1.Service/Method",
			&RawMessage{data: []byte("req")},
			&RawMessage{},
		)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unimplemented, st.Code())
	})

	t.Run("returns Unavailable when Conn is nil", func(t *testing.T) {
		reg := NewServiceRegistry()
		require.NoError(t, reg.Register(RegisteredService{
			Name:       echoServiceName,
			Conn:       nil,
			PluginName: "broken-plugin",
			PluginType: "binary",
		}))

		proxy := NewGRPCServiceProxy(reg)
		frontConn := startFrontendWithProxy(t, proxy)

		err := frontConn.Invoke(
			context.Background(),
			"/"+echoServiceName+"/Echo",
			&RawMessage{data: []byte("req")},
			&RawMessage{},
		)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
		assert.Contains(t, st.Message(), "no connection")
	})

	t.Run("returns Unavailable when health reporter says unhealthy", func(t *testing.T) {
		backendConn := startBackendServer(t)

		reg := NewServiceRegistry()
		require.NoError(t, reg.Register(RegisteredService{
			Name:       echoServiceName,
			Conn:       backendConn,
			PluginName: "sick-plugin",
			PluginType: "binary",
			Health:     &unhealthyReporter{},
		}))

		proxy := NewGRPCServiceProxy(reg)
		frontConn := startFrontendWithProxy(t, proxy)

		err := frontConn.Invoke(
			context.Background(),
			"/"+echoServiceName+"/Echo",
			&RawMessage{data: []byte("req")},
			&RawMessage{},
		)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unavailable, st.Code())
		assert.Contains(t, st.Message(), "unhealthy")
	})
}
