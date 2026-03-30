// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package handlers provides compiled-in command handlers for quit and shutdown.
// All other commands have been migrated to core plugins under plugins/core-*.
package handlers

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/observability"
)

// This file provides output helper functions used by the remaining compiled-in
// handlers (quit and shutdown). Functions previously used by migrated handlers
// have been removed.

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

