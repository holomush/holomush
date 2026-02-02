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

// createTestPlayer creates a player in the database for testing password resets.
func createTestPlayer(ctx context.Context, t *testing.T, username string) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at)
		VALUES ($1, $2, 'testhash', NOW())
	`, playerID.String(), username)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	return playerID
}

func TestPasswordResetRepository_Create(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPasswordResetRepository(testPool)
	playerID := createTestPlayer(ctx, t, "reset_create_test")

	t.Run("creates new reset", func(t *testing.T) {
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "testhash123",
			ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
			CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, reset)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM password_resets WHERE id = $1`, reset.ID.String())
		})

		// Verify it was stored
		stored, err := repo.GetByTokenHash(ctx, reset.TokenHash)
		require.NoError(t, err)
		assert.Equal(t, reset.ID, stored.ID)
		assert.Equal(t, reset.PlayerID, stored.PlayerID)
		assert.Equal(t, reset.TokenHash, stored.TokenHash)
	})

	t.Run("fails on duplicate token_hash", func(t *testing.T) {
		// This test simulates the rare birthday problem scenario where token generation
		// produces a hash collision. The database unique constraint should reject it.
		reset1 := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "duplicate_hash",
			ExpiresAt: time.Now().Add(time.Hour).UTC(),
			CreatedAt: time.Now().UTC(),
		}
		err := repo.Create(ctx, reset1)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM password_resets WHERE token_hash = $1`, "duplicate_hash")
		})

		reset2 := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "duplicate_hash",
			ExpiresAt: time.Now().Add(time.Hour).UTC(),
			CreatedAt: time.Now().UTC(),
		}
		err = repo.Create(ctx, reset2)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "RESET_CREATE_FAILED")
	})
}

func TestPasswordResetRepository_GetByPlayer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPasswordResetRepository(testPool)
	playerID := createTestPlayer(ctx, t, "reset_getbyplayer_test")

	t.Run("returns most recent reset", func(t *testing.T) {
		// Create two resets with different timestamps
		older := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "older_hash",
			ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
			CreatedAt: time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond),
		}
		err := repo.Create(ctx, older)
		require.NoError(t, err)

		newer := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "newer_hash",
			ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
			CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		err = repo.Create(ctx, newer)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM password_resets WHERE player_id = $1`, playerID.String())
		})

		// Should return the newer one
		result, err := repo.GetByPlayer(ctx, playerID)
		require.NoError(t, err)
		assert.Equal(t, newer.ID, result.ID)
		assert.Equal(t, "newer_hash", result.TokenHash)
	})

	t.Run("returns ErrNotFound for non-existent player", func(t *testing.T) {
		nonExistentID := ulid.Make()
		result, err := repo.GetByPlayer(ctx, nonExistentID)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPasswordResetRepository_GetByTokenHash(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPasswordResetRepository(testPool)
	playerID := createTestPlayer(ctx, t, "reset_getbyhash_test")

	t.Run("returns reset by hash", func(t *testing.T) {
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "unique_test_hash",
			ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
			CreatedAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		err := repo.Create(ctx, reset)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM password_resets WHERE id = $1`, reset.ID.String())
		})

		result, err := repo.GetByTokenHash(ctx, "unique_test_hash")
		require.NoError(t, err)
		assert.Equal(t, reset.ID, result.ID)
		assert.Equal(t, reset.PlayerID, result.PlayerID)
	})

	t.Run("returns ErrNotFound for non-existent hash", func(t *testing.T) {
		result, err := repo.GetByTokenHash(ctx, "nonexistent_hash")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPasswordResetRepository_Delete(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPasswordResetRepository(testPool)
	playerID := createTestPlayer(ctx, t, "reset_delete_test")

	t.Run("deletes existing reset", func(t *testing.T) {
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "delete_test_hash",
			ExpiresAt: time.Now().Add(time.Hour).UTC(),
			CreatedAt: time.Now().UTC(),
		}
		err := repo.Create(ctx, reset)
		require.NoError(t, err)

		err = repo.Delete(ctx, reset.ID)
		require.NoError(t, err)

		// Verify it's gone
		result, err := repo.GetByTokenHash(ctx, "delete_test_hash")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})

	t.Run("returns ErrNotFound for non-existent ID", func(t *testing.T) {
		nonExistentID := ulid.Make()
		err := repo.Delete(ctx, nonExistentID)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPasswordResetRepository_DeleteByPlayer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPasswordResetRepository(testPool)
	playerID := createTestPlayer(ctx, t, "reset_deletebyplayer_test")

	t.Run("deletes all resets for player", func(t *testing.T) {
		// Create multiple resets
		for i := 0; i < 3; i++ {
			reset := &auth.PasswordReset{
				ID:        ulid.Make(),
				PlayerID:  playerID,
				TokenHash: "deletebyplayer_hash_" + ulid.Make().String(),
				ExpiresAt: time.Now().Add(time.Hour).UTC(),
				CreatedAt: time.Now().UTC(),
			}
			err := repo.Create(ctx, reset)
			require.NoError(t, err)
		}

		err := repo.DeleteByPlayer(ctx, playerID)
		require.NoError(t, err)

		// Verify all are gone
		result, err := repo.GetByPlayer(ctx, playerID)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})

	t.Run("succeeds even when no resets exist", func(t *testing.T) {
		nonExistentPlayerID := ulid.Make()
		err := repo.DeleteByPlayer(ctx, nonExistentPlayerID)
		// Should not error - valid state to have no resets
		assert.NoError(t, err)
	})
}

func TestPasswordResetRepository_DeleteExpired(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPasswordResetRepository(testPool)
	playerID := createTestPlayer(ctx, t, "reset_deleteexpired_test")

	t.Run("deletes expired resets and returns count", func(t *testing.T) {
		// Create an expired reset
		expired := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "expired_hash_" + ulid.Make().String(),
			ExpiresAt: time.Now().Add(-time.Hour).UTC(), // Already expired
			CreatedAt: time.Now().Add(-2 * time.Hour).UTC(),
		}
		err := repo.Create(ctx, expired)
		require.NoError(t, err)

		// Create a valid (non-expired) reset
		valid := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "valid_hash_" + ulid.Make().String(),
			ExpiresAt: time.Now().Add(time.Hour).UTC(), // Not expired
			CreatedAt: time.Now().UTC(),
		}
		err = repo.Create(ctx, valid)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM password_resets WHERE player_id = $1`, playerID.String())
		})

		// Delete expired
		count, err := repo.DeleteExpired(ctx)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(1))

		// Verify expired is gone
		result, err := repo.GetByTokenHash(ctx, expired.TokenHash)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)

		// Verify valid still exists
		result, err = repo.GetByTokenHash(ctx, valid.TokenHash)
		require.NoError(t, err)
		assert.Equal(t, valid.ID, result.ID)
	})

	t.Run("returns zero when no expired resets", func(t *testing.T) {
		// Clean up any existing expired resets first
		_, _ = testPool.Exec(ctx, `DELETE FROM password_resets WHERE expires_at < NOW()`)

		count, err := repo.DeleteExpired(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	})
}

// Compile-time interface check (test ensures the repository implements the interface).
var _ auth.PasswordResetRepository = (*postgres.PasswordResetRepository)(nil)
