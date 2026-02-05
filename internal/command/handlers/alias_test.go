// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/mocks"
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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		require.NoError(t, registry.Register(command.NewTestEntry(command.CommandEntryConfig{
			Name:    "look",
			Handler: testHandler,
			Source:  "core",
		})))
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "look=examine",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=list",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=list",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "c=a",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular reference detected")
	})

	t.Run("rejects invalid alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "123invalid=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("rejects missing equals sign", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "aliasonly",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("rejects empty alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("rejects empty command", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("trims whitespace around alias and command", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "  l = look  ",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasCache: cache}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)
		// Verify the alias was stored without whitespace
		result := cache.Resolve(playerID, "l", registry)
		assert.Equal(t, "look", result.Resolved)
		assert.True(t, result.WasAlias)
	})

	t.Run("rejects whitespace-only alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "   =look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("rejects whitespace-only command", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=   ",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("rejects whitespace-only input", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "   ",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})

	t.Run("returns error when alias cache is nil", func(t *testing.T) {
		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    ulid.Make(),
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}), // No AliasCache
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "nonexistent",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := aliasRemoveImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "No alias 'nonexistent' found")
	})

	t.Run("rejects empty alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		require.NoError(t, registry.Register(command.NewTestEntry(command.CommandEntryConfig{
			Name:    "look",
			Handler: testHandler,
			Source:  "core",
		})))

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "look=examine",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "l=list",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "c=a",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular reference detected")
	})

	t.Run("rejects invalid alias name", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "123invalid=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
	})
}

func TestSysaliasRemoveHandler(t *testing.T) {
	t.Run("removes existing system alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		require.NoError(t, cache.SetSystemAlias("l", "look"))

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "l",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "nonexistent",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

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
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}),
		})

		err := sysaliasListImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		assert.Contains(t, buf.String(), "No system aliases defined")
	})
}

// --- Persistence Tests ---

func TestAliasAddHandler_PersistsToDatabase(t *testing.T) {
	t.Run("persists player alias to database before updating cache", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		mockRepo := mocks.NewMockAliasWriter(t)
		playerID := ulid.Make()

		// Expect database write with correct parameters
		mockRepo.EXPECT().
			SetPlayerAlias(mock.Anything, playerID, "l", "look").
			Return(nil)

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		// Verify alias is in cache
		result := cache.Resolve(playerID, "l", nil)
		assert.Equal(t, "look", result.Resolved)
		assert.True(t, result.WasAlias)
	})

	t.Run("does not update cache if database write fails", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		mockRepo := mocks.NewMockAliasWriter(t)
		playerID := ulid.Make()

		// Simulate database failure
		mockRepo.EXPECT().
			SetPlayerAlias(mock.Anything, playerID, "l", "look").
			Return(errors.New("database connection failed"))

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database connection failed")

		// Verify alias was NOT added to cache
		_, exists := cache.GetPlayerAlias(playerID, "l")
		assert.False(t, exists, "alias should not be in cache when database write fails")
	})

	t.Run("works without repository (cache-only mode)", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		playerID := ulid.Make()

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{}), // No AliasRepo
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		// Alias should still be added to cache
		result := cache.Resolve(playerID, "l", nil)
		assert.Equal(t, "look", result.Resolved)
	})

	t.Run("rolls back database write if cache update fails", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		mockRepo := mocks.NewMockAliasWriter(t)
		playerID := ulid.Make()

		// Create a circular alias scenario: add "look" -> "l" first
		// Then when we try to add "l" -> "look", the cache will detect the cycle
		require.NoError(t, cache.SetPlayerAlias(playerID, "look", "l"))

		// Expect database write to succeed
		mockRepo.EXPECT().
			SetPlayerAlias(mock.Anything, playerID, "l", "look").
			Return(nil)

		// Expect rollback (delete) when cache fails due to circular reference
		mockRepo.EXPECT().
			DeletePlayerAlias(mock.Anything, playerID, "l").
			Return(nil)

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular")

		// Verify: database write was rolled back (DeletePlayerAlias was called)
		// The mock expectations above verify this automatically
	})
}

