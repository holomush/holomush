// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"connectrpc.com/connect"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// signalSessionCookie sets the signal headers that CookieMiddleware translates
// into a Set-Cookie header. Always sets the session token; sets MaxAge from
// the core RPC's session_ttl_seconds so guest cookies (2h TTL) do not outlive
// their sessions. A zero/missing TTL falls back to the cookie default.
func signalSessionCookie(h http.Header, token string, ttlSeconds int64) {
	h.Set(headerSetSessionToken, token)
	if ttlSeconds > 0 {
		h.Set(headerSetSessionMaxAge, strconv.FormatInt(ttlSeconds, 10))
	}
}

// Cookie signal headers used to communicate between handlers and CookieMiddleware.
const (
	headerSetSessionToken    = "X-Set-Session-Token" //nolint:gosec // not a credential, just a header name
	headerSetSessionMaxAge   = "X-Set-Session-Max-Age"
	headerClearSession       = "X-Clear-Session"
	headerInjectSessionToken = "X-Session-Token"
)

// Exported aliases of the wire-level header names so integration tests can
// thread the same header values without duplicating string literals. These
// MUST stay in sync with the unexported constants above.
const (
	HeaderInjectSessionToken = headerInjectSessionToken
	HeaderSetSessionToken    = headerSetSessionToken
	HeaderSetSessionMaxAge   = headerSetSessionMaxAge
	HeaderClearSession       = headerClearSession
)

// playerTokenFromHeader extracts the player session token from the
// X-Session-Token header injected by CookieMiddleware. Returns
// CodeUnauthenticated if the header is missing or empty.
func playerTokenFromHeader(h http.Header) (string, error) {
	token := h.Get(headerInjectSessionToken)
	if token == "" {
		return "", connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("no player session"))
	}
	return token, nil
}

// checkCookieCollision implements the cookie-collision gate documented in
// docs/superpowers/specs/2026-04-25-multi-tab-session-isolation-design.md §4.2.0.
//
// Returns:
//   - name: the existing player's display name (only meaningful when gated=true).
//   - gated: true if the request carries a valid PlayerSession cookie and the
//     caller MUST short-circuit with ALREADY_AUTHENTICATED.
//   - err: non-nil iff the cookie validation hit an unexpected error
//     (transport / lookup-failed). Auth-failure (PLAYER_SESSION_NOT_FOUND /
//     PLAYER_SESSION_EXPIRED) is a normal case and returns gated=false, err=nil.
func (h *Handler) checkCookieCollision(ctx context.Context, headers http.Header) (name string, gated bool, err error) {
	token := headers.Get(headerInjectSessionToken)
	if token == "" {
		return "", false, nil
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	resp, err := h.client.CheckPlayerSession(rpcCtx, &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		if isPlayerSessionAuthFailure(err) {
			return "", false, nil
		}
		return "", false, oops.Code("COOKIE_GATE_LOOKUP_FAILED").Wrap(err)
	}
	return resp.GetPlayerName(), true, nil
}

// isPlayerSessionAuthFailure reports whether err is one of the documented
// auth-failure codes that mean "cookie is invalid; treat as no-cookie".
// Unknown error types and non-string codes return false (caller surfaces).
func isPlayerSessionAuthFailure(err error) bool {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return false
	}
	code, ok := oopsErr.Code().(string)
	if !ok {
		return false
	}
	return code == "PLAYER_SESSION_NOT_FOUND" ||
		code == "PLAYER_SESSION_EXPIRED" ||
		code == "SESSION_NOT_FOUND"
}

