// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFanout_TeesToAllChildren(t *testing.T) {
	var a, b bytes.Buffer
	h := NewFanout(
		slog.NewJSONHandler(&a, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	logger := slog.New(h)
	logger.Info("hello")
	require.Contains(t, a.String(), "hello")
	require.Contains(t, b.String(), "hello")
}

func TestLevelGate_FiltersBelowMin(t *testing.T) {
	var buf bytes.Buffer
	gated := NewLevelGate(slog.LevelWarn, slog.NewJSONHandler(&buf, nil))
	logger := slog.New(gated)
	logger.Info("dropped")
	logger.Warn("kept")
	out := buf.String()
	require.False(t, strings.Contains(out, "dropped"))
	require.True(t, strings.Contains(out, "kept"))
}

func TestFanout_SingleChildIsTransparent(t *testing.T) {
	var buf bytes.Buffer
	h := NewFanout(slog.NewJSONHandler(&buf, nil))
	require.True(t, h.Enabled(context.Background(), slog.LevelError))
}
