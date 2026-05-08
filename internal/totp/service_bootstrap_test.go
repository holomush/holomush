// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	kekMocks "github.com/holomush/holomush/internal/eventbus/crypto/kek/mocks"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/internal/totp/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

func newBootstrapFixture(t *testing.T, fakeNow time.Time) (totp.Service, *mocks.MockRepository, *kekMocks.MockProvider) {
	t.Helper()
	repo := mocks.NewMockRepository(t)
	kp := kekMocks.NewMockProvider(t)
	clk := totp.NewFakeClock(fakeNow)
	hasher := auth.NewArgon2idHasher()
	svc, err := totp.NewService(totp.Config{GameID: "default"}, repo, kp, clk, hasher)
	require.NoError(t, err)
	return svc, repo, kp
}

// INV-A13: refuses unknown player.
func TestBootstrapEnrollRefusesUnknownPlayer(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(false, nil)
	_, err := svc.BootstrapEnroll(context.Background(), pid)
	assert.ErrorContains(t, err, "player not found")
}

// INV-A1: refuses after first success.
func TestBootstrapEnrollRefusesAfterFirstSuccess(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("w"), "kek-v1", nil)
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(), mock.Anything).
		Return(totp.ErrBootstrapAlreadyConsumed)
	_, err := svc.BootstrapEnroll(context.Background(), pid)
	errutil.AssertErrorCode(t, err, "TOTP_BOOTSTRAP_CONSUMED")
}

// INV-A9: KEK-wrapped secret.
func TestBootstrapEnrollWrapsSecretWithKEK(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	wrapped := []byte("wrapped-bytes")
	kp.On("Wrap", mock.Anything, mock.Anything).Return(wrapped, "kek-v1", nil)
	var captured totp.EnrollmentRecord
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(),
		mock.MatchedBy(func(r totp.EnrollmentRecord) bool { captured = r; return true })).
		Return(nil)
	res, err := svc.BootstrapEnroll(context.Background(), pid)
	require.NoError(t, err)
	assert.Equal(t, wrapped, captured.WrappedSecret)
	assert.Equal(t, "kek-v1", captured.WrapKeyID)
	assert.NotEmpty(t, res.Enrollment.Secret)
}

// INV-A11: recovery codes Argon2id-hashed.
func TestBootstrapEnrollHashesRecoveryCodes(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("w"), "kek-v1", nil)
	var captured totp.EnrollmentRecord
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(),
		mock.MatchedBy(func(r totp.EnrollmentRecord) bool { captured = r; return true })).
		Return(nil)
	res, err := svc.BootstrapEnroll(context.Background(), pid)
	require.NoError(t, err)
	require.Len(t, res.Enrollment.RecoveryCodes, 10)
	require.Len(t, captured.RecoveryCodes, 10)
	hasher := auth.NewArgon2idHasher()
	for i, h := range captured.RecoveryCodes {
		raw := res.Enrollment.RecoveryCodes[i]
		assert.NotEqual(t, raw, h.CodeHash)
		ok, _ := hasher.Verify(raw, h.CodeHash)
		assert.True(t, ok)
	}
}

// INV-A15: any error in BootstrapEnrollAtomic propagates; no partial state.
// (Real-PG rollback verified at the repo layer in T5; here we verify the
// service propagates the error without further partial writes.)
func TestBootstrapEnrollPropagatesAtomicError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("w"), "kek-v1", nil)
	want := errors.New("pg insert failed")
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(), mock.Anything).Return(want)
	_, err := svc.BootstrapEnroll(context.Background(), pid)
	assert.ErrorIs(t, err, want)
}

// Result-struct metadata population (replaces retired INV-A10 audit-emit tests).
func TestBootstrapResultCarriesAuditMetadata(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("w"), "kek-v1", nil)
	repo.On("BootstrapEnrollAtomic", mock.Anything, "totp_v1", pid.String(), mock.Anything).Return(nil)
	res, err := svc.BootstrapEnroll(context.Background(), pid)
	require.NoError(t, err)
	assert.Equal(t, now, res.AuditConsumedAt)
	assert.Equal(t, pid, res.AuditPlayerID)
	assert.Equal(t, "totp_v1", res.BootstrapKey)
}
