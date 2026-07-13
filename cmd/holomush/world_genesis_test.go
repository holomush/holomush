// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build !integration

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewWorldCmdStructure verifies the `holomush world genesis` and
// `holomush world epoch-reset` operator subcommands exist with a --game flag
// (round-4 A3 — the real operator entry point at cutover / DB-restore).
func TestNewWorldCmdStructure(t *testing.T) {
	cmd := NewWorldCmd()
	assert.Equal(t, "world", cmd.Name())

	genesis, _, err := cmd.Find([]string{"genesis"})
	require.NoError(t, err)
	assert.Equal(t, "genesis", genesis.Name())
	require.NotNil(t, genesis.Flags().Lookup("game"), "genesis has a --game flag")

	reset, _, err := cmd.Find([]string{"epoch-reset"})
	require.NoError(t, err)
	assert.Equal(t, "epoch-reset", reset.Name())
	require.NotNil(t, reset.Flags().Lookup("game"), "epoch-reset has a --game flag")
}

// TestRootRegistersWorld verifies the world command is wired under the root.
func TestRootRegistersWorld(t *testing.T) {
	root := NewRootCmd()
	sub, _, err := root.Find([]string{"world"})
	require.NoError(t, err)
	assert.Equal(t, "world", sub.Name())
}
