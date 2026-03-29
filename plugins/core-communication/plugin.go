// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package communication implements the core-communication plugin, providing
// all communication commands (say, pose, page, whisper, ooc, pemit, emit, wall)
// as LocalCommandHandler implementations for the LocalPluginHost.
package communication

import (
	"context"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// PluginName is the canonical name matching plugin.yaml.
const PluginName = "core-communication"

// handler dispatches commands to the appropriate handler.
type handler struct {
	handlers map[string]plugins.LocalCommandHandler
}

// NewHandler creates a combined LocalCommandHandler that routes commands
// to the correct sub-handler based on the command name.
func NewHandler() plugins.LocalCommandHandler {
	say := &SayHandler{}
	pose := &PoseHandler{}
	page := &PageHandler{}
	whisper := &WhisperHandler{}
	ooc := &OOCHandler{}
	pemit := &PemitHandler{}
	emit := &EmitHandler{}
	wall := &WallHandler{}

	return &handler{
		handlers: map[string]plugins.LocalCommandHandler{
			"say":     say,
			"pose":    pose,
			"page":    page,
			"p":       page,
			"whisper": whisper,
			"w":       whisper,
			"ooc":     ooc,
			"pemit":   pemit,
			"emit":    emit,
			"wall":    wall,
		},
	}
}

// HandleCommand routes to the appropriate sub-handler.
func (h *handler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	sub, ok := h.handlers[cmd.Command]
	if !ok {
		proxy.Log(ctx, "error", "communication: unsupported routed command: "+cmd.Command)
		return pluginsdk.Failuref("That command is temporarily unavailable. Please try again later."), nil
	}
	return sub.HandleCommand(ctx, cmd, proxy)
}
