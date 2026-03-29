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
		return nil, err
	}

	if err := validateAliasName(alias); err != nil {
		return nil, err
	}

	// Block if shadowing an existing system alias.
	sysAliases, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("checking system aliases: %w", err)
	}
	if existingCmd, ok := findAlias(sysAliases, alias); ok {
		return nil, fmt.Errorf("'%s' shadows existing system alias for '%s'. Use 'sysunsalias %s' first", alias, existingCmd, alias)
	}

	var warnings []string

	// Check if alias shadows a registered command.
	shadows, _, err := proxy.CheckAliasShadow(ctx, alias)
	if err != nil {
		return nil, fmt.Errorf("checking alias shadow: %w", err)
	}
	if shadows {
		warnings = append(warnings, fmt.Sprintf("Warning: '%s' is an existing command. System alias will override it.", alias))
	}

	// Set the system alias (proxy handles DB + cache).
	if err := proxy.SetSystemAlias(ctx, alias, command, cmd.CharacterID); err != nil {
		return nil, err //nolint:wrapcheck // proxy returns structured errors
	}

	var out strings.Builder
	for _, w := range warnings {
		fmt.Fprintln(&out, w)
	}
	fmt.Fprintf(&out, "System alias '%s' added: %s\n", alias, command)

	return &pluginsdk.CommandResponse{Output: out.String()}, nil
}

// handleSysaliasRemove removes a system alias.
// Usage: sysunsalias <name>
func handleSysaliasRemove(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	alias := strings.TrimSpace(cmd.Args)
	if alias == "" {
		return nil, fmt.Errorf("usage: sysunsalias <alias>")
	}

	// Check if alias exists before removing.
	sysAliases, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing system aliases: %w", err)
	}
	if _, ok := findAlias(sysAliases, alias); !ok {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("No system alias '%s' found.\n", alias),
		}, nil
	}

	if err := proxy.DeleteSystemAlias(ctx, alias); err != nil {
		return nil, err //nolint:wrapcheck // proxy returns structured errors
	}

	return &pluginsdk.CommandResponse{
		Output: fmt.Sprintf("System alias '%s' removed.\n", alias),
	}, nil
}

// handleSysaliasList lists all system aliases.
// Usage: sysaliases
func handleSysaliasList(ctx context.Context, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	aliases, err := proxy.ListSystemAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing system aliases: %w", err)
	}

	if len(aliases) == 0 {
		return &pluginsdk.CommandResponse{Output: "No system aliases defined."}, nil
	}

	sort.Slice(aliases, func(i, j int) bool {
		return aliases[i].Alias < aliases[j].Alias
	})

	var out strings.Builder
	fmt.Fprintln(&out, "System aliases:")
	for _, a := range aliases {
		fmt.Fprintf(&out, "  %s = %s\n", a.Alias, a.Command)
	}

	return &pluginsdk.CommandResponse{Output: out.String()}, nil
}
