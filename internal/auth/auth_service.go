// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// Service provides authentication operations.
type Service struct {
	players  PlayerRepository
	sessions WebSessionRepository
	hasher   PasswordHasher
	logger   *slog.Logger
}

// NewAuthService creates a new Service with a no-op logger.
// Returns an error if any required dependency is nil.
func NewAuthService(players PlayerRepository, sessions WebSessionRepository, hasher PasswordHasher) (*Service, error) {
	if players == nil {
		return nil, oops.Errorf("players repository is required")
	}
	if sessions == nil {
		return nil, oops.Errorf("sessions repository is required")
	}
	if hasher == nil {
		return nil, oops.Errorf("password hasher is required")
	}
	return &Service{
		players:  players,
		sessions: sessions,
		hasher:   hasher,
		logger:   slog.New(slog.DiscardHandler),
	}, nil
}

// NewAuthServiceWithLogger creates a new Service with the provided logger.
// Returns an error if any required dependency is nil.
func NewAuthServiceWithLogger(players PlayerRepository, sessions WebSessionRepository, hasher PasswordHasher, logger *slog.Logger) (*Service, error) {
	if players == nil {
		return nil, oops.Errorf("players repository is required")
	}
	if sessions == nil {
		return nil, oops.Errorf("sessions repository is required")
	}
	if hasher == nil {
		return nil, oops.Errorf("password hasher is required")
	}
	if logger == nil {
		return nil, oops.Errorf("logger is required")
	}
	return &Service{
		players:  players,
		sessions: sessions,
		hasher:   hasher,
		logger:   logger,
	}, nil
}

// dummyPasswordHash is used when a user doesn't exist to prevent timing attacks.
// We still run password verification to make response time consistent.
// This is NOT a real credential - it's a fake hash that will never match any password.
//
//nolint:gosec // G101: This is an intentionally fake hash for timing attack prevention, not a credential.
const dummyPasswordHash = "$argon2id$v=19$m=65536,t=1,p=4$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// Login authenticates a player and creates a web session.
// Returns the session, plaintext token, and any error.
// Uses constant-time operations to prevent timing-based username enumeration.
func (s *Service) Login(ctx context.Context, username, password, userAgent, ipAddress string) (*WebSession, string, error) {
	// Look up player by username
	player, lookupErr := s.players.GetByUsername(ctx, username)

	// Determine which hash to verify against (real or dummy for timing attack prevention)
	var targetHash string
	var playerExists bool

	if lookupErr != nil {
		if errors.Is(lookupErr, ErrNotFound) {
			// Use dummy hash - still perform verification to maintain constant time
			targetHash = dummyPasswordHash
			playerExists = false
		} else {
			return nil, "", oops.Code("AUTH_LOGIN_FAILED").
				With("operation", "get player by username").
				Wrap(lookupErr)
		}
	} else {
		targetHash = player.PasswordHash
		playerExists = true
	}

	// Always verify password (constant-time operation for timing attack prevention)
	valid, verifyErr := s.hasher.Verify(password, targetHash)
	if verifyErr != nil {
		// For dummy hash verification errors, just treat as invalid
		if !playerExists {
			return nil, "", oops.Code("AUTH_INVALID_CREDENTIALS").Errorf("invalid username or password")
		}
		return nil, "", oops.Code("AUTH_LOGIN_FAILED").
			With("operation", "verify password").
			Wrap(verifyErr)
	}

	// If player doesn't exist OR password invalid, return same error
	if !playerExists || !valid {
		if playerExists {
			// Record failure only for existing players
			player.RecordFailure()
			if err := s.players.Update(ctx, player); err != nil {
				s.logger.Warn("best-effort player update failed",
					"event", "player_update_failed",
					"player_id", player.ID.String(),
					"operation", "record_failure",
					"error", err.Error(),
				)
			}
		}
		return nil, "", oops.Code("AUTH_INVALID_CREDENTIALS").Errorf("invalid username or password")
	}

	// Check lockout AFTER password verification to maintain constant time
	if player.IsLocked() {
		return nil, "", oops.Code("AUTH_ACCOUNT_LOCKED").
			With("locked_until", player.LockedUntil).
			Errorf("account is temporarily locked")
	}

	// Success - reset failure counter
	player.RecordSuccess()

	// Check if password needs upgrade (e.g., from bcrypt to argon2id)
	if s.hasher.NeedsUpgrade(player.PasswordHash) {
		newHash, hashErr := s.hasher.Hash(password)
		if hashErr == nil {
			player.PasswordHash = newHash
		}
	}

	// Update player with reset failure count (and possibly upgraded hash)
	// Login should succeed even if update fails
	if err := s.players.Update(ctx, player); err != nil {
		s.logger.Warn("best-effort player update failed",
			"event", "player_update_failed",
			"player_id", player.ID.String(),
			"operation", "record_success",
			"error", err.Error(),
		)
	}

	// Generate session token
	token, tokenHash, err := GenerateSessionToken()
	if err != nil {
		return nil, "", oops.Code("AUTH_LOGIN_FAILED").
			With("operation", "generate session token").
			Wrap(err)
	}

	// Create session
	expiresAt := time.Now().Add(SessionTokenExpiry)
	session, err := NewWebSession(player.ID, nil, tokenHash, userAgent, ipAddress, expiresAt)
	if err != nil {
		return nil, "", oops.Code("AUTH_LOGIN_FAILED").
			With("operation", "create web session").
			Wrap(err)
	}

	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, "", oops.Code("AUTH_SESSION_CREATE_FAILED").
			With("operation", "persist session").
			Wrap(err)
	}

	return session, token, nil
}

