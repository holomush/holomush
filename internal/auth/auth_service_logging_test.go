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

// mockPlayerRepoLogging is a mock that fails on Update for testing logging.
type mockPlayerRepoLogging struct {
	player    *auth.Player
	updateErr error
}

func (m *mockPlayerRepoLogging) GetByUsername(_ context.Context, _ string) (*auth.Player, error) {
	if m.player == nil {
		return nil, auth.ErrNotFound
	}
	// Return a copy to avoid mutation issues
	playerCopy := *m.player
	return &playerCopy, nil
}

func (m *mockPlayerRepoLogging) GetByEmail(_ context.Context, _ string) (*auth.Player, error) {
	return nil, auth.ErrNotFound
}

func (m *mockPlayerRepoLogging) Create(_ context.Context, _ *auth.Player) error {
	return nil
}

func (m *mockPlayerRepoLogging) Update(_ context.Context, _ *auth.Player) error {
	return m.updateErr
}

func (m *mockPlayerRepoLogging) UpdatePassword(_ context.Context, _ ulid.ULID, _ string) error {
	return nil
}

func (m *mockPlayerRepoLogging) GetByID(_ context.Context, id ulid.ULID) (*auth.Player, error) {
	if m.player != nil && m.player.ID == id {
		playerCopy := *m.player
		return &playerCopy, nil
	}
	return nil, auth.ErrNotFound
}

func (m *mockPlayerRepoLogging) Delete(_ context.Context, _ ulid.ULID) error {
	return nil
}

// mockSessionRepoLogging is a mock that can fail on operations for testing logging.
type mockSessionRepoLogging struct {
	session       *auth.WebSession
	updateLastErr error
}

func (m *mockSessionRepoLogging) Create(_ context.Context, s *auth.WebSession) error {
	m.session = s
	return nil
}

func (m *mockSessionRepoLogging) GetByTokenHash(_ context.Context, _ string) (*auth.WebSession, error) {
	if m.session == nil {
		return nil, auth.ErrNotFound
	}
	return m.session, nil
}

func (m *mockSessionRepoLogging) Delete(_ context.Context, _ ulid.ULID) error {
	return nil
}

func (m *mockSessionRepoLogging) GetByID(_ context.Context, id ulid.ULID) (*auth.WebSession, error) {
	if m.session != nil && m.session.ID == id {
		return m.session, nil
	}
	return nil, auth.ErrNotFound
}

func (m *mockSessionRepoLogging) GetByPlayer(_ context.Context, _ ulid.ULID) ([]*auth.WebSession, error) {
	if m.session != nil {
		return []*auth.WebSession{m.session}, nil
	}
	return nil, nil
}

func (m *mockSessionRepoLogging) UpdateLastSeen(_ context.Context, _ ulid.ULID, _ time.Time) error {
	return m.updateLastErr
}

func (m *mockSessionRepoLogging) UpdateCharacter(_ context.Context, _, _ ulid.ULID) error {
	return nil
}

func (m *mockSessionRepoLogging) DeleteExpired(_ context.Context) (int64, error) {
	return 0, nil
}

func (m *mockSessionRepoLogging) DeleteByPlayer(_ context.Context, _ ulid.ULID) error {
	return nil
}

// mockHasherLogging is a mock hasher for testing.
// It validates passwords based on a simple rule: password must be "correctpassword".
type mockHasherLogging struct{}

func (m *mockHasherLogging) Hash(_ string) (string, error) {
	return "$argon2id$v=19$m=65536,t=1,p=4$salt$hash", nil
}

func (m *mockHasherLogging) Verify(password, hash string) (bool, error) {
	// Only accept "correctpassword" as valid, regardless of hash
	// For dummy hash (timing attack prevention), always return false
	if hash == "$argon2id$v=19$m=65536,t=1,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" {
		return false, nil
	}
	return password == "correctpassword", nil
}

func (m *mockHasherLogging) NeedsUpgrade(_ string) bool {
	return false
}

