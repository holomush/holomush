// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//nolint:wrapcheck // transparent middleware wrapper; re-wrapping errors adds no value
package plugins

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// Compile-time interface check.
var _ Host = (*HostMiddleware)(nil)

// HostMiddleware wraps a Host with OpenTelemetry tracing and metrics.
type HostMiddleware struct {
	next   Host
	tracer trace.Tracer
	meter  metric.Meter

	commandDuration metric.Float64Histogram
	commandErrors   metric.Int64Counter
	eventDuration   metric.Float64Histogram
	eventErrors     metric.Int64Counter
	eventsEmitted   metric.Int64Counter
}

// NewHostMiddleware wraps a Host with OTel instrumentation.
func NewHostMiddleware(next Host, tp trace.TracerProvider, mp metric.MeterProvider) (*HostMiddleware, error) {
	tracer := tp.Tracer("holomush.plugin")
	meter := mp.Meter("holomush.plugin")

	commandDuration, err := meter.Float64Histogram(
		"plugin_command_duration_seconds",
		metric.WithDescription("Duration of plugin command delivery"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	commandErrors, err := meter.Int64Counter(
		"plugin_errors_total",
		metric.WithDescription("Total plugin errors"),
	)
	if err != nil {
		return nil, err
	}

	eventDuration, err := meter.Float64Histogram(
		"plugin_event_duration_seconds",
		metric.WithDescription("Duration of plugin event delivery"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, err
	}

	eventErrors, err := meter.Int64Counter(
		"plugin_event_errors_total",
		metric.WithDescription("Total plugin event errors"),
	)
	if err != nil {
		return nil, err
	}

	eventsEmitted, err := meter.Int64Counter(
		"plugin_events_emitted_total",
		metric.WithDescription("Total events emitted by plugins"),
	)
	if err != nil {
		return nil, err
	}

	return &HostMiddleware{
		next:            next,
		tracer:          tracer,
		meter:           meter,
		commandDuration: commandDuration,
		commandErrors:   commandErrors,
		eventDuration:   eventDuration,
		eventErrors:     eventErrors,
		eventsEmitted:   eventsEmitted,
	}, nil
}

// Load delegates to the wrapped host.
func (m *HostMiddleware) Load(ctx context.Context, manifest *Manifest, dir string) error {
	return m.next.Load(ctx, manifest, dir)
}

// Unload delegates to the wrapped host.
func (m *HostMiddleware) Unload(ctx context.Context, name string) error {
	return m.next.Unload(ctx, name)
}

// DeliverCommand instruments command delivery with tracing and metrics.
func (m *HostMiddleware) DeliverCommand(ctx context.Context, name string, cmd pluginsdk.CommandRequest) (*pluginsdk.CommandResponse, error) {
	attrs := []attribute.KeyValue{
		attribute.String("plugin.name", name),
		attribute.String("command.name", cmd.Command),
	}

	ctx, span := m.tracer.Start(
		ctx, "plugin.command",
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	start := time.Now()
	resp, err := m.next.DeliverCommand(ctx, name, cmd)
	duration := time.Since(start).Seconds()

	metricAttrs := metric.WithAttributes(
		attribute.String("plugin", name),
		attribute.String("command", cmd.Command),
	)
	m.commandDuration.Record(ctx, duration, metricAttrs)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		m.commandErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("plugin", name),
			attribute.String("kind", "command_error"),
		))
		return nil, err
	}

	if resp != nil {
		m.eventsEmitted.Add(ctx, int64(len(resp.Events)), metric.WithAttributes(
			attribute.String("plugin", name),
		))
	}

	return resp, nil
}

// DeliverEvent instruments event delivery with tracing and metrics.
func (m *HostMiddleware) DeliverEvent(ctx context.Context, name string, event pluginsdk.Event) ([]pluginsdk.EmitEvent, error) {
	attrs := []attribute.KeyValue{
		attribute.String("plugin.name", name),
		attribute.String("event.type", string(event.Type)),
	}

	ctx, span := m.tracer.Start(
		ctx, "plugin.event",
		trace.WithAttributes(attrs...),
	)
	defer span.End()

	start := time.Now()
	emits, err := m.next.DeliverEvent(ctx, name, event)
	duration := time.Since(start).Seconds()

	metricAttrs := metric.WithAttributes(
		attribute.String("plugin", name),
		attribute.String("event_type", string(event.Type)),
	)
	m.eventDuration.Record(ctx, duration, metricAttrs)

	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		m.eventErrors.Add(ctx, 1, metric.WithAttributes(
			attribute.String("plugin", name),
			attribute.String("kind", "event_error"),
		))
		return nil, err
	}

	m.eventsEmitted.Add(ctx, int64(len(emits)), metric.WithAttributes(
		attribute.String("plugin", name),
	))

	return emits, nil
}

// QuerySessionStreams delegates to the wrapped host.
func (m *HostMiddleware) QuerySessionStreams(ctx context.Context, name string, req SessionStreamsRequest) ([]string, error) {
	return m.next.QuerySessionStreams(ctx, name, req)
}

// Plugins delegates to the wrapped host.
func (m *HostMiddleware) Plugins() []string {
	return m.next.Plugins()
}

// Close delegates to the wrapped host.
func (m *HostMiddleware) Close(ctx context.Context) error {
	return m.next.Close(ctx)
}

// Unwrap returns the underlying host. Used by the manager to discover
// optional interfaces (ServiceConnProvider, AttributeResolverProvider) that
// only some host implementations support.
func (m *HostMiddleware) Unwrap() Host {
	return m.next
}
