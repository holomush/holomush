// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
	"github.com/holomush/holomush/pkg/holo"
)

// createPattern matches: create <type> "<name>"
var createPattern = regexp.MustCompile(`^(\w+)\s+"([^"]+)"$`)

// CreateHandler handles the create command.
// Syntax: create <type> "<name>"
// Types: object, location
func CreateHandler(ctx context.Context, exec *command.CommandExecution) error {
	charID := exec.CharacterID.String()
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		if n, err := fmt.Fprintln(exec.Output, "Usage: create <type> \"<name>\""); err != nil {
			logOutputError(ctx, "create", charID, n, err)
		}
		return nil
	}

	matches := createPattern.FindStringSubmatch(args)
	if matches == nil {
		if n, err := fmt.Fprintln(exec.Output, "Usage: create <type> \"<name>\""); err != nil {
			logOutputError(ctx, "create", charID, n, err)
		}
		return nil
	}

	entityType := strings.ToLower(matches[1])
	name := matches[2]
	subjectID := "char:" + charID

	switch entityType {
	case "object":
		return createObject(ctx, exec, subjectID, name)
	case "location":
		return createLocation(ctx, exec, subjectID, name)
	default:
		slog.DebugContext(ctx, "create: unknown entity type",
			"character_id", exec.CharacterID,
			"entity_type", entityType)
		if n, err := fmt.Fprintf(exec.Output, "Unknown type: %s. Use: object, location\n", entityType); err != nil {
			logOutputError(ctx, "create", charID, n, err)
		}
		return nil
	}
}

func createObject(ctx context.Context, exec *command.CommandExecution, subjectID, name string) error {
	charID := exec.CharacterID.String()
	obj, err := world.NewObject(name, world.InLocation(exec.LocationID))
	if err != nil {
		slog.ErrorContext(ctx, "create object: NewObject failed",
			"character_id", exec.CharacterID,
			"object_name", name,
			"error", err)
		if n, writeErr := fmt.Fprintln(exec.Output, "Failed to create object."); writeErr != nil {
			logOutputError(ctx, "create", charID, n, writeErr)
		}
		//nolint:wrapcheck // WorldError creates a structured oops error
		return command.WorldError("Failed to create object.", err)
	}

	if err := exec.Services.World.CreateObject(ctx, subjectID, obj); err != nil {
		slog.ErrorContext(ctx, "create object: CreateObject failed",
			"character_id", exec.CharacterID,
			"object_name", name,
			"error", err)
		if n, writeErr := fmt.Fprintln(exec.Output, "Failed to create object."); writeErr != nil {
			logOutputError(ctx, "create", charID, n, writeErr)
		}
		//nolint:wrapcheck // WorldError creates a structured oops error
		return command.WorldError("Failed to create object.", err)
	}

	if n, err := fmt.Fprintf(exec.Output, "Created object \"%s\" (#%s)\n", name, obj.ID); err != nil {
		logOutputError(ctx, "create", charID, n, err)
	}
	return nil
}

func createLocation(ctx context.Context, exec *command.CommandExecution, subjectID, name string) error {
	charID := exec.CharacterID.String()
	loc, err := world.NewLocation(name, "", world.LocationTypePersistent)
	if err != nil {
		slog.ErrorContext(ctx, "create location: NewLocation failed",
			"character_id", exec.CharacterID,
			"location_name", name,
			"error", err)
		if n, writeErr := fmt.Fprintln(exec.Output, "Failed to create location."); writeErr != nil {
			logOutputError(ctx, "create", charID, n, writeErr)
		}
		//nolint:wrapcheck // WorldError creates a structured oops error
		return command.WorldError("Failed to create location.", err)
	}

	if err := exec.Services.World.CreateLocation(ctx, subjectID, loc); err != nil {
		slog.ErrorContext(ctx, "create location: CreateLocation failed",
			"character_id", exec.CharacterID,
			"location_name", name,
			"error", err)
		if n, writeErr := fmt.Fprintln(exec.Output, "Failed to create location."); writeErr != nil {
			logOutputError(ctx, "create", charID, n, writeErr)
		}
		//nolint:wrapcheck // WorldError creates a structured oops error
		return command.WorldError("Failed to create location.", err)
	}

	if n, err := fmt.Fprintf(exec.Output, "Created location \"%s\" (#%s)\n", name, loc.ID); err != nil {
		logOutputError(ctx, "create", charID, n, err)
	}
	return nil
}

// setPattern matches: set <property> of <target> to <value>
var setPattern = regexp.MustCompile(`^(\w+)\s+of\s+(\S+)\s+to\s+(.+)$`)

