// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package command_test

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access/accesstest"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/command/handlers"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// inMemoryAliasRepo is a simple in-memory implementation of command.AliasWriter for testing.
// It simulates database persistence without requiring PostgreSQL.
type inMemoryAliasRepo struct {
	mu            sync.RWMutex
	systemAliases map[string]aliasEntry
	playerAliases map[ulid.ULID]map[string]aliasEntry
}

type aliasEntry struct {
	command   string
	createdBy string // only for system aliases
}

func newInMemoryAliasRepo() *inMemoryAliasRepo {
	return &inMemoryAliasRepo{
		systemAliases: make(map[string]aliasEntry),
		playerAliases: make(map[ulid.ULID]map[string]aliasEntry),
	}
}

func (r *inMemoryAliasRepo) SetSystemAlias(_ context.Context, alias, cmd, createdBy string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.systemAliases[alias] = aliasEntry{command: cmd, createdBy: createdBy}
	return nil
}

func (r *inMemoryAliasRepo) DeleteSystemAlias(_ context.Context, alias string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.systemAliases, alias)
	return nil
}

func (r *inMemoryAliasRepo) SetPlayerAlias(_ context.Context, playerID ulid.ULID, alias, cmd string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.playerAliases[playerID] == nil {
		r.playerAliases[playerID] = make(map[string]aliasEntry)
	}
	r.playerAliases[playerID][alias] = aliasEntry{command: cmd}
	return nil
}

func (r *inMemoryAliasRepo) DeletePlayerAlias(_ context.Context, playerID ulid.ULID, alias string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.playerAliases[playerID] != nil {
		delete(r.playerAliases[playerID], alias)
	}
	return nil
}

// GetSystemAliases returns all system aliases (for loading into cache after "restart").
func (r *inMemoryAliasRepo) GetSystemAliases() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	for alias, entry := range r.systemAliases {
		result[alias] = entry.command
	}
	return result
}

// GetPlayerAliases returns all player aliases (for loading into cache after "restart").
func (r *inMemoryAliasRepo) GetPlayerAliases(playerID ulid.ULID) map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string)
	if aliases, ok := r.playerAliases[playerID]; ok {
		for alias, entry := range aliases {
			result[alias] = entry.command
		}
	}
	return result
}

