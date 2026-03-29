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
		return pluginsdk.Errorf("%s", err.Error()), nil
	}

	if err := validateAliasName(alias); err != nil {
		return pluginsdk.Errorf("%s", err.Error()), nil
	}

	var warnings []string

	// Check if alias shadows an existing command.
	shadows, shadowedCmd, err := proxy.CheckAliasShadow(ctx, alias)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("alias: failed to check shadow for %q: %v", alias, err))
		return pluginsdk.Failuref("Unable to create alias right now. Please try again."), nil
	}
	if shadows {
		warnings = append(warnings, fmt.Sprintf("Warning: '%s' is an existing command. Your alias will override it.", alias))
	}

	// Check if alias shadows a system alias.
	sysAliases, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("alias: failed to list system aliases: %v", err))
		return pluginsdk.Failuref("Unable to create alias right now. Please try again."), nil
	}
	if sysCmd, ok := findAlias(sysAliases, alias); ok {
		warnings = append(warnings, fmt.Sprintf("Warning: '%s' is a system alias for '%s'. Your alias will take precedence.", alias, sysCmd))
	}
	_ = shadowedCmd // used only for command shadow detection above

	// Check if replacing an existing player alias.
	playerAliases, err := proxy.ListPlayerAliases(ctx, cmd.CharacterID)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("alias: failed to list player aliases: %v", err))
		return pluginsdk.Failuref("Unable to create alias right now. Please try again."), nil
	}
	if existingCmd, ok := findAlias(playerAliases, alias); ok {
		warnings = append(warnings, fmt.Sprintf("Warning: Replacing existing alias '%s' (was: '%s').", alias, existingCmd))
	}

	// Set the alias (proxy handles DB + cache).
	if err := proxy.SetPlayerAlias(ctx, cmd.CharacterID, alias, command); err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("alias: failed to set alias %q: %v", alias, err))
		return pluginsdk.Failuref("Unable to create alias right now. Please try again."), nil
	}

	var out strings.Builder
	for _, w := range warnings {
		fmt.Fprintln(&out, w)
	}
	fmt.Fprintf(&out, "Alias '%s' added: %s\n", alias, command)

	return pluginsdk.OK(out.String()), nil
}

// handleAliasRemove removes a player alias.
// Usage: unalias <name>
func handleAliasRemove(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	alias := strings.TrimSpace(cmd.Args)
	if alias == "" {
		return pluginsdk.Errorf("Usage: unalias <alias>"), nil
	}

	// Check if alias exists before removing.
	playerAliases, err := proxy.ListPlayerAliases(ctx, cmd.CharacterID)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("unalias: failed to list player aliases: %v", err))
		return pluginsdk.Failuref("Unable to remove alias right now. Please try again."), nil
	}
	if _, ok := findAlias(playerAliases, alias); !ok {
		return pluginsdk.Errorf("No alias '%s' found.", alias), nil
	}

	if err := proxy.DeletePlayerAlias(ctx, cmd.CharacterID, alias); err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("unalias: failed to delete alias %q: %v", alias, err))
		return pluginsdk.Failuref("Unable to remove alias right now. Please try again."), nil
	}

	return pluginsdk.OK(fmt.Sprintf("Alias '%s' removed.\n", alias)), nil
}

// handleAliasList lists all player aliases.
// Usage: aliases
func handleAliasList(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	aliases, err := proxy.ListPlayerAliases(ctx, cmd.CharacterID)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("aliases: failed to list player aliases: %v", err))
		return pluginsdk.Failuref("Unable to list aliases right now. Please try again."), nil
	}

	if len(aliases) == 0 {
		return pluginsdk.OK("You have no aliases defined."), nil
	}

	sort.Slice(aliases, func(i, j int) bool {
		return aliases[i].Alias < aliases[j].Alias
	})

	var out strings.Builder
	fmt.Fprintln(&out, "Your aliases:")
	for _, a := range aliases {
		fmt.Fprintf(&out, "  %s = %s\n", a.Alias, a.Command)
	}

	return pluginsdk.OK(out.String()), nil
}
