// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package migrations holds the host database migration chain.
//
// Both `.sql` and `.go` migrations live in this one directory, mixed and
// version-ordered (D-08), so a single `ls` shows the whole ordered chain. The
// `.sql` files are compiled in by `//go:embed migrations/*.sql` in the parent
// package; that glob matches `.sql` only, so `.go` files here are ordinary Go
// source and are NOT embedded.
//
// # The filename IS the version declaration
//
// goose derives a Go migration's version from `runtime.Caller(1)`'s filename at
// the moment `goose.AddMigrationContext` runs. There is no other declaration and
// no override: a file named `000055_backfill_names.go` registers version 55
// because of its name. Renaming the file renumbers the migration.
//
// This is also why THIS file must never carry a version prefix. It registers
// nothing, and a versioned name on a non-registering file is a trap: it reads
// like a migration in the `ls` the mixed directory exists to make legible.
//
// # Registration is mandatory and is not self-enforcing
//
// A `.go` migration is part of the chain only if its `init()` calls
// `goose.AddMigrationContext`. goose ships its own "unregistered Go migration"
// guard, but that guard cannot fire here: it depends on the file being visible
// to goose's source collector, and the parent package embeds `*.sql` only. A
// file that forgets its `init()` is therefore SILENTLY absent from the chain —
// every migration count is right, the schema matches the SQL-only corpus, and
// the suite is green.
//
// `migrations_register_test.go` in the parent package closes that hole, and
// `migrations_register.go` carries the blank import without which no `init()`
// here runs at all.
//
// # Transactions
//
// `goose.AddMigrationContext` (transactional, `func(ctx, *sql.Tx) error`) is the
// default. `goose.AddMigrationNoTxContext` requires a
//
//	// goose-no-tx: <reason>
//
// comment naming the specific statement that forbids a transaction (for example
// `CREATE INDEX CONCURRENTLY`). That is a correctness requirement rather than a
// style rule: a backfill that must roll back and leave the schema at the prior
// version on collision (D-22) only does so inside a transaction. The same guard
// enforces it.
package migrations
