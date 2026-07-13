// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/pgnanos"
)

// PlayerRepository implements auth.PlayerRepository using PostgreSQL.
type PlayerRepository struct {
	pool *pgxpool.Pool
}

// NewPlayerRepository creates a new PlayerRepository.
func NewPlayerRepository(pool *pgxpool.Pool) *PlayerRepository {
	return &PlayerRepository{pool: pool}
}

// Create stores a new player.
func (r *PlayerRepository) Create(ctx context.Context, player *auth.Player) error {
	prefsJSON, err := json.Marshal(player.Preferences)
	if err != nil {
		return oops.Code("PLAYER_CREATE_FAILED").
			With("operation", "marshal preferences").
			Wrap(err)
	}

	var defaultCharID *string
	if player.DefaultCharacterID != nil {
		s := player.DefaultCharacterID.String()
		defaultCharID = &s
	}

	var lockedUntilArg *pgnanos.Time
	if player.LockedUntil != nil {
		t := pgnanos.From(*player.LockedUntil)
		lockedUntilArg = &t
	}

	_, err = r.pool.Exec(
		ctx, `
		INSERT INTO players (
			id, username, password_hash, email, email_verified,
			failed_attempts, locked_until, default_character_id,
			preferences, is_guest, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		player.ID.String(),
		player.Username,
		player.PasswordHash,
		player.Email,
		player.EmailVerified,
		player.FailedAttempts,
		lockedUntilArg,
		defaultCharID,
		prefsJSON,
		player.IsGuest,
		pgnanos.From(player.CreatedAt),
		pgnanos.From(player.UpdatedAt),
	)
	if err != nil {
		return oops.Code("PLAYER_CREATE_FAILED").
			With("operation", "insert player").
			With("username", player.Username).
			Wrap(err)
	}
	return nil
}

// GetByID retrieves a player by ID.
func (r *PlayerRepository) GetByID(ctx context.Context, id ulid.ULID) (*auth.Player, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, email, email_verified,
		       failed_attempts, locked_until, default_character_id,
		       preferences, is_guest, created_at, updated_at
		FROM players
		WHERE id = $1
	`, id.String())

	player, err := r.scanPlayer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("PLAYER_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("PLAYER_GET_BY_ID_FAILED").
			With("operation", "get player by id").
			With("id", id.String()).
			Wrap(err)
	}
	return player, nil
}

// GetByUsername retrieves a player by username (case-insensitive).
func (r *PlayerRepository) GetByUsername(ctx context.Context, username string) (*auth.Player, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, email, email_verified,
		       failed_attempts, locked_until, default_character_id,
		       preferences, is_guest, created_at, updated_at
		FROM players
		WHERE LOWER(username) = LOWER($1)
	`, username)

	player, err := r.scanPlayer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("PLAYER_NOT_FOUND").
			With("username", username).
			Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("PLAYER_GET_BY_USERNAME_FAILED").
			With("operation", "get player by username").
			With("username", username).
			Wrap(err)
	}
	return player, nil
}

// GetByEmail retrieves a player by email (case-insensitive).
func (r *PlayerRepository) GetByEmail(ctx context.Context, email string) (*auth.Player, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, username, password_hash, email, email_verified,
		       failed_attempts, locked_until, default_character_id,
		       preferences, is_guest, created_at, updated_at
		FROM players
		WHERE LOWER(email) = LOWER($1)
	`, email)

	player, err := r.scanPlayer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("PLAYER_NOT_FOUND").
			With("email", email).
			Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, oops.Code("PLAYER_GET_BY_EMAIL_FAILED").
			With("operation", "get player by email").
			With("email", email).
			Wrap(err)
	}
	return player, nil
}

