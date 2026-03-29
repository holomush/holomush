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

// pagePayload mirrors core.PagePayload for JSON serialization.
type pagePayload struct {
	SenderID   string `json:"sender_id"`
	SenderName string `json:"sender_name"`
	Message    string `json:"message"`
	IsPose     bool   `json:"is_pose"`
}

// PageHandler handles the "page" and "p" commands for OOC private messaging.
//
// Syntax:
//   - page <character>=<message>   -- page someone, set as last-paged
//   - page <message>               -- page last-paged character
//   - page <character>=:<action>   -- pose-page (colon prefix)
//   - page <character>=;<action>   -- no-space pose (semicolon prefix)
type PageHandler struct{}

func (h *PageHandler) HandleCommand(ctx context.Context, cmd pluginsdk.CommandRequest, proxy plugins.ServiceProxy) (*pluginsdk.CommandResponse, error) {
	args := strings.TrimSpace(cmd.Args)
	if args == "" {
		return pluginsdk.Errorf("Usage: page <name>=<message>"), nil
	}

	var targetName, rawMessage string
	var useLastPaged bool

	idx := strings.IndexByte(args, '=')
	if idx > 0 {
		targetName = strings.TrimSpace(args[:idx])
		rawMessage = args[idx+1:] // do NOT trim -- leading : or ; is meaningful
	} else if idx == 0 {
		return pluginsdk.Errorf("Usage: page <name>=<message>"), nil
	} else {
		rawMessage = args
		useLastPaged = true
	}

	if strings.TrimSpace(rawMessage) == "" {
		return pluginsdk.Errorf("Usage: page <name>=<message>"), nil
	}

	// Resolve target name from last-paged if needed.
	if useLastPaged {
		senderSession, err := proxy.FindSessionByName(ctx, cmd.CharacterName)
		if err != nil {
			proxy.Log(ctx, "error", fmt.Sprintf("page: failed to find sender session: %v", err))
			return pluginsdk.Failuref("Unable to page right now. Please try again."), nil
		}
		if senderSession == nil || senderSession.LastWhispered == "" {
			return pluginsdk.Errorf("You have no last-paged character. Use: page <name>=<message>"), nil
		}
		targetName = senderSession.LastWhispered
	}

	// Look up target session.
	targetSession, err := proxy.FindSessionByName(ctx, targetName)
	if err != nil {
		proxy.Log(ctx, "error", fmt.Sprintf("page: failed to find session for %q: %v", targetName, err))
		return pluginsdk.Failuref("Unable to reach %q right now. Please try again.", targetName), nil
	}
	if targetSession == nil {
		return pluginsdk.Errorf("No one named %q is connected.", targetName), nil
	}

	// Determine pose vs. normal message.
	isPose := false
	var formattedForTarget, formattedForSender string

	switch {
	case strings.HasPrefix(rawMessage, ":"):
		action := strings.TrimSpace(rawMessage[1:])
		if action == "" {
			return pluginsdk.Errorf("Usage: page <name>=:<action>"), nil
		}
		isPose = true
		formattedForTarget = fmt.Sprintf("From afar, %s %s", cmd.CharacterName, action)
		formattedForSender = fmt.Sprintf("Long distance to %s: %s %s", targetSession.CharacterName, cmd.CharacterName, action)

	case strings.HasPrefix(rawMessage, ";"):
		action := strings.TrimSpace(rawMessage[1:])
		if action == "" {
			return pluginsdk.Errorf("Usage: page <name>=;<action>"), nil
		}
		isPose = true
		formattedForTarget = fmt.Sprintf("From afar, %s%s", cmd.CharacterName, action)
		formattedForSender = fmt.Sprintf("Long distance to %s: %s%s", targetSession.CharacterName, cmd.CharacterName, action)

	default:
		formattedForTarget = fmt.Sprintf("%s pages: %s", cmd.CharacterName, rawMessage)
		formattedForSender = fmt.Sprintf("You paged %s: %s", targetSession.CharacterName, rawMessage)
	}

	// Build page event for target's character stream.
	payload, err := json.Marshal(pagePayload{
		SenderID:   cmd.CharacterID,
		SenderName: cmd.CharacterName,
		Message:    formattedForTarget,
		IsPose:     isPose,
	})
	if err != nil {
		return nil, oops.With("operation", "marshal_page_payload").Wrap(err)
	}

	// Update last-paged on the sender's session.
	if cmd.SessionID != "" {
		if setErr := proxy.SetLastWhispered(ctx, cmd.SessionID, targetSession.CharacterName); setErr != nil {
			// Log but do not fail -- the page event will still be emitted.
			proxy.Log(ctx, "warn", "page: could not update last-paged state: "+setErr.Error())
		}
	}

	return &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Events: []pluginsdk.EmitEvent{
			{
				Stream:  "character:" + targetSession.CharacterID,
				Type:    "page",
				Payload: string(payload),
			},
		},
		Output: formattedForSender,
	}, nil
}
