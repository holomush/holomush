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

// RekeyRPCHandler is the narrow surface the socket server needs from the
// RekeyConnectHandler (holomush-jxo8.7.28, holomush-jxo8.7.29,
// holomush-jxo8.7.30). When nil, all Rekey RPCs return
// connect.CodeUnimplemented.
type RekeyRPCHandler interface {
	HandleRekey(ctx context.Context, req *connect.Request[adminv1.RekeyRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error
	HandleRekeyResume(ctx context.Context, req *connect.Request[adminv1.RekeyResumeRequest], stream *connect.ServerStream[adminv1.RekeyProgress]) error
	HandleRekeyAbort(ctx context.Context, req *connect.Request[adminv1.RekeyAbortRequest]) (*connect.Response[adminv1.RekeyAbortResponse], error)
	HandleRekeyStatus(ctx context.Context, req *connect.Request[adminv1.RekeyStatusRequest]) (*connect.Response[adminv1.RekeyStatusResponse], error)
	HandleRekeyList(ctx context.Context, req *connect.Request[adminv1.RekeyListRequest], stream *connect.ServerStream[adminv1.RekeyStatusResponse]) error
}

// ReadStreamRPCHandler is the narrow surface the socket needs from the
// AdminReadStream handler (holomush-jxo8.8.36). When nil, AdminReadStream
// returns connect.CodeUnimplemented. R.15 will supply the real implementation.
type ReadStreamRPCHandler interface {
	HandleAdminReadStream(
		ctx context.Context,
		req *connect.Request[adminv1.AdminReadStreamRequest],
		stream *connect.ServerStream[adminv1.AdminReadStreamResponse],
	) error
}
