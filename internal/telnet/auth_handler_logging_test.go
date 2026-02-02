// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/telnet"
)

// mockAuthServiceForLogging is a mock that can fail on Logout for testing logging.
type mockAuthServiceForLogging struct {
	logoutErr error
}

func (m *mockAuthServiceForLogging) Login(_ context.Context, _, _, _, _ string) (*auth.WebSession, string, error) {
	return nil, "", errors.New("not implemented")
}

func (m *mockAuthServiceForLogging) Logout(_ context.Context, _ ulid.ULID) error {
	return m.logoutErr
}

func (m *mockAuthServiceForLogging) SelectCharacter(_ context.Context, _, _ ulid.ULID) error {
	return nil
}

// mockRegServiceForLogging is a stub registration service.
type mockRegServiceForLogging struct{}

func (m *mockRegServiceForLogging) Register(_ context.Context, _, _, _ string) (*auth.Player, error) {
	return nil, errors.New("not implemented")
}

func (m *mockRegServiceForLogging) IsRegistrationEnabled() bool {
	return false
}

// mockCharListerForLogging is a stub character lister.
type mockCharListerForLogging struct{}

func (m *mockCharListerForLogging) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*telnet.CharacterInfo, error) {
	return nil, nil
}

func (m *mockCharListerForLogging) GetByName(_ context.Context, _ string) (*telnet.CharacterInfo, error) {
	return nil, errors.New("not found")
}

// logEntry represents a parsed JSON log entry.
type logEntry struct {
	Level     string `json:"level"`
	Msg       string `json:"msg"`
	Event     string `json:"event"`
	Operation string `json:"operation"`
	Error     string `json:"error"`
	SessionID string `json:"session_id"`
}

func TestAuthHandler_HandleQuit_LogsLogoutFailure(t *testing.T) {
	// Setup: logout fails
	logoutErr := errors.New("session store unavailable")
	authService := &mockAuthServiceForLogging{logoutErr: logoutErr}
	regService := &mockRegServiceForLogging{}
	charLister := &mockCharListerForLogging{}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler, err := telnet.NewAuthHandlerWithLogger(authService, regService, charLister, logger)
	require.NoError(t, err)

	// Handle quit - this succeeds (user disconnects) but logout fails
	sessionID := ulid.Make()
	result := handler.HandleQuit(context.Background(), &sessionID)
	assert.True(t, result.Success) // Quit should succeed despite logout failure
	assert.Equal(t, "Goodbye!", result.Message)

	// Parse and verify log output
	var entry logEntry
	unmarshalErr := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, unmarshalErr, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Contains(t, entry.Msg, "best-effort")
	assert.Equal(t, "logout", entry.Operation)
	assert.Contains(t, entry.Error, "session store unavailable")
}
