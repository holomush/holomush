// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"errors"
	"time"

	"github.com/samber/oops"
)

// MinPasswordLength is the minimum allowed password length.
const MinPasswordLength = 8

// ValidatePassword validates a password against rules.
// Returns an error if the password is less than MinPasswordLength characters.
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLength {
		return oops.Code("AUTH_INVALID_PASSWORD").
			With("min", MinPasswordLength).
			Errorf("password must be at least %d characters", MinPasswordLength)
	}
	return nil
}

// ValidateCredentials validates username and password without creating a session.
// It uses the same constant-time verification as Login to prevent timing attacks.
// Returns the authenticated Player on success.
func (s *Service) ValidateCredentials(ctx context.Context, username, password string) (*Player, error) {
	player, lookupErr := s.players.GetByUsername(ctx, username)

	var targetHash string
	var playerExists bool

	if lookupErr != nil {
		if errors.Is(lookupErr, ErrNotFound) {
			targetHash = dummyPasswordHash
			playerExists = false
		} else {
			return nil, oops.Code("AUTH_LOGIN_FAILED").
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
		if !playerExists {
			return nil, oops.Code("AUTH_INVALID_CREDENTIALS").Errorf("invalid username or password")
		}
		return nil, oops.Code("AUTH_LOGIN_FAILED").
			With("operation", "verify password").
			Wrap(verifyErr)
	}

	if !playerExists || !valid {
		if playerExists {
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
		return nil, oops.Code("AUTH_INVALID_CREDENTIALS").Errorf("invalid username or password")
	}

	// SECURITY: Check lockout AFTER password verification to maintain constant time.
	if player.IsLocked() {
		return nil, oops.Code("AUTH_ACCOUNT_LOCKED").
			With("locked_until", player.LockedUntil).
			Errorf("account is temporarily locked")
	}

	// Success - reset failure counter
	player.RecordSuccess()

	if err := s.players.Update(ctx, player); err != nil {
		s.logger.Warn("best-effort player update failed",
			"event", "player_update_failed",
			"player_id", player.ID.String(),
			"operation", "record_success",
			"error", err.Error(),
		)
	}

	return player, nil
}

// CreatePlayer creates a new player account with the given credentials.
// Validates the username and password, checks for username availability,
// hashes the password, and persists the new player.
// Returns the created Player and a short-lived PlayerToken for character selection.
func (s *Service) CreatePlayer(ctx context.Context, username, password, email string) (*Player, *PlayerToken, error) {
	if err := ValidateUsername(username); err != nil {
		return nil, nil, oops.Code("REGISTER_INVALID_USERNAME").
			With("username", username).
			With("reason", err.Error()).
			Errorf("invalid username")
	}
	if err := ValidatePassword(password); err != nil {
		return nil, nil, oops.Code("REGISTER_INVALID_PASSWORD").
			With("reason", err.Error()).
			Errorf("invalid password")
	}

	// Check if username is already taken
	_, err := s.players.GetByUsername(ctx, username)
	if err == nil {
		return nil, nil, oops.Code("REGISTER_USERNAME_TAKEN").
			With("username", username).
			Errorf("username %q is already taken", username)
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "check username availability").
			Wrap(err)
	}

	hashedPassword, err := s.hasher.Hash(password)
	if err != nil {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "hash password").
			Wrap(err)
	}

	var emailPtr *string
	if email != "" {
		emailPtr = &email
	}

	player, err := NewPlayer(username, emailPtr, hashedPassword)
	if err != nil {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "create player").
			Wrap(err)
	}

	if createErr := s.players.Create(ctx, player); createErr != nil {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "persist player").
			Wrap(createErr)
	}

	token, err := NewPlayerToken(player.ID, 5*time.Minute)
	if err != nil {
		return nil, nil, oops.Code("REGISTER_FAILED").
			With("operation", "generate player token").
			Wrap(err)
	}

	return player, token, nil
}
