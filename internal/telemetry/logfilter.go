// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"

	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// levelFilter is a log.Processor that forwards only records at or above a
// minimum severity to the wrapped downstream processor. It is the mechanism
// that lets a single LoggerProvider give the collector and Sentry sinks
// independent severity floors (spec INV-L4).
type levelFilter struct {
	next sdklog.Processor
	min  otellog.Severity
}

func newLevelFilter(next sdklog.Processor, minSeverity otellog.Severity) *levelFilter {
	return &levelFilter{next: next, min: minSeverity}
}

func (f *levelFilter) OnEmit(ctx context.Context, rec *sdklog.Record) error {
	if rec.Severity() < f.min {
		return nil
	}
	return f.next.OnEmit(ctx, rec) //nolint:wrapcheck // delegating passthrough to wrapped sdklog.Processor
}

// Enabled delegates to the wrapped processor. Severity filtering is applied at
// OnEmit time; callers must not rely on Enabled to reflect the level floor.
func (f *levelFilter) Enabled(ctx context.Context, param sdklog.EnabledParameters) bool {
	return f.next.Enabled(ctx, param)
}

func (f *levelFilter) Shutdown(ctx context.Context) error {
	return f.next.Shutdown(ctx) //nolint:wrapcheck // delegating passthrough to wrapped sdklog.Processor
}

func (f *levelFilter) ForceFlush(ctx context.Context) error {
	return f.next.ForceFlush(ctx) //nolint:wrapcheck // delegating passthrough to wrapped sdklog.Processor
}
