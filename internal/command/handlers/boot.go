// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/access/policy/types"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// BootHandler disconnects a target player from the server.
// Self-boot bypasses the admin.boot capability check (implemented in handler),
// allowing any user to boot themselves (like "quit with reason").
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
	subjectID := access.SubjectCharacter + exec.CharacterID().String()
	targetCharID, targetCharName, err := findCharacterByName(ctx, exec, subjectID, targetName)
	if err != nil {
		return err
	}

	// Check if this is a self-boot (allowed for all users)
	isSelfBoot := targetCharID == exec.CharacterID()

	// Boot others requires admin.boot capability
	if !isSelfBoot {
		decision, evalErr := exec.Services().Engine().Evaluate(ctx, types.AccessRequest{
			Subject:  subjectID,
			Action:   "execute",
			Resource: "admin.boot",
		})
		if evalErr != nil {
			slog.ErrorContext(ctx, "boot access evaluation failed",
				"subject", subjectID,
				"action", "execute",
				"resource", "admin.boot",
				"error", evalErr,
			)
		}
		if evalErr != nil || !decision.IsAllowed() {
			//nolint:wrapcheck // ErrPermissionDenied creates a structured oops error
			return command.ErrPermissionDenied("boot", "admin.boot")
		}
	}

	// Notify the target before disconnecting them
	message := formatBootMessage(exec.CharacterName(), reason, isSelfBoot)
	stream := "session:" + targetCharID.String()
	exec.Services().BroadcastSystemMessage(stream, message)

	// End the target's session
	if err := exec.Services().Session().EndSession(targetCharID); err != nil {
		return oops.Code(command.CodeWorldError).
			With("message", "Unable to boot player. Session may have already ended.").
			Wrap(err)
	}

	// Log admin boots (but not self-boots)
	if !isSelfBoot {
		slog.Info("admin boot",
			"admin_id", exec.CharacterID().String(),
			"admin_name", exec.CharacterName(),
			"target_id", targetCharID.String(),
			"target_name", targetCharName,
			"reason", reason,
		)
	}

	// Notify the executor - output write errors are logged but don't fail the boot
	switch {
	case isSelfBoot:
		writeOutput(ctx, exec, "boot", "Disconnecting...")
	case reason != "":
		writeOutputf(ctx, exec, "boot", "%s has been booted. Reason: %s\n", targetCharName, reason)
	default:
		writeOutputf(ctx, exec, "boot", "%s has been booted.\n", targetCharName)
	}

	return nil
}

// formatBootMessage creates the appropriate boot notification message.
func formatBootMessage(adminName, reason string, isSelfBoot bool) string {
	if isSelfBoot {
		if reason != "" {
			return fmt.Sprintf("Disconnecting: %s", reason)
		}
		return "Disconnecting..."
	}
	if reason != "" {
		return fmt.Sprintf("You have been disconnected by %s. Reason: %s", adminName, reason)
	}
	return fmt.Sprintf("You have been disconnected by %s.", adminName)
}

// findCharacterByName searches active sessions for a character with the given name.
// Returns the character ID and name if found, or an error if not found.
// If unexpected errors occur during search (database failures, timeouts), returns a
// system error instead of "not found" to avoid misleading the user.
func findCharacterByName(ctx context.Context, exec *command.CommandExecution, subjectID, targetName string) (ulid.ULID, string, error) {
	sessions := exec.Services().Session().ListActiveSessions()

	var errorCount int

	for _, session := range sessions {
		// Get character info for this session
		char, err := exec.Services().World().GetCharacter(ctx, subjectID, session.CharacterID)
		if err != nil {
			// Skip expected errors (not found, permission denied)
			if errors.Is(err, world.ErrNotFound) || errors.Is(err, world.ErrPermissionDenied) {
				continue
			}
			// Track unexpected errors (database failures, timeouts, etc.) but continue searching
			errorCount++
			slog.ErrorContext(ctx, "unexpected error looking up character",
				"target_name", targetName,
				"session_char_id", session.CharacterID.String(),
				"error", err,
			)
			continue
		}

		// Case-insensitive name match
		if strings.EqualFold(char.Name, targetName) {
			return char.ID, char.Name, nil
		}
	}

	// If unexpected errors occurred and no match was found, report system error
	// rather than "not found" to avoid misleading the user.
	// Note: We don't wrap lastErr because oops preserves the inner error's code,
	// and we need WORLD_ERROR code for PlayerMessage to return our custom message.
	if errorCount > 0 {
		//nolint:wrapcheck // WorldError creates a structured oops error
		return ulid.ULID{}, "", command.WorldError("Unable to search for player due to system error. Try again.", nil)
	}

	//nolint:wrapcheck // ErrTargetNotFound creates a structured oops error
	return ulid.ULID{}, "", command.ErrTargetNotFound(targetName)
}
