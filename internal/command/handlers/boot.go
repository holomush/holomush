// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
)

// BootHandler disconnects a target player from the server.
// Self-boot: allowed for all users (like "quit with reason").
// Boot others: requires admin.boot capability.
// Usage: boot <player> [reason]
func BootHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("boot", "boot <player> [reason]")
	}

	// Parse target name and optional reason
	parts := strings.SplitN(args, " ", 2)
	targetName := parts[0]
	var reason string
	if len(parts) > 1 {
		reason = parts[1]
	}

	// Find the target session by character name
	subjectID := "char:" + exec.CharacterID.String()
	targetCharID, targetCharName, err := findCharacterByName(ctx, exec, subjectID, targetName)
	if err != nil {
		return err
	}

	// Check if this is a self-boot (allowed for all users)
	isSelfBoot := targetCharID == exec.CharacterID

	// Boot others requires admin.boot capability
	if !isSelfBoot {
		allowed := exec.Services.Access.Check(ctx, subjectID, "execute", "admin.boot")
		if !allowed {
			//nolint:wrapcheck // ErrPermissionDenied creates a structured oops error
			return command.ErrPermissionDenied("boot", "admin.boot")
		}
	}

	// Notify the target before disconnecting them
	if exec.Services.Broadcaster != nil {
		notifyTargetOfBoot(exec, targetCharID, exec.CharacterName, reason, isSelfBoot)
	}

	// End the target's session
	if err := exec.Services.Session.EndSession(targetCharID); err != nil {
		return oops.Code(command.CodeWorldError).
			With("message", "Unable to boot player. Session may have already ended.").
			Wrap(err)
	}

	// Log admin boots (but not self-boots)
	if !isSelfBoot {
		slog.Info("admin boot",
			"admin_id", exec.CharacterID.String(),
			"admin_name", exec.CharacterName,
			"target_id", targetCharID.String(),
			"target_name", targetCharName,
			"reason", reason,
		)
	}

	// Notify the executor
	//nolint:errcheck // output write error is acceptable; player display is best-effort
	switch {
	case isSelfBoot:
		_, _ = fmt.Fprintln(exec.Output, "Disconnecting...")
	case reason != "":
		_, _ = fmt.Fprintf(exec.Output, "%s has been booted. Reason: %s\n", targetCharName, reason)
	default:
		_, _ = fmt.Fprintf(exec.Output, "%s has been booted.\n", targetCharName)
	}

	return nil
}

// notifyTargetOfBoot sends a system event to the target player's session stream
// to notify them they are being booted.
func notifyTargetOfBoot(exec *command.CommandExecution, targetCharID ulid.ULID, adminName, reason string, isSelfBoot bool) {
	var message string
	if isSelfBoot {
		if reason != "" {
			message = fmt.Sprintf("Disconnecting: %s", reason)
		} else {
			message = "Disconnecting..."
		}
	} else {
		if reason != "" {
			message = fmt.Sprintf("You have been disconnected by %s. Reason: %s", adminName, reason)
		} else {
			message = fmt.Sprintf("You have been disconnected by %s.", adminName)
		}
	}

	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{
		"message": message,
	})

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "session:" + targetCharID.String(),
		Type:      core.EventTypeSystem,
		Timestamp: time.Now(),
		Actor: core.Actor{
			Kind: core.ActorSystem,
			ID:   "system",
		},
		Payload: payload,
	}

	exec.Services.Broadcaster.Broadcast(event)
}

// findCharacterByName searches active sessions for a character with the given name.
// Returns the character ID and name if found, or an error if not found.
func findCharacterByName(ctx context.Context, exec *command.CommandExecution, subjectID, targetName string) (ulid.ULID, string, error) {
	sessions := exec.Services.Session.ListActiveSessions()

	for _, session := range sessions {
		// Get character info for this session
		char, err := exec.Services.World.GetCharacter(ctx, subjectID, session.CharacterID)
		if err != nil {
			// Skip inaccessible or missing characters
			continue
		}

		// Case-insensitive name match
		if strings.EqualFold(char.Name, targetName) {
			return char.ID, char.Name, nil
		}
	}

	//nolint:wrapcheck // ErrTargetNotFound creates a structured oops error
	return ulid.ULID{}, "", command.ErrTargetNotFound(targetName)
}
