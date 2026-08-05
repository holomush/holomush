// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

// goMigrationNames is the census of every GO migration in
// internal/store/migrations, keyed by version.
//
// # Why this exists at all
//
// `//go:embed migrations/*.sql` globs `.sql` only, so a Go migration is
// invisible to `migrationsFS` — and every version helper in this package
// (loadMigrationVersions, loadMigrationNames, and through them
// PendingMigrations, AppliedMigrations and the adopt gate's seed derivation)
// reads the embedded FS. Before the first Go migration landed those helpers
// happened to describe the whole chain; they no longer do, and the gap is not
// cosmetic: the adopt gate seeds one goose ledger row per version it knows
// about, so a version missing from this census is seeded as NOT applied while
// every higher version IS, and goose then refuses the next Up with
// "detected 1 missing (out-of-order) migration lower than database version".
//
// # Why it is written out literally
//
// The same reason migrate_embed_test.go's count and migrate_test.go's pending
// census are literal: deriving it from the directory the helpers read would
// make the assertion tautological and blind to a migration that went missing.
// Extend it in the same change that adds a Go migration.
//
// TestGoMigrationCensusMatchesTheMigrationsDirectory holds it to the on-disk
// corpus in both directions, so drift fails the untagged unit lane rather than
// surfacing as a wedged rollout.
//
// The name value matches the SQL corpus's convention — the filename with its
// extension stripped — because MigrationName returns both kinds to the same
// caller.
var goMigrationNames = map[uint]string{
	55: "000055_backfill_character_normalized_names",
}
