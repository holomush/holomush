// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"time"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/world"
)

// playerInfo holds display information for a connected player.
type playerInfo struct {
	Name     string
	IdleTime time.Duration
}

// WhoHandler displays a list of connected players with idle times.
func WhoHandler(ctx context.Context, exec *command.CommandExecution) error {
	sessions := exec.Services().Session().ListActiveSessions()

	if len(sessions) == 0 {
		if n, err := writeWhoOutput(exec.Output(), nil); err != nil {
			logOutputError(ctx, "who", exec.CharacterID().String(), n, err)
		}
		return nil
	}

	subjectID := access.SubjectCharacter + exec.CharacterID().String()
	now := time.Now()

	// Collect visible players
	players := make([]playerInfo, 0, len(sessions))
	var errorCount int
	for _, session := range sessions {
		// Try to get character info - skip if not accessible
		char, err := exec.Services().World().GetCharacter(ctx, subjectID, session.CharacterID)
		if err != nil {
			// Skip expected errors (not found, permission denied)
			if errors.Is(err, world.ErrNotFound) || errors.Is(err, world.ErrPermissionDenied) {
				continue
			}
			// ErrAccessEvaluationFailed falls through here intentionally —
			// engine failures should be visible to users via the error count message.
			// Log unexpected errors (database failures, timeouts, etc.) but continue
			slog.ErrorContext(ctx, "unexpected error looking up character in who list",
				"session_char_id", session.CharacterID.String(),
				"error", err,
			)
			errorCount++
			continue
		}

		idleTime := now.Sub(session.LastActivity)
		players = append(players, playerInfo{
			Name:     char.Name,
			IdleTime: idleTime,
		})
	}

	if n, err := writeWhoOutput(exec.Output(), players); err != nil {
		logOutputError(ctx, "who", exec.CharacterID().String(), n, err)
	}

	// Warn user if any characters couldn't be displayed due to errors.
	// Output write errors are logged but don't fail the command.
	if errorCount > 0 {
		if errorCount == 1 {
			writeOutput(ctx, exec, "who", "(Note: 1 player could not be displayed due to a system error)")
		} else {
			writeOutputf(ctx, exec, "who", "(Note: %d players could not be displayed due to system errors)\n", errorCount)
		}
	}
	return nil
}

// writeWhoOutput formats and writes the who list to the output.
// Returns total bytes written and the first error encountered (if any).
func writeWhoOutput(w io.Writer, players []playerInfo) (int, error) {
	var totalBytes int
	var firstErr error

	// Helper to track bytes and capture first error
	write := func(n int, err error) {
		totalBytes += n
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if len(players) == 0 {
		write(fmt.Fprintln(w, "No players online."))
		return totalBytes, firstErr
	}

	// Sort players by name for consistent output
	sort.Slice(players, func(i, j int) bool {
		return players[i].Name < players[j].Name
	})

	// Output header
	write(fmt.Fprintln(w, "Players Online:"))
	write(fmt.Fprintln(w, "---------------"))

	// Output each player
	for _, p := range players {
		write(fmt.Fprintf(w, "  %-20s  Idle %s\n", p.Name, formatIdleTime(p.IdleTime)))
	}

	// Output footer
	write(fmt.Fprintln(w, "---------------"))
	if len(players) == 1 {
		write(fmt.Fprintln(w, "1 player online."))
	} else {
		write(fmt.Fprintf(w, "%d players online.\n", len(players)))
	}

	return totalBytes, firstErr
}

// formatIdleTime formats a duration as a human-readable idle time.
func formatIdleTime(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}

	// Round to nearest second
	d = d.Round(time.Second)

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}