func TestAliasRemoveHandler_PersistsToDatabase(t *testing.T) {
	t.Run("removes player alias from database before updating cache", func(t *testing.T) {
		cache := command.NewAliasCache()
		mockRepo := mocks.NewMockAliasWriter(t)
		playerID := ulid.Make()

		// Pre-populate cache
		require.NoError(t, cache.SetPlayerAlias(playerID, "l", "look"))

		// Expect database delete with correct parameters
		mockRepo.EXPECT().
			DeletePlayerAlias(mock.Anything, playerID, "l").
			Return(nil)

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := aliasRemoveImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		// Verify alias is removed from cache
		_, exists := cache.GetPlayerAlias(playerID, "l")
		assert.False(t, exists)
	})

	t.Run("does not update cache if database delete fails", func(t *testing.T) {
		cache := command.NewAliasCache()
		mockRepo := mocks.NewMockAliasWriter(t)
		playerID := ulid.Make()

		// Pre-populate cache
		require.NoError(t, cache.SetPlayerAlias(playerID, "l", "look"))

		// Simulate database failure
		mockRepo.EXPECT().
			DeletePlayerAlias(mock.Anything, playerID, "l").
			Return(errors.New("database connection failed"))

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := aliasRemoveImpl(context.Background(), exec, cache)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database connection failed")

		// Verify alias is still in cache
		cmd, exists := cache.GetPlayerAlias(playerID, "l")
		assert.True(t, exists, "alias should still be in cache when database delete fails")
		assert.Equal(t, "look", cmd)
	})
}

func TestSysaliasAddHandler_PersistsToDatabase(t *testing.T) {
	t.Run("persists system alias to database before updating cache", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		mockRepo := mocks.NewMockAliasWriter(t)
		charID := ulid.Make()

		// Expect database write with correct parameters (createdBy is character ID string)
		mockRepo.EXPECT().
			SetSystemAlias(mock.Anything, "l", "look", charID.String()).
			Return(nil)

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: charID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.NoError(t, err)

		// Verify alias is in cache
		result := cache.Resolve(ulid.ULID{}, "l", nil)
		assert.Equal(t, "look", result.Resolved)
		assert.True(t, result.WasAlias)
	})

	t.Run("does not update cache if database write fails", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		mockRepo := mocks.NewMockAliasWriter(t)
		charID := ulid.Make()

		// Simulate database failure
		mockRepo.EXPECT().
			SetSystemAlias(mock.Anything, "l", "look", charID.String()).
			Return(errors.New("database connection failed"))

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: charID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database connection failed")

		// Verify alias was NOT added to cache
		_, exists := cache.GetSystemAlias("l")
		assert.False(t, exists, "alias should not be in cache when database write fails")
	})

	t.Run("rolls back database write if cache update fails", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		mockRepo := mocks.NewMockAliasWriter(t)
		charID := ulid.Make()

		// Create a circular alias scenario: add "look" -> "l" first
		// Then when we try to add "l" -> "look", the cache will detect the cycle
		require.NoError(t, cache.SetSystemAlias("look", "l"))

		// Expect database write to succeed
		mockRepo.EXPECT().
			SetSystemAlias(mock.Anything, "l", "look", charID.String()).
			Return(nil)

		// Expect rollback (delete) when cache fails due to circular reference
		mockRepo.EXPECT().
			DeleteSystemAlias(mock.Anything, "l").
			Return(nil)

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: charID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular")

		// Verify: database write was rolled back (DeleteSystemAlias was called)
		// The mock expectations above verify this automatically
	})
}

