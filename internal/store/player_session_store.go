// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
)

// PostgresPlayerSessionStore implements auth.PlayerSessionRepository using PostgreSQL.
type PostgresPlayerSessionStore struct {
	pool poolIface
}

// NewPostgresPlayerSessionStore creates a new Postgres-backed player session store.
func NewPostgresPlayerSessionStore(pool poolIface) *PostgresPlayerSessionStore {
	return &PostgresPlayerSessionStore{pool: pool}
}

// compile-time check
var _ auth.PlayerSessionRepository = (*PostgresPlayerSessionStore)(nil)

// Create inserts a new player session.
func (s *PostgresPlayerSessionStore) Create(ctx context.Context, session *auth.PlayerSession) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO player_sessions (id, player_id, token_hash, user_agent, ip_address, expires_at, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		session.ID.String(),
		session.PlayerID.String(),
		session.TokenHash,
		session.UserAgent,
		session.IPAddress,
		session.ExpiresAt,
		session.CreatedAt,
		session.UpdatedAt,
	)
	if err != nil {
		return oops.With("operation", "create player session").With("player_id", session.PlayerID.String()).Wrap(err)
	}
	return nil
}

// GetByTokenHash retrieves a player session by its token hash.
// If the session exists but is expired, it is deleted and PLAYER_SESSION_EXPIRED is returned.
func (s *PostgresPlayerSessionStore) GetByTokenHash(ctx context.Context, tokenHash string) (*auth.PlayerSession, error) {
	var ps auth.PlayerSession
	var idStr, playerIDStr string

	err := s.pool.QueryRow(ctx,
		`SELECT id, player_id, token_hash, user_agent, ip_address, expires_at, created_at, updated_at FROM player_sessions WHERE token_hash = $1`,
		tokenHash,
	).Scan(&idStr, &playerIDStr, &ps.TokenHash, &ps.UserAgent, &ps.IPAddress, &ps.ExpiresAt, &ps.CreatedAt, &ps.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("PLAYER_SESSION_NOT_FOUND").With("token_hash_prefix", safePrefix(tokenHash)).Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, oops.With("operation", "get player session").With("token_hash_prefix", safePrefix(tokenHash)).Wrap(err)
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, oops.With("operation", "parse session id").With("raw_id", idStr).Wrap(err)
	}
	ps.ID = id

	playerID, err := ulid.Parse(playerIDStr)
	if err != nil {
		return nil, oops.With("operation", "parse player_id").With("raw_id", playerIDStr).Wrap(err)
	}
	ps.PlayerID = playerID

	if ps.IsExpired() {
		// Clean up the expired session and signal expiry to caller.
		// The conditional WHERE guards against deleting a session that was refreshed by a concurrent request.
		_, _ = s.pool.Exec(ctx, `DELETE FROM player_sessions WHERE id = $1 AND expires_at < now()`, ps.ID.String()) //nolint:errcheck // best-effort cleanup; session already expired
		return nil, oops.Code("PLAYER_SESSION_EXPIRED").With("session_id", ps.ID.String()).Wrap(auth.ErrNotFound)
	}

	return &ps, nil
}

// Delete removes a single session by ID.
func (s *PostgresPlayerSessionStore) Delete(ctx context.Context, id ulid.ULID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM player_sessions WHERE id = $1`, id.String())
	if err != nil {
		return oops.With("operation", "delete player session").With("session_id", id.String()).Wrap(err)
	}
	return nil
}

// DeleteByPlayer removes all sessions for a player.
func (s *PostgresPlayerSessionStore) DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM player_sessions WHERE player_id = $1`, playerID.String())
	if err != nil {
		return oops.With("operation", "delete player sessions by player").With("player_id", playerID.String()).Wrap(err)
	}
	return nil
}

// DeleteExpired removes all sessions whose expiry time has passed and returns
// the number of rows deleted.
func (s *PostgresPlayerSessionStore) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM player_sessions WHERE expires_at < now()`)
	if err != nil {
		return 0, oops.With("operation", "delete expired player sessions").Wrap(err)
	}
	return tag.RowsAffected(), nil
}

// RefreshTTL extends the expiry of a session by ttl from now.
func (s *PostgresPlayerSessionStore) RefreshTTL(ctx context.Context, id ulid.ULID, ttl time.Duration) error {
	if ttl <= 0 {
		return oops.With("operation", "refresh player session ttl").
			With("session_id", id.String()).
			Code("SESSION_INVALID_TTL").
			Errorf("ttl must be positive")
	}
	now := time.Now()
	_, err := s.pool.Exec(ctx,
		`UPDATE player_sessions SET expires_at = $1, updated_at = $2 WHERE id = $3`,
		now.Add(ttl),
		now,
		id.String(),
	)
	if err != nil {
		return oops.With("operation", "refresh player session ttl").With("session_id", id.String()).Wrap(err)
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
