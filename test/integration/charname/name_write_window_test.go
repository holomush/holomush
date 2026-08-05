//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package charname_test

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention

	"github.com/holomush/holomush/internal/testsupport/chartest"
)

// The 000055 -> 000056 write window.
//
// goose's advisory lock serializes MIGRATORS, not application writers, so
// nothing in the framework stops an old binary inserting a NULL normalized_name
// after the backfill commits but before SET NOT NULL, or a new binary inserting
// a duplicate before the index exists. 000056's header states the deployment
// precondition that closes it — quiesce character writers across the boundary —
// and rests on the claim that a violation fails LOUDLY rather than silently, so
// the exposure is a repeatedly-failing rollout rather than corruption.
//
// These specs are what turn that claim into a fact. Each writes into the window
// and then applies 000056, asserting the failure is loud AND that the schema is
// left at 000055 rather than half-constrained.

// applyUniqueIndexMigration applies 000056's two statements in order against an
// already-staged database, returning whatever Postgres says.
//
// It runs the statements directly rather than through goose so the SPEC can see
// which of the two failed. Order is preserved exactly: SET NOT NULL first, then
// CREATE UNIQUE INDEX. That order is the migration's load-bearing property —
// Postgres treats NULLs as distinct for uniqueness, so an index created over an
// unbackfilled nullable column succeeds while enforcing nothing.
//
// # Why one transaction
//
// Both statements run in a SINGLE transaction, which is what goose already does
// for 000056 in production: goose wraps each migration in a transaction by
// default and 000056 carries no NO TRANSACTION annotation. Executing them on
// separate connections would model a migration this repository does not have —
// and would leave the fixture half-migrated when the index fails, with
// normalized_name permanently NOT NULL for the rest of the spec. Rolling back
// is what lets each spec assert the schema is left at 000055 rather than
// half-constrained, which is the claim this whole file exists to prove.
//
// The migration header's refusal to "merge the backfill and the constraint into
// one transaction" is a different question — that is about 000055's unbounded
// backfill holding ACCESS EXCLUSIVE, not about 000056's own two statements.
func applyUniqueIndexMigration(ctx context.Context, pool *pgxpool.Pool) (notNullErr, indexErr error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err, nil
	}
	// Rollback is a no-op once the tx has committed.
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`ALTER TABLE characters ALTER COLUMN normalized_name SET NOT NULL`); err != nil {
		return err, nil
	}
	if _, err := tx.Exec(ctx,
		`CREATE UNIQUE INDEX characters_normalized_name_key ON characters (normalized_name)`); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}

// seedPlayerRow inserts a player and returns its id.
func seedPlayerRow(ctx context.Context, pool *pgxpool.Pool) ulid.ULID {
	GinkgoHelper()
	id := ulid.Make()
	_, err := pool.Exec(ctx,
		`INSERT INTO players (id, username, password_hash) VALUES ($1, $2, 'hash')`,
		id.String(), "window_"+id.String())
	Expect(err).NotTo(HaveOccurred())
	return id
}

var _ = Describe("The 000055 -> 000056 write window fails loudly, never silently", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		pool   *pgxpool.Pool
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
		DeferCleanup(cancel)

		connStr := stageSchemaWithoutUniqueIndex(ctx, suiteT)
		var err error
		pool, err = pgxpool.New(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(pool.Close)
	})

	It("applies cleanly when the window was respected — the paired positive control", func() {
		// Nothing wrote into the window: the backfill left every row populated
		// and unique, so 000056 applies. Without this, the two failure cases
		// below could both pass against a database where 000056 can never apply
		// for some unrelated reason.
		notNullErr, indexErr := applyUniqueIndexMigration(ctx, pool)
		Expect(notNullErr).NotTo(HaveOccurred())
		Expect(indexErr).NotTo(HaveOccurred())
		Expect(uniqueIndexExists(ctx, pool)).To(BeTrue())
	})

	It("fails on SET NOT NULL when an old writer inserted a NULL normalized_name after the backfill", func() {
		playerID := seedPlayerRow(ctx, pool)
		_, err := pool.Exec(ctx,
			`INSERT INTO characters (id, player_id, name) VALUES ($1, $2, 'Windowed')`,
			ulid.Make().String(), playerID.String())
		Expect(err).NotTo(HaveOccurred(),
			"an old binary CAN still write a NULL key in this window — that is the hazard")

		notNullErr, indexErr := applyUniqueIndexMigration(ctx, pool)
		Expect(notNullErr).To(HaveOccurred(),
			"the loud failure the deployment precondition documents")
		Expect(notNullErr.Error()).To(ContainSubstring("null value"))
		Expect(indexErr).NotTo(HaveOccurred(), "the index was never reached")

		// The schema is left at 000055, not half-constrained.
		Expect(uniqueIndexExists(ctx, pool)).To(BeFalse())
		Expect(normalizedNameIsNullable(ctx, pool)).To(BeTrue())
	})

	It("fails on CREATE UNIQUE INDEX when a new writer inserted a duplicate after the backfill", func() {
		const dupName = "Windowed"
		ident := chartest.IdentityFor(dupName)
		for range 2 {
			playerID := seedPlayerRow(ctx, pool)
			_, err := pool.Exec(ctx, `
				INSERT INTO characters (id, player_id, name, normalized_name, name_skeleton, name_skeleton_unicode_version)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				ulid.Make().String(), playerID.String(), dupName,
				ident.NormalizedName, ident.Skeleton, ident.UnicodeVersion)
			Expect(err).NotTo(HaveOccurred(),
				"a new binary CAN still write a duplicate key in this window — that is the hazard")
		}

		notNullErr, indexErr := applyUniqueIndexMigration(ctx, pool)
		Expect(notNullErr).NotTo(HaveOccurred(),
			"every row has a key, so the NOT NULL passes; the duplicate is caught one statement later")
		Expect(indexErr).To(HaveOccurred(),
			"the loud failure the deployment precondition documents")
		// Postgres words this one "could not create unique index ... (SQLSTATE
		// 23505)" rather than the "duplicate key" phrasing an ordinary INSERT
		// violation uses; the SQLSTATE is the stable half.
		Expect(indexErr.Error()).To(ContainSubstring("23505"))
		Expect(indexErr.Error()).To(ContainSubstring("characters_normalized_name_key"))

		// The schema is left at 000055, not half-constrained. The SET NOT NULL
		// SUCCEEDED here, so this holds only because the failing index rolls the
		// whole migration back — exactly as goose does in production.
		Expect(uniqueIndexExists(ctx, pool)).To(BeFalse())
		Expect(normalizedNameIsNullable(ctx, pool)).To(BeTrue())
	})
})

// normalizedNameIsNullable reports whether characters.normalized_name still
// permits NULL — i.e. whether 000056's SET NOT NULL took effect.
func normalizedNameIsNullable(ctx context.Context, pool *pgxpool.Pool) bool {
	GinkgoHelper()
	var isNullable string
	Expect(pool.QueryRow(ctx, `
		SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'characters' AND column_name = 'normalized_name'`).
		Scan(&isNullable)).To(Succeed())
	return isNullable == "YES"
}
