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
)

func TestPlayerRepository_Create(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("creates new player", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "create_test_user",
			PasswordHash: "hash123",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}

		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		// Verify it was stored
		stored, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		assert.Equal(t, player.ID, stored.ID)
		assert.Equal(t, player.Username, stored.Username)
		assert.Equal(t, player.PasswordHash, stored.PasswordHash)
	})

	t.Run("creates player with email", func(t *testing.T) {
		email := "test@example.com"
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "create_email_user",
			PasswordHash: "hash123",
			Email:        &email,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}

		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		stored, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		require.NotNil(t, stored.Email)
		assert.Equal(t, email, *stored.Email)
	})

	t.Run("creates player with preferences", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "create_prefs_user",
			PasswordHash: "hash123",
			Preferences: auth.PlayerPreferences{
				AutoLogin:     true,
				MaxCharacters: 10,
				Theme:         "dark",
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}

		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		stored, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		assert.True(t, stored.Preferences.AutoLogin)
		assert.Equal(t, 10, stored.Preferences.MaxCharacters)
		assert.Equal(t, "dark", stored.Preferences.Theme)
	})

	t.Run("fails on duplicate username", func(t *testing.T) {
		player1 := &auth.Player{
			ID:           ulid.Make(),
			Username:     "duplicate_user",
			PasswordHash: "hash123",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player1)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE username = $1`, "duplicate_user")
		})

		player2 := &auth.Player{
			ID:           ulid.Make(),
			Username:     "duplicate_user",
			PasswordHash: "hash456",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err = repo.Create(ctx, player2)
		assert.Error(t, err)
	})

	t.Run("fails on duplicate email", func(t *testing.T) {
		email := "duplicate@example.com"
		player1 := &auth.Player{
			ID:           ulid.Make(),
			Username:     "dup_email_user1",
			PasswordHash: "hash123",
			Email:        &email,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player1)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE email = $1`, email)
		})

		player2 := &auth.Player{
			ID:           ulid.Make(),
			Username:     "dup_email_user2",
			PasswordHash: "hash456",
			Email:        &email,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err = repo.Create(ctx, player2)
		assert.Error(t, err)
	})
}

func TestPlayerRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("returns player by ID", func(t *testing.T) {
		player := &auth.Player{
			ID:            ulid.Make(),
			Username:      "getbyid_user",
			PasswordHash:  "hash123",
			EmailVerified: true,
			CreatedAt:     time.Now().UTC(),
			UpdatedAt:     time.Now().UTC(),
		}
		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		result, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		assert.Equal(t, player.ID, result.ID)
		assert.Equal(t, player.Username, result.Username)
		assert.Equal(t, player.PasswordHash, result.PasswordHash)
		assert.True(t, result.EmailVerified)
	})

	t.Run("returns ErrNotFound for non-existent ID", func(t *testing.T) {
		nonExistentID := ulid.Make()
		result, err := repo.GetByID(ctx, nonExistentID)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPlayerRepository_GetByUsername(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("returns player by username", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "getbyusername_user",
			PasswordHash: "hash123",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		result, err := repo.GetByUsername(ctx, "getbyusername_user")
		require.NoError(t, err)
		assert.Equal(t, player.ID, result.ID)
		assert.Equal(t, player.Username, result.Username)
	})

	t.Run("case-insensitive username lookup", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "CaseSensitiveUser",
			PasswordHash: "hash123",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		// Should find with different case
		result, err := repo.GetByUsername(ctx, "casesensitiveuser")
		require.NoError(t, err)
		assert.Equal(t, player.ID, result.ID)

		result, err = repo.GetByUsername(ctx, "CASESENSITIVEUSER")
		require.NoError(t, err)
		assert.Equal(t, player.ID, result.ID)
	})

	t.Run("returns ErrNotFound for non-existent username", func(t *testing.T) {
		result, err := repo.GetByUsername(ctx, "nonexistent_user")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPlayerRepository_GetByEmail(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("returns player by email", func(t *testing.T) {
		email := "getbyemail@example.com"
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "getbyemail_user",
			PasswordHash: "hash123",
			Email:        &email,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		result, err := repo.GetByEmail(ctx, email)
		require.NoError(t, err)
		assert.Equal(t, player.ID, result.ID)
		require.NotNil(t, result.Email)
		assert.Equal(t, email, *result.Email)
	})

	t.Run("case-insensitive email lookup", func(t *testing.T) {
		email := "CaseEmail@Example.COM"
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "caseemail_user",
			PasswordHash: "hash123",
			Email:        &email,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		// Should find with different case
		result, err := repo.GetByEmail(ctx, "caseemail@example.com")
		require.NoError(t, err)
		assert.Equal(t, player.ID, result.ID)

		result, err = repo.GetByEmail(ctx, "CASEEMAIL@EXAMPLE.COM")
		require.NoError(t, err)
		assert.Equal(t, player.ID, result.ID)
	})

	t.Run("returns ErrNotFound for non-existent email", func(t *testing.T) {
		result, err := repo.GetByEmail(ctx, "nonexistent@example.com")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPlayerRepository_Update(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("updates player fields", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "update_user",
			PasswordHash: "hash123",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		// Update fields
		email := "updated@example.com"
		player.Email = &email
		player.EmailVerified = true
		player.FailedAttempts = 3
		lockTime := time.Now().Add(time.Hour).UTC()
		player.LockedUntil = &lockTime
		player.Preferences = auth.PlayerPreferences{
			AutoLogin:     true,
			MaxCharacters: 15,
			Theme:         "light",
		}
		player.UpdatedAt = time.Now().UTC()

		err = repo.Update(ctx, player)
		require.NoError(t, err)

		// Verify updates
		result, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		require.NotNil(t, result.Email)
		assert.Equal(t, email, *result.Email)
		assert.True(t, result.EmailVerified)
		assert.Equal(t, 3, result.FailedAttempts)
		require.NotNil(t, result.LockedUntil)
		assert.True(t, lockTime.Equal(*result.LockedUntil))
		assert.True(t, result.Preferences.AutoLogin)
		assert.Equal(t, 15, result.Preferences.MaxCharacters)
		assert.Equal(t, "light", result.Preferences.Theme)
	})

	t.Run("returns ErrNotFound for non-existent player", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "nonexistent_update",
			PasswordHash: "hash123",
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Update(ctx, player)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPlayerRepository_UpdatePassword(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("updates password hash only", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "updatepw_user",
			PasswordHash: "original_hash",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		err = repo.UpdatePassword(ctx, player.ID, "new_hash")
		require.NoError(t, err)

		// Verify password was updated
		result, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		assert.Equal(t, "new_hash", result.PasswordHash)
		// Other fields unchanged
		assert.Equal(t, player.Username, result.Username)
	})

	t.Run("returns ErrNotFound for non-existent player", func(t *testing.T) {
		nonExistentID := ulid.Make()
		err := repo.UpdatePassword(ctx, nonExistentID, "new_hash")
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPlayerRepository_Delete(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("deletes existing player", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "delete_user",
			PasswordHash: "hash123",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		err := repo.Create(ctx, player)
		require.NoError(t, err)

		err = repo.Delete(ctx, player.ID)
		require.NoError(t, err)

		// Verify it's gone
		result, err := repo.GetByID(ctx, player.ID)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})

	t.Run("returns ErrNotFound for non-existent ID", func(t *testing.T) {
		nonExistentID := ulid.Make()
		err := repo.Delete(ctx, nonExistentID)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestPlayerRepository_LockedUntil(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("stores and retrieves locked_until correctly", func(t *testing.T) {
		lockTime := time.Now().Add(time.Hour).UTC()
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "locked_user",
			PasswordHash: "hash123",
			LockedUntil:  &lockTime,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}

		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		result, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		require.NotNil(t, result.LockedUntil)
		assert.True(t, lockTime.Equal(*result.LockedUntil))
	})

	t.Run("handles nil locked_until", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "unlocked_user",
			PasswordHash: "hash123",
			LockedUntil:  nil,
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}

		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		result, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		assert.Nil(t, result.LockedUntil)
	})
}

func TestPlayerRepository_DefaultCharacterID(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("handles nil default_character_id", func(t *testing.T) {
		player := &auth.Player{
			ID:                 ulid.Make(),
			Username:           "no_default_char",
			PasswordHash:       "hash123",
			DefaultCharacterID: nil,
			CreatedAt:          time.Now().UTC(),
			UpdatedAt:          time.Now().UTC(),
		}

		err := repo.Create(ctx, player)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		result, err := repo.GetByID(ctx, player.ID)
		require.NoError(t, err)
		assert.Nil(t, result.DefaultCharacterID)
	})
}

// TestPlayerRepository_DeleteGuestPlayer_CascadesBindings is the regression
// lock for holomush-21bd9: before migration 000040 the
// player_character_bindings.player_id FK had no ON DELETE CASCADE, so reaping a
// guest that had a character binding failed with SQLSTATE 23503 and the
// GuestReaper logged a WARN every interval forever. This reproduces that exact
// shape (guest player + character + active binding) and asserts the delete now
// succeeds and cascades.
func TestPlayerRepository_DeleteGuestPlayer_CascadesBindings(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	guestID := ulid.Make()
	charID := ulid.Make()
	bindingID := ulid.Make()

	_, err := testPool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash, is_guest) VALUES ($1, $2, '', true)`,
		guestID.String(), "guest_cascade_"+guestID.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, guestID.String())
	})

	_, err = testPool.Exec(ctx,
		`INSERT INTO characters (id, player_id, name) VALUES ($1, $2, $3)`,
		charID.String(), guestID.String(), "Guest Char")
	require.NoError(t, err)

	_, err = testPool.Exec(ctx,
		`INSERT INTO player_character_bindings (id, player_id, character_id) VALUES ($1, $2, $3)`,
		bindingID.String(), guestID.String(), charID.String())
	require.NoError(t, err)

	err = repo.DeleteGuestPlayer(ctx, guestID)
	require.NoError(t, err, "deleting a guest with a character binding must not violate the FK")

	var bindings int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT count(*) FROM player_character_bindings WHERE player_id = $1`, guestID.String()).Scan(&bindings))
	assert.Zero(t, bindings, "binding should cascade-delete with the guest player")

	var chars int
	require.NoError(t, testPool.QueryRow(ctx,
		`SELECT count(*) FROM characters WHERE id = $1`, charID.String()).Scan(&chars))
	assert.Zero(t, chars, "character should cascade-delete with the guest player")
}

// MarkReaping sets players.reaping_at for a guest player and is a no-op
// (GUEST_NOT_FOUND) for a non-guest, so the anti-TOCTOU flag is only ever set on
// reapable guests (round-6 R6-2).
func TestPlayerRepository_MarkReaping(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	t.Run("marks a guest player reaping", func(t *testing.T) {
		guestID := ulid.Make()
		_, err := testPool.Exec(ctx,
			`INSERT INTO players (id, username, password_hash, is_guest) VALUES ($1, $2, '', true)`,
			guestID.String(), "guest_mark_"+guestID.String())
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, guestID.String())
		})

		require.NoError(t, repo.MarkReaping(ctx, guestID))

		var reapingAt *int64
		require.NoError(t, testPool.QueryRow(ctx,
			`SELECT reaping_at FROM players WHERE id = $1`, guestID.String()).Scan(&reapingAt))
		require.NotNil(t, reapingAt, "reaping_at should be set")
		assert.Positive(t, *reapingAt)
	})

	t.Run("is GUEST_NOT_FOUND for a non-guest player", func(t *testing.T) {
		player := &auth.Player{
			ID:           ulid.Make(),
			Username:     "nonguest_mark_" + ulid.Make().String(),
			PasswordHash: "hash",
			CreatedAt:    time.Now().UTC(),
			UpdatedAt:    time.Now().UTC(),
		}
		require.NoError(t, repo.Create(ctx, player))
		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, player.ID.String())
		})

		err := repo.MarkReaping(ctx, player.ID)
		require.Error(t, err)

		var reapingAt *int64
		require.NoError(t, testPool.QueryRow(ctx,
			`SELECT reaping_at FROM players WHERE id = $1`, player.ID.String()).Scan(&reapingAt))
		assert.Nil(t, reapingAt, "non-guest player must not be marked reaping")
	})
}

