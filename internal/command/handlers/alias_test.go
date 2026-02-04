// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
)

// testHandler is a no-op handler for testing purposes.
func testHandler(_ context.Context, _ *command.CommandExecution) error {
	return nil
}

// --- Player Alias Commands Tests ---

func TestAliasAddHandler(t *testing.T) {
	t.Run("adds new alias successfully", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "Alias 'l' added")

		// Verify alias was actually added
		result := cache.Resolve(playerID, "l", nil)
		assert.Equal(t, "look", result.Resolved)
		assert.True(t, result.WasAlias)
	})

	t.Run("warns when shadowing existing command", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		require.NoError(t, registry.Register(command.CommandEntry{
			Name:    "look",
			Handler: testHandler,
			Source:  "core",
		}))
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "look=examine",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Warning")
		assert.Contains(t, output, "'look' is an existing command")
		assert.Contains(t, output, "Your alias will override it")
		assert.Contains(t, output, "Alias 'look' added")
	})

	t.Run("warns when shadowing system alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		require.NoError(t, cache.SetSystemAlias("l", "look"))
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=list",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Warning")
		assert.Contains(t, output, "'l' is a system alias")
		assert.Contains(t, output, "Your alias will take precedence")
		assert.Contains(t, output, "Alias 'l' added")
	})

	t.Run("warns when replacing own existing alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()
		require.NoError(t, cache.SetPlayerAlias(playerID, "l", "look"))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=list",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Warning")
		assert.Contains(t, output, "Replacing existing alias 'l'")
		assert.Contains(t, output, "was: 'look'")
		assert.Contains(t, output, "Alias 'l' added")
	})

	t.Run("rejects circular alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()
		require.NoError(t, cache.SetPlayerAlias(playerID, "a", "b"))
		require.NoError(t, cache.SetPlayerAlias(playerID, "b", "c"))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "c=a",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular reference detected")
	})

	t.Run("rejects invalid alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "123invalid=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("rejects missing equals sign", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "aliasonly",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("rejects empty alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("rejects empty command", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("returns error when alias cache is nil", func(t *testing.T) {
		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    ulid.Make(),
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}), // No AliasCache
		}

		err := AliasAddHandler(context.Background(), exec)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "alias operations require a configured alias cache")
	})
}

func TestAliasRemoveHandler(t *testing.T) {
	t.Run("removes existing alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		playerID := ulid.Make()
		require.NoError(t, cache.SetPlayerAlias(playerID, "l", "look"))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasRemoveImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "Alias 'l' removed")

		// Verify alias was removed
		result := cache.Resolve(playerID, "l", nil)
		assert.False(t, result.WasAlias)
	})

	t.Run("reports when alias doesn't exist", func(t *testing.T) {
		cache := command.NewAliasCache()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "nonexistent",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasRemoveImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "No alias 'nonexistent' found")
	})

	t.Run("rejects empty alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasRemoveImpl(context.Background(), exec, cache)
		require.Error(t, err)
	})
}

func TestAliasListHandler(t *testing.T) {
	t.Run("lists player aliases", func(t *testing.T) {
		cache := command.NewAliasCache()
		playerID := ulid.Make()
		require.NoError(t, cache.SetPlayerAlias(playerID, "l", "look"))
		require.NoError(t, cache.SetPlayerAlias(playerID, "n", "north"))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasListImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Your aliases:")
		assert.Contains(t, output, "l")
		assert.Contains(t, output, "look")
		assert.Contains(t, output, "n")
		assert.Contains(t, output, "north")
	})

	t.Run("shows message when no aliases", func(t *testing.T) {
		cache := command.NewAliasCache()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := aliasListImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "You have no aliases")
	})
}

// --- System Alias Commands Tests ---

func TestSysaliasAddHandler(t *testing.T) {
	t.Run("adds new system alias successfully", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "System alias 'l' added")

		// Verify alias was actually added
		result := cache.Resolve(ulid.ULID{}, "l", nil)
		assert.Equal(t, "look", result.Resolved)
		assert.True(t, result.WasAlias)
	})

	t.Run("warns when shadowing existing command", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		require.NoError(t, registry.Register(command.CommandEntry{
			Name:    "look",
			Handler: testHandler,
			Source:  "core",
		}))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Args:        "look=examine",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "Warning")
		assert.Contains(t, output, "'look' is an existing command")
		assert.Contains(t, output, "System alias 'look' added")
	})

	t.Run("blocks when shadowing existing system alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		require.NoError(t, cache.SetSystemAlias("l", "look"))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Args:        "l=list",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shadows existing system alias")
	})

	t.Run("rejects circular alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		require.NoError(t, cache.SetSystemAlias("a", "b"))
		require.NoError(t, cache.SetSystemAlias("b", "c"))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Args:        "c=a",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular reference detected")
	})

	t.Run("rejects invalid alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Args:        "123invalid=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})
}

func TestSysaliasRemoveHandler(t *testing.T) {
	t.Run("removes existing system alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		require.NoError(t, cache.SetSystemAlias("l", "look"))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Args:        "l",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasRemoveImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "System alias 'l' removed")

		// Verify alias was removed
		result := cache.Resolve(ulid.ULID{}, "l", nil)
		assert.False(t, result.WasAlias)
	})

	t.Run("reports when system alias doesn't exist", func(t *testing.T) {
		cache := command.NewAliasCache()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Args:        "nonexistent",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasRemoveImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "No system alias 'nonexistent' found")
	})
}

func TestSysaliasListHandler(t *testing.T) {
	t.Run("lists system aliases", func(t *testing.T) {
		cache := command.NewAliasCache()
		require.NoError(t, cache.SetSystemAlias("l", "look"))
		require.NoError(t, cache.SetSystemAlias("n", "north"))

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasListImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		output := buf.String()
		assert.Contains(t, output, "System aliases:")
		assert.Contains(t, output, "l")
		assert.Contains(t, output, "look")
		assert.Contains(t, output, "n")
		assert.Contains(t, output, "north")
	})

	t.Run("shows message when no system aliases", func(t *testing.T) {
		cache := command.NewAliasCache()

		var buf bytes.Buffer
		exec := &command.CommandExecution{
			CharacterID: ulid.Make(),
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		}

		err := sysaliasListImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "No system aliases defined")
	})
}
