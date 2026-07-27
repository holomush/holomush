// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

// MigrationSQLForTest returns the embedded source of the named migration file
// (e.g. "000053_sessions_location_index.up.sql").
//
// It exists so that a migration spec can execute the migration's OWN SQL
// instead of re-typing it as a Go string literal. A re-typed copy makes the
// spec unfalsifiable: it asserts the idempotency of the string the test wrote,
// so deleting IF NOT EXISTS from the migration file leaves the spec green.
//
// migrationsFS is unexported and the specs live in package store_test; both
// packages are linked into the same test binary, so an export_test.go accessor
// is the narrowest seam that does not widen the production surface.
func MigrationSQLForTest(name string) (string, error) {
	data, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		return "", err //nolint:wrapcheck // test-only accessor; the caller asserts on the raw fs error
	}
	return string(data), nil
}
