// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// recordingProcessor captures emitted records for assertions.
type recordingProcessor struct{ severities []otellog.Severity }

func (r *recordingProcessor) OnEmit(_ context.Context, rec *sdklog.Record) error {
	r.severities = append(r.severities, rec.Severity())
	return nil
}

func (r *recordingProcessor) Enabled(_ context.Context, _ sdklog.EnabledParameters) bool {
	return true
}
func (r *recordingProcessor) Shutdown(context.Context) error   { return nil }
func (r *recordingProcessor) ForceFlush(context.Context) error { return nil }

func TestLevelFilter_DropsBelowThreshold(t *testing.T) {
	sink := &recordingProcessor{}
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
	sink := &recordingProcessor{}
	f := newLevelFilter(sink, otellog.SeverityWarn)

	// Below threshold: Enabled should still delegate (filter applies at OnEmit).
	params := sdklog.EnabledParameters{Severity: otellog.SeverityInfo}
	require.True(t, f.Enabled(context.Background(), params))
}
