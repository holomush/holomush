// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/core"
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
	if exec.Services.Broadcaster != nil {
		broadcastShutdownWarning(exec, delaySeconds)
	}

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

// broadcastShutdownWarning sends a system-wide shutdown warning to all players.
func broadcastShutdownWarning(exec *command.CommandExecution, delaySeconds int64) {
	var message string
	if delaySeconds == 0 {
		message = "[SHUTDOWN] Server shutting down NOW."
	} else {
		message = fmt.Sprintf("[SHUTDOWN] Server shutting down in %d seconds...", delaySeconds)
	}

	//nolint:errcheck // json.Marshal cannot fail for map[string]string
	payload, _ := json.Marshal(map[string]string{
		"message": message,
	})

	event := core.Event{
		ID:        ulid.Make(),
		Stream:    "system",
		Type:      core.EventTypeSystem,
		Timestamp: time.Now(),
		Actor: core.Actor{
			Kind: core.ActorSystem,
			ID:   "system",
		},
		Payload: payload,
	}

	exec.Services.Broadcaster.Broadcast(event)
}
