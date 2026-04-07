// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telnet

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/naming"
	"github.com/oklog/ulid/v2"
)

// TestGemstoneElementTheme_Name verifies the theme name.
func TestGemstoneElementTheme_Name(t *testing.T) {
	theme := naming.NewGemstoneElementTheme()
	assert.Equal(t, "gemstone_element", theme.Name())
}

// TestGemstoneElementTheme_Generate verifies generated names are non-empty and distinct.
func TestGemstoneElementTheme_Generate(t *testing.T) {
	theme := naming.NewGemstoneElementTheme()
	first, second := theme.Generate()
	assert.NotEmpty(t, first)
	assert.NotEmpty(t, second)
	assert.NotEqual(t, first, second)
}

// TestGemstoneElementTheme_UniqueNames generates names and verifies there is varied output.
func TestGemstoneElementTheme_UniqueNames(t *testing.T) {
	theme := naming.NewGemstoneElementTheme()
	seen := make(map[string]struct{})
	for i := 0; i < 50; i++ {
		first, second := theme.Generate()
		key := first + "_" + second
		seen[key] = struct{}{}
	}
	// With a 20x20 = 400 pool and random selection, 50 draws should produce varied output.
	assert.Greater(t, len(seen), 1, "expected varied output from 50 generates")
}

// TestGemstoneElementTheme_TitleCase verifies each name part matches ^[A-Z][a-z]+$.
func TestGemstoneElementTheme_TitleCase(t *testing.T) {
	theme := naming.NewGemstoneElementTheme()
	re := regexp.MustCompile(`^[A-Z][a-z]+$`)
	for i := 0; i < 30; i++ {
		first, second := theme.Generate()
		assert.Truef(t, re.MatchString(first), "first name %q does not match title case pattern", first)
		assert.Truef(t, re.MatchString(second), "second name %q does not match title case pattern", second)
	}
}

func TestGuestAuthenticator_StartLocationReturnsConfiguredLocation(t *testing.T) {
	startLocation := ulid.Make()
	auth := NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLocation)
	assert.Equal(t, startLocation, auth.StartLocation())
}

func TestGuestAuthenticator_GenerateName(t *testing.T) {
	startLocation := ulid.Make()
	auth := NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLocation)

	name, err := auth.GenerateName()
	require.NoError(t, err)
	assert.NotEmpty(t, name)
	assert.Contains(t, name, "_") // themed names use underscore separator
}

func TestGuestAuthenticator_GenerateName_Unique(t *testing.T) {
	startLocation := ulid.Make()
	auth := NewGuestAuthenticator(naming.NewGemstoneElementTheme(), startLocation)

	seen := make(map[string]struct{})
	for i := 0; i < 20; i++ {
		name, err := auth.GenerateName()
		require.NoError(t, err)
		_, duplicate := seen[name]
		assert.False(t, duplicate, "duplicate name: %s", name)
		seen[name] = struct{}{}
	}
}

func TestGuestAuthenticator_ReleaseGuestRemovesFromActiveSet(t *testing.T) {
	auth := NewGuestAuthenticator(naming.NewGemstoneElementTheme(), ulid.Make())

	name, err := auth.GenerateName()
	require.NoError(t, err)
	assert.Equal(t, 1, auth.ActiveCount())

	auth.ReleaseGuest(name)
	assert.Equal(t, 0, auth.ActiveCount())
}
