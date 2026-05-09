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

// PlayerRoleHasher is the narrow surface for INV-D16 role re-check.
// Mirrors approval.RoleHasher per-package to avoid cross-package coupling.
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
// session, re-asserts capability + role (INV-D16), validates the
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

	hasCap, err := access.HasPlayerGrant(ctx, h.grants, identity.PlayerID, access.CapabilityCryptoOperator)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, oops.Wrap(err))
	}
	if !hasCap {
		return nil, MapDenyToConnect(oops.Code("DENY_NOT_OPERATOR").
			With("player_id", identity.PlayerID).Errorf("crypto.operator capability absent"))
	}

	hasRole, err := h.roleStore.PlayerHasRole(ctx, identity.PlayerID, access.RoleAdmin)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, oops.Wrap(err))
	}
	if !hasRole {
		return nil, MapDenyToConnect(oops.Code("DENY_NOT_ADMIN_ROLE").
			With("player_id", identity.PlayerID).Errorf("admin role absent"))
	}

	targetID, err := ulid.Parse(req.Msg.GetTargetPlayerId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			oops.Code("RESET_INVALID_TARGET_PID").
				With("target_player_id", req.Msg.GetTargetPlayerId()).
				Errorf("target_player_id MUST be a ULID: %w", err))
	}

	res, err := h.totpSvc.ClearTOTP(ctx, targetID, totp.ClearReasonAdminReset)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, oops.Wrap(err))
	}
	return connect.NewResponse(&adminv1.ResetTOTPResponse{Cleared: res.WasEnrolled}), nil
}
