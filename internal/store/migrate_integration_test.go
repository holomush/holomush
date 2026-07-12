//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
	"context"
	"sort"
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
// from the public schema, sorted alphabetically.
func queryTableNames(t *testing.T, ctx context.Context, connStr string) []string {
	t.Helper()
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx,
		`SELECT tablename FROM pg_catalog.pg_tables
		 WHERE schemaname = 'public' AND tablename != 'schema_migrations'
		 ORDER BY tablename`)
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
			Expect(version).To(Equal(uint(50)))
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
			Expect(version).To(Equal(uint(50)))
			Expect(dirty).To(BeFalse())

			tables = queryTableNames(suiteT, ctx, connStr)
			Expect(tables).To(Equal(expectedTables), "second Up() should recreate all tables")
			verifySeedData(suiteT, ctx, connStr)
		})
	})

	Describe("DirtyStateRecovery", func() {
		It("detects dirty state and recovers via Force()", func() {
			ctx := context.Background()

			connStr := testutil.RawDatabase(suiteT, sharedPG)

			// Create migrator and apply migrations
			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			Expect(migrator.Up()).To(Succeed())

			// Capture current version before simulating dirty state
			currentVersion, dirty, err := migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(dirty).To(BeFalse(), "database should start clean")
			Expect(currentVersion).To(BeNumerically(">", uint(0)), "should have at least one migration applied")

			// Simulate dirty state using raw SQL
			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			_, err = conn.Exec(ctx, "UPDATE schema_migrations SET dirty = true")
			Expect(err).NotTo(HaveOccurred())

			// Verify dirty state is reflected
			version, dirty, err := migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(currentVersion))
			Expect(dirty).To(BeTrue(), "database should be dirty after manual update")

			// Verify Up() fails when database is dirty
			// golang-migrate returns ErrDirty when trying to run migrations on a dirty database
			err = migrator.Up()
			Expect(err).To(HaveOccurred(), "Up() should fail when database is dirty")
			Expect(err.Error()).To(ContainSubstring("Dirty"), "error should indicate dirty state")

			// Verify Steps() also fails when dirty
			err = migrator.Steps(1)
			Expect(err).To(HaveOccurred(), "Steps() should fail when database is dirty")
			Expect(err.Error()).To(ContainSubstring("Dirty"), "error should indicate dirty state")

			// Use Force() to recover from dirty state
			Expect(migrator.Force(int(currentVersion))).To(Succeed(), "Force() should succeed and clear dirty flag")

			// Verify dirty flag is cleared
			version, dirty, err = migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(currentVersion), "version should remain unchanged after Force()")
			Expect(dirty).To(BeFalse(), "dirty flag should be cleared after Force()")

			// Verify migrations can continue - Up() should succeed (or return no change)
			Expect(migrator.Up()).To(Succeed(), "Up() should succeed after Force() clears dirty state")
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

	// Force_VersionExceedsAvailable documents the behavior when Force() is called
	// with a version higher than available migrations.
	//
	// While Force() accepts any version value, the consequences are:
	//   - Version() returns the forced version (999)
	//   - Up() returns an error (migration file not found)
	//   - PendingMigrations() returns empty (thinks we're past all migrations)
	//
	// This is dangerous because PendingMigrations() indicates nothing is pending,
	// but Up() will actually fail. The only recovery is to Force() back to a valid version.
	//
	// Force() should only be used for recovery from dirty state, never to
	// artificially advance the version.
	Describe("Force_VersionExceedsAvailable", func() {
		It("documents dangerous behavior: PendingMigrations() returns empty but Up() fails", func() {
			connStr := testutil.RawDatabase(suiteT, sharedPG)

			// Create migrator
			migrator, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator.Close()

			// Apply all available migrations first
			Expect(migrator.Up()).To(Succeed())

			// Verify we're at the latest version
			latestVersion, dirty, err := migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(dirty).To(BeFalse())
			Expect(latestVersion).To(BeNumerically(">", uint(0)), "should have at least one migration")

			// DANGEROUS: Force version to a value higher than any available migration
			// This should NOT be done in production - only for documenting behavior
			Expect(migrator.Force(999)).To(Succeed(), "Force() accepts any version, even non-existent ones")

			// Verify version is now 999
			version, dirty, err := migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(999)), "Force() sets version to arbitrary value")
			Expect(dirty).To(BeFalse(), "Force() clears dirty flag")

			// Up() returns an error because there's no migration file for version 999
			// golang-migrate tries to read the current version's down migration and fails
			err = migrator.Up()
			Expect(err).To(HaveOccurred(), "Up() should fail when forced to non-existent version")
			Expect(err.Error()).To(ContainSubstring("999"),
				"error message should reference the invalid version")

			// PendingMigrations() returns empty because version 999 > all migrations
			// This is misleading since Up() will actually fail
			pending, err := migrator.PendingMigrations()
			Expect(err).NotTo(HaveOccurred())
			Expect(pending).To(BeEmpty(), "PendingMigrations() returns empty when version exceeds all migrations")

			// Version remains 999
			version, _, err = migrator.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(999)), "version unchanged after failed Up()")
		})
	})

	// ConcurrentMigrationDirtyStateHandling verifies that dirty state
	// detection works correctly when multiple migrators access the same database
	// and one experiences a partial failure (simulated via dirty flag).
	//
	// This test simulates the scenario where:
	// 1. Two migrators point to the same database
	// 2. A migration fails partway through (leaving dirty state)
	// 3. Both migrators detect the dirty state
	// 4. Force() can recover the database to a clean state
	Describe("ConcurrentMigrationDirtyStateHandling", func() {
		It("both migrators detect dirty state and recover via Force()", func() {
			ctx := context.Background()

			connStr := testutil.RawDatabase(suiteT, sharedPG)

			// Apply migrations first to establish baseline
			migrator1, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator1.Close()

			Expect(migrator1.Up()).To(Succeed())

			baseVersion, dirty, err := migrator1.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(dirty).To(BeFalse(), "database should start clean")
			Expect(baseVersion).To(BeNumerically(">", uint(0)), "should have at least one migration applied")

			// Roll back to version 0 so we can simulate concurrent migration attempts
			Expect(migrator1.Down()).To(Succeed())

			version, dirty, err := migrator1.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(version).To(Equal(uint(0)), "should be at version 0 after Down()")
			Expect(dirty).To(BeFalse())

			// Now simulate a concurrent scenario where one migrator sets dirty state
			// mid-execution (as would happen if a migration fails partway through).
			// We'll:
			// 1. Start migrator2
			// 2. Manually set dirty state (simulating mid-migration crash)
			// 3. Verify both migrators detect the dirty state
			// 4. Verify Force() recovery works

			migrator2, err := store.NewMigrator(connStr)
			Expect(err).NotTo(HaveOccurred())
			defer migrator2.Close()

			// First, apply one migration step to create the schema_migrations table
			// and establish a version for dirty state simulation
			Expect(migrator1.Steps(1)).To(Succeed())

			currentVersion, dirty, err := migrator1.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(dirty).To(BeFalse())
			Expect(currentVersion).To(BeNumerically(">", uint(0)))

			// Simulate a partial migration failure by setting dirty flag directly.
			// This simulates what happens when a migration crashes mid-execution:
			// the database is left in a dirty state at the current version.
			conn, err := pgx.Connect(ctx, connStr)
			Expect(err).NotTo(HaveOccurred())
			defer conn.Close(ctx)

			_, err = conn.Exec(ctx, "UPDATE schema_migrations SET dirty = true")
			Expect(err).NotTo(HaveOccurred())

			// Test: Both migrators detect the dirty state
			v1, dirty1, err := migrator1.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(v1).To(Equal(currentVersion), "migrator1 should report correct version")
			Expect(dirty1).To(BeTrue(), "migrator1 should detect dirty state")

			v2, dirty2, err := migrator2.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(v2).To(Equal(currentVersion), "migrator2 should report correct version")
			Expect(dirty2).To(BeTrue(), "migrator2 should detect dirty state")

			// Test: Up() fails for both migrators due to dirty state
			err = migrator1.Up()
			Expect(err).To(HaveOccurred(), "migrator1.Up() should fail when database is dirty")
			Expect(err.Error()).To(ContainSubstring("Dirty"), "error should indicate dirty state")

			err = migrator2.Up()
			Expect(err).To(HaveOccurred(), "migrator2.Up() should fail when database is dirty")
			Expect(err.Error()).To(ContainSubstring("Dirty"), "error should indicate dirty state")

			// Test: Steps() fails for both migrators due to dirty state
			err = migrator1.Steps(1)
			Expect(err).To(HaveOccurred(), "migrator1.Steps() should fail when database is dirty")
			Expect(err.Error()).To(ContainSubstring("Dirty"), "error should indicate dirty state")

			err = migrator2.Steps(1)
			Expect(err).To(HaveOccurred(), "migrator2.Steps() should fail when database is dirty")
			Expect(err.Error()).To(ContainSubstring("Dirty"), "error should indicate dirty state")

			// Test: Force() can recover from dirty state
			// Using migrator1 to force the version clears dirty flag
			Expect(migrator1.Force(int(currentVersion))).To(Succeed(), "migrator1.Force() should succeed")

			// Test: Both migrators now see clean state
			v1, dirty1, err = migrator1.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(v1).To(Equal(currentVersion))
			Expect(dirty1).To(BeFalse(), "migrator1 should see dirty flag cleared after Force()")

			v2, dirty2, err = migrator2.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(v2).To(Equal(currentVersion))
			Expect(dirty2).To(BeFalse(), "migrator2 should see dirty flag cleared after Force()")

			// Test: Migrations can continue after Force() recovery
			Expect(migrator1.Up()).To(Succeed(), "migrator1.Up() should succeed after Force() recovery")

			// Verify final state
			finalVersion, dirty, err := migrator1.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(finalVersion).To(Equal(baseVersion), "should reach final version after recovery")
			Expect(dirty).To(BeFalse(), "database should be clean after recovery and Up()")

			// migrator2 should also see the consistent final state
			v2Final, dirty2Final, err := migrator2.Version()
			Expect(err).NotTo(HaveOccurred())
			Expect(v2Final).To(Equal(finalVersion), "migrator2 should see same final version")
			Expect(dirty2Final).To(BeFalse(), "migrator2 should see clean state")
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