var _ = Describe("Alias Management Integration", func() {
	var (
		registry   *command.Registry
		dispatcher *command.Dispatcher
		mockAccess *accesstest.MockAccessControl
		aliasCache *command.AliasCache
		services   *command.Services
	)

	BeforeEach(func() {
		registry = command.NewRegistry()
		mockAccess = accesstest.NewMockAccessControl()
		aliasCache = command.NewAliasCache()

		// Create services with alias cache and registry
		var err error
		services, err = command.NewServices(command.ServicesConfig{
			World:       &world.Service{},
			Session:     &stubSessionService{},
			Access:      mockAccess,
			Events:      &stubEventStore{},
			Broadcaster: &core.Broadcaster{},
			AliasCache:  aliasCache,
			Registry:    registry,
		})
		Expect(err).NotTo(HaveOccurred())
	})

	Describe("Player alias management workflow", func() {
		var (
			playerID ulid.ULID
			charID   ulid.ULID
		)

		BeforeEach(func() {
			playerID = ulid.Make()
			charID = ulid.Make()

			// Register alias commands
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name: "alias",
				Help: "Manage player aliases",
				Handler: func(ctx context.Context, exec *command.CommandExecution) error {
					return nil // Subcommand placeholder
				},
				Source: "core",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			// Register a test command that can be aliased
			entry, err = command.NewCommandEntry(command.CommandEntryConfig{
				Name: "look",
				Help: "Look around",
				Handler: func(_ context.Context, exec *command.CommandExecution) error {
					_, _ = exec.Output().Write([]byte("You look around."))
					return nil
				},
				Source: "core",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			var dispErr error
			dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
				command.WithAliasCache(aliasCache))
			Expect(dispErr).NotTo(HaveOccurred())
		})

		It("adds a player alias and verifies it works", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Add alias using the handler directly
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "l=look",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.AliasAddHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("Alias 'l' added"))

			// Verify the alias resolves correctly
			result := aliasCache.Resolve(playerID, "l", registry)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))
			Expect(result.AliasUsed).To(Equal("l"))

			// Verify dispatching through alias works
			buf.Reset()
			dispExec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Output:      &buf,
				Services:    stubServices(),
			})
			err = dispatcher.Dispatch(ctx, "l", dispExec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("You look around"))
		})

		It("lists player aliases and verifies the new alias appears", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Add some aliases first
			Expect(aliasCache.SetPlayerAlias(playerID, "l", "look")).To(Succeed())
			Expect(aliasCache.SetPlayerAlias(playerID, "n", "north")).To(Succeed())

			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Output:      &buf,
				Services:    services,
			})

			err := handlers.AliasListHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())

			output := buf.String()
			Expect(output).To(ContainSubstring("Your aliases:"))
			Expect(output).To(ContainSubstring("l = look"))
			Expect(output).To(ContainSubstring("n = north"))
		})

		It("removes a player alias and verifies it's gone", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Add alias first
			Expect(aliasCache.SetPlayerAlias(playerID, "l", "look")).To(Succeed())

			// Verify it exists
			cmd, exists := aliasCache.GetPlayerAlias(playerID, "l")
			Expect(exists).To(BeTrue())
			Expect(cmd).To(Equal("look"))

			// Remove it
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "l",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.AliasRemoveHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("Alias 'l' removed"))

			// Verify it's gone
			_, exists = aliasCache.GetPlayerAlias(playerID, "l")
			Expect(exists).To(BeFalse())
		})
	})

	Describe("System alias management workflow", func() {
		var charID ulid.ULID

		BeforeEach(func() {
			charID = ulid.Make()

			// Register sysalias commands
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name: "sysalias",
				Help: "Manage system aliases",
				Handler: func(ctx context.Context, exec *command.CommandExecution) error {
					return nil // Subcommand placeholder
				},
				Source: "core",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			// Register a test command that can be aliased
			entry, err = command.NewCommandEntry(command.CommandEntryConfig{
				Name: "look",
				Help: "Look around",
				Handler: func(_ context.Context, exec *command.CommandExecution) error {
					_, _ = exec.Output().Write([]byte("You look around."))
					return nil
				},
				Source: "core",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			var dispErr error
			dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
				command.WithAliasCache(aliasCache))
			Expect(dispErr).NotTo(HaveOccurred())
		})

		It("adds a system alias and verifies it works", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   ulid.Make(),
				Args:        "l=look",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.SysaliasAddHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("System alias 'l' added"))

			// Verify the system alias resolves for any player
			randomPlayer := ulid.Make()
			result := aliasCache.Resolve(randomPlayer, "l", registry)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))
		})

		It("lists system aliases", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Add some system aliases first
			Expect(aliasCache.SetSystemAlias("l", "look")).To(Succeed())
			Expect(aliasCache.SetSystemAlias("q", "quit")).To(Succeed())

			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   ulid.Make(),
				Output:      &buf,
				Services:    services,
			})

			err := handlers.SysaliasListHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())

			output := buf.String()
			Expect(output).To(ContainSubstring("System aliases:"))
			Expect(output).To(ContainSubstring("l = look"))
			Expect(output).To(ContainSubstring("q = quit"))
		})

		It("removes a system alias", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Add system alias first
			Expect(aliasCache.SetSystemAlias("l", "look")).To(Succeed())

			// Verify it exists
			cmd, exists := aliasCache.GetSystemAlias("l")
			Expect(exists).To(BeTrue())
			Expect(cmd).To(Equal("look"))

			// Remove it
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   ulid.Make(),
				Args:        "l",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.SysaliasRemoveHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("System alias 'l' removed"))

			// Verify it's gone
			_, exists = aliasCache.GetSystemAlias("l")
			Expect(exists).To(BeFalse())
		})
	})

	Describe("Shadow warnings", func() {
		var (
			playerID ulid.ULID
			charID   ulid.ULID
		)

		BeforeEach(func() {
			playerID = ulid.Make()
			charID = ulid.Make()

			// Register a command that can be shadowed
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name: "look",
				Help: "Look around",
				Handler: func(_ context.Context, _ *command.CommandExecution) error {
					return nil
				},
				Source: "core",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			var dispErr error
			dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
				command.WithAliasCache(aliasCache))
			Expect(dispErr).NotTo(HaveOccurred())
		})

		It("warns when player alias shadows an existing command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "look=examine",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.AliasAddHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())

			output := buf.String()
			Expect(output).To(ContainSubstring("Warning"))
			Expect(output).To(ContainSubstring("'look' is an existing command"))
			Expect(output).To(ContainSubstring("Your alias will override it"))
			Expect(output).To(ContainSubstring("Alias 'look' added"))
		})

		It("warns when player alias shadows a system alias", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// First add a system alias
			Expect(aliasCache.SetSystemAlias("l", "look")).To(Succeed())

			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "l=list",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.AliasAddHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())

			output := buf.String()
			Expect(output).To(ContainSubstring("Warning"))
			Expect(output).To(ContainSubstring("'l' is a system alias"))
			Expect(output).To(ContainSubstring("Your alias will take precedence"))
			Expect(output).To(ContainSubstring("Alias 'l' added"))
		})

		It("warns when replacing an existing player alias", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// First add a player alias
			Expect(aliasCache.SetPlayerAlias(playerID, "l", "look")).To(Succeed())

			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "l=list",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.AliasAddHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())

			output := buf.String()
			Expect(output).To(ContainSubstring("Warning"))
			Expect(output).To(ContainSubstring("Replacing existing alias 'l'"))
			Expect(output).To(ContainSubstring("was: 'look'"))
			Expect(output).To(ContainSubstring("Alias 'l' added"))
		})

		It("warns when system alias shadows an existing command", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   ulid.Make(),
				Args:        "look=examine",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.SysaliasAddHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())

			output := buf.String()
			Expect(output).To(ContainSubstring("Warning"))
			Expect(output).To(ContainSubstring("'look' is an existing command"))
			Expect(output).To(ContainSubstring("System alias 'look' added"))
		})
	})

	Describe("System alias conflict blocking", func() {
		var charID ulid.ULID

		BeforeEach(func() {
			charID = ulid.Make()

			var dispErr error
			dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
				command.WithAliasCache(aliasCache))
			Expect(dispErr).NotTo(HaveOccurred())
		})

		It("blocks adding a system alias that shadows an existing system alias", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// First add a system alias
			Expect(aliasCache.SetSystemAlias("l", "look")).To(Succeed())

			// Try to add another system alias with the same name
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   ulid.Make(),
				Args:        "l=list",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.SysaliasAddHandler(ctx, exec)
			Expect(err).To(HaveOccurred())

			// Verify error code
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(oopsErr.Code()).To(Equal(command.CodeAliasConflict))

			// Verify original alias is unchanged
			cmd, exists := aliasCache.GetSystemAlias("l")
			Expect(exists).To(BeTrue())
			Expect(cmd).To(Equal("look"))
		})
	})

	Describe("Player alias isolation", func() {
		It("maintains separate aliases for different players", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			player1 := ulid.Make()
			player2 := ulid.Make()

			// Player 1 adds alias l=look
			var buf1 bytes.Buffer
			exec1 := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: ulid.Make(),
				PlayerID:    player1,
				SessionID:   ulid.Make(),
				Args:        "l=look",
				Output:      &buf1,
				Services:    services,
			})
			Expect(handlers.AliasAddHandler(ctx, exec1)).To(Succeed())

			// Player 2 adds alias l=list (same alias name, different command)
			var buf2 bytes.Buffer
			exec2 := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: ulid.Make(),
				PlayerID:    player2,
				SessionID:   ulid.Make(),
				Args:        "l=list",
				Output:      &buf2,
				Services:    services,
			})
			Expect(handlers.AliasAddHandler(ctx, exec2)).To(Succeed())

			// Verify each player has their own alias
			result1 := aliasCache.Resolve(player1, "l", nil)
			Expect(result1.WasAlias).To(BeTrue())
			Expect(result1.Resolved).To(Equal("look"))

			result2 := aliasCache.Resolve(player2, "l", nil)
			Expect(result2.WasAlias).To(BeTrue())
			Expect(result2.Resolved).To(Equal("list"))
		})
	})

	Describe("Player alias precedence over system alias", func() {
		It("resolves player alias when both player and system aliases exist", func() {
			// Add system alias
			Expect(aliasCache.SetSystemAlias("l", "look")).To(Succeed())

			// Add player alias with same name
			playerID := ulid.Make()
			Expect(aliasCache.SetPlayerAlias(playerID, "l", "list")).To(Succeed())

			// Player alias should take precedence
			result := aliasCache.Resolve(playerID, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("list"))

			// Other players should still get system alias
			otherPlayer := ulid.Make()
			result2 := aliasCache.Resolve(otherPlayer, "l", nil)
			Expect(result2.WasAlias).To(BeTrue())
			Expect(result2.Resolved).To(Equal("look"))
		})
	})

	Describe("Circular alias detection", func() {
		var (
			playerID ulid.ULID
			charID   ulid.ULID
		)

		BeforeEach(func() {
			playerID = ulid.Make()
			charID = ulid.Make()
		})

		It("rejects circular player alias chains", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Set up a chain: a -> b -> c
			Expect(aliasCache.SetPlayerAlias(playerID, "a", "b")).To(Succeed())
			Expect(aliasCache.SetPlayerAlias(playerID, "b", "c")).To(Succeed())

			// Try to create a cycle: c -> a
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "c=a",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.AliasAddHandler(ctx, exec)
			Expect(err).To(HaveOccurred())

			// Verify error code
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(oopsErr.Code()).To(Equal(command.CodeCircularAlias))
		})

		It("rejects circular system alias chains", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Set up a chain: x -> y -> z
			Expect(aliasCache.SetSystemAlias("x", "y")).To(Succeed())
			Expect(aliasCache.SetSystemAlias("y", "z")).To(Succeed())

			// Try to create a cycle: z -> x
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   ulid.Make(),
				Args:        "z=x",
				Output:      &buf,
				Services:    services,
			})

			err := handlers.SysaliasAddHandler(ctx, exec)
			Expect(err).To(HaveOccurred())

			// Verify error code
			oopsErr, ok := oops.AsOops(err)
			Expect(ok).To(BeTrue())
			Expect(oopsErr.Code()).To(Equal(command.CodeCircularAlias))
		})
	})
})

