// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"context"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// channelNameResolver resolves a channel name to its persisted row for the
// command layer (name → id). *channelStore satisfies it via GetByName.
type channelNameResolver interface {
	GetByName(ctx context.Context, name string) (*channelRow, error)
}

// HandleCommand routes the `channel` command (and its `=` prefix-alias
// reassembly) to the per-subcommand dispatcher. SKELETON — filled in the GREEN
// commit.
func (p *channelPlugin) HandleCommand(ctx context.Context, req pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	switch req.Command {
	case "channel":
		return p.dispatchChannelCommand(ctx, req)
	default:
		return pluginsdk.Errorf("core-channels does not handle command %q", req.Command), nil
	}
}

func (p *channelPlugin) dispatchChannelCommand(_ context.Context, _ pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	return pluginsdk.Errorf("channel command not implemented"), nil
}
