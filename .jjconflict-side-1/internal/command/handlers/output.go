// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/observability"
)

// logOutputError logs a write failure at warn level with structured context
// and increments the command output failure metric.
// This provides visibility into connection issues without failing the command.
// Parameters:
//   - ctx: context for structured logging
//   - cmd: command name (e.g., "look", "move")
//   - charID: character ID as string for logging
//   - bytesWritten: number of bytes successfully written before error
//   - err: the write error
func logOutputError(ctx context.Context, cmd, charID string, bytesWritten int, err error) {
	slog.WarnContext(ctx, "failed to write command output",
		"command", cmd,
		"character_id", charID,
		"bytes_written", bytesWritten,
		"error", err,
	)
	observability.RecordCommandOutputFailure(cmd)
}

// writeOutput writes a message to the command output and logs any errors.
// This is a convenience wrapper that handles the common pattern of writing
// output and logging failures without failing the command.
func writeOutput(ctx context.Context, exec *command.CommandExecution, cmd, msg string) {
	if n, err := fmt.Fprintln(exec.Output(), msg); err != nil {
		logOutputError(ctx, cmd, exec.CharacterID().String(), n, err)
	}
}

// writeOutputf writes a formatted message to the command output and logs any errors.
// This is a convenience wrapper that handles the common pattern of writing
// formatted output and logging failures without failing the command.
func writeOutputf(ctx context.Context, exec *command.CommandExecution, cmd, format string, args ...any) {
	if n, err := fmt.Fprintf(exec.Output(), format, args...); err != nil {
		logOutputError(ctx, cmd, exec.CharacterID().String(), n, err)
	}
}

// writeLocationOutput writes a location name + description pair to output.
func writeLocationOutput(ctx context.Context, exec *command.CommandExecution, cmd, name, description string) {
	writeOutputf(ctx, exec, cmd, "%s\n%s\n", name, description)
}

// handleError writes a user-facing message to output and returns a WorldError
// with a (potentially distinct) internal message for diagnostic context.
func handleError(ctx context.Context, exec *command.CommandExecution, cmd, userMessage, internalMessage string, err error) error {
	writeOutput(ctx, exec, cmd, userMessage)
	//nolint:wrapcheck // WorldError creates a structured oops error
	return command.WorldError(internalMessage, err)
}

// writeOutputWithWorldError writes a message to the command output and returns a WorldError.
// This combines the common error handling pattern of notifying the player and returning
// a structured error for downstream handling.
func writeOutputWithWorldError(ctx context.Context, exec *command.CommandExecution, cmd, userMessage string, err error) error {
	return handleError(ctx, exec, cmd, userMessage, userMessage, err)
}

// writeOutputfWithWorldError writes a formatted message to the command output and returns a WorldError.
// This combines formatted output with structured error wrapping.
func writeOutputfWithWorldError(ctx context.Context, exec *command.CommandExecution, cmd, userFormat string, err error, args ...any) error {
	writeOutputf(ctx, exec, cmd, userFormat, args...)
	formattedMessage := fmt.Sprintf(userFormat, args...)
	//nolint:wrapcheck // WorldError creates a structured oops error
	return command.WorldError(formattedMessage, err)
}