// SetHandler handles the set command.
// Syntax: set <property> of <target> to <value>
// Properties support prefix matching (desc -> description).
func SetHandler(ctx context.Context, exec *command.CommandExecution) error {
	charID := exec.CharacterID.String()
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		if n, err := fmt.Fprintln(exec.Output, "Usage: set <property> of <target> to <value>"); err != nil {
			logOutputError(ctx, "set", charID, n, err)
		}
		return nil
	}

	matches := setPattern.FindStringSubmatch(args)
	if matches == nil {
		if n, err := fmt.Fprintln(exec.Output, "Usage: set <property> of <target> to <value>"); err != nil {
			logOutputError(ctx, "set", charID, n, err)
		}
		return nil
	}

	propertyPrefix := matches[1]
	target := matches[2]
	value := matches[3]

	// Resolve property with prefix matching
	registry := holo.DefaultRegistry()
	prop, err := registry.Resolve(propertyPrefix)
	if err != nil {
		slog.DebugContext(ctx, "set: property resolution failed",
			"character_id", exec.CharacterID,
			"property_prefix", propertyPrefix,
			"error", err)
		if n, writeErr := fmt.Fprintf(exec.Output, "Unknown property: %s\n", propertyPrefix); writeErr != nil {
			logOutputError(ctx, "set", charID, n, writeErr)
		}
		//nolint:wrapcheck // WorldError creates a structured oops error
		return command.WorldError("Property resolution failed.", err)
	}

	// Resolve target
	entityType, entityID, err := resolveTarget(ctx, exec, target)
	if err != nil {
		slog.DebugContext(ctx, "set: target resolution failed",
			"character_id", exec.CharacterID,
			"target", target,
			"error", err)
		if n, writeErr := fmt.Fprintf(exec.Output, "Could not find target: %s\n", target); writeErr != nil {
			logOutputError(ctx, "set", charID, n, writeErr)
		}
		return err
	}

	// Apply the property change
	if err := applyProperty(ctx, exec, entityType, entityID, prop.Name, value); err != nil {
		slog.ErrorContext(ctx, "set: apply property failed",
			"character_id", exec.CharacterID,
			"entity_type", entityType,
			"entity_id", entityID,
			"property", prop.Name,
			"error", err)
		if n, writeErr := fmt.Fprintln(exec.Output, "Failed to set property. Please try again."); writeErr != nil {
			logOutputError(ctx, "set", charID, n, writeErr)
		}
		//nolint:wrapcheck // WorldError creates a structured oops error
		return command.WorldError("Failed to apply property.", err)
	}

	if n, err := fmt.Fprintf(exec.Output, "Set %s of %s.\n", prop.Name, target); err != nil {
		logOutputError(ctx, "set", charID, n, err)
	}
	return nil
}

func resolveTarget(ctx context.Context, exec *command.CommandExecution, target string) (string, ulid.ULID, error) {
	// "here" -> current location
	if target == "here" {
		return "location", exec.LocationID, nil
	}
	// "me" -> current character
	if target == "me" {
		return "character", exec.CharacterID, nil
	}
	// #<id> -> direct ID reference (assume object by default, could be extended)
	if strings.HasPrefix(target, "#") {
		id, err := ulid.Parse(target[1:])
		if err != nil {
			slog.DebugContext(ctx, "resolveTarget: invalid ID format",
				"target", target,
				"error", err)
			return "", ulid.ULID{}, oops.Code(command.CodeInvalidArgs).
				With("target", target).
				Wrapf(err, "invalid target ID format")
		}
		// For now, assume objects for direct ID references
		// Future: could query world to determine entity type
		return "object", id, nil
	}
	// Otherwise, target not found
	// Future: implement object search by name in current location
	slog.DebugContext(ctx, "resolveTarget: target not found",
		"target", target,
		"character_id", exec.CharacterID,
		"location_id", exec.LocationID)
	return "", ulid.ULID{}, oops.Code(command.CodeTargetNotFound).
		With("target", target).
		Errorf("target not found: %s", target)
}

func applyProperty(ctx context.Context, exec *command.CommandExecution, entityType string, entityID ulid.ULID, propName, value string) error {
	subjectID := "char:" + exec.CharacterID.String()

	switch entityType {
	case "location":
		return applyPropertyToLocation(ctx, exec, subjectID, entityID, propName, value)
	case "object":
		return applyPropertyToObject(ctx, exec, subjectID, entityID, propName, value)
	case "character":
		return oops.Code(command.CodeInvalidArgs).
			With("entity_type", entityType).
			With("property", propName).
			Errorf("setting properties on characters not yet supported")
	default:
		return oops.Code(command.CodeInvalidArgs).
			With("entity_type", entityType).
			With("property", propName).
			Errorf("cannot set properties on %s", entityType)
	}
}

func applyPropertyToLocation(ctx context.Context, exec *command.CommandExecution, subjectID string, entityID ulid.ULID, propName, value string) error {
	loc, err := exec.Services.World.GetLocation(ctx, subjectID, entityID)
	if err != nil {
		return oops.Code(command.CodeWorldError).
			With("entity_type", "location").
			With("entity_id", entityID.String()).
			With("operation", "get").
			Wrapf(err, "get location failed")
	}
	switch propName {
	case "description":
		loc.Description = value
	case "name":
		loc.Name = value
	default:
		return oops.Code(command.CodeInvalidArgs).
			With("entity_type", "location").
			With("entity_id", entityID.String()).
			With("property", propName).
			Errorf("property %s not applicable to location", propName)
	}
	if err := exec.Services.World.UpdateLocation(ctx, subjectID, loc); err != nil {
		return oops.Code(command.CodeWorldError).
			With("entity_type", "location").
			With("entity_id", entityID.String()).
			With("property", propName).
			With("operation", "update").
			Wrapf(err, "update location failed")
	}
	return nil
}

func applyPropertyToObject(ctx context.Context, exec *command.CommandExecution, subjectID string, entityID ulid.ULID, propName, value string) error {
	obj, err := exec.Services.World.GetObject(ctx, subjectID, entityID)
	if err != nil {
		return oops.Code(command.CodeWorldError).
			With("entity_type", "object").
			With("entity_id", entityID.String()).
			With("operation", "get").
			Wrapf(err, "get object failed")
	}
	switch propName {
	case "description":
		obj.Description = value
	case "name":
		obj.Name = value
	default:
		return oops.Code(command.CodeInvalidArgs).
			With("entity_type", "object").
			With("entity_id", entityID.String()).
			With("property", propName).
			Errorf("property %s not applicable to object", propName)
	}
	if err := exec.Services.World.UpdateObject(ctx, subjectID, obj); err != nil {
		return oops.Code(command.CodeWorldError).
			With("entity_type", "object").
			With("entity_id", entityID.String()).
			With("property", propName).
			With("operation", "update").
			Wrapf(err, "update object failed")
	}
	return nil
}
