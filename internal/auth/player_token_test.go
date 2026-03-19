// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth

import (
	"testing"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPlayerToken(t *testing.T) {
	playerID := ulid.Make()
	token, err := NewPlayerToken(playerID, 24*time.Hour)
	require.NoError(t, err)
	assert.NotEmpty(t, token.Token)
	assert.Equal(t, playerID, token.PlayerID)
	assert.False(t, token.IsExpired())
}

func TestPlayerToken_IsExpired(t *testing.T) {
	playerID := ulid.Make()
	token, err := NewPlayerToken(playerID, -1*time.Hour)
	require.NoError(t, err)
	assert.True(t, token.IsExpired())
}

func TestPlayerToken_IsExpired_FixedTimes(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"expired one hour ago", now.Add(-1 * time.Hour), true},
		{"expires in one hour", now.Add(1 * time.Hour), false},
		{"expired one nanosecond ago", now.Add(-1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt := &PlayerToken{ExpiresAt: tt.expiresAt}
			assert.Equal(t, tt.want, pt.IsExpired())
		})
	}
}

func TestNewPlayerToken_TokenEntropy(t *testing.T) {
	playerID := ulid.Make()

	token1, err := NewPlayerToken(playerID, time.Hour)
	require.NoError(t, err)

	token2, err := NewPlayerToken(playerID, time.Hour)
	require.NoError(t, err)

	// Tokens should be 64 hex chars (32 bytes)
	assert.Len(t, token1.Token, 64)
	assert.Len(t, token2.Token, 64)

	// Two tokens for the same player must differ
	assert.NotEqual(t, token1.Token, token2.Token)
}
