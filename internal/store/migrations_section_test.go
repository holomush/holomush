// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Deliberately NO //go:build integration tag: this covers the pure parsing
// behavior of the MigrationSectionForTest seam, so it must run under plain
// `task test` (which does not compile integration-tagged files at all).

func TestMigrationSectionForTest(t *testing.T) {
	const (
		indexMigration     = "000053_sessions_location_index.sql"
		partitionMigration = "000052_events_audit_partition.sql"
	)

	t.Run("returns only the up body when asked for the up direction", func(t *testing.T) {
		got, err := MigrationSectionForTest(indexMigration, "up")
		require.NoError(t, err)

		assert.Contains(t, got, "CREATE INDEX IF NOT EXISTS idx_sessions_location_id",
			"the up section must carry the migration's own CREATE")
		assert.NotContains(t, got, "DROP INDEX",
			"the up section MUST NOT carry the down body — a caller that Execs this would "+
				"drop the index it just created")
		assert.NotContains(t, got, "+goose Down",
			"the +goose Down annotation is the section boundary and must not be returned")
	})

	t.Run("returns only the down body when asked for the down direction", func(t *testing.T) {
		got, err := MigrationSectionForTest(indexMigration, "down")
		require.NoError(t, err)

		assert.Contains(t, got, "DROP INDEX IF EXISTS idx_sessions_location_id",
			"the down section must carry the migration's own DROP")
		assert.NotContains(t, got, "CREATE INDEX",
			"the down section MUST NOT carry the up body")
	})

	t.Run("strips goose statement markers so the section is directly executable", func(t *testing.T) {
		got, err := MigrationSectionForTest(partitionMigration, "up")
		require.NoError(t, err)

		assert.Equal(t, 0, strings.Count(got, "+goose StatementBegin"),
			"StatementBegin markers are goose parser directives; PostgreSQL must never see them")
		assert.Equal(t, 0, strings.Count(got, "+goose StatementEnd"),
			"StatementEnd markers are goose parser directives; PostgreSQL must never see them")
		assert.Contains(t, got, "DO $$",
			"stripping the markers MUST NOT damage the dollar-quoted bodies they wrapped")
		assert.Contains(t, got, "PARTITION BY RANGE (event_ms)",
			"precondition: the section read back must be migration 000052's up body")
	})

	t.Run("rejects an unknown direction", func(t *testing.T) {
		_, err := MigrationSectionForTest(indexMigration, "sideways")
		require.Error(t, err, "an unrecognized direction must not silently return a section")
	})

	t.Run("rejects a migration source with no goose Down annotation", func(t *testing.T) {
		_, err := migrationSection("-- +goose Up\nCREATE TABLE t ();\n", "up")
		require.Error(t, err,
			"without a Down annotation the up section has no end boundary; returning the whole "+
				"file here is exactly the bug this seam exists to prevent")
	})

	t.Run("rejects a migration source with no goose Up annotation", func(t *testing.T) {
		_, err := migrationSection("CREATE TABLE t ();\n-- +goose Down\nDROP TABLE t;\n", "up")
		require.Error(t, err)
	})

	t.Run("returns the filesystem error for an unknown migration file", func(t *testing.T) {
		_, err := MigrationSectionForTest("999999_does_not_exist.sql", "up")
		require.Error(t, err)
	})
}
