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

// TestEnrollRefusesIfAlreadyEnrolled — INV: Enroll returns ErrAlreadyEnrolled
// when the player already has an active enrollment.
func TestEnrollRefusesIfAlreadyEnrolled(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, _ := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("IsEnrolled", mock.Anything, pid.String()).Return(true, nil)
	_, err := svc.Enroll(context.Background(), pid)
	assert.ErrorIs(t, err, totp.ErrAlreadyEnrolled)
}

// TestEnrollSucceedsForUnenrolledPlayer — INV: Enroll returns a populated
// EnrollResult with AuditEnrolledAt, AuditPlayerID, and 10 recovery codes.
func TestEnrollSucceedsForUnenrolledPlayer(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("IsEnrolled", mock.Anything, pid.String()).Return(false, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("wrapped"), "kek-v1", nil)
	repo.On("InsertEnrollment", mock.Anything, mock.Anything).Return(nil)
	res, err := svc.Enroll(context.Background(), pid)
	require.NoError(t, err)
	assert.Equal(t, now, res.AuditEnrolledAt)
	assert.Equal(t, pid, res.AuditPlayerID)
	assert.Len(t, res.Enrollment.RecoveryCodes, 10)
}

// TestEnrollPropagatesInsertError — INV: Enroll propagates errors from
// InsertEnrollment without swallowing them.
func TestEnrollPropagatesInsertError(t *testing.T) {
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	svc, repo, kp := newBootstrapFixture(t, now)
	pid := ulid.Make()
	repo.On("IsEnrolled", mock.Anything, pid.String()).Return(false, nil)
	kp.On("Wrap", mock.Anything, mock.Anything).Return([]byte("wrapped"), "kek-v1", nil)
	want := errors.New("pg insert fail")
	repo.On("InsertEnrollment", mock.Anything, mock.Anything).Return(want)
	_, err := svc.Enroll(context.Background(), pid)
	assert.ErrorIs(t, err, want)
}
