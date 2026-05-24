// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/contrib/bridges/otelslog"
)

// TestLogSDKSurface pins the experimental v0.x API shapes this epic relies
// on. If an OTel log-module bump changes any of these, this test breaks
// first and the implementer reconciles the rest of the package against it.
func TestLogSDKSurface(t *testing.T) {
	lp := sdklog.NewLoggerProvider() // no processors → no-op
	t.Cleanup(func() { _ = lp.Shutdown(context.Background()) })

	h := otelslog.NewHandler("holomush-test", otelslog.WithLoggerProvider(lp))
	require.NotNil(t, h)

	require.Equal(t, otellog.SeverityWarn, otellog.SeverityWarn)
}
