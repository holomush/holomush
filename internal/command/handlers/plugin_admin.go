// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/holomush/holomush/internal/command"
	plugins "github.com/holomush/holomush/internal/plugin"
)

const pluginCommandName = "plugin"

// PluginLister provides read-only access to loaded plugin metadata.
// This is the ISP interface for the plugin admin commands.
type PluginLister interface {
	ListPlugins() []string
	GetLoadedPlugin(name string) (*plugins.DiscoveredPlugin, bool)
}

// NewPluginHandler creates a command handler that routes plugin subcommands.
func NewPluginHandler(lister PluginLister) command.CommandHandler {
	return func(ctx context.Context, exec *command.CommandExecution) error {
		return handlePlugin(ctx, exec, lister)
	}
}

func handlePlugin(ctx context.Context, exec *command.CommandExecution, lister PluginLister) error {
	args := strings.TrimSpace(exec.Args)

	switch {
	case args == "list":
		return handlePluginList(ctx, exec, lister)
	case strings.HasPrefix(args, "info "):
		name := strings.TrimSpace(strings.TrimPrefix(args, "info "))
		if name == "" {
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return command.ErrInvalidArgs(pluginCommandName, "plugin info <name>")
		}
		return handlePluginInfo(ctx, exec, lister, name)
	default:
		writeOutput(ctx, exec, pluginCommandName,
			"Usage: plugin list | plugin info <name>")
		return nil
	}
}

func handlePluginList(ctx context.Context, exec *command.CommandExecution, lister PluginLister) error {
	names := lister.ListPlugins()
	if len(names) == 0 {
		writeOutput(ctx, exec, pluginCommandName, "No plugins loaded.")
		return nil
	}

	var sb strings.Builder
	sb.WriteString("Loaded plugins:")
	for _, name := range names {
		dp, ok := lister.GetLoadedPlugin(name)
		if !ok {
			continue
		}
		m := dp.Manifest
		fmt.Fprintf(&sb, "\n  %-24s %-10s %s", m.Name, string(m.Type), m.Version)
	}
	writeOutput(ctx, exec, pluginCommandName, sb.String())
	return nil
}

func handlePluginInfo(ctx context.Context, exec *command.CommandExecution, lister PluginLister, name string) error {
	dp, ok := lister.GetLoadedPlugin(name)
	if !ok {
		//nolint:wrapcheck // ErrTargetNotFound creates a structured oops error
		return command.ErrTargetNotFound(name)
	}

	m := dp.Manifest
	var sb strings.Builder
	fmt.Fprintf(&sb, "Plugin: %s\n", m.Name)
	fmt.Fprintf(&sb, "Version: %s\n", m.Version)
	fmt.Fprintf(&sb, "Type: %s\n", string(m.Type))
	fmt.Fprintf(&sb, "Storage: %s", string(m.Storage))

	if len(m.Requires) > 0 {
		fmt.Fprintf(&sb, "\nRequires: %s", strings.Join(m.RequiresDisplay(), ", "))
	}
	if len(m.Provides) > 0 {
		fmt.Fprintf(&sb, "\nProvides: %s", strings.Join(m.Provides, ", "))
	}
	if len(m.Commands) > 0 {
		cmdNames := make([]string, len(m.Commands))
		for i := range m.Commands {
			cmdNames[i] = m.Commands[i].Name
		}
		fmt.Fprintf(&sb, "\nCommands: %s", strings.Join(cmdNames, ", "))
	}
	if len(m.Verbs) > 0 {
		verbDescs := make([]string, len(m.Verbs))
		for i, v := range m.Verbs {
			verbDescs[i] = fmt.Sprintf("%s (%s/%s)", v.Type, v.Category, v.Format)
		}
		fmt.Fprintf(&sb, "\nVerbs: %s", strings.Join(verbDescs, ", "))
	}

	writeOutput(ctx, exec, pluginCommandName, sb.String())
	return nil
}
