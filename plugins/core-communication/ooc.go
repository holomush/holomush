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

// oocPayload mirrors core.OOCPayload for JSON serialization.
type oocPayload struct {
	CharacterName string `json:"character_name"`
	Message       string `json:"message"`
	Style         string `json:"style"`
}

// OOCHandler handles the "ooc" command by emitting an OOC event to the
// character's current location stream.
type OOCHandler struct{}

func (h *OOCHandler) HandleCommand(_ context.Context, cmd pluginsdk.CommandRequest, _ plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	msg := strings.TrimSpace(cmd.Args)
	if msg == "" {
		return &pluginsdk.CommandResponse{
			Output: "Usage: ooc <message>",
		}, nil
	}

	var style, text string
	switch {
	case strings.HasPrefix(msg, ":"):
		style = "pose"
		text = msg[1:]
	case strings.HasPrefix(msg, ";"):
		style = "semipose"
		text = msg[1:]
	default:
		style = "say"
		text = msg
	}

	payload, err := json.Marshal(oocPayload{
		CharacterName: cmd.CharacterName,
		Message:       text,
		Style:         style,
	})
	if err != nil {
		return nil, oops.With("operation", "marshal_ooc_payload").Wrap(err)
	}

	return &pluginsdk.CommandResponse{
		Events: []pluginsdk.EmitEvent{
			{
				Stream:  "location:" + cmd.LocationID,
				Type:    "ooc",
				Payload: string(payload),
			},
		},
	}, nil
}
