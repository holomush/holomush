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
	Level       string `json:"level"`
	Msg         string `json:"msg"`
	Event       string `json:"event"`
	Operation   string `json:"operation"`
	Error       string `json:"error"`
	SessionID   string `json:"session_id"`
	PlayerID    string `json:"player_id"`
	CharacterID string `json:"character_id"`
}

// mockAuthServiceUnexpectedLoginError returns an unexpected (non-coded) error on Login.
type mockAuthServiceUnexpectedLoginError struct {
	loginErr error
}

func (m *mockAuthServiceUnexpectedLoginError) Login(_ context.Context, _, _, _, _ string) (*auth.WebSession, string, error) {
	return nil, "", m.loginErr
}

func (m *mockAuthServiceUnexpectedLoginError) Logout(_ context.Context, _ ulid.ULID) error {
	return nil
}

func (m *mockAuthServiceUnexpectedLoginError) SelectCharacter(_ context.Context, _, _ ulid.ULID) error {
	return nil
}

// mockRegServiceUnexpectedError returns an unexpected (non-coded) error on Register.
type mockRegServiceUnexpectedError struct {
	registerErr error
	enabled     bool
}

func (m *mockRegServiceUnexpectedError) Register(_ context.Context, _, _, _ string) (*auth.Player, error) {
	return nil, m.registerErr
}

func (m *mockRegServiceUnexpectedError) IsRegistrationEnabled() bool {
	return m.enabled
}

// mockAuthServiceWithSelectCharError returns an error on SelectCharacter.
type mockAuthServiceWithSelectCharError struct {
	selectErr error
}

func (m *mockAuthServiceWithSelectCharError) Login(_ context.Context, _, _, _, _ string) (*auth.WebSession, string, error) {
	return nil, "", errors.New("not implemented")
}

func (m *mockAuthServiceWithSelectCharError) Logout(_ context.Context, _ ulid.ULID) error {
	return nil
}

func (m *mockAuthServiceWithSelectCharError) SelectCharacter(_ context.Context, _, _ ulid.ULID) error {
	return m.selectErr
}

// mockCharListerWithResults returns configurable results for character listing.
type mockCharListerWithResults struct {
	getByNameResult *telnet.CharacterInfo
	getByNameErr    error
	listResult      []*telnet.CharacterInfo
	listErr         error
}

func (m *mockCharListerWithResults) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*telnet.CharacterInfo, error) {
	return m.listResult, m.listErr
}

func (m *mockCharListerWithResults) GetByName(_ context.Context, _ string) (*telnet.CharacterInfo, error) {
	return m.getByNameResult, m.getByNameErr
}

// mockCharListerFailsListing always fails ListByPlayer for testing logging.
type mockCharListerFailsListing struct{}

func (m *mockCharListerFailsListing) ListByPlayer(_ context.Context, _ ulid.ULID) ([]*telnet.CharacterInfo, error) {
	return nil, errors.New("database connection lost")
}

func (m *mockCharListerFailsListing) GetByName(_ context.Context, _ string) (*telnet.CharacterInfo, error) {
	return nil, errors.New("not found")
}

// mockAuthServiceSuccessfulLogin returns a successful session for testing.
type mockAuthServiceSuccessfulLogin struct{}

func (m *mockAuthServiceSuccessfulLogin) Login(_ context.Context, _, _, _, _ string) (*auth.WebSession, string, error) {
	return &auth.WebSession{
		ID:       ulid.Make(),
		PlayerID: ulid.Make(),
	}, "token", nil
}

func (m *mockAuthServiceSuccessfulLogin) Logout(_ context.Context, _ ulid.ULID) error {
	return nil
}

func (m *mockAuthServiceSuccessfulLogin) SelectCharacter(_ context.Context, _, _ ulid.ULID) error {
	return nil
}

