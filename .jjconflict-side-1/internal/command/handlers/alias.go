// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"log/slog"
	"sort"
	"strings"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// errNoAliasCache creates an error for when alias operations are attempted
// without a configured alias cache.
func errNoAliasCache() error {
	return oops.Code(command.CodeNoAliasCache).
		Errorf("alias operations require a configured alias cache")
}

// --- Player Alias Commands ---

// AliasAddHandler adds a player alias.
// Usage: alias <alias>=<command>
//
// Warnings are issued but don't prevent the operation:
//   - If alias shadows an existing command
//   - If alias shadows a system alias
//   - If alias replaces an existing player alias
func AliasAddHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services().AliasCache()
	registry := exec.Services().Registry()

	if cache == nil {
		return errNoAliasCache()
	}

	return aliasAddImpl(ctx, exec, cache, registry)
}

// aliasAddImpl contains the implementation for player alias add.
// Extracted for testing with explicit dependencies.
func aliasAddImpl(ctx context.Context, exec *command.CommandExecution, cache *command.AliasCache, registry *command.Registry) error {
	alias, cmd, err := parseAliasDefinition(exec.Args)
	if err != nil {
		return err
	}

	if err := command.ValidateAliasName(alias); err != nil {
		return err //nolint:wrapcheck // ValidateAliasName creates a structured oops error
	}

	// Check for shadow conditions and emit warnings
	var warnings []string

	// Check if shadowing a registered command
	if checkCommandShadows(cache, registry, alias) {
		warnings = append(warnings, "Warning: '"+alias+"' is an existing command. Your alias will override it.")
	}

	// Check if shadowing a system alias
	if sysCmd, shadows := checkSystemAliasShadows(cache, alias); shadows {
		warnings = append(warnings, "Warning: '"+alias+"' is a system alias for '"+sysCmd+"'. Your alias will take precedence.")
	}

	// Check if replacing own existing alias
	if existingCmd, exists := cache.GetPlayerAlias(exec.PlayerID(), alias); exists {
		warnings = append(warnings, "Warning: Replacing existing alias '"+alias+"' (was: '"+existingCmd+"').")
	}

	// Persist to database first (if repository is available)
	// Database write must succeed before updating cache to maintain consistency
	repo := exec.Services().AliasRepo()
	if repo != nil {
		if err := repo.SetPlayerAlias(ctx, exec.PlayerID(), alias, cmd); err != nil {
			return oops.With("operation", "persist player alias").
				With("alias", alias).
				Wrap(err)
		}
	}

	// Attempt to set the alias in cache (may fail due to circular reference)
	if err := cache.SetPlayerAlias(exec.PlayerID(), alias, cmd); err != nil {
		// Rollback: delete from database since cache update failed
		if repo != nil {
			if rollbackErr := repo.DeletePlayerAlias(ctx, exec.PlayerID(), alias); rollbackErr != nil {
				// CRITICAL: Both cache update and rollback failed.
				// Database contains alias but cache does not - inconsistent state.
				// Requires manual intervention. See operator docs for recovery.
				slog.ErrorContext(ctx, "CRITICAL: alias rollback failed - database-cache inconsistency",
					"severity", "critical",
					"alias", alias,
					"player_id", exec.PlayerID().String(),
					"cache_error", err.Error(),
					"rollback_error", rollbackErr.Error(),
					"recovery", "see operator documentation: alias-inconsistency-recovery")
				command.RecordAliasRollbackFailure()
			}
		}
		return err //nolint:wrapcheck // SetPlayerAlias returns structured oops error
	}

	// Output warnings followed by success message
	for _, warning := range warnings {
		writeOutput(ctx, exec, "alias", warning)
	}
	writeOutputf(ctx, exec, "alias", "Alias '%s' added: %s\n", alias, cmd)

	return nil
}

// AliasRemoveHandler removes a player alias.
// Usage: unalias <alias>
func AliasRemoveHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services().AliasCache()

	if cache == nil {
		return errNoAliasCache()
	}

	return aliasRemoveImpl(ctx, exec, cache)
}

