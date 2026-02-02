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

	_, err = r.pool.Exec(ctx, `
		INSERT INTO players (
			id, username, password_hash, email, email_verified,
			failed_attempts, locked_until, default_character_id,
			preferences, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`,
		player.ID.String(),
		player.Username,
		player.PasswordHash,
		player.Email,
		player.EmailVerified,
		player.FailedAttempts,
		player.LockedUntil,
		defaultCharID,
		prefsJSON,
		player.CreatedAt,
		player.UpdatedAt,
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
		       preferences, created_at, updated_at
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
		       preferences, created_at, updated_at
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
		       preferences, created_at, updated_at
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

	result, err := r.pool.Exec(ctx, `
		UPDATE players SET
			username = $2,
			password_hash = $3,
			email = $4,
			email_verified = $5,
			failed_attempts = $6,
			locked_until = $7,
			default_character_id = $8,
			preferences = $9,
			updated_at = $10
		WHERE id = $1
	`,
		player.ID.String(),
		player.Username,
		player.PasswordHash,
		player.Email,
		player.EmailVerified,
		player.FailedAttempts,
		player.LockedUntil,
		defaultCharID,
		prefsJSON,
		player.UpdatedAt,
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
	`, id.String(), passwordHash, time.Now())
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
		lockedUntil      *time.Time
		defaultCharIDStr *string
		prefsJSON        []byte
		createdAt        time.Time
		updatedAt        time.Time
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

	return &auth.Player{
		ID:                 id,
		Username:           username,
		PasswordHash:       passwordHash,
		Email:              email,
		EmailVerified:      emailVerified,
		FailedAttempts:     failedAttempts,
		LockedUntil:        lockedUntil,
		DefaultCharacterID: defaultCharID,
		Preferences:        prefs,
		CreatedAt:          createdAt,
		UpdatedAt:          updatedAt,
	}, nil
}

// Compile-time interface check.
var _ auth.PlayerRepository = (*PlayerRepository)(nil)
