// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
	characteraccessv1 "github.com/holomush/holomush/pkg/proto/holomush/characteraccess/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// WebGetCharacterProfile proxies to CharacterAccessService.GetCharacterProfile.
// The gateway reads the player_session_token from the X-Session-Token cookie
// header (injected by CookieMiddleware) and forwards it with the character_id;
// it never reads a token from the request body, so a client cannot assert an
// identity the gateway did not authenticate.
//
// An ABSENT header is the ordinary logged-out case, not an error: the facade
// resolves the anonymous rung from an empty token and returns the public
// profile. Every visibility decision — reachability, per-attribute floors, the
// viewer rung itself — is owned by the facade; this handler computes nothing.
func (h *Handler) WebGetCharacterProfile(ctx context.Context, req *connect.Request[webv1.WebGetCharacterProfileRequest]) (*connect.Response[webv1.WebGetCharacterProfileResponse], error) {
	slog.DebugContext(ctx, "web: WebGetCharacterProfile", "character_id", req.Msg.GetCharacterId())
	if h.characterAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("character access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.characterAccess.GetCharacterProfile(rpcCtx, &characteraccessv1.GetCharacterProfileRequest{
		CharacterId:        req.Msg.GetCharacterId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: get character profile RPC failed", err, "character_id", req.Msg.GetCharacterId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebGetCharacterProfileResponse{
		Character: resp.GetCharacter(),
	}), nil
}

// The four owner-audience proxies below copy WebGetCharacterProfile's shape
// exactly: nil-client guard, token from the header, bounded context,
// field-by-field forward, log-then-pass-through on error. They compute nothing.
//
// All four facade handlers are LIVE (landed in plans 04-05 and 04-06):
// CharacterAccessServer.ListMyCharacters, GetMyCharacter,
// UpdateCharacterProfile, and UpdateCharacterDescription. The routing census
// (characterWebProxyRPCs in test/meta/characteraccess_routing_census_test.go)
// pins exactly these four as the owner-audience proxy set by set-equality, so
// this list cannot drift from the shipped handlers without failing that gate.
//
// The ONLY Unimplemented these proxies still produce is their own
// h.characterAccess == nil guard, and it means an UNWIRED CLIENT in
// cmd/holomush — never an unbuilt facade. An operator seeing CodeUnimplemented
// from one of these should look at gateway wiring.

// WebListMyCharacters proxies to CharacterAccessService.ListMyCharacters. The
// request message carries no fields at all — whose roster is returned follows
// entirely from the header token, so a client cannot name someone else's.
func (h *Handler) WebListMyCharacters(ctx context.Context, req *connect.Request[webv1.WebListMyCharactersRequest]) (*connect.Response[webv1.WebListMyCharactersResponse], error) {
	slog.DebugContext(ctx, "web: WebListMyCharacters")
	if h.characterAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("character access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.characterAccess.ListMyCharacters(rpcCtx, &characteraccessv1.ListMyCharactersRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list my characters RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebListMyCharactersResponse{
		Characters: resp.GetCharacters(),
	}), nil
}

// WebGetMyCharacter proxies to CharacterAccessService.GetMyCharacter. Ownership
// is verified in the facade against the token's player; the gateway forwards
// the id it was given and makes no ownership judgement of its own.
func (h *Handler) WebGetMyCharacter(ctx context.Context, req *connect.Request[webv1.WebGetMyCharacterRequest]) (*connect.Response[webv1.WebGetMyCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebGetMyCharacter", "character_id", req.Msg.GetCharacterId())
	if h.characterAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("character access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.characterAccess.GetMyCharacter(rpcCtx, &characteraccessv1.GetMyCharacterRequest{
		CharacterId:        req.Msg.GetCharacterId(),
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: get my character RPC failed", err, "character_id", req.Msg.GetCharacterId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebGetMyCharacterResponse{
		Character: resp.GetCharacter(),
	}), nil
}

