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

// CreateWithCap atomically inserts the new session and trims oldest non-expired
// sessions for the same player so the total active count is at most maxActive.
// A maxActive value <= 0 disables trimming (equivalent to Create). Returns the
// ULIDs of the trimmed PlayerSessions so callers can emit session_ended events
// for child game sessions before they cascade-delete.
//
// The INSERT + trim DELETE execute in a single transaction so any failure
// rolls back both. The DELETE uses ORDER BY created_at DESC + OFFSET
// (maxActive - 1) to skip the newest (maxActive - 1) other sessions (which we
// keep alongside the just-inserted one) and deletes the rest — i.e. the oldest
// sessions beyond the cap. Combined with id != $new_id this guarantees the
// just-inserted session is never trimmed, leaving the player with exactly
// min(maxActive, total) active sessions.
func (s *PostgresPlayerSessionStore) CreateWithCap(ctx context.Context, session *auth.PlayerSession, maxActive int) ([]ulid.ULID, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, oops.Code("PLAYER_SESSION_TX_BEGIN_FAILED").
			With("player_id", session.PlayerID.String()).Wrap(err)
	}
	// Rollback is a no-op once Commit succeeds.
	defer tx.Rollback(ctx) //nolint:errcheck // rollback after commit is a no-op

	// Serialize concurrent CreateWithCap calls for the same player via a
	// transaction-scoped advisory lock keyed on the player_id hash. Without
	// this, two concurrent transactions at READ COMMITTED each see the
	// pre-insert snapshot, each trim to cap-1, each insert → the player ends
	// up above cap. The lock is released automatically at COMMIT/ROLLBACK.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, session.PlayerID.String()); err != nil {
		return nil, oops.Code("PLAYER_SESSION_LOCK_FAILED").
			With("player_id", session.PlayerID.String()).Wrap(err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO player_sessions (id, player_id, token_hash, user_agent, ip_address, expires_at, created_at, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		session.ID.String(),
		session.PlayerID.String(),
		session.TokenHash,
		session.UserAgent,
		session.IPAddress,
		session.ExpiresAt,
		session.CreatedAt,
		session.UpdatedAt,
	); err != nil {
		return nil, oops.Code("PLAYER_SESSION_CREATE_FAILED").
			With("player_id", session.PlayerID.String()).Wrap(err)
	}

	var trimmedIDs []ulid.ULID
	if maxActive > 0 {
		rows, trimErr := tx.Query(ctx, `
			DELETE FROM player_sessions
			WHERE id IN (
				SELECT id FROM player_sessions
				WHERE player_id = $1 AND id != $2 AND expires_at > now()
				ORDER BY created_at DESC
				OFFSET $3
			)
			RETURNING id
		`, session.PlayerID.String(), session.ID.String(), maxActive-1)
		if trimErr != nil {
			return nil, oops.Code("PLAYER_SESSION_TRIM_FAILED").
				With("player_id", session.PlayerID.String()).Wrap(trimErr)
		}
		defer rows.Close()
		for rows.Next() {
			var idStr string
			if scanErr := rows.Scan(&idStr); scanErr != nil {
				return nil, oops.Code("PLAYER_SESSION_TRIM_SCAN_FAILED").
					With("player_id", session.PlayerID.String()).Wrap(scanErr)
			}
			parsedID, parseErr := ulid.Parse(idStr)
			if parseErr != nil {
				return nil, oops.Code("PLAYER_SESSION_TRIM_PARSE_FAILED").
					With("raw_id", idStr).Wrap(parseErr)
			}
			trimmedIDs = append(trimmedIDs, parsedID)
		}
		if rows.Err() != nil {
			return nil, oops.Code("PLAYER_SESSION_TRIM_FAILED").
				With("player_id", session.PlayerID.String()).Wrap(rows.Err())
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, oops.Code("PLAYER_SESSION_TX_COMMIT_FAILED").
			With("player_id", session.PlayerID.String()).Wrap(err)
	}
	return trimmedIDs, nil
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

// GetByID retrieves a player session by its ULID primary key.
// Returns auth.ErrNotFound if no row exists.
func (s *PostgresPlayerSessionStore) GetByID(ctx context.Context, id ulid.ULID) (*auth.PlayerSession, error) {
	var ps auth.PlayerSession
	var idStr, playerIDStr string

	err := s.pool.QueryRow(ctx,
		`SELECT id, player_id, token_hash, user_agent, ip_address, expires_at, created_at, updated_at FROM player_sessions WHERE id = $1`,
		id.String(),
	).Scan(&idStr, &playerIDStr, &ps.TokenHash, &ps.UserAgent, &ps.IPAddress, &ps.ExpiresAt, &ps.CreatedAt, &ps.UpdatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("PLAYER_SESSION_NOT_FOUND").With("session_id", id.String()).Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("PLAYER_SESSION_GET_BY_ID_FAILED").With("session_id", id.String()).Wrap(err)
	}

	parsedID, err := ulid.Parse(idStr)
	if err != nil {
		return nil, oops.With("operation", "parse session id").With("raw_id", idStr).Wrap(err)
	}
	ps.ID = parsedID

	playerID, err := ulid.Parse(playerIDStr)
	if err != nil {
		return nil, oops.With("operation", "parse player_id").With("raw_id", playerIDStr).Wrap(err)
	}
	ps.PlayerID = playerID

	return &ps, nil
}

// CountActiveByPlayer returns the number of non-expired sessions for a player.
func (s *PostgresPlayerSessionStore) CountActiveByPlayer(ctx context.Context, playerID ulid.ULID) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM player_sessions WHERE player_id = $1 AND expires_at > now()`,
		playerID.String(),
	).Scan(&n)
	if err != nil {
		return 0, oops.Code("PLAYER_SESSION_COUNT_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	return n, nil
}

// ListByPlayer returns all non-expired sessions for a player, newest first.
func (s *PostgresPlayerSessionStore) ListByPlayer(ctx context.Context, playerID ulid.ULID) ([]*auth.PlayerSession, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, player_id, token_hash, user_agent, ip_address, expires_at, created_at, updated_at
		 FROM player_sessions
		 WHERE player_id = $1 AND expires_at > now()
		 ORDER BY created_at DESC`,
		playerID.String(),
	)
	if err != nil {
		return nil, oops.Code("PLAYER_SESSION_LIST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	defer rows.Close()

	var sessions []*auth.PlayerSession
	for rows.Next() {
		var ps auth.PlayerSession
		var idStr, playerIDStr string
		if scanErr := rows.Scan(
			&idStr, &playerIDStr, &ps.TokenHash, &ps.UserAgent, &ps.IPAddress,
			&ps.ExpiresAt, &ps.CreatedAt, &ps.UpdatedAt,
		); scanErr != nil {
			return nil, oops.Code("PLAYER_SESSION_LIST_SCAN_FAILED").With("player_id", playerID.String()).Wrap(scanErr)
		}
		parsedID, parseErr := ulid.Parse(idStr)
		if parseErr != nil {
			return nil, oops.With("operation", "parse session id").With("raw_id", idStr).Wrap(parseErr)
		}
		ps.ID = parsedID
		parsedPlayerID, parseErr := ulid.Parse(playerIDStr)
		if parseErr != nil {
			return nil, oops.With("operation", "parse player_id").With("raw_id", playerIDStr).Wrap(parseErr)
		}
		ps.PlayerID = parsedPlayerID
		sessions = append(sessions, &ps)
	}
	if rows.Err() != nil {
		return nil, oops.Code("PLAYER_SESSION_LIST_FAILED").With("player_id", playerID.String()).Wrap(rows.Err())
	}
	return sessions, nil
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

// DeleteOldestForPlayer deletes the single oldest non-expired session for the
// player using a single-round-trip DELETE ... WHERE id = (SELECT ...). Returns
// the deleted session (for logging) or (nil, nil) if the player had no active
// sessions.
func (s *PostgresPlayerSessionStore) DeleteOldestForPlayer(ctx context.Context, playerID ulid.ULID) (*auth.PlayerSession, error) {
	var idStr string
	err := s.pool.QueryRow(ctx, `
		DELETE FROM player_sessions
		WHERE id = (
			SELECT id FROM player_sessions
			WHERE player_id = $1 AND expires_at > now()
			ORDER BY created_at ASC
			LIMIT 1
		)
		RETURNING id
	`, playerID.String()).Scan(&idStr)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // explicit "no rows" signal per interface contract
	}
	if err != nil {
		return nil, oops.Code("PLAYER_SESSION_DELETE_OLDEST_FAILED").With("player_id", playerID.String()).Wrap(err)
	}
	deletedID, parseErr := ulid.Parse(idStr)
	if parseErr != nil {
		return nil, oops.Code("PLAYER_SESSION_DELETE_OLDEST_PARSE_FAILED").With("raw_id", idStr).Wrap(parseErr)
	}
	return &auth.PlayerSession{ID: deletedID, PlayerID: playerID}, nil
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
