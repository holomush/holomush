// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins

import (
	"errors"
	"io"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCServiceProxy forwards gRPC calls for plugin-provided services
// to the service registry. Install as a grpc.UnknownServiceHandler.
type GRPCServiceProxy struct {
	registry *ServiceRegistry
}

// NewGRPCServiceProxy creates a proxy that routes unknown gRPC methods
// to plugin-provided services via the registry.
func NewGRPCServiceProxy(registry *ServiceRegistry) *GRPCServiceProxy {
	return &GRPCServiceProxy{registry: registry}
}

// Handler returns a grpc.ServerOption that installs this proxy as the
// unknown service handler on a gRPC server.
func (p *GRPCServiceProxy) Handler() grpc.ServerOption {
	return grpc.UnknownServiceHandler(p.streamHandler)
}

func (p *GRPCServiceProxy) streamHandler(_ interface{}, stream grpc.ServerStream) error {
	method, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Error(codes.Internal, "failed to get method from stream") //nolint:wrapcheck // transparent gRPC proxy returns status errors directly
	}

	serviceName := extractServiceName(method)
	if serviceName == "" {
		return status.Errorf(codes.Unimplemented, "unknown method %s", method)
	}

	svc, err := p.registry.Resolve(serviceName)
	if err != nil {
		return status.Errorf(codes.Unimplemented, "unknown service %s", serviceName)
	}

	if svc.IsServerInternal() {
		return status.Errorf(codes.Unavailable, "service %s is server-internal", serviceName)
	}

	if svc.Conn == nil {
		return status.Errorf(codes.Unavailable, "service %s has no connection", serviceName)
	}

	if svc.Health != nil && !svc.Health.Healthy() {
		return status.Errorf(codes.Unavailable, "service %s is unhealthy", serviceName)
	}

	// Forward the unary or streaming call to the plugin
	clientStream, streamErr := svc.Conn.NewStream(
		stream.Context(),
		&grpc.StreamDesc{ServerStreams: true, ClientStreams: true},
		method,
	)
	if streamErr != nil {
		slog.Error("failed to create proxy stream", "service", serviceName, "error", streamErr)
		return status.Errorf(codes.Internal, "service temporarily unavailable")
	}

	return ProxyStreams(stream, clientStream)
}

// extractServiceName extracts "package.Service" from "/package.Service/Method".
func extractServiceName(fullMethod string) string {
	if !strings.HasPrefix(fullMethod, "/") {
		return ""
	}
	parts := strings.SplitN(fullMethod[1:], "/", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

// ProxyStreams bidirectionally proxies between a server stream and client stream.
// NOTE: This implementation only supports unary RPCs safely. For client-streaming
// or bidi-streaming RPCs, the goroutine blocked on srv.RecvMsg could deadlock if
// the backend closes early. All current plugin services use unary RPCs.
func ProxyStreams(srv grpc.ServerStream, cli grpc.ClientStream) error {
	// Forward client→server (request) in a goroutine
	errCh := make(chan error, 1)
	go func() {
		for {
			msg := &RawMessage{}
			if err := srv.RecvMsg(msg); err != nil {
				_ = cli.CloseSend() //nolint:errcheck // best-effort close on proxy teardown
				errCh <- nil        // EOF or error from client side
				return
			}
			if err := cli.SendMsg(msg); err != nil {
				errCh <- err
				return
			}
		}
	}()

	// Forward server→client (response) in main goroutine
	for {
		msg := &RawMessage{}
		if err := cli.RecvMsg(msg); err != nil {
			<-errCh
			if errors.Is(err, io.EOF) {
				return nil // backend finished sending — normal completion
			}
			return err //nolint:wrapcheck // transparent gRPC proxy forwards errors as-is
		}
		if err := srv.SendMsg(msg); err != nil {
			<-errCh
			return err //nolint:wrapcheck // transparent gRPC proxy forwards errors as-is
		}
	}
}

// RawMessage is a pass-through protobuf message for gRPC proxying.
// It stores raw bytes without deserialization.
type RawMessage struct {
	data []byte
}

// Marshal returns the raw bytes.
func (m *RawMessage) Marshal() ([]byte, error) { return m.data, nil }

// Unmarshal stores raw bytes without deserialization.
func (m *RawMessage) Unmarshal(b []byte) error { m.data = b; return nil }

// ProtoMessage marks RawMessage as a proto.Message.
func (m *RawMessage) ProtoMessage() {}

// Reset clears the stored bytes.
func (m *RawMessage) Reset() { m.data = nil }

// String returns the raw bytes as a string.
func (m *RawMessage) String() string { return string(m.data) }
