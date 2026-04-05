// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package hostfunc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/holomush/holomush/internal/idgen"
	"github.com/holomush/holomush/internal/world"
)

// PluginErrorContext carries context fields used when sanitizing and logging
// errors on behalf of a plugin. It provides enough information to log a useful
// diagnostic entry while keeping plugin-facing messages generic.
type PluginErrorContext struct {
	// Plugin is the name of the calling plugin (e.g. "core-help").
	Plugin string
	// Operation is the operation being performed (e.g. "get", "set", "query").
	Operation string
	// Subject is the entity type or subject category (e.g. "location", "key").
	Subject string
	// SubjectID is the identifier of the specific subject (e.g. a ULID or key name).
	SubjectID string
}

// SanitizeErrorForPlugin converts an internal error to a safe, generic message
// suitable for returning to a Lua plugin. It handles well-known error types
// with friendly messages and logs unexpected errors at ERROR level for operators,
// including a unique correlation ID so operators can match plugin-reported errors
// to server log entries.
//
// Returns an empty string for a nil error.
func SanitizeErrorForPlugin(ctx PluginErrorContext, err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, world.ErrNotFound) {
		return fmt.Sprintf("%s not found", ctx.Subject)
	}
	if errors.Is(err, world.ErrPermissionDenied) {
		return "access denied"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		slog.Warn("plugin operation timed out",
			"plugin", ctx.Plugin,
			"operation", ctx.Operation,
			"subject", ctx.Subject,
			"subject_id", ctx.SubjectID)
		return "operation timed out"
	}

	// Generate a correlation ID so operators can find this specific error in logs.
	errorID := idgen.New().String()
	slog.Error("internal error in plugin operation",
		"error_id", errorID,
		"plugin", ctx.Plugin,
		"operation", ctx.Operation,
		"subject", ctx.Subject,
		"subject_id", ctx.SubjectID,
		"error", err)
	return fmt.Sprintf("internal error (ref: %s)", errorID)
}
