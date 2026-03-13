// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSeedsCommand_Help(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"validate-seeds", "--help"})

	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "Validate")
	assert.Contains(t, output, "seed policy")
}

func TestValidateSeedsCommand_SucceedsWithValidSeeds(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	errBuf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(errBuf)
	cmd.SetArgs([]string{"validate-seeds"})

	err := cmd.Execute()
	require.NoError(t, err, "validate-seeds should succeed with valid seed policies")
}

func TestValidateSeedsCommand_DoesNotStartServer(t *testing.T) {
	// validate-seeds should exit immediately without needing DATABASE_URL or network
	t.Setenv("DATABASE_URL", "")

	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"validate-seeds"})

	err := cmd.Execute()
	require.NoError(t, err, "validate-seeds should work without DATABASE_URL")
}

func TestValidateSeedsCommand_InRootHelp(t *testing.T) {
	cmd := NewRootCmd()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs([]string{"--help"})

	require.NoError(t, cmd.Execute())

	output := buf.String()
	assert.Contains(t, output, "validate-seeds", "Root help should list validate-seeds command")
}

func TestRunValidateSeeds_AllSeedsValid(t *testing.T) {
	err := runValidateSeeds()
	require.NoError(t, err, "all built-in seed policies should compile successfully")
}