// WebUpdateCharacterProfile proxies to
// CharacterAccessService.UpdateCharacterProfile. The mask and every prose field
// are forwarded VERBATIM: which paths are acceptable, and which fields they
// reach, is the facade's allowlist decision. A gateway that filtered the mask
// would be a second allowlist that can drift from the authoritative one.
func (h *Handler) WebUpdateCharacterProfile(ctx context.Context, req *connect.Request[webv1.WebUpdateCharacterProfileRequest]) (*connect.Response[webv1.WebUpdateCharacterProfileResponse], error) {
	slog.DebugContext(ctx, "web: WebUpdateCharacterProfile", "character_id", req.Msg.GetCharacterId())
	if h.characterAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("character access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.characterAccess.UpdateCharacterProfile(rpcCtx, &characteraccessv1.UpdateCharacterProfileRequest{
		CharacterId:        req.Msg.GetCharacterId(),
		PlayerSessionToken: token,
		ExpectedVersion:    req.Msg.GetExpectedVersion(),
		Pronouns:           req.Msg.GetPronouns(),
		Concept:            req.Msg.GetConcept(),
		Species:            req.Msg.GetSpecies(),
		Age:                req.Msg.GetAge(),
		Faction:            req.Msg.GetFaction(),
		Appearance:         req.Msg.GetAppearance(),
		Personality:        req.Msg.GetPersonality(),
		Biography:          req.Msg.GetBiography(),
		Rumors:             req.Msg.GetRumors(),
		Currently:          req.Msg.GetCurrently(),
		RpPreferences:      req.Msg.GetRpPreferences(),
		Timezone:           req.Msg.GetTimezone(),
		UpdateMask:         req.Msg.GetUpdateMask(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: update character profile RPC failed", err, "character_id", req.Msg.GetCharacterId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebUpdateCharacterProfileResponse{
		Character: resp.GetCharacter(),
	}), nil
}

// WebUpdateCharacterDescription proxies to
// CharacterAccessService.UpdateCharacterDescription — the intrinsic
// characters.description column, not a profile.* row.
func (h *Handler) WebUpdateCharacterDescription(ctx context.Context, req *connect.Request[webv1.WebUpdateCharacterDescriptionRequest]) (*connect.Response[webv1.WebUpdateCharacterDescriptionResponse], error) {
	slog.DebugContext(ctx, "web: WebUpdateCharacterDescription", "character_id", req.Msg.GetCharacterId())
	if h.characterAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("character access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.characterAccess.UpdateCharacterDescription(rpcCtx, &characteraccessv1.UpdateCharacterDescriptionRequest{
		CharacterId:        req.Msg.GetCharacterId(),
		PlayerSessionToken: token,
		ExpectedVersion:    req.Msg.GetExpectedVersion(),
		Description:        req.Msg.GetDescription(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: update character description RPC failed", err, "character_id", req.Msg.GetCharacterId())
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebUpdateCharacterDescriptionResponse{
		Character: resp.GetCharacter(),
	}), nil
}

// WebListCharacterDirectory proxies to
// CharacterAccessService.ListCharacterDirectory. The request message carries no
// fields at all — the listing is viewer-scoped, so the header token is the whole
// input and a client cannot name an acting character the gateway did not
// authenticate.
//
// It REPLACES WebListAllCharacters, which proxied CoreService.ListAllCharacters
// and re-exported that RPC's roster verbatim. That surface is gone rather than
// forwarded: the replacement answers a different question (which characters this
// VIEWER may reach, as identity only) against a different facade, and a
// forwarder would have preserved a contract with no consumer.
//
// The gateway computes nothing here in particular: it does not filter the list,
// does not re-sort it, and does not learn why a character is absent. Reachability
// and ordering are decided in the facade.
func (h *Handler) WebListCharacterDirectory(ctx context.Context, req *connect.Request[webv1.WebListCharacterDirectoryRequest]) (*connect.Response[webv1.WebListCharacterDirectoryResponse], error) {
	slog.DebugContext(ctx, "web: WebListCharacterDirectory")
	if h.characterAccess == nil {
		return nil, connect.NewError(connect.CodeUnimplemented, oops.Errorf("character access client not configured"))
	}

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.characterAccess.ListCharacterDirectory(rpcCtx, &characteraccessv1.ListCharacterDirectoryRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list character directory RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}

	return connect.NewResponse(&webv1.WebListCharacterDirectoryResponse{
		Characters: resp.GetCharacters(),
	}), nil
}
