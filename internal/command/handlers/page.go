// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
	"github.com/holomush/holomush/internal/world"
)

// PageHandler handles the page command for OOC private messaging.
//
// Syntax:
//   - page <character>=<message>   — page someone, set as last-paged
//   - page <message>               — page last-paged character (no = means message to last target)
//   - page <character>=:<action>   — pose-page (colon prefix)
//   - page <character>=;<action>   — no-space pose (semicolon prefix)
func PageHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("page", "page <name>=<message>")
	}

	var targetName, rawMessage string
	var useLastPaged bool

	idx := strings.IndexByte(args, '=')
	if idx > 0 {
		targetName = strings.TrimSpace(args[:idx])
		rawMessage = args[idx+1:] // do NOT trim — leading : or ; is meaningful
	} else {
		// No '=' — page last-paged character with args as message
		rawMessage = args
		useLastPaged = true
	}

	if rawMessage == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("page", "page <name>=<message>")
	}

	// Resolve target name from last-paged if needed.
	if useLastPaged {
		senderSession, err := exec.Services().Session().FindByCharacter(ctx, exec.CharacterID())
		if err != nil {
			return oops.With("operation", "find_sender_session").Wrap(err)
		}
		if senderSession == nil || senderSession.LastPaged == "" {
			writeOutput(ctx, exec, "page", "You have no last-paged character. Use: page <name>=<message>")
			return nil
		}
		targetName = senderSession.LastPaged
	}

	// Look up target session.
	targetSession, err := exec.Services().Session().FindByCharacterName(ctx, targetName)
	if err != nil {
		return oops.With("operation", "find_target_session").Wrap(err)
	}
	if targetSession == nil {
		writeOutputf(ctx, exec, "page", "No one named %q is connected.\n", targetName)
		return nil
	}

	// Determine pose vs. normal message.
	isPose := false
	var formattedForTarget, formattedForSender string

	switch {
	case strings.HasPrefix(rawMessage, ":"):
		// Colon pose: "page alex=:waves" → "From afar, Sean waves."
		action := rawMessage[1:]
		if action == "" {
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return command.ErrInvalidArgs("page", "page <name>=:<action>")
		}
		isPose = true
		formattedForTarget = fmt.Sprintf("From afar, %s %s", exec.CharacterName(), action)
		formattedForSender = fmt.Sprintf("Long distance to %s: %s %s", targetSession.CharacterName, exec.CharacterName(), action)

	case strings.HasPrefix(rawMessage, ";"):
		// Semicolon pose (no space): "page alex=;'s jaw drops" → "From afar, Sean's jaw drops."
		action := rawMessage[1:]
		if action == "" {
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return command.ErrInvalidArgs("page", "page <name>=;<action>")
		}
		isPose = true
		formattedForTarget = fmt.Sprintf("From afar, %s%s", exec.CharacterName(), action)
		formattedForSender = fmt.Sprintf("Long distance to %s: %s%s", targetSession.CharacterName, exec.CharacterName(), action)

	default:
		// Normal message.
		formattedForTarget = fmt.Sprintf("%s pages: %s", exec.CharacterName(), rawMessage)
		formattedForSender = fmt.Sprintf("You paged %s: %s", targetSession.CharacterName, rawMessage)
	}

	// Emit page event to target's character stream.
	pagePayload, err := json.Marshal(core.PagePayload{
		SenderID:   exec.CharacterID().String(),
		SenderName: exec.CharacterName(),
		Message:    formattedForTarget,
		IsPose:     isPose,
	})
	if err != nil {
		return oops.With("operation", "marshal_page_payload").Wrap(err)
	}

	targetCharID := targetSession.CharacterID
	pageEvent := core.Event{
		ID:        core.NewULID(),
		Stream:    world.CharacterStream(targetCharID),
		Type:      core.EventTypePage,
		Timestamp: time.Now(),
		Actor:     core.Actor{Kind: core.ActorCharacter, ID: exec.CharacterID().String()},
		Payload:   pagePayload,
	}
	if err := exec.Services().Events().Append(ctx, pageEvent); err != nil {
		return oops.With("operation", "append_page_event").Wrap(err)
	}

	// Update last-paged on the sender's session.
	if sid := exec.SessionID(); !sid.IsZero() {
		if err := exec.Services().Session().UpdateLastPaged(ctx, sid.String(), targetSession.CharacterName); err != nil {
			// Log but do not fail the command — the page was already delivered.
			writeOutput(ctx, exec, "page", "(Warning: could not update last-paged state.)")
		}
	}

	// Send confirmation to sender.
	writeOutput(ctx, exec, "page", formattedForSender)
	return nil
}
