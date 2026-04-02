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

func TestNewPlayerSession(t *testing.T) {
	validPlayerID := ulid.Make()
	validHash := "abc123def456"
	validUserAgent := "Mozilla/5.0 Test Browser"
	validIPAddress := "192.168.1.1"

	t.Run("creates valid session", func(t *testing.T) {
		session, err := auth.NewPlayerSession(validPlayerID, validHash, validUserAgent, validIPAddress, auth.PlayerSessionTTL)
		require.NoError(t, err)
		assert.Equal(t, validPlayerID, session.PlayerID)
		assert.Equal(t, validHash, session.TokenHash)
		assert.Equal(t, validUserAgent, session.UserAgent)
		assert.Equal(t, validIPAddress, session.IPAddress)
		assert.False(t, session.ID.Compare(ulid.ULID{}) == 0)
		assert.False(t, session.CreatedAt.IsZero())
		assert.False(t, session.UpdatedAt.IsZero())
		assert.True(t, session.ExpiresAt.After(time.Now()))
	})

	t.Run("rejects zero player ID", func(t *testing.T) {
		_, err := auth.NewPlayerSession(ulid.ULID{}, validHash, validUserAgent, validIPAddress, auth.PlayerSessionTTL)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_PLAYER")
	})

	t.Run("rejects empty token hash", func(t *testing.T) {
		_, err := auth.NewPlayerSession(validPlayerID, "", validUserAgent, validIPAddress, auth.PlayerSessionTTL)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_HASH")
	})

	t.Run("accepts empty user agent", func(t *testing.T) {
		session, err := auth.NewPlayerSession(validPlayerID, validHash, "", validIPAddress, auth.PlayerSessionTTL)
		require.NoError(t, err)
		assert.Empty(t, session.UserAgent)
	})

	t.Run("accepts empty IP address", func(t *testing.T) {
		session, err := auth.NewPlayerSession(validPlayerID, validHash, validUserAgent, "", auth.PlayerSessionTTL)
		require.NoError(t, err)
		assert.Empty(t, session.IPAddress)
	})

	t.Run("rejects zero TTL", func(t *testing.T) {
		_, err := auth.NewPlayerSession(validPlayerID, validHash, validUserAgent, validIPAddress, 0)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_TTL")
	})

	t.Run("rejects negative TTL", func(t *testing.T) {
		_, err := auth.NewPlayerSession(validPlayerID, validHash, validUserAgent, validIPAddress, -time.Hour)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_TTL")
	})

	t.Run("ExpiresAt is approximately TTL from now", func(t *testing.T) {
		before := time.Now()
		session, err := auth.NewPlayerSession(validPlayerID, validHash, validUserAgent, validIPAddress, auth.PlayerSessionTTL)
		require.NoError(t, err)
		after := time.Now()

		assert.True(t, session.ExpiresAt.After(before.Add(auth.PlayerSessionTTL-time.Second)))
		assert.True(t, session.ExpiresAt.Before(after.Add(auth.PlayerSessionTTL+time.Second)))
	})
}

func TestPlayerSession_IsExpired(t *testing.T) {
	t.Run("false when ExpiresAt is in future", func(t *testing.T) {
		session := &auth.PlayerSession{
			ExpiresAt: time.Now().Add(time.Hour),
		}
		assert.False(t, session.IsExpired())
	})

	t.Run("true when ExpiresAt is in past", func(t *testing.T) {
		session := &auth.PlayerSession{
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		assert.True(t, session.IsExpired())
	})
}

func TestPlayerSession_Refresh(t *testing.T) {
	t.Run("updates ExpiresAt and UpdatedAt", func(t *testing.T) {
		session := &auth.PlayerSession{
			ExpiresAt: time.Now().Add(-time.Hour), // expired
			UpdatedAt: time.Now().Add(-time.Hour),
		}
		oldUpdatedAt := session.UpdatedAt

		before := time.Now()
		require.NoError(t, session.Refresh(auth.PlayerSessionTTL))
		after := time.Now()

		assert.True(t, session.ExpiresAt.After(before.Add(auth.PlayerSessionTTL-time.Second)))
		assert.True(t, session.ExpiresAt.Before(after.Add(auth.PlayerSessionTTL+time.Second)))
		assert.True(t, session.UpdatedAt.After(oldUpdatedAt))
	})

	t.Run("session is no longer expired after refresh", func(t *testing.T) {
		session := &auth.PlayerSession{
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		assert.True(t, session.IsExpired())

		require.NoError(t, session.Refresh(auth.PlayerSessionTTL))
		assert.False(t, session.IsExpired())
	})

	t.Run("rejects zero TTL", func(t *testing.T) {
		session := &auth.PlayerSession{ExpiresAt: time.Now().Add(time.Hour)}
		err := session.Refresh(0)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_TTL")
	})

	t.Run("rejects negative TTL", func(t *testing.T) {
		session := &auth.PlayerSession{ExpiresAt: time.Now().Add(time.Hour)}
		err := session.Refresh(-time.Minute)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "SESSION_INVALID_TTL")
	})
}

func TestPlayerSessionTTL(t *testing.T) {
	t.Run("TTL is 24 hours", func(t *testing.T) {
		assert.Equal(t, 24*time.Hour, auth.PlayerSessionTTL)
	})
}
