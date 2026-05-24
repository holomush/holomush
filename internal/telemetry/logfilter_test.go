// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// recordingProcessor captures emitted records for assertions and tracks
// delegation of lifecycle/query methods.
type recordingProcessor struct {
	severities       []otellog.Severity
	enabledCalled    bool
	shutdownCalled   bool
	forceFlushCalled bool
	enabledResult    bool
	shutdownErr      error
	forceFlushErr    error
}

func (r *recordingProcessor) OnEmit(_ context.Context, rec *sdklog.Record) error {
	r.severities = append(r.severities, rec.Severity())
	return nil
}

func (r *recordingProcessor) Enabled(_ context.Context, _ sdklog.EnabledParameters) bool {
	r.enabledCalled = true
	return r.enabledResult
}

func (r *recordingProcessor) Shutdown(_ context.Context) error {
	r.shutdownCalled = true
	return r.shutdownErr
}

func (r *recordingProcessor) ForceFlush(_ context.Context) error {
	r.forceFlushCalled = true
	return r.forceFlushErr
}

func TestLevelFilter_DropsBelowThreshold(t *testing.T) { // INV-L4
	sink := &recordingProcessor{enabledResult: true}
	f := newLevelFilter(sink, otellog.SeverityWarn)

	for _, sev := range []otellog.Severity{
		otellog.SeverityInfo, otellog.SeverityWarn, otellog.SeverityError,
	} {
		var rec sdklog.Record
		rec.SetSeverity(sev)
		require.NoError(t, f.OnEmit(context.Background(), &rec))
	}

	require.Equal(t, []otellog.Severity{otellog.SeverityWarn, otellog.SeverityError}, sink.severities)
}

func TestLevelFilter_Enabled_DelegatesToNext(t *testing.T) {
	sink := &recordingProcessor{enabledResult: true}
	f := newLevelFilter(sink, otellog.SeverityWarn)

	// Below threshold: Enabled should still delegate (filter applies at OnEmit).
	params := sdklog.EnabledParameters{Severity: otellog.SeverityInfo}
	got := f.Enabled(context.Background(), params)
	require.True(t, got)
	require.True(t, sink.enabledCalled, "Enabled must delegate to the wrapped processor")
}

func TestLevelFilter_Enabled_DelegatesToNext_WhenFalse(t *testing.T) {
	sink := &recordingProcessor{enabledResult: false}
	f := newLevelFilter(sink, otellog.SeverityWarn)
	require.False(t, f.Enabled(context.Background(), sdklog.EnabledParameters{}))
	require.True(t, sink.enabledCalled)
}

func TestLevelFilter_Shutdown_DelegatesToNext(t *testing.T) {
	sentinel := errors.New("shutdown-err")
	sink := &recordingProcessor{shutdownErr: sentinel}
	f := newLevelFilter(sink, otellog.SeverityInfo)
	err := f.Shutdown(context.Background())
	require.ErrorIs(t, err, sentinel)
	require.True(t, sink.shutdownCalled)
}

func TestLevelFilter_ForceFlush_DelegatesToNext(t *testing.T) {
	sentinel := errors.New("flush-err")
	sink := &recordingProcessor{forceFlushErr: sentinel}
	f := newLevelFilter(sink, otellog.SeverityInfo)
	err := f.ForceFlush(context.Background())
	require.ErrorIs(t, err, sentinel)
	require.True(t, sink.forceFlushCalled)
}

// TestSlogToOTel_AllBands exercises every severity band of slogToOTel.
func TestSlogToOTel_AllBands(t *testing.T) {
	tests := []struct {
		in   slog.Level
		want otellog.Severity
	}{
		{slog.LevelDebug, otellog.SeverityDebug},
		{slog.LevelDebug - 4, otellog.SeverityDebug}, // below Debug → still Debug
		{slog.LevelInfo, otellog.SeverityInfo},
		{slog.LevelInfo + 1, otellog.SeverityInfo}, // between Info and Warn
		{slog.LevelWarn, otellog.SeverityWarn},
		{slog.LevelWarn + 1, otellog.SeverityWarn}, // between Warn and Error
		{slog.LevelError, otellog.SeverityError},
		{slog.LevelError + 4, otellog.SeverityError}, // above Error → still Error
	}
	for _, tt := range tests {
		got := slogToOTel(tt.in)
		require.Equal(t, tt.want, got, "slogToOTel(%v)", tt.in)
	}
}
