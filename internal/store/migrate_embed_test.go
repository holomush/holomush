// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationsFS_EmbeddedFiles(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	require.NoError(t, err, "should read embedded migrations directory")

	// We have 7 migrations, each with up and down = 14 files
	assert.GreaterOrEqual(t, len(entries), 14, "should have at least 14 migration files (7 up + 7 down)")

	// Verify naming pattern - check first migration exists
	expectedFiles := []string{
		"000001_initial.up.sql",
		"000001_initial.down.sql",
	}

	fileNames := make(map[string]bool)
	for _, entry := range entries {
		fileNames[entry.Name()] = true
	}

	for _, expected := range expectedFiles {
		assert.True(t, fileNames[expected], "should contain %s", expected)
	}

	// Verify all files follow expected naming pattern
	pattern := regexp.MustCompile(`^\d{6}_\w+\.(up|down)\.sql$`)
	for _, entry := range entries {
		assert.True(t, pattern.MatchString(entry.Name()),
			"file %s should match pattern NNNNNN_name.(up|down).sql", entry.Name())
	}
}
