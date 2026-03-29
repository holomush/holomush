// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package communication

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/samber/oops"

	plugins "github.com/holomush/holomush/internal/plugin"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// pemitPayload mirrors core.PemitPayload for JSON serialization.
type pemitPayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	TargetID   string `json:"target_id"`
	Message    string `json:"message"`
}

// PemitHandler handles the "pemit" command for private GM narration.
//
// Syntax: pemit <character>=<message>
//
// Emits a pemit event on the target's character stream. The target sees the
// raw message text; the sender receives a confirmation.
type PemitHandler struct{}

func (h *PemitHandler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := strings.TrimSpace(cmd.Args)

	idx := strings.IndexByte(args, '=')
	if idx <= 0 {
		return &pluginsdk.CommandResponse{
			Output: "Usage: pemit <character>=<message>",
		}, nil
	}

	targetName := strings.TrimSpace(args[:idx])
	message := args[idx+1:]

	if message == "" {
		return &pluginsdk.CommandResponse{
			Output: "Usage: pemit <character>=<message>",
		}, nil
	}

	// Resolve target session by character name.
	targetSession, err := proxy.FindSessionByName(ctx, targetName)
	if err != nil {
		return nil, oops.With("operation", "find_target_session").Wrap(err)
	}
	if targetSession == nil {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("No character found named %q.", targetName),
		}, nil
	}

	payload, err := json.Marshal(pemitPayload{
		SenderID:   cmd.CharacterID,
		SenderName: cmd.CharacterName,
		TargetID:   targetSession.CharacterID,
		Message:    message,
	})
	if err != nil {
		return nil, oops.With("operation", "marshal_pemit_payload").Wrap(err)
	}

	return &pluginsdk.CommandResponse{
		Events: []pluginsdk.EmitEvent{
			{
				Stream:  "character:" + targetSession.CharacterID,
				Type:    "pemit",
				Payload: string(payload),
			},
		},
		Output: fmt.Sprintf("Pemit sent to %s.", targetSession.CharacterName),
	}, nil
}
