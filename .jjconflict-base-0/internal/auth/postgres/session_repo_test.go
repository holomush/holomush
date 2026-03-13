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

// createTestPlayerForSession creates a player in the database for testing web sessions.
func createTestPlayerForSession(ctx context.Context, t *testing.T, username string) ulid.ULID {
	t.Helper()
	playerID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO players (id, username, password_hash, created_at, updated_at)
		VALUES ($1, $2, 'testhash', NOW(), NOW())
	`, playerID.String(), username)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM players WHERE id = $1`, playerID.String())
	})

	return playerID
}

// createTestCharacterForSession creates a character in the database for testing web sessions.
func createTestCharacterForSession(ctx context.Context, t *testing.T, playerID ulid.ULID, name string) ulid.ULID {
	t.Helper()
	characterID := ulid.Make()
	_, err := testPool.Exec(ctx, `
		INSERT INTO characters (id, player_id, name, created_at)
		VALUES ($1, $2, $3, NOW())
	`, characterID.String(), playerID.String(), name)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM characters WHERE id = $1`, characterID.String())
	})

	return characterID
}

func TestWebSessionRepository_Create(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_create_test")

	t.Run("creates new session without character", func(t *testing.T) {
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "test_token_hash_" + ulid.Make().String(),
			UserAgent:  "Mozilla/5.0 Test Browser",
			IPAddress:  "192.168.1.1",
			ExpiresAt:  time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
			CreatedAt:  time.Now().UTC().Truncate(time.Microsecond),
			LastSeenAt: time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, session)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE id = $1`, session.ID.String())
		})

		// Verify it was stored
		stored, err := repo.GetByID(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, session.ID, stored.ID)
		assert.Equal(t, session.PlayerID, stored.PlayerID)
		assert.Nil(t, stored.CharacterID)
		assert.Equal(t, session.TokenHash, stored.TokenHash)
		assert.Equal(t, session.UserAgent, stored.UserAgent)
		assert.Equal(t, session.IPAddress, stored.IPAddress)
	})

	t.Run("creates new session with character", func(t *testing.T) {
		characterID := createTestCharacterForSession(ctx, t, playerID, "session_char_test")
		session := &auth.WebSession{
			ID:          ulid.Make(),
			PlayerID:    playerID,
			CharacterID: &characterID,
			TokenHash:   "test_token_hash_with_char_" + ulid.Make().String(),
			UserAgent:   "Mozilla/5.0 Test Browser",
			IPAddress:   "192.168.1.2",
			ExpiresAt:   time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
			CreatedAt:   time.Now().UTC().Truncate(time.Microsecond),
			LastSeenAt:  time.Now().UTC().Truncate(time.Microsecond),
		}

		err := repo.Create(ctx, session)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE id = $1`, session.ID.String())
		})

		// Verify it was stored with character
		stored, err := repo.GetByID(ctx, session.ID)
		require.NoError(t, err)
		require.NotNil(t, stored.CharacterID)
		assert.Equal(t, characterID, *stored.CharacterID)
	})

	t.Run("fails on duplicate token_hash", func(t *testing.T) {
		tokenHash := "duplicate_session_hash_" + ulid.Make().String()

		session1 := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  tokenHash,
			ExpiresAt:  time.Now().Add(time.Hour).UTC(),
			CreatedAt:  time.Now().UTC(),
			LastSeenAt: time.Now().UTC(),
		}
		err := repo.Create(ctx, session1)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE token_hash = $1`, tokenHash)
		})

		session2 := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  tokenHash,
			ExpiresAt:  time.Now().Add(time.Hour).UTC(),
			CreatedAt:  time.Now().UTC(),
			LastSeenAt: time.Now().UTC(),
		}
		err = repo.Create(ctx, session2)
		assert.Error(t, err)
	})
}

