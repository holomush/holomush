// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
)

// mockResetRepoLogging is a mock that can fail on DeleteByPlayer for testing logging.
type mockResetRepoLogging struct {
	reset          *auth.PasswordReset
	deleteByPlayer error
}

func (m *mockResetRepoLogging) Create(_ context.Context, _ *auth.PasswordReset) error {
	return nil
}

func (m *mockResetRepoLogging) GetByTokenHash(_ context.Context, _ string) (*auth.PasswordReset, error) {
	if m.reset == nil {
		return nil, auth.ErrNotFound
	}
	return m.reset, nil
}

func (m *mockResetRepoLogging) DeleteByPlayer(_ context.Context, _ ulid.ULID) error {
	return m.deleteByPlayer
}

func (m *mockResetRepoLogging) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockResetRepoLogging) GetByPlayer(_ context.Context, _ ulid.ULID) (*auth.PasswordReset, error) {
	return m.reset, nil
}

func (m *mockResetRepoLogging) Delete(_ context.Context, _ ulid.ULID) error {
	return nil
}

// mockPlayerRepoForReset is a mock player repo for reset tests.
type mockPlayerRepoForReset struct {
	passwordUpdated bool
}

func (m *mockPlayerRepoForReset) GetByUsername(_ context.Context, _ string) (*auth.Player, error) {
	return nil, auth.ErrNotFound
}

func (m *mockPlayerRepoForReset) GetByEmail(_ context.Context, _ string) (*auth.Player, error) {
	return nil, auth.ErrNotFound
}

func (m *mockPlayerRepoForReset) Create(_ context.Context, _ *auth.Player) error {
	return nil
}

func (m *mockPlayerRepoForReset) Update(_ context.Context, _ *auth.Player) error {
	return nil
}

func (m *mockPlayerRepoForReset) UpdatePassword(_ context.Context, _ ulid.ULID, _ string) error {
	m.passwordUpdated = true
	return nil
}

func (m *mockPlayerRepoForReset) GetByID(_ context.Context, _ ulid.ULID) (*auth.Player, error) {
	return nil, auth.ErrNotFound
}

func (m *mockPlayerRepoForReset) Delete(_ context.Context, _ ulid.ULID) error {
	return nil
}

func TestPasswordResetService_ResetPassword_LogsDeleteByPlayerFailure(t *testing.T) {
	// Setup: valid reset token exists, password update succeeds, but DeleteByPlayer fails
	playerID := ulid.Make()
	token, hash, err := auth.GenerateResetToken()
	require.NoError(t, err)

	reset, err := auth.NewPasswordReset(playerID, hash, time.Now().Add(time.Hour))
	require.NoError(t, err)

	deleteErr := errors.New("cleanup connection refused")
	resetRepo := &mockResetRepoLogging{
		reset:          reset,
		deleteByPlayer: deleteErr,
	}
	playerRepo := &mockPlayerRepoForReset{}
	hasher := &mockHasherLogging{}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	svc, err := auth.NewPasswordResetServiceWithLogger(playerRepo, resetRepo, hasher, logger)
	require.NoError(t, err)

	// Reset password - this succeeds but DeleteByPlayer fails
	err = svc.ResetPassword(context.Background(), token, "newpassword123")
	require.NoError(t, err) // Reset should succeed despite cleanup failure
	assert.True(t, playerRepo.passwordUpdated)

	// Parse and verify log output
	var entry logEntry
	err = json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Contains(t, entry.Msg, "best-effort")
	assert.Equal(t, "delete_tokens", entry.Operation)
	assert.Contains(t, entry.Error, "cleanup connection refused")
}