// Count returns the total number of players.
func (r *PlayerRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM players`).Scan(&count)
	if err != nil {
		return 0, oops.With("operation", "count players").Wrap(err)
	}
	return count, nil
}

// Update updates an existing player.
func (r *PlayerRepository) Update(ctx context.Context, player *auth.Player) error {
	prefsJSON, err := json.Marshal(player.Preferences)
	if err != nil {
		return oops.Code("PLAYER_UPDATE_FAILED").
			With("operation", "marshal preferences").
			Wrap(err)
	}

	var defaultCharID *string
	if player.DefaultCharacterID != nil {
		s := player.DefaultCharacterID.String()
		defaultCharID = &s
	}

	var updateLockedUntilArg *pgnanos.Time
	if player.LockedUntil != nil {
		t := pgnanos.From(*player.LockedUntil)
		updateLockedUntilArg = &t
	}

	result, err := r.pool.Exec(
		ctx, `
		UPDATE players SET
			username = $2,
			password_hash = $3,
			email = $4,
			email_verified = $5,
			failed_attempts = $6,
			locked_until = $7,
			default_character_id = $8,
			preferences = $9,
			is_guest = $10,
			updated_at = $11
		WHERE id = $1
	`,
		player.ID.String(),
		player.Username,
		player.PasswordHash,
		player.Email,
		player.EmailVerified,
		player.FailedAttempts,
		updateLockedUntilArg,
		defaultCharID,
		prefsJSON,
		player.IsGuest,
		pgnanos.From(player.UpdatedAt),
	)
	if err != nil {
		return oops.Code("PLAYER_UPDATE_FAILED").
			With("operation", "update player").
			With("id", player.ID.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("PLAYER_NOT_FOUND").
			With("id", player.ID.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// UpdatePassword updates only the password hash for a player.
func (r *PlayerRepository) UpdatePassword(ctx context.Context, id ulid.ULID, passwordHash string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE players SET password_hash = $2, updated_at = $3
		WHERE id = $1
	`, id.String(), passwordHash, pgnanos.From(time.Now()))
	if err != nil {
		return oops.Code("PLAYER_UPDATE_PASSWORD_FAILED").
			With("operation", "update password").
			With("id", id.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("PLAYER_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// UpdatePasswordAndClearLockout atomically updates the password hash and
// clears lockout state (failed_attempts = 0, locked_until = NULL).
func (r *PlayerRepository) UpdatePasswordAndClearLockout(ctx context.Context, id ulid.ULID, passwordHash string) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE players SET password_hash = $2, failed_attempts = 0, locked_until = NULL, updated_at = $3
		WHERE id = $1
	`, id.String(), passwordHash, pgnanos.From(time.Now()))
	if err != nil {
		return oops.Code("PLAYER_UPDATE_PASSWORD_FAILED").
			With("operation", "update password and clear lockout").
			With("id", id.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("PLAYER_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// Delete removes a player.
func (r *PlayerRepository) Delete(ctx context.Context, id ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM players WHERE id = $1
	`, id.String())
	if err != nil {
		return oops.Code("PLAYER_DELETE_FAILED").
			With("operation", "delete player").
			With("id", id.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("PLAYER_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// ListIdleGuests returns guest players whose updated_at is before idleSince
// and who have no active or detached game sessions.
func (r *PlayerRepository) ListIdleGuests(ctx context.Context, idleSince time.Time) ([]*auth.Player, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT p.id, p.username, p.password_hash, p.email, p.email_verified,
		       p.failed_attempts, p.locked_until, p.default_character_id,
		       p.preferences, p.is_guest, p.created_at, p.updated_at
		FROM players p
		WHERE p.is_guest = true
		  AND p.updated_at < $1
		  AND NOT EXISTS (
		    SELECT 1 FROM characters c
		    JOIN sessions s ON s.character_id = c.id
		    WHERE c.player_id = p.id
		      AND s.status IN ('active', 'detached')
		  )
	`, pgnanos.From(idleSince))
	if err != nil {
		return nil, oops.Code("GUEST_LIST_FAILED").Wrap(err)
	}
	defer rows.Close()

	var players []*auth.Player
	for rows.Next() {
		p, err := r.scanPlayer(rows)
		if err != nil {
			return nil, err
		}
		players = append(players, p)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("GUEST_LIST_FAILED").
			With("operation", "iterate guest players").Wrap(err)
	}
	return players, nil
}

// DeleteGuestPlayer removes a guest player. The is_guest=true guard prevents
// accidental deletion of registered players. FK cascades delete characters,
// player sessions, and player_character_bindings (the bindings cascade was
// added in migration 000040; without it the reaper failed with a 23503 FK
// violation for any guest that had a character binding).
func (r *PlayerRepository) DeleteGuestPlayer(ctx context.Context, playerID ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM players WHERE id = $1 AND is_guest = true
	`, playerID.String())
	if err != nil {
		return oops.Code("GUEST_DELETE_FAILED").
			With("player_id", playerID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("GUEST_NOT_FOUND").
			With("player_id", playerID.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// MarkReaping sets players.reaping_at (epoch-ns) for a guest player, marking it
// as being reaped so the character-genesis service (05-15) rejects any new
// character creation for it (round-6 R6-2 anti-TOCTOU). The is_guest=true guard
// prevents accidentally marking a registered player. The UPDATE takes the
// players ROW LOCK, so an in-flight genesis holding SELECT reaping_at ... FOR
// UPDATE on the same row blocks this mark until the character commits (then the
// reaper's enumeration sees + tombstones it); a genesis starting after this mark
// commits observes reaping_at set and is rejected. Idempotent for the reaper: a
// re-mark simply overwrites the timestamp.
func (r *PlayerRepository) MarkReaping(ctx context.Context, playerID ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `
		UPDATE players SET reaping_at = $2 WHERE id = $1 AND is_guest = true
	`, playerID.String(), pgnanos.From(time.Now()))
	if err != nil {
		return oops.Code("GUEST_MARK_REAPING_FAILED").
			With("player_id", playerID.String()).Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("GUEST_NOT_FOUND").
			With("player_id", playerID.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// ExistingIDs returns the subset of the input ID strings that exist in
// the players table. Used by the crypto.operators startup cross-check
// (sub-epic B) to identify configured operator IDs that don't correspond
// to any player. Read-only; no schema mutation.
//
// Returns an empty slice for nil or empty input without issuing a query.
// Returns the IDs in arbitrary order (caller must not depend on input order).
func (r *PlayerRepository) ExistingIDs(ctx context.Context, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return []string{}, nil
	}

	rows, err := r.pool.Query(
		ctx,
		`SELECT id FROM players WHERE id = ANY($1::text[])`,
		ids,
	)
	if err != nil {
		return nil, oops.
			Code("PLAYER_REPO_EXISTING_IDS_FAILED").
			With("input_count", len(ids)).
			Wrap(err)
	}
	defer rows.Close()

	found := make([]string, 0, len(ids))
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, oops.Code("PLAYER_REPO_EXISTING_IDS_SCAN_FAILED").Wrap(err)
		}
		found = append(found, id)
	}
	if err := rows.Err(); err != nil {
		return nil, oops.Code("PLAYER_REPO_EXISTING_IDS_ROWS_FAILED").Wrap(err)
	}
	return found, nil
}

// scanPlayer scans a single row into a Player.
// Callers are responsible for handling pgx.ErrNoRows.
func (r *PlayerRepository) scanPlayer(row pgx.Row) (*auth.Player, error) {
	var (
		idStr            string
		username         string
		passwordHash     string
		email            *string
		emailVerified    bool
		failedAttempts   int
		lockedUntil      *pgnanos.Time
		defaultCharIDStr *string
		prefsJSON        []byte
		isGuest          bool
		createdAt        pgnanos.Time
		updatedAt        pgnanos.Time
	)

	err := row.Scan(
		&idStr,
		&username,
		&passwordHash,
		&email,
		&emailVerified,
		&failedAttempts,
		&lockedUntil,
		&defaultCharIDStr,
		&prefsJSON,
		&isGuest,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		// Propagate pgx.ErrNoRows unchanged for callers to handle with context.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err //nolint:wrapcheck // Callers wrap with context-specific info
		}
		return nil, oops.Code("PLAYER_SCAN_FAILED").
			With("operation", "scan player").
			Wrap(err)
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, oops.Code("PLAYER_INVALID_ID").
			With("operation", "parse player id").
			With("id", idStr).
			Wrap(err)
	}

	var defaultCharID *ulid.ULID
	if defaultCharIDStr != nil {
		parsed, err := ulid.Parse(*defaultCharIDStr)
		if err != nil {
			return nil, oops.Code("PLAYER_INVALID_DEFAULT_CHAR_ID").
				With("operation", "parse default character id").
				With("default_character_id", *defaultCharIDStr).
				Wrap(err)
		}
		defaultCharID = &parsed
	}

	var prefs auth.PlayerPreferences
	if len(prefsJSON) > 0 {
		if err := json.Unmarshal(prefsJSON, &prefs); err != nil {
			return nil, oops.Code("PLAYER_INVALID_PREFERENCES").
				With("operation", "unmarshal preferences").
				Wrap(err)
		}
	}

	var lockedUntilTime *time.Time
	if lockedUntil != nil {
		t := lockedUntil.Time()
		lockedUntilTime = &t
	}

	return &auth.Player{
		ID:                 id,
		Username:           username,
		PasswordHash:       passwordHash,
		Email:              email,
		EmailVerified:      emailVerified,
		FailedAttempts:     failedAttempts,
		LockedUntil:        lockedUntilTime,
		DefaultCharacterID: defaultCharID,
		Preferences:        prefs,
		IsGuest:            isGuest,
		CreatedAt:          createdAt.Time(),
		UpdatedAt:          updatedAt.Time(),
	}, nil
}

// Compile-time interface check.
var _ auth.PlayerRepository = (*PlayerRepository)(nil)
