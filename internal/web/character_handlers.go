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
