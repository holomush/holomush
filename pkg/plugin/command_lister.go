// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package pluginsdk

import (
	"context"

	"github.com/samber/oops"

	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// CommandSummary is one command's metadata as seen by a binary plugin.
type CommandSummary struct {
	Name, Help, Usage, Source string
}

// CommandList is the result of CommandLister.ListCommands.
type CommandList struct {
	Commands   []CommandSummary
	Incomplete bool
}

// CommandLister is the SDK facade binary plugins use to enumerate the commands a
// character may execute (parity with the Lua holomush.list_commands host function).
type CommandLister interface {
	ListCommands(ctx context.Context, characterID string) (CommandList, error)
}

// CommandListerAware is the optional interface a service provider implements to
// receive a CommandLister during Init, parallel to HostEvaluatorAware.
type CommandListerAware interface {
	SetCommandLister(CommandLister)
}

type hostCommandClient struct {
	client pluginv1.PluginHostServiceClient
}

// ListCommands implements CommandLister. A nil client fails closed.
func (c *hostCommandClient) ListCommands(ctx context.Context, characterID string) (CommandList, error) {
	if c.client == nil {
		return CommandList{}, oops.New("host command lister client is not configured")
	}
	resp, err := c.client.ListCommands(ctx, &pluginv1.PluginHostServiceListCommandsRequest{CharacterId: characterID})
	if err != nil {
		return CommandList{}, oops.With("character_id", characterID).Wrap(err)
	}
	out := make([]CommandSummary, 0, len(resp.GetCommands()))
	for _, ci := range resp.GetCommands() {
		out = append(out, CommandSummary{Name: ci.GetName(), Help: ci.GetHelp(), Usage: ci.GetUsage(), Source: ci.GetSource()})
	}
	return CommandList{Commands: out, Incomplete: resp.GetIncomplete()}, nil
}
