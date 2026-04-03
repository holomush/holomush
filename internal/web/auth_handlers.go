// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"connectrpc.com/connect"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// Cookie signal headers used to communicate between handlers and CookieMiddleware.
const (
	headerSetSessionToken    = "X-Set-Session-Token" //nolint:gosec // not a credential, just a header name
	headerClearSession       = "X-Clear-Session"
	headerInjectSessionToken = "X-Session-Token"
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

// WebAuthenticatePlayer validates player credentials and returns a player token with character list.
func (h *Handler) WebAuthenticatePlayer(ctx context.Context, req *connect.Request[webv1.WebAuthenticatePlayerRequest]) (*connect.Response[webv1.WebAuthenticatePlayerResponse], error) {
	slog.DebugContext(ctx, "web: WebAuthenticatePlayer", "username", req.Msg.GetUsername())

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.AuthenticatePlayer(rpcCtx, &corev1.AuthenticatePlayerRequest{
		Username:   req.Msg.GetUsername(),
		Password:   req.Msg.GetPassword(),
		RememberMe: req.Msg.GetRememberMe(),
	})
	if err != nil {
		slog.Error("web: authenticate player RPC failed", "error", err)
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
	resp.Header().Set(headerSetSessionToken, coreResp.GetPlayerSessionToken())
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
	})
	if err != nil {
		slog.Error("web: select character RPC failed", "error", err)
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

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CreatePlayer(rpcCtx, &corev1.CreatePlayerRequest{
		Username: req.Msg.GetUsername(),
		Password: req.Msg.GetPassword(),
		Email:    req.Msg.GetEmail(),
	})
	if err != nil {
		slog.Error("web: create player RPC failed", "error", err)
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
	resp.Header().Set(headerSetSessionToken, coreResp.GetPlayerSessionToken())
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
		slog.Error("web: create character RPC failed", "error", err)
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
		slog.Error("web: list characters RPC failed", "error", err)
		return nil, connect.NewError(connect.CodeUnauthenticated, fmt.Errorf("session expired or invalid"))
	}

	return connect.NewResponse(&webv1.WebListCharactersResponse{
		Characters: translateCharacterSummaries(coreResp.GetCharacters()),
	}), nil
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
			slog.Error("web: logout RPC failed", "error", err)
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
		slog.Error("web: request password reset RPC failed", "error", err)
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
		slog.Error("web: confirm password reset RPC failed", "error", err)
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
func (h *Handler) WebCreateGuest(ctx context.Context, _ *connect.Request[webv1.WebCreateGuestRequest]) (*connect.Response[webv1.WebCreateGuestResponse], error) {
	slog.DebugContext(ctx, "web: WebCreateGuest")

	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CreateGuest(rpcCtx, &corev1.CreateGuestRequest{})
	if err != nil {
		slog.Error("web: create guest RPC failed", "error", err)
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
	resp.Header().Set(headerSetSessionToken, coreResp.GetPlayerSessionToken())
	return resp, nil
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