func TestAuthHandler_HandleConnect_LogsCharacterListFailure(t *testing.T) {
	// Setup: login succeeds but character listing fails
	listErr := errors.New("database connection lost")
	_ = listErr // error message used in mock
	authService := &mockAuthServiceSuccessfulLogin{}
	regService := &mockRegServiceForLogging{}
	charLister := &mockCharListerFailsListing{}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler, err := telnet.NewAuthHandlerWithLogger(authService, regService, charLister, logger)
	require.NoError(t, err)

	// HandleConnect - login succeeds but character listing fails
	result := handler.HandleConnect(context.Background(), "testuser", "password", "127.0.0.1")
	assert.True(t, result.Success) // Connect should succeed despite listing failure
	assert.Contains(t, result.Message, "Could not retrieve character list")

	// Parse and verify log output
	var entry logEntry
	unmarshalErr := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, unmarshalErr, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Contains(t, entry.Msg, "best-effort")
	assert.Equal(t, "character_list_failed", entry.Event)
	assert.Equal(t, "list_by_player", entry.Operation)
	assert.Contains(t, entry.Error, "database connection lost")
	assert.NotEmpty(t, entry.SessionID, "should include session_id")
	assert.NotEmpty(t, entry.PlayerID, "should include player_id")
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

func TestAuthHandler_HandleConnect_LogsUnexpectedLoginError(t *testing.T) {
	// Setup: login fails with an unexpected (non-coded) error
	loginErr := errors.New("database connection timeout")
	authService := &mockAuthServiceUnexpectedLoginError{loginErr: loginErr}
	regService := &mockRegServiceForLogging{}
	charLister := &mockCharListerForLogging{}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler, err := telnet.NewAuthHandlerWithLogger(authService, regService, charLister, logger)
	require.NoError(t, err)

	// HandleConnect - login fails with unexpected error
	result := handler.HandleConnect(context.Background(), "testuser", "password", "127.0.0.1")
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Login failed")

	// Parse and verify log output
	var entry logEntry
	unmarshalErr := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, unmarshalErr, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Equal(t, "unexpected login failure", entry.Msg)
	assert.Equal(t, "login_failed_unexpected", entry.Event)
	assert.Equal(t, "login", entry.Operation)
	assert.Contains(t, entry.Error, "database connection timeout")
}

func TestAuthHandler_HandleCreate_LogsUnexpectedRegistrationError(t *testing.T) {
	// Setup: registration fails with an unexpected (non-coded) error
	regErr := errors.New("email service unavailable")
	authService := &mockAuthServiceForLogging{}
	regService := &mockRegServiceUnexpectedError{registerErr: regErr, enabled: true}
	charLister := &mockCharListerForLogging{}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler, err := telnet.NewAuthHandlerWithLogger(authService, regService, charLister, logger)
	require.NoError(t, err)

	// HandleCreate - registration fails with unexpected error
	result := handler.HandleCreate(context.Background(), "newuser", "password123")
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Registration failed")

	// Parse and verify log output
	var entry logEntry
	unmarshalErr := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, unmarshalErr, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Equal(t, "unexpected registration failure", entry.Msg)
	assert.Equal(t, "registration_failed_unexpected", entry.Event)
	assert.Equal(t, "register", entry.Operation)
	assert.Contains(t, entry.Error, "email service unavailable")
}

func TestAuthHandler_HandlePlay_LogsOwnershipVerificationFailure(t *testing.T) {
	// Setup: character lookup succeeds but ownership verification fails
	charID := ulid.Make()
	charInfo := &telnet.CharacterInfo{ID: charID, Name: "Hero"}
	listErr := errors.New("database query failed")

	authService := &mockAuthServiceWithSelectCharError{}
	regService := &mockRegServiceForLogging{}
	charLister := &mockCharListerWithResults{
		getByNameResult: charInfo,
		getByNameErr:    nil,
		listResult:      nil,
		listErr:         listErr,
	}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler, err := telnet.NewAuthHandlerWithLogger(authService, regService, charLister, logger)
	require.NoError(t, err)

	sessionID := ulid.Make()
	playerID := ulid.Make()

	// HandlePlay - ownership verification fails
	result := handler.HandlePlay(context.Background(), sessionID, playerID, "Hero")
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Could not verify")

	// Parse and verify log output
	var entry logEntry
	unmarshalErr := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, unmarshalErr, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Equal(t, "character ownership verification failed", entry.Msg)
	assert.Equal(t, "ownership_verification_failed", entry.Event)
	assert.Equal(t, "list_by_player", entry.Operation)
	assert.Contains(t, entry.Error, "database query failed")
	assert.Equal(t, sessionID.String(), entry.SessionID)
	assert.Equal(t, playerID.String(), entry.PlayerID)
}

func TestAuthHandler_HandlePlay_LogsSelectCharacterFailure(t *testing.T) {
	// Setup: character lookup and ownership verification succeed, but select fails
	charID := ulid.Make()
	charInfo := &telnet.CharacterInfo{ID: charID, Name: "Hero"}
	selectErr := errors.New("session expired during selection")

	authService := &mockAuthServiceWithSelectCharError{selectErr: selectErr}
	regService := &mockRegServiceForLogging{}
	charLister := &mockCharListerWithResults{
		getByNameResult: charInfo,
		getByNameErr:    nil,
		listResult:      []*telnet.CharacterInfo{charInfo},
		listErr:         nil,
	}

	// Capture logs
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))

	handler, err := telnet.NewAuthHandlerWithLogger(authService, regService, charLister, logger)
	require.NoError(t, err)

	sessionID := ulid.Make()
	playerID := ulid.Make()

	// HandlePlay - select character fails
	result := handler.HandlePlay(context.Background(), sessionID, playerID, "Hero")
	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Failed to select")

	// Parse and verify log output
	var entry logEntry
	unmarshalErr := json.Unmarshal(buf.Bytes(), &entry)
	require.NoError(t, unmarshalErr, "should have logged JSON entry")

	assert.Equal(t, "WARN", entry.Level)
	assert.Equal(t, "character selection failed", entry.Msg)
	assert.Equal(t, "character_select_failed", entry.Event)
	assert.Equal(t, "select_character", entry.Operation)
	assert.Contains(t, entry.Error, "session expired during selection")
	assert.Equal(t, sessionID.String(), entry.SessionID)
	assert.Equal(t, charID.String(), entry.CharacterID)
}
