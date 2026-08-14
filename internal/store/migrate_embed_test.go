// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// expectedMigrationCount is the number of goose single-file migrations in the
// embedded corpus. It is written out literally rather than derived from the
// directory listing: deriving it from the same source the test walks would make
// the assertion tautological and blind to a migration file that went missing.
// Bump it in the same change that adds a migration.
//
// This counts EMBEDDED `.sql` files only. //go:embed migrations/*.sql globs
// `.sql`, so registered Go migrations (000055) are absent from this number
// while being present in migrate_integration_test.go's
// expectedAppliedMigrationRows, which counts what goose actually applied.
const expectedMigrationCount = 47

// TestMigrationsFSContainsSingleFileGooseMigrationsWithBothAnnotations pins the
// post-goose corpus shape. goose parses one file per version carrying both
// directions, so the golang-migrate-era .up.sql/.down.sql pairing has no referent:
// handing goose the old corpus makes it report a duplicate version for every
// migration, because 000001_baseline.up.sql and 000001_baseline.down.sql both
// parse as version 1.
func TestMigrationsFSContainsSingleFileGooseMigrationsWithBothAnnotations(t *testing.T) {
	entries, err := migrationsFS.ReadDir("migrations")
	require.NoError(t, err, "should read embedded migrations directory")

	require.Len(t, entries, expectedMigrationCount,
		"embedded corpus should hold exactly %d single-file migrations", expectedMigrationCount)

	namePattern := regexp.MustCompile(`^\d{6}_\w+\.sql$`)
	splitPattern := regexp.MustCompile(`\.(up|down)\.sql$`)

	fileNames := make(map[string]bool, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		fileNames[name] = true

		assert.True(t, namePattern.MatchString(name),
			"file %s should match pattern NNNNNN_name.sql", name)
		assert.False(t, splitPattern.MatchString(name),
			"file %s is a golang-migrate-era split file; goose takes one file per version", name)

		// Every migration must carry BOTH directions. A file missing its Down
		// annotation still applies cleanly on the way up and only fails when a
		// rollback is attempted, which is the worst time to discover it.
		content, readErr := migrationsFS.ReadFile("migrations/" + name)
		require.NoError(t, readErr, "should read embedded migration %s", name)

		assert.Contains(t, string(content), "-- +goose Up",
			"migration %s must carry a -- +goose Up annotation", name)
		assert.Contains(t, string(content), "-- +goose Down",
			"migration %s must carry a -- +goose Down annotation", name)
	}

	assert.True(t, fileNames["000001_baseline.sql"], "should contain the baseline migration")
}