var _ = Describe("Alias Persistence Integration", func() {
	var aliasRepo *inMemoryAliasRepo

	BeforeEach(func() {
		aliasRepo = newInMemoryAliasRepo()
	})

	Describe("Aliases survive server restart", func() {
		It("persists player aliases and restores them after cache clear", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			playerID := ulid.Make()
			charID := ulid.Make()

			// Create a fresh cache and services
			cache1 := command.NewAliasCache()
			mockAccess := accesstest.NewMockAccessControl()
			services1, err := command.NewServices(command.ServicesConfig{
				World:       &world.Service{},
				Session:     &stubSessionService{},
				Access:      mockAccess,
				Events:      &stubEventStore{},
				Broadcaster: &core.Broadcaster{},
				AliasCache:  cache1,
				AliasRepo:   aliasRepo,
			})
			Expect(err).NotTo(HaveOccurred())

			// Add a player alias
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "l=look",
				Output:      &buf,
				Services:    services1,
			})

			err = handlers.AliasAddHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("Alias 'l' added"))

			// Verify alias is in cache
			result := cache1.Resolve(playerID, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))

			// Simulate server restart: create a new cache (old cache is gone)
			cache2 := command.NewAliasCache()

			// Load aliases from "database" (our in-memory repo)
			playerAliases := aliasRepo.GetPlayerAliases(playerID)
			cache2.LoadPlayerAliases(playerID, playerAliases)

			// Verify alias was restored from "database"
			result2 := cache2.Resolve(playerID, "l", nil)
			Expect(result2.WasAlias).To(BeTrue())
			Expect(result2.Resolved).To(Equal("look"))
		})

		It("persists system aliases and restores them after cache clear", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			charID := ulid.Make()

			// Create a fresh cache and services
			cache1 := command.NewAliasCache()
			mockAccess := accesstest.NewMockAccessControl()
			services1, err := command.NewServices(command.ServicesConfig{
				World:       &world.Service{},
				Session:     &stubSessionService{},
				Access:      mockAccess,
				Events:      &stubEventStore{},
				Broadcaster: &core.Broadcaster{},
				AliasCache:  cache1,
				AliasRepo:   aliasRepo,
			})
			Expect(err).NotTo(HaveOccurred())

			// Add a system alias
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   ulid.Make(),
				Args:        "q=quit",
				Output:      &buf,
				Services:    services1,
			})

			err = handlers.SysaliasAddHandler(ctx, exec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("System alias 'q' added"))

			// Verify alias is in cache
			result := cache1.Resolve(ulid.ULID{}, "q", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("quit"))

			// Simulate server restart: create a new cache (old cache is gone)
			cache2 := command.NewAliasCache()

			// Load aliases from "database" (our in-memory repo)
			systemAliases := aliasRepo.GetSystemAliases()
			cache2.LoadSystemAliases(systemAliases)

			// Verify alias was restored from "database"
			result2 := cache2.Resolve(ulid.ULID{}, "q", nil)
			Expect(result2.WasAlias).To(BeTrue())
			Expect(result2.Resolved).To(Equal("quit"))
		})

		It("removes alias from database when deleted", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			playerID := ulid.Make()
			charID := ulid.Make()

			// Create cache and services
			cache := command.NewAliasCache()
			mockAccess := accesstest.NewMockAccessControl()
			services, err := command.NewServices(command.ServicesConfig{
				World:       &world.Service{},
				Session:     &stubSessionService{},
				Access:      mockAccess,
				Events:      &stubEventStore{},
				Broadcaster: &core.Broadcaster{},
				AliasCache:  cache,
				AliasRepo:   aliasRepo,
			})
			Expect(err).NotTo(HaveOccurred())

			// Add and then remove a player alias
			var buf bytes.Buffer
			addExec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "l=look",
				Output:      &buf,
				Services:    services,
			})

			err = handlers.AliasAddHandler(ctx, addExec)
			Expect(err).NotTo(HaveOccurred())

			// Remove the alias
			buf.Reset()
			removeExec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "l",
				Output:      &buf,
				Services:    services,
			})

			err = handlers.AliasRemoveHandler(ctx, removeExec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("Alias 'l' removed"))

			// Verify alias is gone from database
			playerAliases := aliasRepo.GetPlayerAliases(playerID)
			Expect(playerAliases).NotTo(HaveKey("l"))

			// Simulate server restart and verify alias is NOT restored
			cache2 := command.NewAliasCache()
			cache2.LoadPlayerAliases(playerID, aliasRepo.GetPlayerAliases(playerID))

			result := cache2.Resolve(playerID, "l", nil)
			Expect(result.WasAlias).To(BeFalse())
		})
	})
})

