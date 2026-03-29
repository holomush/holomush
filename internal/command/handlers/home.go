// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// HomeHandler returns the character to their home location. If no home property
// is set, it falls back to the server's default starting location. If neither is
// available, it informs the character.
func HomeHandler(ctx context.Context, exec *command.CommandExecution) error {
	subjectID := access.CharacterSubject(exec.CharacterID().String())

	// Determine the target home location.
	homeLocID, resolveErr := resolveHomeLocationID(ctx, exec, subjectID)
	if resolveErr != nil {
		if errors.Is(resolveErr, errNoHomeOutput) {
			// User-facing message already written; nothing more to do.
			return nil
		}
		// Access errors should propagate.
		return resolveErr
	}
	if homeLocID.IsZero() {
		return nil
	}

	// Already home?
	if homeLocID == exec.LocationID() {
		writeOutput(ctx, exec, "home", "You are already home.")
		return nil
	}

	// Move to home.
	moveErr := exec.Services().World().MoveCharacter(ctx, subjectID, exec.CharacterID(), homeLocID)
	if moveErr != nil {
		if errors.Is(moveErr, world.ErrAccessEvaluationFailed) || errors.Is(moveErr, world.ErrPermissionDenied) {
			return moveErr //nolint:wrapcheck // preserve oops error code from world service
		}
		// Home location could not be reached (e.g., deleted between property read and move).
		writeOutput(ctx, exec, "home", "Your home location no longer exists.")
		return nil
	}

	// Show the new location.
	loc, locErr := exec.Services().World().GetLocation(ctx, subjectID, homeLocID)
	if locErr != nil {
		if errors.Is(locErr, world.ErrAccessEvaluationFailed) || errors.Is(locErr, world.ErrPermissionDenied) {
			return locErr //nolint:wrapcheck // preserve oops error code from world service
		}
		writeOutput(ctx, exec, "home", "You arrive somewhere strange...")
		return nil
	}

	writeLocationOutput(ctx, exec, "home", loc.Name, loc.Description)
	return nil
}

// errNoHomeOutput is a sentinel error returned by resolveHomeLocationID when
// a user-facing message has already been written to the output and the caller
// should return nil without further action.
var errNoHomeOutput = errors.New("no home location: output written to player")

// resolveHomeLocationID finds the character's home location ULID.
// Priority:
//  1. Character's "home" property value (ULID string).
//  2. Server's default starting location ID.
//
// Returns (id, nil) on success.
// Returns (zero, errNoHomeOutput) when the "no home" message was written to output.
// Returns (zero, accessErr) when an ABAC error should propagate.
func resolveHomeLocationID(ctx context.Context, exec *command.CommandExecution, subjectID string) (ulid.ULID, error) {
	props, propErr := exec.Services().World().ListPropertiesByParent(ctx, subjectID, "character", exec.CharacterID())
	if propErr != nil {
		if errors.Is(propErr, world.ErrAccessEvaluationFailed) || errors.Is(propErr, world.ErrPermissionDenied) {
			return ulid.ULID{}, propErr //nolint:wrapcheck // preserve oops error code from world service
		}
		// Property store unavailable — fall through to default.
		props = nil
	}

	for _, p := range props {
		if p.Name == "home" && p.Value != nil {
			id, parseErr := ulid.Parse(*p.Value)
			if parseErr == nil {
				return id, nil
			}
		}
	}

	// No home property — use server default if configured.
	defaultID := exec.Services().StartingLocationID()
	if !defaultID.IsZero() {
		return defaultID, nil
	}

	// No home and no default.
	writeOutput(ctx, exec, "home", "You have no home location set.")
	return ulid.ULID{}, errNoHomeOutput
}
