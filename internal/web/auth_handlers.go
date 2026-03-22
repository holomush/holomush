// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// Cookie signal headers used to communicate between handlers and CookieMiddleware.
const (
	headerSetSessionToken    = "X-Set-Session-Token" //nolint:gosec // not a credential, just a header name
	headerClearSession       = "X-Clear-Session"
	headerRememberMe         = "X-Remember-Me"
	headerInjectSessionToken = "X-Session-Token"
)

// WebAuthenticatePlayer validates player credentials and returns a player token with character list.
func (h *Handler) WebAuthenticatePlayer(ctx context.Context, req *connect.Request[webv1.WebAuthenticatePlayerRequest]) (*connect.Response[webv1.WebAuthenticatePlayerResponse], error) {
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
		PlayerToken:        coreResp.GetPlayerToken(),
		Characters:         translateCharacterSummaries(coreResp.GetCharacters()),
		DefaultCharacterId: coreResp.GetDefaultCharacterId(),
	})
	resp.Header().Set(headerSetSessionToken, coreResp.GetPlayerToken())
	if req.Msg.GetRememberMe() {
		resp.Header().Set(headerRememberMe, "true")
	}
	return resp, nil
}

// WebSelectCharacter selects a character and creates or reattaches a game session.
func (h *Handler) WebSelectCharacter(ctx context.Context, req *connect.Request[webv1.WebSelectCharacterRequest]) (*connect.Response[webv1.WebSelectCharacterResponse], error) {
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.SelectCharacter(rpcCtx, &corev1.SelectCharacterRequest{
		PlayerToken: req.Msg.GetPlayerToken(),
		CharacterId: req.Msg.GetCharacterId(),
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
		Success:     true,
		PlayerToken: coreResp.GetPlayerToken(),
		Characters:  translateCharacterSummaries(coreResp.GetCharacters()),
	})
	resp.Header().Set(headerSetSessionToken, coreResp.GetPlayerToken())
	return resp, nil
}

// WebCreateCharacter creates a new character for the authenticated player.
func (h *Handler) WebCreateCharacter(ctx context.Context, req *connect.Request[webv1.WebCreateCharacterRequest]) (*connect.Response[webv1.WebCreateCharacterResponse], error) {
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.CreateCharacter(rpcCtx, &corev1.CreateCharacterRequest{
		PlayerToken:   req.Msg.GetPlayerToken(),
		CharacterName: req.Msg.GetCharacterName(),
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
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	coreResp, err := h.client.ListCharacters(rpcCtx, &corev1.ListCharactersRequest{
		PlayerToken: req.Msg.GetPlayerToken(),
	})
	if err != nil {
		slog.Error("web: list characters RPC failed", "error", err)
		return connect.NewResponse(&webv1.WebListCharactersResponse{}), nil
	}

	return connect.NewResponse(&webv1.WebListCharactersResponse{
		Characters: translateCharacterSummaries(coreResp.GetCharacters()),
	}), nil
}

// WebLogout ends the current session and clears the session cookie.
func (h *Handler) WebLogout(ctx context.Context, req *connect.Request[webv1.WebLogoutRequest]) (*connect.Response[webv1.WebLogoutResponse], error) {
	rpcCtx, cancel := context.WithTimeout(ctx, rpcTimeout)
	defer cancel()

	if _, err := h.client.Logout(rpcCtx, &corev1.LogoutRequest{
		SessionId: req.Msg.GetSessionId(),
	}); err != nil {
		slog.Error("web: logout RPC failed", "session_id", req.Msg.GetSessionId(), "error", err)
	}

	resp := connect.NewResponse(&webv1.WebLogoutResponse{})
	resp.Header().Set(headerClearSession, "true")
	return resp, nil
}

// WebRequestPasswordReset initiates a password reset flow.
func (h *Handler) WebRequestPasswordReset(ctx context.Context, req *connect.Request[webv1.WebRequestPasswordResetRequest]) (*connect.Response[webv1.WebRequestPasswordResetResponse], error) {
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
