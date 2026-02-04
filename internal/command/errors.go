// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package command

import (
	"log/slog"

	"github.com/samber/oops"
)

// Error codes for command dispatch failures.
const (
	CodeUnknownCommand    = "UNKNOWN_COMMAND"
	CodePermissionDenied  = "PERMISSION_DENIED"
	CodeInvalidArgs       = "INVALID_ARGS"
	CodeWorldError        = "WORLD_ERROR"
	CodeRateLimited       = "RATE_LIMITED"
	CodeCircularAlias     = "CIRCULAR_ALIAS"
	CodeAliasConflict     = "ALIAS_CONFLICT"
	CodeNoCharacter       = "NO_CHARACTER"
	CodeTargetNotFound    = "TARGET_NOT_FOUND"
	CodeShutdownRequested = "SHUTDOWN_REQUESTED"
	CodeNilServices       = "NIL_SERVICES"
	CodeInvalidName       = "INVALID_NAME"
	CodeNoAliasCache      = "NO_ALIAS_CACHE"
)

// Sentinel errors for special conditions.
var (
	// ErrShutdownRequested signals that a graceful shutdown has been requested.
	// Command dispatchers should check for this error and initiate shutdown.
	ErrShutdownRequested = oops.Code(CodeShutdownRequested).Errorf("shutdown requested")

	// ErrEmptyCommandName is returned when registering a command with an empty name.
	ErrEmptyCommandName = oops.Errorf("command name cannot be empty")

	// ErrNilHandler is returned when registering a command with a nil handler.
	ErrNilHandler = oops.Errorf("command handler cannot be nil")

	// ErrNilRegistry is returned when creating a dispatcher with a nil registry.
	ErrNilRegistry = oops.Errorf("registry cannot be nil")

	// ErrNilAccessControl is returned when creating a dispatcher with nil access control.
	ErrNilAccessControl = oops.Errorf("access control cannot be nil")
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

// ErrAliasConflict creates an error when a system alias shadows another system alias.
func ErrAliasConflict(alias, existingCommand string) error {
	return oops.Code(CodeAliasConflict).
		With("alias", alias).
		With("existing_command", existingCommand).
		Errorf("'%s' shadows existing system alias for '%s'. Use 'sysalias remove %s' first.", alias, existingCommand, alias)
}

// ErrNoCharacter creates an error when command is executed without a character.
func ErrNoCharacter() error {
	return oops.Code(CodeNoCharacter).
		Errorf("no character associated with session")
}

// ErrTargetNotFound creates an error when a target player cannot be found.
func ErrTargetNotFound(target string) error {
	return oops.Code(CodeTargetNotFound).
		With("target", target).
		Errorf("player not found: %s", target)
}

// ErrNilServices creates an error when command execution has nil Services.
func ErrNilServices() error {
	return oops.Code(CodeNilServices).
		Errorf("command execution context missing services")
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
	case CodeAliasConflict:
		if alias, ok := oopsErr.Context()["alias"].(string); ok {
			if existingCmd, ok := oopsErr.Context()["existing_command"].(string); ok {
				return "'" + alias + "' shadows existing system alias for '" + existingCmd + "'. Use 'sysalias remove " + alias + "' first."
			}
		}
		return "Alias conflicts with an existing system alias."
	case CodeNoCharacter:
		return "No character selected. Please select a character first."
	case CodeTargetNotFound:
		if target, ok := oopsErr.Context()["target"].(string); ok && target != "" {
			return "Target not found: " + target
		}
		return "Target not found."
	case CodeNilServices:
		return "Internal error: services unavailable."
	case CodeInvalidName:
		// INVALID_NAME errors contain helpful context in the message itself
		// (e.g., "alias name cannot be empty", "alias name exceeds maximum length of 20")
		return err.Error()
	case CodeNoAliasCache:
		return "Alias system is not available. Contact the server administrator."
	default:
		slog.Warn("unhandled error code in PlayerMessage",
			"code", oopsErr.Code(),
			"error", err)
		return "Something went wrong. Try again."
	}
}
