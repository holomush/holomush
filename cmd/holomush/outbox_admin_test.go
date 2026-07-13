// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewOutboxCmdStructure verifies the `holomush outbox skip` subcommand exists
// with the --game and --position flags.
func TestNewOutboxCmdStructure(t *testing.T) {
	cmd := NewOutboxCmd()
	assert.Equal(t, "outbox", cmd.Name())

	skip, _, err := cmd.Find([]string{"skip"})
	require.NoError(t, err)
	assert.Equal(t, "skip", skip.Name())
	require.NotNil(t, skip.Flags().Lookup("game"), "skip has a --game flag")
	require.NotNil(t, skip.Flags().Lookup("position"), "skip has a --position flag")
}

// TestRootRegistersOutbox verifies the outbox command is wired under the root.
func TestRootRegistersOutbox(t *testing.T) {
	root := NewRootCmd()
	sub, _, err := root.Find([]string{"outbox"})
	require.NoError(t, err)
	assert.Equal(t, "outbox", sub.Name())
}

// TestOutboxSkipRejectsNonPositivePosition proves the CLI validates --position
// before touching the database or NATS (a poison row is a positive feed_position).
func TestOutboxSkipRejectsNonPositivePosition(t *testing.T) {
	cmd := newOutboxSkipCmd()
	cmd.SetArgs([]string{"--position", "0"})
	err := cmd.Execute()
	require.Error(t, err, "a non-positive --position is a usage error")
}
