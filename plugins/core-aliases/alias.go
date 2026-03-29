// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corealiases

import (
	"context"
	"fmt"
	"sort"
	"strings"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// handleAliasAdd creates or updates a player alias.
// Usage: alias <name>=<command>
func handleAliasAdd(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	alias, command, err := parseAliasDefinition(cmd.Args)
	if err != nil {
		return nil, err
	}

	if err := validateAliasName(alias); err != nil {
		return nil, err
	}

	var warnings []string

	// Check if alias shadows an existing command.
	shadows, shadowedCmd, err := proxy.CheckAliasShadow(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("checking alias shadow: %w", err)
	}
	if shadows {
		warnings = append(warnings, fmt.Sprintf("Warning: '%s' is an existing command. Your alias will override it.", alias))
	}

	// Check if alias shadows a system alias.
	sysAliases, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("checking system aliases: %w", err)
	}
	if sysCmd, ok := findAlias(sysAliases, alias); ok {
		warnings = append(warnings, fmt.Sprintf("Warning: '%s' is a system alias for '%s'. Your alias will take precedence.", alias, sysCmd))
	}
	_ = shadowedCmd // used only for command shadow detection above

	// Check if replacing an existing player alias.
	playerAliases, err := proxy.ListPlayerAliases(ctx, cmd.CharacterID)
	if err != nil {
		return nil, fmt.Errorf("listing player aliases: %w", err)
	}
	if existingCmd, ok := findAlias(playerAliases, alias); ok {
		warnings = append(warnings, fmt.Sprintf("Warning: Replacing existing alias '%s' (was: '%s').", alias, existingCmd))
	}

	// Set the alias (proxy handles DB + cache).
	if err := proxy.SetPlayerAlias(ctx, cmd.CharacterID, alias, command); err != nil {
		return nil, err //nolint:wrapcheck // proxy returns structured errors
	}

	var out strings.Builder
	for _, w := range warnings {
		fmt.Fprintln(&out, w)
	}
	fmt.Fprintf(&out, "Alias '%s' added: %s\n", alias, command)

	return &pluginsdk.CommandResponse{Output: out.String()}, nil
}

// handleAliasRemove removes a player alias.
// Usage: unalias <name>
func handleAliasRemove(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	alias := strings.TrimSpace(cmd.Args)
	if alias == "" {
		return nil, fmt.Errorf("usage: unalias <alias>")
	}

	// Check if alias exists before removing.
	playerAliases, err := proxy.ListPlayerAliases(ctx, cmd.CharacterID)
	if err != nil {
		return nil, fmt.Errorf("listing player aliases: %w", err)
	}
	if _, ok := findAlias(playerAliases, alias); !ok {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("No alias '%s' found.\n", alias),
		}, nil
	}

	if err := proxy.DeletePlayerAlias(ctx, cmd.CharacterID, alias); err != nil {
		return nil, err //nolint:wrapcheck // proxy returns structured errors
	}

	return &pluginsdk.CommandResponse{
		Output: fmt.Sprintf("Alias '%s' removed.\n", alias),
	}, nil
}

// handleAliasList lists all player aliases.
// Usage: aliases
func handleAliasList(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	aliases, err := proxy.ListPlayerAliases(ctx, cmd.CharacterID)
	if err != nil {
		return nil, fmt.Errorf("listing player aliases: %w", err)
	}

	if len(aliases) == 0 {
		return &pluginsdk.CommandResponse{Output: "You have no aliases defined."}, nil
	}

	sort.Slice(aliases, func(i, j int) bool {
		return aliases[i].Alias < aliases[j].Alias
	})

	var out strings.Builder
	fmt.Fprintln(&out, "Your aliases:")
	for _, a := range aliases {
		fmt.Fprintf(&out, "  %s = %s\n", a.Alias, a.Command)
	}

	return &pluginsdk.CommandResponse{Output: out.String()}, nil
}
