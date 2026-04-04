// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package coreobjects provides the core-objects plugin, handling object-related
// commands: describe, examine, create, and set.
package coreobjects

import (
	"context"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"

	plugins "github.com/holomush/holomush/internal/plugin"
)

// Handler implements LocalCommandHandler for all core-objects commands.
type Handler struct{}

// HandleCommand dispatches to the appropriate command handler.
func (h *Handler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	switch cmd.Command {
	case "describe":
		return handleDescribe(ctx, cmd, proxy)
	case "examine":
		return handleExamine(ctx, cmd, proxy)
	case "create":
		return handleCreate(ctx, cmd, proxy)
	case "set":
		return handleSet(ctx, cmd, proxy)
	default:
		return pluginsdk.Errorf("Unknown command: %s", cmd.Command), nil
	}
}
