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

// ApproveHandler is the ConnectRPC handler for AdminService.Approve.
type ApproveHandler struct {
	sessions  adminauth.SessionStore
	repo      Repo
	grants    access.SubjectResolver
	roleStore adminauth.PlayerRoleHasher
}

// NewApproveHandler constructs the handler with explicit dependencies.
// roleStore is the canonical adminauth.PlayerRoleHasher (the per-package
// duplicate previously defined here was collapsed when AssertOperatorAdmin
// was extracted).
func NewApproveHandler(s adminauth.SessionStore, r Repo, g access.SubjectResolver, rh adminauth.PlayerRoleHasher) *ApproveHandler {
	return &ApproveHandler{sessions: s, repo: r, grants: g, roleStore: rh}
}

// Approve is the AdminService.Approve RPC entry point. It resolves the
// session_token, re-asserts capability + role (defense-in-depth per
// INV-CRYPTO-83) via adminauth.AssertOperatorAdmin, then calls Repo.MarkApproved
// which atomically rejects self-approval, expired rows, and already-approved
// rows.
func (h *ApproveHandler) Approve(
	ctx context.Context,
	req *connect.Request[adminv1.ApproveRequest],
) (*connect.Response[adminv1.ApproveResponse], error) {
	identity, err := h.sessions.Get(req.Msg.GetSessionToken())
	if err != nil {
		return nil, adminauth.MapDenyToConnect(err) //nolint:wrapcheck // MapDenyToConnect produces *connect.Error; wrapping again would hide the ConnectRPC code
	}

	if err = adminauth.AssertOperatorAdmin(ctx, h.grants, h.roleStore, identity.PlayerID); err != nil {
		return nil, adminauth.MapDenyToConnect(err) //nolint:wrapcheck // MapDenyToConnect produces *connect.Error; wrapping again would hide the ConnectRPC code
	}

	if len(req.Msg.GetRequestId()) != 16 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			oops.Code("APPROVE_INVALID_REQUEST_ID").Errorf("request_id must be a non-zero 16-byte ULID"))
	}
	var rid RequestID
	copy(rid[:], req.Msg.GetRequestId())
	// All-zero request_id is never a valid ULID and is the trivial
	// forgery shape — reject explicitly so callers cannot use it as a
	// sentinel that round-trips through the repo layer.
	if rid.IsZero() {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			oops.Code("APPROVE_INVALID_REQUEST_ID").Errorf("request_id must be a non-zero 16-byte ULID"))
	}

	if err = h.repo.MarkApproved(ctx, rid, identity.PlayerID); err != nil {
		return nil, adminauth.MapDenyToConnect(err) //nolint:wrapcheck // MapDenyToConnect produces *connect.Error; wrapping again would hide the ConnectRPC code
	}
	return connect.NewResponse(&adminv1.ApproveResponse{}), nil
}
