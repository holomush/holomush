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

// handleSysaliasAdd creates or updates a system alias.
// Usage: sysalias <name>=<command>
// Blocks if shadowing an existing system alias; warns on command shadow.
func handleSysaliasAdd(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	alias, command, err := parseAliasDefinition(cmd.Args)
	if err != nil {
		return pluginsdk.Errorf("%s", err.Error()), nil
	}

	if err := validateAliasName(alias); err != nil {
		return pluginsdk.Errorf("%s", err.Error()), nil
	}

	// Block if shadowing an existing system alias.
	sysAliases, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("sysalias: failed to list system aliases: %v", err))
		return pluginsdk.Failuref("Unable to create system alias right now. Please try again."), nil
	}
	if existingCmd, ok := findAlias(sysAliases, alias); ok {
		return pluginsdk.Errorf("'%s' shadows existing system alias for '%s'. Use 'sysunsalias %s' first.", alias, existingCmd, alias), nil
	}

	var warnings []string

	// Check if alias shadows a registered command.
	shadows, _, err := proxy.CheckAliasShadow(ctx, alias)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("sysalias: failed to check shadow for %q: %v", alias, err))
		return pluginsdk.Failuref("Unable to create system alias right now. Please try again."), nil
	}
	if shadows {
		warnings = append(warnings, fmt.Sprintf("Warning: '%s' is an existing command. System alias will override it.", alias))
	}

	// Re-check inside the write path to prevent TOCTOU races: if another admin
	// created the alias between our preflight and this point, SetSystemAlias
	// still succeeds (it upserts). Re-fetch and reject if the alias now exists.
	sysAliases2, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("sysalias: failed to re-check system aliases: %v", err))
		return pluginsdk.Failuref("Unable to create system alias right now. Please try again."), nil
	}
	if existingCmd, ok := findAlias(sysAliases2, alias); ok {
		return pluginsdk.Errorf("'%s' shadows existing system alias for '%s'. Use 'sysunsalias %s' first.", alias, existingCmd, alias), nil
	}

	// Set the system alias (proxy handles DB + cache).
	if err := proxy.SetSystemAlias(ctx, alias, command, cmd.CharacterID); err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("sysalias: failed to set system alias %q: %v", alias, err))
		return pluginsdk.Failuref("Unable to create system alias right now. Please try again."), nil
	}

	var out strings.Builder
	for _, w := range warnings {
		fmt.Fprintln(&out, w)
	}
	fmt.Fprintf(&out, "System alias '%s' added: %s\n", alias, command)

	return pluginsdk.OK(out.String()), nil
}

// handleSysaliasRemove removes a system alias.
// Usage: sysunsalias <name>
func handleSysaliasRemove(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	alias := strings.TrimSpace(cmd.Args)
	if alias == "" {
		return pluginsdk.Errorf("Usage: sysunsalias <alias>"), nil
	}

	// Check if alias exists before removing.
	sysAliases, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("sysunsalias: failed to list system aliases: %v", err))
		return pluginsdk.Failuref("Unable to remove system alias right now. Please try again."), nil
	}
	if _, ok := findAlias(sysAliases, alias); !ok {
		return pluginsdk.Errorf("No system alias '%s' found.", alias), nil
	}

	if err := proxy.DeleteSystemAlias(ctx, alias); err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("sysunsalias: failed to delete system alias %q: %v", alias, err))
		return pluginsdk.Failuref("Unable to remove system alias right now. Please try again."), nil
	}

	return pluginsdk.OK(fmt.Sprintf("System alias '%s' removed.\n", alias)), nil
}

// handleSysaliasList lists all system aliases.
// Usage: sysaliases
func handleSysaliasList(ctx context.Context, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	aliases, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("sysaliases: failed to list system aliases: %v", err))
		return pluginsdk.Failuref("Unable to list system aliases right now. Please try again."), nil
	}

	if len(aliases) == 0 {
		return pluginsdk.OK("No system aliases defined."), nil
	}

	sort.Slice(aliases, func(i, j int) bool {
		return aliases[i].Alias < aliases[j].Alias
	})

	var out strings.Builder
	fmt.Fprintln(&out, "System aliases:")
	for _, a := range aliases {
		fmt.Fprintf(&out, "  %s = %s\n", a.Alias, a.Command)
	}

	return pluginsdk.OK(out.String()), nil
}