// aliasRemoveImpl contains the implementation for player alias remove.
func aliasRemoveImpl(ctx context.Context, exec *command.CommandExecution, cache *command.AliasCache) error {
	alias := strings.TrimSpace(exec.Args)
	if alias == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("unalias", "unalias <alias>")
	}

	// Check if alias exists before removing
	if _, exists := cache.GetPlayerAlias(exec.PlayerID(), alias); !exists {
		writeOutputf(ctx, exec, "alias", "No alias '%s' found.\n", alias)
		return nil
	}

	// Delete from database first (if repository is available)
	// Database delete must succeed before updating cache to maintain consistency
	if repo := exec.Services().AliasRepo(); repo != nil {
		if err := repo.DeletePlayerAlias(ctx, exec.PlayerID(), alias); err != nil {
			return oops.With("operation", "delete player alias").
				With("alias", alias).
				Wrap(err)
		}
	}

	cache.RemovePlayerAlias(exec.PlayerID(), alias)
	writeOutputf(ctx, exec, "alias", "Alias '%s' removed.\n", alias)

	return nil
}

// AliasListHandler lists all player aliases.
// Usage: aliases
func AliasListHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services().AliasCache()

	if cache == nil {
		return errNoAliasCache()
	}

	return aliasListImpl(ctx, exec, cache)
}

// aliasListImpl contains the implementation for player alias list.
func aliasListImpl(ctx context.Context, exec *command.CommandExecution, cache *command.AliasCache) error {
	aliases := cache.ListPlayerAliases(exec.PlayerID())

	if len(aliases) == 0 {
		writeOutput(ctx, exec, "alias", "You have no aliases defined.")
		return nil
	}

	writeOutput(ctx, exec, "alias", "Your aliases:")

	// Sort aliases for consistent output
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, alias := range keys {
		writeOutputf(ctx, exec, "alias", "  %s = %s\n", alias, aliases[alias])
	}

	return nil
}

// --- System Alias Commands ---

// SysaliasAddHandler adds a system alias.
// Usage: sysalias <alias>=<command>
//
// Warns if shadowing a command, but BLOCKS if shadowing another system alias.
func SysaliasAddHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services().AliasCache()
	registry := exec.Services().Registry()

	if cache == nil {
		return errNoAliasCache()
	}

	return sysaliasAddImpl(ctx, exec, cache, registry)
}

// sysaliasAddImpl contains the implementation for system alias add.
func sysaliasAddImpl(ctx context.Context, exec *command.CommandExecution, cache *command.AliasCache, registry *command.Registry) error {
	alias, cmd, err := parseAliasDefinition(exec.Args)
	if err != nil {
		return err
	}

	if err := command.ValidateAliasName(alias); err != nil {
		return err //nolint:wrapcheck // ValidateAliasName creates a structured oops error
	}

	// Block if shadowing an existing system alias
	if existingCmd, shadows := checkSystemAliasShadows(cache, alias); shadows {
		//nolint:wrapcheck // ErrAliasConflict creates a structured oops error
		return command.ErrAliasConflict(alias, existingCmd)
	}

	// Check for shadow conditions and emit warnings
	var warnings []string

	// Check if shadowing a registered command
	if checkCommandShadows(cache, registry, alias) {
		warnings = append(warnings, "Warning: '"+alias+"' is an existing command. System alias will override it.")
	}

	// Persist to database first (if repository is available)
	// Database write must succeed before updating cache to maintain consistency
	repo := exec.Services().AliasRepo()
	if repo != nil {
		createdBy := exec.CharacterID().String()
		if err := repo.SetSystemAlias(ctx, alias, cmd, createdBy); err != nil {
			return oops.With("operation", "persist system alias").
				With("alias", alias).
				Wrap(err)
		}
	}

	// Attempt to set the alias in cache (may fail due to circular reference)
	if err := cache.SetSystemAlias(alias, cmd); err != nil {
		// Rollback: delete from database since cache update failed
		if repo != nil {
			if rollbackErr := repo.DeleteSystemAlias(ctx, alias); rollbackErr != nil {
				// CRITICAL: Both cache update and rollback failed.
				// Database contains alias but cache does not - inconsistent state.
				// Requires manual intervention. See operator docs for recovery.
				slog.ErrorContext(ctx, "CRITICAL: system alias rollback failed - database-cache inconsistency",
					"severity", "critical",
					"alias", alias,
					"cache_error", err.Error(),
					"rollback_error", rollbackErr.Error(),
					"recovery", "see operator documentation: alias-inconsistency-recovery")
				command.RecordAliasRollbackFailure()
			}
		}
		return err //nolint:wrapcheck // SetSystemAlias returns structured oops error
	}

	// Output warnings followed by success message
	for _, warning := range warnings {
		writeOutput(ctx, exec, "sysalias", warning)
	}
	writeOutputf(ctx, exec, "sysalias", "System alias '%s' added: %s\n", alias, cmd)

	return nil
}

