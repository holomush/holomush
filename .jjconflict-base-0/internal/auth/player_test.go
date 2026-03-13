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

func TestNewPlayer(t *testing.T) {
	t.Run("creates valid player with email", func(t *testing.T) {
		email := "test@example.com"
		player, err := auth.NewPlayer("ValidUser", &email, "$argon2id$hash")
		require.NoError(t, err)
		require.NotNil(t, player)

		assert.NotEqual(t, ulid.ULID{}, player.ID)
		assert.Equal(t, "ValidUser", player.Username)
		assert.Equal(t, &email, player.Email)
		assert.Equal(t, "$argon2id$hash", player.PasswordHash)
		assert.False(t, player.CreatedAt.IsZero())
		assert.False(t, player.UpdatedAt.IsZero())
		assert.Equal(t, player.CreatedAt, player.UpdatedAt)
	})

	t.Run("creates valid player without email", func(t *testing.T) {
		player, err := auth.NewPlayer("ValidUser", nil, "$argon2id$hash")
		require.NoError(t, err)
		require.NotNil(t, player)
		assert.Nil(t, player.Email)
	})

	t.Run("rejects empty username", func(t *testing.T) {
		player, err := auth.NewPlayer("", nil, "$argon2id$hash")
		assert.Nil(t, player)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
	})

	t.Run("rejects short username", func(t *testing.T) {
		player, err := auth.NewPlayer("ab", nil, "$argon2id$hash")
		assert.Nil(t, player)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
	})

	t.Run("rejects empty password hash", func(t *testing.T) {
		player, err := auth.NewPlayer("ValidUser", nil, "")
		assert.Nil(t, player)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_PASSWORD")
	})

	t.Run("rejects whitespace-only password hash", func(t *testing.T) {
		player, err := auth.NewPlayer("ValidUser", nil, "   \t  ")
		assert.Nil(t, player)
		require.Error(t, err)
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_PASSWORD")
	})
}

func TestPlayer_IsLocked(t *testing.T) {
	t.Run("no lockout", func(t *testing.T) {
		p := &auth.Player{}
		assert.False(t, p.IsLocked())
	})

	t.Run("future lockout", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		p := &auth.Player{LockedUntil: &future}
		assert.True(t, p.IsLocked())
	})

	t.Run("past lockout", func(t *testing.T) {
		past := time.Now().Add(-time.Hour)
		p := &auth.Player{LockedUntil: &past}
		assert.False(t, p.IsLocked())
	})
}

func TestPlayer_RecordFailure(t *testing.T) {
	t.Run("increments counter", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: 0}
		p.RecordFailure()
		assert.Equal(t, 1, p.FailedAttempts)
	})

	t.Run("no lockout below threshold", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: auth.LockoutThreshold - 2}
		p.RecordFailure()
		assert.Equal(t, auth.LockoutThreshold-1, p.FailedAttempts)
		assert.Nil(t, p.LockedUntil)
	})

	t.Run("sets lockout at threshold", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: auth.LockoutThreshold - 1}
		p.RecordFailure()
		assert.Equal(t, auth.LockoutThreshold, p.FailedAttempts)
		assert.NotNil(t, p.LockedUntil)
		assert.True(t, p.LockedUntil.After(time.Now()))
	})

	t.Run("updates UpdatedAt", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: 0}
		before := time.Now().Add(-time.Millisecond)
		p.RecordFailure()
		assert.True(t, p.UpdatedAt.After(before))
	})
}

func TestPlayer_RecordSuccess(t *testing.T) {
	t.Run("resets failures and lockout", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		p := &auth.Player{
			FailedAttempts: 5,
			LockedUntil:    &future,
		}
		p.RecordSuccess()
		assert.Equal(t, 0, p.FailedAttempts)
		assert.Nil(t, p.LockedUntil)
	})

	t.Run("updates UpdatedAt", func(t *testing.T) {
		p := &auth.Player{FailedAttempts: 3}
		before := time.Now().Add(-time.Millisecond)
		p.RecordSuccess()
		assert.True(t, p.UpdatedAt.After(before))
	})
}

