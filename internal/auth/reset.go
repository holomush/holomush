// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package auth provides authentication primitives for HoloMUSH.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"
)

// Reset token configuration.
const (
	ResetTokenBytes  = 32        // 32 bytes = 64 hex chars
	ResetTokenExpiry = time.Hour // 1 hour expiry
)

// PasswordReset represents a password reset request.
type PasswordReset struct {
	ID        ulid.ULID
	PlayerID  ulid.ULID
	TokenHash string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// IsExpired returns true if the reset token has expired.
func (r *PasswordReset) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// GenerateResetToken creates a secure random token and its hash.
// Returns (plaintext_token, sha256_hash, error).
// The plaintext token is sent to the user; the hash is stored in the database.
func GenerateResetToken() (token, hash string, err error) {
	tokenBytes := make([]byte, ResetTokenBytes)
	if _, err = rand.Read(tokenBytes); err != nil {
		return "", "", oops.Code("RESET_TOKEN_GENERATE_FAILED").Wrap(err)
	}

	token = hex.EncodeToString(tokenBytes)
	hash = hashResetToken(token)

	return token, hash, nil
}

// VerifyResetToken checks if the plaintext token matches the stored hash.
// Uses constant-time comparison to prevent timing attacks.
func VerifyResetToken(token, hash string) bool {
	if token == "" || hash == "" {
		return false
	}
	computed := hashResetToken(token)
	// Both are hex-encoded SHA256 hashes (64 chars), use constant-time compare
	return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1
}

// hashResetToken computes the SHA256 hash of a token.
func hashResetToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// PasswordResetRepository manages password reset persistence.
type PasswordResetRepository interface {
	// Create stores a new password reset request.
	Create(ctx context.Context, reset *PasswordReset) error

	// GetByPlayer retrieves the latest reset request for a player.
	GetByPlayer(ctx context.Context, playerID ulid.ULID) (*PasswordReset, error)

	// GetByTokenHash retrieves a reset request by its token hash.
	GetByTokenHash(ctx context.Context, tokenHash string) (*PasswordReset, error)

	// Delete removes a password reset request.
	Delete(ctx context.Context, id ulid.ULID) error

	// DeleteByPlayer removes all reset requests for a player.
	DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error

	// DeleteExpired removes all expired reset requests.
	DeleteExpired(ctx context.Context) (int64, error)
}
