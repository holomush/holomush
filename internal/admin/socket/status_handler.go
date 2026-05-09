// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"

	"connectrpc.com/connect"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
	"github.com/holomush/holomush/pkg/proto/holomush/admin/v1/adminv1connect"
)

// statusHandler implements adminv1connect.AdminServiceHandler.
type statusHandler struct {
	version string
}

// Ensure statusHandler satisfies the generated interface at compile time.
var _ adminv1connect.AdminServiceHandler = (*statusHandler)(nil)

// Status returns the admin socket's liveness state and binary version.
func (h *statusHandler) Status(
	_ context.Context,
	_ *connect.Request[adminv1.StatusRequest],
) (*connect.Response[adminv1.StatusResponse], error) {
	return connect.NewResponse(&adminv1.StatusResponse{
		Version: h.version,
		Healthy: true,
	}), nil
}
