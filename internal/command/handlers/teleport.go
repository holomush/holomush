// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/oklog/ulid/v2"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// TeleportHandler teleports a character to a named location.
//
// Syntax:
//   - teleport <location>              — move self to named location
//   - teleport <character>=<location>  — move another character (admin only)
//
// Scope enforcement:
//   - Default role: only allowed to teleport self to home location
//   - Builder role: can teleport self to any location, cannot teleport others
//   - Admin role: can teleport to any location, can teleport others
func TeleportHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("teleport", "teleport <location>")
	}

	subjectID := access.CharacterSubject(exec.CharacterID().String())

	// Parse: <character>=<location> or <location>
	var targetName, locationName string
	var teleportOther bool

	if idx := strings.IndexByte(args, '='); idx > 0 {
		targetName = strings.TrimSpace(args[:idx])
		locationName = strings.TrimSpace(args[idx+1:])
		teleportOther = true
	} else {
		locationName = args
	}

	if locationName == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("teleport", "teleport <location>")
	}

	// Resolve location by name.
	loc, findErr := exec.Services().World().FindLocationByName(ctx, subjectID, locationName)
	if findErr != nil {
		if errors.Is(findErr, world.ErrNotFound) {
			writeOutputf(ctx, exec, "teleport", "No location found named %q.\n", locationName)
			return nil
		}
		if errors.Is(findErr, world.ErrAccessEvaluationFailed) || errors.Is(findErr, world.ErrPermissionDenied) {
			return findErr //nolint:wrapcheck // preserve oops error code from world service
		}
		writeOutput(ctx, exec, "teleport", "Unable to find that location.")
		return nil
	}

	if teleportOther {
		return handleTeleportOther(ctx, exec, subjectID, targetName, loc)
	}

	return handleTeleportSelf(ctx, exec, subjectID, loc)
}

// handleTeleportSelf handles self-teleportation with role-based scope enforcement.
func handleTeleportSelf(ctx context.Context, exec *command.CommandExecution, subjectID string, loc *world.Location) error {
	// Already at location?
	if loc.ID == exec.LocationID() {
		writeOutputf(ctx, exec, "teleport", "You are already at %s.\n", loc.Name)
		return nil
	}

	// Check scope: admin > builder > default (home only)
	if err := checkTeleportSelfPermission(ctx, exec, subjectID, loc); err != nil {
		return err
	}

	// Move character.
	moveErr := exec.Services().World().MoveCharacter(ctx, subjectID, exec.CharacterID(), loc.ID)
	if moveErr != nil {
		if errors.Is(moveErr, world.ErrAccessEvaluationFailed) || errors.Is(moveErr, world.ErrPermissionDenied) {
			return moveErr //nolint:wrapcheck // preserve oops error code from world service
		}
		writeOutput(ctx, exec, "teleport", "Teleport failed.")
		return nil
	}

	return showTeleportLocation(ctx, exec, subjectID, loc.ID)
}

// checkTeleportSelfPermission determines if the user has permission to teleport
// to the given location based on their role tier.
func checkTeleportSelfPermission(ctx context.Context, exec *command.CommandExecution, subjectID string, loc *world.Location) error {
	engine := exec.Services().Engine()

	// Admin: can teleport anywhere (seed:admin-full-access permits everything)
	if command.CheckCapability(ctx, engine, subjectID, "admin.boot", "teleport") == nil {
		return nil
	}

	// Builder: can teleport self to any location
	if command.CheckCapability(ctx, engine, subjectID, "build.teleport", "teleport") == nil {
		return nil
	}

	// Default role: only allowed to teleport to home location.
	homeLocID, resolveErr := resolveHomeLocationID(ctx, exec, subjectID)
	if resolveErr != nil {
		if errors.Is(resolveErr, errNoHomeOutput) {
			//nolint:wrapcheck // ErrPermissionDenied creates a structured oops error
			return command.ErrPermissionDenied("teleport", "teleport")
		}
		return resolveErr
	}

	if loc.ID == homeLocID {
		return nil
	}

	//nolint:wrapcheck // ErrPermissionDenied creates a structured oops error
	return command.ErrPermissionDenied("teleport", "teleport")
}

// handleTeleportOther handles teleporting another character (admin only).
func handleTeleportOther(ctx context.Context, exec *command.CommandExecution, subjectID, targetName string, loc *world.Location) error {
	// Only admins can teleport others.
	if err := command.CheckCapability(ctx, exec.Services().Engine(), subjectID, "admin.boot", "teleport"); err != nil {
		//nolint:wrapcheck // CheckCapability returns structured oops errors with code and context
		return err
	}

	// Find target session.
	targetSession, findErr := exec.Services().Session().FindByCharacterName(ctx, targetName)
	if findErr != nil {
		writeOutput(ctx, exec, "teleport", "Unable to find that character.")
		return nil //nolint:nilerr // session lookup failure is shown to user; not an application error
	}
	if targetSession == nil {
		writeOutputf(ctx, exec, "teleport", "No character found named %q.\n", targetName)
		return nil
	}

	// Move the target character.
	moveErr := exec.Services().World().MoveCharacter(ctx, subjectID, targetSession.CharacterID, loc.ID)
	if moveErr != nil {
		if errors.Is(moveErr, world.ErrAccessEvaluationFailed) || errors.Is(moveErr, world.ErrPermissionDenied) {
			return moveErr //nolint:wrapcheck // preserve oops error code from world service
		}
		writeOutput(ctx, exec, "teleport", "Teleport failed.")
		return nil
	}

	// Notify the target.
	msg := fmt.Sprintf("You have been teleported to %s by %s.", loc.Name, exec.CharacterName())
	stream := "session:" + targetSession.CharacterID.String()
	exec.Services().BroadcastSystemMessage(ctx, stream, msg)

	return showTeleportLocation(ctx, exec, subjectID, loc.ID)
}

// showTeleportLocation displays the destination location to the executor.
func showTeleportLocation(ctx context.Context, exec *command.CommandExecution, subjectID string, locID ulid.ULID) error {
	loc, err := exec.Services().World().GetLocation(ctx, subjectID, locID)
	if err != nil {
		if errors.Is(err, world.ErrAccessEvaluationFailed) || errors.Is(err, world.ErrPermissionDenied) {
			return err //nolint:wrapcheck // preserve oops error code from world service
		}
		writeOutput(ctx, exec, "teleport", "You arrive somewhere strange...")
		return nil
	}

	writeLocationOutput(ctx, exec, "teleport", loc.Name, loc.Description)
	return nil
}