func TestWebSessionRepository_GetByID(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_getbyid_test")

	t.Run("returns session by ID", func(t *testing.T) {
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "getbyid_hash_" + ulid.Make().String(),
			UserAgent:  "Test Agent",
			IPAddress:  "10.0.0.1",
			ExpiresAt:  time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
			CreatedAt:  time.Now().UTC().Truncate(time.Microsecond),
			LastSeenAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		err := repo.Create(ctx, session)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE id = $1`, session.ID.String())
		})

		result, err := repo.GetByID(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, session.ID, result.ID)
		assert.Equal(t, session.PlayerID, result.PlayerID)
		assert.Equal(t, session.TokenHash, result.TokenHash)
		assert.Equal(t, session.UserAgent, result.UserAgent)
		assert.Equal(t, session.IPAddress, result.IPAddress)
	})

	t.Run("returns ErrNotFound for non-existent ID", func(t *testing.T) {
		nonExistentID := ulid.Make()
		result, err := repo.GetByID(ctx, nonExistentID)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestWebSessionRepository_GetByTokenHash(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_getbyhash_test")

	t.Run("returns session by token hash", func(t *testing.T) {
		tokenHash := "unique_session_token_hash_" + ulid.Make().String()
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  tokenHash,
			ExpiresAt:  time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond),
			CreatedAt:  time.Now().UTC().Truncate(time.Microsecond),
			LastSeenAt: time.Now().UTC().Truncate(time.Microsecond),
		}
		err := repo.Create(ctx, session)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE id = $1`, session.ID.String())
		})

		result, err := repo.GetByTokenHash(ctx, tokenHash)
		require.NoError(t, err)
		assert.Equal(t, session.ID, result.ID)
		assert.Equal(t, session.PlayerID, result.PlayerID)
	})

	t.Run("returns ErrNotFound for non-existent hash", func(t *testing.T) {
		result, err := repo.GetByTokenHash(ctx, "nonexistent_session_hash")
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestWebSessionRepository_GetByPlayer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_getbyplayer_test")

	t.Run("returns all sessions for player", func(t *testing.T) {
		// Create multiple sessions
		var sessionIDs []ulid.ULID
		for i := 0; i < 3; i++ {
			session := &auth.WebSession{
				ID:         ulid.Make(),
				PlayerID:   playerID,
				TokenHash:  "getbyplayer_hash_" + ulid.Make().String(),
				ExpiresAt:  time.Now().Add(time.Hour).UTC(),
				CreatedAt:  time.Now().UTC(),
				LastSeenAt: time.Now().UTC(),
			}
			err := repo.Create(ctx, session)
			require.NoError(t, err)
			sessionIDs = append(sessionIDs, session.ID)
		}

		t.Cleanup(func() {
			for _, id := range sessionIDs {
				_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE id = $1`, id.String())
			}
		})

		results, err := repo.GetByPlayer(ctx, playerID)
		require.NoError(t, err)
		assert.Len(t, results, 3)

		// Verify all belong to the same player
		for _, r := range results {
			assert.Equal(t, playerID, r.PlayerID)
		}
	})

	t.Run("returns empty slice for player with no sessions", func(t *testing.T) {
		otherPlayerID := createTestPlayerForSession(ctx, t, "session_getbyplayer_empty_test")
		results, err := repo.GetByPlayer(ctx, otherPlayerID)
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestWebSessionRepository_UpdateLastSeen(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_updatelastseen_test")

	t.Run("updates last seen timestamp", func(t *testing.T) {
		originalTime := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "updatelastseen_hash_" + ulid.Make().String(),
			ExpiresAt:  time.Now().Add(time.Hour).UTC(),
			CreatedAt:  originalTime,
			LastSeenAt: originalTime,
		}
		err := repo.Create(ctx, session)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE id = $1`, session.ID.String())
		})

		newTime := time.Now().UTC().Truncate(time.Microsecond)
		err = repo.UpdateLastSeen(ctx, session.ID, newTime)
		require.NoError(t, err)

		// Verify update
		result, err := repo.GetByID(ctx, session.ID)
		require.NoError(t, err)
		assert.Equal(t, newTime, result.LastSeenAt.UTC().Truncate(time.Microsecond))
	})

	t.Run("returns ErrNotFound for non-existent ID", func(t *testing.T) {
		nonExistentID := ulid.Make()
		err := repo.UpdateLastSeen(ctx, nonExistentID, time.Now())
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestWebSessionRepository_UpdateCharacter(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_updatechar_test")

	t.Run("updates character ID", func(t *testing.T) {
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "updatechar_hash_" + ulid.Make().String(),
			ExpiresAt:  time.Now().Add(time.Hour).UTC(),
			CreatedAt:  time.Now().UTC(),
			LastSeenAt: time.Now().UTC(),
		}
		err := repo.Create(ctx, session)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE id = $1`, session.ID.String())
		})

		// Verify no character initially
		result, err := repo.GetByID(ctx, session.ID)
		require.NoError(t, err)
		assert.Nil(t, result.CharacterID)

		// Update with character
		characterID := createTestCharacterForSession(ctx, t, playerID, "updatechar_char_test")
		err = repo.UpdateCharacter(ctx, session.ID, characterID)
		require.NoError(t, err)

		// Verify update
		result, err = repo.GetByID(ctx, session.ID)
		require.NoError(t, err)
		require.NotNil(t, result.CharacterID)
		assert.Equal(t, characterID, *result.CharacterID)
	})

	t.Run("returns ErrNotFound for non-existent ID", func(t *testing.T) {
		nonExistentID := ulid.Make()
		characterID := ulid.Make()
		err := repo.UpdateCharacter(ctx, nonExistentID, characterID)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestWebSessionRepository_Delete(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_delete_test")

	t.Run("deletes existing session", func(t *testing.T) {
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "delete_test_hash_" + ulid.Make().String(),
			ExpiresAt:  time.Now().Add(time.Hour).UTC(),
			CreatedAt:  time.Now().UTC(),
			LastSeenAt: time.Now().UTC(),
		}
		err := repo.Create(ctx, session)
		require.NoError(t, err)

		err = repo.Delete(ctx, session.ID)
		require.NoError(t, err)

		// Verify it's gone
		result, err := repo.GetByID(ctx, session.ID)
		assert.Nil(t, result)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})

	t.Run("returns ErrNotFound for non-existent ID", func(t *testing.T) {
		nonExistentID := ulid.Make()
		err := repo.Delete(ctx, nonExistentID)
		assert.ErrorIs(t, err, auth.ErrNotFound)
	})
}

func TestWebSessionRepository_DeleteByPlayer(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_deletebyplayer_test")

	t.Run("deletes all sessions for player", func(t *testing.T) {
		// Create multiple sessions
		for i := 0; i < 3; i++ {
			session := &auth.WebSession{
				ID:         ulid.Make(),
				PlayerID:   playerID,
				TokenHash:  "deletebyplayer_hash_" + ulid.Make().String(),
				ExpiresAt:  time.Now().Add(time.Hour).UTC(),
				CreatedAt:  time.Now().UTC(),
				LastSeenAt: time.Now().UTC(),
			}
			err := repo.Create(ctx, session)
			require.NoError(t, err)
		}

		err := repo.DeleteByPlayer(ctx, playerID)
		require.NoError(t, err)

		// Verify all are gone
		results, err := repo.GetByPlayer(ctx, playerID)
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("succeeds even when no sessions exist", func(t *testing.T) {
		otherPlayerID := createTestPlayerForSession(ctx, t, "session_deletebyplayer_empty_test")
		err := repo.DeleteByPlayer(ctx, otherPlayerID)
		// Should not error - valid state to have no sessions
		assert.NoError(t, err)
	})
}

func TestWebSessionRepository_DeleteExpired(t *testing.T) {
	ctx := context.Background()
	repo := postgres.NewWebSessionRepository(testPool)
	playerID := createTestPlayerForSession(ctx, t, "session_deleteexpired_test")

	t.Run("deletes expired sessions and returns count", func(t *testing.T) {
		// Create an expired session
		expired := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "expired_session_hash_" + ulid.Make().String(),
			ExpiresAt:  time.Now().Add(-time.Hour).UTC(), // Already expired
			CreatedAt:  time.Now().Add(-2 * time.Hour).UTC(),
			LastSeenAt: time.Now().Add(-2 * time.Hour).UTC(),
		}
		err := repo.Create(ctx, expired)
		require.NoError(t, err)

		// Create a valid (non-expired) session
		valid := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "valid_session_hash_" + ulid.Make().String(),
			ExpiresAt:  time.Now().Add(time.Hour).UTC(), // Not expired
			CreatedAt:  time.Now().UTC(),
			LastSeenAt: time.Now().UTC(),
		}
		err = repo.Create(ctx, valid)
		require.NoError(t, err)

		t.Cleanup(func() {
			_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE player_id = $1`, playerID.String())
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

	t.Run("returns zero when no expired sessions", func(t *testing.T) {
		// Clean up any existing expired sessions first
		_, _ = testPool.Exec(ctx, `DELETE FROM web_sessions WHERE expires_at < NOW()`)

		count, err := repo.DeleteExpired(ctx)
		require.NoError(t, err)
		assert.Equal(t, int64(0), count)
	})
}

// Compile-time interface check (test ensures the repository implements the interface).
var _ auth.WebSessionRepository = (*postgres.WebSessionRepository)(nil)
