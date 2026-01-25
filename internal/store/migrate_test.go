// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"testing"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/stretchr/testify/require"
)

func TestNewMigrator_InvalidURL(t *testing.T) {
	_, err := NewMigrator("invalid://url")
	require.Error(t, err)
	// Verify the error code is set correctly
	errutil.AssertErrorCode(t, err, "MIGRATION_INIT_FAILED")
}