// Logout invalidates a web session.
func (s *Service) Logout(ctx context.Context, sessionID ulid.ULID) error {
	err := s.sessions.Delete(ctx, sessionID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("SESSION_NOT_FOUND").
				With("session_id", sessionID.String()).
				Wrap(err)
		}
		return oops.Code("AUTH_LOGOUT_FAILED").
			With("operation", "delete session").
			With("session_id", sessionID.String()).
			Wrap(err)
	}
	return nil
}

// ValidateSession validates a session token and returns the session if valid.
// Also updates the LastSeenAt timestamp.
func (s *Service) ValidateSession(ctx context.Context, token string) (*WebSession, error) {
	if token == "" {
		return nil, oops.Code("SESSION_TOKEN_EMPTY").Errorf("session token cannot be empty")
	}

	// Hash the token to look it up
	tokenHash := HashSessionToken(token)

	session, err := s.sessions.GetByTokenHash(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, oops.Code("SESSION_INVALID").Errorf("invalid session token")
		}
		return nil, oops.Code("SESSION_VALIDATE_FAILED").
			With("operation", "get session by token hash").
			Wrap(err)
	}

	// Check if expired
	if session.IsExpired() {
		return nil, oops.Code("SESSION_EXPIRED").Errorf("session has expired")
	}

	// Update last seen timestamp (non-blocking)
	// Validation succeeds even if update fails
	now := time.Now()
	if err := s.sessions.UpdateLastSeen(ctx, session.ID, now); err != nil {
		s.logger.Warn("best-effort session update failed",
			"event", "session_update_failed",
			"session_id", session.ID.String(),
			"operation", "update_last_seen",
			"error", err.Error(),
		)
	}

	return session, nil
}

// SelectCharacter updates the character selection for a session.
func (s *Service) SelectCharacter(ctx context.Context, sessionID, characterID ulid.ULID) error {
	err := s.sessions.UpdateCharacter(ctx, sessionID, characterID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return oops.Code("SESSION_NOT_FOUND").
				With("session_id", sessionID.String()).
				Wrap(err)
		}
		return oops.Code("SESSION_SELECT_CHAR_FAILED").
			With("operation", "update character").
			With("session_id", sessionID.String()).
			With("character_id", characterID.String()).
			Wrap(err)
	}
	return nil
}
