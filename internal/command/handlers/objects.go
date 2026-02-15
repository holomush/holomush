// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/property"
	"github.com/holomush/holomush/internal/world"
)

// createPattern matches: create <type> "<name>"
var createPattern = regexp.MustCompile(`^(\w+)\s+"([^"]+)"$`)

// CreateHandler handles the create command.
// Syntax: create <type> "<name>"
// Types: object, location
func CreateHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("create", "create <type> \"<name>\"")
	}

	matches := createPattern.FindStringSubmatch(args)
	if matches == nil {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("create", "create <type> \"<name>\"")
	}

	entityType := strings.ToLower(matches[1])
	name := matches[2]
	subjectID := access.SubjectCharacter + exec.CharacterID().String()

	switch entityType {
	case "object":
		return createObject(ctx, exec, subjectID, name)
	case "location":
		return createLocation(ctx, exec, subjectID, name)
	default:
		slog.DebugContext(ctx, "create: unknown entity type",
			"character_id", exec.CharacterID(),
			"entity_type", entityType)
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("create", "create <type> \"<name>\" (valid types: object, location)")
	}
}

func createObject(ctx context.Context, exec *command.CommandExecution, subjectID, name string) error {
	obj, err := world.NewObject(name, world.InLocation(exec.LocationID()))
	if err != nil {
		slog.ErrorContext(ctx, "create object: NewObject failed",
			"character_id", exec.CharacterID(),
			"object_name", name,
			"error", err)
		return writeOutputWithWorldError(ctx, exec, "create", "Failed to create object.", err)
	}

	if err := exec.Services().World().CreateObject(ctx, subjectID, obj); err != nil {
		slog.ErrorContext(ctx, "create object: CreateObject failed",
			"character_id", exec.CharacterID(),
			"object_name", name,
			"error", err)
		// Preserve access evaluation failures with their specific codes
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return err //nolint:wrapcheck // preserve oops error code from world service
		}
		return writeOutputWithWorldError(ctx, exec, "create", "Failed to create object.", err)
	}

	writeOutputf(ctx, exec, "create", "Created object \"%s\" (#%s)\n", name, obj.ID)
	return nil
}

func createLocation(ctx context.Context, exec *command.CommandExecution, subjectID, name string) error {
	loc, err := world.NewLocation(name, "", world.LocationTypePersistent)
	if err != nil {
		slog.ErrorContext(ctx, "create location: NewLocation failed",
			"character_id", exec.CharacterID(),
			"location_name", name,
			"error", err)
		return writeOutputWithWorldError(ctx, exec, "create", "Failed to create location.", err)
	}

	if err := exec.Services().World().CreateLocation(ctx, subjectID, loc); err != nil {
		slog.ErrorContext(ctx, "create location: CreateLocation failed",
			"character_id", exec.CharacterID(),
			"location_name", name,
			"error", err)
		// Preserve access evaluation failures with their specific codes
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return err //nolint:wrapcheck // preserve oops error code from world service
		}
		return writeOutputWithWorldError(ctx, exec, "create", "Failed to create location.", err)
	}

	writeOutputf(ctx, exec, "create", "Created location \"%s\" (#%s)\n", name, loc.ID)
	return nil
}

// setPattern matches: set <property> of <target> to <value>
var setPattern = regexp.MustCompile(`^(\w+)\s+of\s+(\S+)\s+to\s+(.+)$`)

// SetHandler handles the set command.
// Syntax: set <property> of <target> to <value>
// Properties support prefix matching (desc -> description).
func SetHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("set", "set <property> of <target> to <value>")
	}

	matches := setPattern.FindStringSubmatch(args)
	if matches == nil {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("set", "set <property> of <target> to <value>")
	}

	propertyPrefix := matches[1]
	target := matches[2]
	value := matches[3]

	registry := exec.Services().PropertyRegistry()
	if registry == nil {
		err := oops.Code(command.CodeNilService).
			With("service", "PropertyRegistry").
			Errorf("property registry not configured")
		writeOutput(ctx, exec, "set", "Failed to set property. Please try again.")
		return err
	}

	// Resolve property with prefix matching
	entry, err := registry.Resolve(propertyPrefix)
	if err != nil {
		slog.DebugContext(ctx, "set: property resolution failed",
			"character_id", exec.CharacterID(),
			"property_prefix", propertyPrefix,
			"error", err)
		return writeOutputfWithWorldError(ctx, exec, "set", "Unknown property: %s\n", err, propertyPrefix)
	}

	// Resolve target
	entityType, entityID, err := resolveTarget(ctx, exec, target)
	if err != nil {
		slog.DebugContext(ctx, "set: target resolution failed",
			"character_id", exec.CharacterID(),
			"target", target,
			"error", err)
		writeOutputf(ctx, exec, "set", "Could not find target: %s\n", target)
		return err
	}

	// Apply the property change
	if err := applyProperty(ctx, exec, entityType, entityID, entry.Name, entry.Definition, value); err != nil {
		slog.ErrorContext(ctx, "set: apply property failed",
			"character_id", exec.CharacterID(),
			"entity_type", entityType,
			"entity_id", entityID,
			"property", entry.Name,
			"error", err)
		return writeOutputWithWorldError(ctx, exec, "set", "Failed to set property. Please try again.", err)
	}

	writeOutputf(ctx, exec, "set", "Set %s of %s.\n", entry.Name, target)
	return nil
}

