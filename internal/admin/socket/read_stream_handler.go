// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// AdminReadStreamConnectHandler embeds an UnimplementedAdminServiceHandler
// and overrides only AdminReadStream to delegate to the injected
// ReadStreamRPCHandler. Used for tests that want to mount just AdminReadStream
// without standing up the full admin service.
type AdminReadStreamConnectHandler struct {
	adminv1connect.UnimplementedAdminServiceHandler
	inner ReadStreamRPCHandler
}

// NewAdminReadStreamConnectHandler constructs the adapter.
func NewAdminReadStreamConnectHandler(h ReadStreamRPCHandler) *AdminReadStreamConnectHandler {
	return &AdminReadStreamConnectHandler{inner: h}
}

// AdminReadStream delegates to the injected ReadStreamRPCHandler, or returns
// connect.CodeUnimplemented when the handler is nil.
func (a *AdminReadStreamConnectHandler) AdminReadStream(
	ctx context.Context,
	req *connect.Request[adminv1.AdminReadStreamRequest],
	stream *connect.ServerStream[adminv1.AdminReadStreamResponse],
) error {
	if a.inner == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("AdminReadStream: handler nil"))
	}
	return a.inner.HandleAdminReadStream(ctx, req, stream) //nolint:wrapcheck // handler returns *connect.Error; wrapping would discard the ConnectRPC code
}
