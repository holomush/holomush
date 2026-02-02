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

// PasswordResetService handles password reset operations.
type PasswordResetService struct {
	playerRepo PlayerRepository
	resetRepo  PasswordResetRepository
	hasher     PasswordHasher
	logger     *slog.Logger
}

// NewPasswordResetService creates a new PasswordResetService with a no-op logger.
// Returns an error if any required dependency is nil.
func NewPasswordResetService(
	playerRepo PlayerRepository,
	resetRepo PasswordResetRepository,
	hasher PasswordHasher,
) (*PasswordResetService, error) {
	if playerRepo == nil {
		return nil, oops.Errorf("player repository is required")
	}
	if resetRepo == nil {
		return nil, oops.Errorf("reset repository is required")
	}
	if hasher == nil {
		return nil, oops.Errorf("password hasher is required")
	}
	return &PasswordResetService{
		playerRepo: playerRepo,
		resetRepo:  resetRepo,
		hasher:     hasher,
		logger:     slog.New(slog.DiscardHandler),
	}, nil
}

// NewPasswordResetServiceWithLogger creates a new PasswordResetService with the provided logger.
// Returns an error if any required dependency is nil.
func NewPasswordResetServiceWithLogger(
	playerRepo PlayerRepository,
	resetRepo PasswordResetRepository,
	hasher PasswordHasher,
	logger *slog.Logger,
) (*PasswordResetService, error) {
	if playerRepo == nil {
		return nil, oops.Errorf("player repository is required")
	}
	if resetRepo == nil {
		return nil, oops.Errorf("reset repository is required")
	}
	if hasher == nil {
		return nil, oops.Errorf("password hasher is required")
	}
	if logger == nil {
		return nil, oops.Errorf("logger is required")
	}
	return &PasswordResetService{
		playerRepo: playerRepo,
		resetRepo:  resetRepo,
		hasher:     hasher,
		logger:     logger,
	}, nil
}

// RequestReset requests a password reset for a player by email.
// If the player exists, generates a reset token and stores the hash.
// Returns the plaintext token for sending via email (email sending is NOT this service's job).
// If the player doesn't exist, returns success anyway (empty token) to prevent email enumeration.
func (s *PasswordResetService) RequestReset(ctx context.Context, email string) (string, error) {
	player, err := s.playerRepo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Return success with empty token to prevent email enumeration
			return "", nil
		}
		return "", oops.Code("RESET_REQUEST_FAILED").
			With("operation", "GetByEmail").
			Wrap(err)
	}

	token, hash, err := GenerateResetToken()
	if err != nil {
		return "", oops.Code("RESET_REQUEST_FAILED").
			With("operation", "GenerateResetToken").
			Wrap(err)
	}

	// Create password reset record
	reset, err := NewPasswordReset(player.ID, hash, time.Now().Add(ResetTokenExpiry))
	if err != nil {
		return "", oops.Code("RESET_REQUEST_FAILED").
			With("operation", "NewPasswordReset").
			Wrap(err)
	}

	if err := s.resetRepo.Create(ctx, reset); err != nil {
		return "", oops.Code("RESET_REQUEST_FAILED").
			With("operation", "Create").
			Wrap(err)
	}

	return token, nil
}

// ValidateToken validates a reset token and returns the associated player ID.
// Returns an error if the token is invalid, expired, or not found.
func (s *PasswordResetService) ValidateToken(ctx context.Context, token string) (ulid.ULID, error) {
	if token == "" {
		return ulid.ULID{}, oops.Code("RESET_TOKEN_EMPTY").Errorf("reset token cannot be empty")
	}

	// Hash the token to look it up
	hash := hashResetToken(token)

	// Look up the reset by hash
	reset, err := s.resetRepo.GetByTokenHash(ctx, hash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return ulid.ULID{}, oops.Code("RESET_TOKEN_INVALID").Errorf("reset token not found")
		}
		return ulid.ULID{}, oops.Code("RESET_VALIDATE_FAILED").
			With("operation", "GetByTokenHash").
			Wrap(err)
	}

	// Check if expired
	if reset.IsExpired() {
		return ulid.ULID{}, oops.Code("RESET_TOKEN_EXPIRED").Errorf("reset token has expired")
	}

	return reset.PlayerID, nil
}

// ResetPassword resets a player's password using a valid reset token.
// Validates the token, hashes the new password, updates the player's password,
// and deletes all reset tokens for the player.
func (s *PasswordResetService) ResetPassword(ctx context.Context, token, newPassword string) error {
	// Validate password first (defense in depth - hasher also checks, but be explicit)
	if newPassword == "" {
		return oops.Code("RESET_PASSWORD_EMPTY").Errorf("new password cannot be empty")
	}

	playerID, err := s.ValidateToken(ctx, token)
	if err != nil {
		return err // Already has appropriate error code
	}

	hashedPassword, err := s.hasher.Hash(newPassword)
	if err != nil {
		return oops.Code("RESET_PASSWORD_FAILED").
			With("operation", "Hash").
			Wrap(err)
	}

	if err := s.playerRepo.UpdatePassword(ctx, playerID, hashedPassword); err != nil {
		return oops.Code("RESET_PASSWORD_FAILED").
			With("operation", "UpdatePassword").
			Wrap(err)
	}

	// Delete all reset tokens for the player.
	// This is cleanup - if it fails, the password was still updated successfully.
	if err := s.resetRepo.DeleteByPlayer(ctx, playerID); err != nil {
		s.logger.Warn("best-effort token cleanup failed",
			"event", "token_cleanup_failed",
			"player_id", playerID.String(),
			"operation", "delete_tokens",
			"error", err.Error(),
		)
	}

	return nil
}
