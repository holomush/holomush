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
	kekMocks "github.com/holomush/holomush/internal/eventbus/crypto/kek/mocks"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/internal/totp/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

// Service error-path coverage for non-transactional methods. Each row
// exercises a single repo-error or KEK-error branch in BootstrapEnroll /
// Enroll / ClearTOTP that the happy-path fixtures don't reach. Verify
// gets dedicated tests below since it uses InTransaction semantics.

func TestServicePropagatesRepoAndKEKErrors(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	pid := ulid.Make()

	cases := []struct {
		name   string
		setup  func(repo *mocks.MockRepository, kp *kekMocks.MockProvider) error // returns the planted sentinel
		invoke func(svc totp.Service) error
	}{
		{
			name: "BootstrapEnroll propagates PlayerExists error",
			setup: func(repo *mocks.MockRepository, _ *kekMocks.MockProvider) error {
				want := errors.New("pg connection lost")
				repo.On("PlayerExists", mock.Anything, pid.String()).Return(false, want)
				return want
			},
			invoke: func(svc totp.Service) error {
				_, err := svc.BootstrapEnroll(context.Background(), pid)
				return err
			},
		},
		{
			name: "BootstrapEnroll propagates KEK Wrap error",
			setup: func(repo *mocks.MockRepository, kp *kekMocks.MockProvider) error {
				want := errors.New("kek backend unreachable")
				repo.On("PlayerExists", mock.Anything, pid.String()).Return(true, nil)
				kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte(nil), "", want)
				return want
			},
			invoke: func(svc totp.Service) error {
				_, err := svc.BootstrapEnroll(context.Background(), pid)
				return err
			},
		},
		{
			name: "Enroll propagates IsEnrolled error",
			setup: func(repo *mocks.MockRepository, _ *kekMocks.MockProvider) error {
				want := errors.New("pg query failed")
				repo.On("IsEnrolled", mock.Anything, pid.String()).Return(false, want)
				return want
			},
			invoke: func(svc totp.Service) error {
				_, err := svc.Enroll(context.Background(), pid)
				return err
			},
		},
		{
			name: "ClearTOTP propagates ClearEnrollment error",
			setup: func(repo *mocks.MockRepository, _ *kekMocks.MockProvider) error {
				want := errors.New("pg delete failed")
				repo.On("ClearEnrollment", mock.Anything, pid.String()).Return(false, want)
				return want
			},
			invoke: func(svc totp.Service) error {
				_, err := svc.ClearTOTP(context.Background(), pid, totp.ClearReasonAdminReset)
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, kp := newBootstrapFixture(t, now)
			want := tc.setup(repo, kp)
			err := tc.invoke(svc)
			require.Error(t, err)
			assert.ErrorIs(t, err, want)
		})
	}
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

// Sanity: an oops-coded error other than TOTP_NOT_ENROLLED must NOT be
// classified as not-enrolled. Identity-checks the surfaced error by oops
// code (NOT errors.Is — that's tautological under samber/oops; see
// holomush-8lhd) so a regression that swaps the misclassified path back
// in would fail this test.
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
	errutil.AssertErrorCode(t, err, "SOMETHING_ELSE_ENTIRELY")
}
