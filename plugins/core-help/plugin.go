// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package corehelp implements the help command as a core plugin.
package corehelp

import (
	"context"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// Handler implements LocalCommandHandler for the help command.
type Handler struct{}

// Compile-time check.
var _ plugins.LocalCommandHandler = (*Handler)(nil)

// HandleCommand dispatches help subcommands.
func (h *Handler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := trimSpace(cmd.Args)

	if args == "" {
		return listCommands(ctx, cmd, proxy)
	}

	if term, ok := parseSearchTerm(args); ok {
		return searchCommands(ctx, cmd, proxy, term)
	}

	return showCommandHelp(ctx, cmd, proxy, args)
}
