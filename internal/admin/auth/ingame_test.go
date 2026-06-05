// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package adminauth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	adminauth "github.com/holomush/holomush/internal/admin/auth"
	"github.com/holomush/holomush/internal/admin/socket"
	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/totp"
)

// fakeCreds implements adminauth.CredentialValidator.
type fakeCreds struct {
	player *auth.Player
	err    error
	calls  int
}

func (f *fakeCreds) ValidateCredentials(_ context.Context, _, _ string) (*auth.Player, error) {
	f.calls++
	return f.player, f.err
}

// fakeTOTP implements adminauth.EnrollmentChecker.
type fakeTOTP struct {
	enrolled      bool
	enrolledErr   error
	verifyResult  totp.VerifyResult
	verifyErr     error
	enrolledCalls int
	verifyCalls   int
}

func (f *fakeTOTP) IsEnrolled(_ context.Context, _ ulid.ULID) (bool, error) {
	f.enrolledCalls++
	return f.enrolled, f.enrolledErr
}

func (f *fakeTOTP) Verify(_ context.Context, _ ulid.ULID, _ string) (totp.VerifyResult, error) {
	f.verifyCalls++
	return f.verifyResult, f.verifyErr
}

// fakeResolver implements access.SubjectResolver. The grant set is
// stored as []string and published under access.PlayerGrantsAttribute
// matching the pattern from internal/access/grants_test.go.
type fakeResolver struct {
	grants []string
	err    error
	calls  int
}

func (r *fakeResolver) ResolveSubjectAttributes(_ context.Context, _ string, _ string) (*types.AttributeBags, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	bags := &types.AttributeBags{
		Subject: map[string]any{},
	}
	if r.grants != nil {
		bags.Subject[access.PlayerGrantsAttribute] = r.grants
	}
	return bags, nil
}

// Compile-time assertion.
var _ access.SubjectResolver = (*fakeResolver)(nil)

// fakeRoles implements store.RoleStore.
type fakeRoles struct {
	playerRoles map[string][]string
	err         error
	calls       int
}

func (f *fakeRoles) GetRoles(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (f *fakeRoles) AddRole(_ context.Context, _, _ string) error           { return nil }
func (f *fakeRoles) RemoveRole(_ context.Context, _, _ string) error        { return nil }
func (f *fakeRoles) PlayerHasRole(_ context.Context, playerID, role string) (bool, error) {
	f.calls++
	if f.err != nil {
		return false, f.err
	}
	for _, r := range f.playerRoles[playerID] {
		if r == role {
			return true, nil
		}
	}
	return false, nil
}

func newProvider(t *testing.T, creds *fakeCreds, ttp *fakeTOTP, res *fakeResolver, roles *fakeRoles) *adminauth.InGameCredentialsProvider {
	t.Helper()
	p, err := adminauth.NewInGameCredentialsProvider(creds, ttp, res, roles)
	require.NoError(t, err)
	return p
}

func happyPlayer() *auth.Player {
	return &auth.Player{ID: ulid.Make(), Username: "alice"}
}

func happyAuthRequest() adminauth.AuthRequest {
	return adminauth.AuthRequest{Username: "alice", Password: "password1", TOTPCode: "123456"}
}

func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	require.Error(t, err)
	o, ok := oops.AsOops(err)
	require.True(t, ok, "expected oops error, got %T", err)
	assert.Equal(t, want, o.Code())
}

func TestInGameAuthenticateHappyPath(t *testing.T) {
	pl := happyPlayer()
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeOK}}
	res := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{pl.ID.String(): {access.RoleAdmin}}}
	p := newProvider(t, creds, ttp, res, roles)

	id, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.NoError(t, err)
	assert.Equal(t, pl.ID.String(), id.PlayerID)
	assert.True(t, id.TOTPVerified)
	assert.Equal(t, "ingame-creds-totp", id.AuthProviderName)
}

