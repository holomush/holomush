// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package corealiases implements the core-aliases plugin, providing personal
// and system alias management commands (alias, unalias, aliases, sysalias,
// sysunsalias, sysaliases).
package corealiases

import (
	"context"
	"fmt"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Handler dispatches alias commands to the appropriate sub-handler.
type Handler struct{}

// HandleCommand routes commands to their implementations.
func (h *Handler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	switch cmd.Command {
	case "alias":
		return handleAliasAdd(ctx, cmd, proxy)
	case "unalias":
		return handleAliasRemove(ctx, cmd, proxy)
	case "aliases":
		return handleAliasList(ctx, cmd, proxy)
	case "sysalias":
		return handleSysaliasAdd(ctx, cmd, proxy)
	case "sysunsalias":
		return handleSysaliasRemove(ctx, cmd, proxy)
	case "sysaliases":
		return handleSysaliasList(ctx, proxy)
	default:
		proxy.Log(ctx, "error", fmt.Sprintf("core-aliases: unknown routed command %q", cmd.Command))
		return pluginsdk.Failuref("An internal error occurred processing your request."), nil
	}
}