func TestAliasAddHandler_DoubleFailure(t *testing.T) {
	t.Run("logs critical when both cache update and rollback fail for player alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		mockRepo := mocks.NewMockAliasWriter(t)
		playerID := ulid.Make()

		// Create a circular alias scenario: add "look" -> "l" first
		// Then when we try to add "l" -> "look", the cache will detect the cycle
		require.NoError(t, cache.SetPlayerAlias(playerID, "look", "l"))

		// Expect database write to succeed
		mockRepo.EXPECT().
			SetPlayerAlias(mock.Anything, playerID, "l", "look").
			Return(nil)

		// Simulate rollback failure - this creates the double-failure scenario
		mockRepo.EXPECT().
			DeletePlayerAlias(mock.Anything, playerID, "l").
			Return(errors.New("rollback failed: database connection lost"))

		// Capture metrics before the call
		beforeCount := testutil.ToFloat64(command.AliasRollbackFailures)

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			PlayerID:    playerID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := aliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular")

		// Verify metric was incremented
		afterCount := testutil.ToFloat64(command.AliasRollbackFailures)
		assert.Equal(t, beforeCount+1, afterCount, "rollback failure metric should increment")
	})
}

func TestSysaliasAddHandler_DoubleFailure(t *testing.T) {
	t.Run("logs critical when both cache update and rollback fail for system alias", func(t *testing.T) {
		cache := command.NewAliasCache()
		registry := command.NewRegistry()
		mockRepo := mocks.NewMockAliasWriter(t)
		charID := ulid.Make()

		// Create a circular alias scenario: add "look" -> "l" first
		// Then when we try to add "l" -> "look", the cache will detect the cycle
		require.NoError(t, cache.SetSystemAlias("look", "l"))

		// Expect database write to succeed
		mockRepo.EXPECT().
			SetSystemAlias(mock.Anything, "l", "look", charID.String()).
			Return(nil)

		// Simulate rollback failure - this creates the double-failure scenario
		mockRepo.EXPECT().
			DeleteSystemAlias(mock.Anything, "l").
			Return(errors.New("rollback failed: database connection lost"))

		// Capture metrics before the call
		beforeCount := testutil.ToFloat64(command.AliasRollbackFailures)

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: charID,
			Args:        "l=look",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := sysaliasAddImpl(context.Background(), exec, cache, registry)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular")

		// Verify metric was incremented
		afterCount := testutil.ToFloat64(command.AliasRollbackFailures)
		assert.Equal(t, beforeCount+1, afterCount, "rollback failure metric should increment")
	})
}

func TestSysaliasRemoveHandler_PersistsToDatabase(t *testing.T) {
	t.Run("removes system alias from database before updating cache", func(t *testing.T) {
		cache := command.NewAliasCache()
		mockRepo := mocks.NewMockAliasWriter(t)

		// Pre-populate cache
		require.NoError(t, cache.SetSystemAlias("l", "look"))

		// Expect database delete with correct parameters
		mockRepo.EXPECT().
			DeleteSystemAlias(mock.Anything, "l").
			Return(nil)

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "l",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := sysaliasRemoveImpl(context.Background(), exec, cache)
		require.NoError(t, err)

		// Verify alias is removed from cache
		_, exists := cache.GetSystemAlias("l")
		assert.False(t, exists)
	})

	t.Run("does not update cache if database delete fails", func(t *testing.T) {
		cache := command.NewAliasCache()
		mockRepo := mocks.NewMockAliasWriter(t)

		// Pre-populate cache
		require.NoError(t, cache.SetSystemAlias("l", "look"))

		// Simulate database failure
		mockRepo.EXPECT().
			DeleteSystemAlias(mock.Anything, "l").
			Return(errors.New("database connection failed"))

		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: ulid.Make(),
			Args:        "l",
			Output:      &buf,
			Services:    command.NewTestServices(command.ServicesConfig{AliasRepo: mockRepo}),
		})

		err := sysaliasRemoveImpl(context.Background(), exec, cache)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "database connection failed")

		// Verify alias is still in cache
		cmd, exists := cache.GetSystemAlias("l")
		assert.True(t, exists, "alias should still be in cache when database delete fails")
		assert.Equal(t, "look", cmd)
	})
}
