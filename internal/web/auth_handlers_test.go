// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// requestWithToken builds a connect.Request with the session token header injected.
func requestWithToken[T any](msg *T, token string) *connect.Request[T] {
	req := connect.NewRequest(msg)
	req.Header().Set(headerInjectSessionToken, token)
	return req
}

// --- playerTokenFromHeader ---

func TestPlayerTokenFromHeader_Present(t *testing.T) {
	h := http.Header{}
	h.Set(headerInjectSessionToken, "tok-abc")
	token, err := playerTokenFromHeader(h)
	require.NoError(t, err)
	assert.Equal(t, "tok-abc", token)
}

func TestPlayerTokenFromHeader_Missing(t *testing.T) {
	h := http.Header{}
	token, err := playerTokenFromHeader(h)
	assert.Empty(t, token)
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestPlayerTokenFromHeader_Empty(t *testing.T) {
	h := http.Header{}
	h.Set(headerInjectSessionToken, "")
	token, err := playerTokenFromHeader(h)
	assert.Empty(t, token)
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

// --- WebAuthenticatePlayer ---

func TestWebAuthenticatePlayer_Success(t *testing.T) {
	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-abc",
			Characters: []*corev1.CharacterSummary{
				{CharacterId: "c1", CharacterName: "Alice"},
			},
			DefaultCharacterId: "c1",
		},
	}
	h := NewHandler(client)

	resp, err := h.WebAuthenticatePlayer(context.Background(), connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{
		Username:   "user",
		Password:   "pass",
		RememberMe: true,
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Len(t, resp.Msg.GetCharacters(), 1)
	assert.Equal(t, "Alice", resp.Msg.GetCharacters()[0].GetCharacterName())
	assert.Equal(t, "c1", resp.Msg.GetDefaultCharacterId())
	assert.Equal(t, "tok-abc", resp.Header().Get(headerSetSessionToken))
}

func TestWebAuthenticatePlayer_CoreFailure(t *testing.T) {
	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:      false,
			ErrorMessage: "bad credentials",
		},
	}
	h := NewHandler(client)

	resp, err := h.WebAuthenticatePlayer(context.Background(), connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{
		Username: "user",
		Password: "wrong",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "bad credentials", resp.Msg.GetErrorMessage())
	assert.Empty(t, resp.Header().Get(headerSetSessionToken))
}

func TestWebAuthenticatePlayer_RPCError(t *testing.T) {
	client := &mockCoreClient{
		authPlayerErr: errors.New("connection refused"),
	}
	h := NewHandler(client)

	resp, err := h.WebAuthenticatePlayer(context.Background(), connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{
		Username: "user",
		Password: "pass",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "authentication error", resp.Msg.GetErrorMessage())
	assert.Empty(t, resp.Header().Get(headerSetSessionToken))
}

func TestWebAuthenticatePlayer_NoRememberMe(t *testing.T) {
	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-short",
		},
	}
	h := NewHandler(client)

	resp, err := h.WebAuthenticatePlayer(context.Background(), connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{
		Username:   "user",
		Password:   "pass",
		RememberMe: false,
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, "tok-short", resp.Header().Get(headerSetSessionToken))
}

// --- WebSelectCharacter ---

func TestWebSelectCharacter_Success(t *testing.T) {
	client := &mockCoreClient{
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-123",
			CharacterName: "Alice",
			Reattached:    false,
		},
	}
	h := NewHandler(client)

	resp, err := h.WebSelectCharacter(context.Background(), requestWithToken(&webv1.WebSelectCharacterRequest{
		CharacterId: "c1",
	}, "tok-abc"))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, "sess-123", resp.Msg.GetSessionId())
	assert.Equal(t, "Alice", resp.Msg.GetCharacterName())
	assert.False(t, resp.Msg.GetReattached())
}

func TestWebSelectCharacter_Reattached(t *testing.T) {
	client := &mockCoreClient{
		selectCharResp: &corev1.SelectCharacterResponse{
			Success:       true,
			SessionId:     "sess-456",
			CharacterName: "Bob",
			Reattached:    true,
		},
	}
	h := NewHandler(client)

	resp, err := h.WebSelectCharacter(context.Background(), requestWithToken(&webv1.WebSelectCharacterRequest{
		CharacterId: "c2",
	}, "tok-abc"))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetReattached())
}

