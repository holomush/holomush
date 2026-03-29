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

// whisperPayload mirrors core.WhisperPayload for JSON serialization.
type whisperPayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Message    string `json:"message"`
	IsPose     bool   `json:"is_pose"`
}

// whisperNoticePayload mirrors core.WhisperNoticePayload for JSON serialization.
type whisperNoticePayload struct {
	SenderName string `json:"sender_name"`
	TargetName string `json:"target_name"`
	Notice     string `json:"notice"`
}

// WhisperHandler handles the "whisper" and "w" commands for location-scoped
// private messaging. Ported from the Lua communication plugin.
//
// Syntax:
//   - whisper <name>=<message>   -- whisper to someone
//   - whisper <name>=:<action>   -- whisper-pose
//   - whisper <name>=;<action>   -- no-space whisper-pose
//   - w <message>                -- whisper to last whispered target
type WhisperHandler struct{}

func (h *WhisperHandler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := strings.TrimSpace(cmd.Args)
	if args == "" {
		return &pluginsdk.CommandResponse{
			Output: "Usage: whisper <name>=<message>",
		}, nil
	}

	var targetName, message string

	idx := strings.IndexByte(args, '=')
	if idx > 0 {
		targetName = strings.TrimSpace(args[:idx])
		message = args[idx+1:]
	} else if cmd.InvokedAs == "w" {
		// Short form: use last whispered target.
		senderSession, err := proxy.FindSessionByName(ctx, cmd.CharacterName)
		if err != nil {
			return nil, oops.With("operation", "find_sender_session").Wrap(err)
		}
		if senderSession == nil || senderSession.LastWhispered == "" {
			return &pluginsdk.CommandResponse{
				Output: "Whisper to whom? Use: whisper <name>=<message>",
			}, nil
		}
		targetName = senderSession.LastWhispered
		message = args
	} else {
		return &pluginsdk.CommandResponse{
			Output: "Usage: whisper <name>=<message> or w <message>",
		}, nil
	}

	if targetName == "" {
		return &pluginsdk.CommandResponse{
			Output: "Whisper to whom? Use: whisper <name>=<message>",
		}, nil
	}
	if message == "" {
		return &pluginsdk.CommandResponse{
			Output: "What do you want to whisper?",
		}, nil
	}

	// Find target session.
	target, err := proxy.FindSessionByName(ctx, targetName)
	if err != nil {
		return nil, oops.With("operation", "find_target_session").Wrap(err)
	}
	if target == nil {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("No one named %q is connected.", targetName),
		}, nil
	}

	// Same-location check.
	if target.LocationID != cmd.LocationID {
		return &pluginsdk.CommandResponse{
			Output: fmt.Sprintf("You don't see anyone named %q here.", targetName),
		}, nil
	}

	// Detect pose mode.
	isPose := false
	poseSpace := " "
	if first := message[0]; first == ':' {
		isPose = true
		poseSpace = " "
		message = message[1:]
	} else if first == ';' {
		isPose = true
		poseSpace = ""
		message = message[1:]
	}

	if message == "" {
		return &pluginsdk.CommandResponse{
			Output: "What do you want to whisper?",
		}, nil
	}

	// Build target message.
	var targetMsg string
	if isPose {
		targetMsg = "From nearby, " + cmd.CharacterName + poseSpace + message
	} else {
		targetMsg = cmd.CharacterName + " whispers, \"" + message + "\""
	}

	// Build sender confirmation.
	var senderMsg string
	if isPose {
		senderMsg = "You whisper-pose to " + target.CharacterName + ": " + message
	} else {
		senderMsg = "You whisper to " + target.CharacterName + ": " + message
	}

	// Build notice payload for location (content not revealed).
	noticeData, err := json.Marshal(whisperNoticePayload{
		SenderName: cmd.CharacterName,
		TargetName: target.CharacterName,
		Notice:     cmd.CharacterName + " whispers to " + target.CharacterName + ".",
	})
	if err != nil {
		return nil, oops.With("operation", "marshal_whisper_notice").Wrap(err)
	}

	// Build whisper payload for target.
	whisperData, err := json.Marshal(whisperPayload{
		SenderID:   cmd.CharacterID,
		SenderName: cmd.CharacterName,
		Message:    targetMsg,
		IsPose:     isPose,
	})
	if err != nil {
		return nil, oops.With("operation", "marshal_whisper_payload").Wrap(err)
	}

	// Record last whispered target.
	if cmd.SessionID != "" {
		if setErr := proxy.SetLastWhispered(ctx, cmd.SessionID, target.CharacterName); setErr != nil {
			proxy.Log(ctx, "warn", "whisper: could not update last-whispered state: "+setErr.Error())
		}
	}

	return &pluginsdk.CommandResponse{
		Events: []pluginsdk.EmitEvent{
			{
				Stream:  "location:" + cmd.LocationID,
				Type:    "whisper_notice",
				Payload: string(noticeData),
			},
			{
				Stream:  "character:" + target.CharacterID,
				Type:    "whisper",
				Payload: string(whisperData),
			},
		},
		Output: senderMsg,
	}, nil
}
