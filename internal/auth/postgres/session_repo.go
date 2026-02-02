// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
)

// WebSessionRepository implements auth.WebSessionRepository using PostgreSQL.
type WebSessionRepository struct {
	pool *pgxpool.Pool
}

// NewWebSessionRepository creates a new WebSessionRepository.
func NewWebSessionRepository(pool *pgxpool.Pool) *WebSessionRepository {
	return &WebSessionRepository{pool: pool}
}

// Create stores a new web session.
func (r *WebSessionRepository) Create(ctx context.Context, session *auth.WebSession) error {
	var charIDStr *string
	if session.CharacterID != nil {
		s := session.CharacterID.String()
		charIDStr = &s
	}

	_, err := r.pool.Exec(ctx, `
		INSERT INTO web_sessions (id, player_id, character_id, token_hash, user_agent, ip_address, expires_at, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`,
		session.ID.String(),
		session.PlayerID.String(),
		charIDStr,
		session.TokenHash,
		session.UserAgent,
		session.IPAddress,
		session.ExpiresAt,
		session.CreatedAt,
		session.LastSeenAt,
	)
	if err != nil {
		return oops.Code("SESSION_CREATE_FAILED").
			With("operation", "insert web_session").
			With("player_id", session.PlayerID.String()).
			Wrap(err)
	}
	return nil
}

// GetByID retrieves a session by its ID.
func (r *WebSessionRepository) GetByID(ctx context.Context, id ulid.ULID) (*auth.WebSession, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, player_id, character_id, token_hash, user_agent, ip_address, expires_at, created_at, last_seen_at
		FROM web_sessions
		WHERE id = $1
	`, id.String())

	session, err := r.scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("SESSION_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("SESSION_GET_BY_ID_FAILED").
			With("operation", "get session by id").
			With("id", id.String()).
			Wrap(err)
	}
	return session, nil
}

// GetByTokenHash retrieves a session by its token hash.
func (r *WebSessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*auth.WebSession, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, player_id, character_id, token_hash, user_agent, ip_address, expires_at, created_at, last_seen_at
		FROM web_sessions
		WHERE token_hash = $1
	`, tokenHash)

	session, err := r.scanSession(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("SESSION_NOT_FOUND").Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("SESSION_GET_BY_TOKEN_FAILED").
			With("operation", "get session by token hash").
			Wrap(err)
	}
	return session, nil
}

