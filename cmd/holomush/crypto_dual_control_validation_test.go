// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDualControlLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	h := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), &buf
}

// TestValidateDualControlRequired_FiltersUnknownOps verifies unknown
// op_kinds are dropped with a slog.Warn (spec §9 lax+warn).
func TestValidateDualControlRequired_FiltersUnknownOps(t *testing.T) {
	logger, buf := newTestDualControlLogger(t)
	got := validateDualControlRequired([]string{"rekey", "make_coffee", "admin_read_stream", "unknown_op"}, logger)

	assert.Equal(t, []string{"rekey", "admin_read_stream"}, got)
	out := buf.String()
	require.Contains(t, out, "make_coffee")
	require.Contains(t, out, "unknown_op")
	require.Equal(t, 2, strings.Count(out, "crypto.dual_control_required references unknown op_kind"))
}

// TestValidateDualControlRequired_PreservesKnownOps verifies a clean list
// passes through untouched.
func TestValidateDualControlRequired_PreservesKnownOps(t *testing.T) {
	logger, buf := newTestDualControlLogger(t)
	got := validateDualControlRequired([]string{"rekey", "admin_read_stream"}, logger)
	assert.Equal(t, []string{"rekey", "admin_read_stream"}, got)
	assert.Empty(t, buf.String(), "no warns expected")
}

// TestValidateDualControlRequired_AcceptsEmpty verifies an empty list
// (lax mode, no dual-control required) is preserved.
func TestValidateDualControlRequired_AcceptsEmpty(t *testing.T) {
	logger, buf := newTestDualControlLogger(t)
	got := validateDualControlRequired(nil, logger)
	assert.Empty(t, got)
	assert.Empty(t, buf.String())
}
