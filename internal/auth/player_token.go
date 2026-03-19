// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"context"
	"time"

	"github.com/oklog/ulid/v2"
)

// PlayerToken is an opaque token for two-phase login.
// Players authenticate once to get a token, then use it for
// character selection without re-entering credentials.
type PlayerToken struct {
	Token     string
	PlayerID  ulid.ULID
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewPlayerToken creates a player token with a ULID as the token value.
func NewPlayerToken(playerID ulid.ULID, ttl time.Duration) (*PlayerToken, error) {
	now := time.Now()
	return &PlayerToken{
		Token:     ulid.Make().String(),
		PlayerID:  playerID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}, nil
}

// IsExpired returns true if the token has passed its expiry time.
func (t *PlayerToken) IsExpired() bool {
	return time.Now().After(t.ExpiresAt)
}

// PlayerTokenRepository manages player token persistence.
type PlayerTokenRepository interface {
	Create(ctx context.Context, token *PlayerToken) error
	GetByToken(ctx context.Context, token string) (*PlayerToken, error)
	DeleteByToken(ctx context.Context, token string) error
	DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error
	DeleteExpired(ctx context.Context) (int64, error)
}
