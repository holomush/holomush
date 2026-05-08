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

	"github.com/holomush/holomush/internal/totp"
)

// INV-A6 propagation: ConsumeRecoveryCode surfaces ErrInvalidRecoveryCode
// unchanged so callers can branch on it.
func TestConsumeRecoveryCodePropagatesInvalidCode(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()

	repo.EXPECT().
		ConsumeRecoveryCode(mock.Anything, pid.String(), "bad-code", mock.Anything, now).
		Return(ulid.ULID{}, totp.ErrInvalidRecoveryCode)

	_, err := svc.ConsumeRecoveryCode(context.Background(), pid, "bad-code")
	assert.ErrorIs(t, err, totp.ErrInvalidRecoveryCode)
}

func TestConsumeRecoveryCodeReturnsConsumedID(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	consumedID := ulid.Make()

	repo.EXPECT().
		ConsumeRecoveryCode(mock.Anything, pid.String(), "good-code", mock.Anything, now).
		Return(consumedID, nil)

	res, err := svc.ConsumeRecoveryCode(context.Background(), pid, "good-code")
	require.NoError(t, err)
	assert.Equal(t, consumedID, res.RecoveryCodeID)
	assert.Equal(t, now, res.AuditConsumedAt)
	assert.Equal(t, pid, res.AuditPlayerID)
}

func TestConsumeRecoveryCodePropagatesRepoError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	want := errors.New("pg connection lost")

	repo.EXPECT().
		ConsumeRecoveryCode(mock.Anything, pid.String(), "any", mock.Anything, now).
		Return(ulid.ULID{}, want)

	_, err := svc.ConsumeRecoveryCode(context.Background(), pid, "any")
	assert.ErrorIs(t, err, want)
}

// INV-A7 (repo-level deletion) propagation + INV-A8 (no bootstrap-state mutation):
// ClearTOTP delegates strictly to repo.ClearEnrollment. The mockery EXPECT
// pattern (with the `repo := mocks.NewMockRepository(t)` constructor) fails
// the test if any unexpected method is called — so the absence of a
// BootstrapClaim / BootstrapEnrollAtomic expectation is the proof of INV-A8.
func TestClearTOTPDelegatesToRepoAndPopulatesResult(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()

	repo.EXPECT().
		ClearEnrollment(mock.Anything, pid.String()).
		Return(true, nil)

	res, err := svc.ClearTOTP(context.Background(), pid, totp.ClearReasonRecoveryCode)
	require.NoError(t, err)
	assert.Equal(t, totp.ClearReasonRecoveryCode, res.ClearedBy)
	assert.Equal(t, now, res.AuditClearedAt)
	assert.Equal(t, pid, res.AuditPlayerID)
	assert.True(t, res.WasEnrolled)
}

// RecoverAndClear delegates to repo.RecoverAndClearAtomic and packs the
// audit metadata for both events. INV-A6 (recovery single-use) and
// INV-A7 (clear deletes both tables) hold jointly under the shared txn.
func TestRecoverAndClearDelegatesAtomic(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	consumedID := ulid.Make()

	repo.EXPECT().
		RecoverAndClearAtomic(mock.Anything, pid.String(), "good-code", mock.Anything, now).
		Return(consumedID, true, nil)

	res, err := svc.RecoverAndClear(context.Background(), pid, "good-code")
	require.NoError(t, err)
	assert.Equal(t, consumedID, res.RecoveryCodeID)
	assert.True(t, res.WasEnrolled)
	assert.Equal(t, now, res.AuditConsumedAt)
	assert.Equal(t, now, res.AuditClearedAt)
	assert.Equal(t, pid, res.AuditPlayerID)
}

func TestRecoverAndClearPropagatesInvalidCode(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()

	repo.EXPECT().
		RecoverAndClearAtomic(mock.Anything, pid.String(), "bad", mock.Anything, now).
		Return(ulid.ULID{}, false, totp.ErrInvalidRecoveryCode)

	_, err := svc.RecoverAndClear(context.Background(), pid, "bad")
	assert.ErrorIs(t, err, totp.ErrInvalidRecoveryCode)
}

func TestClearTOTPPropagatesWasEnrolledFalse(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()

	repo.EXPECT().
		ClearEnrollment(mock.Anything, pid.String()).
		Return(false, nil)

	res, err := svc.ClearTOTP(context.Background(), pid, totp.ClearReasonAdminReset)
	require.NoError(t, err)
	assert.False(t, res.WasEnrolled)
	assert.Equal(t, totp.ClearReasonAdminReset, res.ClearedBy)
}
