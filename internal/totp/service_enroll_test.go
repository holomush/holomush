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

	kekMocks "github.com/holomush/holomush/internal/eventbus/crypto/kek/mocks"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/internal/totp/mocks"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestEnrollScenarios consolidates the Enroll cases (already-enrolled,
// success, insert-error) per repo guideline "Use table-driven tests for
// comprehensive coverage."
func TestEnrollScenarios(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name      string
		setup     func(repo *mocks.MockRepository, kp *kekMocks.MockProvider, pid ulid.ULID)
		assertErr func(t *testing.T, err error)
		assertRes func(t *testing.T, res totp.EnrollResult, pid ulid.ULID)
	}{
		{
			name: "already enrolled rejects",
			setup: func(repo *mocks.MockRepository, _ *kekMocks.MockProvider, pid ulid.ULID) {
				repo.On("IsEnrolled", mock.Anything, pid.String()).Return(true, nil)
			},
			assertErr: func(t *testing.T, err error) {
				errutil.AssertErrorCode(t, err, "TOTP_ALREADY_ENROLLED")
			},
		},
		{
			name: "success populates audit metadata + 10 recovery codes",
			setup: func(repo *mocks.MockRepository, kp *kekMocks.MockProvider, pid ulid.ULID) {
				repo.On("IsEnrolled", mock.Anything, pid.String()).Return(false, nil)
				kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("wrapped"), "kek-v1", nil)
				repo.On("InsertEnrollment", mock.Anything, mock.Anything).Return(nil)
			},
			assertErr: func(t *testing.T, err error) { require.NoError(t, err) },
			assertRes: func(t *testing.T, res totp.EnrollResult, pid ulid.ULID) {
				assert.Equal(t, now, res.AuditEnrolledAt)
				assert.Equal(t, pid, res.AuditPlayerID)
				assert.Len(t, res.Enrollment.RecoveryCodes, 10)
			},
		},
		{
			name: "insert error propagates",
			setup: func(repo *mocks.MockRepository, kp *kekMocks.MockProvider, pid ulid.ULID) {
				repo.On("IsEnrolled", mock.Anything, pid.String()).Return(false, nil)
				kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("wrapped"), "kek-v1", nil)
				repo.On("InsertEnrollment", mock.Anything, mock.Anything).
					Return(errors.New("pg insert fail"))
			},
			assertErr: func(t *testing.T, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "pg insert fail")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, kp := newBootstrapFixture(t, now)
			pid := ulid.Make()
			tc.setup(repo, kp, pid)

			res, err := svc.Enroll(context.Background(), pid)
			tc.assertErr(t, err)
			if tc.assertRes != nil {
				tc.assertRes(t, res, pid)
			}
		})
	}
}
