//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

// legacyBookkeepingDDL reproduces golang-migrate's ledger shape: a single mutable
// row carrying the current version and a dirty flag. It is deliberately NOT read
// from the codebase — golang-migrate is gone, so there is nothing left to read, and
// this is the external shape adopt must consume.
const legacyBookkeepingDDL = `CREATE TABLE schema_migrations (
	version bigint NOT NULL PRIMARY KEY,
	dirty   boolean NOT NULL
)`

// newPreConversionDatabase builds the state a real production database is in on the
// morning of the cutover: the FULL schema physically applied, but recorded by
// golang-migrate rather than goose.
//
// It gets there by migrating for real and then rewinding the bookkeeping, rather
// than by hand-creating schema_migrations on a blank database. The difference is
// load-bearing for the rollback guard: a blank database with a ledger claiming
// version 53 has no tables to roll back, so Down() would trivially "succeed"
// regardless of the order it walked, and C2d would prove nothing.
func newPreConversionDatabase(t testing.TB, ctx context.Context, recorded int64, dirty bool) string {
	t.Helper()

	connStr := testutil.RawDatabase(suiteT, sharedPG)

	migrator, err := store.NewMigrator(connStr)
	require.NoError(t, err)
	require.NoError(t, migrator.Up(), "fixture setup: the real schema must be applied first")
	require.NoError(t, migrator.Close())

	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

	// Rewind the bookkeeping to the pre-conversion shape, leaving the schema itself
	// exactly where the real migrations put it.
	_, err = conn.Exec(ctx, `DROP TABLE goose_db_version`)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, legacyBookkeepingDDL)
	require.NoError(t, err)
	_, err = conn.Exec(ctx,
		`INSERT INTO schema_migrations (version, dirty) VALUES ($1, $2)`, recorded, dirty)
	require.NoError(t, err)

	return connStr
}

// insertLegacyRow adds a second row to the pre-conversion ledger, producing the
// ambiguous shape golang-migrate never writes but a partial restore, a merged
// dump, or a manual repair can leave behind.
func insertLegacyRow(t testing.TB, ctx context.Context, connStr string, version int64, dirty bool) {
	t.Helper()

	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

	_, err = conn.Exec(ctx,
		`INSERT INTO schema_migrations (version, dirty) VALUES ($1, $2)`, version, dirty)
	require.NoError(t, err)
}

// legacyRowCount reports how many rows the pre-conversion ledger holds, so the
// ambiguity spec can assert its own precondition rather than assuming it.
func legacyRowCount(t testing.TB, ctx context.Context, connStr string) int64 {
	t.Helper()

	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

	var count int64
	require.NoError(t, conn.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count))
	return count
}

