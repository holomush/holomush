// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

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

// Session token configuration.
const (
	SessionTokenBytes  = 32             // 32 bytes = 64 hex chars
	SessionTokenExpiry = 24 * time.Hour // 24 hour expiry
)

// WebSession represents a web client session.
type WebSession struct {
	ID          ulid.ULID
	PlayerID    ulid.ULID
	CharacterID *ulid.ULID // nil if character not yet selected
	TokenHash   string
	UserAgent   string
	IPAddress   string
	ExpiresAt   time.Time
	CreatedAt   time.Time
	LastSeenAt  time.Time
}

// NewWebSession creates a validated WebSession instance.
// Returns an error if any required fields are invalid.
// CharacterID is optional and may be nil.
// UserAgent and IPAddress are optional and may be empty.
func NewWebSession(playerID ulid.ULID, characterID *ulid.ULID, tokenHash, userAgent, ipAddress string, expiresAt time.Time) (*WebSession, error) {
	if playerID.Compare(ulid.ULID{}) == 0 {
		return nil, oops.Code("SESSION_INVALID_PLAYER").Errorf("player ID cannot be zero")
	}
	if characterID != nil && characterID.Compare(ulid.ULID{}) == 0 {
		return nil, oops.Code("SESSION_INVALID_CHARACTER").Errorf("character ID cannot be zero when provided")
	}
	if tokenHash == "" {
		return nil, oops.Code("SESSION_INVALID_HASH").Errorf("token hash cannot be empty")
	}
	if expiresAt.IsZero() {
		return nil, oops.Code("SESSION_INVALID_EXPIRY").Errorf("expiry time cannot be zero")
	}

	now := time.Now()
	return &WebSession{
		ID:          ulid.Make(),
		PlayerID:    playerID,
		CharacterID: characterID,
		TokenHash:   tokenHash,
		UserAgent:   userAgent,
		IPAddress:   ipAddress,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
		LastSeenAt:  now,
	}, nil
}

// IsExpired returns true if the session has expired.
func (s *WebSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// IsExpiredAt returns true if the session would be expired at the given time.
// Useful for testing with deterministic time values.
func (s *WebSession) IsExpiredAt(t time.Time) bool {
	return t.After(s.ExpiresAt)
}

// GenerateSessionToken creates a secure random token and its hash.
// Returns (plaintext_token, sha256_hash, error).
// The plaintext token is sent to the client; the hash is stored in the database.
func GenerateSessionToken() (token, hash string, err error) {
	tokenBytes := make([]byte, SessionTokenBytes)
	if _, err = rand.Read(tokenBytes); err != nil {
		return "", "", oops.Code("SESSION_TOKEN_GENERATE_FAILED").
			With("operation", "crypto/rand.Read").
			With("requested_bytes", SessionTokenBytes).
			Wrap(err)
	}

	token = hex.EncodeToString(tokenBytes)
	hash = HashSessionToken(token)

	return token, hash, nil
}

// HashSessionToken computes the SHA256 hash of a session token.
// This is used to securely store tokens in the database.
func HashSessionToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// VerifySessionToken checks if the plaintext token matches the stored hash.
// Uses constant-time comparison to prevent timing attacks.
// Returns (true, nil) on match, (false, nil) on mismatch, or (false, error) on invalid input.
func VerifySessionToken(token, hash string) (bool, error) {
	if token == "" {
		return false, oops.Code("SESSION_TOKEN_EMPTY").Errorf("session token cannot be empty")
	}
	if hash == "" {
		return false, oops.Code("SESSION_HASH_EMPTY").Errorf("stored hash cannot be empty")
	}
	computed := HashSessionToken(token)
	// Both are hex-encoded SHA256 hashes (64 chars), use constant-time compare
	return subtle.ConstantTimeCompare([]byte(computed), []byte(hash)) == 1, nil
}

// WebSessionRepository manages web session persistence.
type WebSessionRepository interface {
	// Create stores a new web session.
	Create(ctx context.Context, session *WebSession) error

	// GetByID retrieves a session by its ID.
	GetByID(ctx context.Context, id ulid.ULID) (*WebSession, error)

	// GetByTokenHash retrieves a session by its token hash.
	GetByTokenHash(ctx context.Context, tokenHash string) (*WebSession, error)

	// GetByPlayer retrieves all active sessions for a player.
	GetByPlayer(ctx context.Context, playerID ulid.ULID) ([]*WebSession, error)

	// UpdateLastSeen updates the LastSeenAt timestamp for a session.
	UpdateLastSeen(ctx context.Context, id ulid.ULID, lastSeen time.Time) error

	// UpdateCharacter updates the CharacterID for a session.
	UpdateCharacter(ctx context.Context, id ulid.ULID, characterID ulid.ULID) error

	// Delete removes a session by ID.
	Delete(ctx context.Context, id ulid.ULID) error

	// DeleteByPlayer removes all sessions for a player.
	DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error

	// DeleteExpired removes all expired sessions and returns the count
	// of deleted records.
	DeleteExpired(ctx context.Context) (int64, error)
}
