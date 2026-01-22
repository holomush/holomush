// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil

import (
	"log/slog"

	"github.com/samber/oops"
)

// LogError logs an error with structured context if it's an oops error.
// For oops errors, it extracts and logs the message, code, context, and stacktrace.
// For standard errors, it logs the error string.
func LogError(logger *slog.Logger, msg string, err error) {
	if oopsErr, ok := oops.AsOops(err); ok {
		attrs := []any{
			"error", oopsErr.Error(),
		}
		if code := oopsErr.Code(); code != nil {
			attrs = append(attrs, "code", code)
		}
		if ctx := oopsErr.Context(); len(ctx) > 0 {
			attrs = append(attrs, "context", ctx)
		}
		logger.Error(msg, attrs...)
	} else {
		logger.Error(msg, "error", err)
	}
}
