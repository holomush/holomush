// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package approval

import (
	"context"

	"connectrpc.com/connect"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// RoleHasher is the narrow surface ApproveHandler needs from store.RoleStore.
// Mirrors the same shape used by adminauth.PlayerRoleHasher (T17). Defined
// per-package to avoid cross-package coupling.
type RoleHasher interface {
	PlayerHasRole(ctx context.Context, playerID, role string) (bool, error)
}

// ApproveHandler is the ConnectRPC handler for AdminService.Approve.
type ApproveHandler struct {
	sessions  adminauth.SessionStore
	repo      Repo
	grants    access.SubjectResolver
	roleStore RoleHasher
}

// NewApproveHandler constructs the handler with explicit dependencies.
func NewApproveHandler(s adminauth.SessionStore, r Repo, g access.SubjectResolver, rh RoleHasher) *ApproveHandler {
	return &ApproveHandler{sessions: s, repo: r, grants: g, roleStore: rh}
}

// Approve is the AdminService.Approve RPC entry point. It resolves the
// session_token, re-asserts capability + role (defense-in-depth per
// INV-D16), then calls Repo.MarkApproved which atomically rejects
// self-approval, expired rows, and already-approved rows.
func (h *ApproveHandler) Approve(
	ctx context.Context,
	req *connect.Request[adminv1.ApproveRequest],
) (*connect.Response[adminv1.ApproveResponse], error) {
	identity, err := h.sessions.Get(req.Msg.GetSessionToken())
	if err != nil {
		return nil, adminauth.MapDenyToConnect(err) //nolint:wrapcheck // MapDenyToConnect produces *connect.Error; wrapping again would hide the ConnectRPC code
	}

	hasCap, err := access.HasPlayerGrant(ctx, h.grants, identity.PlayerID, access.CapabilityCryptoOperator)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, oops.Wrap(err))
	}
	if !hasCap {
		return nil, adminauth.MapDenyToConnect(oops.Code("DENY_NOT_OPERATOR"). //nolint:wrapcheck // MapDenyToConnect produces *connect.Error; wrapping again would hide the ConnectRPC code
									With("player_id", identity.PlayerID).Errorf("crypto.operator capability absent"))
	}

	hasRole, err := h.roleStore.PlayerHasRole(ctx, identity.PlayerID, access.RoleAdmin)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, oops.Wrap(err))
	}
	if !hasRole {
		return nil, adminauth.MapDenyToConnect(oops.Code("DENY_NOT_ADMIN_ROLE"). //nolint:wrapcheck // MapDenyToConnect produces *connect.Error; wrapping again would hide the ConnectRPC code
									With("player_id", identity.PlayerID).Errorf("admin role absent"))
	}

	if len(req.Msg.GetRequestId()) != 16 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			oops.Code("APPROVE_INVALID_REQUEST_ID").Errorf("request_id MUST be a 16-byte ULID"))
	}
	var rid RequestID
	copy(rid[:], req.Msg.GetRequestId())

	if err := h.repo.MarkApproved(ctx, rid, identity.PlayerID); err != nil {
		return nil, adminauth.MapDenyToConnect(err) //nolint:wrapcheck // MapDenyToConnect produces *connect.Error; wrapping again would hide the ConnectRPC code
	}
	return connect.NewResponse(&adminv1.ApproveResponse{}), nil
}
