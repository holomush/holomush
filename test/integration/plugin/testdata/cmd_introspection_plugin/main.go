// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package main implements the cmd_introspection_plugin: a test-only binary
// plugin used by command_introspection_parity_test.go to prove INV-COMMAND-2
// (runtime parity): the binary PluginHostService.ListCommands and the Lua
// holomush.list_commands host function delegate to the same
// commandquery.Querier and return identical filtered command-name sets.
//
// The plugin implements CommandListerAware to receive the CommandLister
// SDK facade during Init. On a trigger event, the plugin calls
// commandLister.ListCommands(ctx, character_id) and RETURNS a single
// EmitEvent whose payload is a JSON CmdListResult document with sorted
// command names and the incomplete flag.
//
// Using the HandleEvent return-value path (rather than sink.Emit) avoids
// the gRPC-back-through-broker round trip for emitting and lets the test
// read the result directly from host.DeliverEvent's return value.
//
// Trigger payload schema (TriggerPayload JSON):
//
//	{"character_id": "<ULID>", "emit_subject": "<subject string>"}
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"google.golang.org/grpc"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
	pluginv1 "github.com/holomush/holomush/pkg/proto/holomush/plugin/v1"
)

// TriggerPayload is the event payload the test sends to drive the plugin.
type TriggerPayload struct {
	CharacterID string `json:"character_id"`
	EmitSubject string `json:"emit_subject"`
}

// CmdListResult is the result payload the plugin returns as an EmitEvent.
type CmdListResult struct {
	Names      []string `json:"names"`
	Incomplete bool     `json:"incomplete"`
}

type cmdIntrospectionPlugin struct {
	commandLister pluginsdk.CommandLister
}

// SetCommandLister captures the SDK-injected CommandLister.
func (p *cmdIntrospectionPlugin) SetCommandLister(cl pluginsdk.CommandLister) {
	p.commandLister = cl
}

// HandleEvent is called on each incoming event. The plugin parses the
// TriggerPayload, calls ListCommands for the given character, and returns
// a single EmitEvent carrying the CmdListResult JSON payload. The return-
// value emit path avoids the need for an EventSink round-trip back to the
// host (no token complexity, no extra gRPC call).
func (p *cmdIntrospectionPlugin) HandleEvent(ctx context.Context, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	if p.commandLister == nil {
		return nil, fmt.Errorf("cmd_introspection_plugin: commandLister not injected")
	}

	var trigger TriggerPayload
	if err := json.Unmarshal([]byte(event.Payload), &trigger); err != nil {
		return nil, fmt.Errorf("cmd_introspection_plugin: malformed trigger payload: %w", err)
	}

	list, err := p.commandLister.ListCommands(ctx, trigger.CharacterID)
	if err != nil {
		return nil, fmt.Errorf("cmd_introspection_plugin: ListCommands failed: %w", err)
	}

	names := make([]string, 0, len(list.Commands))
	for _, c := range list.Commands {
		names = append(names, c.Name)
	}
	sort.Strings(names)

	result := CmdListResult{Names: names, Incomplete: list.Incomplete}
	payload, err := json.Marshal(result)
	if err != nil {
		return nil, fmt.Errorf("cmd_introspection_plugin: marshal result: %w", err)
	}

	// Return the result via the HandleEvent return-value path. The host
	// collects these and returns them to host.DeliverEvent's caller. The test
	// reads the payload directly from the returned EmitEvent slice, avoiding
	// the EventSink gRPC round-trip entirely.
	return []pluginsdk.EmitEvent{
		{
			Stream:  trigger.EmitSubject,
			Type:    pluginsdk.EventType("location.cmd-introspection-result"),
			Payload: string(payload),
		},
	}, nil
}

// RegisterServices is a no-op — cmd_introspection_plugin provides no gRPC
// services beyond HandleEvent + CommandLister injection.
func (p *cmdIntrospectionPlugin) RegisterServices(_ grpc.ServiceRegistrar) {}

// Init is a no-op; this plugin needs no DB or external service wiring.
func (p *cmdIntrospectionPlugin) Init(_ context.Context, _ *pluginv1.ServiceConfig) error {
	return nil
}

func main() {
	plugin := &cmdIntrospectionPlugin{}
	pluginsdk.ServeWithServices(
		&pluginsdk.ServeConfig{Handler: plugin},
		plugin,
	)
}