func TestPlayerPreferences(t *testing.T) {
	t.Run("default values", func(t *testing.T) {
		prefs := auth.PlayerPreferences{}
		assert.False(t, prefs.AutoLogin)
		assert.Equal(t, 0, prefs.MaxCharacters) // 0 means use default
	})

	t.Run("effective max characters uses default when zero", func(t *testing.T) {
		prefs := auth.PlayerPreferences{}
		assert.Equal(t, auth.DefaultMaxCharacters, prefs.EffectiveMaxCharacters())
	})

	t.Run("effective max characters uses custom when set", func(t *testing.T) {
		prefs := auth.PlayerPreferences{MaxCharacters: 10}
		assert.Equal(t, 10, prefs.EffectiveMaxCharacters())
	})

	t.Run("effective max characters uses default when negative", func(t *testing.T) {
		prefs := auth.PlayerPreferences{MaxCharacters: -1}
		assert.Equal(t, auth.DefaultMaxCharacters, prefs.EffectiveMaxCharacters())
	})
}

func TestPlayer_Fields(t *testing.T) {
	t.Run("all fields are settable", func(t *testing.T) {
		now := time.Now()
		playerID := ulid.Make()
		charID := ulid.Make()
		email := "test@example.com"

		p := &auth.Player{
			ID:                 playerID,
			Username:           "testuser",
			PasswordHash:       "$argon2id$v=19$...",
			Email:              &email,
			EmailVerified:      true,
			FailedAttempts:     2,
			LockedUntil:        nil,
			DefaultCharacterID: &charID,
			Preferences: auth.PlayerPreferences{
				AutoLogin:     true,
				MaxCharacters: 3,
				Theme:         "dark",
			},
			CreatedAt: now,
			UpdatedAt: now,
		}

		assert.Equal(t, playerID, p.ID)
		assert.Equal(t, "testuser", p.Username)
		assert.Equal(t, "$argon2id$v=19$...", p.PasswordHash)
		assert.Equal(t, &email, p.Email)
		assert.True(t, p.EmailVerified)
		assert.Equal(t, 2, p.FailedAttempts)
		assert.Nil(t, p.LockedUntil)
		assert.Equal(t, &charID, p.DefaultCharacterID)
		assert.True(t, p.Preferences.AutoLogin)
		assert.Equal(t, 3, p.Preferences.MaxCharacters)
		assert.Equal(t, "dark", p.Preferences.Theme)
		assert.Equal(t, now, p.CreatedAt)
		assert.Equal(t, now, p.UpdatedAt)
	})
}

func TestValidateUsername(t *testing.T) {
	tests := []struct {
		name     string
		username string
		wantErr  bool
	}{
		{"valid", "testuser", false},
		{"valid with numbers", "user123", false},
		{"valid with underscore", "test_user", false},
		{"valid min length", "abc", false},
		{"valid max length", "abcdefghijklmnopqrstuvwxyz1234", false}, // 30 chars
		{"too short", "ab", true},
		{"too long", "abcdefghijklmnopqrstuvwxyz12345", true}, // 31 chars
		{"empty", "", true},
		{"spaces", "test user", true},
		{"special chars at", "test@user", true},
		{"special chars bang", "test!user", true},
		{"special chars hyphen", "test-user", true},
		{"starts with number", "123user", true},
		{"starts with underscore", "_user", true},
		{"uppercase valid", "TestUser", false},
		{"mixed case valid", "Test_User_123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := auth.ValidateUsername(tt.username)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateUsername_ErrorCodes(t *testing.T) {
	t.Run("empty username has correct error code", func(t *testing.T) {
		err := auth.ValidateUsername("")
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("too short has correct error code", func(t *testing.T) {
		err := auth.ValidateUsername("ab")
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
		assert.Contains(t, err.Error(), "at least")
	})

	t.Run("too long has correct error code", func(t *testing.T) {
		err := auth.ValidateUsername("abcdefghijklmnopqrstuvwxyz12345")
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
		assert.Contains(t, err.Error(), "at most")
	})

	t.Run("invalid chars has correct error code", func(t *testing.T) {
		err := auth.ValidateUsername("test@user")
		errutil.AssertErrorCode(t, err, "AUTH_INVALID_USERNAME")
		assert.Contains(t, err.Error(), "letter")
	})
}
