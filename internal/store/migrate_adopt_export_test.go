// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import "context"

// AdoptForTest runs ONLY the adopt gate against databaseURL, without the
// provider run that Migrator.Up performs immediately afterward.
//
// The separation matters for two of the C2 specs. Up() is adopt followed by
// provider.Up, so a spec that observes the ledger after Up() cannot tell which of
// the two wrote a given row. Against a database recorded BELOW the highest embedded
// version that distinction is the whole point: adopt should seed exactly the
// versions at or below the recorded one, and the provider should then apply the
// rest. Only an adopt-in-isolation call can assert the seed's boundary before the
// provider moves it.
//
// adoptFromGolangMigrate is unexported and the specs live in package store_test;
// both are linked into the same test binary, so an export_test.go accessor is the
// narrowest seam that does not widen the production surface. This mirrors the
// existing MigrationSQLForTest / MigrationSectionForTest convention in
// migrations_export_test.go, including its bare error returns — this is a test
// seam, not a trust boundary.
func AdoptForTest(ctx context.Context, databaseURL string) error {
	m, err := NewMigrator(databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = m.Close() }() // best-effort cleanup in a test seam
	return m.adoptFromGolangMigrate(ctx)
}
