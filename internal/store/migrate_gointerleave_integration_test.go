//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"time"

	// Register pgx stdlib driver for database/sql. goose.NewProvider takes a
	// *sql.DB, not a pgxpool, so this file opens a database/sql handle rather
	// than following the pgxpool shape its structural sibling
	// (migrate_clamp_integration_test.go) uses.
	_ "github.com/jackc/pgx/v5/stdlib"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/pressly/goose/v3"
	"github.com/samber/oops"

	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// goInterleaveGoVersion is the version the fixture's Go migration claims.
//
// It is a named constant because it is the falsifiability knob for this whole
// spec: setting it to 4 moves the Go step BEHIND the SET NOT NULL step, and both
// the source-order assertion and migration 3 must then change behaviour. A spec
// that only asserted the backfill's end state would survive that edit — which is
// exactly the failure mode ("the Go migration ran" is not "the Go migration ran
// in version order") this file exists to rule out. See the SUMMARY for the
// observed output of that experiment.
const goInterleaveGoVersion = 2

// goInterleaveIrreversibleCode is the oops code the fixture's down returns.
//
// D-09: a Go migration whose effect cannot be reverted returns an error naming
// why rather than silently succeeding. A no-op down lets an operator run
// `migrate down`, see success, and believe state reverted when it did not.
const goInterleaveIrreversibleCode = "MIGRATION_FIXTURE_IRREVERSIBLE"

// goInterleaveSeededRows is the number of rows migration 1 inserts, and
// therefore the number the Go step at version 2 must observe. Asserting the
// count the Go step READ — not merely the count it wrote — is what proves data
// dependence on the prior SQL step rather than mere ordering.
const goInterleaveSeededRows = 2

// goInterleaveCreateSQL is the fixture's migration 1: the D-21 (A) step. It
// creates the target column NULLABLE and seeds rows that leave it NULL, so the
// backfill at version 2 has real work to do and migration 3 has a precondition
// that can actually fail.
const goInterleaveCreateSQL = `-- +goose Up
CREATE TABLE gointerleave_names (
    id              BIGINT PRIMARY KEY,
    name            TEXT NOT NULL,
    normalized_name TEXT
);
INSERT INTO gointerleave_names (id, name) VALUES (1, 'Alpha'), (2, 'Beta');

-- +goose Down
DROP TABLE gointerleave_names;
`

// goInterleaveConstrainSQL is the fixture's migration 3: the D-21 (C) step.
//
// SET NOT NULL before CREATE UNIQUE INDEX is load-bearing, not stylistic.
// Postgres treats NULLs as distinct for uniqueness, so a unique index over an
// unbackfilled nullable column succeeds and enforces nothing — a green deploy
// with the guarantee silently absent. Putting SET NOT NULL first makes the Go
// backfill's execution a hard precondition of this migration: if version 2 had
// not run, every normalized_name would still be NULL and this ALTER would fail
// with "column contains null values" before the index was ever considered.
const goInterleaveConstrainSQL = `-- +goose Up
ALTER TABLE gointerleave_names ALTER COLUMN normalized_name SET NOT NULL;
CREATE UNIQUE INDEX gointerleave_names_normalized_key
    ON gointerleave_names (normalized_name);

-- +goose Down
DROP INDEX gointerleave_names_normalized_key;
ALTER TABLE gointerleave_names ALTER COLUMN normalized_name DROP NOT NULL;
`

// sourceVersions flattens a provider's source list to (version, type) pairs so
// the spec can assert the ORDER goose resolved, not just the set it collected.
func sourceVersions(sources []*goose.Source) []string {
	GinkgoHelper()
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		out = append(out, string(s.Type)+":"+strconv.FormatInt(s.Version, 10))
	}
	return out
}

// resultVersions flattens applied migration results the same way sourceVersions
// flattens sources, so the two orders are directly comparable.
func resultVersions(results []*goose.MigrationResult) []string {
	GinkgoHelper()
	out := make([]string, 0, len(results))
	for _, r := range results {
		out = append(out, string(r.Source.Type)+":"+strconv.FormatInt(r.Source.Version, 10))
	}
	return out
}

// goInterleaveGoSource is the (type:version) token the Go step must occupy in
// both the source list and the applied list.
func goInterleaveGoSource() string {
	return "go:" + strconv.FormatInt(goInterleaveGoVersion, 10)
}

