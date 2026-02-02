// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAliasCache(t *testing.T) {
	cache := NewAliasCache()

	assert.NotNil(t, cache)
	assert.NotNil(t, cache.playerAliases)
	assert.NotNil(t, cache.systemAliases)
}

func TestAliasCache_LoadSystemAliases(t *testing.T) {
	cache := NewAliasCache()

	aliases := map[string]string{
		"l":  "look",
		"q":  "quit",
		"n":  "north",
		"s":  "south",
	}

	cache.LoadSystemAliases(aliases)

	// Verify all aliases were loaded
	for alias, cmd := range aliases {
		resolved, wasAlias := cache.Resolve(ulid.ULID{}, alias, nil)
		assert.Equal(t, cmd, resolved)
		assert.True(t, wasAlias)
	}
}

func TestAliasCache_LoadPlayerAliases(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	aliases := map[string]string{
		"attack": "combat attack",
		"heal":   "cast heal",
	}

	cache.LoadPlayerAliases(playerID, aliases)

	// Verify aliases work for this player
	for alias, cmd := range aliases {
		resolved, wasAlias := cache.Resolve(playerID, alias, nil)
		assert.Equal(t, cmd, resolved)
		assert.True(t, wasAlias)
	}

	// Verify aliases don't work for other players
	otherPlayer := ulid.MustNew(2, nil)
	resolved, wasAlias := cache.Resolve(otherPlayer, "attack", nil)
	assert.Equal(t, "attack", resolved)
	assert.False(t, wasAlias)
}