// logEntry represents a parsed JSON log entry.
type logEntry struct {
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Event     string `json:"event"`
	Operation string `json:"operation"`
	Error     string `json:"error"`
	PlayerID  string `json:"player_id"`
	SessionID string `json:"session_id"`
}

func TestService_Login_LogsUpdateFailure_RecordFailure(t *testing.T) {
	// Setup: player exists but update fails
	playerID := ulid.Make()
	player := &auth.Player{
		ID:           playerID,
		Username:     "testuser",
		PasswordHash: "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
	}

	updateErr := errors.New("database connection lost")
	playerRepo := &mockPlayerRepoLogging{
		player:    player,
		updateErr: updateErr,
	}
	sessionRepo := &mockSessionRepoLogging{}
	hasher := &mockHasherLogging{}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	svc, err := auth.NewAuthServiceWithLogger(playerRepo, sessionRepo, hasher, logger)
	require.NoError(t, err)

	// Attempt login with wrong password - this triggers RecordFailure which will fail
	_, _, err = svc.Login(context.Background(), "testuser", "wrongpassword", "test-agent", "127.0.0.1")
	assert.Error(t, err) // Login fails due to wrong password

	// Parse and verify log output
	var entry logEntry
	err = json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Contains(t, entry.Msg, "best-effort")
	assert.Equal(t, "record_failure", entry.Operation)
	assert.Contains(t, entry.Error, "database connection lost")
}

func TestService_Login_LogsUpdateFailure_RecordSuccess(t *testing.T) {
	// Setup: player exists, login succeeds, but update fails
	playerID := ulid.Make()
	player := &auth.Player{
		ID:           playerID,
		Username:     "testuser",
		PasswordHash: "$argon2id$v=19$m=65536,t=1,p=4$salt$hash",
	}

	updateErr := errors.New("database timeout")
	playerRepo := &mockPlayerRepoLogging{
		player:    player,
		updateErr: updateErr,
	}
	sessionRepo := &mockSessionRepoLogging{}
	hasher := &mockHasherLogging{}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	svc, err := auth.NewAuthServiceWithLogger(playerRepo, sessionRepo, hasher, logger)
	require.NoError(t, err)

	// Login with correct password - this succeeds but RecordSuccess update fails
	session, token, err := svc.Login(context.Background(), "testuser", "correctpassword", "test-agent", "127.0.0.1")
	require.NoError(t, err) // Login should succeed despite update failure
	assert.NotNil(t, session)
	assert.NotEmpty(t, token)

	// Parse and verify log output
	var entry logEntry
	err = json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Contains(t, entry.Msg, "best-effort")
	assert.Equal(t, "record_success", entry.Operation)
	assert.Contains(t, entry.Error, "database timeout")
}

func TestService_ValidateSession_LogsUpdateLastSeenFailure(t *testing.T) {
	// Setup: session exists but UpdateLastSeen fails
	sessionID := ulid.Make()
	playerID := ulid.Make()
	token, tokenHash, err := auth.GenerateSessionToken()
	require.NoError(t, err)

	session, err := auth.NewWebSession(playerID, nil, tokenHash, "test-agent", "127.0.0.1", time.Now().Add(time.Hour))
	require.NoError(t, err)
	session.ID = sessionID

	updateLastErr := errors.New("redis unavailable")
	playerRepo := &mockPlayerRepoLogging{}
	sessionRepo := &mockSessionRepoLogging{
		session:       session,
		updateLastErr: updateLastErr,
	}
	hasher := &mockHasherLogging{}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	svc, err := auth.NewAuthServiceWithLogger(playerRepo, sessionRepo, hasher, logger)
	require.NoError(t, err)

	// Validate session - this succeeds but UpdateLastSeen fails
	result, err := svc.ValidateSession(context.Background(), token)
	require.NoError(t, err) // Validation should succeed despite update failure
	assert.NotNil(t, result)

	// Parse and verify log output
	var entry logEntry
	err = json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, err, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Contains(t, entry.Msg, "best-effort")
	assert.Equal(t, "update_last_seen", entry.Operation)
	assert.Contains(t, entry.Error, "redis unavailable")
}
