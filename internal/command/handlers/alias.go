// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"sort"
	"strings"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// CodeNoAliasCache is the error code for missing alias cache.
const CodeNoAliasCache = "NO_ALIAS_CACHE"

// errNoAliasCache creates an error for when alias operations are attempted
// without a configured alias cache.
func errNoAliasCache() error {
	return oops.Code(CodeNoAliasCache).
		Errorf("alias operations require a configured alias cache")
}

// --- Player Alias Commands ---

// AliasAddHandler adds a player alias.
// Usage: alias add <alias>=<command>
//
// Warnings are issued but don't prevent the operation:
//   - If alias shadows an existing command
//   - If alias shadows a system alias
//   - If alias replaces an existing player alias
func AliasAddHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services.AliasCache
	registry := exec.Services.Registry

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
	if cache.ShadowsCommand(alias, registry) {
		warnings = append(warnings, "Warning: '"+alias+"' is an existing command. Your alias will override it.")
	}

	// Check if shadowing a system alias
	if sysCmd, shadows := cache.ShadowsSystemAlias(alias); shadows {
		warnings = append(warnings, "Warning: '"+alias+"' is a system alias for '"+sysCmd+"'. Your alias will take precedence.")
	}

	// Check if replacing own existing alias
	if existingCmd, exists := cache.GetPlayerAlias(exec.PlayerID, alias); exists {
		warnings = append(warnings, "Warning: Replacing existing alias '"+alias+"' (was: '"+existingCmd+"').")
	}

	// Attempt to set the alias (may fail due to circular reference)
	if err := cache.SetPlayerAlias(exec.PlayerID, alias, cmd); err != nil {
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
// Usage: alias remove <alias>
func AliasRemoveHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services.AliasCache

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
		return command.ErrInvalidArgs("alias remove", "alias remove <alias>")
	}

	// Check if alias exists before removing
	if _, exists := cache.GetPlayerAlias(exec.PlayerID, alias); !exists {
		writeOutputf(ctx, exec, "alias", "No alias '%s' found.\n", alias)
		return nil
	}

	cache.RemovePlayerAlias(exec.PlayerID, alias)
	writeOutputf(ctx, exec, "alias", "Alias '%s' removed.\n", alias)

	return nil
}

// AliasListHandler lists all player aliases.
// Usage: alias list
func AliasListHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services.AliasCache

	if cache == nil {
		return errNoAliasCache()
	}

	return aliasListImpl(ctx, exec, cache)
}

// aliasListImpl contains the implementation for player alias list.
func aliasListImpl(ctx context.Context, exec *command.CommandExecution, cache *command.AliasCache) error {
	aliases := cache.ListPlayerAliases(exec.PlayerID)

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
// Usage: sysalias add <alias>=<command>
//
// Warns if shadowing a command, but BLOCKS if shadowing another system alias.
func SysaliasAddHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services.AliasCache
	registry := exec.Services.Registry

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
	if existingCmd, shadows := cache.ShadowsSystemAlias(alias); shadows {
		//nolint:wrapcheck // ErrAliasConflict creates a structured oops error
		return command.ErrAliasConflict(alias, existingCmd)
	}

	// Check for shadow conditions and emit warnings
	var warnings []string

	// Check if shadowing a registered command
	if cache.ShadowsCommand(alias, registry) {
		warnings = append(warnings, "Warning: '"+alias+"' is an existing command. System alias will override it.")
	}

	// Attempt to set the alias (may fail due to circular reference)
	if err := cache.SetSystemAlias(alias, cmd); err != nil {
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
// Usage: sysalias remove <alias>
func SysaliasRemoveHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services.AliasCache

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
		return command.ErrInvalidArgs("sysalias remove", "sysalias remove <alias>")
	}

	// Check if alias exists before removing
	if _, exists := cache.GetSystemAlias(alias); !exists {
		writeOutputf(ctx, exec, "sysalias", "No system alias '%s' found.\n", alias)
		return nil
	}

	cache.RemoveSystemAlias(alias)
	writeOutputf(ctx, exec, "sysalias", "System alias '%s' removed.\n", alias)

	return nil
}

// SysaliasListHandler lists all system aliases.
// Usage: sysalias list
func SysaliasListHandler(ctx context.Context, exec *command.CommandExecution) error {
	cache := exec.Services.AliasCache

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
