// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
)

// PostgresPlayerTokenStore implements auth.PlayerTokenRepository using PostgreSQL.
type PostgresPlayerTokenStore struct {
	pool poolIface
}

// NewPostgresPlayerTokenStore creates a new Postgres-backed player token store.
func NewPostgresPlayerTokenStore(pool poolIface) *PostgresPlayerTokenStore {
	return &PostgresPlayerTokenStore{pool: pool}
}

// compile-time check
var _ auth.PlayerTokenRepository = (*PostgresPlayerTokenStore)(nil)

// Create inserts a new player token.
func (s *PostgresPlayerTokenStore) Create(ctx context.Context, token *auth.PlayerToken) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO player_tokens (token, player_id, created_at, expires_at) VALUES ($1, $2, $3, $4)`,
		token.Token,
		token.PlayerID.String(),
		token.CreatedAt,
		token.ExpiresAt,
	)
	if err != nil {
		return oops.With("operation", "create player token").With("player_id", token.PlayerID.String()).Wrap(err)
	}
	return nil
}

// GetByToken retrieves a player token by its token value.
// If the token exists but is expired, it is deleted and TOKEN_EXPIRED is returned.
func (s *PostgresPlayerTokenStore) GetByToken(ctx context.Context, token string) (*auth.PlayerToken, error) {
	var pt auth.PlayerToken
	var playerIDStr string

	err := s.pool.QueryRow(ctx,
		`SELECT token, player_id, created_at, expires_at FROM player_tokens WHERE token = $1`,
		token,
	).Scan(&pt.Token, &playerIDStr, &pt.CreatedAt, &pt.ExpiresAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("TOKEN_NOT_FOUND").With("token_prefix", safePrefix(token)).Wrap(err)
	}
	if err != nil {
		return nil, oops.With("operation", "get player token").With("token_prefix", safePrefix(token)).Wrap(err)
	}

	playerID, err := ulid.Parse(playerIDStr)
	if err != nil {
		return nil, oops.With("operation", "parse player_id").With("raw_id", playerIDStr).Wrap(err)
	}
	pt.PlayerID = playerID

	if pt.IsExpired() {
		// Clean up the expired token and signal expiry to caller.
		_, _ = s.pool.Exec(ctx, `DELETE FROM player_tokens WHERE token = $1`, token) //nolint:errcheck // best-effort cleanup; token already expired
		return nil, oops.Code("TOKEN_EXPIRED").With("token_prefix", safePrefix(token)).Errorf("token has expired")
	}

	return &pt, nil
}

// DeleteByToken removes a single token.
func (s *PostgresPlayerTokenStore) DeleteByToken(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM player_tokens WHERE token = $1`, token)
	if err != nil {
		return oops.With("operation", "delete player token").With("token_prefix", safePrefix(token)).Wrap(err)
	}
	return nil
}

// safePrefix returns the first 8 characters of a token for safe logging.
func safePrefix(token string) string {
	if len(token) <= 8 {
		return token
	}
	return token[:8]
}

// DeleteByPlayer removes all tokens for a player.
func (s *PostgresPlayerTokenStore) DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM player_tokens WHERE player_id = $1`, playerID.String())
	if err != nil {
		return oops.With("operation", "delete player tokens by player").With("player_id", playerID.String()).Wrap(err)
	}
	return nil
}

// DeleteExpired removes all tokens whose expiry time has passed and returns
// the number of rows deleted.
func (s *PostgresPlayerTokenStore) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM player_tokens WHERE expires_at < now()`)
	if err != nil {
		return 0, oops.With("operation", "delete expired player tokens").Wrap(err)
	}
	return tag.RowsAffected(), nil
}
