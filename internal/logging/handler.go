// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package logging provides structured logging with OpenTelemetry trace context.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// traceHandler wraps a slog.Handler to add trace context.
type traceHandler struct {
	handler slog.Handler
	service string
	version string
}

// Handle adds trace context to the log record.
func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	// Add service and version
	r.AddAttrs(
		slog.String("service", h.service),
		slog.String("version", h.version),
	)

	// Extract trace context if present
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.HasTraceID() {
		r.AddAttrs(slog.String("trace_id", spanCtx.TraceID().String()))
	}
	if spanCtx.HasSpanID() {
		r.AddAttrs(slog.String("span_id", spanCtx.SpanID().String()))
	}

	//nolint:wrapcheck // Handler interface requires unwrapped error passthrough
	return h.handler.Handle(ctx, r)
}

// Enabled returns true if the level is enabled.
func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

// WithAttrs returns a new handler with the given attributes.
func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{
		handler: h.handler.WithAttrs(attrs),
		service: h.service,
		version: h.version,
	}
}

// WithGroup returns a new handler with the given group.
func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{
		handler: h.handler.WithGroup(name),
		service: h.service,
		version: h.version,
	}
}

// Setup creates a configured slog.Logger.
// format: "json" or "text" (defaults to "json" if empty)
// If w is nil, writes to os.Stderr.
func Setup(service, version, format string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}

	var baseHandler slog.Handler
	opts := &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}

	if format == "text" {
		baseHandler = slog.NewTextHandler(w, opts)
	} else {
		baseHandler = slog.NewJSONHandler(w, opts)
	}

	handler := &traceHandler{
		handler: baseHandler,
		service: service,
		version: version,
	}

	return slog.New(handler)
}

// SetDefault sets up and configures the default logger.
func SetDefault(service, version, format string) {
	logger := Setup(service, version, format, nil)
	slog.SetDefault(logger)
}