func TestWebSelectCharacter_MissingToken(t *testing.T) {
	client := &mockCoreClient{}
	h := NewHandler(client)

	_, err := h.WebSelectCharacter(context.Background(), connect.NewRequest(&webv1.WebSelectCharacterRequest{
		CharacterId: "c1",
	}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestWebSelectCharacter_RPCError(t *testing.T) {
	client := &mockCoreClient{
		selectCharErr: errors.New("timeout"),
	}
	h := NewHandler(client)

	resp, err := h.WebSelectCharacter(context.Background(), requestWithToken(&webv1.WebSelectCharacterRequest{
		CharacterId: "c1",
	}, "tok-abc"))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "character selection error", resp.Msg.GetErrorMessage())
}

// --- WebCreatePlayer ---

func TestWebCreatePlayer_Success(t *testing.T) {
	client := &mockCoreClient{
		createPlayerResp: &corev1.CreatePlayerResponse{
			Success:            true,
			PlayerSessionToken: "tok-new",
			Characters:         []*corev1.CharacterSummary{},
		},
	}
	h := NewHandler(client)

	resp, err := h.WebCreatePlayer(context.Background(), connect.NewRequest(&webv1.WebCreatePlayerRequest{
		Username: "newuser",
		Password: "secret",
		Email:    "new@example.com",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, "tok-new", resp.Header().Get(headerSetSessionToken))
}

func TestWebCreatePlayer_CoreFailure(t *testing.T) {
	client := &mockCoreClient{
		createPlayerResp: &corev1.CreatePlayerResponse{
			Success:      false,
			ErrorMessage: "username taken",
		},
	}
	h := NewHandler(client)

	resp, err := h.WebCreatePlayer(context.Background(), connect.NewRequest(&webv1.WebCreatePlayerRequest{
		Username: "taken",
		Password: "secret",
		Email:    "t@example.com",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "username taken", resp.Msg.GetErrorMessage())
	assert.Empty(t, resp.Header().Get(headerSetSessionToken))
}

func TestWebCreatePlayer_RPCError(t *testing.T) {
	client := &mockCoreClient{
		createPlayerErr: errors.New("unavailable"),
	}
	h := NewHandler(client)

	resp, err := h.WebCreatePlayer(context.Background(), connect.NewRequest(&webv1.WebCreatePlayerRequest{
		Username: "user",
		Password: "pass",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "registration error", resp.Msg.GetErrorMessage())
}

// --- WebCreateCharacter ---

func TestWebCreateCharacter_Success(t *testing.T) {
	client := &mockCoreClient{
		createCharResp: &corev1.CreateCharacterResponse{
			Success:       true,
			CharacterId:   "char-new",
			CharacterName: "NewChar",
		},
	}
	h := NewHandler(client)

	resp, err := h.WebCreateCharacter(context.Background(), requestWithToken(&webv1.WebCreateCharacterRequest{
		CharacterName: "NewChar",
	}, "tok-abc"))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, "char-new", resp.Msg.GetCharacterId())
	assert.Equal(t, "NewChar", resp.Msg.GetCharacterName())
}

func TestWebCreateCharacter_MissingToken(t *testing.T) {
	client := &mockCoreClient{}
	h := NewHandler(client)

	_, err := h.WebCreateCharacter(context.Background(), connect.NewRequest(&webv1.WebCreateCharacterRequest{
		CharacterName: "Char",
	}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestWebCreateCharacter_RPCError(t *testing.T) {
	client := &mockCoreClient{
		createCharErr: errors.New("timeout"),
	}
	h := NewHandler(client)

	resp, err := h.WebCreateCharacter(context.Background(), requestWithToken(&webv1.WebCreateCharacterRequest{
		CharacterName: "Char",
	}, "tok-abc"))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "character creation error", resp.Msg.GetErrorMessage())
}

// --- WebListCharacters ---

func TestWebListCharacters_Success(t *testing.T) {
	client := &mockCoreClient{
		listCharsResp: &corev1.ListCharactersResponse{
			Characters: []*corev1.CharacterSummary{
				{CharacterId: "c1", CharacterName: "Alice", HasActiveSession: true},
				{CharacterId: "c2", CharacterName: "Bob", HasActiveSession: false},
			},
		},
	}
	h := NewHandler(client)

	resp, err := h.WebListCharacters(context.Background(), requestWithToken(&webv1.WebListCharactersRequest{}, "tok-abc"))
	require.NoError(t, err)
	assert.Len(t, resp.Msg.GetCharacters(), 2)
	assert.Equal(t, "Alice", resp.Msg.GetCharacters()[0].GetCharacterName())
	assert.True(t, resp.Msg.GetCharacters()[0].GetHasActiveSession())
}

func TestWebListCharacters_MissingToken(t *testing.T) {
	client := &mockCoreClient{}
	h := NewHandler(client)

	resp, err := h.WebListCharacters(context.Background(), connect.NewRequest(&webv1.WebListCharactersRequest{}))
	require.Error(t, err)
	assert.Nil(t, resp)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestWebListCharacters_RPCError(t *testing.T) {
	client := &mockCoreClient{
		listCharsErr: errors.New("unavailable"),
	}
	h := NewHandler(client)

	resp, err := h.WebListCharacters(context.Background(), requestWithToken(&webv1.WebListCharactersRequest{}, "tok-abc"))
	require.Error(t, err)
	assert.Nil(t, resp)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

// --- WebLogout ---

func TestWebLogout_Success(t *testing.T) {
	client := &mockCoreClient{
		logoutResp: &corev1.LogoutResponse{},
	}
	h := NewHandler(client)

	resp, err := h.WebLogout(context.Background(), requestWithToken(&webv1.WebLogoutRequest{}, "sess-123"))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
	assert.Equal(t, "true", resp.Header().Get(headerClearSession))
}

func TestWebLogout_RPCError(t *testing.T) {
	client := &mockCoreClient{
		logoutErr: errors.New("core down"),
	}
	h := NewHandler(client)

	resp, err := h.WebLogout(context.Background(), requestWithToken(&webv1.WebLogoutRequest{}, "sess-123"))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
	assert.Equal(t, "true", resp.Header().Get(headerClearSession), "cookie should be cleared even on RPC error")
}

func TestWebLogout_NoToken_StillClears(t *testing.T) {
	client := &mockCoreClient{}
	h := NewHandler(client)

	resp, err := h.WebLogout(context.Background(), connect.NewRequest(&webv1.WebLogoutRequest{}))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
	assert.Equal(t, "true", resp.Header().Get(headerClearSession))
}

// --- WebCheckSession ---

func TestWebCheckSession_Success(t *testing.T) {
	client := &mockCoreClient{
		checkSessionResp: &corev1.CheckPlayerSessionResponse{
			PlayerName: "alice",
		},
	}
	h := NewHandler(client)

	resp, err := h.WebCheckSession(context.Background(), requestWithToken(&webv1.WebCheckSessionRequest{}, "tok-abc"))
	require.NoError(t, err)
	assert.Equal(t, "alice", resp.Msg.GetPlayerName())
}

func TestWebCheckSession_NoCookie(t *testing.T) {
	client := &mockCoreClient{}
	h := NewHandler(client)

	_, err := h.WebCheckSession(context.Background(), connect.NewRequest(&webv1.WebCheckSessionRequest{}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestWebCheckSession_CoreRPCError(t *testing.T) {
	client := &mockCoreClient{
		checkSessionErr: errors.New("session expired"),
	}
	h := NewHandler(client)

	_, err := h.WebCheckSession(context.Background(), requestWithToken(&webv1.WebCheckSessionRequest{}, "tok-expired"))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

// --- WebRequestPasswordReset ---

func TestWebRequestPasswordReset_Success(t *testing.T) {
	client := &mockCoreClient{
		reqPwResetResp: &corev1.RequestPasswordResetResponse{
			Success: true,
		},
	}
	h := NewHandler(client)

	resp, err := h.WebRequestPasswordReset(context.Background(), connect.NewRequest(&webv1.WebRequestPasswordResetRequest{
		Email: "user@example.com",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
}

func TestWebRequestPasswordReset_RPCError_ReturnsSuccessToAvoidLeak(t *testing.T) {
	client := &mockCoreClient{
		reqPwResetErr: errors.New("timeout"),
	}
	h := NewHandler(client)

	resp, err := h.WebRequestPasswordReset(context.Background(), connect.NewRequest(&webv1.WebRequestPasswordResetRequest{
		Email: "unknown@example.com",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess(), "should return success to avoid leaking email existence")
}

// --- WebConfirmPasswordReset ---

func TestWebConfirmPasswordReset_Success(t *testing.T) {
	client := &mockCoreClient{
		confirmPwResetResp: &corev1.ConfirmPasswordResetResponse{
			Success: true,
		},
	}
	h := NewHandler(client)

	resp, err := h.WebConfirmPasswordReset(context.Background(), connect.NewRequest(&webv1.WebConfirmPasswordResetRequest{
		Token:       "reset-tok",
		NewPassword: "newpass",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Empty(t, resp.Msg.GetErrorMessage())
}

func TestWebConfirmPasswordReset_CoreFailure(t *testing.T) {
	client := &mockCoreClient{
		confirmPwResetResp: &corev1.ConfirmPasswordResetResponse{
			Success:      false,
			ErrorMessage: "token expired",
		},
	}
	h := NewHandler(client)

	resp, err := h.WebConfirmPasswordReset(context.Background(), connect.NewRequest(&webv1.WebConfirmPasswordResetRequest{
		Token:       "expired-tok",
		NewPassword: "newpass",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "token expired", resp.Msg.GetErrorMessage())
}

func TestWebConfirmPasswordReset_RPCError(t *testing.T) {
	client := &mockCoreClient{
		confirmPwResetErr: errors.New("connection refused"),
	}
	h := NewHandler(client)

	resp, err := h.WebConfirmPasswordReset(context.Background(), connect.NewRequest(&webv1.WebConfirmPasswordResetRequest{
		Token:       "tok",
		NewPassword: "pass",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "password reset error", resp.Msg.GetErrorMessage())
}

// --- WebListPlayerSessions ---

func TestWebListPlayerSessionsForwardsTokenAndTranslatesResponse(t *testing.T) {
	now := time.Now()
	created := timestamppb.New(now.Add(-time.Hour))
	lastActive := timestamppb.New(now)
	client := &mockCoreClient{
		listSessionsResp: &corev1.ListPlayerSessionsResponse{
			Sessions: []*corev1.PlayerSessionInfo{
				{
					Id:         "s1",
					CreatedAt:  created,
					LastActive: lastActive,
					UserAgent:  "chrome",
					IpAddress:  "10.0.0.1",
					IsCurrent:  true,
				},
				{
					Id:        "s2",
					UserAgent: "ff",
					IsCurrent: false,
				},
			},
		},
	}
	h := NewHandler(client)

	resp, err := h.WebListPlayerSessions(context.Background(), requestWithToken(&webv1.WebListPlayerSessionsRequest{}, "tok-abc"))
	require.NoError(t, err)
	require.Len(t, resp.Msg.GetSessions(), 2)
	assert.Equal(t, "s1", resp.Msg.GetSessions()[0].GetId())
	assert.Equal(t, "chrome", resp.Msg.GetSessions()[0].GetUserAgent())
	assert.Equal(t, "10.0.0.1", resp.Msg.GetSessions()[0].GetIpAddress())
	assert.True(t, resp.Msg.GetSessions()[0].GetIsCurrent())
	assert.Equal(t, created.AsTime().Unix(), resp.Msg.GetSessions()[0].GetCreatedAt().AsTime().Unix())
	assert.Equal(t, lastActive.AsTime().Unix(), resp.Msg.GetSessions()[0].GetLastActive().AsTime().Unix())
	assert.Equal(t, "s2", resp.Msg.GetSessions()[1].GetId())
	assert.False(t, resp.Msg.GetSessions()[1].GetIsCurrent())

	require.NotNil(t, client.listSessionsReq)
	assert.Equal(t, "tok-abc", client.listSessionsReq.GetPlayerSessionToken())
}

func TestWebListPlayerSessions_MissingToken(t *testing.T) {
	h := NewHandler(&mockCoreClient{})

	_, err := h.WebListPlayerSessions(context.Background(), connect.NewRequest(&webv1.WebListPlayerSessionsRequest{}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestWebListPlayerSessions_RPCError(t *testing.T) {
	client := &mockCoreClient{
		listSessionsErr: errors.New("core down"),
	}
	h := NewHandler(client)

	_, err := h.WebListPlayerSessions(context.Background(), requestWithToken(&webv1.WebListPlayerSessionsRequest{}, "tok-abc"))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
}

// --- WebRevokePlayerSession ---

func TestWebRevokePlayerSessionForwardsTokenAndTargetID(t *testing.T) {
	client := &mockCoreClient{
		revokeSessionResp: &corev1.RevokePlayerSessionResponse{
			Success: true,
		},
	}
	h := NewHandler(client)

	resp, err := h.WebRevokePlayerSession(context.Background(), requestWithToken(&webv1.WebRevokePlayerSessionRequest{
		TargetSessionId: "sess-target",
	}, "tok-abc"))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Empty(t, resp.Msg.GetErrorMessage())

	require.NotNil(t, client.revokeSessionReq)
	assert.Equal(t, "tok-abc", client.revokeSessionReq.GetPlayerSessionToken())
	assert.Equal(t, "sess-target", client.revokeSessionReq.GetTargetSessionId())
}

func TestWebRevokePlayerSessionPropagatesCoreFailure(t *testing.T) {
	client := &mockCoreClient{
		revokeSessionResp: &corev1.RevokePlayerSessionResponse{
			Success:      false,
			ErrorMessage: "not found",
		},
	}
	h := NewHandler(client)

	resp, err := h.WebRevokePlayerSession(context.Background(), requestWithToken(&webv1.WebRevokePlayerSessionRequest{
		TargetSessionId: "sess-missing",
	}, "tok-abc"))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "not found", resp.Msg.GetErrorMessage())
}

func TestWebRevokePlayerSession_MissingToken(t *testing.T) {
	h := NewHandler(&mockCoreClient{})

	_, err := h.WebRevokePlayerSession(context.Background(), connect.NewRequest(&webv1.WebRevokePlayerSessionRequest{
		TargetSessionId: "sess-target",
	}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestWebRevokePlayerSession_RPCError(t *testing.T) {
	client := &mockCoreClient{
		revokeSessionErr: errors.New("core down"),
	}
	h := NewHandler(client)

	_, err := h.WebRevokePlayerSession(context.Background(), requestWithToken(&webv1.WebRevokePlayerSessionRequest{
		TargetSessionId: "sess-target",
	}, "tok-abc"))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
}

// --- WebRevokeOtherPlayerSessions ---

func TestWebRevokeOtherPlayerSessionsForwardsTokenAndReturnsCount(t *testing.T) {
	client := &mockCoreClient{
		revokeOtherResp: &corev1.RevokeOtherPlayerSessionsResponse{
			Success:      true,
			RevokedCount: 3,
		},
	}
	h := NewHandler(client)

	resp, err := h.WebRevokeOtherPlayerSessions(context.Background(), requestWithToken(&webv1.WebRevokeOtherPlayerSessionsRequest{}, "tok-abc"))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, int32(3), resp.Msg.GetRevokedCount())

	require.NotNil(t, client.revokeOtherReq)
	assert.Equal(t, "tok-abc", client.revokeOtherReq.GetPlayerSessionToken())
}

func TestWebRevokeOtherPlayerSessions_MissingToken(t *testing.T) {
	h := NewHandler(&mockCoreClient{})

	_, err := h.WebRevokeOtherPlayerSessions(context.Background(), connect.NewRequest(&webv1.WebRevokeOtherPlayerSessionsRequest{}))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeUnauthenticated, connectErr.Code())
}

func TestWebRevokeOtherPlayerSessions_RPCError(t *testing.T) {
	client := &mockCoreClient{
		revokeOtherErr: errors.New("core down"),
	}
	h := NewHandler(client)

	_, err := h.WebRevokeOtherPlayerSessions(context.Background(), requestWithToken(&webv1.WebRevokeOtherPlayerSessionsRequest{}, "tok-abc"))
	require.Error(t, err)
	var connectErr *connect.Error
	require.ErrorAs(t, err, &connectErr)
	assert.Equal(t, connect.CodeInternal, connectErr.Code())
}

// --- translateCharacterSummaries ---

func TestTranslateCharacterSummaries(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		assert.Nil(t, translateCharacterSummaries(nil))
	})

	t.Run("empty input", func(t *testing.T) {
		assert.Nil(t, translateCharacterSummaries([]*corev1.CharacterSummary{}))
	})

	t.Run("translates all fields", func(t *testing.T) {
		in := []*corev1.CharacterSummary{
			{
				CharacterId:      "c1",
				CharacterName:    "Alice",
				HasActiveSession: true,
				SessionStatus:    "active",
				LastLocation:     "Tavern",
				LastPlayedAt:     1234567890,
			},
		}
		out := translateCharacterSummaries(in)
		require.Len(t, out, 1)
		assert.Equal(t, "c1", out[0].GetCharacterId())
		assert.Equal(t, "Alice", out[0].GetCharacterName())
		assert.True(t, out[0].GetHasActiveSession())
		assert.Equal(t, "active", out[0].GetSessionStatus())
		assert.Equal(t, "Tavern", out[0].GetLastLocation())
		assert.Equal(t, int64(1234567890), out[0].GetLastPlayedAt())
	})
}

// --- CookieMiddleware ---

func TestCookieMiddleware_InjectsCookieAsHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(headerInjectSessionToken)
		assert.Equal(t, "my-token", token)
		w.WriteHeader(http.StatusOK)
	})

	handler := CookieMiddleware(false, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "my-token", HttpOnly: true, Secure: true})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestCookieMiddleware_NoCookieNoHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := r.Header.Get(headerInjectSessionToken)
		assert.Empty(t, token)
		w.WriteHeader(http.StatusOK)
	})

	handler := CookieMiddleware(false, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestCookieMiddleware_SetsSessionCookieFromSignalHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerSetSessionToken, "new-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := CookieMiddleware(false, inner)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, cookieName, cookies[0].Name)
	assert.Equal(t, "new-token", cookies[0].Value)
	assert.Equal(t, cookieMaxAge, cookies[0].MaxAge)

	// Signal header should be stripped
	assert.Empty(t, rr.Header().Get(headerSetSessionToken))
}

func TestCookieMiddleware_SetsSessionCookieShortLived(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerSetSessionToken, "short-token")
		w.WriteHeader(http.StatusOK)
	})

	handler := CookieMiddleware(true, inner)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "short-token", cookies[0].Value)
	assert.Equal(t, cookieMaxAge, cookies[0].MaxAge)
	assert.True(t, cookies[0].Secure)
}

func TestCookieMiddleware_ClearsSessionCookie(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerClearSession, "true")
		w.WriteHeader(http.StatusOK)
	})

	handler := CookieMiddleware(false, inner)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, cookieName, cookies[0].Name)
	assert.Equal(t, -1, cookies[0].MaxAge)

	assert.Empty(t, rr.Header().Get(headerClearSession))
}

func TestCookieMiddleware_NoSignalHeaders_NoCookie(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := CookieMiddleware(false, inner)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Empty(t, rr.Result().Cookies())
}

func TestCookieMiddleware_WriteWithoutExplicitWriteHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set(headerSetSessionToken, "implicit-token")
		_, _ = w.Write([]byte("OK"))
	})

	handler := CookieMiddleware(false, inner)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	cookies := rr.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "implicit-token", cookies[0].Value)
}
