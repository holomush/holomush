// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package luabridge_test

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/holomush/holomush/internal/plugin/goplugin"
	"github.com/holomush/holomush/internal/plugin/luabridge"
)

// echoServiceDescriptor builds a programmatic ServiceDescriptor for a single
// unary RPC `Echo(EchoRequest{message}) -> EchoReply{reply}` in package
// `test.echo.v1`, service short-name `Echo`. The host holds such a descriptor at
// load time from the provider's registered FileDescriptor; here it is synthesized
// so the test does not depend on a compiled provider .pb.go.
func echoServiceDescriptor(t *testing.T) protoreflect.ServiceDescriptor {
	t.Helper()
	strKind := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	optLabel := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test/echo/v1/echo.proto"),
		Package: proto.String("test.echo.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("EchoRequest"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("message"), Number: proto.Int32(1), Type: strKind, Label: optLabel, JsonName: proto.String("message")},
				},
			},
			{
				Name: proto.String("EchoReply"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("reply"), Number: proto.Int32(1), Type: strKind, Label: optLabel, JsonName: proto.String("reply")},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("Echo"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:       proto.String("Echo"),
						InputType:  proto.String(".test.echo.v1.EchoRequest"),
						OutputType: proto.String(".test.echo.v1.EchoReply"),
					},
				},
			},
		},
	}
	fd, err := protodesc.NewFile(fdp, nil)
	require.NoError(t, err)
	return fd.Services().Get(0)
}

// startEchoProvider stands up a gRPC server that serves the echo service via a
// generic byte-forwarding UnknownServiceHandler (the provider's real handler
// shape), decoding the dynamic request and echoing `message` back as `reply`. It
// returns a ClientConn to that provider — the same `svc.Conn` the binary host
// would hand to NewBrokerProxy.
func startEchoProvider(t *testing.T, desc protoreflect.ServiceDescriptor) *grpc.ClientConn {
	t.Helper()
	method := desc.Methods().Get(0)

	handler := grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		in := dynamicpb.NewMessage(method.Input())
		if err := stream.RecvMsg(in); err != nil {
			return err
		}
		msg := in.Get(method.Input().Fields().ByName("message")).String()
		out := dynamicpb.NewMessage(method.Output())
		out.Set(method.Output().Fields().ByName("reply"), protoreflect.ValueOfString(msg))
		return stream.SendMsg(out)
	})

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer(handler) // nosemgrep: go.grpc.security.grpc-server-insecure-connection.grpc-server-insecure-connection -- in-memory bufconn for tests
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

// brokerProxyLoopback wraps providerConn in the *same* BrokerProxy the binary
// host uses (NewBrokerProxy), serves it on a bufconn, and returns a ClientConn
// to the proxy. This is the loopback the Lua bridge dials: server end is
// BrokerProxy, which transparently forwards to the provider.
func brokerProxyLoopback(t *testing.T, providerConn grpc.ClientConnInterface, pluginName string) *grpc.ClientConn {
	t.Helper()
	factory := goplugin.NewBrokerProxy(providerConn, pluginName)
	srv := factory(nil)

	lis := bufconn.Listen(1 << 20)
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

// TestPluginServiceTableInvokesViaBrokerProxy builds a Lua table for a fake
// provider service from its ServiceDescriptor and asserts a call from Lua
// reaches the provider through the *same* BrokerProxy the binary path uses, with
// the request and response round-tripping through dynamicpb.
func TestPluginServiceTableInvokesViaBrokerProxy(t *testing.T) {
	desc := echoServiceDescriptor(t)
	providerConn := startEchoProvider(t, desc)
	conn := brokerProxyLoopback(t, providerConn, "echo-bot")

	L := lua.NewState()
	defer L.Close()

	require.NoError(t, luabridge.RegisterPluginService(L, conn, desc, "echo-bot"))

	// namespace = lowercased service short-name ("Echo" -> "echo"); method "Echo".
	require.NoError(t, L.DoString(`local r = echo.Echo{message="hi"}; assert(r.reply == "hi", "expected reply 'hi', got "..tostring(r.reply))`))
}

// TestRegisterPluginServiceSetsNamespaceGlobal asserts the service is registered
// under a global named by the lowercased service short-name, exposing each unary
// method as a function field.
func TestRegisterPluginServiceSetsNamespaceGlobal(t *testing.T) {
	desc := echoServiceDescriptor(t)
	providerConn := startEchoProvider(t, desc)
	conn := brokerProxyLoopback(t, providerConn, "echo-bot")

	L := lua.NewState()
	defer L.Close()
	require.NoError(t, luabridge.RegisterPluginService(L, conn, desc, "echo-bot"))

	ns := L.GetGlobal("echo")
	require.Equal(t, lua.LTTable, ns.Type(), "service namespace must be a global table")
	tbl, ok := ns.(*lua.LTable)
	require.True(t, ok)
	assert.Equal(t, lua.LTFunction, L.GetField(tbl, "Echo").Type(), "method Echo must be a function field")
}

// TestRegisterPluginServiceFailsEarlyOnStreamingOnlyService asserts fail-early
// validation: a service whose only method is streaming (which Lua cannot consume)
// is rejected at build time (load), not deferred to first call.
func TestRegisterPluginServiceFailsEarlyOnStreamingOnlyService(t *testing.T) {
	desc := streamingOnlyServiceDescriptor(t)
	L := lua.NewState()
	defer L.Close()
	err := luabridge.RegisterPluginService(L, nil, desc, "stream-bot")
	require.Error(t, err, "a service with no unary methods must fail at build")
	assert.Equal(t, lua.LTNil, L.GetGlobal("streamonly").Type(), "no global on a failed registration")
}

// TestRegisterPluginServiceRejectsNilDescriptor asserts a nil descriptor fails at
// build rather than panicking or deferring to call time.
func TestRegisterPluginServiceRejectsNilDescriptor(t *testing.T) {
	L := lua.NewState()
	defer L.Close()
	err := luabridge.RegisterPluginService(L, nil, nil, "echo-bot")
	require.Error(t, err)
}

// streamingOnlyServiceDescriptor builds a service whose single method is
// server-streaming, used to prove streaming methods are skipped and a service
// left with zero unary methods fails early.
func streamingOnlyServiceDescriptor(t *testing.T) protoreflect.ServiceDescriptor {
	t.Helper()
	strKind := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	optLabel := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	fdp := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test/streamonly/v1/s.proto"),
		Package: proto.String("test.streamonly.v1"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("Req"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("message"), Number: proto.Int32(1), Type: strKind, Label: optLabel, JsonName: proto.String("message")},
				},
			},
			{
				Name: proto.String("Resp"),
				Field: []*descriptorpb.FieldDescriptorProto{
					{Name: proto.String("reply"), Number: proto.Int32(1), Type: strKind, Label: optLabel, JsonName: proto.String("reply")},
				},
			},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{
			{
				Name: proto.String("StreamOnly"),
				Method: []*descriptorpb.MethodDescriptorProto{
					{
						Name:            proto.String("Watch"),
						InputType:       proto.String(".test.streamonly.v1.Req"),
						OutputType:      proto.String(".test.streamonly.v1.Resp"),
						ServerStreaming: proto.Bool(true),
					},
				},
			},
		},
	}
	fd, err := protodesc.NewFile(fdp, nil)
	require.NoError(t, err)
	return fd.Services().Get(0)
}
