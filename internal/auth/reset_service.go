// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"errors"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// PasswordResetService handles password reset operations.
type PasswordResetService struct {
	playerRepo PlayerRepository
	resetRepo  PasswordResetRepository
	hasher     PasswordHasher
}

// NewPasswordResetService creates a new PasswordResetService.
func NewPasswordResetService(
	playerRepo PlayerRepository,
	resetRepo PasswordResetRepository,
	hasher PasswordHasher,
) *PasswordResetService {
	return &PasswordResetService{
		playerRepo: playerRepo,
		resetRepo:  resetRepo,
		hasher:     hasher,
	}
}

// RequestReset requests a password reset for a player by email.
// If the player exists, generates a reset token and stores the hash.
// Returns the plaintext token for sending via email (email sending is NOT this service's job).
// If the player doesn't exist, returns success anyway (empty token) to prevent email enumeration.
func (s *PasswordResetService) RequestReset(ctx context.Context, email string) (string, error) {
	// Look up player by email
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

	// Generate reset token
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

	// Store the reset
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

	// Validate the token
	playerID, err := s.ValidateToken(ctx, token)
	if err != nil {
		return err // Already has appropriate error code
	}

	// Hash the new password
	hashedPassword, err := s.hasher.Hash(newPassword)
	if err != nil {
		return oops.Code("RESET_PASSWORD_FAILED").
			With("operation", "Hash").
			Wrap(err)
	}

	// Update the player's password
	if err := s.playerRepo.UpdatePassword(ctx, playerID, hashedPassword); err != nil {
		return oops.Code("RESET_PASSWORD_FAILED").
			With("operation", "UpdatePassword").
			Wrap(err)
	}

	// Delete all reset tokens for the player.
	// This is cleanup - if it fails, the password was still updated successfully.
	// The error is intentionally ignored because the main operation (password update) succeeded.
	//nolint:errcheck // Cleanup failure is acceptable; password was already updated
	s.resetRepo.DeleteByPlayer(ctx, playerID)

	return nil
}
