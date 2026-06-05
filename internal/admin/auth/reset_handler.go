// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth

import (
	"context"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/totp"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// ClearTOTPCaller is the narrow surface ResetTOTPHandler needs from
// totpaudit.AuditingService. Any implementor that emits the
// crypto.totp_cleared audit event on success is acceptable; the production
// wire injects an *totpaudit.AuditingService.
type ClearTOTPCaller interface {
	ClearTOTP(ctx context.Context, playerID ulid.ULID, by totp.ClearReason) (totp.ClearResult, error)
}

// PlayerRoleHasher is the narrow surface for INV-CRYPTO-83 role re-check.
// Used by AssertOperatorAdmin and by the Approve / ResetTOTP handlers,
// which import this canonical definition from this package (the previous
// per-package duplicate in approval/handler.go was collapsed when
// AssertOperatorAdmin was extracted).
type PlayerRoleHasher interface {
	PlayerHasRole(ctx context.Context, playerID, role string) (bool, error)
}

// ResetTOTPHandler is the ConnectRPC handler for AdminService.ResetTOTP.
type ResetTOTPHandler struct {
	sessions  SessionStore
	grants    access.SubjectResolver
	roleStore PlayerRoleHasher
	totpSvc   ClearTOTPCaller
}

// NewResetTOTPHandler constructs the handler with explicit dependencies.
func NewResetTOTPHandler(s SessionStore, g access.SubjectResolver, rh PlayerRoleHasher, t ClearTOTPCaller) *ResetTOTPHandler {
	return &ResetTOTPHandler{sessions: s, grants: g, roleStore: rh, totpSvc: t}
}

// ResetTOTP is the AdminService.ResetTOTP RPC entry point. Resolves
// session, re-asserts capability + role (INV-CRYPTO-83), validates the
// target_player_id is a valid ULID, then calls AuditingService.ClearTOTP
// with ClearReasonAdminReset (which emits crypto.totp_cleared on success
// per T13). Response.Cleared = ClearResult.WasEnrolled (false if the
// call was a no-op because the player wasn't enrolled).
func (h *ResetTOTPHandler) ResetTOTP(
	ctx context.Context,
	req *connect.Request[adminv1.ResetTOTPRequest],
) (*connect.Response[adminv1.ResetTOTPResponse], error) {
	identity, err := h.sessions.Get(req.Msg.GetSessionToken())
	if err != nil {
		return nil, MapDenyToConnect(err)
	}

	if err = AssertOperatorAdmin(ctx, h.grants, h.roleStore, identity.PlayerID); err != nil {
		return nil, MapDenyToConnect(err)
	}

	targetID, err := ulid.Parse(req.Msg.GetTargetPlayerId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			oops.Code("RESET_INVALID_TARGET_PID").
				With("target_player_id", req.Msg.GetTargetPlayerId()).
				Errorf("target_player_id MUST be a ULID: %w", err))
	}
	// Reject the zero ULID — ulid.Parse accepts the all-zero shape, but
	// 00000000000000000000000000 is a sentinel and must never identify
	// a real player. Same defensive shape Approve uses for request_id.
	if targetID == (ulid.ULID{}) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			oops.Code("RESET_INVALID_TARGET_PID").
				With("target_player_id", req.Msg.GetTargetPlayerId()).
				Errorf("target_player_id MUST be a non-zero ULID"))
	}

	res, err := h.totpSvc.ClearTOTP(ctx, targetID, totp.ClearReasonAdminReset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, oops.Wrap(err))
	}
	return connect.NewResponse(&adminv1.ResetTOTPResponse{Cleared: res.WasEnrolled}), nil
}