func TestAliasCache_SetSystemAlias(t *testing.T) {
	cache := NewAliasCache()

	cache.SetSystemAlias("w", "west")

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "w", nil)
	assert.Equal(t, "west", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_SetSystemAlias_Update(t *testing.T) {
	cache := NewAliasCache()

	cache.SetSystemAlias("w", "west")
	cache.SetSystemAlias("w", "whisper")

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "w", nil)
	assert.Equal(t, "whisper", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_SetPlayerAlias(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	cache.SetPlayerAlias(playerID, "aa", "attack all")

	resolved, wasAlias := cache.Resolve(playerID, "aa", nil)
	assert.Equal(t, "attack all", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_SetPlayerAlias_Update(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	cache.SetPlayerAlias(playerID, "aa", "attack all")
	cache.SetPlayerAlias(playerID, "aa", "attack ally")

	resolved, wasAlias := cache.Resolve(playerID, "aa", nil)
	assert.Equal(t, "attack ally", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_RemoveSystemAlias(t *testing.T) {
	cache := NewAliasCache()

	cache.SetSystemAlias("w", "west")
	cache.RemoveSystemAlias("w")

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "w", nil)
	assert.Equal(t, "w", resolved)
	assert.False(t, wasAlias)
}

func TestAliasCache_RemoveSystemAlias_NonExistent(t *testing.T) {
	cache := NewAliasCache()

	// Should not panic
	cache.RemoveSystemAlias("nonexistent")

	// Verify nothing changed
	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "nonexistent", nil)
	assert.Equal(t, "nonexistent", resolved)
	assert.False(t, wasAlias)
}

func TestAliasCache_RemovePlayerAlias(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	cache.SetPlayerAlias(playerID, "aa", "attack all")
	cache.RemovePlayerAlias(playerID, "aa")

	resolved, wasAlias := cache.Resolve(playerID, "aa", nil)
	assert.Equal(t, "aa", resolved)
	assert.False(t, wasAlias)
}

func TestAliasCache_RemovePlayerAlias_NonExistent(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Should not panic
	cache.RemovePlayerAlias(playerID, "nonexistent")

	// Verify nothing changed
	resolved, wasAlias := cache.Resolve(playerID, "nonexistent", nil)
	assert.Equal(t, "nonexistent", resolved)
	assert.False(t, wasAlias)
}

func TestAliasCache_ClearPlayer(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	cache.SetPlayerAlias(playerID, "aa", "attack all")
	cache.SetPlayerAlias(playerID, "bb", "bash barrier")
	cache.ClearPlayer(playerID)

	resolved1, wasAlias1 := cache.Resolve(playerID, "aa", nil)
	assert.Equal(t, "aa", resolved1)
	assert.False(t, wasAlias1)

	resolved2, wasAlias2 := cache.Resolve(playerID, "bb", nil)
	assert.Equal(t, "bb", resolved2)
	assert.False(t, wasAlias2)
}

func TestAliasCache_ClearPlayer_NonExistent(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Should not panic
	cache.ClearPlayer(playerID)

	// Verify cache is still functional
	cache.SetPlayerAlias(playerID, "test", "look")
	resolved, wasAlias := cache.Resolve(playerID, "test", nil)
	assert.Equal(t, "look", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_Resolve_RegisteredCommand(t *testing.T) {
	cache := NewAliasCache()
	registry := NewRegistry()

	// Register "look" as a command
	err := registry.Register(CommandEntry{
		Name:   "look",
		Source: "core",
	})
	require.NoError(t, err)

	// Also set "look" as an alias (should be ignored)
	cache.SetSystemAlias("look", "something else")

	// Exact match should return unchanged
	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "look", registry)
	assert.Equal(t, "look", resolved)
	assert.False(t, wasAlias)
}

func TestAliasCache_Resolve_PlayerAliasExpandsCommand(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	cache.SetPlayerAlias(playerID, "l", "look")

	resolved, wasAlias := cache.Resolve(playerID, "l", nil)
	assert.Equal(t, "look", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_Resolve_SystemAliasExpandsCommand(t *testing.T) {
	cache := NewAliasCache()

	cache.SetSystemAlias("l", "look")

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "l", nil)
	assert.Equal(t, "look", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_Resolve_PlayerAliasOverridesSystem(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	cache.SetSystemAlias("l", "look")
	cache.SetPlayerAlias(playerID, "l", "list")

	// Player alias takes precedence
	resolved, wasAlias := cache.Resolve(playerID, "l", nil)
	assert.Equal(t, "list", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_Resolve_NoMatch(t *testing.T) {
	cache := NewAliasCache()

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "unknown", nil)
	assert.Equal(t, "unknown", resolved)
	assert.False(t, wasAlias)
}

func TestAliasCache_Resolve_ExpansionDepthLimit(t *testing.T) {
	cache := NewAliasCache()

	// Create circular alias chain
	cache.SetSystemAlias("a", "b")
	cache.SetSystemAlias("b", "c")
	cache.SetSystemAlias("c", "d")
	cache.SetSystemAlias("d", "e")
	cache.SetSystemAlias("e", "f")
	cache.SetSystemAlias("f", "g")
	cache.SetSystemAlias("g", "h")
	cache.SetSystemAlias("h", "i")
	cache.SetSystemAlias("i", "j")
	cache.SetSystemAlias("j", "k")
	cache.SetSystemAlias("k", "l") // 11th level - should stop before this

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "a", nil)

	// Should stop at MaxExpansionDepth=10
	assert.True(t, wasAlias)
	assert.NotEqual(t, "a", resolved) // Should have expanded at least some
}

func TestAliasCache_Resolve_CircularAlias(t *testing.T) {
	cache := NewAliasCache()

	// Create true circular reference
	cache.SetSystemAlias("a", "b")
	cache.SetSystemAlias("b", "c")
	cache.SetSystemAlias("c", "a")

	_, wasAlias := cache.Resolve(ulid.ULID{}, "a", nil)

	// Should stop at MaxExpansionDepth without panic/hang
	assert.True(t, wasAlias)
	// The exact resolved value depends on when depth limit is hit
}

func TestAliasCache_ConcurrentAccess(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	// Concurrent reads and writes
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				switch n % 5 {
				case 0:
					cache.SetSystemAlias("test", "look")
				case 1:
					cache.SetPlayerAlias(playerID, "test", "look")
				case 2:
					cache.RemoveSystemAlias("test")
				case 3:
					cache.RemovePlayerAlias(playerID, "test")
				case 4:
					cache.Resolve(playerID, "test", nil)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify cache is still functional after concurrent access
	cache.SetSystemAlias("verify", "check")
	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "verify", nil)
	assert.Equal(t, "check", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_Resolve_PreservesArgs(t *testing.T) {
	cache := NewAliasCache()

	cache.SetSystemAlias("l", "look")

	// Input with arguments should preserve them
	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "l here", nil)
	assert.Equal(t, "look here", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_Resolve_MultiWordAlias(t *testing.T) {
	cache := NewAliasCache()

	// Alias only matches first word
	cache.SetSystemAlias("look", "examine")

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "look room", nil)
	assert.Equal(t, "examine room", resolved)
	assert.True(t, wasAlias)
}

func TestAliasCache_Resolve_EmptyInput(t *testing.T) {
	cache := NewAliasCache()

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "", nil)
	assert.Equal(t, "", resolved)
	assert.False(t, wasAlias)
}

func TestAliasCache_Resolve_WhitespaceOnly(t *testing.T) {
	cache := NewAliasCache()

	resolved, wasAlias := cache.Resolve(ulid.ULID{}, "   ", nil)
	assert.Equal(t, "   ", resolved)
	assert.False(t, wasAlias)
}

func BenchmarkAliasCache_Resolve(b *testing.B) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)
	registry := NewRegistry()

	// Set up some aliases
	cache.LoadSystemAliases(map[string]string{
		"l": "look",
		"n": "north",
		"s": "south",
		"e": "east",
		"w": "west",
	})
	cache.SetPlayerAlias(playerID, "aa", "attack all")

	// Register a command
	_ = registry.Register(CommandEntry{Name: "look", Source: "core"})

	b.Run("system_alias", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Resolve(playerID, "n", registry)
		}
	})

	b.Run("player_alias", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Resolve(playerID, "aa", registry)
		}
	})

	b.Run("registered_command", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Resolve(playerID, "look", registry)
		}
	})

	b.Run("no_match", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			cache.Resolve(playerID, "unknown", registry)
		}
	})
}
