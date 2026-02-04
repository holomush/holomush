// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/holomush/holomush/internal/command"
)

// WallUrgency represents the urgency level of a wall message.
type WallUrgency string

// Wall urgency levels for admin broadcast messages.
const (
	WallUrgencyInfo     WallUrgency = "info"     // Normal announcements
	WallUrgencyWarning  WallUrgency = "warning"  // Warning messages
	WallUrgencyCritical WallUrgency = "critical" // Critical alerts
)

// urgencyPrefixes maps urgency levels to display prefixes.
var urgencyPrefixes = map[WallUrgency]string{
	WallUrgencyInfo:     "[ADMIN ANNOUNCEMENT]",
	WallUrgencyWarning:  "[ADMIN WARNING]",
	WallUrgencyCritical: "[ADMIN CRITICAL]",
}

// WallHandler broadcasts an announcement to all connected sessions.
// Requires admin.wall capability (checked by dispatcher).
// Usage: wall [level] <message>
// Levels: info (default), warning, critical
func WallHandler(_ context.Context, exec *command.CommandExecution) error {
	args := strings.TrimSpace(exec.Args)
	if args == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("wall", "wall [info|warning|critical] <message>")
	}

	// Parse urgency level and message
	urgency, message := parseWallArgs(args)
	if message == "" {
		//nolint:wrapcheck // ErrInvalidArgs creates a structured oops error
		return command.ErrInvalidArgs("wall", "wall [info|warning|critical] <message>")
	}

	// Get all active sessions
	sessions := exec.Services.Session.ListActiveSessions()

	// Format the announcement message
	prefix := urgencyPrefixes[urgency]
	announcement := fmt.Sprintf("%s %s: %s", prefix, exec.CharacterName, message)

	// Log admin action
	slog.Info("admin wall",
		"admin_id", exec.CharacterID.String(),
		"admin_name", exec.CharacterName,
		"urgency", string(urgency),
		"message", message,
		"session_count", len(sessions),
	)

	// Broadcast to all sessions
	for _, session := range sessions {
		stream := "session:" + session.CharacterID.String()
		exec.Services.BroadcastSystemMessage(stream, announcement)
	}

	// Notify the executor
	sessionWord := "sessions"
	if len(sessions) == 1 {
		sessionWord = "session"
	}
	//nolint:errcheck // output write error is acceptable; player display is best-effort
	_, _ = fmt.Fprintf(exec.Output, "Announcement sent to %d %s.\n", len(sessions), sessionWord)

	return nil
}

// parseWallArgs parses the wall command arguments into urgency level and message.
// If the first word is a valid urgency level, it's used; otherwise defaults to info.
func parseWallArgs(args string) (urgency WallUrgency, message string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) == 1 {
		// Single word - treat as message with default urgency
		return WallUrgencyInfo, parts[0]
	}

	// Check if first word is a valid urgency level
	firstWord := strings.ToLower(parts[0])
	switch firstWord {
	case "info":
		return WallUrgencyInfo, parts[1]
	case "warning", "warn":
		return WallUrgencyWarning, parts[1]
	case "critical", "crit":
		return WallUrgencyCritical, parts[1]
	default:
		// First word is not an urgency level, treat entire args as message
		return WallUrgencyInfo, args
	}
}
