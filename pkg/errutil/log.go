// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package errutil

import (
	"context"
	"log/slog"

	"github.com/samber/oops"
)

// LogError logs an error with structured context if it's an oops error.
// For oops errors, it extracts and logs the message, code, context, and stacktrace.
// For standard errors, it logs the error string.
func LogError(logger *slog.Logger, msg string, err error) {
	logger.Error(msg, oopsAttrs(err)...)
}

// LogErrorContext logs an error at ERROR level with structured context from oops errors,
// forwarding the context.Context for trace/span ID propagation. This combines the
// benefits of LogError (structured oops field extraction) with context-aware logging.
func LogErrorContext(ctx context.Context, msg string, err error, extraAttrs ...any) {
	attrs := oopsAttrs(err)
	attrs = append(attrs, extraAttrs...)
	slog.ErrorContext(ctx, msg, attrs...)
}

// oopsAttrs extracts structured attributes from an error. For oops errors, it returns
// the error message, code, and context map. For standard errors, just the error string.
func oopsAttrs(err error) []any {
	oopsErr, ok := oops.AsOops(err)
	if !ok {
		return []any{"error", err}
	}
	attrs := []any{
		"error", oopsErr.Error(),
	}
	if code := oopsErr.Code(); code != nil {
		attrs = append(attrs, "code", code)
	}
	if oopsCtx := oopsErr.Context(); len(oopsCtx) > 0 {
		attrs = append(attrs, "context", oopsCtx)
	}
	return attrs
}
