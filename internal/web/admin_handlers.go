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

// WebAdminGetSection proxies AdminPortalService.AdminGetSection.
//
// Same five-part shape as its sibling above: nil-client guard, token from the
// HEADER, bounded context, field-by-field forward, log-then-pass-through. It
// computes nothing and decides nothing — in particular it does NOT check the
// section id against any list, because the core interceptor gates the supplied
// id before the core handler runs and a gateway-side check would be both
// bypassable and in the wrong process.
//
// # Why this has no browser caller in v0.13, and is not dead code
//
// D-100 makes both registry RPCs published wire contract and census members:
// AdminGetSection is the single endpoint through which every one of the seven
// sections — including the six deferred ones — is reachable, gated, and
// refusing AFTER the gate, and the wire-level tests in test/integration/access
// are what exercise it. The /admin routes render their nav and their
// planned-section screens from the already-authorized WebAdminListSections
// payload, so nothing in the browser calls this yet. Do not delete it as unused.
func (h *Handler) WebAdminGetSection(
	ctx context.Context,
	req *connect.Request[webv1.WebAdminGetSectionRequest],
) (*connect.Response[webv1.WebAdminGetSectionResponse], error) {
	slog.DebugContext(ctx, "web: WebAdminGetSection")
	if h.adminPortal == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("admin portal client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.adminPortal.AdminGetSection(rpcCtx, &adminportalv1.AdminGetSectionRequest{
		SectionId:          req.Msg.GetSectionId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: admin get section RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebAdminGetSectionResponse{
		Section: resp.GetSection(),
	}), nil
}
