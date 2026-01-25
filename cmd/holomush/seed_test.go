// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
	"bytes"
	"context"
	"testing"

	"github.com/oklog/ulid/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/pkg/errutil"
)

func TestSeedULIDIsValid(t *testing.T) {
	// The well-known seed ULID used for idempotency
	// Must be exactly 26 characters using Crockford's base32 alphabet
	seedULID := "01HZN3XS000000000000000000"

	require.Len(t, seedULID, 26, "seed ULID must be exactly 26 characters")

	id, err := ulid.Parse(seedULID)
	require.NoError(t, err, "seed ULID should be valid")
	require.NotEqual(t, ulid.ULID{}, id, "parsed ULID should not be zero")
}

func TestRunSeed_MissingDatabaseURL(t *testing.T) {
	// Clear DATABASE_URL to test missing config
	t.Setenv("DATABASE_URL", "")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})

	err := runSeed(cmd, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "CONFIG_INVALID")
	assert.Contains(t, err.Error(), "DATABASE_URL")
}

func TestRunSeed_InvalidDatabaseURL(t *testing.T) {
	// Use a malformed connection string that will fail during parsing/connection
	// Using an invalid scheme forces an early failure
	t.Setenv("DATABASE_URL", "invalid://not-a-valid-url")

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetOut(&bytes.Buffer{})

	err := runSeed(cmd, nil)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "DB_CONNECT_FAILED")
}

func TestNewSeedCmd(t *testing.T) {
	cmd := NewSeedCmd()
	require.NotNil(t, cmd)
	assert.Equal(t, "seed", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.RunE)
}
