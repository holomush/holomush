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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/internal/store"
	"github.com/holomush/holomush/internal/testsupport/quarantinetest"
	"github.com/holomush/holomush/test/testutil"
)

// latestMigrationVersion is the numeric prefix of the highest migration in
// internal/store/migrations. Bump it in the same change that adds a migration —
// the FullCycle spec below asserts Up() lands exactly here, so a new migration
// fails this spec until the constant follows it.
const latestMigrationVersion = 54

// Bookkeeping shape of goose's ledger after a fresh-database Up().
//
// goose writes ONE row per applied migration, PLUS a version-0 bootstrap row it
// inserts when it first creates goose_db_version. golang-migrate kept a single
// mutable row, so this is a genuine change of shape rather than a rename — and
// it is why a bare `count(*) == 44` is WRONG against a correct database.
//
// The migration corpus has gaps (versions 21–29 are unused), so the highest
// version is 54 while only 45 migrations exist. Bump these together with
// latestMigrationVersion when a migration is added.
const (
	expectedAppliedMigrationRows = 45
	gooseBootstrapRows           = 1
	expectedGooseLedgerRows      = expectedAppliedMigrationRows + gooseBootstrapRows
)

// expectedTables lists every table present after all migrations have been applied.
// NOTE: The `events` table was dropped by migration 000010 (F6 schema cutover);
// it does NOT appear here.
var expectedTables = []string{
	"access_audit_log",
	"access_policies",
	"access_policy_versions",
	"admin_approvals",
	"bootstrap_metadata",
	"character_roles",
	"characters",
	"content_items",
	"crypto_bootstrap_state",
	"crypto_keys",
	"crypto_rekey_checkpoints",
	"entity_properties",
	"events_audit",
	"events_audit_unpartitioned",
	"exits",
	"holomush_system_info",
	"locations",
	"objects",
	"outbox",
	"password_resets",
	"player_aliases",
	"player_character_bindings",
	"player_sessions",
	"player_totp",
	"player_totp_recovery_codes",
	"players",
	"plugins",
	"scene_participants",
	"session_connections",
	"sessions",
	"setting_bootstrap_state",
	"system_aliases",
	"world_consumer_receipts",
	"world_consumer_watermarks",
	"world_feed_counter",
	"world_genesis_checkpoint",
}

// queryTableNames returns user-defined table names (excluding schema_migrations)
// from the public schema, sorted alphabetically. Partition CHILD tables are
// excluded (relispartition): they are an implementation detail and their names
// are time-varying (e.g. the events_audit_YYYY_MM monthly partitions created by
// 000052), so only the ordinary tables and partitioned PARENTS are returned.
func queryTableNames(t *testing.T, ctx context.Context, connStr string) []string {
	t.Helper()
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx,
		`SELECT c.relname
		 FROM pg_catalog.pg_class c
		 JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public'
		   AND c.relkind IN ('r', 'p')
		   AND c.relispartition = false
		   AND c.relname NOT IN ('schema_migrations', 'goose_db_version')
		 ORDER BY c.relname`)
	require.NoError(t, err)
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())
	sort.Strings(tables)
	return tables
}

// verifySeedData checks that the baseline seed rows required for bootstrap exist.
func verifySeedData(t *testing.T, ctx context.Context, connStr string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	// System aliases required by command dispatcher
	var aliasCount int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM system_aliases WHERE alias IN ('tel', 'ex')").Scan(&aliasCount)
	require.NoError(t, err)
	assert.Equal(t, 2, aliasCount, "should have 'tel' and 'ex' system aliases")

	// Starting location required by bootstrap
	var locCount int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM locations WHERE name = 'The Void'").Scan(&locCount)
	require.NoError(t, err)
	assert.Equal(t, 1, locCount, "should have 'The Void' starting location")

	// Test player and character required by bootstrap
	var playerCount int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM players WHERE username = 'testuser'").Scan(&playerCount)
	require.NoError(t, err)
	assert.Equal(t, 1, playerCount, "should have 'testuser' player")

	var charCount int
	err = conn.QueryRow(ctx, "SELECT count(*) FROM characters WHERE name = 'TestChar'").Scan(&charCount)
	require.NoError(t, err)
	assert.Equal(t, 1, charCount, "should have 'TestChar' character")
}