// WebAuthenticatePlayer validates player credentials and returns a player token with character list.
func (h *Handler) WebAuthenticatePlayer(ctx context.Context, req *connect.Request[webv1.WebAuthenticatePlayerRequest]) (*connect.Response[webv1.WebAuthenticatePlayerResponse], error) {
	slog.DebugContext(ctx, "web: WebAuthenticatePlayer", "username", req.Msg.GetUsername())

	if name, gated, err := h.checkCookieCollision(ctx, req.Header()); err != nil {
		return nil, oops.Wrap(err)
	} else if gated {
		return connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
			Success:           false,
			ErrorCode:         "ALREADY_AUTHENTICATED",
			ErrorMessage:      fmt.Sprintf("Already signed in as %s.", name),
			CurrentPlayerName: name,
		}), nil
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.AuthenticatePlayer(rpcCtx, &corev1.AuthenticatePlayerRequest{
		Username:   req.Msg.GetUsername(),
		Password:   req.Msg.GetPassword(),
		RememberMe: req.Msg.GetRememberMe(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: authenticate player RPC failed", err)
		return connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
			Success: false, ErrorMessage: "authentication error",
		}), nil
	}
	if !coreResp.GetSuccess() {
		return connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
			Success: false, ErrorMessage: coreResp.GetErrorMessage(),
		}), nil
	}

	resp := connect.NewResponse(&webv1.WebAuthenticatePlayerResponse{
		Success:            true,
		Characters:         translateCharacterSummaries(coreResp.GetCharacters()),
		DefaultCharacterId: coreResp.GetDefaultCharacterId(),
	})
	signalSessionCookie(resp.Header(), coreResp.GetPlayerSessionToken(), coreResp.GetSessionTtlSeconds())
	return resp, nil
}

