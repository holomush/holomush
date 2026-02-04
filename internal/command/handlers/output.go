// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"log/slog"

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
