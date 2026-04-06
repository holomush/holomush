// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package plugins_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/internal/plugin/mocks"
	pluginsdk "github.com/holomush/holomush/pkg/plugin"
)

// otelTestHarness bundles OTel test exporters for reuse across tests.
type otelTestHarness struct {
	spanExporter   *tracetest.InMemoryExporter
	metricReader   *sdkmetric.ManualReader
	tracerProvider *sdktrace.TracerProvider
	meterProvider  *sdkmetric.MeterProvider
}

func newOTelTestHarness() *otelTestHarness {
	spanExp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spanExp))

	metricReader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(metricReader))

	return &otelTestHarness{
		spanExporter:   spanExp,
		metricReader:   metricReader,
		tracerProvider: tp,
		meterProvider:  mp,
	}
}

func (h *otelTestHarness) spans() tracetest.SpanStubs {
	return h.spanExporter.GetSpans()
}

func (h *otelTestHarness) metrics(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.metricReader.Collect(context.Background(), &rm))
	return rm
}

func (h *otelTestHarness) shutdown(t *testing.T) {
	t.Helper()
	require.NoError(t, h.tracerProvider.Shutdown(context.Background()))
	require.NoError(t, h.meterProvider.Shutdown(context.Background()))
}

// --- HostMiddleware tests ---

func TestHostMiddlewareDeliverCommandSuccess(t *testing.T) {
	h := newOTelTestHarness()
	defer h.shutdown(t)

	mockHost := mocks.NewMockHost(t)
	resp := &pluginsdk.CommandResponse{
		Status: pluginsdk.CommandOK,
		Output: "hello",
		Events: []pluginsdk.EmitEvent{{Stream: "loc", Type: "say"}},
	}
	mockHost.EXPECT().
		DeliverCommand(mock.Anything, "test-plugin", mock.Anything).
		Return(resp, nil)

	mw, err := plugins.NewHostMiddleware(mockHost, h.tracerProvider, h.meterProvider)
	require.NoError(t, err)

	cmd := pluginsdk.CommandRequest{Command: "say", Args: "hi"}
	got, err := mw.DeliverCommand(context.Background(), "test-plugin", cmd)
	require.NoError(t, err)
	assert.Equal(t, "hello", got.Output)

	spans := h.spans()
	require.Len(t, spans, 1)
	assert.Equal(t, "plugin.command", spans[0].Name)
	assertSpanAttribute(t, spans[0], "plugin.name", "test-plugin")
	assertSpanAttribute(t, spans[0], "command.name", "say")

	rm := h.metrics(t)
	assertHistogramExists(t, rm, "plugin_command_duration_seconds")
	assertCounterValue(t, rm, "plugin_events_emitted_total", 1)
}

func TestHostMiddlewareDeliverCommandError(t *testing.T) {
	h := newOTelTestHarness()
	defer h.shutdown(t)

	mockHost := mocks.NewMockHost(t)
	mockHost.EXPECT().
		DeliverCommand(mock.Anything, "fail-plugin", mock.Anything).
		Return(nil, errors.New("boom"))

	mw, err := plugins.NewHostMiddleware(mockHost, h.tracerProvider, h.meterProvider)
	require.NoError(t, err)

	cmd := pluginsdk.CommandRequest{Command: "bad"}
	_, err = mw.DeliverCommand(context.Background(), "fail-plugin", cmd)
	require.Error(t, err)

	spans := h.spans()
	require.Len(t, spans, 1)
	assert.Equal(t, codes.Error, spans[0].Status.Code, "span should have error status")

	rm := h.metrics(t)
	assertCounterValue(t, rm, "plugin_errors_total", 1)
}

func TestHostMiddlewareDeliverEventSuccess(t *testing.T) {
	h := newOTelTestHarness()
	defer h.shutdown(t)

	mockHost := mocks.NewMockHost(t)
	emits := []pluginsdk.EmitEvent{
		{Stream: "loc", Type: "say"},
		{Stream: "loc", Type: "pose"},
	}
	mockHost.EXPECT().
		DeliverEvent(mock.Anything, "event-plugin", mock.Anything).
		Return(emits, nil)

	mw, err := plugins.NewHostMiddleware(mockHost, h.tracerProvider, h.meterProvider)
	require.NoError(t, err)

	event := pluginsdk.Event{Type: pluginsdk.EventTypeSay}
	got, err := mw.DeliverEvent(context.Background(), "event-plugin", event)
	require.NoError(t, err)
	assert.Len(t, got, 2)

	spans := h.spans()
	require.Len(t, spans, 1)
	assert.Equal(t, "plugin.event", spans[0].Name)
	assertSpanAttribute(t, spans[0], "plugin.name", "event-plugin")
	assertSpanAttribute(t, spans[0], "event.type", "say")

	rm := h.metrics(t)
	assertHistogramExists(t, rm, "plugin_event_duration_seconds")
	assertCounterValue(t, rm, "plugin_events_emitted_total", 2)
}

func TestHostMiddlewareDeliverEventError(t *testing.T) {
	h := newOTelTestHarness()
	defer h.shutdown(t)

	mockHost := mocks.NewMockHost(t)
	mockHost.EXPECT().
		DeliverEvent(mock.Anything, "err-plugin", mock.Anything).
		Return(nil, errors.New("event-fail"))

	mw, err := plugins.NewHostMiddleware(mockHost, h.tracerProvider, h.meterProvider)
	require.NoError(t, err)

	event := pluginsdk.Event{Type: pluginsdk.EventTypePose}
	_, err = mw.DeliverEvent(context.Background(), "err-plugin", event)
	require.Error(t, err)

	rm := h.metrics(t)
	assertCounterValue(t, rm, "plugin_event_errors_total", 1)
}

func TestHostMiddlewarePassthrough(t *testing.T) {
	h := newOTelTestHarness()
	defer h.shutdown(t)

	mockHost := mocks.NewMockHost(t)
	mockHost.EXPECT().Plugins().Return([]string{"a", "b"})
	mockHost.EXPECT().Close(mock.Anything).Return(nil)
	mockHost.EXPECT().Load(mock.Anything, mock.Anything, "dir").Return(nil)
	mockHost.EXPECT().Unload(mock.Anything, "a").Return(nil)

	mw, err := plugins.NewHostMiddleware(mockHost, h.tracerProvider, h.meterProvider)
	require.NoError(t, err)

	assert.Equal(t, []string{"a", "b"}, mw.Plugins())
	require.NoError(t, mw.Load(context.Background(), &plugins.Manifest{}, "dir"))
	require.NoError(t, mw.Unload(context.Background(), "a"))
	require.NoError(t, mw.Close(context.Background()))
}

// --- Test helpers ---

func assertSpanAttribute(t *testing.T, span tracetest.SpanStub, key, expected string) {
	t.Helper()
	for _, attr := range span.Attributes {
		if string(attr.Key) == key {
			assert.Equal(t, expected, attr.Value.AsString())
			return
		}
	}
	t.Errorf("span attribute %q not found", key)
}

func assertHistogramExists(t *testing.T, rm metricdata.ResourceMetrics, name string) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return
			}
		}
	}
	t.Errorf("histogram %q not found in metrics", name)
}

func assertCounterValue(t *testing.T, rm metricdata.ResourceMetrics, name string, expected int64) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					var total int64
					for _, dp := range sum.DataPoints {
						total += dp.Value
					}
					assert.Equal(t, expected, total, "counter %s value", name)
					return
				}
				t.Errorf("metric %q is not a Sum[int64]", name)
				return
			}
		}
	}
	t.Errorf("counter %q not found in metrics", name)
}
