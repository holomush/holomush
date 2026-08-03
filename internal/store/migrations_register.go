// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
	// Blank-imported so every Go migration's init() runs. This single line is
	// what puts Go migrations into the chain: goose learns about a Go migration
	// only when goose.AddMigrationContext executes, and that call lives in an
	// init(), which runs only if something imports the package. The migrator
	// itself does not — it reaches the SQL corpus through the embedded FS, and
	// //go:embed migrations/*.sql globs .sql only, so the embed can never
	// substitute for this import.
	//
	// Delete this line and every Go migration silently vanishes from the chain.
	// The failure is invisible by construction: goose runs the SQL migrations,
	// every version count is correct, the schema is consistent with the SQL-only
	// corpus, and the suite passes. Nothing errors — the Go step simply never
	// happened. migrations_register_test.go asserts this import is still here.
	//
	// It lives in its own file rather than in migrate.go's import block so this
	// reason is not buried among two dozen other imports, where a tidy pass or an
	// unused-import sweep would read it as dead weight.
	_ "github.com/holomush/holomush/internal/store/migrations"
)
