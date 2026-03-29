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

// posePayload mirrors core.PosePayload for JSON serialization.
type posePayload struct {
	CharacterName string `json:"character_name"`
	Action        string `json:"action"`
	NoSpace       bool   `json:"no_space,omitempty"`
}

// PoseHandler handles the "pose" command by emitting a pose event to the
// character's current location stream.
type PoseHandler struct{}

func (h *PoseHandler) HandleCommand(_ context.Context, cmd pluginsdk.CommandRequest, _ plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	action := strings.TrimSpace(cmd.Args)
	if action == "" {
		return pluginsdk.Errorf("What do you want to pose?"), nil
	}

	payload, err := json.Marshal(posePayload{
		CharacterName: cmd.CharacterName,
		Action:        action,
		NoSpace:       cmd.InvokedAs == ";",
	})
	if err != nil {
		return nil, oops.With("operation", "marshal_pose_payload").Wrap(err)
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Events: []pluginsdk.EmitEvent{
			{
				Stream:  "location:" + cmd.LocationID,
				Type:    pluginsdk.EventTypePose,
				Payload: string(payload),
			},
		},
	}, nil
}
