// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/pquerna/otp/hotp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/internal/totp/mocks"
)

// runInTxn wires up the mock Repository.InTransaction using the canonical
// mockery v3 RunAndReturn pattern: fn is called exactly once, and its return
// value becomes InTransaction's return value.
//
// DO NOT use Run(...).Return(...) — that double-invokes fn.
func runInTxn(repo *mocks.MockRepository) {
	repo.EXPECT().InTransaction(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
}

// testSecret is a 32-char base32 string (20 raw bytes) — valid RFC 4226 §4
// input for hotp.GenerateCode.
const testSecret = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

// codeAtStep returns the 6-digit HOTP code for testSecret at the given step.
func codeAtStep(t *testing.T, step int64) string {
	t.Helper()
	code, err := hotp.GenerateCode(testSecret, uint64(step)) //nolint:gosec // G115: step is a UNIX timestamp / 30; always positive and cannot overflow uint64
	require.NoError(t, err)
	return code
}

// validState returns a VerifyState with testSecret already KEK-"unwrapped"
// (in tests, wrapped and unwrapped byte slice are the same base32 bytes).
func validState() totp.VerifyState {
	return totp.VerifyState{
		PlayerID:      ulid.Make().String(),
		WrappedSecret: []byte(testSecret),
		WrapKeyID:     "kek-v1",
	}
}

// TestVerifyReturnsOutcomeNotEnrolledWhenNoEnrollment — when LoadEnrollment
// returns ErrNotEnrolled the service returns OutcomeNotEnrolled with no error.
func TestVerifyReturnsOutcomeNotEnrolledWhenNoEnrollment(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(totp.VerifyState{}, totp.ErrNotEnrolled)

	res, err := svc.Verify(context.Background(), pid, "123456")
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeNotEnrolled, res.Outcome)
	assert.Equal(t, now, res.AuditAt)
}

// TestVerifyReturnsOutcomeLockedWhenLockedUntilInFuture — when state.LockedUntil
// is in the future the service returns OutcomeLocked with LockedUntil populated.
func TestVerifyReturnsOutcomeLockedWhenLockedUntilInFuture(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()

	until := now.Add(15 * time.Minute)
	state := validState()
	state.LockedUntil = &until

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(state, nil)

	res, err := svc.Verify(context.Background(), pid, "123456")
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeLocked, res.Outcome)
	require.NotNil(t, res.LockedUntil)
	assert.Equal(t, until, *res.LockedUntil)
}

// TestVerifyAcceptsValidCodeAtCurrentStep (INV-A12 base) — a code for the
// current step is accepted. MarkVerified is called.
func TestVerifyAcceptsValidCodeAtCurrentStep(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()

	step := now.Unix() / 30
	code := codeAtStep(t, step)
	state := validState()

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(state, nil)
	kp.On("Unwrap", mock.Anything, []byte(testSecret), "kek-v1").Return([]byte(testSecret), nil)
	repo.On("MarkVerified", mock.Anything, pid.String(), step, now).Return(nil)

	res, err := svc.Verify(context.Background(), pid, code)
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeOK, res.Outcome)
}

// TestVerifyAcceptsValidCodeAtPreviousStep (INV-A12) — a code for step-1 is
// accepted within the skew=1 window. MarkVerified is called with the matched step.
func TestVerifyAcceptsValidCodeAtPreviousStep(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()

	step := now.Unix() / 30
	prevStep := step - 1
	code := codeAtStep(t, prevStep)
	state := validState()

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(state, nil)
	kp.On("Unwrap", mock.Anything, []byte(testSecret), "kek-v1").Return([]byte(testSecret), nil)
	repo.On("MarkVerified", mock.Anything, pid.String(), prevStep, now).Return(nil)

	res, err := svc.Verify(context.Background(), pid, code)
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeOK, res.Outcome)
}

// TestVerifyAcceptsValidCodeAtNextStep (INV-A12) — a code for step+1 is
// accepted within the skew=1 window. MarkVerified is called with the matched step.
func TestVerifyAcceptsValidCodeAtNextStep(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()

	step := now.Unix() / 30
	nextStep := step + 1
	code := codeAtStep(t, nextStep)
	state := validState()

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(state, nil)
	kp.On("Unwrap", mock.Anything, []byte(testSecret), "kek-v1").Return([]byte(testSecret), nil)
	repo.On("MarkVerified", mock.Anything, pid.String(), nextStep, now).Return(nil)

	res, err := svc.Verify(context.Background(), pid, code)
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeOK, res.Outcome)
}