var _ = Describe("Migrator", func() {
	Describe("FullCycle", func() {
		It("applies all migrations, rolls back to zero, and is idempotent on re-apply", func() {
			ctx := context.Background()

			connStr := testutil.RawDatabase(suiteT, sharedPG)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			// Phase 1: Fresh database — version 0, no tables
			version, dirty, err := migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(0)))
			Expect(dirty).To(BeFalse())

			tables := queryTableNames(suiteT, ctx, connStr)
			Expect(tables).To(BeEmpty(), "fresh database should have no user tables")

			// Phase 2: Up — apply all migrations, verify all tables
			Expect(migrator.Up()).To(Succeed())

			version, dirty, err = migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(latestMigrationVersion)))
			Expect(dirty).To(BeFalse())

			tables = queryTableNames(suiteT, ctx, connStr)
			Expect(tables).To(Equal(expectedTables), "Up() should create all expected tables")
			verifySeedData(suiteT, ctx, connStr)

			// Phase 3: Down — rollback, verify all tables gone
			Expect(migrator.Down()).To(Succeed())

			version, dirty, err = migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(0)))
			Expect(dirty).To(BeFalse())

			tables = queryTableNames(suiteT, ctx, connStr)
			Expect(tables).To(BeEmpty(), "Down() should remove all user tables")

			// Phase 4: Re-apply — prove idempotency
			Expect(migrator.Up()).To(Succeed())

			version, dirty, err = migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(latestMigrationVersion)))
			Expect(dirty).To(BeFalse())

			tables = queryTableNames(suiteT, ctx, connStr)
			Expect(tables).To(Equal(expectedTables), "second Up() should recreate all tables")
			verifySeedData(suiteT, ctx, connStr)
		})
	})

	// GooseBookkeeping pins the ledger end-state of the golang-migrate → goose
	// swap. The FullCycle spec above asserts the resulting SCHEMA; nothing
	// asserted the BOOKKEEPING, so a regression that reverted the ledger table
	// or double-recorded versions would have been invisible to the suite.
	//
	// The version-set equality is the load-bearing half. A row count alone
	// cannot distinguish the real corpus from 44 phantom rows filling the
	// unused 21–29 gap, and it is exactly the kind of correct-looking number
	// that survives a broken migration source.
	Describe("GooseBookkeeping", func() {
		It("records one goose_db_version row per embedded migration plus the version-0 bootstrap row, and no schema_migrations table", func() {
			ctx := context.Background()

			connStr := testutil.RawDatabase(suiteT, sharedPG)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			// Read the EMBEDDED version set before applying anything: on a blank
			// database every embedded version is pending, so this is the corpus
			// as the filesystem describes it, derived independently of the ledger
			// the assertions below read back.
			embedded, err := migrator.PendingMigrations()
			Expect(err).NotTo(HaveOccurred())
			Expect(embedded).To(HaveLen(expectedAppliedMigrationRows),
				"precondition: the embedded corpus MUST hold %d migrations, or the set "+
					"equality below would compare the ledger against an empty expectation",
				expectedAppliedMigrationRows)

			Expect(migrator.Up()).To(Succeed())

			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			// Three separate expectations rather than one compound check, so a
			// failure names which property broke.
			var appliedRows int
			Expect(conn.QueryRow(ctx,
				`SELECT count(*) FROM goose_db_version WHERE version_id > 0`).Scan(&appliedRows)).To(Succeed())
			Expect(appliedRows).To(Equal(expectedAppliedMigrationRows),
				"goose records one row per applied migration")

			var totalRows int
			Expect(conn.QueryRow(ctx,
				`SELECT count(*) FROM goose_db_version`).Scan(&totalRows)).To(Succeed())
			Expect(totalRows).To(Equal(expectedGooseLedgerRows),
				"the ledger also carries goose's version-0 bootstrap row; asserting a bare "+
					"%d total would fail against a CORRECT database",
				expectedAppliedMigrationRows)

			var minVersion int64
			Expect(conn.QueryRow(ctx,
				`SELECT min(version_id) FROM goose_db_version`).Scan(&minVersion)).To(Succeed())
			Expect(minVersion).To(Equal(int64(0)),
				"the extra row MUST be goose's version-0 bootstrap row, not a duplicate "+
					"or an out-of-corpus version")

			// golang-migrate's ledger table must not have been created. Asserted
			// on the catalog rather than by catching a query error, so a
			// connection-level failure cannot masquerade as absence. The paired
			// goose_db_version probe is the positive control: without it a typo
			// in the table name would make the absence assertion vacuously true.
			var schemaMigrationsAbsent, gooseLedgerPresent bool
			Expect(conn.QueryRow(ctx, `
				SELECT to_regclass('public.schema_migrations') IS NULL,
				       to_regclass('public.goose_db_version') IS NOT NULL
			`).Scan(&schemaMigrationsAbsent, &gooseLedgerPresent)).To(Succeed())
			Expect(gooseLedgerPresent).To(BeTrue(),
				"positive control: to_regclass MUST resolve a table that does exist here")
			Expect(schemaMigrationsAbsent).To(BeTrue(),
				"schema_migrations is golang-migrate's ledger and MUST NOT exist after the swap")

			// Set equality, not a count: the recorded versions must be exactly
			// the embedded ones.
			rows, err := conn.Query(ctx,
				`SELECT version_id FROM goose_db_version WHERE version_id > 0 ORDER BY version_id`)
			Expect(err).NotTo(HaveOccurred())
			defer rows.Close()

			var recorded []string
			for rows.Next() {
				var v int64
				Expect(rows.Scan(&v)).To(Succeed())
				recorded = append(recorded, strconv.FormatInt(v, 10))
			}
			Expect(rows.Err()).NotTo(HaveOccurred())

			want := make([]string, 0, len(embedded))
			for _, v := range embedded {
				want = append(want, strconv.FormatUint(uint64(v), 10))
			}
			sort.Strings(want)
			sort.Strings(recorded)

			Expect(strings.Join(recorded, ",")).To(Equal(strings.Join(want, ",")),
				"the recorded version set MUST equal the embedded version set — a matching "+
					"count with different members (e.g. phantom rows filling the unused "+
					"21–29 gap) is exactly what this comparison exists to catch")
		})
	})

	Describe("ConcurrentUp", func() {
		It("allows at least one concurrent Up() to succeed with consistent final state", func() {
			if !quarantinetest.Enabled() {
				Skip("quarantined: holomush-pqzv")
			}
			connStr := testutil.RawDatabase(suiteT, sharedPG)

			// Create two migrators pointing to the same database
			migrator1, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator1.Close()

			migrator2, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator2.Close()

			// Run migrations concurrently
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

			// At least one should succeed (or both if sequential lock acquisition)
			// ErrNoChange is acceptable (means migrations already applied)
			// Both succeeding is fine due to lock serialization
			successCount := 0
			if err1 == nil {
				successCount++
			}
			if err2 == nil {
				successCount++
			}
			Expect(successCount).To(BeNumerically(">=", 1), "at least one migration should succeed")

			// Verify consistent final state using a fresh migrator
			verifier, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer verifier.Close()

			version, dirty, err := verifier.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(BeNumerically(">", uint(0)), "migrations should have been applied")
			Expect(dirty).To(BeFalse(), "database should not be in dirty state")

			// Verify both migrators report same version
			v1, dirty1, err := migrator1.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(v1).To(Equal(version), "migrator1 should report same version")
			Expect(dirty1).To(BeFalse())

			v2, dirty2, err := migrator2.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(v2).To(Equal(version), "migrator2 should report same version")
			Expect(dirty2).To(BeFalse())
		})
	})

	// Migration000035_DropsCryptoRekeyCheckpointFKs asserts the load-bearing
	// post-condition of migration 000035 (holomush-jxo8.7.48): no foreign-key
	// constraints remain on crypto_rekey_checkpoints.{new_dek_id,old_dek_id}.
	//
	// Without this assertion, a future regression that names the constraints
	// differently (e.g. explicit CONSTRAINT clause in 000031) would silently
	// leave the FKs in place — the migration's DROP CONSTRAINT IF EXISTS would
	// match nothing, no error, no test failure. Asserting the actual
	// information_schema state is the load-bearing check.
	Describe("Migration000035_DropsCryptoRekeyCheckpointFKs", func() {
		It("drops both FK constraints on crypto_rekey_checkpoints after migration 000035", func() {
			ctx := context.Background()

			connStr := testutil.RawDatabase(suiteT, sharedPG)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Up()).To(Succeed())

			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			// Count remaining FK constraints on crypto_rekey_checkpoints. After
			// migration 000035 there MUST be zero. (The table itself still has
			// PRIMARY KEY, CHECK, and UNIQUE constraints — those are unaffected.)
			var fkCount int
			err = conn.QueryRow(ctx, `
		        SELECT COUNT(*)
		          FROM information_schema.table_constraints
		         WHERE table_name = 'crypto_rekey_checkpoints'
		           AND constraint_type = 'FOREIGN KEY'
		    `).Scan(&fkCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(fkCount).To(Equal(0),
				"migration 000035 (holomush-jxo8.7.48) MUST drop both FKs on crypto_rekey_checkpoints "+
					"(new_dek_id, old_dek_id). Future migrations that re-introduce a FK to crypto_keys "+
					"would defeat the no-prod-shape design — see spec §3.6.1.")

			// Defense-in-depth: also confirm the specific column FKs aren't present
			// (an FK to a different table wouldn't be caught by the count-zero
			// check above, but it would be even worse).
			var perColumnFKCount int
			err = conn.QueryRow(ctx, `
		        SELECT COUNT(*)
		          FROM information_schema.referential_constraints rc
		          JOIN information_schema.key_column_usage kcu
		            ON kcu.constraint_name = rc.constraint_name
		         WHERE kcu.table_name = 'crypto_rekey_checkpoints'
		           AND kcu.column_name IN ('new_dek_id', 'old_dek_id')
		    `).Scan(&perColumnFKCount)
			Expect(err).NotTo(HaveOccurred())
			Expect(perColumnFKCount).To(Equal(0),
				"no FK from crypto_rekey_checkpoints.{new_dek_id,old_dek_id} to any table is permitted "+
					"after migration 000035")
		})
	})

	// Migration000047_DisablesUnconditionalSceneWriteSeed pins the load-bearing
	// behavior of the holomush-8m01u fix on the EXISTING-deployment path. The seed
	// was removed from the Go corpus (so fresh installs never create it), but a
	// deployment that already bootstrapped seed:player-scene-participant keeps a
	// live enabled row until this migration disables it. The focus_routed_input
	// integration spec only exercises a fresh DB where the row never exists, so
	// without this test the migration's compare-and-swap guard — the sole
	// mechanism closing the ABAC bypass in production — is never run against a
	// real pre-existing row.
	Describe("Migration000047_DisablesUnconditionalSceneWriteSeed", func() {
		const (
			seedName     = "seed:player-scene-participant"
			vestigialDSL = `permit(principal is character, action in ["write"], resource is scene);`
		)

		// insertSceneWriteSeed seeds a source='seed', enabled=true access_policies
		// row, modelling a deployment that bootstrapped the vestigial seed.
		// created_at/updated_at/version use their column defaults (BIGINT epoch-ns
		// since migration 000043).
		insertSceneWriteSeed := func(ctx context.Context, conn *pgx.Conn, dsl string) {
			_, err := conn.Exec(ctx, `
				INSERT INTO access_policies
					(id, name, description, effect, source, dsl_text, compiled_ast, enabled, seed_version, created_by)
				VALUES
					('pol-8m01u-test', $1, 'scene write seed', 'permit', 'seed', $2, '{"grammar_version":1}'::jsonb, true, 1, 'system')`,
				seedName, dsl)
			Expect(err).NotTo(HaveOccurred())
		}

		enabledOf := func(ctx context.Context, conn *pgx.Conn) bool {
			var enabled bool
			Expect(conn.QueryRow(
				ctx,
				`SELECT enabled FROM access_policies WHERE name = $1`, seedName,
			).Scan(&enabled)).To(Succeed())
			return enabled
		}

		It("disables a pre-existing enabled seed row carrying the exact vestigial DSL", func() {
			ctx := context.Background()
			connStr := testutil.RawDatabase(suiteT, sharedPG)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			// Stop at 46 so access_policies exists but 000047 has not run.
			Expect(migrator.Migrate(46)).To(Succeed())

			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			insertSceneWriteSeed(ctx, conn, vestigialDSL)
			Expect(enabledOf(ctx, conn)).To(BeTrue(), "precondition: the seed row starts enabled")

			Expect(migrator.Migrate(47)).To(Succeed())

			Expect(enabledOf(ctx, conn)).To(BeFalse(),
				"migration 000047 (holomush-8m01u) MUST disable the vestigial unconditional scene-write "+
					"seed so the ABAC bypass is closed in existing deployments")
		})

		It("leaves an operator-customized seed row untouched (exact-DSL guard)", func() {
			ctx := context.Background()
			connStr := testutil.RawDatabase(suiteT, sharedPG)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Migrate(46)).To(Succeed())

			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			// An operator who narrowed the seed via `policy edit` carries a
			// different dsl_text; the migration's exact-DSL guard MUST skip it.
			customDSL := `permit(principal is character, action in ["write"], resource is scene) when { principal.id in resource.scene.participants };`
			insertSceneWriteSeed(ctx, conn, customDSL)

			Expect(migrator.Migrate(47)).To(Succeed())

			Expect(enabledOf(ctx, conn)).To(BeTrue(),
				"migration 000047 MUST NOT disable an operator-customized row (dsl_text != the vestigial "+
					"permit) — the WHERE guard's exact-DSL clause respects `policy edit` customizations")
		})
	})

	// Migration000048_DisablesUnconditionalSceneReadSeed is the read twin of the
	// 000047 suite above (holomush-sjtlz mirrors holomush-8m01u): the vestigial
	// unconditional seed:player-scene-read was removed from the Go corpus, but a
	// deployment that already bootstrapped it keeps a live enabled row until this
	// migration disables it. The fresh-DB integration specs never create the row,
	// so without this test the migration's compare-and-swap guard — the sole
	// mechanism closing the metadata-read bypass in production — is never run
	// against a real pre-existing row.
	Describe("Migration000048_DisablesUnconditionalSceneReadSeed", func() {
		const (
			seedName     = "seed:player-scene-read"
			vestigialDSL = `permit(principal is character, action in ["read"], resource is scene);`
		)

		// insertSceneReadSeed seeds a source='seed', enabled=true access_policies
		// row, modelling a deployment that bootstrapped the vestigial seed.
		insertSceneReadSeed := func(ctx context.Context, conn *pgx.Conn, dsl string) {
			_, err := conn.Exec(ctx, `
				INSERT INTO access_policies
					(id, name, description, effect, source, dsl_text, compiled_ast, enabled, seed_version, created_by)
				VALUES
					('pol-sjtlz-test', $1, 'scene read seed', 'permit', 'seed', $2, '{"grammar_version":1}'::jsonb, true, 1, 'system')`,
				seedName, dsl)
			Expect(err).NotTo(HaveOccurred())
		}

		enabledOf := func(ctx context.Context, conn *pgx.Conn) bool {
			var enabled bool
			Expect(conn.QueryRow(
				ctx,
				`SELECT enabled FROM access_policies WHERE name = $1`, seedName,
			).Scan(&enabled)).To(Succeed())
			return enabled
		}

		It("disables a pre-existing enabled seed row carrying the exact vestigial DSL", func() {
			ctx := context.Background()
			connStr := testutil.RawDatabase(suiteT, sharedPG)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			// Stop at 47 so access_policies exists but 000048 has not run.
			Expect(migrator.Migrate(47)).To(Succeed())

			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			insertSceneReadSeed(ctx, conn, vestigialDSL)
			Expect(enabledOf(ctx, conn)).To(BeTrue(), "precondition: the seed row starts enabled")

			Expect(migrator.Migrate(48)).To(Succeed())

			Expect(enabledOf(ctx, conn)).To(BeFalse(),
				"migration 000048 (holomush-sjtlz) MUST disable the vestigial unconditional scene-read "+
					"seed so the metadata-read bypass is closed in existing deployments")
		})

		It("leaves an operator-customized seed row untouched (exact-DSL guard)", func() {
			ctx := context.Background()
			connStr := testutil.RawDatabase(suiteT, sharedPG)

			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Migrate(47)).To(Succeed())

			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			// An operator who narrowed the seed via `policy edit` carries a
			// different dsl_text; the migration's exact-DSL guard MUST skip it.
			customDSL := `permit(principal is character, action in ["read"], resource is scene) when { principal.id in resource.scene.participants };`
			insertSceneReadSeed(ctx, conn, customDSL)

			Expect(migrator.Migrate(48)).To(Succeed())

			Expect(enabledOf(ctx, conn)).To(BeTrue(),
				"migration 000048 MUST NOT disable an operator-customized row (dsl_text != the vestigial "+
					"permit) — the WHERE guard's exact-DSL clause respects `policy edit` customizations")
		})
	})
})