func TestInGameAuthenticateRejectsInvalidCredentials(t *testing.T) {
	creds := &fakeCreds{err: errors.New("bad credentials")}
	ttp := &fakeTOTP{}
	res := &fakeResolver{}
	roles := &fakeRoles{}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assertCode(t, err, "DENY_INVALID_CREDENTIALS")
	assert.Equal(t, 1, creds.calls)
	assert.Equal(t, 0, ttp.enrolledCalls, "step 2 must not execute")
	assert.Equal(t, 0, ttp.verifyCalls, "step 3 must not execute")
	assert.Equal(t, 0, res.calls, "step 4 must not execute")
	assert.Equal(t, 0, roles.calls, "step 5 must not execute")
}

func TestInGameAuthenticateRejectsNotEnrolled(t *testing.T) {
	pl := happyPlayer()
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolled: false}
	res := &fakeResolver{}
	roles := &fakeRoles{}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assertCode(t, err, "DENY_NOT_ENROLLED")
	assert.Equal(t, 0, ttp.verifyCalls, "step 3 must not execute")
}

func TestInGameAuthenticateRejectsBadTOTP(t *testing.T) {
	pl := happyPlayer()
	creds := &fakeCreds{player: pl}
	// OutcomeInvalidCode is a non-OK, non-Locked outcome that falls through to DENY_BAD_TOTP.
	ttp := &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeInvalidCode}}
	res := &fakeResolver{}
	roles := &fakeRoles{}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assertCode(t, err, "DENY_BAD_TOTP")
	assert.Equal(t, 0, res.calls, "later steps must not execute")
}

func TestInGameAuthenticateRejectsLocked(t *testing.T) {
	pl := happyPlayer()
	until := time.Unix(1700000060, 0)
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeLocked, LockedUntil: &until}}
	res := &fakeResolver{}
	roles := &fakeRoles{}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assertCode(t, err, "DENY_LOCKED")
}

func TestInGameAuthenticateRejectsNonOperator(t *testing.T) {
	pl := happyPlayer()
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeOK}}
	res := &fakeResolver{grants: []string{}} // no crypto.operator
	roles := &fakeRoles{}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assertCode(t, err, "DENY_NOT_OPERATOR")
	assert.Equal(t, 0, roles.calls, "step 5 must not execute")
}

func TestInGameAuthenticateRejectsPlayerWithoutAdminRole(t *testing.T) {
	pl := happyPlayer()
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeOK}}
	res := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{playerRoles: map[string][]string{}} // no admin role
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assertCode(t, err, "DENY_NOT_ADMIN_ROLE")
}

func TestInGameAuthenticateIgnoresPeerCredForGating(t *testing.T) {
	pl := happyPlayer()

	makeSetup := func(uid uint32) (*adminauth.InGameCredentialsProvider, adminauth.AuthRequest) {
		creds := &fakeCreds{player: pl}
		ttp := &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeOK}}
		res := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
		roles := &fakeRoles{playerRoles: map[string][]string{pl.ID.String(): {access.RoleAdmin}}}
		req := adminauth.AuthRequest{
			Username: "alice",
			Password: "password1",
			TOTPCode: "123456",
			PeerCred: socket.PeerCred{UID: uid, GID: 100, PID: 1234},
		}
		p, err := adminauth.NewInGameCredentialsProvider(creds, ttp, res, roles)
		require.NoError(t, err)
		return p, req
	}

	p1, req1 := makeSetup(1000)
	id1, err1 := p1.Authenticate(context.Background(), req1)
	require.NoError(t, err1)

	p2, req2 := makeSetup(2000)
	id2, err2 := p2.Authenticate(context.Background(), req2)
	require.NoError(t, err2)

	// Same logical inputs (creds, totp, capability, role) → both succeed
	// regardless of the differing PeerCred.UID. PeerCred surfaces in the
	// returned identity but does not influence the outcome (INV-CRYPTO-71).
	assert.NotEqual(t, id1.PeerCred.UID, id2.PeerCred.UID, "peer creds preserved through to identity")
	assert.Equal(t, pl.ID.String(), id1.PlayerID)
	assert.Equal(t, pl.ID.String(), id2.PlayerID)
}