var _ = Describe("Session Termination Alias Cache Invalidation", func() {
	var (
		aliasRepo  *inMemoryAliasRepo
		aliasCache *command.AliasCache
		registry   *command.Registry
		dispatcher *command.Dispatcher
		mockAccess *accesstest.MockAccessControl
		services   *command.Services
	)

	BeforeEach(func() {
		aliasRepo = newInMemoryAliasRepo()
		aliasCache = command.NewAliasCache()
		registry = command.NewRegistry()
		mockAccess = accesstest.NewMockAccessControl()

		// Register a command that aliases can target
		entry, err := command.NewCommandEntry(command.CommandEntryConfig{
			Name: "look",
			Help: "Look around",
			Handler: func(_ context.Context, exec *command.CommandExecution) error {
				_, _ = exec.Output().Write([]byte("You look around."))
				return nil
			},
			Source: "core",
		})
		Expect(err).NotTo(HaveOccurred())
		err = registry.Register(*entry)
		Expect(err).NotTo(HaveOccurred())

		entry, err = command.NewCommandEntry(command.CommandEntryConfig{
			Name: "north",
			Help: "Go north",
			Handler: func(_ context.Context, exec *command.CommandExecution) error {
				_, _ = exec.Output().Write([]byte("You go north."))
				return nil
			},
			Source: "core",
		})
		Expect(err).NotTo(HaveOccurred())
		err = registry.Register(*entry)
		Expect(err).NotTo(HaveOccurred())

		services, err = command.NewServices(command.ServicesConfig{
			World:       &world.Service{},
			Session:     &stubSessionService{},
			Access:      mockAccess,
			Events:      &stubEventStore{},
			Broadcaster: &core.Broadcaster{},
			AliasCache:  aliasCache,
			AliasRepo:   aliasRepo,
			Registry:    registry,
		})
		Expect(err).NotTo(HaveOccurred())

		var dispErr error
		dispatcher, dispErr = command.NewDispatcher(registry, mockAccess,
			command.WithAliasCache(aliasCache))
		Expect(dispErr).NotTo(HaveOccurred())
	})

	It("clears player aliases from cache on session termination", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		playerID := ulid.Make()
		charID := ulid.Make()
		sessionID := ulid.Make()

		// Step 1: Simulate session establishment by loading player aliases
		aliasCache.LoadPlayerAliases(playerID, map[string]string{
			"l": "look",
			"n": "north",
		})

		// Step 2: Verify aliases work during active session
		result := aliasCache.Resolve(playerID, "l", registry)
		Expect(result.WasAlias).To(BeTrue())
		Expect(result.Resolved).To(Equal("look"))

		result = aliasCache.Resolve(playerID, "n", registry)
		Expect(result.WasAlias).To(BeTrue())
		Expect(result.Resolved).To(Equal("north"))

		// Verify dispatching through alias works
		var buf bytes.Buffer
		dispExec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: charID,
			PlayerID:    playerID,
			SessionID:   sessionID,
			Output:      &buf,
			Services:    services,
		})
		err := dispatcher.Dispatch(ctx, "l", dispExec)
		Expect(err).NotTo(HaveOccurred())
		Expect(buf.String()).To(ContainSubstring("You look around"))

		// Step 3: Simulate session termination
		aliasCache.ClearPlayer(playerID)

		// Step 4: Verify player aliases are cleared from cache
		result = aliasCache.Resolve(playerID, "l", registry)
		Expect(result.WasAlias).To(BeFalse())
		Expect(result.Resolved).To(Equal("l"))

		result = aliasCache.Resolve(playerID, "n", registry)
		Expect(result.WasAlias).To(BeFalse())
		Expect(result.Resolved).To(Equal("n"))

		// Verify GetPlayerAlias also returns nothing
		_, exists := aliasCache.GetPlayerAlias(playerID, "l")
		Expect(exists).To(BeFalse())

		// Verify ListPlayerAliases returns empty
		aliases := aliasCache.ListPlayerAliases(playerID)
		Expect(aliases).To(BeEmpty())
	})

	It("does not affect system aliases when clearing player cache", func() {
		playerID := ulid.Make()

		// Set up both system and player aliases
		Expect(aliasCache.SetSystemAlias("l", "look")).To(Succeed())
		aliasCache.LoadPlayerAliases(playerID, map[string]string{
			"n": "north",
		})

		// Verify both resolve
		result := aliasCache.Resolve(playerID, "l", nil)
		Expect(result.WasAlias).To(BeTrue())
		result = aliasCache.Resolve(playerID, "n", nil)
		Expect(result.WasAlias).To(BeTrue())

		// Simulate session termination
		aliasCache.ClearPlayer(playerID)

		// Player alias should be gone
		result = aliasCache.Resolve(playerID, "n", nil)
		Expect(result.WasAlias).To(BeFalse())

		// System alias should still work
		result = aliasCache.Resolve(playerID, "l", nil)
		Expect(result.WasAlias).To(BeTrue())
		Expect(result.Resolved).To(Equal("look"))
	})

	It("does not affect other players when clearing one player's cache", func() {
		player1 := ulid.Make()
		player2 := ulid.Make()

		// Load aliases for both players
		aliasCache.LoadPlayerAliases(player1, map[string]string{"l": "look"})
		aliasCache.LoadPlayerAliases(player2, map[string]string{"l": "list"})

		// Terminate player1's session
		aliasCache.ClearPlayer(player1)

		// Player1's aliases should be gone
		result := aliasCache.Resolve(player1, "l", nil)
		Expect(result.WasAlias).To(BeFalse())

		// Player2's aliases should be unaffected
		result = aliasCache.Resolve(player2, "l", nil)
		Expect(result.WasAlias).To(BeTrue())
		Expect(result.Resolved).To(Equal("list"))
	})

	It("allows fresh alias loading after session termination and reconnect", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		playerID := ulid.Make()
		charID := ulid.Make()

		// Simulate first session: add aliases via handler (persisted to repo)
		var buf bytes.Buffer
		exec := command.NewTestExecution(command.CommandExecutionConfig{
			CharacterID: charID,
			PlayerID:    playerID,
			SessionID:   ulid.Make(),
			Args:        "l=look",
			Output:      &buf,
			Services:    services,
		})
		err := handlers.AliasAddHandler(ctx, exec)
		Expect(err).NotTo(HaveOccurred())

		// Verify alias works
		result := aliasCache.Resolve(playerID, "l", registry)
		Expect(result.WasAlias).To(BeTrue())
		Expect(result.Resolved).To(Equal("look"))

		// Simulate session termination
		aliasCache.ClearPlayer(playerID)

		// Verify alias is gone from cache
		result = aliasCache.Resolve(playerID, "l", registry)
		Expect(result.WasAlias).To(BeFalse())

		// Step 5: Simulate reconnection by loading fresh aliases from database
		freshAliases := aliasRepo.GetPlayerAliases(playerID)
		aliasCache.LoadPlayerAliases(playerID, freshAliases)

		// Verify aliases are restored from database
		result = aliasCache.Resolve(playerID, "l", registry)
		Expect(result.WasAlias).To(BeTrue())
		Expect(result.Resolved).To(Equal("look"))
	})
})

