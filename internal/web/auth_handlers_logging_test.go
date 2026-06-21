// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package web

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	webv1 "github.com/holomush/holomush/pkg/proto/holomush/web/v1"
)

// captureSingleHandlerLog runs invoke with slog.Default() redirected to an
// in-memory Info-level JSON handler — which suppresses the entry-DebugContext
// line, leaving exactly the one ERROR record a failing-RPC handler emits — and
// returns that record decoded. It fails the test if the handler emits anything
// other than exactly one JSON line (json.Unmarshal rejects trailing objects).
func captureSingleHandlerLog(t *testing.T, invoke func()) map[string]any {
	t.Helper()

	orig := slog.Default()
	var buf bytes.Buffer
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })

	invoke()

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry),
		"handler must emit exactly one structured JSON log line; got: %q", buf.String())
	return entry
}

// TestWebAuthHandlersLogRPCErrorsWithStructuredOopsFields verifies that every
// auth/character BFF handler logs core-RPC failures via errutil.LogErrorContext
// (not a bare slog.ErrorContext), which extracts the oops error's Code() and
// Context() as TOP-LEVEL structured attributes for Loki/Sentry correlation.
//
// The discriminator is the top-level "code" attr: errutil.oopsAttrs emits it
// flat, whereas slog.ErrorContext(ctx, msg, "error", err) renders the oops
// LogValuer as a nested group under the (non-empty) "error" key, leaving
// "code" absent at top level. Asserting the flat "code" pins the migration.
func TestWebAuthHandlersLogRPCErrorsWithStructuredOopsFields(t *testing.T) {
	const wantCode = "WEB_AUTH_RPC_ERR"

	tests := []struct {
		name    string
		wantMsg string
		setErr  func(m *mockCoreClient, err error)
		invoke  func(h *Handler)
	}{
		{
			name:    "WebAuthenticatePlayer",
			wantMsg: "web: authenticate player RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.authPlayerErr = err },
			invoke: func(h *Handler) {
				// No session-token header: checkCookieCollision is a no-op, so
				// execution reaches the AuthenticatePlayer RPC.
				_, _ = h.WebAuthenticatePlayer(context.Background(),
					connect.NewRequest(&webv1.WebAuthenticatePlayerRequest{Username: "u", Password: "p"}))
			},
		},
		{
			name:    "WebSelectCharacter",
			wantMsg: "web: select character RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.selectCharErr = err },
			invoke: func(h *Handler) {
				req := connect.NewRequest(&webv1.WebSelectCharacterRequest{CharacterId: "char-1"})
				req.Header().Set(HeaderInjectSessionToken, "tok")
				_, _ = h.WebSelectCharacter(context.Background(), req)
			},
		},
		{
			name:    "WebCreatePlayer",
			wantMsg: "web: create player RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.createPlayerErr = err },
			invoke: func(h *Handler) {
				_, _ = h.WebCreatePlayer(context.Background(),
					connect.NewRequest(&webv1.WebCreatePlayerRequest{Username: "u", Password: "p"}))
			},
		},
		{
			name:    "WebCreateCharacter",
			wantMsg: "web: create character RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.createCharErr = err },
			invoke: func(h *Handler) {
				req := connect.NewRequest(&webv1.WebCreateCharacterRequest{CharacterName: "n"})
				req.Header().Set(HeaderInjectSessionToken, "tok")
				_, _ = h.WebCreateCharacter(context.Background(), req)
			},
		},
		{
			name:    "WebListCharacters",
			wantMsg: "web: list characters RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.listCharsErr = err },
			invoke: func(h *Handler) {
				req := connect.NewRequest(&webv1.WebListCharactersRequest{})
				req.Header().Set(HeaderInjectSessionToken, "tok")
				_, _ = h.WebListCharacters(context.Background(), req)
			},
		},
		{
			name:    "WebLogout",
			wantMsg: "web: logout RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.logoutErr = err },
			invoke: func(h *Handler) {
				// A non-empty token is required for WebLogout to call the
				// Logout RPC (and thus reach the error-log path).
				req := connect.NewRequest(&webv1.WebLogoutRequest{})
				req.Header().Set(HeaderInjectSessionToken, "tok")
				_, _ = h.WebLogout(context.Background(), req)
			},
		},
		{
			name:    "WebRequestPasswordReset",
			wantMsg: "web: request password reset RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.reqPwResetErr = err },
			invoke: func(h *Handler) {
				_, _ = h.WebRequestPasswordReset(context.Background(),
					connect.NewRequest(&webv1.WebRequestPasswordResetRequest{Email: "e@example.com"}))
			},
		},
		{
			name:    "WebConfirmPasswordReset",
			wantMsg: "web: confirm password reset RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.confirmPwResetErr = err },
			invoke: func(h *Handler) {
				_, _ = h.WebConfirmPasswordReset(context.Background(),
					connect.NewRequest(&webv1.WebConfirmPasswordResetRequest{Token: "t", NewPassword: "p"}))
			},
		},
		{
			name:    "WebCreateGuest",
			wantMsg: "web: create guest RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.createGuestErr = err },
			invoke: func(h *Handler) {
				// No session-token header: checkCookieCollision is a no-op.
				_, _ = h.WebCreateGuest(context.Background(),
					connect.NewRequest(&webv1.WebCreateGuestRequest{}))
			},
		},
		{
			name:    "WebListPlayerSessions",
			wantMsg: "web: list player sessions RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.listSessionsErr = err },
			invoke: func(h *Handler) {
				req := connect.NewRequest(&webv1.WebListPlayerSessionsRequest{})
				req.Header().Set(HeaderInjectSessionToken, "tok")
				_, _ = h.WebListPlayerSessions(context.Background(), req)
			},
		},
		{
			name:    "WebRevokePlayerSession",
			wantMsg: "web: revoke player session RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.revokeSessionErr = err },
			invoke: func(h *Handler) {
				req := connect.NewRequest(&webv1.WebRevokePlayerSessionRequest{TargetSessionId: "sess-x"})
				req.Header().Set(HeaderInjectSessionToken, "tok")
				_, _ = h.WebRevokePlayerSession(context.Background(), req)
			},
		},
		{
			name:    "WebRevokeOtherPlayerSessions",
			wantMsg: "web: revoke other player sessions RPC failed",
			setErr:  func(m *mockCoreClient, err error) { m.revokeOtherErr = err },
			invoke: func(h *Handler) {
				req := connect.NewRequest(&webv1.WebRevokeOtherPlayerSessionsRequest{})
				req.Header().Set(HeaderInjectSessionToken, "tok")
				_, _ = h.WebRevokeOtherPlayerSessions(context.Background(), req)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" logs the core error with a top-level oops code", func(t *testing.T) {
			client := &mockCoreClient{}
			tt.setErr(client, oops.Code(wantCode).Errorf("core unreachable"))
			h := NewHandler(client)

			entry := captureSingleHandlerLog(t, func() { tt.invoke(h) })

			assert.Equal(t, "ERROR", entry["level"])
			assert.Equal(t, tt.wantMsg, entry["msg"])
			assert.Equal(t, wantCode, entry["code"],
				"oops code must be a top-level attr (errutil.LogErrorContext), not nested under error")
		})
	}
}
