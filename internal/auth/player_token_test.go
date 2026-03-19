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
