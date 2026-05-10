// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package socket

import (
	"context"

	"connectrpc.com/connect"

	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// AuthenticateHandler is the narrow surface the socket server needs from
// the adminauth.AuthenticateHandler. Decoupling here avoids importing
// the full adminauth package into socket, which would create a layering
// concern for future restructure.
type AuthenticateHandler interface {
	Authenticate(ctx context.Context, req *connect.Request[adminv1.AuthenticateRequest]) (*connect.Response[adminv1.AuthenticateResponse], error)
}

// ApproveHandler is the narrow surface the socket server needs from
// the approval.ApproveHandler.
type ApproveHandler interface {
	Approve(ctx context.Context, req *connect.Request[adminv1.ApproveRequest]) (*connect.Response[adminv1.ApproveResponse], error)
}

// ResetTOTPHandler is the narrow surface the socket server needs from
// the adminauth.ResetTOTPHandler.
type ResetTOTPHandler interface {
	ResetTOTP(ctx context.Context, req *connect.Request[adminv1.ResetTOTPRequest]) (*connect.Response[adminv1.ResetTOTPResponse], error)
}