// TestNewInGameCredentialsProviderRejectsNilDeps covers the four nil-guard
// branches in NewInGameCredentialsProvider: each constructor argument that
// must be non-nil is rejected with a typed code.
func TestNewInGameCredentialsProviderRejectsNilDeps(t *testing.T) {
	creds := &fakeCreds{}
	ttp := &fakeTOTP{}
	res := &fakeResolver{}
	roles := &fakeRoles{}

	tests := []struct {
		name     string
		creds    adminauth.CredentialValidator
		totp     adminauth.EnrollmentChecker
		resolver access.SubjectResolver
		roles    store.RoleStore
		wantCode string
	}{
		{name: "nil_creds", creds: nil, totp: ttp, resolver: res, roles: roles, wantCode: "INGAME_NIL_CREDS"},
		{name: "nil_totp", creds: creds, totp: nil, resolver: res, roles: roles, wantCode: "INGAME_NIL_TOTP"},
		{name: "nil_resolver", creds: creds, totp: ttp, resolver: nil, roles: roles, wantCode: "INGAME_NIL_RESOLVER"},
		{name: "nil_roles", creds: creds, totp: ttp, resolver: res, roles: nil, wantCode: "INGAME_NIL_ROLESTORE"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := adminauth.NewInGameCredentialsProvider(tc.creds, tc.totp, tc.resolver, tc.roles)
			require.Error(t, err)
			o, ok := oops.AsOops(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, o.Code())
		})
	}
}

// TestInGameAuthenticateWrapsTOTPLookupError covers the infrastructure
// error path INGAME_TOTP_LOOKUP_FAILED (step 2 returned err): the inner
// error is wrapped, not swallowed, and step 3 must not execute.
func TestInGameAuthenticateWrapsTOTPLookupError(t *testing.T) {
	pl := happyPlayer()
	infra := errors.New("totp store unavailable")
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolledErr: infra}
	res := &fakeResolver{}
	roles := &fakeRoles{}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assert.ErrorIs(t, err, infra)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INGAME_TOTP_LOOKUP_FAILED", o.Code())
	assert.Equal(t, 0, ttp.verifyCalls, "step 3 must not execute on step 2 infra error")
}

// TestInGameAuthenticateWrapsTOTPVerifyError covers the infrastructure
// error path INGAME_TOTP_VERIFY_FAILED (step 3 returned err): the inner
// error is wrapped, and steps 4-5 must not execute.
func TestInGameAuthenticateWrapsTOTPVerifyError(t *testing.T) {
	pl := happyPlayer()
	infra := errors.New("totp verify backend down")
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolled: true, verifyErr: infra}
	res := &fakeResolver{}
	roles := &fakeRoles{}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assert.ErrorIs(t, err, infra)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INGAME_TOTP_VERIFY_FAILED", o.Code())
	assert.Equal(t, 0, res.calls, "step 4 must not execute on step 3 infra error")
	assert.Equal(t, 0, roles.calls, "step 5 must not execute on step 3 infra error")
}

// TestInGameAuthenticateWrapsResolverError covers the
// INGAME_GRANT_LOOKUP_FAILED path bubbling out of step 4 via
// AssertOperatorAdmin. Step 5 must not execute.
func TestInGameAuthenticateWrapsResolverError(t *testing.T) {
	pl := happyPlayer()
	infra := errors.New("attribute store unavailable")
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeOK}}
	res := &fakeResolver{err: infra}
	roles := &fakeRoles{}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assert.ErrorIs(t, err, infra)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INGAME_GRANT_LOOKUP_FAILED", o.Code())
	assert.Equal(t, 0, roles.calls, "step 5 must not execute on step 4 infra error")
}

// TestInGameAuthenticateWrapsRoleStoreError covers the
// INGAME_ROLE_LOOKUP_FAILED path bubbling out of step 5 via
// AssertOperatorAdmin.
func TestInGameAuthenticateWrapsRoleStoreError(t *testing.T) {
	pl := happyPlayer()
	infra := errors.New("role store unavailable")
	creds := &fakeCreds{player: pl}
	ttp := &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeOK}}
	res := &fakeResolver{grants: []string{access.CapabilityCryptoOperator}}
	roles := &fakeRoles{err: infra}
	p := newProvider(t, creds, ttp, res, roles)

	_, err := p.Authenticate(context.Background(), happyAuthRequest())
	require.Error(t, err)
	assert.ErrorIs(t, err, infra)
	o, ok := oops.AsOops(err)
	require.True(t, ok)
	assert.Equal(t, "INGAME_ROLE_LOOKUP_FAILED", o.Code())
}

