// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
)

func TestGenerateResetToken(t *testing.T) {
	t.Run("generates secure token", func(t *testing.T) {
		token, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)
		assert.Len(t, token, 64) // 32 bytes hex-encoded
		assert.NotEmpty(t, hash)
		assert.NotEqual(t, token, hash)
	})

	t.Run("generates unique tokens", func(t *testing.T) {
		token1, hash1, err := auth.GenerateResetToken()
		require.NoError(t, err)

		token2, hash2, err := auth.GenerateResetToken()
		require.NoError(t, err)

		assert.NotEqual(t, token1, token2)
		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("hash is SHA256 hex-encoded", func(t *testing.T) {
		_, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)
		// SHA256 produces 32 bytes = 64 hex chars
		assert.Len(t, hash, 64)
	})
}

func TestVerifyResetToken(t *testing.T) {
	t.Run("verifies correct token", func(t *testing.T) {
		token, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		valid, err := auth.VerifyResetToken(token, hash)
		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("rejects incorrect token", func(t *testing.T) {
		_, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		valid, err := auth.VerifyResetToken("wrongtoken", hash)
		require.NoError(t, err)
		assert.False(t, valid)
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		_, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		valid, err := auth.VerifyResetToken("", hash)
		assert.Error(t, err)
		assert.False(t, valid)
		assert.Contains(t, err.Error(), "reset token cannot be empty")
	})

	t.Run("returns error for empty hash", func(t *testing.T) {
		token, _, err := auth.GenerateResetToken()
		require.NoError(t, err)

		valid, err := auth.VerifyResetToken(token, "")
		assert.Error(t, err)
		assert.False(t, valid)
		assert.Contains(t, err.Error(), "stored hash cannot be empty")
	})

	t.Run("rejects token with swapped characters", func(t *testing.T) {
		token, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		// Swap two different characters in the token to guarantee mutation.
		tokenBytes := []byte(token)
		for i := 0; i < len(tokenBytes)-1; i++ {
			if tokenBytes[i] != tokenBytes[i+1] {
				tokenBytes[i], tokenBytes[i+1] = tokenBytes[i+1], tokenBytes[i]
				break
			}
		}
		tamperedToken := string(tokenBytes)
		require.NotEqual(t, token, tamperedToken, "swap must produce a different token")

		valid, err := auth.VerifyResetToken(tamperedToken, hash)
		require.NoError(t, err)
		assert.False(t, valid)
	})
}

func TestPasswordReset_IsExpired(t *testing.T) {
	playerID := ulid.Make()

	t.Run("not expired when ExpiresAt is in future", func(t *testing.T) {
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "somehash",
			ExpiresAt: time.Now().Add(time.Hour),
			CreatedAt: time.Now(),
		}
		assert.False(t, reset.IsExpired())
	})

	t.Run("expired when ExpiresAt is in past", func(t *testing.T) {
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "somehash",
			ExpiresAt: time.Now().Add(-time.Hour),
			CreatedAt: time.Now().Add(-2 * time.Hour),
		}
		assert.True(t, reset.IsExpired())
	})

	t.Run("expired when ExpiresAt is just past now", func(t *testing.T) {
		now := time.Now()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "somehash",
			ExpiresAt: now.Add(-time.Nanosecond),
			CreatedAt: now.Add(-time.Hour),
		}
		assert.True(t, reset.IsExpired())
	})
}

func TestPasswordReset_IsExpiredAt(t *testing.T) {
	playerID := ulid.Make()
	baseTime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	t.Run("not expired when check time is before expiry", func(t *testing.T) {
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "somehash",
			ExpiresAt: baseTime.Add(time.Hour),
			CreatedAt: baseTime,
		}
		assert.False(t, reset.IsExpiredAt(baseTime.Add(30*time.Minute)))
	})

	t.Run("expired when check time is after expiry", func(t *testing.T) {
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "somehash",
			ExpiresAt: baseTime.Add(time.Hour),
			CreatedAt: baseTime,
		}
		assert.True(t, reset.IsExpiredAt(baseTime.Add(2*time.Hour)))
	})

	t.Run("not expired when check time equals expiry", func(t *testing.T) {
		expiryTime := baseTime.Add(time.Hour)
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "somehash",
			ExpiresAt: expiryTime,
			CreatedAt: baseTime,
		}
		// time.After returns false when times are equal
		assert.False(t, reset.IsExpiredAt(expiryTime))
	})
}

func TestNewPasswordReset(t *testing.T) {
	validPlayerID := ulid.Make()
	validHash := "abc123def456"
	validExpiry := time.Now().Add(time.Hour)

	t.Run("creates valid reset", func(t *testing.T) {
		reset, err := auth.NewPasswordReset(validPlayerID, validHash, validExpiry)
		require.NoError(t, err)
		assert.Equal(t, validPlayerID, reset.PlayerID)
		assert.Equal(t, validHash, reset.TokenHash)
		assert.Equal(t, validExpiry, reset.ExpiresAt)
		assert.False(t, reset.ID.Compare(ulid.ULID{}) == 0)
		assert.False(t, reset.CreatedAt.IsZero())
	})

	t.Run("rejects zero player ID", func(t *testing.T) {
		_, err := auth.NewPasswordReset(ulid.ULID{}, validHash, validExpiry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "player ID cannot be zero")
	})

	t.Run("rejects empty token hash", func(t *testing.T) {
		_, err := auth.NewPasswordReset(validPlayerID, "", validExpiry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "token hash cannot be empty")
	})

	t.Run("rejects zero expiry time", func(t *testing.T) {
		_, err := auth.NewPasswordReset(validPlayerID, validHash, time.Time{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expiry time cannot be zero")
	})
}

func TestResetTokenConstants(t *testing.T) {
	t.Run("token bytes is 32", func(t *testing.T) {
		assert.Equal(t, 32, auth.ResetTokenBytes)
	})

	t.Run("token expiry is 1 hour", func(t *testing.T) {
		assert.Equal(t, time.Hour, auth.ResetTokenExpiry)
	})
}
