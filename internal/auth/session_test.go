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
	"github.com/holomush/holomush/pkg/errutil"
)

func TestGenerateSessionToken(t *testing.T) {
	t.Run("generates secure token", func(t *testing.T) {
		token, hash, err := auth.GenerateSessionToken()
		require.NoError(t, err)
		assert.Len(t, token, 64) // 32 bytes hex-encoded
		assert.NotEmpty(t, hash)
		assert.NotEqual(t, token, hash)
	})

	t.Run("generates unique tokens", func(t *testing.T) {
		token1, hash1, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		token2, hash2, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		assert.NotEqual(t, token1, token2)
		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("hash is SHA256 hex-encoded", func(t *testing.T) {
		_, hash, err := auth.GenerateSessionToken()
		require.NoError(t, err)
		// SHA256 produces 32 bytes = 64 hex chars
		assert.Len(t, hash, 64)
	})
}

func TestHashSessionToken(t *testing.T) {
	t.Run("produces consistent hash", func(t *testing.T) {
		token := "testtoken123"
		hash1 := auth.HashSessionToken(token)
		hash2 := auth.HashSessionToken(token)
		assert.Equal(t, hash1, hash2)
	})

	t.Run("produces different hashes for different tokens", func(t *testing.T) {
		hash1 := auth.HashSessionToken("token1")
		hash2 := auth.HashSessionToken("token2")
		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("hash is SHA256 hex-encoded", func(t *testing.T) {
		hash := auth.HashSessionToken("anytoken")
		assert.Len(t, hash, 64) // SHA256 = 32 bytes = 64 hex chars
	})
}

func TestWebSession_IsExpired(t *testing.T) {
	playerID := ulid.Make()

	t.Run("not expired when ExpiresAt is in future", func(t *testing.T) {
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "somehash",
			ExpiresAt:  time.Now().Add(time.Hour),
			CreatedAt:  time.Now(),
			LastSeenAt: time.Now(),
		}
		assert.False(t, session.IsExpired())
	})

	t.Run("expired when ExpiresAt is in past", func(t *testing.T) {
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "somehash",
			ExpiresAt:  time.Now().Add(-time.Hour),
			CreatedAt:  time.Now().Add(-2 * time.Hour),
			LastSeenAt: time.Now().Add(-time.Hour),
		}
		assert.True(t, session.IsExpired())
	})

	t.Run("expired when ExpiresAt is just past now", func(t *testing.T) {
		now := time.Now()
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "somehash",
			ExpiresAt:  now.Add(-time.Nanosecond),
			CreatedAt:  now.Add(-time.Hour),
			LastSeenAt: now.Add(-time.Minute),
		}
		assert.True(t, session.IsExpired())
	})
}

func TestWebSession_IsExpiredAt(t *testing.T) {
	playerID := ulid.Make()
	baseTime := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)

	t.Run("not expired when check time is before expiry", func(t *testing.T) {
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "somehash",
			ExpiresAt:  baseTime.Add(time.Hour),
			CreatedAt:  baseTime,
			LastSeenAt: baseTime,
		}
		assert.False(t, session.IsExpiredAt(baseTime.Add(30*time.Minute)))
	})

	t.Run("expired when check time is after expiry", func(t *testing.T) {
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "somehash",
			ExpiresAt:  baseTime.Add(time.Hour),
			CreatedAt:  baseTime,
			LastSeenAt: baseTime,
		}
		assert.True(t, session.IsExpiredAt(baseTime.Add(2*time.Hour)))
	})

	t.Run("not expired when check time equals expiry", func(t *testing.T) {
		expiryTime := baseTime.Add(time.Hour)
		session := &auth.WebSession{
			ID:         ulid.Make(),
			PlayerID:   playerID,
			TokenHash:  "somehash",
			ExpiresAt:  expiryTime,
			CreatedAt:  baseTime,
			LastSeenAt: baseTime,
		}
		// time.After returns false when times are equal
		assert.False(t, session.IsExpiredAt(expiryTime))
	})
}