func TestInGameAuthenticateStepOrderFixedOnFailure(t *testing.T) {
	pl := happyPlayer()

	tests := []struct {
		name       string
		setupCreds func() *fakeCreds
		setupTOTP  func() *fakeTOTP
		setupRes   func() *fakeResolver
		setupRoles func() *fakeRoles
		wantCode   string
		wantStep1  bool
		wantStep2  bool
		wantStep3  bool
		wantStep4  bool
		wantStep5  bool
	}{
		{
			name:       "step1_invalid_creds",
			setupCreds: func() *fakeCreds { return &fakeCreds{err: errors.New("bad")} },
			setupTOTP:  func() *fakeTOTP { return &fakeTOTP{} },
			setupRes:   func() *fakeResolver { return &fakeResolver{} },
			setupRoles: func() *fakeRoles { return &fakeRoles{} },
			wantCode:   "DENY_INVALID_CREDENTIALS",
			wantStep1:  true,
		},
		{
			name:       "step2_not_enrolled",
			setupCreds: func() *fakeCreds { return &fakeCreds{player: pl} },
			setupTOTP:  func() *fakeTOTP { return &fakeTOTP{enrolled: false} },
			setupRes:   func() *fakeResolver { return &fakeResolver{} },
			setupRoles: func() *fakeRoles { return &fakeRoles{} },
			wantCode:   "DENY_NOT_ENROLLED",
			wantStep1:  true, wantStep2: true,
		},
		{
			name:       "step3_bad_totp",
			setupCreds: func() *fakeCreds { return &fakeCreds{player: pl} },
			setupTOTP: func() *fakeTOTP {
				return &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeInvalidCode}}
			},
			setupRes:   func() *fakeResolver { return &fakeResolver{} },
			setupRoles: func() *fakeRoles { return &fakeRoles{} },
			wantCode:   "DENY_BAD_TOTP",
			wantStep1:  true, wantStep2: true, wantStep3: true,
		},
		{
			name:       "step4_not_operator",
			setupCreds: func() *fakeCreds { return &fakeCreds{player: pl} },
			setupTOTP: func() *fakeTOTP {
				return &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeOK}}
			},
			setupRes:   func() *fakeResolver { return &fakeResolver{grants: []string{}} },
			setupRoles: func() *fakeRoles { return &fakeRoles{} },
			wantCode:   "DENY_NOT_OPERATOR",
			wantStep1:  true, wantStep2: true, wantStep3: true, wantStep4: true,
		},
		{
			name:       "step5_not_admin",
			setupCreds: func() *fakeCreds { return &fakeCreds{player: pl} },
			setupTOTP: func() *fakeTOTP {
				return &fakeTOTP{enrolled: true, verifyResult: totp.VerifyResult{Outcome: totp.OutcomeOK}}
			},
			setupRes:   func() *fakeResolver { return &fakeResolver{grants: []string{access.CapabilityCryptoOperator}} },
			setupRoles: func() *fakeRoles { return &fakeRoles{playerRoles: map[string][]string{}} },
			wantCode:   "DENY_NOT_ADMIN_ROLE",
			wantStep1:  true, wantStep2: true, wantStep3: true, wantStep4: true, wantStep5: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			creds := tc.setupCreds()
			ttp := tc.setupTOTP()
			res := tc.setupRes()
			roles := tc.setupRoles()
			p := newProvider(t, creds, ttp, res, roles)

			_, err := p.Authenticate(context.Background(), happyAuthRequest())
			require.Error(t, err)
			assertCode(t, err, tc.wantCode)

			assert.Equal(t, tc.wantStep1, creds.calls > 0, "step1 creds called")
			assert.Equal(t, tc.wantStep2, ttp.enrolledCalls > 0, "step2 IsEnrolled called")
			assert.Equal(t, tc.wantStep3, ttp.verifyCalls > 0, "step3 Verify called")
			assert.Equal(t, tc.wantStep4, res.calls > 0, "step4 resolver called")
			assert.Equal(t, tc.wantStep5, roles.calls > 0, "step5 roleStore called")
		})
	}
}
