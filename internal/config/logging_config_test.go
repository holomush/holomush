// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package config

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoggingConfig_Defaults(t *testing.T) {
	c := DefaultLoggingConfig()
	require.True(t, c.Stderr.Enabled)
	require.True(t, c.OTel.Enabled)
	require.True(t, c.Sentry.Enabled)

	// Stderr and the collector inherit the global level (empty per-sink
	// level); the Sentry sink defaults to WARN so info/debug never reach
	// the Sentry Logs view regardless of the global level.
	require.Empty(t, c.Stderr.Level)
	require.Empty(t, c.OTel.Level)
	require.Equal(t, SentryLogLevelDefault, c.Sentry.Level)

	// The default Sentry level must actually resolve to WARN, and must hold
	// that floor even when the global level is lower (debug).
	require.Equal(t, slog.LevelWarn, c.Sentry.EffectiveLevel(slog.LevelDebug))
}

func TestLoggingSink_EffectiveLevel(t *testing.T) { // INV-L4
	global := slog.LevelInfo
	require.Equal(t, slog.LevelWarn, LoggingSink{Level: "warn"}.EffectiveLevel(global))
	require.Equal(t, global, LoggingSink{}.EffectiveLevel(global)) // unset → global
	require.Equal(t, slog.LevelError, LoggingSink{Level: "error"}.EffectiveLevel(global))
	require.Equal(t, global, LoggingSink{Level: "bogus"}.EffectiveLevel(global)) // unparseable → global
	require.Equal(t, slog.LevelDebug, LoggingSink{Level: "debug"}.EffectiveLevel(global))
	require.Equal(t, slog.LevelInfo, LoggingSink{Level: "info"}.EffectiveLevel(global))
}
