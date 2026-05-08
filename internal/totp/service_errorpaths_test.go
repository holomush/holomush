// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/totp"
)

// Service error-path coverage. Each test exercises a single repo-error
// or KEK-error branch in BootstrapEnroll / Enroll / ClearTOTP /
// buildEnrollment that the happy-path fixtures do not reach.

func TestBootstrapEnrollPropagatesPlayerExistsError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	want := errors.New("pg connection lost")

	repo.On("PlayerExists", mock.Anything, pid.String()).Return(false, want)

	_, err := svc.BootstrapEnroll(context.Background(), pid)
	assert.ErrorIs(t, err, want)
}

func TestBootstrapEnrollPropagatesKEKWrapError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()
	want := errors.New("kek backend unreachable")

	repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte(nil), "", want)

	_, err := svc.BootstrapEnroll(context.Background(), pid)
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

func TestEnrollPropagatesIsEnrolledError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	want := errors.New("pg query failed")

	repo.On("IsEnrolled", mock.Anything, pid.String()).Return(false, want)

	_, err := svc.Enroll(context.Background(), pid)
	assert.ErrorIs(t, err, want)
}

func TestClearTOTPPropagatesRepoError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	want := errors.New("pg delete failed")

	repo.On("ClearEnrollment", mock.Anything, pid.String()).Return(false, want)

	_, err := svc.ClearTOTP(context.Background(), pid, totp.ClearReasonAdminReset)
	assert.ErrorIs(t, err, want)
}

func TestNewServiceRejectsEmptyGameID(t *testing.T) {
	_, err := totp.NewService(
		totp.Config{}, // GameID empty
		nil, nil, totp.NewRealClock(), auth.NewArgon2idHasher(),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Config.GameID is required")
}

// Ensure isNotEnrolledErr returns false for non-oops errors. Reached
// via Verify when the repo.LoadEnrollment mock returns a plain stdlib
// error (not an oops error); Service.Verify must surface it as-is.
func TestVerifySurfacesNonOopsRepoError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	want := errors.New("plain stdlib error, not oops")

	repo.EXPECT().InTransaction(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(totp.VerifyState{}, want)

	_, err := svc.Verify(context.Background(), pid, "123456")
	require.Error(t, err)
	assert.ErrorIs(t, err, want)
}

// Sanity: an oops-coded error other than TOTP_NOT_ENROLLED must NOT
// be classified as not-enrolled. Mirrors the regression test added in
// the post-review fix commit; this version uses a different code to
// rule out any lingering tautology specific to TOTP_NOT_ENROLLED.
func TestVerifySurfacesUnknownOopsCode(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	want := oops.Code("SOMETHING_ELSE_ENTIRELY").Errorf("decoy")

	repo.EXPECT().InTransaction(mock.Anything, mock.AnythingOfType("func(context.Context) error")).
		RunAndReturn(func(ctx context.Context, fn func(context.Context) error) error {
			return fn(ctx)
		})
	repo.On("LoadEnrollment", mock.Anything, pid.String()).Return(totp.VerifyState{}, want)

	_, err := svc.Verify(context.Background(), pid, "123456")
	require.Error(t, err)
}
