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

	"github.com/oklog/ulid/v2"
	"github.com/samber/oops"

	"github.com/holomush/holomush/internal/access"
	"github.com/holomush/holomush/internal/command"
	"github.com/holomush/holomush/internal/observability"
	"github.com/holomush/holomush/internal/world"
)

// maxWhereEngineErrors is the circuit breaker threshold for the where handler.
// After this many total engine errors (access evaluation failures), the handler
// stops querying to prevent amplifying load on a degraded system.
const maxWhereEngineErrors = 3

// whereEntry holds display information for one visible character.
type whereEntry struct {
	CharacterName string
	LocationName  string
}

// WhereHandler displays connected characters and their locations for social discovery.
func WhereHandler(ctx context.Context, exec *command.CommandExecution) error {
	sessions, err := exec.Services().Session().ListActive(ctx)
	if err != nil {
		return oops.Code(command.CodeWorldError).
			With("message", "Unable to retrieve player list. Please try again.").
			Wrap(err)
	}

	if len(sessions) == 0 {
		if n, err := writeWhereOutput(exec.Output(), nil); err != nil {
			logOutputError(ctx, "where", exec.CharacterID().String(), n, err)
		}
		return nil
	}

	subjectID := access.CharacterSubject(exec.CharacterID().String())

	entries := make([]whereEntry, 0, len(sessions))
	var errorCount int
	var engineErrorCount int
	var permDeniedCount int
	var skippedCount int

	for i, sess := range sessions {
		// Circuit breaker: stop querying if the engine is consistently failing.
		if engineErrorCount >= maxWhereEngineErrors {
			skippedCount = len(sessions) - i
			slog.WarnContext(ctx, "where handler circuit breaker tripped: aborting after engine failures",
				"engine_failures", engineErrorCount,
				"threshold", maxWhereEngineErrors,
				"skipped_sessions", skippedCount,
			)
			observability.RecordCircuitBreakerTrip("where", skippedCount)
			break
		}

		char, err := exec.Services().World().GetCharacter(ctx, subjectID, sess.CharacterID)
		if err != nil {
			if errors.Is(err, world.ErrPermissionDenied) {
				permDeniedCount++
				continue
			}
			if errors.Is(err, world.ErrNotFound) {
				continue
			}
			if errors.Is(err, world.ErrAccessEvaluationFailed) {
				errorCount++
				engineErrorCount++
				continue
			}
			slog.ErrorContext(ctx, "unexpected error looking up character in where list",
				"session_char_id", sess.CharacterID.String(),
				"error", err,
			)
			errorCount++
			continue
		}

		locationName := resolveLocationName(ctx, exec, subjectID, sess.LocationID)
		entries = append(entries, whereEntry{
			CharacterName: char.Name,
			LocationName:  locationName,
		})
	}

	// Anomaly detection: all sessions denied.
	if permDeniedCount > 0 && permDeniedCount == len(sessions) {
		slog.WarnContext(ctx, "where handler: all sessions denied by policy engine — possible policy misconfiguration",
			"total_sessions", len(sessions),
			"permission_denied_count", permDeniedCount,
			"subject", subjectID,
		)
	}

	// Anomaly detection: all sessions failed with engine errors.
	if engineErrorCount > 0 && (engineErrorCount+skippedCount) >= len(sessions) {
		slog.WarnContext(ctx, "where handler: all sessions failed with engine errors — possible engine outage",
			"total_sessions", len(sessions),
			"engine_error_count", engineErrorCount,
			"skipped_count", skippedCount,
		)
	}

	if n, err := writeWhereOutput(exec.Output(), entries); err != nil {
		logOutputError(ctx, "where", exec.CharacterID().String(), n, err)
	}

	if errorCount > 0 || skippedCount > 0 {
		switch {
		case errorCount > 0 && skippedCount > 0:
			writeOutputf(ctx, exec, "where",
				"(Note: %d characters could not be displayed due to system errors, %d skipped due to circuit breaker)\n",
				errorCount, skippedCount)
		case errorCount == 1:
			writeOutput(ctx, exec, "where", "(Note: 1 character could not be displayed due to a system error)")
		case errorCount > 1:
			writeOutputf(ctx, exec, "where", "(Note: %d characters could not be displayed due to system errors)\n", errorCount)
		default:
			if skippedCount > 0 {
				writeOutputf(ctx, exec, "where", "(Note: %d characters skipped due to system issues)\n", skippedCount)
			}
		}
	}
	return nil
}

// resolveLocationName returns the location name for the given locationID.
// Returns "[Private]" if the location is not accessible or cannot be found.
func resolveLocationName(ctx context.Context, exec *command.CommandExecution, subjectID string, locationID ulid.ULID) string {
	// Zero ULID means no location set — show as [Private].
	if locationID == (ulid.ULID{}) {
		return "[Private]"
	}

	loc, err := exec.Services().World().GetLocation(ctx, subjectID, locationID)
	if err != nil {
		// Permission denied and not found both result in [Private].
		if errors.Is(err, world.ErrPermissionDenied) ||
			errors.Is(err, world.ErrNotFound) ||
			errors.Is(err, world.ErrAccessEvaluationFailed) {
			return "[Private]"
		}
		slog.WarnContext(ctx, "unexpected error looking up location in where list",
			"location_id", locationID.String(),
			"error", err,
		)
		return "[Private]"
	}
	return loc.Name
}

// writeWhereOutput formats and writes the where list to the output.
// Returns total bytes written and the first error encountered (if any).
func writeWhereOutput(w io.Writer, entries []whereEntry) (int, error) {
	var totalBytes int
	var firstErr error

	write := func(n int, err error) {
		totalBytes += n
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if len(entries) == 0 {
		write(fmt.Fprintln(w, "=== Where Is Everyone? ==="))
		write(fmt.Fprintln(w, "No one is online."))
		write(fmt.Fprintln(w, "─────────────────────────────────"))
		write(fmt.Fprintln(w, "0 characters online"))
		return totalBytes, firstErr
	}

	// Sort by location name, then character name within each location.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].LocationName != entries[j].LocationName {
			return entries[i].LocationName < entries[j].LocationName
		}
		return entries[i].CharacterName < entries[j].CharacterName
	})

	write(fmt.Fprintln(w, "=== Where Is Everyone? ==="))
	write(fmt.Fprintf(w, "%-16s %s\n", "Character", "Location"))
	write(fmt.Fprintln(w, "─────────────────────────────────"))

	for _, e := range entries {
		write(fmt.Fprintf(w, "%-16s %s\n", e.CharacterName, e.LocationName))
	}

	write(fmt.Fprintln(w, "─────────────────────────────────"))
	if len(entries) == 1 {
		write(fmt.Fprintln(w, "1 character online"))
	} else {
		write(fmt.Fprintf(w, "%d characters online\n", len(entries)))
	}

	return totalBytes, firstErr
}