var _ = Describe("Go/SQL interleave: a Go migration between two SQL migrations (criterion 4)", func() {
	// D-07: the proof is a fixture chain, never a throwaway migration in the
	// real corpus. Migrations are permanent and replay on every fresh database
	// forever, so buying a junk chain entry to satisfy a criterion is not an
	// option. WithGoMigrations + WithDisableGlobalRegistry(true) is what keeps
	// this fixture's Go step and the real chain mutually invisible.
	It("applies SQL -> Go -> SQL in version order, with the Go step observing the prior SQL step's rows", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		// The fixture SQL lives in a temp dir, NEVER under
		// internal/store/migrations/. That directory is swept by the format
		// meta-test, lint:no-timestamptz, and test/meta/world_sql_fence_test.go;
		// a fixture landing there would be picked up by all three and could
		// reach a real database.
		fixtureDir := suiteT.TempDir()
		Expect(os.WriteFile(
			filepath.Join(fixtureDir, "000001_create_interleave_fixture.sql"),
			[]byte(goInterleaveCreateSQL), 0o600,
		)).To(Succeed())
		Expect(os.WriteFile(
			filepath.Join(fixtureDir, "000003_constrain_interleave_fixture.sql"),
			[]byte(goInterleaveConstrainSQL), 0o600,
		)).To(Succeed())

		db, err := sql.Open("pgx", testutil.RawDatabase(suiteT, sharedPG))
		Expect(err).NotTo(HaveOccurred())
		defer func() { Expect(db.Close()).To(Succeed()) }()
		Expect(db.PingContext(ctx)).To(Succeed())

		// observedRows records what the Go step READ, captured out of the
		// migration closure. -1 is a sentinel that survives "the Go step never
		// ran": a zero default would be indistinguishable from "ran and saw
		// nothing".
		observedRows := -1

		backfill := goose.NewGoMigration(
			goInterleaveGoVersion,
			&goose.GoFunc{
				// D-10/D-22: transactional. The whole point of the Go step being
				// a migration is that a failure rolls back and leaves the schema
				// at the prior version.
				Mode: goose.TransactionEnabled,
				RunTx: func(ctx context.Context, tx *sql.Tx) error {
					rows, queryErr := tx.QueryContext(ctx, `SELECT id, name FROM gointerleave_names`)
					if queryErr != nil {
						return queryErr
					}
					defer func() { _ = rows.Close() }() //nolint:errcheck // rows.Err() below is the authoritative check

					type record struct {
						id   int64
						name string
					}
					var seen []record
					for rows.Next() {
						var r record
						if scanErr := rows.Scan(&r.id, &r.name); scanErr != nil {
							return scanErr
						}
						seen = append(seen, r)
					}
					if rowsErr := rows.Err(); rowsErr != nil {
						return rowsErr
					}
					observedRows = len(seen)

					for _, r := range seen {
						if _, execErr := tx.ExecContext(
							ctx,
							`UPDATE gointerleave_names SET normalized_name = lower(name) WHERE id = $1`,
							r.id,
						); execErr != nil {
							return execErr
						}
					}
					return nil
				},
			},
			&goose.GoFunc{
				Mode: goose.TransactionEnabled,
				RunTx: func(_ context.Context, _ *sql.Tx) error {
					return oops.Code(goInterleaveIrreversibleCode).
						Errorf("migration %d normalized names in place; the pre-normalization values are not recoverable",
							goInterleaveGoVersion)
				},
			},
		)

		provider, err := goose.NewProvider(
			goose.DialectPostgres,
			db,
			os.DirFS(fixtureDir),
			goose.WithGoMigrations(backfill),
			// Fixture-only. The production provider in internal/store/migrate.go
			// MUST NOT set this: disabling the global registry there would make a
			// Phase-2 Go migration invisible to the real chain.
			goose.WithDisableGlobalRegistry(true),
		)
		Expect(err).NotTo(HaveOccurred())
		defer func() { Expect(provider.Close()).To(Succeed()) }()

		By("resolving the sources in version order BEFORE anything is applied")
		// Asserting source order separately from apply order is what
		// distinguishes "goose ordered them" from "they happened to work". An
		// apply-order-only assertion passes even when goose collected the Go
		// step last and merely executed it in a lucky sequence.
		Expect(sourceVersions(provider.ListSources())).To(Equal([]string{
			"sql:1",
			goInterleaveGoSource(),
			"sql:3",
		}), "goose must interleave the fixture-supplied Go migration between the two SQL sources by version")

		By("applying all three in ascending version order")
		results, err := provider.Up(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(resultVersions(results)).To(Equal([]string{
			"sql:1",
			goInterleaveGoSource(),
			"sql:3",
		}), "the APPLIED sequence, not just the end state — a chain that ran 1,3,2 would reach the same schema")

		By("proving data dependence: the Go step read the rows migration 1 inserted")
		Expect(observedRows).To(Equal(goInterleaveSeededRows),
			"the Go step must observe migration 1's inserts from inside its own transaction; "+
				"this is the property Phase 2's backfill depends on")

		By("confirming migration 3's SET NOT NULL landed on the backfilled column")
		// This assertion is a corroboration, not the proof: migration 3 could
		// not have succeeded at all if the backfill had not run, because
		// SET NOT NULL fails outright on a column still holding NULLs.
		var isNullable string
		Expect(db.QueryRowContext(ctx, `
			SELECT is_nullable FROM information_schema.columns
			WHERE table_name = 'gointerleave_names' AND column_name = 'normalized_name'
		`).Scan(&isNullable)).To(Succeed())
		Expect(isNullable).To(Equal("NO"), "D-21 (C): SET NOT NULL must precede the unique index")

		var normalized int
		Expect(db.QueryRowContext(
			ctx,
			`SELECT count(*) FROM gointerleave_names WHERE normalized_name = lower(name)`,
		).Scan(&normalized)).To(Succeed())
		Expect(normalized).To(Equal(goInterleaveSeededRows))

		By("rolling migration 3 back cleanly")
		_, err = provider.DownTo(ctx, goInterleaveGoVersion)
		Expect(err).NotTo(HaveOccurred())
		version, err := provider.GetDBVersion(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(version).To(Equal(int64(goInterleaveGoVersion)))

		By("refusing to roll the irreversible Go migration back (C4b, D-09)")
		_, downErr := provider.Down(ctx)
		Expect(downErr).To(HaveOccurred(),
			"an irreversible down MUST fail; a silent success tells the operator state reverted when it did not")
		// Error IDENTITY, not a message substring. goose's *PartialError
		// implements Unwrap, so oops.AsOops walks past it to the fixture's code.
		errutil.AssertErrorCode(suiteT, downErr, goInterleaveIrreversibleCode)

		By("leaving the refused version still recorded as applied")
		// A refusal that also rewound the ledger would be the worst of both
		// worlds: the caller sees an error AND the bookkeeping claims the
		// migration is gone.
		version, err = provider.GetDBVersion(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(version).To(Equal(int64(goInterleaveGoVersion)),
			"the refused migration must remain recorded as applied")
	})
})
