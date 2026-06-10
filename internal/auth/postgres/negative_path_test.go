// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
	"github.com/holomush/holomush/internal/auth/postgres"
	"github.com/holomush/holomush/pkg/errutil"
)

// TestPlayerRepository_DuplicateUsername_ReturnsConstraintError verifies that
// inserting a second player with the same username (unique constraint) returns a
// PLAYER_CREATE_FAILED error, not a silent success or a different code.
func TestPlayerRepository_DuplicateUsername_ReturnsConstraintError(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	username := "neg_dup_username_" + ulid.Make().String()[:8]

	first := &auth.Player{
		ID:           ulid.Make(),
		Username:     username,
		PasswordHash: "hash1",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	require.NoError(t, repo.Create(ctx, first))
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE username = $1`, username)
	})

	second := &auth.Player{
		ID:           ulid.Make(), // distinct ID, same username
		Username:     username,
		PasswordHash: "hash2",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	err := repo.Create(ctx, second)
	require.Error(t, err, "second insert with duplicate username must fail")
	errutil.AssertErrorCode(t, err, "PLAYER_CREATE_FAILED")
}

// TestPlayerRepository_DuplicateEmail_ReturnsConstraintError verifies that
// inserting a second player with the same email (unique index) returns a
// PLAYER_CREATE_FAILED error.
func TestPlayerRepository_DuplicateEmail_ReturnsConstraintError(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	email := "neg_dup_" + ulid.Make().String()[:8] + "@example.com"

	first := &auth.Player{
		ID:           ulid.Make(),
		Username:     "neg_email_user1_" + ulid.Make().String()[:8],
		PasswordHash: "hash1",
		Email:        &email,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	require.NoError(t, repo.Create(ctx, first))
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE email = $1`, email)
	})

	second := &auth.Player{
		ID:           ulid.Make(), // distinct ID and username, same email
		Username:     "neg_email_user2_" + ulid.Make().String()[:8],
		PasswordHash: "hash2",
		Email:        &email,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	err := repo.Create(ctx, second)
	require.Error(t, err, "second insert with duplicate email must fail")
	errutil.AssertErrorCode(t, err, "PLAYER_CREATE_FAILED")
}

// TestPlayerRepository_DefaultCharacterID_FKViolation verifies that setting
// default_character_id to a non-existent character ID fails with a FK error
// (PostgreSQL 23503) wrapped as PLAYER_UPDATE_FAILED.
func TestPlayerRepository_DefaultCharacterID_FKViolation(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	player := &auth.Player{
		ID:           ulid.Make(),
		Username:     "neg_fk_defchar_" + ulid.Make().String()[:8],
		PasswordHash: "hash",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	require.NoError(t, repo.Create(ctx, player))
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
	})

	// Point default_character_id at a non-existent character.
	ghost := ulid.Make()
	player.DefaultCharacterID = &ghost
	player.UpdatedAt = time.Now().UTC()

	err := repo.Update(ctx, player)
	require.Error(t, err, "update with non-existent default_character_id must fail")
	errutil.AssertErrorCode(t, err, "PLAYER_UPDATE_FAILED")
}

// TestPlayerRepository_ConcurrentUpdateConflict verifies that a row-level lock
// held by an open transaction causes a concurrent Update to block until the lock
// is released, and that the update eventually succeeds once the lock is freed.
func TestPlayerRepository_ConcurrentUpdateConflict(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	player := &auth.Player{
		ID:           ulid.Make(),
		Username:     "neg_concurrent_" + ulid.Make().String()[:8],
		PasswordHash: "hash",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	require.NoError(t, repo.Create(ctx, player))
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
	})

	// Open a transaction and lock the row.
	tx, err := testPool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `SELECT id FROM players WHERE id = $1 FOR UPDATE`, player.ID.String())
	require.NoError(t, err)

	// A concurrent update with a very short timeout must block and time out.
	shortCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	player.PasswordHash = "new_hash"
	player.UpdatedAt = time.Now().UTC()
	err = repo.Update(shortCtx, player)
	require.Error(t, err, "update should time out while the row is locked")
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Release the lock.
	require.NoError(t, tx.Rollback(ctx))

	// The update must now succeed.
	player.UpdatedAt = time.Now().UTC()
	require.NoError(t, repo.Update(ctx, player))

	got, err := repo.GetByID(ctx, player.ID)
	require.NoError(t, err)
	assert.Equal(t, "new_hash", got.PasswordHash)
}

// TestPasswordResetRepository_FKViolation_NonExistentPlayer verifies that
// creating a password reset for a player that does not exist fails with
// RESET_CREATE_FAILED (FK violation, PostgreSQL 23503).
func TestPasswordResetRepository_FKViolation_NonExistentPlayer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPasswordResetRepository(testPool)

	reset := &auth.PasswordReset{
		ID:        ulid.Make(),
		PlayerID:  ulid.Make(), // no player with this ID exists
		TokenHash: "fk_ghost_player_" + ulid.Make().String(),
		ExpiresAt: time.Now().Add(time.Hour).UTC(),
		CreatedAt: time.Now().UTC(),
	}
	err := repo.Create(ctx, reset)
	require.Error(t, err, "creating a reset for a non-existent player must fail")
	errutil.AssertErrorCode(t, err, "RESET_CREATE_FAILED")
}