// Compile-time interface check.
var _ auth.PlayerRepository = (*postgres.PlayerRepository)(nil)

func TestPlayerRepository_ExistingIDs(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	p1 := &auth.Player{
		ID:           ulid.Make(),
		Username:     "existing_ids_user_1",
		PasswordHash: "hash",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	p2 := &auth.Player{
		ID:           ulid.Make(),
		Username:     "existing_ids_user_2",
		PasswordHash: "hash",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	require.NoError(t, repo.Create(ctx, p1))
	require.NoError(t, repo.Create(ctx, p2))
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = ANY($1::text[])`,
			[]string{p1.ID.String(), p2.ID.String()})
	})

	nonexistent := ulid.Make().String()

	found, err := repo.ExistingIDs(ctx, []string{
		p1.ID.String(),
		nonexistent,
		p2.ID.String(),
	})
	require.NoError(t, err)
	assert.ElementsMatch(t,
		[]string{p1.ID.String(), p2.ID.String()},
		found,
		"should return only the IDs that exist in the players table")
}

func TestPlayerRepository_ExistingIDs_EmptyInput(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewPlayerRepository(testPool)

	found, err := repo.ExistingIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, found, "nil input should return empty slice without querying")

	found, err = repo.ExistingIDs(ctx, []string{})
	require.NoError(t, err)
	assert.Empty(t, found, "empty input should return empty slice without querying")
}
