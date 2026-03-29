// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package corehelp

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/holomush/holomush/pkg/holo"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// listCommands returns a grouped table of all available commands.
func listCommands(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	commands, err := proxy.ListCommands(ctx, cmd.CharacterID)
	if err != nil {
		return &pluginsdk.CommandResponse{
			Output: "Error listing commands: " + err.Error(),
		}, nil
	}

	if len(commands) == 0 {
		return &pluginsdk.CommandResponse{
			Output: "No commands available.",
		}, nil
	}

	// Group by source.
	bySource := make(map[string][]plugins.CommandInfo)
	for _, c := range commands {
		src := c.Source
		if src == "" {
			src = "other"
		}
		bySource[src] = append(bySource[src], c)
	}

	// Sort source names for stable output.
	sources := make([]string, 0, len(bySource))
	for src := range bySource {
		sources = append(sources, src)
	}
	sort.Strings(sources)

	// Build output.
	var out strings.Builder
	out.WriteString(holo.Fmt.Header("Available Commands").RenderANSI())
	out.WriteString("\n\n")

	for _, src := range sources {
		cmds := bySource[src]
		sort.SliceStable(cmds, func(i, j int) bool {
			return cmds[i].Name < cmds[j].Name
		})
		out.WriteString(holo.Fmt.Bold(capitalize(src)).RenderANSI())
		out.WriteString("\n")

		rows := make([][]string, 0, len(cmds))
		for _, c := range cmds {
			rows = append(rows, []string{c.Name, c.Help})
		}
		out.WriteString(holo.Fmt.Table(holo.TableOpts{
			Headers: []string{"Command", "Description"},
			Rows:    rows,
		}).RenderANSI())
		out.WriteString("\n\n")
	}

	out.WriteString(holo.Fmt.Dim("Type 'help <command>' for detailed help.").RenderANSI())

	return &pluginsdk.CommandResponse{Output: out.String()}, nil
}

// showCommandHelp returns detailed help for a single command.
func showCommandHelp(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, name string) (*pluginsdk.CommandResponse, error) {
	info, err := proxy.GetCommandHelp(ctx, name, cmd.CharacterID)
	if err != nil {
		return &pluginsdk.CommandResponse{
			Output: "Error getting help: " + err.Error(),
		}, nil
	}
	if info == nil {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("Unknown command: %s\nType 'help' to see available commands.", name),
		}, nil
	}

	var out strings.Builder
	out.WriteString(holo.Fmt.Header(info.Name).RenderANSI())
	out.WriteString("\n\n")

	if info.Help != "" {
		out.WriteString(info.Help)
		out.WriteString("\n\n")
	}

	if info.Usage != "" {
		out.WriteString(holo.Fmt.Bold("Usage: ").RenderANSI())
		out.WriteString(info.Usage)
		out.WriteString("\n\n")
	}

	if info.HelpText != "" {
		out.WriteString(info.HelpText)
		out.WriteString("\n")
	}

	if info.Source != "" {
		out.WriteString(holo.Fmt.Dim("Source: " + info.Source).RenderANSI())
	}

	return &pluginsdk.CommandResponse{Output: out.String()}, nil
}

// searchCommands filters all commands by a search term.
func searchCommands(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy, term string) (*pluginsdk.CommandResponse, error) {
	commands, err := proxy.ListCommands(ctx, cmd.CharacterID)
	if err != nil {
		return &pluginsdk.CommandResponse{
			Output: "Error searching commands: " + err.Error(),
		}, nil
	}

	lower := strings.ToLower(term)
	var matches []plugins.CommandInfo
	for _, c := range commands {
		if strings.Contains(strings.ToLower(c.Name), lower) ||
			strings.Contains(strings.ToLower(c.Help), lower) {
			matches = append(matches, c)
		}
	}

	sort.SliceStable(matches, func(i, j int) bool {
		return matches[i].Name < matches[j].Name
	})

	if len(matches) == 0 {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("No commands found matching '%s'.", term),
		}, nil
	}

	var out strings.Builder
	out.WriteString(holo.Fmt.Header(fmt.Sprintf("Search Results for '%s'", term)).RenderANSI())
	out.WriteString("\n\n")

	rows := make([][]string, 0, len(matches))
	for _, c := range matches {
		rows = append(rows, []string{c.Name, c.Help})
	}
	out.WriteString(holo.Fmt.Table(holo.TableOpts{
		Headers: []string{"Command", "Description"},
		Rows:    rows,
	}).RenderANSI())
	out.WriteString("\n\n")

	out.WriteString(holo.Fmt.Dim(fmt.Sprintf("Found %d command(s).", len(matches))).RenderANSI())

	return &pluginsdk.CommandResponse{Output: out.String()}, nil
}

// trimSpace trims leading/trailing whitespace.
func trimSpace(s string) string {
	return strings.TrimSpace(s)
}

// parseSearchTerm extracts the term from "search <term>".
func parseSearchTerm(args string) (string, bool) {
	if !strings.HasPrefix(strings.ToLower(args), "search ") {
		return "", false
	}
	term := strings.TrimSpace(args[len("search "):])
	if term == "" {
		return "", false
	}
	return term, true
}

// capitalize upper-cases the first letter of a string (rune-safe).
func capitalize(s string) string {
	if s == "" {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