// WebSelectCharacter selects a character and creates or reattaches a game session.
func (h *Handler) WebSelectCharacter(ctx context.Context, req *connect.Request[webv1.WebSelectCharacterRequest]) (*connect.Response[webv1.WebSelectCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebSelectCharacter", "character_id", req.Msg.GetCharacterId())

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.SelectCharacter(rpcCtx, &corev1.SelectCharacterRequest{
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
		ClientType:         req.Msg.GetClientType(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: select character RPC failed", err)
		return connect.NewResponse(&webv1.WebSelectCharacterResponse{
			Success: false, ErrorMessage: "character selection error",
		}), nil
	}
	if !coreResp.GetSuccess() {
		return connect.NewResponse(&webv1.WebSelectCharacterResponse{
			Success: false, ErrorMessage: coreResp.GetErrorMessage(),
		}), nil
	}

	return connect.NewResponse(&webv1.WebSelectCharacterResponse{
		Success:       true,
		SessionId:     coreResp.GetSessionId(),
		CharacterName: coreResp.GetCharacterName(),
		Reattached:    coreResp.GetReattached(),
	}), nil
}

// WebCreatePlayer creates a new player account.
func (h *Handler) WebCreatePlayer(ctx context.Context, req *connect.Request[webv1.WebCreatePlayerRequest]) (*connect.Response[webv1.WebCreatePlayerResponse], error) {
	slog.DebugContext(ctx, "web: WebCreatePlayer", "username", req.Msg.GetUsername())

	if name, gated, err := h.checkCookieCollision(ctx, req.Header()); err != nil {
		return nil, oops.Wrap(err)
	} else if gated {
		return connect.NewResponse(&webv1.WebCreatePlayerResponse{
			Success:           false,
			ErrorCode:         "ALREADY_AUTHENTICATED",
			ErrorMessage:      fmt.Sprintf("Already signed in as %s.", name),
			CurrentPlayerName: name,
		}), nil
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CreatePlayer(rpcCtx, &corev1.CreatePlayerRequest{
		Username: req.Msg.GetUsername(),
		Password: req.Msg.GetPassword(),
		Email:    req.Msg.GetEmail(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: create player RPC failed", err)
		return connect.NewResponse(&webv1.WebCreatePlayerResponse{
			Success: false, ErrorMessage: "registration error",
		}), nil
	}
	if !coreResp.GetSuccess() {
		return connect.NewResponse(&webv1.WebCreatePlayerResponse{
			Success: false, ErrorMessage: coreResp.GetErrorMessage(),
		}), nil
	}

	resp := connect.NewResponse(&webv1.WebCreatePlayerResponse{
		Success:    true,
		Characters: translateCharacterSummaries(coreResp.GetCharacters()),
	})
	signalSessionCookie(resp.Header(), coreResp.GetPlayerSessionToken(), coreResp.GetSessionTtlSeconds())
	return resp, nil
}

// WebCreateCharacter creates a new character for the authenticated player.
func (h *Handler) WebCreateCharacter(ctx context.Context, req *connect.Request[webv1.WebCreateCharacterRequest]) (*connect.Response[webv1.WebCreateCharacterResponse], error) {
	slog.DebugContext(ctx, "web: WebCreateCharacter", "character_name", req.Msg.GetCharacterName())

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CreateCharacter(rpcCtx, &corev1.CreateCharacterRequest{
		PlayerSessionToken: token,
		CharacterName:      req.Msg.GetCharacterName(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: create character RPC failed", err)
		return connect.NewResponse(&webv1.WebCreateCharacterResponse{
			Success: false, ErrorMessage: "character creation error",
		}), nil
	}
	if !coreResp.GetSuccess() {
		return connect.NewResponse(&webv1.WebCreateCharacterResponse{
			Success: false, ErrorMessage: coreResp.GetErrorMessage(),
		}), nil
	}

	return connect.NewResponse(&webv1.WebCreateCharacterResponse{
		Success:       true,
		CharacterId:   coreResp.GetCharacterId(),
		CharacterName: coreResp.GetCharacterName(),
	}), nil
}

// WebListCharacters returns the characters available for the authenticated player.
func (h *Handler) WebListCharacters(ctx context.Context, req *connect.Request[webv1.WebListCharactersRequest]) (*connect.Response[webv1.WebListCharactersResponse], error) {
	slog.DebugContext(ctx, "web: WebListCharacters")

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.ListCharacters(rpcCtx, &corev1.ListCharactersRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list characters RPC failed", err)
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("session expired or invalid"))
	}

	return connect.NewResponse(&webv1.WebListCharactersResponse{
		Characters: translateCharacterSummaries(coreResp.GetCharacters()),
	}), nil
}

// WebListAllCharacters returns the full character directory (id+name) for the
// picker. Proxies to CoreService.ListAllCharacters, which authorizes the call:
// any session (registered or guest) holding the named, session-owned acting
// character may list names (INV-ACCESS-9). Authorization is enforced at core;
// this BFF only forwards the cookie token and character_id.
func (h *Handler) WebListAllCharacters(ctx context.Context, req *connect.Request[webv1.WebListAllCharactersRequest]) (*connect.Response[webv1.WebListAllCharactersResponse], error) {
	slog.DebugContext(ctx, "web: WebListAllCharacters")
	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()
	coreResp, err := h.client.ListAllCharacters(rpcCtx, &corev1.ListAllCharactersRequest{
		PlayerSessionToken: token,
		CharacterId:        req.Msg.GetCharacterId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list all characters RPC failed", err)
		return nil, err //nolint:wrapcheck // gRPC status errors pass through as-is
	}
	return connect.NewResponse(&webv1.WebListAllCharactersResponse{Characters: coreResp.GetCharacters()}), nil
}

// WebLogout ends the current session and clears the session cookie.
func (h *Handler) WebLogout(ctx context.Context, req *connect.Request[webv1.WebLogoutRequest]) (*connect.Response[webv1.WebLogoutResponse], error) {
	slog.DebugContext(ctx, "web: WebLogout")

	token := req.Header().Get(headerInjectSessionToken)

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if token != "" {
		if _, err := h.client.Logout(rpcCtx, &corev1.LogoutRequest{
			PlayerSessionToken: token,
		}); err != nil {
			errutil.LogErrorContext(ctx, "web: logout RPC failed", err)
		}
	}

	resp := connect.NewResponse(&webv1.WebLogoutResponse{})
	resp.Header().Set(headerClearSession, "true")
	return resp, nil
}

// WebCheckSession validates the player session from the cookie and returns the player name.
func (h *Handler) WebCheckSession(ctx context.Context, req *connect.Request[webv1.WebCheckSessionRequest]) (*connect.Response[webv1.WebCheckSessionResponse], error) {
	slog.DebugContext(ctx, "web: WebCheckSession")

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CheckPlayerSession(rpcCtx, &corev1.CheckPlayerSessionRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("session expired or invalid"))
	}

	return connect.NewResponse(&webv1.WebCheckSessionResponse{
		PlayerName: coreResp.GetPlayerName(),
		PlayerId:   coreResp.GetPlayerId(),
		IsGuest:    coreResp.GetIsGuest(),
		Characters: translateCharacterSummaries(coreResp.GetCharacters()),
	}), nil
}

// WebRequestPasswordReset initiates a password reset flow.
func (h *Handler) WebRequestPasswordReset(ctx context.Context, req *connect.Request[webv1.WebRequestPasswordResetRequest]) (*connect.Response[webv1.WebRequestPasswordResetResponse], error) {
	slog.DebugContext(ctx, "web: WebRequestPasswordReset")

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.RequestPasswordReset(rpcCtx, &corev1.RequestPasswordResetRequest{
		Email: req.Msg.GetEmail(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: request password reset RPC failed", err)
		// Return success to avoid leaking whether the email exists.
		return connect.NewResponse(&webv1.WebRequestPasswordResetResponse{
			Success: true,
		}), nil
	}

	return connect.NewResponse(&webv1.WebRequestPasswordResetResponse{
		Success: coreResp.GetSuccess(),
	}), nil
}

// WebConfirmPasswordReset completes a password reset using a token.
func (h *Handler) WebConfirmPasswordReset(ctx context.Context, req *connect.Request[webv1.WebConfirmPasswordResetRequest]) (*connect.Response[webv1.WebConfirmPasswordResetResponse], error) {
	slog.DebugContext(ctx, "web: WebConfirmPasswordReset")

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.ConfirmPasswordReset(rpcCtx, &corev1.ConfirmPasswordResetRequest{
		Token:       req.Msg.GetToken(),
		NewPassword: req.Msg.GetNewPassword(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: confirm password reset RPC failed", err)
		return connect.NewResponse(&webv1.WebConfirmPasswordResetResponse{
			Success: false, ErrorMessage: "password reset error",
		}), nil
	}
	if !coreResp.GetSuccess() {
		return connect.NewResponse(&webv1.WebConfirmPasswordResetResponse{
			Success: false, ErrorMessage: coreResp.GetErrorMessage(),
		}), nil
	}

	return connect.NewResponse(&webv1.WebConfirmPasswordResetResponse{
		Success: true,
	}), nil
}

// WebCreateGuest creates an ephemeral guest player and returns a session cookie.
func (h *Handler) WebCreateGuest(ctx context.Context, req *connect.Request[webv1.WebCreateGuestRequest]) (*connect.Response[webv1.WebCreateGuestResponse], error) {
	slog.DebugContext(ctx, "web: WebCreateGuest")

	if name, gated, err := h.checkCookieCollision(ctx, req.Header()); err != nil {
		return nil, oops.Wrap(err)
	} else if gated {
		return connect.NewResponse(&webv1.WebCreateGuestResponse{
			Success:           false,
			ErrorCode:         "ALREADY_AUTHENTICATED",
			ErrorMessage:      fmt.Sprintf("Already signed in as %s.", name),
			CurrentPlayerName: name,
		}), nil
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CreateGuest(rpcCtx, &corev1.CreateGuestRequest{})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: create guest RPC failed", err)
		return connect.NewResponse(&webv1.WebCreateGuestResponse{
			Success: false, ErrorMessage: "guest creation error",
		}), nil
	}
	if !coreResp.GetSuccess() {
		return connect.NewResponse(&webv1.WebCreateGuestResponse{
			Success: false, ErrorMessage: coreResp.GetErrorMessage(),
		}), nil
	}

	resp := connect.NewResponse(&webv1.WebCreateGuestResponse{
		Success:            true,
		Characters:         translateCharacterSummaries(coreResp.GetCharacters()),
		DefaultCharacterId: coreResp.GetDefaultCharacterId(),
	})
	signalSessionCookie(resp.Header(), coreResp.GetPlayerSessionToken(), coreResp.GetSessionTtlSeconds())
	return resp, nil
}

// WebListPlayerSessions returns the caller's active PlayerSessions. The
// caller is identified via the X-Session-Token cookie header; the returned
// sessions each include an is_current flag marking the calling session.
func (h *Handler) WebListPlayerSessions(ctx context.Context, req *connect.Request[webv1.WebListPlayerSessionsRequest]) (*connect.Response[webv1.WebListPlayerSessionsResponse], error) {
	slog.DebugContext(ctx, "web: WebListPlayerSessions")

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.ListPlayerSessions(rpcCtx, &corev1.ListPlayerSessionsRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: list player sessions RPC failed", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	sessions := make([]*webv1.WebPlayerSessionInfo, 0, len(coreResp.GetSessions()))
	for _, s := range coreResp.GetSessions() {
		sessions = append(sessions, &webv1.WebPlayerSessionInfo{
			Id:         s.GetId(),
			CreatedAt:  s.GetCreatedAt(),
			LastActive: s.GetLastActive(),
			UserAgent:  s.GetUserAgent(),
			IpAddress:  s.GetIpAddress(),
			IsCurrent:  s.GetIsCurrent(),
		})
	}
	return connect.NewResponse(&webv1.WebListPlayerSessionsResponse{Sessions: sessions}), nil
}

// WebRevokePlayerSession revokes a specific PlayerSession owned by the caller.
// The caller is identified via the X-Session-Token cookie header.
func (h *Handler) WebRevokePlayerSession(ctx context.Context, req *connect.Request[webv1.WebRevokePlayerSessionRequest]) (*connect.Response[webv1.WebRevokePlayerSessionResponse], error) {
	slog.DebugContext(ctx, "web: WebRevokePlayerSession", "target_session_id", req.Msg.GetTargetSessionId())

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.RevokePlayerSession(rpcCtx, &corev1.RevokePlayerSessionRequest{
		PlayerSessionToken: token,
		TargetSessionId:    req.Msg.GetTargetSessionId(),
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: revoke player session RPC failed", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&webv1.WebRevokePlayerSessionResponse{
		Success:      coreResp.GetSuccess(),
		ErrorMessage: coreResp.GetErrorMessage(),
	}), nil
}

// WebRevokeOtherPlayerSessions revokes all PlayerSessions for the caller
// except the current one. The caller is identified via the X-Session-Token
// cookie header.
func (h *Handler) WebRevokeOtherPlayerSessions(ctx context.Context, req *connect.Request[webv1.WebRevokeOtherPlayerSessionsRequest]) (*connect.Response[webv1.WebRevokeOtherPlayerSessionsResponse], error) {
	slog.DebugContext(ctx, "web: WebRevokeOtherPlayerSessions")

	token, err := playerTokenFromHeader(req.Header())
	if err != nil {
		return nil, err
	}

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.RevokeOtherPlayerSessions(rpcCtx, &corev1.RevokeOtherPlayerSessionsRequest{
		PlayerSessionToken: token,
	})
	if err != nil {
		errutil.LogErrorContext(ctx, "web: revoke other player sessions RPC failed", err)
		return nil, connect.NewError(connect.CodeInternal, err)
	}

	return connect.NewResponse(&webv1.WebRevokeOtherPlayerSessionsResponse{
		Success:      coreResp.GetSuccess(),
		RevokedCount: coreResp.GetRevokedCount(),
	}), nil
}

// translateCharacterSummaries converts core proto CharacterSummary slices to web proto equivalents.
func translateCharacterSummaries(core []*corev1.CharacterSummary) []*webv1.CharacterSummary {
	if len(core) == 0 {
		return nil
	}
	out := make([]*webv1.CharacterSummary, len(core))
	for i, c := range core {
		out[i] = &webv1.CharacterSummary{
			CharacterId:      c.GetCharacterId(),
			CharacterName:    c.GetCharacterName(),
			HasActiveSession: c.GetHasActiveSession(),
			SessionStatus:    c.GetSessionStatus(),
			LastLocation:     c.GetLastLocation(),
			LastPlayedAt:     c.GetLastPlayedAt(),
		}
	}
	return out
}
