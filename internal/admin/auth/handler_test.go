// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/pkg/errutil"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// fakeProvider is a hand-rolled OperatorAuthProvider for handler tests.
type fakeProvider struct {
	identity adminauth.OperatorIdentity
	err      error
	captured adminauth.AuthRequest
	calls    int
}

func (f *fakeProvider) Name() string { return "fake-provider" }

func (f *fakeProvider) Authenticate(_ context.Context, req adminauth.AuthRequest) (adminauth.OperatorIdentity, error) {
	f.calls++
	f.captured = req
	return f.identity, f.err
}

// adminClock returns a fakeClock at a fixed time. The fakeClock type
// is defined in session_test.go (same package adminauth_test).
func adminClock(_ *testing.T) adminauth.Clock {
	return &fakeClock{t: time.Unix(1700000000, 0)}
}

func TestAuthenticateHandlerHappyPath(t *testing.T) {
	provider := &fakeProvider{
		identity: adminauth.OperatorIdentity{
			PlayerID:         "01HZA00000000000000000000",
			TOTPVerified:     true,
			AuthProviderName: "ingame-creds-totp",
		},
	}
	sessions := adminauth.NewSessionStore(adminClock(t), 10*time.Minute)
	h := adminauth.NewAuthenticateHandler(provider, sessions)

	req := connect.NewRequest(&adminv1.AuthenticateRequest{
		Username: "alice",
		Password: "hunter2",
		TotpCode: "123456",
	})
	resp, err := h.Authenticate(context.Background(), req)
	require.NoError(t, err)
	assert.NotEmpty(t, resp.Msg.SessionToken)
	assert.Equal(t, "01HZA00000000000000000000", resp.Msg.PlayerId)
	require.NotNil(t, resp.Msg.ExpiresAt)
	assert.True(t, resp.Msg.ExpiresAt.AsTime().After(time.Unix(1700000000, 0)))
}

func TestAuthenticateHandlerSurfacesEachDENYCode(t *testing.T) {
	cases := []struct {
		denyCode    string
		connectCode connect.Code
	}{
		{"DENY_INVALID_CREDENTIALS", connect.CodeUnauthenticated},
		{"DENY_NOT_ENROLLED", connect.CodeFailedPrecondition},
		{"DENY_BAD_TOTP", connect.CodeUnauthenticated},
		{"DENY_LOCKED", connect.CodeUnavailable},
		{"DENY_NOT_OPERATOR", connect.CodePermissionDenied},
		{"DENY_NOT_ADMIN_ROLE", connect.CodePermissionDenied},
	}
	for _, tc := range cases {
		t.Run(tc.denyCode, func(t *testing.T) {
			denyErr := oops.Code(tc.denyCode).Errorf("denied")
			provider := &fakeProvider{err: denyErr}
			sessions := adminauth.NewSessionStore(adminClock(t), 10*time.Minute)
			h := adminauth.NewAuthenticateHandler(provider, sessions)

			req := connect.NewRequest(&adminv1.AuthenticateRequest{Username: "x", Password: "y", TotpCode: "z"})
			_, err := h.Authenticate(context.Background(), req)
			require.Error(t, err)
			var ce *connect.Error
			require.True(t, errors.As(err, &ce))
			assert.Equal(t, tc.connectCode, ce.Code())
			// Original oops code preserved in the error chain.
			errutil.AssertErrorCode(t, err, tc.denyCode)
		})
	}
}

func TestAuthenticateHandlerCapturesPeerCredIntoIdentity(t *testing.T) {
	provider := &fakeProvider{
		identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"},
	}
	sessions := adminauth.NewSessionStore(adminClock(t), 10*time.Minute)
	h := adminauth.NewAuthenticateHandler(provider, sessions)

	ctx := socket.WithPeerCred(context.Background(), socket.PeerCred{UID: 1001, GID: 100, PID: 4242})
	req := connect.NewRequest(&adminv1.AuthenticateRequest{Username: "alice", Password: "p", TotpCode: "c"})
	_, err := h.Authenticate(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, uint32(1001), provider.captured.PeerCred.UID)
	assert.Equal(t, uint32(100), provider.captured.PeerCred.GID)
	assert.Equal(t, int32(4242), provider.captured.PeerCred.PID)
}

func TestAuthenticateHandlerUnknownErrorMapsToInternal(t *testing.T) {
	provider := &fakeProvider{err: errors.New("plain error, no oops code")}
	sessions := adminauth.NewSessionStore(adminClock(t), 10*time.Minute)
	h := adminauth.NewAuthenticateHandler(provider, sessions)

	req := connect.NewRequest(&adminv1.AuthenticateRequest{Username: "a", Password: "p", TotpCode: "c"})
	_, err := h.Authenticate(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeInternal, ce.Code())
}
