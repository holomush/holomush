// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"context"
	"sync"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testHandler is a no-op handler for tests.
func testHandler(_ context.Context, _ *CommandExecution) error {
	return nil
}

func TestNewAliasCache(t *testing.T) {
	cache := NewAliasCache()

	assert.NotNil(t, cache)
	assert.NotNil(t, cache.playerAliases)
	assert.NotNil(t, cache.systemAliases)
}

func TestAliasCache_LoadSystemAliases(t *testing.T) {
	cache := NewAliasCache()

	aliases := map[string]string{
		"l": "look",
		"q": "quit",
		"n": "north",
		"s": "south",
	}

	cache.LoadSystemAliases(aliases)

	// Verify all aliases were loaded
	for alias, cmd := range aliases {
		result := cache.Resolve(ulid.ULID{}, alias, nil)
		assert.Equal(t, cmd, result.Resolved)
		assert.True(t, result.WasAlias)
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
		result := cache.Resolve(playerID, alias, nil)
		assert.Equal(t, cmd, result.Resolved)
		assert.True(t, result.WasAlias)
	}

	// Verify aliases don't work for other players
	otherPlayer := ulid.MustNew(2, nil)
	result := cache.Resolve(otherPlayer, "attack", nil)
	assert.Equal(t, "attack", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_SetSystemAlias(t *testing.T) {
	cache := NewAliasCache()

	err := cache.SetSystemAlias("w", "west")
	require.NoError(t, err)

	result := cache.Resolve(ulid.ULID{}, "w", nil)
	assert.Equal(t, "west", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_SetSystemAlias_Update(t *testing.T) {
	cache := NewAliasCache()

	err := cache.SetSystemAlias("w", "west")
	require.NoError(t, err)
	err = cache.SetSystemAlias("w", "whisper")
	require.NoError(t, err)

	result := cache.Resolve(ulid.ULID{}, "w", nil)
	assert.Equal(t, "whisper", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_SetPlayerAlias(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	err := cache.SetPlayerAlias(playerID, "aa", "attack all")
	require.NoError(t, err)

	result := cache.Resolve(playerID, "aa", nil)
	assert.Equal(t, "attack all", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_SetPlayerAlias_Update(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	err := cache.SetPlayerAlias(playerID, "aa", "attack all")
	require.NoError(t, err)
	err = cache.SetPlayerAlias(playerID, "aa", "attack ally")
	require.NoError(t, err)

	result := cache.Resolve(playerID, "aa", nil)
	assert.Equal(t, "attack ally", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_RemoveSystemAlias(t *testing.T) {
	cache := NewAliasCache()

	err := cache.SetSystemAlias("w", "west")
	require.NoError(t, err)
	cache.RemoveSystemAlias("w")

	result := cache.Resolve(ulid.ULID{}, "w", nil)
	assert.Equal(t, "w", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_RemoveSystemAlias_NonExistent(t *testing.T) {
	cache := NewAliasCache()

	// Should not panic
	cache.RemoveSystemAlias("nonexistent")

	// Verify nothing changed
	result := cache.Resolve(ulid.ULID{}, "nonexistent", nil)
	assert.Equal(t, "nonexistent", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_RemovePlayerAlias(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	err := cache.SetPlayerAlias(playerID, "aa", "attack all")
	require.NoError(t, err)
	cache.RemovePlayerAlias(playerID, "aa")

	result := cache.Resolve(playerID, "aa", nil)
	assert.Equal(t, "aa", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_RemovePlayerAlias_NonExistent(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Should not panic
	cache.RemovePlayerAlias(playerID, "nonexistent")

	// Verify nothing changed
	result := cache.Resolve(playerID, "nonexistent", nil)
	assert.Equal(t, "nonexistent", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_ClearPlayer(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	err := cache.SetPlayerAlias(playerID, "aa", "attack all")
	require.NoError(t, err)
	err = cache.SetPlayerAlias(playerID, "bb", "bash barrier")
	require.NoError(t, err)
	cache.ClearPlayer(playerID)

	result1 := cache.Resolve(playerID, "aa", nil)
	assert.Equal(t, "aa", result1.Resolved)
	assert.False(t, result1.WasAlias)

	result2 := cache.Resolve(playerID, "bb", nil)
	assert.Equal(t, "bb", result2.Resolved)
	assert.False(t, result2.WasAlias)
}

func TestAliasCache_ClearPlayer_NonExistent(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Should not panic
	cache.ClearPlayer(playerID)

	// Verify cache is still functional
	err := cache.SetPlayerAlias(playerID, "test", "look")
	require.NoError(t, err)
	result := cache.Resolve(playerID, "test", nil)
	assert.Equal(t, "look", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_Resolve_RegisteredCommand(t *testing.T) {
	cache := NewAliasCache()
	registry := NewRegistry()

	// Register "look" as a command
	err := registry.Register(CommandEntry{
		Name:    "look",
		Handler: testHandler,
		Source:  "core",
	})
	require.NoError(t, err)

	// Also set "look" as an alias (should be ignored)
	err = cache.SetSystemAlias("look", "something else")
	require.NoError(t, err)

	// Exact match should return unchanged
	result := cache.Resolve(ulid.ULID{}, "look", registry)
	assert.Equal(t, "look", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_Resolve_PlayerAliasExpandsCommand(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	err := cache.SetPlayerAlias(playerID, "l", "look")
	require.NoError(t, err)

	result := cache.Resolve(playerID, "l", nil)
	assert.Equal(t, "look", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_Resolve_SystemAliasExpandsCommand(t *testing.T) {
	cache := NewAliasCache()

	err := cache.SetSystemAlias("l", "look")
	require.NoError(t, err)

	result := cache.Resolve(ulid.ULID{}, "l", nil)
	assert.Equal(t, "look", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_Resolve_PlayerAliasOverridesSystem(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	err := cache.SetSystemAlias("l", "look")
	require.NoError(t, err)
	err = cache.SetPlayerAlias(playerID, "l", "list")
	require.NoError(t, err)

	// Player alias takes precedence
	result := cache.Resolve(playerID, "l", nil)
	assert.Equal(t, "list", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_Resolve_NoMatch(t *testing.T) {
	cache := NewAliasCache()

	result := cache.Resolve(ulid.ULID{}, "unknown", nil)
	assert.Equal(t, "unknown", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_Resolve_ExpansionDepthLimit(t *testing.T) {
	cache := NewAliasCache()

	// Create a long alias chain (each link points to the next, which doesn't exist yet)
	// This is NOT circular, just a long chain
	require.NoError(t, cache.SetSystemAlias("a", "b"))
	require.NoError(t, cache.SetSystemAlias("b", "c"))
	require.NoError(t, cache.SetSystemAlias("c", "d"))
	require.NoError(t, cache.SetSystemAlias("d", "e"))
	require.NoError(t, cache.SetSystemAlias("e", "f"))
	require.NoError(t, cache.SetSystemAlias("f", "g"))
	require.NoError(t, cache.SetSystemAlias("g", "h"))
	require.NoError(t, cache.SetSystemAlias("h", "i"))
	require.NoError(t, cache.SetSystemAlias("i", "j"))
	require.NoError(t, cache.SetSystemAlias("j", "k"))
	require.NoError(t, cache.SetSystemAlias("k", "l")) // 11th level - should stop before this

	result := cache.Resolve(ulid.ULID{}, "a", nil)

	// Should stop at MaxExpansionDepth=10
	assert.True(t, result.WasAlias)
	assert.NotEqual(t, "a", result.Resolved) // Should have expanded at least some
}

func TestAliasCache_SetSystemAlias_RejectsCircular(t *testing.T) {
	cache := NewAliasCache()

	// Create aliases that will form a cycle when the last one is added
	require.NoError(t, cache.SetSystemAlias("a", "b"))
	require.NoError(t, cache.SetSystemAlias("b", "c"))

	// This should fail - it would create a→b→c→a cycle
	err := cache.SetSystemAlias("c", "a")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference detected")

	// Verify the alias was NOT added
	result := cache.Resolve(ulid.ULID{}, "c", nil)
	assert.Equal(t, "c", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_SetPlayerAlias_RejectsCircular(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.MustNew(1, nil)

	// Create aliases that will form a cycle
	require.NoError(t, cache.SetPlayerAlias(playerID, "x", "y"))
	require.NoError(t, cache.SetPlayerAlias(playerID, "y", "z"))

	// This should fail - it would create x→y→z→x cycle
	err := cache.SetPlayerAlias(playerID, "z", "x")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference detected")

	// Verify the alias was NOT added
	result := cache.Resolve(playerID, "z", nil)
	assert.Equal(t, "z", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_SetSystemAlias_AllowsSelfReference(t *testing.T) {
	cache := NewAliasCache()

	// Self-reference creates an immediate cycle
	err := cache.SetSystemAlias("loop", "loop")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference detected")
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
					_ = cache.SetSystemAlias("test", "look") // Error OK in concurrent test
				case 1:
					_ = cache.SetPlayerAlias(playerID, "test", "look") // Error OK in concurrent test
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
	err := cache.SetSystemAlias("verify", "check")
	require.NoError(t, err)
	result := cache.Resolve(ulid.ULID{}, "verify", nil)
	assert.Equal(t, "check", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_Resolve_PreservesArgs(t *testing.T) {
	cache := NewAliasCache()

	err := cache.SetSystemAlias("l", "look")
	require.NoError(t, err)

	// Input with arguments should preserve them
	result := cache.Resolve(ulid.ULID{}, "l here", nil)
	assert.Equal(t, "look here", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_Resolve_MultiWordAlias(t *testing.T) {
	cache := NewAliasCache()

	// Alias only matches first word
	err := cache.SetSystemAlias("look", "examine")
	require.NoError(t, err)

	result := cache.Resolve(ulid.ULID{}, "look room", nil)
	assert.Equal(t, "examine room", result.Resolved)
	assert.True(t, result.WasAlias)
}

func TestAliasCache_Resolve_EmptyInput(t *testing.T) {
	cache := NewAliasCache()

	result := cache.Resolve(ulid.ULID{}, "", nil)
	assert.Equal(t, "", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_Resolve_WhitespaceOnly(t *testing.T) {
	cache := NewAliasCache()

	result := cache.Resolve(ulid.ULID{}, "   ", nil)
	assert.Equal(t, "   ", result.Resolved)
	assert.False(t, result.WasAlias)
}

func TestAliasCache_Resolve_PrefixAlias(t *testing.T) {
	cache := NewAliasCache()
	playerID := ulid.Make()

	// Set up prefix aliases for poses
	cache.LoadSystemAliases(map[string]string{
		":": "pose",
		";": "pose",
	})

	t.Run("colon prefix expands to pose", func(t *testing.T) {
		result := cache.Resolve(playerID, ":waves hello", nil)
		assert.Equal(t, "pose waves hello", result.Resolved)
		assert.True(t, result.WasAlias)
		assert.Equal(t, ":", result.AliasUsed)
	})

	t.Run("semicolon prefix for possessives", func(t *testing.T) {
		result := cache.Resolve(playerID, ";'s eyes widen", nil)
		assert.Equal(t, "pose 's eyes widen", result.Resolved)
		assert.True(t, result.WasAlias)
		assert.Equal(t, ";", result.AliasUsed)
	})

	t.Run("prefix with separate args", func(t *testing.T) {
		result := cache.Resolve(playerID, ":nods slowly", nil)
		assert.Equal(t, "pose nods slowly", result.Resolved)
		assert.True(t, result.WasAlias)
	})

	t.Run("prefix only returns pose only", func(t *testing.T) {
		result := cache.Resolve(playerID, ":", nil)
		// Single character with no following text is just the alias itself
		assert.Equal(t, "pose", result.Resolved)
		assert.True(t, result.WasAlias)
	})

	// Note: The test case "prefix alias not matched when command exists" was removed
	// because command names starting with special characters (like ":debug") are now
	// invalid per ValidateCommandName. Prefix aliases like ":" will always expand
	// since no valid command can start with a non-letter character.
}

func TestAliasCache_ChainedExpansionPreservesArgs(t *testing.T) {
	tests := []struct {
		name           string
		setupAliases   func(c *AliasCache)
		input          string
		expectedResult string
		expectedAlias  string
	}{
		{
			name: "2-level chain with args at each level",
			setupAliases: func(c *AliasCache) {
				// a → "b extra"
				// b → "final"
				// "a myarg" should become "final extra myarg"
				require.NoError(t, c.SetSystemAlias("a", "b extra"))
				require.NoError(t, c.SetSystemAlias("b", "final"))
			},
			input:          "a myarg",
			expectedResult: "final extra myarg",
			expectedAlias:  "a",
		},
		{
			name: "3-level chain preserves all args",
			setupAliases: func(c *AliasCache) {
				// x → "y arg1"
				// y → "z arg2"
				// z → "done"
				// "x userarg" should become "done arg2 arg1 userarg"
				require.NoError(t, c.SetSystemAlias("x", "y arg1"))
				require.NoError(t, c.SetSystemAlias("y", "z arg2"))
				require.NoError(t, c.SetSystemAlias("z", "done"))
			},
			input:          "x userarg",
			expectedResult: "done arg2 arg1 userarg",
			expectedAlias:  "x",
		},
		{
			name: "2-level chain no user args",
			setupAliases: func(c *AliasCache) {
				require.NoError(t, c.SetSystemAlias("short", "medium foo"))
				require.NoError(t, c.SetSystemAlias("medium", "long"))
			},
			input:          "short",
			expectedResult: "long foo",
			expectedAlias:  "short",
		},
		{
			name: "chain with multiple args per level",
			setupAliases: func(c *AliasCache) {
				// cmd1 → "cmd2 a b"
				// cmd2 → "final"
				// "cmd1 c d" should become "final a b c d"
				require.NoError(t, c.SetSystemAlias("cmd1", "cmd2 a b"))
				require.NoError(t, c.SetSystemAlias("cmd2", "final"))
			},
			input:          "cmd1 c d",
			expectedResult: "final a b c d",
			expectedAlias:  "cmd1",
		},
		{
			name: "player alias chain with args",
			setupAliases: func(c *AliasCache) {
				playerID := ulid.MustNew(1, nil)
				require.NoError(t, c.SetPlayerAlias(playerID, "pa", "pb arg1"))
				require.NoError(t, c.SetPlayerAlias(playerID, "pb", "target"))
			},
			input:          "pa myarg",
			expectedResult: "target arg1 myarg",
			expectedAlias:  "pa",
		},
		{
			name: "mixed player and system chain",
			setupAliases: func(c *AliasCache) {
				playerID := ulid.MustNew(1, nil)
				// Player alias points to system alias
				require.NoError(t, c.SetPlayerAlias(playerID, "player", "sys extra"))
				require.NoError(t, c.SetSystemAlias("sys", "final"))
			},
			input:          "player userarg",
			expectedResult: "final extra userarg",
			expectedAlias:  "player",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewAliasCache()
			tt.setupAliases(cache)

			// Use playerID 1 for player alias tests, zero for system-only
			playerID := ulid.MustNew(1, nil)
			result := cache.Resolve(playerID, tt.input, nil)

			assert.Equal(t, tt.expectedResult, result.Resolved)
			assert.True(t, result.WasAlias)
			assert.Equal(t, tt.expectedAlias, result.AliasUsed)
		})
	}
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
	_ = cache.SetPlayerAlias(playerID, "aa", "attack all")

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
