// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package corebuilding implements the core-building plugin providing dig and link commands.
package corebuilding

import (
	"context"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Handler dispatches building commands to their implementations.
type Handler struct{}

var _ plugins.LocalCommandHandler = (*Handler)(nil)

// HandleCommand routes to the appropriate building command handler.
func (h *Handler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	switch cmd.Command {
	case "dig":
		return handleDig(ctx, cmd, proxy)
	case "link":
		return handleLink(ctx, cmd, proxy)
	default:
		return pluginsdk.Errorf("Unknown building command: %s", cmd.Command), nil
	}
}
