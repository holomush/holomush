// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"context"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcserver "github.com/holomush/holomush/internal/grpc"
	"github.com/oklog/ulid/v2"
)

// TestGemstoneElementTheme_Name verifies the theme name.
func TestGemstoneElementTheme_Name(t *testing.T) {
	theme := NewGemstoneElementTheme()
	assert.Equal(t, "GemstoneElement", theme.Name())
}

// TestGemstoneElementTheme_Generate verifies generated names are non-empty and distinct.
func TestGemstoneElementTheme_Generate(t *testing.T) {
	theme := NewGemstoneElementTheme()
	first, second := theme.Generate()
	assert.NotEmpty(t, first)
	assert.NotEmpty(t, second)
	assert.NotEqual(t, first, second)
}

// TestGemstoneElementTheme_UniqueNames generates 50 names and verifies there is varied output.
func TestGemstoneElementTheme_UniqueNames(t *testing.T) {
	theme := NewGemstoneElementTheme()
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		first, second := theme.Generate()
		key := first + "_" + second
		seen[key] = struct{}{}
	}
	// With a 20x20 = 400 pool and random selection, 50 draws should produce at least 10 distinct names.
	assert.Greater(t, len(seen), 10, "expected varied output from 50 generates")
}

// TestGemstoneElementTheme_TitleCase verifies each name part matches ^[A-Z][a-z]+$.
func TestGemstoneElementTheme_TitleCase(t *testing.T) {
	theme := NewGemstoneElementTheme()
	re := regexp.MustCompile(`^[A-Z][a-z]+$`)
	for i := 0; i < 30; i++ {
		first, second := theme.Generate()
		assert.Truef(t, re.MatchString(first), "first name %q does not match title case pattern", first)
		assert.Truef(t, re.MatchString(second), "second name %q does not match title case pattern", second)
	}
}

// TestGuestAuthenticator_GuestLogin verifies that "guest" username produces a valid AuthResult.
func TestGuestAuthenticator_GuestLogin(t *testing.T) {
	startLocation := ulid.Make()
	auth := NewGuestAuthenticator(NewGemstoneElementTheme(), startLocation)

	result, err := auth.Authenticate(context.Background(), "guest", "")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.CharacterName)
	assert.NotEqual(t, ulid.ULID{}, result.CharacterID)
	assert.Equal(t, startLocation, result.LocationID)
}

// TestGuestAuthenticator_RegisteredLoginRejected verifies non-guest usernames are rejected.
func TestGuestAuthenticator_RegisteredLoginRejected(t *testing.T) {
	startLocation := ulid.Make()
	auth := NewGuestAuthenticator(NewGemstoneElementTheme(), startLocation)

	result, err := auth.Authenticate(context.Background(), "alice", "secret")
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "Registered accounts are not yet available")
}

// TestGuestAuthenticator_UniqueNames verifies 20 guest logins all receive unique names.
func TestGuestAuthenticator_UniqueNames(t *testing.T) {
	startLocation := ulid.Make()
	auth := NewGuestAuthenticator(NewGemstoneElementTheme(), startLocation)

	seen := make(map[string]struct{})
	for i := 0; i < 20; i++ {
		result, err := auth.Authenticate(context.Background(), "guest", "")
		require.NoError(t, err)
		_, duplicate := seen[result.CharacterName]
		assert.Falsef(t, duplicate, "duplicate guest name: %s", result.CharacterName)
		seen[result.CharacterName] = struct{}{}
	}
}

// TestGuestAuthenticator_ImplementsInterface is a compile-time check.
func TestGuestAuthenticator_ImplementsInterface(_ *testing.T) {
	var _ grpcserver.Authenticator = (*GuestAuthenticator)(nil)
}