var _ = Describe("Alias Cache Startup Loading from Database", func() {
	var (
		aliasRepo *inMemoryAliasRepo
	)

	BeforeEach(func() {
		aliasRepo = newInMemoryAliasRepo()
	})

	Describe("System aliases loaded at startup", func() {
		It("seeds database with system aliases and loads them into fresh cache", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Seed "database" with system aliases (simulating admin setup)
			Expect(aliasRepo.SetSystemAlias(ctx, "l", "look", "admin")).To(Succeed())
			Expect(aliasRepo.SetSystemAlias(ctx, "n", "north", "admin")).To(Succeed())
			Expect(aliasRepo.SetSystemAlias(ctx, "s", "south", "admin")).To(Succeed())

			// Simulate server startup: create new cache, load from DB
			cache := command.NewAliasCache()
			systemAliases := aliasRepo.GetSystemAliases()
			cache.LoadSystemAliases(systemAliases)

			// Verify system aliases resolve correctly for any player
			anyPlayer := ulid.Make()
			result := cache.Resolve(anyPlayer, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))

			result = cache.Resolve(anyPlayer, "n", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("north"))

			result = cache.Resolve(anyPlayer, "s", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("south"))
		})

		It("resolves system aliases through dispatcher after startup load", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			// Seed DB
			Expect(aliasRepo.SetSystemAlias(ctx, "l", "look", "admin")).To(Succeed())

			// Create fresh cache and dispatcher (simulating startup)
			cache := command.NewAliasCache()
			cache.LoadSystemAliases(aliasRepo.GetSystemAliases())

			registry := command.NewRegistry()
			entry, err := command.NewCommandEntry(command.CommandEntryConfig{
				Name: "look",
				Help: "Look around",
				Handler: func(_ context.Context, exec *command.CommandExecution) error {
					_, _ = exec.Output().Write([]byte("You look around."))
					return nil
				},
				Source: "core",
			})
			Expect(err).NotTo(HaveOccurred())
			err = registry.Register(*entry)
			Expect(err).NotTo(HaveOccurred())

			mockAccess := accesstest.NewMockAccessControl()
			dispatcher, dispErr := command.NewDispatcher(registry, mockAccess,
				command.WithAliasCache(cache))
			Expect(dispErr).NotTo(HaveOccurred())

			// Dispatch using the system alias
			var buf bytes.Buffer
			exec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: ulid.Make(),
				PlayerID:    ulid.Make(),
				SessionID:   ulid.Make(),
				Output:      &buf,
				Services:    stubServices(),
			})
			err = dispatcher.Dispatch(ctx, "l", exec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("You look around"))
		})
	})

	Describe("Player aliases loaded on session establishment", func() {
		It("loads player aliases from database when session starts", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			playerID := ulid.Make()

			// Seed DB with player aliases (simulating previous session data)
			Expect(aliasRepo.SetPlayerAlias(ctx, playerID, "l", "look")).To(Succeed())
			Expect(aliasRepo.SetPlayerAlias(ctx, playerID, "aa", "attack all")).To(Succeed())

			// Create fresh cache (simulating startup)
			cache := command.NewAliasCache()

			// Simulate session establishment: load player aliases from DB
			playerAliases := aliasRepo.GetPlayerAliases(playerID)
			cache.LoadPlayerAliases(playerID, playerAliases)

			// Verify player aliases resolve correctly
			result := cache.Resolve(playerID, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))

			result = cache.Resolve(playerID, "aa", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("attack all"))

			// Verify other players don't see these aliases
			otherPlayer := ulid.Make()
			result = cache.Resolve(otherPlayer, "l", nil)
			Expect(result.WasAlias).To(BeFalse())
		})
	})

	Describe("Restart scenario: aliases persisted and reloaded", func() {
		It("persists aliases during runtime and reloads after simulated restart", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			playerID := ulid.Make()
			charID := ulid.Make()

			// Phase 1: Runtime - create aliases (persisted to repo)
			cache1 := command.NewAliasCache()
			mockAccess := accesstest.NewMockAccessControl()
			registry := command.NewRegistry()
			services1, err := command.NewServices(command.ServicesConfig{
				World:       &world.Service{},
				Session:     &stubSessionService{},
				Access:      mockAccess,
				Events:      &stubEventStore{},
				Broadcaster: &core.Broadcaster{},
				AliasCache:  cache1,
				AliasRepo:   aliasRepo,
				Registry:    registry,
			})
			Expect(err).NotTo(HaveOccurred())

			// Add system alias via handler
			var buf bytes.Buffer
			sysExec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				SessionID:   ulid.Make(),
				Args:        "q=quit",
				Output:      &buf,
				Services:    services1,
			})
			err = handlers.SysaliasAddHandler(ctx, sysExec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("System alias 'q' added"))

			// Add player alias via handler
			buf.Reset()
			playerExec := command.NewTestExecution(command.CommandExecutionConfig{
				CharacterID: charID,
				PlayerID:    playerID,
				SessionID:   ulid.Make(),
				Args:        "l=look",
				Output:      &buf,
				Services:    services1,
			})
			err = handlers.AliasAddHandler(ctx, playerExec)
			Expect(err).NotTo(HaveOccurred())
			Expect(buf.String()).To(ContainSubstring("Alias 'l' added"))

			// Verify both aliases work in cache1
			result := cache1.Resolve(playerID, "q", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("quit"))

			result = cache1.Resolve(playerID, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))

			// Phase 2: Simulate server restart - create brand new cache
			cache2 := command.NewAliasCache()

			// Verify fresh cache has nothing
			result = cache2.Resolve(playerID, "q", nil)
			Expect(result.WasAlias).To(BeFalse())
			result = cache2.Resolve(playerID, "l", nil)
			Expect(result.WasAlias).To(BeFalse())

			// Load system aliases from database (done at startup)
			cache2.LoadSystemAliases(aliasRepo.GetSystemAliases())

			// Verify system alias restored
			result = cache2.Resolve(ulid.ULID{}, "q", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("quit"))

			// Player alias not yet loaded (session not established)
			result = cache2.Resolve(playerID, "l", nil)
			Expect(result.WasAlias).To(BeFalse())

			// Simulate player session establishment
			cache2.LoadPlayerAliases(playerID, aliasRepo.GetPlayerAliases(playerID))

			// Verify player alias now restored
			result = cache2.Resolve(playerID, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))

			// Verify system alias still works alongside player alias
			result = cache2.Resolve(playerID, "q", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("quit"))
		})

		It("handles multiple player sessions after restart", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			player1 := ulid.Make()
			player2 := ulid.Make()

			// Seed database with aliases for two players
			Expect(aliasRepo.SetPlayerAlias(ctx, player1, "l", "look")).To(Succeed())
			Expect(aliasRepo.SetPlayerAlias(ctx, player1, "n", "north")).To(Succeed())
			Expect(aliasRepo.SetPlayerAlias(ctx, player2, "l", "list")).To(Succeed())
			Expect(aliasRepo.SetPlayerAlias(ctx, player2, "h", "help")).To(Succeed())

			// Simulate server startup with fresh cache
			cache := command.NewAliasCache()

			// Player 1 connects first
			cache.LoadPlayerAliases(player1, aliasRepo.GetPlayerAliases(player1))

			// Verify player 1's aliases
			result := cache.Resolve(player1, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))
			result = cache.Resolve(player1, "n", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("north"))

			// Player 2's aliases should not be loaded yet
			result = cache.Resolve(player2, "l", nil)
			Expect(result.WasAlias).To(BeFalse())

			// Player 2 connects
			cache.LoadPlayerAliases(player2, aliasRepo.GetPlayerAliases(player2))

			// Verify player 2's aliases
			result = cache.Resolve(player2, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("list"))
			result = cache.Resolve(player2, "h", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("help"))

			// Verify player 1's aliases are still intact
			result = cache.Resolve(player1, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("look"))

			// Player 1 disconnects
			cache.ClearPlayer(player1)

			// Player 1's aliases gone
			result = cache.Resolve(player1, "l", nil)
			Expect(result.WasAlias).To(BeFalse())

			// Player 2 still works
			result = cache.Resolve(player2, "l", nil)
			Expect(result.WasAlias).To(BeTrue())
			Expect(result.Resolved).To(Equal("list"))
		})
	})
})
