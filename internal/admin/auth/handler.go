// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth

import (
	"context"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/holomush/holomush/internal/admin/socket"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// denyCodeToConnect maps typed DENY oops codes to ConnectRPC codes.
// Codes not in this map → connect.CodeInternal (unexpected error).
var denyCodeToConnect = map[string]connect.Code{
	"DENY_INVALID_CREDENTIALS":       connect.CodeUnauthenticated,
	"DENY_NOT_ENROLLED":              connect.CodeFailedPrecondition,
	"DENY_BAD_TOTP":                  connect.CodeUnauthenticated,
	"DENY_LOCKED":                    connect.CodeUnavailable,
	"DENY_NOT_OPERATOR":              connect.CodePermissionDenied,
	"DENY_NOT_ADMIN_ROLE":            connect.CodePermissionDenied,
	"DENY_SESSION_INVALID":           connect.CodeUnauthenticated,
	"DENY_SESSION_EXPIRED":           connect.CodeUnauthenticated,
	"DENY_DUAL_CONTROL_SELF":         connect.CodeFailedPrecondition,
	"DENY_DUAL_CONTROL_REQUIRED":     connect.CodeFailedPrecondition,
	"DENY_APPROVAL_ARGS_MISMATCH":    connect.CodeFailedPrecondition,
	"DENY_APPROVAL_EXPIRED":          connect.CodeFailedPrecondition,
	"DENY_APPROVAL_ALREADY_APPROVED": connect.CodeFailedPrecondition,
	"DENY_POLICY_HASH_UNKNOWN":       connect.CodeFailedPrecondition,
}

// AuthenticateHandler is the ConnectRPC handler for AdminService.Authenticate.
type AuthenticateHandler struct {
	provider OperatorAuthProvider
	sessions SessionStore
}

// NewAuthenticateHandler constructs a new AuthenticateHandler.
func NewAuthenticateHandler(p OperatorAuthProvider, s SessionStore) *AuthenticateHandler {
	return &AuthenticateHandler{provider: p, sessions: s}
}

// Authenticate is the AdminService.Authenticate RPC entry point.
func (h *AuthenticateHandler) Authenticate(
	ctx context.Context,
	req *connect.Request[adminv1.AuthenticateRequest],
) (*connect.Response[adminv1.AuthenticateResponse], error) {
	peer, _ := socket.PeerCredFromContext(ctx) // missing PeerCred is OK; zero value
	auth := AuthRequest{
		Username: req.Msg.GetUsername(),
		Password: req.Msg.GetPassword(),
		TOTPCode: req.Msg.GetTotpCode(),
		PeerCred: peer,
	}
	identity, err := h.provider.Authenticate(ctx, auth)
	if err != nil {
		return nil, MapDenyToConnect(err)
	}
	token, expiresAt, err := h.sessions.Issue(identity)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, oops.Wrap(err))
	}
	return connect.NewResponse(&adminv1.AuthenticateResponse{
		SessionToken: token,
		ExpiresAt:    timestamppb.New(expiresAt),
		PlayerId:     identity.PlayerID,
	}), nil
}

// MapDenyToConnect translates a typed oops error into a ConnectRPC error,
// preserving the original error in the chain so errutil.AssertErrorCode
// still works in tests. Exported so T16's Approve handler and T17's
// ResetTOTP handler can share it.
func MapDenyToConnect(err error) error {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return connect.NewError(connect.CodeInternal, err)
	}
	// OopsError.Code() returns any; assert to string for the map lookup.
	codeStr, ok := oopsErr.Code().(string)
	if !ok {
		return connect.NewError(connect.CodeInternal, err)
	}
	connectCode, ok := denyCodeToConnect[codeStr]
	if !ok {
		return connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewError(connectCode, err)
}
