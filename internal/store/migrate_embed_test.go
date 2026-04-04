// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationsFSContainsMatchedUpDownPairsWithCorrectNaming(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	require.NoError(t, err, "should read embedded migrations directory")

	assert.GreaterOrEqual(t, len(entries), 2, "should have at least 2 migration files (1 up + 1 down)")

	fileNames := make(map[string]bool)
	for _, entry := range entries {
		fileNames[entry.Name()] = true
	}
	assert.True(t, fileNames["000001_baseline.up.sql"], "should contain baseline up migration")
	assert.True(t, fileNames["000001_baseline.down.sql"], "should contain baseline down migration")

	pattern := regexp.MustCompile(`^\d{6}_\w+\.(up|down)\.sql$`)
	for _, entry := range entries {
		assert.True(t, pattern.MatchString(entry.Name()),
			"file %s should match pattern NNNNNN_name.(up|down).sql", entry.Name())
	}

	upPattern := regexp.MustCompile(`\.up\.sql$`)
	downPattern := regexp.MustCompile(`\.down\.sql$`)
	ups := make(map[string]bool)
	downs := make(map[string]bool)
	for name := range fileNames {
		base := name[:6]
		if upPattern.MatchString(name) {
			ups[base] = true
		}
		if downPattern.MatchString(name) {
			downs[base] = true
		}
	}
	for v := range ups {
		assert.True(t, downs[v], "migration %s has .up.sql but no .down.sql", v)
	}
	for v := range downs {
		assert.True(t, ups[v], "migration %s has .down.sql but no .up.sql", v)
	}
}
