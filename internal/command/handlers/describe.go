// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// DescribeHandler handles the describe command.
// Syntax:
//   - describe me <text>        — set own character description
//   - describe here <text>      — set current location description
//   - describe <target>=<text>  — set named target description
func DescribeHandler(ctx context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("describe", "describe me <text> | describe here <text> | describe <target>=<text>")
	}

	target, text, err := parseDescribeArgs(args)
	if err != nil {
		return err
	}

	if target == "me" {
		return describeSelf(ctx, exec, text)
	}
	return describeTarget(ctx, exec, target, text)
}

// parseDescribeArgs parses describe command arguments into target and text.
// Returns ErrInvalidArgs if text is missing.
func parseDescribeArgs(args string) (target, text string, err error) {
	// "me <text>"
	if strings.HasPrefix(args, "me ") {
		text = strings.TrimSpace(args[3:])
		if text == "" {
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return "", "", command.ErrInvalidArgs("describe", "describe me <text>")
		}
		return "me", text, nil
	}
	// "here <text>"
	if strings.HasPrefix(args, "here ") {
		text = strings.TrimSpace(args[5:])
		if text == "" {
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return "", "", command.ErrInvalidArgs("describe", "describe here <text>")
		}
		return "here", text, nil
	}
	// "<target>=<text>"
	idx := strings.IndexByte(args, '=')
	if idx > 0 {
		tgt := strings.TrimSpace(args[:idx])
		txt := strings.TrimSpace(args[idx+1:])
		if tgt == "" || txt == "" {
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return "", "", command.ErrInvalidArgs("describe", "describe <target>=<text>")
		}
		return tgt, txt, nil
	}

	// args present but no recognisable form (e.g. "describe me" with no text)
	//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
	return "", "", command.ErrInvalidArgs("describe", "describe me <text> | describe here <text> | describe <target>=<text>")
}

// describeSelf updates the calling character's own description.
func describeSelf(ctx context.Context, exec *command.CommandExecution, text string) error {
	subjectID := access.CharacterSubject(exec.CharacterID().String())
	charID := exec.CharacterID()

	if err := exec.Services().World().UpdateCharacterDescription(ctx, subjectID, charID, text); err != nil {
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return err //nolint:wrapcheck // preserve oops error code from world service
		}
		if errors.Is(err, world.ErrPermissionDenied) {
			return err //nolint:wrapcheck // preserve oops error code from world service
		}
		slog.ErrorContext(ctx, "describe: UpdateCharacterDescription failed",
			"character_id", charID,
			"error", err)
		return writeOutputWithWorldError(ctx, exec, "describe", "Failed to set description. Please try again.", err)
	}

	writeOutput(ctx, exec, "describe", "Description set.\n")
	return nil
}

// describeTarget updates the description of a named target (here, #id, or named object).
// It uses the property registry to locate the "description" property and applies it.
func describeTarget(ctx context.Context, exec *command.CommandExecution, target, text string) error {
	registry := exec.Services().PropertyRegistry()
	if registry == nil {
		return writeOutputWithWorldError(ctx, exec, "describe", "Property registry not configured.", nil)
	}

	entry, err := registry.Resolve("description")
	if err != nil {
		slog.DebugContext(ctx, "describe: property resolution failed",
			"character_id", exec.CharacterID(),
			"error", err)
		return writeOutputfWithWorldError(ctx, exec, "describe", "Unknown property: description\n", err)
	}

	entityType, entityID, err := resolveTarget(ctx, exec, target)
	if err != nil {
		slog.DebugContext(ctx, "describe: target resolution failed",
			"character_id", exec.CharacterID(),
			"target", target,
			"error", err)
		writeOutputf(ctx, exec, "describe", "Could not find target: %s\n", target)
		return err
	}

	if err := applyProperty(ctx, exec, entityType, entityID, entry.Name, entry.Definition, text); err != nil {
		if errors.Is(err, world.ErrAccessEvaluationFailed) {
			return err
		}
		if errors.Is(err, world.ErrPermissionDenied) {
			return err
		}
		slog.ErrorContext(ctx, "describe: applyProperty failed",
			"character_id", exec.CharacterID(),
			"entity_type", entityType,
			"entity_id", entityID,
			"error", err)
		return writeOutputWithWorldError(ctx, exec, "describe", "Failed to set description. Please try again.", err)
	}

	writeOutput(ctx, exec, "describe", "Description set.\n")
	return nil
}
