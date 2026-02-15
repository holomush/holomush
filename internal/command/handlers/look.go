// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package handlers provides command handler implementations.
package handlers

import (
	"context"
	"errors"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// LookHandler displays the current location's name and description.
func LookHandler(ctx context.Context, exec *command.CommandExecution) error {
	subjectID := access.CharacterSubject(exec.CharacterID().String())

	loc, err := exec.Services().World().GetLocation(ctx, subjectID, exec.LocationID())
	if err != nil {
		// Preserve access evaluation failures with their specific codes (e.g., LOCATION_ACCESS_EVALUATION_FAILED)
		// instead of masking them as generic WORLD_ERROR
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return err //nolint:wrapcheck // preserve oops error code from world service
		}
		return oops.Code(command.CodeWorldError).
			With("message", "You can't see anything here.").
			Wrap(err)
	}

	// Output write errors are logged but don't fail the command - the game action succeeded
	writeLocationOutput(ctx, exec, "look", loc.Name, loc.Description)
	return nil
}
