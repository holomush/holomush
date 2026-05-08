// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package totp_test

import (
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/totp"
	"github.com/holomush/holomush/internal/totp/mocks"
)

func newServiceForTest(t *testing.T) (totp.Service, *mocks.MockRepository) {
	t.Helper()
	repo := mocks.NewMockRepository(t)
	hasher := auth.NewArgon2idHasher()
	svc, err := totp.NewService(totp.Config{GameID: "default"}, repo, nil, totp.NewRealClock(), hasher)
	require.NoError(t, err)
	return svc, repo
}

func TestIsEnrolledReturnsRepoResult(t *testing.T) {
	svc, repo := newServiceForTest(t)
	pid := ulid.Make()
	repo.On("IsEnrolled", mock.Anything, pid.String()).Return(true, nil).Once()
	got, err := svc.IsEnrolled(context.Background(), pid)
	require.NoError(t, err)
	assert.True(t, got)
}

func TestIsEnrolledPropagatesError(t *testing.T) {
	svc, repo := newServiceForTest(t)
	pid := ulid.Make()
	want := errors.New("pg down")
	repo.On("IsEnrolled", mock.Anything, pid.String()).Return(false, want).Once()
	_, err := svc.IsEnrolled(context.Background(), pid)
	assert.ErrorIs(t, err, want)
}
