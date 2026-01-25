// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewMigrator_InvalidURL(t *testing.T) {
	_, err := NewMigrator("invalid://url")
	require.Error(t, err)
}

func TestNewMigrator_ValidURL(t *testing.T) {
	// This test requires a real database - skip in unit tests
	t.Skip("requires database connection")
}
