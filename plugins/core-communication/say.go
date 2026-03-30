// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package communication

import (
	"context"
	"encoding/json"

	"github.com/samber/oops"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// sayPayload mirrors core.SayPayload for JSON serialization.
type sayPayload struct {
	CharacterName string `json:"character_name"`
	Message       string `json:"message"`
}

// SayHandler handles the "say" command by emitting a say event to the
// character's current location stream.
type SayHandler struct{}

func (h *SayHandler) HandleCommand(_ context.Context, cmd pluginsdk.CommandRequest, _ plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	payload, err := json.Marshal(sayPayload{
		CharacterName: cmd.CharacterName,
		Message:       cmd.Args,
	})
	if err != nil {
		return nil, oops.With("operation", "marshal_say_payload").Wrap(err)
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Events: []pluginsdk.EmitEvent{
			{
				Stream:  "location:" + cmd.LocationID,
				Type:    pluginsdk.EventTypeSay,
				Payload: string(payload),
			},
		},
	}, nil
}
