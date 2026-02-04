// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
)

// ShutdownHandler initiates a graceful server shutdown.
// Requires admin.shutdown capability.
// Usage: shutdown [delay_seconds]
// If delay is 0 or omitted, shutdown is immediate.
// Broadcasts a warning to all players before initiating shutdown.
func ShutdownHandler(ctx context.Context, exec *command.CommandExecution) error {
	// Check capability
	subjectID := "char:" + exec.CharacterID.String()
	allowed := exec.Services.Access.Check(ctx, subjectID, "execute", "admin.shutdown")
	if !allowed {
		//nolint:wrapcheck // ErrPermissionDenied creates a structured oops error
		return command.ErrPermissionDenied("shutdown", "admin.shutdown")
	}

	// Parse optional delay parameter
	var delaySeconds int64
	args := strings.TrimSpace(exec.Args)
	if args != "" {
		parsed, err := strconv.ParseInt(args, 10, 64)
		if err != nil {
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return command.ErrInvalidArgs("shutdown", "shutdown [delay_seconds]")
		}
		if parsed < 0 {
			//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
			return command.ErrInvalidArgs("shutdown", "shutdown [delay_seconds]")
		}
		delaySeconds = parsed
	}

	// Broadcast warning to all players
	message := formatShutdownMessage(delaySeconds)
	exec.Services.BroadcastSystemMessage("system", message)

	// Log admin action
	slog.Info("admin shutdown",
		"admin_id", exec.CharacterID.String(),
		"admin_name", exec.CharacterName,
		"delay_seconds", delaySeconds,
	)

	// Notify the executor
	if delaySeconds == 0 {
		//nolint:errcheck // output write error is acceptable; player display is best-effort
		_, _ = fmt.Fprintln(exec.Output, "Initiating server shutdown...")
	} else {
		//nolint:errcheck // output write error is acceptable; player display is best-effort
		_, _ = fmt.Fprintf(exec.Output, "Initiating server shutdown in %d seconds...\n", delaySeconds)
	}

	// Return shutdown signal with delay context
	return oops.Code(command.CodeShutdownRequested).
		With("delay_seconds", delaySeconds).
		Wrap(command.ErrShutdownRequested)
}

// formatShutdownMessage creates the appropriate shutdown warning message.
func formatShutdownMessage(delaySeconds int64) string {
	if delaySeconds == 0 {
		return "[SHUTDOWN] Server shutting down NOW."
	}
	return fmt.Sprintf("[SHUTDOWN] Server shutting down in %d seconds...", delaySeconds)
}
