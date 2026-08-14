// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
	adminportalv1 "github.com/holomush/holomush/pkg/proto/holomush/adminportal/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// WebAdminListSections proxies AdminPortalService.AdminListSections.
//
// It copies WebGetCharacterProfile's shape exactly: nil-client guard, token
// from the HEADER, bounded context, field-by-field forward, log-then-pass-
// through on error. It computes nothing, and no authorization decision may ever
// enter this file.
//
// # Why the gateway does not decide
//
// The admin gate is core-side, in the AdminPortalService interceptor. A
// decision here would live in the wrong process — .claude/rules/gateway-boundary.md
// designates internal/web/ protocol-translation-only — and would be bypassable
// by any caller who speaks gRPC to core directly, which is exactly the threat
// the core-side gate answers. So a non-admin's PermissionDenied is FORWARDED
// unmodified rather than turned into an empty list: the browser's /admin route
// renders its not-found page off that denial, and softening it here would
// delete the signal.
func (h *Handler) WebAdminListSections(
	ctx context.Context,
	req *connect.Request[webv1.WebAdminListSectionsRequest],
) (*connect.Response[webv1.WebAdminListSectionsResponse], error) {
	slog.DebugContext(ctx, "web: WebAdminListSections")
	if h.adminPortal == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("admin portal client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.adminPortal.AdminListSections(rpcCtx, &adminportalv1.AdminListSectionsRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: admin list sections RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebAdminListSectionsResponse{
		Sections: resp.GetSections(),
	}), nil
}