func TestNewWebSession(t *testing.T) {
	validPlayerID := ulid.Make()
	validCharacterID := ulid.Make()
	validHash := "abc123def456"
	validUserAgent := "Mozilla/5.0 Test Browser"
	validIPAddress := "192.168.1.1"
	validExpiry := time.Now().Add(24 * time.Hour)

	t.Run("creates valid session without character", func(t *testing.T) {
		session, err := auth.NewWebSession(validPlayerID, nil, validHash, validUserAgent, validIPAddress, validExpiry)
		require.NoError(t, err)
		assert.Equal(t, validPlayerID, session.PlayerID)
		assert.Nil(t, session.CharacterID)
		assert.Equal(t, validHash, session.TokenHash)
		assert.Equal(t, validUserAgent, session.UserAgent)
		assert.Equal(t, validIPAddress, session.IPAddress)
		assert.Equal(t, validExpiry, session.ExpiresAt)
		assert.False(t, session.ID.Compare(ulid.ULID{}) == 0)
		assert.False(t, session.CreatedAt.IsZero())
		assert.False(t, session.LastSeenAt.IsZero())
	})

	t.Run("creates valid session with character", func(t *testing.T) {
		session, err := auth.NewWebSession(validPlayerID, &validCharacterID, validHash, validUserAgent, validIPAddress, validExpiry)
		require.NoError(t, err)
		assert.Equal(t, validPlayerID, session.PlayerID)
		require.NotNil(t, session.CharacterID)
		assert.Equal(t, validCharacterID, *session.CharacterID)
	})

	t.Run("rejects zero player ID", func(t *testing.T) {
		_, err := auth.NewWebSession(ulid.ULID{}, nil, validHash, validUserAgent, validIPAddress, validExpiry)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_PLAYER")
		assert.Contains(t, err.Error(), "player ID cannot be zero")
	})

	t.Run("rejects zero character ID when provided", func(t *testing.T) {
		zeroCharID := ulid.ULID{}
		_, err := auth.NewWebSession(validPlayerID, &zeroCharID, validHash, validUserAgent, validIPAddress, validExpiry)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_CHARACTER")
		assert.Contains(t, err.Error(), "character ID cannot be zero")
	})

	t.Run("rejects empty token hash", func(t *testing.T) {
		_, err := auth.NewWebSession(validPlayerID, nil, "", validUserAgent, validIPAddress, validExpiry)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_HASH")
		assert.Contains(t, err.Error(), "token hash cannot be empty")
	})

	t.Run("rejects zero expiry time", func(t *testing.T) {
		_, err := auth.NewWebSession(validPlayerID, nil, validHash, validUserAgent, validIPAddress, time.Time{})
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_EXPIRY")
		assert.Contains(t, err.Error(), "expiry time cannot be zero")
	})

	t.Run("accepts empty user agent", func(t *testing.T) {
		session, err := auth.NewWebSession(validPlayerID, nil, validHash, "", validIPAddress, validExpiry)
		require.NoError(t, err)
		assert.Empty(t, session.UserAgent)
	})

	t.Run("accepts empty IP address", func(t *testing.T) {
		session, err := auth.NewWebSession(validPlayerID, nil, validHash, validUserAgent, "", validExpiry)
		require.NoError(t, err)
		assert.Empty(t, session.IPAddress)
	})
}

func TestVerifySessionToken(t *testing.T) {
	t.Run("verifies correct token", func(t *testing.T) {
		token, hash, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		valid, err := auth.VerifySessionToken(token, hash)
		require.NoError(t, err)
		assert.True(t, valid)
	})

	t.Run("rejects incorrect token", func(t *testing.T) {
		_, hash, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		valid, err := auth.VerifySessionToken("wrongtoken", hash)
		require.NoError(t, err)
		assert.False(t, valid)
	})

	t.Run("returns error for empty token", func(t *testing.T) {
		_, hash, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		valid, err := auth.VerifySessionToken("", hash)
		assert.Error(t, err)
		assert.False(t, valid)
		errutil.AssertErrorCode(t, err, "SESSION_TOKEN_EMPTY")
	})

	t.Run("returns error for empty hash", func(t *testing.T) {
		token, _, err := auth.GenerateSessionToken()
		require.NoError(t, err)

		valid, err := auth.VerifySessionToken(token, "")
		assert.Error(t, err)
		assert.False(t, valid)
		errutil.AssertErrorCode(t, err, "SESSION_HASH_EMPTY")
	})
}

func TestSessionTokenConstants(t *testing.T) {
	t.Run("token bytes is 32", func(t *testing.T) {
		assert.Equal(t, 32, auth.SessionTokenBytes)
	})

	t.Run("session expiry is 24 hours", func(t *testing.T) {
		assert.Equal(t, 24*time.Hour, auth.SessionTokenExpiry)
	})
}