// gooseLedgerInInsertionOrder returns version_id ordered by the ledger's identity
// column — i.e. the order the rows were INSERTED, which is not necessarily the
// order their versions sort in.
//
// This is the exact projection goose itself consults: its ListMigrations query is
// `ORDER BY id DESC`, so reading id ASC here and reversing gives goose's own view.
// Reading `ORDER BY version_id` instead would sort the evidence into the shape the
// assertion is trying to prove and could never fail.
func gooseLedgerInInsertionOrder(t testing.TB, ctx context.Context, connStr string) []int64 {
	t.Helper()

	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

	// The ledger legitimately does not exist before the first adopt, and querying a
	// missing relation is an error rather than an empty result. Report that state
	// as "no rows" so a precondition can assert emptiness without the query itself
	// blowing up.
	var ledgerExists bool
	require.NoError(t, conn.QueryRow(ctx,
		`SELECT to_regclass('public.goose_db_version') IS NOT NULL`).Scan(&ledgerExists))
	if !ledgerExists {
		return nil
	}

	rows, err := conn.Query(ctx, `SELECT version_id FROM goose_db_version ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	var got []int64
	for rows.Next() {
		var v int64
		require.NoError(t, rows.Scan(&v))
		got = append(got, v)
	}
	require.NoError(t, rows.Err())
	return got
}

// bookkeepingTablePresence reports which of the three bookkeeping tables exist.
// Asserted on the catalog rather than by catching a query error, so a
// connection-level failure cannot masquerade as absence.
func bookkeepingTablePresence(t testing.TB, ctx context.Context, connStr string) (legacy, archived, goose bool) {
	t.Helper()

	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer func() { _ = conn.Close(ctx) }() //nolint:errcheck // best-effort cleanup

	require.NoError(t, conn.QueryRow(ctx, `
		SELECT to_regclass('public.schema_migrations')           IS NOT NULL,
		       to_regclass('public.schema_migrations_pre_goose') IS NOT NULL,
		       to_regclass('public.goose_db_version')            IS NOT NULL
	`).Scan(&legacy, &archived, &goose))
	return legacy, archived, goose
}

// assertStrictlyAscending is the ordering assertion the entire cutover turns on.
//
// goose_db_version.id is GENERATED BY DEFAULT AS IDENTITY and goose's own
// ListMigrations is `ORDER BY id DESC`, so the order adopt INSERTED these rows is
// the order a later rollback walks them. A descending seed satisfies every count,
// every set-equality, and every min/max assertion in this file while rolling back
// in the wrong order — this is the only assertion here that separates the two.
func assertStrictlyAscending(versions []int64) {
	GinkgoHelper()

	Expect(versions).NotTo(BeEmpty())
	for i := 1; i < len(versions); i++ {
		Expect(versions[i]).To(BeNumerically(">", versions[i-1]),
			"ledger row %d (version_id %d) was inserted after version_id %d: the seed "+
				"ran DESCENDING. Every count and set assertion still passes in this "+
				"state; only rollback order breaks. See INV-STORE-10.",
			i, versions[i], versions[i-1])
	}
}

var _ = Describe("Migrator adopt", func() {
	Describe("C2a cutover from golang-migrate bookkeeping", func() {
		It("seeds one ascending row per embedded migration and retires the old table", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			// Precondition, and the positive control for the presence assertions
			// below: the legacy table really is there before Up() runs, so its
			// later absence is a change this code caused rather than a table that
			// never existed.
			legacy, archived, gooseLedger := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(legacy).To(BeTrue(), "precondition: the pre-conversion ledger must exist")
			Expect(archived).To(BeFalse(), "precondition: nothing archived yet")
			Expect(gooseLedger).To(BeFalse(), "precondition: goose bookkeeping must be absent")

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			embedded, err := migrator.PendingMigrations()
			Expect(err).NotTo(HaveOccurred())
			Expect(embedded).To(HaveLen(expectedAppliedMigrationRows),
				"precondition: goose sees the whole corpus as pending before adopt, "+
					"because its own ledger does not exist yet")

			Expect(migrator.Up()).To(Succeed())

			recorded := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)

			// The ledger carries goose's version-0 bootstrap row IN ADDITION to one
			// row per migration. A bare `HaveLen(44)` would fail against a CORRECT
			// database.
			Expect(recorded).To(HaveLen(expectedGooseLedgerRows))
			Expect(recorded[0]).To(Equal(int64(0)),
				"the bootstrap row must be seeded first; Provider.up and Provider.down "+
					"both fail against a ledger that lacks version 0")

			// THE assertion. See assertStrictlyAscending's doc comment.
			assertStrictlyAscending(recorded)

			// Set equality, not a count: counts are conserved under substitution, so
			// a phantom version swapped in for a real one leaves every count intact.
			want := make([]string, 0, len(embedded)+1)
			want = append(want, "0")
			for _, v := range embedded {
				want = append(want, strconv.FormatUint(uint64(v), 10))
			}
			got := make([]string, 0, len(recorded))
			for _, v := range recorded {
				got = append(got, strconv.FormatInt(v, 10))
			}
			sort.Strings(want)
			sort.Strings(got)
			Expect(strings.Join(got, ",")).To(Equal(strings.Join(want, ",")),
				"the seeded version set MUST equal the embedded set plus the version-0 "+
					"bootstrap row; a matching count with different members is exactly "+
					"what this comparison exists to catch")

			legacy, archived, gooseLedger = bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(gooseLedger).To(BeTrue(), "goose bookkeeping must exist after adopt")
			Expect(legacy).To(BeFalse(),
				"the old ledger must not survive under its original name, or a rolled-back "+
					"binary would find a live table and start writing to it")
			Expect(archived).To(BeTrue(),
				"the old ledger must survive under its archived name as a forensic record")
		})

		It("seeds only up to the recorded version when the database is behind", func() {
			ctx := context.Background()
			const recordedVersion = 17
			connStr := newPreConversionDatabase(suiteT, ctx, recordedVersion, false)

			// Adopt IN ISOLATION. Up() would run the provider immediately afterward
			// and apply 18..53, moving the very boundary this spec is asserting.
			Expect(store.AdoptForTest(ctx, connStr)).To(Succeed())

			recorded := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)
			assertStrictlyAscending(recorded)

			Expect(recorded[0]).To(Equal(int64(0)))
			Expect(recorded[len(recorded)-1]).To(Equal(int64(recordedVersion)),
				"the highest seeded version must be the recorded one")
			for _, v := range recorded {
				Expect(v).To(BeNumerically("<=", int64(recordedVersion)),
					"adopt must never record a version above the one golang-migrate reported")
			}

			// 1..17 plus the version-0 row. Stated as a derivation from the corpus
			// rather than a bare literal so a future migration cannot silently
			// invalidate it.
			Expect(recorded).To(HaveLen(recordedVersion+1),
				"versions 1..17 are contiguous in the corpus, so the seed is 17 rows plus "+
					"goose's version-0 bootstrap row")
		})
	})

	Describe("C2b re-running adopt against an already-populated ledger", func() {
		It("changes nothing on the second run", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			// PAIRED POSITIVE CONTROL. "Unchanged" is only evidence if the first run
			// is observed changing something; without this, a no-op adopt that never
			// worked at all would pass the negative control below.
			before := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)
			Expect(before).To(BeEmpty(), "precondition: no goose bookkeeping before the first adopt")

			Expect(store.AdoptForTest(ctx, connStr)).To(Succeed())

			afterFirst := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)
			Expect(afterFirst).To(HaveLen(expectedGooseLedgerRows),
				"positive control: the FIRST adopt must be observed populating the ledger")
			assertStrictlyAscending(afterFirst)

			legacy, archived, _ := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(legacy).To(BeFalse())
			Expect(archived).To(BeTrue())

			// THE NEGATIVE CONTROL: a second adopt must be a no-op. The emptiness
			// re-check happens inside the advisory lock, so the loser of a boot race
			// finds the work already done rather than doubling it.
			Expect(store.AdoptForTest(ctx, connStr)).To(Succeed())

			afterSecond := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)
			Expect(afterSecond).To(Equal(afterFirst),
				"the second adopt must not append, reorder, or duplicate any row")

			legacy, archived, _ = bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(legacy).To(BeFalse(), "the second adopt must not recreate the legacy table")
			Expect(archived).To(BeTrue(), "the archived table must survive the second adopt")
		})
	})

	Describe("adopt after a read-only verb has touched the ledger", func() {
		// Regression guard for a defect found while building C2a.
		//
		// goose's ensureVersionTable runs on ANY versioned operation, including the
		// read-only ones, and creates goose_db_version complete with its version-0
		// bootstrap row. The adopt gate fires only from Up(), so an operator running
		// `holomush migrate status` on a not-yet-adopted database to decide whether
		// to deploy leaves exactly this state behind. If adopt treats a bare
		// bootstrap row as evidence that the cutover already happened, that harmless
		// diagnostic permanently disables it: the next Up() skips adopt, sees an
		// empty ledger, and re-runs all 44 migrations against a database that
		// already has the schema. Observed as `partial migration error
		// (type:sql,version:1): relation "holomush_system_info" already exists`.
		It("still adopts when a status-style read has already created the ledger", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			reader, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			// Version() is what `migrate status` and `migrate version` both call.
			_, _, err = reader.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(reader.Close()).To(Succeed())

			// Positive control: the read really did create the ledger and really did
			// leave the bootstrap row behind. Without this the spec below could pass
			// against a build where reads create nothing, proving nothing.
			_, _, gooseLedger := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(gooseLedger).To(BeTrue(),
				"positive control: a read-only verb MUST have created the ledger, or this "+
					"spec is not exercising the state it exists to guard")
			Expect(gooseLedgerInInsertionOrder(suiteT, ctx, connStr)).To(Equal([]int64{0}),
				"positive control: the read leaves exactly goose's version-0 bootstrap row")

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Up()).To(Succeed(),
				"a prior read-only verb must not disable the cutover")

			recorded := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)
			Expect(recorded).To(HaveLen(expectedGooseLedgerRows),
				"adopt must top up the read-created ledger, neither skipping nor "+
					"duplicating goose's version-0 row")
			assertStrictlyAscending(recorded)

			legacy, archived, _ := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(legacy).To(BeFalse())
			Expect(archived).To(BeTrue())
		})
	})

	Describe("C2c refusing a dirty pre-conversion ledger", func() {
		It("aborts boot and writes nothing", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, true)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			upErr := migrator.Up()
			Expect(upErr).To(HaveOccurred())

			// Error IDENTITY, not a message substring. errutil.AssertErrorCode reads
			// the TOP-LEVEL oops code, so this also proves Up() did not re-wrap the
			// refusal in a generic MIGRATION_UP_FAILED on the way out. A
			// ContainSubstring check could not tell those apart.
			errutil.AssertErrorCode(suiteT, upErr, "MIGRATION_ADOPT_REFUSED_DIRTY")
			errutil.AssertErrorContext(suiteT, upErr, "dirty_version", uint(latestMigrationVersion))
			Expect(upErr.Error()).To(ContainSubstring(strconv.Itoa(latestMigrationVersion)),
				"the operator-facing message must name the version they have to resolve")

			// Nothing written: no goose bookkeeping, and the legacy table still under
			// its ORIGINAL name so the previous tooling can still resolve the dirty
			// state.
			legacy, archived, gooseLedger := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(gooseLedger).To(BeFalse(),
				"a refused adopt must not create goose bookkeeping")
			Expect(legacy).To(BeTrue(),
				"a refused adopt must leave the legacy table under its original name, or "+
					"the tooling the error points the operator at cannot find it")
			Expect(archived).To(BeFalse(),
				"a refused adopt must not archive anything")
		})
	})

	Describe("upward migration paths other than Up", func() {
		// Up() is documented as the entry point that fires the cutover, and the
		// settled decision that the read-only verbs must NOT fire it is about
		// DIAGNOSTICS. Steps(n>0) and Migrate() are neither read-only nor
		// diagnostic: they migrate upward. Against a pre-adopt database an
		// ungated upward path reads goose's empty ledger as version 0 and starts
		// re-applying migration 1 over a schema that is already fully present —
		// the `relation "holomush_system_info" already exists` failure the
		// version_id > 0 probe filter exists to prevent.
		It("adopts before Steps applies anything upward", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Steps(1)).To(Succeed(),
				"a positive Steps against a pre-adopt database must adopt first, not "+
					"re-apply migration 1 over the schema it already has")

			recorded := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)
			Expect(recorded).To(HaveLen(expectedGooseLedgerRows),
				"Steps must leave the same seeded ledger Up would have")
			assertStrictlyAscending(recorded)

			legacy, archived, _ := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(legacy).To(BeFalse())
			Expect(archived).To(BeTrue())
		})

		It("adopts before Migrate decides its direction", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Migrate(latestMigrationVersion)).To(Succeed())

			recorded := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)
			Expect(recorded).To(HaveLen(expectedGooseLedgerRows))
			assertStrictlyAscending(recorded)

			version, _, err := migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(latestMigrationVersion)),
				"the adopted version must be visible to the caller afterwards")
		})

		It("adopts before Migrate rolls a pre-adopt database DOWN to a target", func() {
			ctx := context.Background()
			const target = 40
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			// THE reason adopt runs at the head of Migrate rather than inside its
			// current < target branch: the direction itself is decided from the
			// version read. Read pre-adopt, that version is 0, so a request to move
			// DOWN to 40 is mistaken for a request to move UP to 40 and silently
			// does nothing.
			Expect(migrator.Migrate(target)).To(Succeed())

			version, _, err := migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(target)),
				"Migrate must reach the requested target, not stall at the pre-adopt "+
					"reading of version 0")
		})
	})

	Describe("AdoptPreview", func() {
		It("reports the pending cutover and the recorded version without performing it", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			preview, err := migrator.AdoptPreview()
			Expect(err).NotTo(HaveOccurred())
			Expect(preview.Pending).To(BeTrue())
			Expect(preview.Recorded).To(Equal(uint(latestMigrationVersion)),
				"the preview must report the version the LEGACY ledger records, not goose's 0")
			Expect(preview.Dirty).To(BeFalse())

			// The preview is a diagnostic and must be as inert as the read-only
			// verbs: an operator previewing a cutover must not thereby perform it.
			legacy, archived, _ := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(legacy).To(BeTrue(), "the preview must not archive the legacy table")
			Expect(archived).To(BeFalse())
			Expect(gooseLedgerInInsertionOrder(suiteT, ctx, connStr)).To(BeEmpty(),
				"the preview must not seed, or create, goose bookkeeping")
		})

		It("reports a dirty legacy ledger so a preview warns the deploy will refuse", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, true)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			preview, err := migrator.AdoptPreview()
			Expect(err).NotTo(HaveOccurred())
			Expect(preview.Pending).To(BeTrue())
			Expect(preview.Dirty).To(BeTrue())
		})

		It("reports nothing pending once the cutover has happened", func() {
			// The negative control. Without it a preview hard-wired to Pending=true
			// would satisfy every assertion above.
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Up()).To(Succeed())

			preview, err := migrator.AdoptPreview()
			Expect(err).NotTo(HaveOccurred())
			Expect(preview.Pending).To(BeFalse(),
				"after the cutover goose's own ledger is the truthful one")
		})

		It("reports nothing pending on a database that never used the old tooling", func() {
			connStr := testutil.RawDatabase(suiteT, sharedPG)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			preview, err := migrator.AdoptPreview()
			Expect(err).NotTo(HaveOccurred())
			Expect(preview.Pending).To(BeFalse())
		})
	})

	Describe("C2f refusing an ambiguous pre-conversion ledger", func() {
		// golang-migrate's postgres driver truncates and re-inserts inside one
		// transaction, so a well-formed ledger holds exactly one row and this
		// refusal never fires in normal operation. It is defence on the
		// unrecoverable path: a partial restore, a merged dump, or manual repair by
		// the very operator the dirty refusal points at the old tooling can leave
		// two rows behind, and an unordered single-row read then picks one
		// arbitrarily. If the arbitrary pick is the HIGHER version, adopt records
		// migrations as applied that never ran, archives the evidence, and goose
		// can never detect the divergence. Unlike the dirty case, nothing else
		// refuses.
		It("aborts boot and writes nothing when the legacy table holds more than one row", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)
			insertLegacyRow(suiteT, ctx, connStr, latestMigrationVersion-10, false)

			// Positive control: the fixture really did produce the ambiguous shape.
			// Without it a refusal caused by something else would read as a pass.
			Expect(legacyRowCount(suiteT, ctx, connStr)).To(Equal(int64(2)),
				"precondition: the ledger must actually be ambiguous")

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			upErr := migrator.Up()
			Expect(upErr).To(HaveOccurred())

			// Error IDENTITY at the top level, so this also proves Up did not
			// re-wrap the refusal in a generic MIGRATION_UP_FAILED on the way out.
			errutil.AssertErrorCode(suiteT, upErr, "MIGRATION_ADOPT_REFUSED_AMBIGUOUS_LEDGER")
			errutil.AssertErrorContext(suiteT, upErr, "row_count", int64(2))

			legacy, archived, gooseLedger := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(gooseLedger).To(BeFalse(),
				"a refused adopt must not create goose bookkeeping")
			Expect(legacy).To(BeTrue(),
				"a refused adopt must leave the legacy table under its original name so "+
					"the operator can inspect and repair it")
			Expect(archived).To(BeFalse(),
				"a refused adopt must not archive anything")
			Expect(legacyRowCount(suiteT, ctx, connStr)).To(Equal(int64(2)),
				"a refused adopt must not have edited the ledger it refused")
		})

		It("still adopts the one-row ledger golang-migrate actually maintains", func() {
			// The paired positive control for the refusal above: without it, a
			// readLegacyVersion that refused EVERY ledger would satisfy the spec.
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)
			Expect(legacyRowCount(suiteT, ctx, connStr)).To(Equal(int64(1)))

			Expect(store.AdoptForTest(ctx, connStr)).To(Succeed())
			Expect(gooseLedgerInInsertionOrder(suiteT, ctx, connStr)).
				To(HaveLen(expectedGooseLedgerRows))
		})
	})

	Describe("the dirty refusal against golang-migrate's NilVersion sentinel", func() {
		// readLegacyVersion clamps a negative recorded version to 0 before the
		// dirty check sees it, so a (version = -1, dirty = true) ledger aborted
		// boot with "marked dirty at version 0" — pointing the operator at a
		// version that exists in no corpus. The refusal itself was always correct;
		// only the version it named was not.
		It("names the sentinel rather than a version 0 that does not exist", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, -1, true)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			upErr := migrator.Up()
			Expect(upErr).To(HaveOccurred())
			errutil.AssertErrorCode(suiteT, upErr, "MIGRATION_ADOPT_REFUSED_DIRTY")
			errutil.AssertErrorContext(suiteT, upErr, "raw_version", int64(-1))
			Expect(upErr.Error()).NotTo(ContainSubstring("dirty at version 0"),
				"version 0 is the clamp, not the ledger's value; naming it sends the "+
					"operator looking for a migration that does not exist")
			Expect(upErr.Error()).To(ContainSubstring("records no version"))

			// The refusal must stay as complete as the ordinary dirty one.
			legacy, archived, gooseLedger := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(gooseLedger).To(BeFalse())
			Expect(legacy).To(BeTrue())
			Expect(archived).To(BeFalse())
		})

		It("still names the recorded version for an ordinary dirty ledger", func() {
			// The negative control: a refusal that always said "records no version"
			// would satisfy the spec above.
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, true)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			upErr := migrator.Up()
			Expect(upErr).To(HaveOccurred())
			errutil.AssertErrorContext(suiteT, upErr, "raw_version", int64(latestMigrationVersion))
			Expect(upErr.Error()).To(ContainSubstring(strconv.Itoa(latestMigrationVersion)))
			Expect(upErr.Error()).NotTo(ContainSubstring("records no version"))
		})
	})

	Describe("C2d rollback order after adopt", func() {
		// Verifies: INV-STORE-10
		It("rolls the adopted database all the way down in descending version order", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Up()).To(Succeed())

			// The seed's INSERT order, read back through the identity column that
			// goose's own ORDER BY id DESC consults. This is the property; the
			// Down() below is the consequence of it.
			assertStrictlyAscending(gooseLedgerInInsertionOrder(suiteT, ctx, connStr))

			// Down() walks ListMigrations, which is ORDER BY id DESC — the reverse of
			// the insertion order asserted above. Against an ASCENDING seed that
			// yields descending versions, so each migration is rolled back before the
			// ones it depends on. Against a DESCENDING seed it yields ASCENDING
			// versions and goose tries to roll back version 1 while versions 2..53
			// still stand, which fails partway through with a partial migration
			// error. This spec cannot be derived from an adopt-then-Up() test: an
			// Up() after a correct and an incorrect seed look identical.
			Expect(migrator.Down()).To(Succeed(),
				"a full rollback of the adopted database must complete; failure here means "+
					"the seed inserted in the wrong order")

			version, _, err := migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(0)), "the rollback must reach version 0")

			// The archived legacy ledger is deliberately NOT migration-managed — no
			// down migration knows about it — so it correctly outlives the rollback.
			// Asserting on the exact remaining set rather than merely filtering it
			// out keeps this from quietly tolerating other survivors.
			remaining := queryTableNames(suiteT, ctx, connStr)
			Expect(remaining).To(Equal([]string{"schema_migrations_pre_goose"}),
				"a rollback that completed in the correct order leaves behind only the "+
					"archived pre-goose ledger, which no down migration manages")
		})
	})

	Describe("C2e two replicas booting at once", func() {
		It("seeds exactly once", func() {
			ctx := context.Background()
			connStr := newPreConversionDatabase(suiteT, ctx, latestMigrationVersion, false)

			migrator1, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator1.Close()

			migrator2, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator2.Close()

			var wg sync.WaitGroup
			var err1, err2 error

			wg.Add(2)
			go func() {
				defer wg.Done()
				err1 = migrator1.Up()
			}()
			go func() {
				defer wg.Done()
				err2 = migrator2.Up()
			}()
			wg.Wait()

			// Deliberately NOT asserting which goroutine won — that is a race, and a
			// spec that pins it would be flaky by construction. What must hold is
			// that neither boot failed and the ledger reflects exactly one seeding.
			Expect(err1).NotTo(HaveOccurred(), "replica 1 boot must not fail")
			Expect(err2).NotTo(HaveOccurred(), "replica 2 boot must not fail")

			recorded := gooseLedgerInInsertionOrder(suiteT, ctx, connStr)
			Expect(recorded).To(HaveLen(expectedGooseLedgerRows),
				"a double-seed would land %d rows here", 2*expectedGooseLedgerRows)
			assertStrictlyAscending(recorded)

			// assertStrictlyAscending already rejects duplicates (it requires strictly
			// greater), but state it separately: duplicate version_id values are the
			// specific corruption a lost boot race produces.
			seen := make(map[int64]int, len(recorded))
			for _, v := range recorded {
				seen[v]++
			}
			for v, n := range seen {
				Expect(n).To(Equal(1), "version_id %d recorded %d times", v, n)
			}

			legacy, archived, _ := bookkeepingTablePresence(suiteT, ctx, connStr)
			Expect(legacy).To(BeFalse())
			Expect(archived).To(BeTrue(),
				"exactly one replica may perform the rename; the other must find it done")
		})
	})
})
