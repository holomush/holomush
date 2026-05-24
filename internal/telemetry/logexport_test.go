// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package telemetry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSentryLogsTarget(t *testing.T) { // INV-L8
	dsn := "https://abc123@o4509.ingest.us.sentry.io/4510"
	url, header, err := sentryLogsTarget(dsn)
	require.NoError(t, err)
	require.Equal(t, "https://o4509.ingest.us.sentry.io/api/4510/integration/otlp/v1/logs", url)
	require.Equal(t, "sentry sentry_key=abc123", header)
}

func TestSentryLogsTarget_Invalid(t *testing.T) {
	_, _, err := sentryLogsTarget("not a dsn")
	require.Error(t, err)
}
