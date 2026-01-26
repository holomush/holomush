// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package auth_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/auth"
)

func TestHashPassword(t *testing.T) {
	hasher := auth.NewArgon2idHasher()

	t.Run("produces valid hash", func(t *testing.T) {
		hash, err := hasher.Hash("password123")
		require.NoError(t, err)
		assert.True(t, strings.HasPrefix(hash, "$argon2id$"))
	})

	t.Run("different passwords produce different hashes", func(t *testing.T) {
		hash1, err := hasher.Hash("password1")
		require.NoError(t, err)
		hash2, err := hasher.Hash("password2")
		require.NoError(t, err)
		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("same password produces different hashes (salt)", func(t *testing.T) {
		hash1, err := hasher.Hash("samepassword")
		require.NoError(t, err)
		hash2, err := hasher.Hash("samepassword")
		require.NoError(t, err)
		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("rejects empty password", func(t *testing.T) {
		_, err := hasher.Hash("")
		assert.Error(t, err)
	})
}

func TestVerifyPassword(t *testing.T) {
	hasher := auth.NewArgon2idHasher()

	t.Run("correct password verifies", func(t *testing.T) {
		hash, err := hasher.Hash("correctpassword")
		require.NoError(t, err)

		ok, err := hasher.Verify("correctpassword", hash)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("incorrect password fails", func(t *testing.T) {
		hash, err := hasher.Hash("correctpassword")
		require.NoError(t, err)

		ok, err := hasher.Verify("wrongpassword", hash)
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("invalid hash format returns error", func(t *testing.T) {
		_, err := hasher.Verify("password", "not-a-valid-hash")
		assert.Error(t, err)
	})

	t.Run("wrong algorithm returns error", func(t *testing.T) {
		_, err := hasher.Verify("password", "$argon2i$v=19$m=65536,t=1,p=4$c2FsdA$aGFzaA")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported hash algorithm")
	})

	t.Run("invalid version format returns error", func(t *testing.T) {
		_, err := hasher.Verify("password", "$argon2id$vXX$m=65536,t=1,p=4$c2FsdA$aGFzaA")
		assert.Error(t, err)
	})

	t.Run("invalid parameters format returns error", func(t *testing.T) {
		_, err := hasher.Verify("password", "$argon2id$v=19$invalid$c2FsdA$aGFzaA")
		assert.Error(t, err)
	})

	t.Run("invalid salt base64 returns error", func(t *testing.T) {
		_, err := hasher.Verify("password", "$argon2id$v=19$m=65536,t=1,p=4$!!!invalid!!!$aGFzaA")
		assert.Error(t, err)
	})

	t.Run("invalid hash base64 returns error", func(t *testing.T) {
		_, err := hasher.Verify("password", "$argon2id$v=19$m=65536,t=1,p=4$c2FsdA$!!!invalid!!!")
		assert.Error(t, err)
	})

	t.Run("threads overflow returns error", func(t *testing.T) {
		// threads=256 exceeds uint8 max (255)
		_, err := hasher.Verify("password", "$argon2id$v=19$m=65536,t=1,p=256$c2FsdA$aGFzaA")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "threads value")
	})
}

func TestVerifyBcryptUpgrade(t *testing.T) {
	hasher := auth.NewArgon2idHasher()

	// This is a valid bcrypt hash for testing upgrade detection
	bcryptHash := "$2a$10$N9qo8uLOickgx2ZMRZoMyeIvNq.Uf3hE9tQALNP1Qn9sNp5x5x5x5"

	t.Run("detects bcrypt hash needing upgrade", func(t *testing.T) {
		needsUpgrade := hasher.NeedsUpgrade(bcryptHash)
		assert.True(t, needsUpgrade)
	})

	t.Run("argon2id hash does not need upgrade", func(t *testing.T) {
		hash, err := hasher.Hash("password")
		require.NoError(t, err)
		assert.False(t, hasher.NeedsUpgrade(hash))
	})
}
