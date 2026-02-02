// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"github.com/samber/oops"
)

// Error codes for command dispatch failures.
const (
	CodeUnknownCommand   = "UNKNOWN_COMMAND"
	CodePermissionDenied = "PERMISSION_DENIED"
	CodeInvalidArgs      = "INVALID_ARGS"
	CodeWorldError       = "WORLD_ERROR"
	CodeRateLimited      = "RATE_LIMITED"
	CodeCircularAlias    = "CIRCULAR_ALIAS"
	CodeNoCharacter      = "NO_CHARACTER"
)

// ErrUnknownCommand creates an error for an unknown command.
func ErrUnknownCommand(cmd string) error {
	return oops.Code(CodeUnknownCommand).
		With("command", cmd).
		Errorf("unknown command: %s", cmd)
}

// ErrPermissionDenied creates an error for permission denial.
func ErrPermissionDenied(cmd, capability string) error {
	return oops.Code(CodePermissionDenied).
		With("command", cmd).
		With("capability", capability).
		Errorf("permission denied for command %s", cmd)
}

// ErrInvalidArgs creates an error for invalid arguments.
func ErrInvalidArgs(cmd, usage string) error {
	return oops.Code(CodeInvalidArgs).
		With("command", cmd).
		With("usage", usage).
		Errorf("invalid arguments")
}

// WorldError creates an error for world state issues with a player-facing message.
func WorldError(message string, cause error) error {
	builder := oops.Code(CodeWorldError).With("message", message)
	if cause != nil {
		return builder.Wrap(cause)
	}
	return builder.Errorf("%s", message)
}

// ErrRateLimited creates an error for rate limiting.
func ErrRateLimited(cooldownMs int64) error {
	return oops.Code(CodeRateLimited).
		With("cooldown_ms", cooldownMs).
		Errorf("Too many commands. Please slow down.")
}

// ErrCircularAlias creates an error for circular alias detection.
func ErrCircularAlias(alias string) error {
	return oops.Code(CodeCircularAlias).
		With("alias", alias).
		Errorf("Alias rejected: circular reference detected (expansion depth exceeded)")
}

// ErrNoCharacter creates an error when command is executed without a character.
func ErrNoCharacter() error {
	return oops.Code(CodeNoCharacter).
		Errorf("no character associated with session")
}

// PlayerMessage extracts a player-facing message from an error.
func PlayerMessage(err error) string {
	if err == nil {
		return "Something went wrong. Try again."
	}
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return "Something went wrong. Try again."
	}

	switch oopsErr.Code() {
	case CodeUnknownCommand:
		return "Unknown command. Try 'help'."
	case CodePermissionDenied:
		return "You don't have permission to do that."
	case CodeInvalidArgs:
		if usage, ok := oopsErr.Context()["usage"].(string); ok && usage != "" {
			return "Usage: " + usage
		}
		return "Invalid arguments."
	case CodeWorldError:
		if msg, ok := oopsErr.Context()["message"].(string); ok {
			return msg
		}
		return "Something went wrong. Try again."
	case CodeRateLimited:
		return "Too many commands. Please slow down."
	case CodeCircularAlias:
		return "Alias rejected: circular reference detected (expansion depth exceeded)"
	case CodeNoCharacter:
		return "No character selected. Please select a character first."
	default:
		return "Something went wrong. Try again."
	}
}
