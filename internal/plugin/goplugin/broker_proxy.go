// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package goplugin

import (
	"log/slog"
	"time"

	plugins "github.com/holomush/holomush/internal/plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewBrokerProxy creates a gRPC server factory compatible with
// broker.AcceptAndServe. The returned server transparently proxies all
// RPCs to the given connection using an UnknownServiceHandler.
func NewBrokerProxy(conn grpc.ClientConnInterface, pluginName string) func([]grpc.ServerOption) *grpc.Server {
	return func(opts []grpc.ServerOption) *grpc.Server {
		handler := grpc.UnknownServiceHandler(func(_ interface{}, stream grpc.ServerStream) error {
			method, ok := grpc.MethodFromServerStream(stream)
			if !ok {
				return status.Error(codes.Internal, "failed to get method from stream")
			}

			if conn == nil {
				return status.Errorf(codes.Unavailable, "no connection for proxied service")
			}

			start := time.Now()
			clientStream, err := conn.NewStream(
				stream.Context(),
				&grpc.StreamDesc{ServerStreams: true, ClientStreams: true},
				method,
			)
			if err != nil {
				slog.Error("broker proxy: failed to create upstream stream",
					"plugin", pluginName, "method", method, "error", err)
				return status.Errorf(codes.Internal, "broker proxy: upstream unavailable")
			}

			proxyErr := plugins.ProxyStreams(stream, clientStream)

			slog.Debug("broker proxy: call completed",
				"plugin", pluginName,
				"method", method,
				"duration", time.Since(start),
				"error", proxyErr,
			)

			return proxyErr //nolint:wrapcheck // transparent gRPC proxy forwards errors as-is
		})

		allOpts := append([]grpc.ServerOption{handler}, opts...)
		return grpc.NewServer(allOpts...)
	}
}
