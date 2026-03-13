// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for the refactored Resolve methods
// These test each extracted method in isolation

func TestAliasCache_resolveExact_Match(t *testing.T) {
	cache := NewAliasCache()
	registry := NewRegistry()

	// Register "look" as a command
	err := registry.Register(CommandEntry{
		Name:    "look",
		handler: testHandler,
		Source:  "core",
	})
	require.NoError(t, err)

	// Should return the command unchanged
	resolved, matched := cache.resolveExact("look", registry)
	assert.True(t, matched)
	assert.Equal(t, "look", resolved)
}

func TestAliasCache_resolveExact_NoMatch(t *testing.T) {
	cache := NewAliasCache()
	registry := NewRegistry()

	// Register "look" but check "unknown"
	err := registry.Register(CommandEntry{
		Name:    "look",
		handler: testHandler,
		Source:  "core",
	})
	require.NoError(t, err)

	// Should return empty and false for non-matching command
	resolved, matched := cache.resolveExact("unknown", registry)
	assert.False(t, matched)
	assert.Empty(t, resolved)
}

func TestAliasCache_resolveExact_NilRegistry(t *testing.T) {
	cache := NewAliasCache()

	// Should return empty and false when registry is nil
	resolved, matched := cache.resolveExact("look", nil)
	assert.False(t, matched)
	assert.Empty(t, resolved)
}

func TestAliasCache_resolveExact_EmptyInput(t *testing.T) {
	cache := NewAliasCache()
	registry := NewRegistry()

	// Should return empty and false for empty input
	resolved, matched := cache.resolveExact("", registry)
	assert.False(t, matched)
	assert.Empty(t, resolved)
}

func TestAliasCache_resolvePlayerAlias_Match(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	err := cache.SetPlayerAlias(playerID, "l", "look")
	require.NoError(t, err)

	// Should find player alias
	result, matched := cache.resolvePlayerAlias(playerID, "l")
	assert.True(t, matched)
	assert.Equal(t, "look", result.resolvedCmd)
	assert.Equal(t, "l", result.aliasUsed)
	assert.False(t, result.isPrefix)
	assert.Empty(t, result.rest)
}

func TestAliasCache_resolvePlayerAlias_NoMatch(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Should not match when no alias exists
	result, matched := cache.resolvePlayerAlias(playerID, "unknown")
	assert.False(t, matched)
	assert.Empty(t, result.resolvedCmd)
	assert.Empty(t, result.aliasUsed)
}

func TestAliasCache_resolvePlayerAlias_WrongPlayer(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)
	otherPlayer := ulid.MustNew(2, nil)

	err := cache.SetPlayerAlias(playerID, "l", "look")
	require.NoError(t, err)

	// Should not match for different player
	result, matched := cache.resolvePlayerAlias(otherPlayer, "l")
	assert.False(t, matched)
	assert.Empty(t, result.resolvedCmd)
}

func TestAliasCache_resolveSystemAlias_Match(t *testing.T) {
	cache := NewAliasCache()

	err := cache.SetSystemAlias("l", "look")
	require.NoError(t, err)

	// Should find system alias
	result, matched := cache.resolveSystemAlias("l")
	assert.True(t, matched)
	assert.Equal(t, "look", result.resolvedCmd)
	assert.Equal(t, "l", result.aliasUsed)
	assert.False(t, result.isPrefix)
	assert.Empty(t, result.rest)
}

func TestAliasCache_resolveSystemAlias_NoMatch(t *testing.T) {
	cache := NewAliasCache()

	// Should not match when no alias exists
	result, matched := cache.resolveSystemAlias("unknown")
	assert.False(t, matched)
	assert.Empty(t, result.resolvedCmd)
	assert.Empty(t, result.aliasUsed)
}

func TestAliasCache_resolvePrefix_PlayerAlias(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Set up a prefix alias ":" → "pose"
	err := cache.SetPlayerAlias(playerID, ":", "pose")
	require.NoError(t, err)

	// Should match prefix
	result, matched := cache.resolvePrefix(playerID, ":waves")
	assert.True(t, matched)
	assert.Equal(t, "pose", result.resolvedCmd)
	assert.Equal(t, ":", result.aliasUsed)
	assert.True(t, result.isPrefix)
	assert.Equal(t, "waves", result.rest)
}

func TestAliasCache_resolvePrefix_SystemAlias(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Set up system prefix alias ";" → "pose"
	err := cache.SetSystemAlias(";", "pose")
	require.NoError(t, err)

	// Should match prefix (system aliases checked if no player match)
	result, matched := cache.resolvePrefix(playerID, ";hello")
	assert.True(t, matched)
	assert.Equal(t, "pose", result.resolvedCmd)
	assert.Equal(t, ";", result.aliasUsed)
	assert.True(t, result.isPrefix)
	assert.Equal(t, "hello", result.rest)
}

