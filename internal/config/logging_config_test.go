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
}

func TestLoggingSink_EffectiveLevel(t *testing.T) { // INV-L4
	global := slog.LevelInfo
	require.Equal(t, slog.LevelWarn, LoggingSink{Level: "warn"}.EffectiveLevel(global))
	require.Equal(t, global, LoggingSink{}.EffectiveLevel(global)) // unset → global
	require.Equal(t, slog.LevelError, LoggingSink{Level: "error"}.EffectiveLevel(global))
	require.Equal(t, global, LoggingSink{Level: "bogus"}.EffectiveLevel(global)) // unparseable → global
}
