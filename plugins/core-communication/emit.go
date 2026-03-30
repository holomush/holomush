// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package communication

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// emitPayload is the JSON payload for emit events.
type emitPayload struct {
	Message string `json:"message"`
}

// EmitHandler handles the "emit" command by emitting arbitrary text to
// the character's current location stream. Ported from the Lua
// communication plugin.
type EmitHandler struct{}

func (h *EmitHandler) HandleCommand(_ context.Context, cmd pluginsdk.CommandRequest, _ plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	msg := strings.TrimSpace(cmd.Args)
	if msg == "" {
		return pluginsdk.Errorf("What do you want to emit?"), nil
	}

	payload, err := json.Marshal(emitPayload{
		Message: msg,
	})
	if err != nil {
		return nil, oops.With("operation", "marshal_emit_payload").Wrap(err)
	}

	if cmd.LocationID == "" || cmd.LocationID == "00000000000000000000000000" {
		return pluginsdk.Errorf("You must be in a location to emit."), nil
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Events: []pluginsdk.EmitEvent{
			{
				Stream:  "location:" + cmd.LocationID,
				Type:    pluginsdk.EventTypeEmit,
				Payload: string(payload),
			},
		},
	}, nil
}
