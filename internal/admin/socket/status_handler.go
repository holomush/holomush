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

// compositeHandler implements adminv1connect.AdminServiceHandler. It serves
// Status directly and delegates Authenticate, Approve, ResetTOTP, and Rekey
// to optional per-RPC handlers registered in Config. When a handler is nil the
// RPC returns connect.CodeUnimplemented, preserving backward compatibility for
// callers that do not register all handlers.
type compositeHandler struct {
	version             string
	authenticateHandler AuthenticateHandler
	approveHandler      ApproveHandler
	resetTOTPHandler    ResetTOTPHandler
	rekeyHandler        RekeyRPCHandler
}

// Compile-time assertion: compositeHandler satisfies the generated interface.
var _ adminv1connect.AdminServiceHandler = (*compositeHandler)(nil)

// Status returns the admin socket's liveness state and binary version.
func (h *compositeHandler) Status(
	_ context.Context,
	_ *connect.Request[adminv1.StatusRequest],
) (*connect.Response[adminv1.StatusResponse], error) {
	return connect.NewResponse(&adminv1.StatusResponse{
		Version: h.version,
		Healthy: true,
	}), nil
}

// Authenticate delegates to the registered AuthenticateHandler, or returns
// Unimplemented if none was provided.
func (h *compositeHandler) Authenticate(
	ctx context.Context,
	req *connect.Request[adminv1.AuthenticateRequest],
) (*connect.Response[adminv1.AuthenticateResponse], error) {
	if h.authenticateHandler == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("Authenticate not registered"))
	}
	return h.authenticateHandler.Authenticate(ctx, req) //nolint:wrapcheck // handler returns *connect.Error; wrapping would discard the ConnectRPC code
}

// Approve delegates to the registered ApproveHandler, or returns
// Unimplemented if none was provided.
func (h *compositeHandler) Approve(
	ctx context.Context,
	req *connect.Request[adminv1.ApproveRequest],
) (*connect.Response[adminv1.ApproveResponse], error) {
	if h.approveHandler == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("Approve not registered"))
	}
	return h.approveHandler.Approve(ctx, req) //nolint:wrapcheck // handler returns *connect.Error; wrapping would discard the ConnectRPC code
}

// ResetTOTP delegates to the registered ResetTOTPHandler, or returns
// Unimplemented if none was provided.
func (h *compositeHandler) ResetTOTP(
	ctx context.Context,
	req *connect.Request[adminv1.ResetTOTPRequest],
) (*connect.Response[adminv1.ResetTOTPResponse], error) {
	if h.resetTOTPHandler == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("ResetTOTP not registered"))
	}
	return h.resetTOTPHandler.ResetTOTP(ctx, req) //nolint:wrapcheck // handler returns *connect.Error; wrapping would discard the ConnectRPC code
}

// Rekey delegates to the registered RekeyRPCHandler, or returns Unimplemented
// if none was provided. (holomush-jxo8.7.28)
func (h *compositeHandler) Rekey(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyRequest],
	stream *connect.ServerStream[adminv1.RekeyProgress],
) error {
	if h.rekeyHandler == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("Rekey not registered"))
	}
	return h.rekeyHandler.HandleRekey(ctx, req, stream) //nolint:wrapcheck // handler returns *connect.Error; wrapping would discard the ConnectRPC code
}

// RekeyResume delegates to the registered RekeyRPCHandler, or returns
// Unimplemented if none was provided. (holomush-jxo8.7.28)
func (h *compositeHandler) RekeyResume(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyResumeRequest],
	stream *connect.ServerStream[adminv1.RekeyProgress],
) error {
	if h.rekeyHandler == nil {
		return connect.NewError(connect.CodeUnimplemented, errors.New("RekeyResume not registered"))
	}
	return h.rekeyHandler.HandleRekeyResume(ctx, req, stream) //nolint:wrapcheck // handler returns *connect.Error; wrapping would discard the ConnectRPC code
}

// RekeyAbort delegates to the registered RekeyRPCHandler (holomush-jxo8.7.29),
// or returns Unimplemented if none was provided.
func (h *compositeHandler) RekeyAbort(
	ctx context.Context,
	req *connect.Request[adminv1.RekeyAbortRequest],
) (*connect.Response[adminv1.RekeyAbortResponse], error) {
	if h.rekeyHandler == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, errors.New("RekeyAbort not registered"))
	}
	return h.rekeyHandler.HandleRekeyAbort(ctx, req) //nolint:wrapcheck // handler returns *connect.Error; wrapping would discard the ConnectRPC code
}

// RekeyStatus is implemented by the rekey handler (holomush-jxo8.7.30). Returns
// Unimplemented until the handler is registered via Config.
func (h *compositeHandler) RekeyStatus(
	_ context.Context,
	_ *connect.Request[adminv1.RekeyStatusRequest],
) (*connect.Response[adminv1.RekeyStatusResponse], error) {
	return nil, connect.NewError(connect.CodeUnimplemented, errors.New("RekeyStatus not registered"))
}

// RekeyList is implemented by the rekey handler (holomush-jxo8.7.30). Returns
// Unimplemented until the handler is registered via Config.
func (h *compositeHandler) RekeyList(
	_ context.Context,
	_ *connect.Request[adminv1.RekeyListRequest],
	_ *connect.ServerStream[adminv1.RekeyStatusResponse],
) error {
	return connect.NewError(connect.CodeUnimplemented, errors.New("RekeyList not registered"))
}