// TestVerifyRejectsReplayWithSameStep (INV-A3) — when state.LastUsedStep equals
// the matched step, the service returns OutcomeCodeReuse with no error.
// MarkVerified MUST NOT be called.
func TestVerifyRejectsReplayWithSameStep(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()

	step := now.Unix() / 30
	code := codeAtStep(t, step)
	state := validState()
	state.LastUsedStep = &step // replay: same step already consumed

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(state, nil)
	kp.On("Unwrap", mock.Anything, []byte(testSecret), "kek-v1").Return([]byte(testSecret), nil)
	// MarkVerified must NOT be called — the mock will fail the test if it is.

	res, err := svc.Verify(context.Background(), pid, code)
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeCodeReuse, res.Outcome)
}

// TestVerifyIncrementsFailedAttemptsOnInvalidCode (INV-A14) — an invalid code
// calls IncrementFailedAttempts and returns OutcomeInvalidCode. MarkVerified
// MUST NOT be called.
func TestVerifyIncrementsFailedAttemptsOnInvalidCode(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()

	state := validState()
	postState := totp.VerifyState{FailedAttempts: 1}

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(state, nil)
	kp.On("Unwrap", mock.Anything, []byte(testSecret), "kek-v1").Return([]byte(testSecret), nil)
	repo.On("IncrementFailedAttempts", mock.Anything, pid.String(), 5, 15*time.Minute, now).
		Return(postState, nil)
	// MarkVerified must NOT be called — mock will fail if it is.

	res, err := svc.Verify(context.Background(), pid, "000000")
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeInvalidCode, res.Outcome)
	assert.Nil(t, res.LockedUntil)
	assert.False(t, res.LockoutTransition)
}

// TestVerifyTriggersLockoutTransitionOnNthFailure (INV-A4) — when
// IncrementFailedAttempts returns a state with LockedUntil set and the prior
// state had no LockedUntil, LockoutTransition is true and LockedUntil is populated.
func TestVerifyTriggersLockoutTransitionOnNthFailure(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()

	state := validState() // state.LockedUntil == nil (not yet locked)
	until := now.Add(15 * time.Minute)
	postState := totp.VerifyState{
		FailedAttempts: 5,
		LockedUntil:    &until,
	}

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(state, nil)
	kp.On("Unwrap", mock.Anything, []byte(testSecret), "kek-v1").Return([]byte(testSecret), nil)
	repo.On("IncrementFailedAttempts", mock.Anything, pid.String(), 5, 15*time.Minute, now).
		Return(postState, nil)

	res, err := svc.Verify(context.Background(), pid, "000000")
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeInvalidCode, res.Outcome)
	assert.True(t, res.LockoutTransition, "expected LockoutTransition=true on NULL→locked transition")
	require.NotNil(t, res.LockedUntil)
	assert.Equal(t, until, *res.LockedUntil)
}

// TestVerifySuccessResetsCounter (INV-A5) — a successful verify calls
// MarkVerified (which resets failed_attempts and locked_until at the repo SQL
// layer). This test confirms the service issues the MarkVerified call on OK.
func TestVerifySuccessResetsCounter(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()

	step := now.Unix() / 30
	code := codeAtStep(t, step)
	state := validState()
	state.FailedAttempts = 3 // simulate prior failures

	runInTxn(repo)
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(state, nil)
	kp.On("Unwrap", mock.Anything, []byte(testSecret), "kek-v1").Return([]byte(testSecret), nil)
	// MarkVerified is called — this asserts the reset path fires.
	repo.On("MarkVerified", mock.Anything, pid.String(), step, now).Return(nil)

	res, err := svc.Verify(context.Background(), pid, code)
	require.NoError(t, err)
	assert.Equal(t, totp.OutcomeOK, res.Outcome)
	repo.AssertCalled(t, "MarkVerified", mock.Anything, pid.String(), step, now)
}
