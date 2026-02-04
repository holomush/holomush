// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"strings"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// MoveHandler navigates the character through an exit in the given direction.
// The direction is matched case-insensitively against exit names and aliases.
func MoveHandler(ctx context.Context, exec *command.CommandExecution) error {
	direction := strings.TrimSpace(exec.Args)
	if direction == "" {
		return oops.Code(command.CodeInvalidArgs).
			With("command", "move").
			With("usage", "move <direction>").
			Errorf("no direction specified")
	}

	subjectID := "char:" + exec.CharacterID.String()

	// Get exits from current location
	exits, err := exec.Services.World.GetExitsByLocation(ctx, subjectID, exec.LocationID)
	if err != nil {
		return oops.Code(command.CodeWorldError).
			With("message", "You can't see any way out.").
			Wrap(err)
	}

	// Find matching exit
	for _, exit := range exits {
		if !exit.MatchesName(direction) {
			continue
		}

		// Check if exit is locked
		if exit.Locked {
			return oops.Code(command.CodeWorldError).
				With("message", "The exit is locked.").
				Errorf("exit %q is locked", exit.Name)
		}

		// Move the character
		if err := exec.Services.World.MoveCharacter(ctx, subjectID, exec.CharacterID, exit.ToLocationID); err != nil {
			return oops.Code(command.CodeWorldError).
				With("message", "Something prevents you from going that way.").
				Wrap(err)
		}

		// Show the new location
		loc, err := exec.Services.World.GetLocation(ctx, subjectID, exit.ToLocationID)
		if err != nil {
			return oops.Code(command.CodeWorldError).
				With("message", "You arrive somewhere strange...").
				Wrap(err)
		}

		// Output write errors are logged but don't fail the command - the game action succeeded
		writeOutputf(ctx, exec, "move", "%s\n%s\n", loc.Name, loc.Description)
		return nil
	}

	return oops.Code(command.CodeWorldError).
		With("message", "You can't go that way.").
		Errorf("no exit matching %q", direction)
}
