// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "github.com/holomush/holomush/pkg/proto/holomush/core/v1"
	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// --- WebAuthenticatePlayer ---

func TestWebAuthenticatePlayer_Success(t *testing.T) {
	client := &mockCoreClient{
		authPlayerResp: &corev1.AuthenticatePlayerResponse{
			Success:     true,
			PlayerToken: "tok-abc",
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
	assert.Equal(t, "tok-abc", resp.Msg.GetPlayerToken())
	assert.Len(t, resp.Msg.GetCharacters(), 1)
	assert.Equal(t, "Alice", resp.Msg.GetCharacters()[0].GetCharacterName())
	assert.Equal(t, "c1", resp.Msg.GetDefaultCharacterId())
	assert.Equal(t, "tok-abc", resp.Header().Get(headerSetSessionToken))
	assert.Equal(t, "true", resp.Header().Get(headerRememberMe))
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
			Success:     true,
			PlayerToken: "tok-short",
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
	assert.Empty(t, resp.Header().Get(headerRememberMe))
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

	resp, err := h.WebSelectCharacter(context.Background(), connect.NewRequest(&webv1.WebSelectCharacterRequest{
		PlayerToken: "tok-abc",
		CharacterId: "c1",
	}))
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

	resp, err := h.WebSelectCharacter(context.Background(), connect.NewRequest(&webv1.WebSelectCharacterRequest{
		PlayerToken: "tok-abc",
		CharacterId: "c2",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetReattached())
}

func TestWebSelectCharacter_RPCError(t *testing.T) {
	client := &mockCoreClient{
		selectCharErr: errors.New("timeout"),
	}
	h := NewHandler(client)

	resp, err := h.WebSelectCharacter(context.Background(), connect.NewRequest(&webv1.WebSelectCharacterRequest{
		PlayerToken: "tok-abc",
		CharacterId: "c1",
	}))
	require.NoError(t, err)
	assert.False(t, resp.Msg.GetSuccess())
	assert.Equal(t, "character selection error", resp.Msg.GetErrorMessage())
}

// --- WebCreatePlayer ---

func TestWebCreatePlayer_Success(t *testing.T) {
	client := &mockCoreClient{
		createPlayerResp: &corev1.CreatePlayerResponse{
			Success:     true,
			PlayerToken: "tok-new",
			Characters:  []*corev1.CharacterSummary{},
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
	assert.Equal(t, "tok-new", resp.Msg.GetPlayerToken())
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

	resp, err := h.WebCreateCharacter(context.Background(), connect.NewRequest(&webv1.WebCreateCharacterRequest{
		PlayerToken:   "tok-abc",
		CharacterName: "NewChar",
	}))
	require.NoError(t, err)
	assert.True(t, resp.Msg.GetSuccess())
	assert.Equal(t, "char-new", resp.Msg.GetCharacterId())
	assert.Equal(t, "NewChar", resp.Msg.GetCharacterName())
}

func TestWebCreateCharacter_RPCError(t *testing.T) {
	client := &mockCoreClient{
		createCharErr: errors.New("timeout"),
	}
	h := NewHandler(client)

	resp, err := h.WebCreateCharacter(context.Background(), connect.NewRequest(&webv1.WebCreateCharacterRequest{
		PlayerToken:   "tok-abc",
		CharacterName: "Char",
	}))
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

	resp, err := h.WebListCharacters(context.Background(), connect.NewRequest(&webv1.WebListCharactersRequest{
		PlayerToken: "tok-abc",
	}))
	require.NoError(t, err)
	assert.Len(t, resp.Msg.GetCharacters(), 2)
	assert.Equal(t, "Alice", resp.Msg.GetCharacters()[0].GetCharacterName())
	assert.True(t, resp.Msg.GetCharacters()[0].GetHasActiveSession())
}

func TestWebListCharacters_RPCError(t *testing.T) {
	client := &mockCoreClient{
		listCharsErr: errors.New("unavailable"),
	}
	h := NewHandler(client)

	resp, err := h.WebListCharacters(context.Background(), connect.NewRequest(&webv1.WebListCharactersRequest{
		PlayerToken: "tok-abc",
	}))
	require.NoError(t, err)
	assert.Empty(t, resp.Msg.GetCharacters())
}

// --- WebLogout ---

func TestWebLogout_Success(t *testing.T) {
	client := &mockCoreClient{
		logoutResp: &corev1.LogoutResponse{},
	}
	h := NewHandler(client)

	resp, err := h.WebLogout(context.Background(), connect.NewRequest(&webv1.WebLogoutRequest{
		SessionId: "sess-123",
	}))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
	assert.Equal(t, "true", resp.Header().Get(headerClearSession))
}

func TestWebLogout_RPCError(t *testing.T) {
	client := &mockCoreClient{
		logoutErr: errors.New("core down"),
	}
	h := NewHandler(client)

	resp, err := h.WebLogout(context.Background(), connect.NewRequest(&webv1.WebLogoutRequest{
		SessionId: "sess-123",
	}))
	require.NoError(t, err)
	assert.NotNil(t, resp.Msg)
	assert.Equal(t, "true", resp.Header().Get(headerClearSession), "cookie should be cleared even on RPC error")
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
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "my-token"})
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
		w.Header().Set(headerRememberMe, "true")
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
	assert.Equal(t, cookieMaxAgeLong, cookies[0].MaxAge)

	// Signal headers should be stripped
	assert.Empty(t, rr.Header().Get(headerSetSessionToken))
	assert.Empty(t, rr.Header().Get(headerRememberMe))
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
