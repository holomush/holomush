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

// WebAdminListCharacters proxies AdminPortalService.AdminListCharacters.
//
// Same five-part shape as its section peers: nil-client guard, token from the
// HEADER, bounded context, field-by-field forward, log-then-pass-through. It
// computes NOTHING — in particular it does not clamp page_size and does not
// validate the sort field, because both live core-side where a caller speaking
// gRPC to core directly cannot skip them. A clamp here would be decoration.
func (h *Handler) WebAdminListCharacters(
	ctx context.Context,
	req *connect.Request[webv1.WebAdminListCharactersRequest],
) (*connect.Response[webv1.WebAdminListCharactersResponse], error) {
	slog.DebugContext(ctx, "web: WebAdminListCharacters")
	if h.adminPortal == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("admin portal client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.adminPortal.AdminListCharacters(rpcCtx, &adminportalv1.AdminListCharactersRequest{
		PlayerSessionToken: token,
		SortField:          req.Msg.GetSortField(),
		Descending:         req.Msg.GetDescending(),
		StatusFilter:       req.Msg.GetStatusFilter(),
		PlayerId:           req.Msg.GetPlayerId(),
		Page:               req.Msg.GetPage(),
		PageSize:           req.Msg.GetPageSize(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: admin list characters RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebAdminListCharactersResponse{
		Characters: resp.GetCharacters(),
		TotalCount: resp.GetTotalCount(),
	}), nil
}

// WebAdminSearchCharacters proxies AdminPortalService.AdminSearchCharacters.
//
// `query` is forwarded BYTE-FOR-BYTE. The gateway does not trim it, lower-case
// it or NFKC-fold it: normalization is core-side through the single charname
// pipeline that produced the stored normal form, and a mirror of it here would
// be a second definition of name equality that could drift from the column it
// is matched against.
func (h *Handler) WebAdminSearchCharacters(
	ctx context.Context,
	req *connect.Request[webv1.WebAdminSearchCharactersRequest],
) (*connect.Response[webv1.WebAdminSearchCharactersResponse], error) {
	slog.DebugContext(ctx, "web: WebAdminSearchCharacters")
	if h.adminPortal == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("admin portal client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.adminPortal.AdminSearchCharacters(rpcCtx, &adminportalv1.AdminSearchCharactersRequest{
		PlayerSessionToken: token,
		SortField:          req.Msg.GetSortField(),
		Descending:         req.Msg.GetDescending(),
		StatusFilter:       req.Msg.GetStatusFilter(),
		PlayerId:           req.Msg.GetPlayerId(),
		Page:               req.Msg.GetPage(),
		PageSize:           req.Msg.GetPageSize(),
		Query:              req.Msg.GetQuery(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: admin search characters RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebAdminSearchCharactersResponse{
		Characters: resp.GetCharacters(),
		TotalCount: resp.GetTotalCount(),
	}), nil
}

// WebAdminGetCharacter proxies AdminPortalService.AdminGetCharacter.
//
// It forwards character_id verbatim and returns the detail message unmodified,
// including its closed twelve-key profile map. It does not parse or validate
// the id: the core answers an unparseable and an absent id identically, and a
// gateway-side parse would split those two answers apart again.
func (h *Handler) WebAdminGetCharacter(
	ctx context.Context,
	req *connect.Request[webv1.WebAdminGetCharacterRequest],
) (*connect.Response[webv1.WebAdminGetCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebAdminGetCharacter")
	if h.adminPortal == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("admin portal client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.adminPortal.AdminGetCharacter(rpcCtx, &adminportalv1.AdminGetCharacterRequest{
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: admin get character RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebAdminGetCharacterResponse{
		Character: resp.GetCharacter(),
	}), nil
}

// WebAdminUpdateCharacter proxies AdminPortalService.AdminUpdateCharacter.
//
// It forwards the mask, the thirteen values and the expected_version verbatim
// and COMPUTES NOTHING. The closed exact-string allowlist, the byte caps, the
// version guard and the section gate all live core-side, where a caller speaking
// gRPC directly cannot skip them — a gateway-side copy of any of them would be a
// second definition that can drift, and the one that matters is the one a direct
// caller reaches.
func (h *Handler) WebAdminUpdateCharacter(
	ctx context.Context,
	req *connect.Request[webv1.WebAdminUpdateCharacterRequest],
) (*connect.Response[webv1.WebAdminUpdateCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebAdminUpdateCharacter")
	if h.adminPortal == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("admin portal client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.adminPortal.AdminUpdateCharacter(rpcCtx, &adminportalv1.AdminUpdateCharacterRequest{
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		ExpectedVersion:    req.Msg.GetExpectedVersion(),
		UpdateMask:         req.Msg.GetUpdateMask(),
		Description:        req.Msg.GetDescription(),
		Pronouns:           req.Msg.GetPronouns(),
		Concept:            req.Msg.GetConcept(),
		Species:            req.Msg.GetSpecies(),
		Age:                req.Msg.GetAge(),
		Faction:            req.Msg.GetFaction(),
		Currently:          req.Msg.GetCurrently(),
		Timezone:           req.Msg.GetTimezone(),
		Appearance:         req.Msg.GetAppearance(),
		Personality:        req.Msg.GetPersonality(),
		Biography:          req.Msg.GetBiography(),
		Rumors:             req.Msg.GetRumors(),
		RpPreferences:      req.Msg.GetRpPreferences(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: admin update character RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebAdminUpdateCharacterResponse{
		Character: resp.GetCharacter(),
	}), nil
}

// WebAdminRetireCharacter proxies AdminPortalService.AdminRetireCharacter.
//
// The core's Aborted for a stale version and FailedPrecondition for an
// already-retired character both reach the browser unmodified; the gateway
// neither retries nor re-reads, because a retry here would reintroduce
// last-write-wins one layer up from the guard that exists to prevent it.
func (h *Handler) WebAdminRetireCharacter(
	ctx context.Context,
	req *connect.Request[webv1.WebAdminRetireCharacterRequest],
) (*connect.Response[webv1.WebAdminRetireCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebAdminRetireCharacter")
	if h.adminPortal == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("admin portal client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.adminPortal.AdminRetireCharacter(rpcCtx, &adminportalv1.AdminRetireCharacterRequest{
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		ExpectedVersion:    req.Msg.GetExpectedVersion(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: admin retire character RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebAdminRetireCharacterResponse{
		Character: resp.GetCharacter(),
	}), nil
}

// WebAdminUnretireCharacter proxies AdminPortalService.AdminUnretireCharacter,
// with the same forward-verbatim shape its retire peer has.
//
// There is no WebAdminDeleteCharacter, because no admin delete RPC exists to
// proxy: retire is the only admin-reachable lifecycle exit and it is reversible.
func (h *Handler) WebAdminUnretireCharacter(
	ctx context.Context,
	req *connect.Request[webv1.WebAdminUnretireCharacterRequest],
) (*connect.Response[webv1.WebAdminUnretireCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebAdminUnretireCharacter")
	if h.adminPortal == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("admin portal client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.adminPortal.AdminUnretireCharacter(rpcCtx, &adminportalv1.AdminUnretireCharacterRequest{
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		ExpectedVersion:    req.Msg.GetExpectedVersion(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: admin unretire character RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebAdminUnretireCharacterResponse{
		Character: resp.GetCharacter(),
	}), nil
}