// GetByPlayer retrieves all active sessions for a player.
func (r *WebSessionRepository) GetByPlayer(ctx context.Context, playerID ulid.ULID) ([]*auth.WebSession, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, player_id, character_id, token_hash, user_agent, ip_address, expires_at, created_at, last_seen_at
		FROM web_sessions
		WHERE player_id = $1
		ORDER BY created_at DESC
	`, playerID.String())
	if err != nil {
		return nil, oops.Code("SESSION_GET_BY_PLAYER_FAILED").
			With("operation", "get sessions by player").
			With("player_id", playerID.String()).
			Wrap(err)
	}
	defer rows.Close()

	var sessions []*auth.WebSession
	for rows.Next() {
		session, err := r.scanSessionRow(rows)
		if err != nil {
			return nil, oops.Code("SESSION_SCAN_FAILED").
				With("operation", "scan session row").
				Wrap(err)
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, oops.Code("SESSION_ROWS_ERROR").
			With("operation", "iterate session rows").
			Wrap(err)
	}

	return sessions, nil
}

// UpdateLastSeen updates the LastSeenAt timestamp for a session.
func (r *WebSessionRepository) UpdateLastSeen(ctx context.Context, id ulid.ULID, lastSeen time.Time) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE web_sessions SET last_seen_at = $2
		WHERE id = $1
	`, id.String(), lastSeen)
	if err != nil {
		return oops.Code("SESSION_UPDATE_LAST_SEEN_FAILED").
			With("operation", "update last_seen_at").
			With("id", id.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("SESSION_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// UpdateCharacter updates the CharacterID for a session.
func (r *WebSessionRepository) UpdateCharacter(ctx context.Context, id, characterID ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE web_sessions SET character_id = $2
		WHERE id = $1
	`, id.String(), characterID.String())
	if err != nil {
		return oops.Code("SESSION_UPDATE_CHARACTER_FAILED").
			With("operation", "update character_id").
			With("id", id.String()).
			With("character_id", characterID.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("SESSION_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// Delete removes a session by ID.
func (r *WebSessionRepository) Delete(ctx context.Context, id ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM web_sessions WHERE id = $1
	`, id.String())
	if err != nil {
		return oops.Code("SESSION_DELETE_FAILED").
			With("operation", "delete web_session").
			With("id", id.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("SESSION_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// DeleteByPlayer removes all sessions for a player.
func (r *WebSessionRepository) DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM web_sessions WHERE player_id = $1
	`, playerID.String())
	if err != nil {
		return oops.Code("SESSION_DELETE_BY_PLAYER_FAILED").
			With("operation", "delete web_sessions by player").
			With("player_id", playerID.String()).
			Wrap(err)
	}
	// Note: No ErrNotFound if no rows deleted - that's a valid state
	return nil
}

// DeleteExpired removes all expired sessions and returns the count.
func (r *WebSessionRepository) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM web_sessions WHERE expires_at < $1
	`, time.Now())
	if err != nil {
		return 0, oops.Code("SESSION_DELETE_EXPIRED_FAILED").
			With("operation", "delete expired web_sessions").
			Wrap(err)
	}
	return result.RowsAffected(), nil
}

// scanSession scans a single row into a WebSession.
// Callers are responsible for handling pgx.ErrNoRows.
func (r *WebSessionRepository) scanSession(row pgx.Row) (*auth.WebSession, error) {
	var (
		idStr       string
		playerIDStr string
		charIDStr   *string
		tokenHash   string
		userAgent   string
		ipAddress   string
		expiresAt   time.Time
		createdAt   time.Time
		lastSeenAt  time.Time
	)

	err := row.Scan(&idStr, &playerIDStr, &charIDStr, &tokenHash, &userAgent, &ipAddress, &expiresAt, &createdAt, &lastSeenAt)
	if err != nil {
		// Propagate pgx.ErrNoRows unchanged for callers to handle with context.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err //nolint:wrapcheck // Callers wrap with context-specific info
		}
		return nil, oops.Code("SESSION_SCAN_FAILED").
			With("operation", "scan web_session").
			Wrap(err)
	}

	return r.buildSession(idStr, playerIDStr, charIDStr, tokenHash, userAgent, ipAddress, expiresAt, createdAt, lastSeenAt)
}

// scanSessionRow scans a row from a rows iterator into a WebSession.
func (r *WebSessionRepository) scanSessionRow(rows pgx.Rows) (*auth.WebSession, error) {
	var (
		idStr       string
		playerIDStr string
		charIDStr   *string
		tokenHash   string
		userAgent   string
		ipAddress   string
		expiresAt   time.Time
		createdAt   time.Time
		lastSeenAt  time.Time
	)

	err := rows.Scan(&idStr, &playerIDStr, &charIDStr, &tokenHash, &userAgent, &ipAddress, &expiresAt, &createdAt, &lastSeenAt)
	if err != nil {
		return nil, oops.Code("SESSION_SCAN_FAILED").
			With("operation", "scan web_session row").
			Wrap(err)
	}

	return r.buildSession(idStr, playerIDStr, charIDStr, tokenHash, userAgent, ipAddress, expiresAt, createdAt, lastSeenAt)
}

// buildSession constructs a WebSession from scanned values.
func (r *WebSessionRepository) buildSession(
	idStr, playerIDStr string,
	charIDStr *string,
	tokenHash, userAgent, ipAddress string,
	expiresAt, createdAt, lastSeenAt time.Time,
) (*auth.WebSession, error) {
	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, oops.Code("SESSION_INVALID_ID").
			With("operation", "parse session id").
			With("id", idStr).
			Wrap(err)
	}

	playerID, err := ulid.Parse(playerIDStr)
	if err != nil {
		return nil, oops.Code("SESSION_INVALID_PLAYER_ID").
			With("operation", "parse player id").
			With("player_id", playerIDStr).
			Wrap(err)
	}

	var characterID *ulid.ULID
	if charIDStr != nil {
		parsed, err := ulid.Parse(*charIDStr)
		if err != nil {
			return nil, oops.Code("SESSION_INVALID_CHARACTER_ID").
				With("operation", "parse character id").
				With("character_id", *charIDStr).
				Wrap(err)
		}
		characterID = &parsed
	}

	return &auth.WebSession{
		ID:          id,
		PlayerID:    playerID,
		CharacterID: characterID,
		TokenHash:   tokenHash,
		UserAgent:   userAgent,
		IPAddress:   ipAddress,
		ExpiresAt:   expiresAt,
		CreatedAt:   createdAt,
		LastSeenAt:  lastSeenAt,
	}, nil
}

// Compile-time interface check.
var _ auth.WebSessionRepository = (*WebSessionRepository)(nil)
