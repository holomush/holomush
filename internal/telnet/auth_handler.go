// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"context"
	"log/slog"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
)

// AuthService defines the authentication operations needed by telnet handlers.
type AuthService interface {
	// Login authenticates a player and creates a session.
	Login(ctx context.Context, username, password, userAgent, ipAddress string) (*auth.WebSession, string, error)

	// Logout invalidates a session.
	Logout(ctx context.Context, sessionID ulid.ULID) error

	// SelectCharacter updates the character selection for a session.
	SelectCharacter(ctx context.Context, sessionID, characterID ulid.ULID) error
}

// RegistrationService defines operations for player registration.
type RegistrationService interface {
	// Register creates a new player account.
	Register(ctx context.Context, username, password, email string) (*auth.Player, error)

	// IsRegistrationEnabled returns true if registration is open.
	IsRegistrationEnabled() bool
}

// CharacterLister defines operations for listing a player's characters.
type CharacterLister interface {
	// ListByPlayer returns all characters for a player.
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*CharacterInfo, error)

	// GetByName returns a character by name (case-insensitive).
	GetByName(ctx context.Context, name string) (*CharacterInfo, error)
}

// CharacterInfo contains the minimal character info needed for telnet display.
type CharacterInfo struct {
	ID   ulid.ULID
	Name string
}

// AuthHandler handles authentication-related telnet commands.
type AuthHandler struct {
	authService AuthService
	regService  RegistrationService
	charLister  CharacterLister
	logger      *slog.Logger
}

// NewAuthHandler creates a new AuthHandler with a no-op logger.
// Returns an error if any required dependency is nil.
func NewAuthHandler(authService AuthService, regService RegistrationService, charLister CharacterLister) (*AuthHandler, error) {
	if authService == nil {
		return nil, oops.Errorf("auth service is required")
	}
	if regService == nil {
		return nil, oops.Errorf("registration service is required")
	}
	if charLister == nil {
		return nil, oops.Errorf("character lister is required")
	}
	return &AuthHandler{
		authService: authService,
		regService:  regService,
		charLister:  charLister,
		logger:      slog.New(slog.DiscardHandler),
	}, nil
}

// NewAuthHandlerWithLogger creates a new AuthHandler with the provided logger.
// Returns an error if any required dependency is nil.
func NewAuthHandlerWithLogger(authService AuthService, regService RegistrationService, charLister CharacterLister, logger *slog.Logger) (*AuthHandler, error) {
	if authService == nil {
		return nil, oops.Errorf("auth service is required")
	}
	if regService == nil {
		return nil, oops.Errorf("registration service is required")
	}
	if charLister == nil {
		return nil, oops.Errorf("character lister is required")
	}
	if logger == nil {
		return nil, oops.Errorf("logger is required")
	}
	return &AuthHandler{
		authService: authService,
		regService:  regService,
		charLister:  charLister,
		logger:      logger,
	}, nil
}

// ConnectResult contains the result of a connect command.
type ConnectResult struct {
	Success    bool
	Message    string
	PlayerID   ulid.ULID
	SessionID  ulid.ULID
	Characters []*CharacterInfo
}

// HandleConnect processes a connect command.
func (h *AuthHandler) HandleConnect(ctx context.Context, username, password, ipAddress string) ConnectResult {
	session, _, err := h.authService.Login(ctx, username, password, "telnet", ipAddress)
	if err != nil {
		return h.handleConnectError(err)
	}

	// Get player's characters
	characters, err := h.charLister.ListByPlayer(ctx, session.PlayerID)
	if err != nil {
		// Login succeeded but character listing failed - still allow connection
		return ConnectResult{
			Success:    true,
			Message:    "Welcome! (Note: Could not retrieve character list)",
			PlayerID:   session.PlayerID,
			SessionID:  session.ID,
			Characters: nil,
		}
	}

	message := h.buildWelcomeMessage(username, characters)
	return ConnectResult{
		Success:    true,
		Message:    message,
		PlayerID:   session.PlayerID,
		SessionID:  session.ID,
		Characters: characters,
	}
}

