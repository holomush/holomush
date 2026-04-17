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

	"github.com/holomush/holomush/internal/idgen"
)

// Session token configuration.
const (
	SessionTokenBytes  = 32             // 32 bytes = 64 hex chars
	SessionTokenExpiry = 24 * time.Hour // 24 hour expiry
)

// PlayerSessionTTL is the default time-to-live for a player session.
const PlayerSessionTTL = 24 * time.Hour

// PlayerSession represents a durable authenticated session for a player.
// It persists across connections and uses a sliding 24h TTL.
type PlayerSession struct {
	ID        ulid.ULID
	PlayerID  ulid.ULID
	TokenHash string
	UserAgent string
	IPAddress string
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewPlayerSession creates a validated PlayerSession.
// Returns an error if playerID is the zero ULID or tokenHash is empty.
// UserAgent and IPAddress are optional and may be empty.
func NewPlayerSession(playerID ulid.ULID, tokenHash, userAgent, ipAddress string, ttl time.Duration) (*PlayerSession, error) {
	if playerID.Compare(ulid.ULID{}) == 0 {
		return nil, oops.Code("SESSION_INVALID_PLAYER").Errorf("player ID cannot be zero")
	}
	if tokenHash == "" {
		return nil, oops.Code("SESSION_INVALID_HASH").Errorf("token hash cannot be empty")
	}
	if ttl <= 0 {
		return nil, oops.Code("SESSION_INVALID_TTL").Errorf("ttl must be positive")
	}

	now := time.Now()
	return &PlayerSession{
		ID:        idgen.New(),
		PlayerID:  playerID,
		TokenHash: tokenHash,
		UserAgent: userAgent,
		IPAddress: ipAddress,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// IsExpired returns true if the session has passed its expiry time.
func (s *PlayerSession) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// Refresh extends the session's expiry by ttl from now and updates UpdatedAt.
func (s *PlayerSession) Refresh(ttl time.Duration) error {
	if ttl <= 0 {
		return oops.Code("SESSION_INVALID_TTL").Errorf("ttl must be positive")
	}
	now := time.Now()
	s.ExpiresAt = now.Add(ttl)
	s.UpdatedAt = now
	return nil
}

// PlayerSessionRepository manages player session persistence.
type PlayerSessionRepository interface {
	// Create stores a new player session.
	Create(ctx context.Context, session *PlayerSession) error

	// CreateWithCap atomically inserts the new session and trims oldest
	// non-expired sessions for the same player so the total active count is at
	// most maxActive. A maxActive value <= 0 disables trimming (equivalent to
	// Create). Returns the number of rows trimmed for observability.
	//
	// All operations run in a single transaction: any failure rolls back both
	// the insert and any trimming. This eliminates three correctness gaps in
	// the previous Count + DeleteOldest + Create flow:
	//   - Concurrent logins at cap both observe count == cap, both evict once,
	//     both insert → player ends up at cap + 1.
	//   - Lowering the operator-configured cap with sessions already over the
	//     new limit only evicts a single session per login, taking many
	//     logins to catch up.
	//   - A Create failure after a successful eviction silently leaves the
	//     player below cap with no replacement session.
	CreateWithCap(ctx context.Context, session *PlayerSession, maxActive int) (int, error)

	// GetByTokenHash retrieves a session by its token hash.
	GetByTokenHash(ctx context.Context, tokenHash string) (*PlayerSession, error)

	// GetByID retrieves a session by its ULID primary key. Returns ErrNotFound
	// if no row exists.
	GetByID(ctx context.Context, id ulid.ULID) (*PlayerSession, error)

	// CountActiveByPlayer returns the number of non-expired PlayerSessions
	// owned by the given player.
	CountActiveByPlayer(ctx context.Context, playerID ulid.ULID) (int, error)

	// ListByPlayer returns all non-expired PlayerSessions owned by the given
	// player, ordered by CreatedAt descending (newest first).
	ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*PlayerSession, error)

	// Delete removes a session by ID.
	Delete(ctx context.Context, id ulid.ULID) error

	// DeleteByPlayer removes all sessions for a player.
	DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error

	// DeleteOldestForPlayer deletes the single oldest non-expired PlayerSession
	// for the player. Returns the deleted session (for logging) or nil if the
	// player had no active sessions.
	DeleteOldestForPlayer(ctx context.Context, playerID ulid.ULID) (*PlayerSession, error)

	// DeleteExpired removes all expired sessions and returns the count of deleted records.
	DeleteExpired(ctx context.Context) (int64, error)

	// RefreshTTL extends the expiry of a session by ttl from now.
	RefreshTTL(ctx context.Context, id ulid.ULID, ttl time.Duration) error
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
//
// SECURITY: Without constant-time comparison, an attacker could deduce valid token
// prefixes byte-by-byte by measuring response times. Standard string comparison
// returns early on the first mismatched byte, leaking information about how many
// leading bytes matched. subtle.ConstantTimeCompare always takes the same time
// regardless of where (or if) the strings differ.
//
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