func resolveTarget(ctx context.Context, exec *command.CommandExecution, target string) (string, ulid.ULID, error) {
	// "here" -> current location
	if target == "here" {
		return "location", exec.LocationID(), nil
	}
	// "me" -> current character
	if target == "me" {
		return "character", exec.CharacterID(), nil
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
		"character_id", exec.CharacterID(),
		"location_id", exec.LocationID())
	return "", ulid.ULID{}, oops.Code(command.CodeTargetNotFound).
		With("target", target).
		Errorf("target not found: %s", target)
}

func applyProperty(ctx context.Context, exec *command.CommandExecution, entityType string, entityID ulid.ULID, propName string, definition property.Definition, value string) error {
	subjectID := access.SubjectCharacter + exec.CharacterID().String()

	switch entityType {
	case "character":
		return oops.Code(command.CodeInvalidArgs).
			With("entity_type", entityType).
			With("property", propName).
			Errorf("setting properties on characters not yet supported")
	case "location", "object":
		// continue
	default:
		return oops.Code(command.CodeInvalidArgs).
			With("entity_type", entityType).
			With("property", propName).
			Errorf("cannot set properties on %s", entityType)
	}

	if definition == nil {
		return oops.Code(command.CodeInvalidArgs).
			With("entity_type", entityType).
			With("property", propName).
			Errorf("property %s not registered", propName)
	}
	if err := definition.Validate(entityType); err != nil {
		return oops.Code(command.CodeInvalidArgs).
			With("entity_type", entityType).
			With("property", propName).
			Wrapf(err, "property %s not applicable", propName)
	}

	querier := propertyQuerier{
		service:   exec.Services().World(),
		subjectID: subjectID,
	}
	mutator := propertyMutator{
		service:  exec.Services().World(),
		property: propName,
	}

	if err := definition.Set(ctx, querier, mutator, subjectID, entityType, entityID, value); err != nil {
		return fmt.Errorf("set property: %w", err)
	}
	return nil
}

type propertyQuerier struct {
	service   command.WorldService
	subjectID string
}

func (q propertyQuerier) GetLocation(ctx context.Context, id ulid.ULID) (*world.Location, error) {
	loc, err := q.service.GetLocation(ctx, q.subjectID, id)
	if err != nil {
		// Preserve access evaluation failures with their specific codes
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return nil, err //nolint:wrapcheck // preserve oops error code from world service
		}
		return nil, oops.Code(command.CodeWorldError).
			With("entity_type", "location").
			With("entity_id", id.String()).
			With("operation", "get").
			Wrapf(err, "get location failed")
	}
	return loc, nil
}

func (q propertyQuerier) GetObject(ctx context.Context, id ulid.ULID) (*world.Object, error) {
	obj, err := q.service.GetObject(ctx, q.subjectID, id)
	if err != nil {
		// Preserve access evaluation failures with their specific codes
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return nil, err //nolint:wrapcheck // preserve oops error code from world service
		}
		return nil, oops.Code(command.CodeWorldError).
			With("entity_type", "object").
			With("entity_id", id.String()).
			With("operation", "get").
			Wrapf(err, "get object failed")
	}
	return obj, nil
}

type propertyMutator struct {
	service  command.WorldService
	property string
}

func (m propertyMutator) UpdateLocation(ctx context.Context, subjectID string, loc *world.Location) error {
	if err := m.service.UpdateLocation(ctx, subjectID, loc); err != nil {
		// Preserve access evaluation failures with their specific codes
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return err //nolint:wrapcheck // preserve oops error code from world service
		}
		return oops.Code(command.CodeWorldError).
			With("entity_type", "location").
			With("entity_id", loc.ID.String()).
			With("property", m.property).
			With("operation", "update").
			Wrapf(err, "update location failed")
	}
	return nil
}

func (m propertyMutator) UpdateObject(ctx context.Context, subjectID string, obj *world.Object) error {
	if err := m.service.UpdateObject(ctx, subjectID, obj); err != nil {
		// Preserve access evaluation failures with their specific codes
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return err //nolint:wrapcheck // preserve oops error code from world service
		}
		return oops.Code(command.CodeWorldError).
			With("entity_type", "object").
			With("entity_id", obj.ID.String()).
			With("property", m.property).
			With("operation", "update").
			Wrapf(err, "update object failed")
	}
	return nil
}