// SysaliasRemoveHandler removes a system alias.
// Usage: sysunsalias <alias>
func SysaliasRemoveHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services().AliasCache()

	if cache == nil {
		return errNoAliasCache()
	}

	return sysaliasRemoveImpl(ctx, exec, cache)
}

// sysaliasRemoveImpl contains the implementation for system alias remove.
func sysaliasRemoveImpl(ctx context.Context, exec *command.CommandExecution, cache *command.AliasCache) error {
	alias := strings.TrimSpace(exec.Args)
	if alias == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("sysunsalias", "sysunsalias <alias>")
	}

	// Check if alias exists before removing
	if _, exists := cache.GetSystemAlias(alias); !exists {
		writeOutputf(ctx, exec, "sysalias", "No system alias '%s' found.\n", alias)
		return nil
	}

	// Delete from database first (if repository is available)
	// Database delete must succeed before updating cache to maintain consistency
	if repo := exec.Services().AliasRepo(); repo != nil {
		if err := repo.DeleteSystemAlias(ctx, alias); err != nil {
			return oops.With("operation", "delete system alias").
				With("alias", alias).
				Wrap(err)
		}
	}

	cache.RemoveSystemAlias(alias)
	writeOutputf(ctx, exec, "sysalias", "System alias '%s' removed.\n", alias)

	return nil
}

// SysaliasListHandler lists all system aliases.
// Usage: sysaliases
func SysaliasListHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services().AliasCache()

	if cache == nil {
		return errNoAliasCache()
	}

	return sysaliasListImpl(ctx, exec, cache)
}

// sysaliasListImpl contains the implementation for system alias list.
func sysaliasListImpl(ctx context.Context, exec *command.CommandExecution, cache *command.AliasCache) error {
	aliases := cache.ListSystemAliases()

	if len(aliases) == 0 {
		writeOutput(ctx, exec, "sysalias", "No system aliases defined.")
		return nil
	}

	writeOutput(ctx, exec, "sysalias", "System aliases:")

	// Sort aliases for consistent output
	keys := make([]string, 0, len(aliases))
	for k := range aliases {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, alias := range keys {
		writeOutputf(ctx, exec, "sysalias", "  %s = %s\n", alias, aliases[alias])
	}

	return nil
}

// parseAliasDefinition parses "alias=command" format.
// Returns the alias name, command, and any parse error.
func parseAliasDefinition(args string) (alias, cmd string, err error) {
	args = strings.TrimSpace(args)

	idx := strings.Index(args, "=")
	if idx == -1 {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return "", "", command.ErrInvalidArgs("alias", "<alias>=<command>")
	}

	alias = strings.TrimSpace(args[:idx])
	cmd = strings.TrimSpace(args[idx+1:])

	if alias == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return "", "", command.ErrInvalidArgs("alias", "<alias>=<command> (alias name cannot be empty)")
	}
	if cmd == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return "", "", command.ErrInvalidArgs("alias", "<alias>=<command> (command cannot be empty)")
	}

	return alias, cmd, nil
}
