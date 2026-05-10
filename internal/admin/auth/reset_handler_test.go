// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/internal/totp"
	adminv1 "github.com/holomush/holomush/pkg/proto/holomush/admin/v1"
)

// fakeResetSessions is a hand-rolled SessionStore for ResetTOTPHandler tests.
// Kept separate from the real session store to test the handler in isolation.
type fakeResetSessions struct {
	identity adminauth.OperatorIdentity
	err      error
}

func (f *fakeResetSessions) Issue(_ adminauth.OperatorIdentity) (string, time.Time, error) {
	return "", time.Time{}, errors.New("not used in reset handler tests")
}

func (f *fakeResetSessions) Get(_ string) (adminauth.OperatorIdentity, error) {
	return f.identity, f.err
}
func (f *fakeResetSessions) Revoke(_ string) error { return nil }

// fakeClearTOTPCaller is the narrow fake for ClearTOTPCaller.
type fakeClearTOTPCaller struct {
	res        totp.ClearResult
	err        error
	receivedID ulid.ULID
	receivedBy totp.ClearReason
	calls      int
}

func (f *fakeClearTOTPCaller) ClearTOTP(_ context.Context, pid ulid.ULID, by totp.ClearReason) (totp.ClearResult, error) {
	f.calls++
	f.receivedID = pid
	f.receivedBy = by
	return f.res, f.err
}

// newResetHandler builds a ResetTOTPHandler from the per-test fakes.
// fakeResolver and fakeRoles are defined in ingame_test.go (same package).
func newResetHandler(sessions *fakeResetSessions, resolver *fakeResolver, roles *fakeRoles, totpSvc *fakeClearTOTPCaller) *adminauth.ResetTOTPHandler {
	return adminauth.NewResetTOTPHandler(sessions, resolver, roles, totpSvc)
}

// --- tests ---

func TestResetTOTPHandlerRequiresValidSession(t *testing.T) {
	sessions := &fakeResetSessions{err: oops.Code("DENY_SESSION_INVALID").Errorf("nope")}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{"any": {access.RoleAdmin}}}
	totpSvc := &fakeClearTOTPCaller{}
	h := newResetHandler(sessions, resolver, roles, totpSvc)

	targetID := ulid.Make().String()
	req := connect.NewRequest(&adminv1.ResetTOTPRequest{
		SessionToken:   "bad-token",
		TargetPlayerId: targetID,
	})
	_, err := h.ResetTOTP(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeUnauthenticated, ce.Code())
	assert.Equal(t, 0, totpSvc.calls)
}

func TestResetTOTPRequiresCapabilityOnHandler(t *testing.T) {
	sessions := &fakeResetSessions{
		identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"},
	}
	resolver := &fakeResolver{grants: []string{}} // no crypto.operator cap
	roles := &fakeRoles{playerRoles: map[string][]string{"01HZA00000000000000000000": {access.RoleAdmin}}}
	totpSvc := &fakeClearTOTPCaller{}
	h := newResetHandler(sessions, resolver, roles, totpSvc)

	targetID := ulid.Make().String()
	req := connect.NewRequest(&adminv1.ResetTOTPRequest{
		SessionToken:   "tk",
		TargetPlayerId: targetID,
	})
	_, err := h.ResetTOTP(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodePermissionDenied, ce.Code())
	assert.Equal(t, 0, totpSvc.calls)
}

func TestResetTOTPRequiresAdminRoleOnHandler(t *testing.T) {
	sessions := &fakeResetSessions{
		identity: adminauth.OperatorIdentity{PlayerID: "01HZA00000000000000000000"},
	}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{}} // no admin role
	totpSvc := &fakeClearTOTPCaller{}
	h := newResetHandler(sessions, resolver, roles, totpSvc)

	targetID := ulid.Make().String()
	req := connect.NewRequest(&adminv1.ResetTOTPRequest{
		SessionToken:   "tk",
		TargetPlayerId: targetID,
	})
	_, err := h.ResetTOTP(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodePermissionDenied, ce.Code())
	assert.Equal(t, 0, totpSvc.calls)
}

func TestResetTOTPHandlerCallsClearTOTPThroughDecorator(t *testing.T) {
	operatorID := "01HZA00000000000000000000"
	sessions := &fakeResetSessions{
		identity: adminauth.OperatorIdentity{PlayerID: operatorID},
	}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{operatorID: {access.RoleAdmin}}}
	targetID := ulid.Make()
	totpSvc := &fakeClearTOTPCaller{
		res: totp.ClearResult{
			WasEnrolled:    true,
			AuditClearedAt: time.Unix(1700000000, 0),
			ClearedBy:      totp.ClearReasonAdminReset,
		},
	}
	h := newResetHandler(sessions, resolver, roles, totpSvc)

	req := connect.NewRequest(&adminv1.ResetTOTPRequest{
		SessionToken:   "tk",
		TargetPlayerId: targetID.String(),
	})
	resp, err := h.ResetTOTP(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Msg.Cleared, "Cleared must reflect WasEnrolled=true")
	assert.Equal(t, 1, totpSvc.calls, "ClearTOTP must be called exactly once")
	assert.Equal(t, targetID, totpSvc.receivedID, "ClearTOTP must receive the parsed target ULID")
	assert.Equal(t, totp.ClearReasonAdminReset, totpSvc.receivedBy, "ClearTOTP must receive ClearReasonAdminReset")
}

func TestResetTOTPHandlerRejectsInvalidTargetPlayerID(t *testing.T) {
	operatorID := "01HZA00000000000000000000"
	sessions := &fakeResetSessions{
		identity: adminauth.OperatorIdentity{PlayerID: operatorID},
	}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{operatorID: {access.RoleAdmin}}}
	totpSvc := &fakeClearTOTPCaller{}
	h := newResetHandler(sessions, resolver, roles, totpSvc)

	req := connect.NewRequest(&adminv1.ResetTOTPRequest{
		SessionToken:   "tk",
		TargetPlayerId: "not-a-ulid",
	})
	_, err := h.ResetTOTP(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeInvalidArgument, ce.Code())
	assert.Equal(t, 0, totpSvc.calls)
}

// TestResetTOTPHandlerRejectsZeroTargetPlayerID asserts that the all-zero
// ULID (00000000000000000000000000) — accepted by ulid.Parse but a sentinel
// that must never identify a real player — is rejected with
// connect.CodeInvalidArgument and never forwarded to ClearTOTP. Mirrors the
// Approve handler's all-zero request_id rejection.
func TestResetTOTPHandlerRejectsZeroTargetPlayerID(t *testing.T) {
	operatorID := "01HZA00000000000000000000"
	sessions := &fakeResetSessions{
		identity: adminauth.OperatorIdentity{PlayerID: operatorID},
	}
	resolver := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{operatorID: {access.RoleAdmin}}}
	totpSvc := &fakeClearTOTPCaller{}
	h := newResetHandler(sessions, resolver, roles, totpSvc)

	req := connect.NewRequest(&adminv1.ResetTOTPRequest{
		SessionToken:   "tk",
		TargetPlayerId: "00000000000000000000000000",
	})
	_, err := h.ResetTOTP(context.Background(), req)
	require.Error(t, err)
	var ce *connect.Error
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, connect.CodeInvalidArgument, ce.Code())
	assert.Equal(t, 0, totpSvc.calls, "ClearTOTP must not be invoked for zero ULID")
}
