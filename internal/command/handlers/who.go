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
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/internal/world"
)

// maxEngineErrors is the circuit breaker threshold for the who handler.
// After this many total engine errors (access evaluation failures), the handler stops
// querying the engine to prevent amplifying load on a degraded system.
// Independent from hostfunc/commands.go: who iterates sessions while command-list
// iterates registry entries — different cardinalities and failure impacts.
const maxEngineErrors = 3

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

	subjectID := access.CharacterSubject(exec.CharacterID().String())
	now := time.Now()

	// Collect visible players
	players := make([]playerInfo, 0, len(sessions))
	var errorCount int
	var engineErrorCount int
	var permDeniedCount int
	var skippedCount int
	for i, session := range sessions {
		// Circuit breaker: stop querying if the engine is consistently failing.
		// Track skipped sessions separately from actual errors for accurate messaging.
		if engineErrorCount >= maxEngineErrors {
			skippedCount = len(sessions) - i
			slog.WarnContext(ctx, "who handler circuit breaker tripped: aborting after engine failures",
				"engine_failures", engineErrorCount,
				"threshold", maxEngineErrors,
				"skipped_sessions", skippedCount,
			)
			observability.RecordCircuitBreakerTrip("who", skippedCount)
			break
		}

		// Try to get character info - skip if not accessible
		char, err := exec.Services().World().GetCharacter(ctx, subjectID, session.CharacterID)
		if err != nil {
			// Skip expected errors (not found, permission denied)
			// - permission denied and not found are expected, don't log or count
			if errors.Is(err, world.ErrPermissionDenied) {
				permDeniedCount++
				continue
			}
			if errors.Is(err, world.ErrNotFound) {
				continue
			}
			// Access evaluation failures are already logged by the WorldService.checkAccess method.
			// Count them (but don't re-log) so users see the error notice.
			if errors.Is(err, world.ErrAccessEvaluationFailed) {
				errorCount++
				engineErrorCount++
				continue
			}
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

	// Anomaly detection: if ALL sessions were denied by the policy engine,
	// this likely indicates a broken policy seed rather than expected behavior.
	if permDeniedCount > 0 && permDeniedCount == len(sessions) {
		slog.WarnContext(ctx, "who handler: all sessions denied by policy engine — possible policy misconfiguration",
			"total_sessions", len(sessions),
			"permission_denied_count", permDeniedCount,
			"subject", subjectID,
		)
	}

	// Symmetric anomaly detection for engine errors: if ALL sessions failed
	// with engine errors (or were skipped by the circuit breaker after engine errors),
	// this indicates a possible engine outage. Account for skipped sessions here
	// because the circuit breaker caps engineErrorCount at maxEngineErrors even when
	// all remaining sessions would have failed too.
	if engineErrorCount > 0 && (engineErrorCount+skippedCount) >= len(sessions) {
		slog.WarnContext(ctx, "who handler: all sessions failed with engine errors — possible engine outage",
			"total_sessions", len(sessions),
			"engine_error_count", engineErrorCount,
			"skipped_count", skippedCount,
		)
	}

	if n, err := writeWhoOutput(exec.Output(), players); err != nil {
		logOutputError(ctx, "who", exec.CharacterID().String(), n, err)
	}

	// Warn user if any characters couldn't be displayed due to errors or circuit breaker.
	// Output write errors are logged but don't fail the command.
	if errorCount > 0 || skippedCount > 0 {
		switch {
		case errorCount > 0 && skippedCount > 0:
			writeOutputf(ctx, exec, "who",
				"(Note: %d players could not be displayed due to system errors, %d skipped due to circuit breaker)\n",
				errorCount, skippedCount)
		case errorCount == 1:
			writeOutput(ctx, exec, "who", "(Note: 1 player could not be displayed due to a system error)")
		case errorCount > 1:
			writeOutputf(ctx, exec, "who", "(Note: %d players could not be displayed due to system errors)\n", errorCount)
		default:
			// Defensive: skippedCount > 0 without errorCount > 0 should be unreachable
			// (circuit breaker only trips after engine errors), but guard against logic drift.
			if skippedCount > 0 {
				writeOutputf(ctx, exec, "who", "(Note: %d players skipped due to system issues)\n", skippedCount)
			}
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
