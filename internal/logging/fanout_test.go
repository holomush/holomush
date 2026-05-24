// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package logging

import (
	"bytes"
	"context"
	"errors"
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

// TestFanout_WithAttrs_PropagatesToAllChildren verifies that WithAttrs is
// forwarded to every child so the attribute appears in both outputs.
func TestFanout_WithAttrs_PropagatesToAllChildren(t *testing.T) {
	var a, b bytes.Buffer
	h := NewFanout(
		slog.NewJSONHandler(&a, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	logger := slog.New(h).With("mykey", "myval")
	logger.Info("msg")
	require.Contains(t, a.String(), "mykey")
	require.Contains(t, a.String(), "myval")
	require.Contains(t, b.String(), "mykey")
	require.Contains(t, b.String(), "myval")
}

// TestFanout_WithGroup_PropagatesToAllChildren verifies that WithGroup is
// forwarded to every child so the group prefix appears in both outputs.
func TestFanout_WithGroup_PropagatesToAllChildren(t *testing.T) {
	var a, b bytes.Buffer
	h := NewFanout(
		slog.NewJSONHandler(&a, &slog.HandlerOptions{Level: slog.LevelInfo}),
		slog.NewJSONHandler(&b, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)
	logger := slog.New(h.WithGroup("grp")).With("k", "v")
	logger.Info("msg")
	// JSON handler emits groups as "grp.k":"v"
	require.Contains(t, a.String(), "grp")
	require.Contains(t, b.String(), "grp")
}

// errHandler is a minimal slog.Handler that always returns an error from Handle.
type errHandler struct{ recorded []slog.Record }

func (e *errHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (e *errHandler) Handle(_ context.Context, r slog.Record) error {
	e.recorded = append(e.recorded, r.Clone())
	return errors.New("handle-error")
}
func (e *errHandler) WithAttrs(_ []slog.Attr) slog.Handler { return e }
func (e *errHandler) WithGroup(_ string) slog.Handler      { return e }

// TestFanout_Handle_AggregatesErrors verifies that when one child's Handle
// returns an error the fanout returns a non-nil error AND the other child still
// receives the record (failure of one sink does not suppress the other).
func TestFanout_Handle_AggregatesErrors(t *testing.T) {
	var good bytes.Buffer
	failing := &errHandler{}
	h := NewFanout(
		failing,
		slog.NewJSONHandler(&good, &slog.HandlerOptions{Level: slog.LevelInfo}),
	)

	var r slog.Record
	r = slog.NewRecord(r.Time, slog.LevelInfo, "test-msg", 0)
	err := h.Handle(context.Background(), r)
	require.Error(t, err, "fanout must surface the error from the failing child")
	require.Contains(t, good.String(), "test-msg", "succeeding child must still receive the record")
	require.Len(t, failing.recorded, 1, "failing child must have been called")
}

// TestLevelGate_WithAttrs_Propagates verifies that WithAttrs is delegated
// through the levelGate wrapper so attributes reach the underlying handler.
func TestLevelGate_WithAttrs_Propagates(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	gate := NewLevelGate(slog.LevelInfo, base)
	logger := slog.New(gate).With("gatekey", "gateval")
	logger.Info("gatemsg")
	require.Contains(t, buf.String(), "gatekey")
	require.Contains(t, buf.String(), "gateval")
}

// TestLevelGate_WithGroup_Propagates verifies that WithGroup is delegated
// through the levelGate wrapper so the group prefix reaches the handler.
func TestLevelGate_WithGroup_Propagates(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	gate := NewLevelGate(slog.LevelInfo, base)
	logger := slog.New(gate.WithGroup("grpgate")).With("k", "v")
	logger.Info("msg")
	require.Contains(t, buf.String(), "grpgate")
}
