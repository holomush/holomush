// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package postgres provides PostgreSQL implementations of auth repositories.
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

// PasswordResetRepository implements auth.PasswordResetRepository using PostgreSQL.
type PasswordResetRepository struct {
	pool *pgxpool.Pool
}

// NewPasswordResetRepository creates a new PasswordResetRepository.
func NewPasswordResetRepository(pool *pgxpool.Pool) *PasswordResetRepository {
	return &PasswordResetRepository{pool: pool}
}

// Create stores a new password reset request.
func (r *PasswordResetRepository) Create(ctx context.Context, reset *auth.PasswordReset) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO password_resets (id, player_id, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, reset.ID.String(), reset.PlayerID.String(), reset.TokenHash, reset.ExpiresAt, reset.CreatedAt)
	if err != nil {
		return oops.Code("RESET_CREATE_FAILED").
			With("operation", "insert password_reset").
			With("player_id", reset.PlayerID.String()).
			Wrap(err)
	}
	return nil
}

// GetByPlayer retrieves the most recent reset request for a player.
func (r *PasswordResetRepository) GetByPlayer(ctx context.Context, playerID ulid.ULID) (*auth.PasswordReset, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, player_id, token_hash, expires_at, created_at
		FROM password_resets
		WHERE player_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, playerID.String())

	reset, err := r.scanReset(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("RESET_NOT_FOUND").
			With("player_id", playerID.String()).
			Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return reset, nil
}

// GetByTokenHash retrieves a reset request by its token hash.
func (r *PasswordResetRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*auth.PasswordReset, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, player_id, token_hash, expires_at, created_at
		FROM password_resets
		WHERE token_hash = $1
	`, tokenHash)

	reset, err := r.scanReset(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, oops.Code("RESET_NOT_FOUND").Wrap(auth.ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return reset, nil
}

// Delete removes a password reset request.
func (r *PasswordResetRepository) Delete(ctx context.Context, id ulid.ULID) error {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM password_resets WHERE id = $1
	`, id.String())
	if err != nil {
		return oops.Code("RESET_DELETE_FAILED").
			With("operation", "delete password_reset").
			With("id", id.String()).
			Wrap(err)
	}
	if result.RowsAffected() == 0 {
		return oops.Code("RESET_NOT_FOUND").
			With("id", id.String()).
			Wrap(auth.ErrNotFound)
	}
	return nil
}

// DeleteByPlayer removes all reset requests for a player.
func (r *PasswordResetRepository) DeleteByPlayer(ctx context.Context, playerID ulid.ULID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM password_resets WHERE player_id = $1
	`, playerID.String())
	if err != nil {
		return oops.Code("RESET_DELETE_BY_PLAYER_FAILED").
			With("operation", "delete password_resets by player").
			With("player_id", playerID.String()).
			Wrap(err)
	}
	// Note: No ErrNotFound if no rows deleted - that's a valid state
	return nil
}

// DeleteExpired removes all expired reset requests and returns the count.
func (r *PasswordResetRepository) DeleteExpired(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `
		DELETE FROM password_resets WHERE expires_at < $1
	`, time.Now())
	if err != nil {
		return 0, oops.Code("RESET_DELETE_EXPIRED_FAILED").
			With("operation", "delete expired password_resets").
			Wrap(err)
	}
	return result.RowsAffected(), nil
}

// scanReset scans a single row into a PasswordReset.
// Callers are responsible for handling pgx.ErrNoRows.
func (r *PasswordResetRepository) scanReset(row pgx.Row) (*auth.PasswordReset, error) {
	var (
		idStr       string
		playerIDStr string
		tokenHash   string
		expiresAt   time.Time
		createdAt   time.Time
	)

	err := row.Scan(&idStr, &playerIDStr, &tokenHash, &expiresAt, &createdAt)
	if err != nil {
		// Propagate pgx.ErrNoRows unchanged for callers to handle with context.
		// This matches the established pattern in internal/world/postgres/*_repo.go.
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, err //nolint:wrapcheck // Callers wrap with context-specific info
		}
		return nil, oops.Code("RESET_SCAN_FAILED").
			With("operation", "scan password_reset").
			Wrap(err)
	}

	id, err := ulid.Parse(idStr)
	if err != nil {
		return nil, oops.Code("RESET_INVALID_ID").
			With("operation", "parse reset id").
			With("id", idStr).
			Wrap(err)
	}

	playerID, err := ulid.Parse(playerIDStr)
	if err != nil {
		return nil, oops.Code("RESET_INVALID_PLAYER_ID").
			With("operation", "parse player id").
			With("player_id", playerIDStr).
			Wrap(err)
	}

	return &auth.PasswordReset{
		ID:        id,
		PlayerID:  playerID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
		CreatedAt: createdAt,
	}, nil
}

// Compile-time interface check.
var _ auth.PasswordResetRepository = (*PasswordResetRepository)(nil)