func (h *AuthHandler) handleConnectError(err error) ConnectResult {
	// Check for specific error codes
	oopsErr, ok := oops.AsOops(err)
	if ok {
		switch oopsErr.Code() {
		case "AUTH_INVALID_CREDENTIALS":
			return ConnectResult{Success: false, Message: "Invalid username or password."}
		case "AUTH_ACCOUNT_LOCKED":
			return ConnectResult{Success: false, Message: "Account is temporarily locked. Please try again later."}
		}
	}
	return ConnectResult{Success: false, Message: "Login failed. Please try again."}
}

func (h *AuthHandler) buildWelcomeMessage(username string, characters []*CharacterInfo) string {
	if len(characters) == 0 {
		return "Welcome, " + username + "! You have no characters. Use 'create <name>' to create one."
	}
	return "Welcome, " + username + "!"
}

// CreateResult contains the result of a create command.
type CreateResult struct {
	Success bool
	Message string
}

// HandleCreate processes a create (registration) command.
func (h *AuthHandler) HandleCreate(ctx context.Context, username, password string) CreateResult {
	if !h.regService.IsRegistrationEnabled() {
		return CreateResult{Success: false, Message: "Registration is currently disabled."}
	}

	_, err := h.regService.Register(ctx, username, password, "")
	if err != nil {
		return h.handleCreateError(err)
	}

	return CreateResult{
		Success: true,
		Message: "Account created successfully! Use 'connect <username> <password>' to log in.",
	}
}

func (h *AuthHandler) handleCreateError(err error) CreateResult {
	oopsErr, ok := oops.AsOops(err)
	if ok {
		switch oopsErr.Code() {
		case "AUTH_USERNAME_TAKEN":
			return CreateResult{Success: false, Message: "Username already exists. Please choose another."}
		case "AUTH_INVALID_USERNAME":
			return CreateResult{Success: false, Message: "Username is invalid. It must be 3-30 characters, start with a letter, and contain only letters, numbers, and underscores."}
		case "AUTH_INVALID_PASSWORD":
			return CreateResult{Success: false, Message: "Password is invalid. Please choose a stronger password."}
		}
	}
	return CreateResult{Success: false, Message: "Registration failed. Please try again."}
}

// PlayResult contains the result of a play command.
type PlayResult struct {
	Success       bool
	Message       string
	CharacterID   ulid.ULID
	CharacterName string
}

// HandlePlay processes a play command to select a character.
func (h *AuthHandler) HandlePlay(ctx context.Context, sessionID, playerID ulid.ULID, characterName string) PlayResult {
	// Look up the character by name
	charInfo, err := h.charLister.GetByName(ctx, characterName)
	if err != nil {
		return PlayResult{Success: false, Message: "Character not found."}
	}

	// Verify ownership by checking player's character list
	playerChars, err := h.charLister.ListByPlayer(ctx, playerID)
	if err != nil {
		return PlayResult{Success: false, Message: "Could not verify character ownership."}
	}

	owned := false
	for _, pc := range playerChars {
		if pc.ID == charInfo.ID {
			owned = true
			break
		}
	}
	if !owned {
		return PlayResult{Success: false, Message: "You do not own this character."}
	}

	// Update session with selected character
	if err := h.authService.SelectCharacter(ctx, sessionID, charInfo.ID); err != nil {
		return PlayResult{Success: false, Message: "Failed to select character."}
	}

	return PlayResult{
		Success:       true,
		Message:       "Now playing as " + charInfo.Name + ".",
		CharacterID:   charInfo.ID,
		CharacterName: charInfo.Name,
	}
}

// QuitResult contains the result of a quit command.
type QuitResult struct {
	Success bool
	Message string
}

// HandleQuit processes a quit command.
func (h *AuthHandler) HandleQuit(ctx context.Context, sessionID *ulid.ULID) QuitResult {
	if sessionID != nil {
		// Best effort logout - the user is disconnecting regardless of logout success
		if err := h.authService.Logout(ctx, *sessionID); err != nil {
			h.logger.Warn("best-effort logout failed",
				"event", "logout_failed",
				"session_id", sessionID.String(),
				"operation", "logout",
				"error", err.Error(),
			)
		}
	}

	return QuitResult{
		Success: true,
		Message: "Goodbye!",
	}
}