func TestAliasCache_resolvePrefix_PlayerOverridesSystem(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Set up both player and system prefix aliases
	err := cache.SetSystemAlias(":", "say")
	require.NoError(t, err)
	err = cache.SetPlayerAlias(playerID, ":", "pose")
	require.NoError(t, err)

	// Player alias should take precedence
	result, matched := cache.resolvePrefix(playerID, ":waves")
	assert.True(t, matched)
	assert.Equal(t, "pose", result.resolvedCmd) // Player's alias
	assert.Equal(t, ":", result.aliasUsed)
	assert.True(t, result.isPrefix)
	assert.Equal(t, "waves", result.rest)
}

func TestAliasCache_resolvePrefix_NoMatch(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Should not match when no prefix alias exists
	result, matched := cache.resolvePrefix(playerID, ":waves")
	assert.False(t, matched)
	assert.Empty(t, result.resolvedCmd)
}

func TestAliasCache_resolvePrefix_SingleChar(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Set up a prefix alias
	err := cache.SetPlayerAlias(playerID, ":", "pose")
	require.NoError(t, err)

	// Single character input should not be treated as prefix
	// (it's an exact alias match, not a prefix match)
	_, matched := cache.resolvePrefix(playerID, ":")
	assert.False(t, matched)
}

func TestAliasCache_resolvePrefix_EmptyInput(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Set up a prefix alias
	err := cache.SetPlayerAlias(playerID, ":", "pose")
	require.NoError(t, err)

	// Empty input should not match
	_, matched := cache.resolvePrefix(playerID, "")
	assert.False(t, matched)
}

func TestAliasCache_resolvePrefix_Possessive(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Set up a prefix alias ";" → "pose"
	err := cache.SetPlayerAlias(playerID, ";", "pose")
	require.NoError(t, err)

	// Test possessive form
	result, matched := cache.resolvePrefix(playerID, ";'s eyes widen")
	assert.True(t, matched)
	assert.Equal(t, "pose", result.resolvedCmd)
	assert.Equal(t, ";", result.aliasUsed)
	assert.True(t, result.isPrefix)
	assert.Equal(t, "'s eyes widen", result.rest)
}

func TestAliasCache_resolveAlias_WithRecursiveExpansion(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Create chain: a → b, b → look
	err := cache.SetSystemAlias("a", "b")
	require.NoError(t, err)
	err = cache.SetSystemAlias("b", "look")
	require.NoError(t, err)

	// Should recursively expand
	result, matched := cache.resolveAlias(playerID, "a")
	assert.True(t, matched)
	assert.Equal(t, "look", result.resolvedCmd)
	assert.Equal(t, "a", result.aliasUsed)
	assert.False(t, result.isPrefix)
}

func TestAliasCache_resolveAlias_PlayerOverridesSystem(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Player alias overrides system alias
	err := cache.SetSystemAlias("l", "look")
	require.NoError(t, err)
	err = cache.SetPlayerAlias(playerID, "l", "list")
	require.NoError(t, err)

	// Should return player alias
	result, matched := cache.resolveAlias(playerID, "l")
	assert.True(t, matched)
	assert.Equal(t, "list", result.resolvedCmd)
	assert.Equal(t, "l", result.aliasUsed)
}

func TestAliasCache_resolveAlias_NoMatch(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// No aliases defined
	result, matched := cache.resolveAlias(playerID, "unknown")
	assert.False(t, matched)
	assert.Empty(t, result.resolvedCmd)
	assert.Empty(t, result.aliasUsed)
}

func TestAliasCache_resolveAlias_WithArgs(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Alias with args in expansion
	err := cache.SetSystemAlias("a", "b extra")
	require.NoError(t, err)
	err = cache.SetSystemAlias("b", "final")
	require.NoError(t, err)

	// Should preserve args through expansion
	result, matched := cache.resolveAlias(playerID, "a")
	assert.True(t, matched)
	assert.Equal(t, "final extra", result.resolvedCmd)
	assert.Equal(t, "a", result.aliasUsed)
}

func TestAliasCache_resolveAlias_DepthLimit(t *testing.T) {
	cache := NewAliasCache()

	// Create a deep chain (11 levels)
	for i := 0; i < MaxExpansionDepth; i++ {
		from := string(rune('a' + i))
		to := string(rune('a' + i + 1))
		err := cache.SetSystemAlias(from, to)
		require.NoError(t, err)
	}

	// Should stop at depth limit
	result, matched := cache.resolveAlias(ulid.ULID{}, "a")
	assert.True(t, matched)
	// Should have expanded some but stopped at limit
	assert.NotEqual(t, "a", result.resolvedCmd)
}
