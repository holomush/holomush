// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package logging provides structured logging with OpenTelemetry trace context.
package logging

import (
	"context"
	"errors"
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

// Setup creates a stderr-only logger (unchanged public behaviour).
// format: "json" or "text" (defaults to "json" if empty)
// If w is nil, writes to os.Stderr.
func Setup(service, version, format string, w io.Writer, level slog.Level) *slog.Logger {
	return SetupWithBridge(service, version, format, w, true, level, nil, level)
}

// SetupWithBridge creates a logger that tees to up to two sinks: stderr
// (when stderrEnabled) and an OTel bridge handler (when bridge != nil),
// both independently gate-able (INV-L2). When all sinks are disabled the
// returned logger discards all records. When bridge is nil and stderrEnabled
// the result is the stderr-only logger (INV-L7).
func SetupWithBridge(
	service, version, format string, w io.Writer,
	stderrEnabled bool, stderrLevel slog.Level,
	bridge slog.Handler, bridgeLevel slog.Level,
) *slog.Logger {
	var handlers []slog.Handler
	if stderrEnabled {
		if w == nil {
			w = os.Stderr
		}
		opts := &slog.HandlerOptions{Level: stderrLevel}
		var base slog.Handler
		if format == "text" {
			base = slog.NewTextHandler(w, opts)
		} else {
			base = slog.NewJSONHandler(w, opts)
		}
		handlers = append(handlers, &traceHandler{handler: base, service: service, version: version})
	}
	if bridge != nil {
		handlers = append(handlers, NewLevelGate(bridgeLevel, bridge))
	}
	switch len(handlers) {
	case 0:
		// All sinks disabled — discard. Unusual but a valid operator choice.
		return slog.New(slog.DiscardHandler)
	case 1:
		return slog.New(handlers[0])
	default:
		return slog.New(NewFanout(handlers...))
	}
}

// SetDefault sets up and configures the default logger.
func SetDefault(service, version, format string, level slog.Level) {
	logger := Setup(service, version, format, nil, level)
	slog.SetDefault(logger)
}

// SetDefaultWithBridge configures the default logger to tee stderr and,
// when bridge != nil, an OTel bridge handler. Mirrors SetDefault.
// stderrEnabled controls whether the stderr sink is active (INV-L2).
func SetDefaultWithBridge(service, version, format string, stderrEnabled bool, stderrLevel slog.Level, bridge slog.Handler, bridgeLevel slog.Level) {
	logger := SetupWithBridge(service, version, format, nil, stderrEnabled, stderrLevel, bridge, bridgeLevel)
	slog.SetDefault(logger)
}

// fanoutHandler dispatches each record to every child handler. Enabled is
// the OR of children, so a record is processed if any sink wants it; each
// child then applies its own level/filtering. Used to tee stderr logging
// and the OTel bridge from one slog.Logger (INV-L2).
type fanoutHandler struct{ children []slog.Handler }

// NewFanout returns a handler that tees to all children. With a single child
// it is transparent (degenerate case, INV-L7): no panic, behaviour identical
// to using that child directly.
func NewFanout(children ...slog.Handler) slog.Handler {
	return &fanoutHandler{children: children}
}

func (h *fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, c := range h.children {
		if c.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	// Write to every enabled child even if one fails — a failing stderr sink
	// must not suppress the OTel/Sentry sink (or vice versa). Aggregate errors.
	var errs error
	for _, c := range h.children {
		if !c.Enabled(ctx, r.Level) {
			continue
		}
		if err := c.Handle(ctx, r.Clone()); err != nil {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

func (h *fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.children))
	for i, c := range h.children {
		next[i] = c.WithAttrs(attrs)
	}
	return &fanoutHandler{children: next}
}

func (h *fanoutHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.children))
	for i, c := range h.children {
		next[i] = c.WithGroup(name)
	}
	return &fanoutHandler{children: next}
}

// levelGate wraps a handler with a minimum level, giving a per-sink floor
// (INV-L4) independent of the global logger level.
type levelGate struct {
	min     slog.Level
	handler slog.Handler
}

// NewLevelGate returns a handler that only passes records at or above min to h.
func NewLevelGate(minLevel slog.Level, h slog.Handler) slog.Handler {
	return &levelGate{min: minLevel, handler: h}
}

func (g *levelGate) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= g.min && g.handler.Enabled(ctx, level)
}

func (g *levelGate) Handle(ctx context.Context, r slog.Record) error {
	return g.handler.Handle(ctx, r) //nolint:wrapcheck // slog.Handler contract: pass child error through
}

func (g *levelGate) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &levelGate{min: g.min, handler: g.handler.WithAttrs(attrs)}
}

func (g *levelGate) WithGroup(name string) slog.Handler {
	return &levelGate{min: g.min, handler: g.handler.WithGroup(name)}
}
