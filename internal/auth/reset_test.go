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

		assert.True(t, auth.VerifyResetToken(token, hash))
	})

	t.Run("rejects incorrect token", func(t *testing.T) {
		_, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		assert.False(t, auth.VerifyResetToken("wrongtoken", hash))
	})

	t.Run("rejects empty token", func(t *testing.T) {
		_, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		assert.False(t, auth.VerifyResetToken("", hash))
	})

	t.Run("rejects empty hash", func(t *testing.T) {
		token, _, err := auth.GenerateResetToken()
		require.NoError(t, err)

		assert.False(t, auth.VerifyResetToken(token, ""))
	})

	t.Run("rejects token with swapped characters", func(t *testing.T) {
		token, hash, err := auth.GenerateResetToken()
		require.NoError(t, err)

		// Swap two characters in the token
		tokenBytes := []byte(token)
		tokenBytes[0], tokenBytes[1] = tokenBytes[1], tokenBytes[0]
		tamperedToken := string(tokenBytes)

		assert.False(t, auth.VerifyResetToken(tamperedToken, hash))
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

	t.Run("expired when ExpiresAt is exactly now", func(t *testing.T) {
		now := time.Now()
		reset := &auth.PasswordReset{
			ID:        ulid.Make(),
			PlayerID:  playerID,
			TokenHash: "somehash",
			ExpiresAt: now.Add(-time.Nanosecond), // Just past now
			CreatedAt: now.Add(-time.Hour),
		}
		assert.True(t, reset.IsExpired())
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
