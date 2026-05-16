// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/onsi/ginkgo/v2" //nolint:revive // ginkgo convention
	. "github.com/onsi/gomega"    //nolint:revive // gomega convention
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/pkg/errutil"
	"github.com/holomush/holomush/test/testutil"
)

func replaceUser(t *testing.T, connStr, user, password string) string {
	t.Helper()
	u, err := url.Parse(connStr)
	require.NoError(t, err)
	u.User = url.UserPassword(user, password)
	return u.String()
}

// setupSchemaTestDB creates a raw database with holomush role access,
// matching production StartPostgres setup. Returns holomush-role and
// superuser connection strings.
func setupSchemaTestDB(t *testing.T) (holomushConnStr, adminConnStr string) {
	t.Helper()
	adminConnStr = testutil.RawDatabase(t, sharedPG)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminConnStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	// The holomush role already exists at the cluster level from SharedPostgres.
	// Grant it access to this specific database and own the public schema.
	dbName := extractDBName(t, adminConnStr)
	_, err = conn.Exec(ctx, ddlGrantToHolomush(dbName))
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "ALTER SCHEMA public OWNER TO holomush")
	require.NoError(t, err)

	holomushConnStr = replaceUser(t, adminConnStr, "holomush", "holomush")
	return holomushConnStr, adminConnStr
}

func extractDBName(t *testing.T, connStr string) string {
	t.Helper()
	u, err := url.Parse(connStr)
	require.NoError(t, err)
	return strings.TrimPrefix(u.Path, "/")
}

// ddlGrantToHolomush builds a GRANT DDL statement. DDL does not support
// parameterized queries; dbName is hex-encoded crypto/rand output from
// randomDBName(), not user input.
func ddlGrantToHolomush(dbName string) string {
	return fmt.Sprintf("GRANT ALL ON DATABASE %s TO holomush", dbName)
}

var _ = Describe("Schema isolation: SchemaProvisioner role + schema lifecycle", func() {
	It("Init fails without CREATEROLE", func() {
		ctx := context.Background()

		_, adminConnStr := setupSchemaTestDB(suiteT)

		// Create a restricted role WITHOUT CREATEROLE.
		adminConn, err := pgx.Connect(ctx, adminConnStr)
		Expect(err).NotTo(HaveOccurred())
		defer adminConn.Close(ctx)

		// Defensive: drop any stale role from a prior run. Postgres roles
		// are cluster-level, and a testcontainers reuse-mode container
		// would otherwise carry this role across `task test:int` runs and
		// fail the CREATE with "role already exists".
		_, err = adminConn.Exec(ctx, "DROP ROLE IF EXISTS restricted")
		Expect(err).NotTo(HaveOccurred())
		_, err = adminConn.Exec(ctx, "CREATE ROLE restricted LOGIN PASSWORD 'restricted'")
		Expect(err).NotTo(HaveOccurred())
		adminConn.Close(ctx)

		restrictedConnStr := replaceUser(suiteT, adminConnStr, "restricted", "restricted")

		sp := plugins.NewSchemaProvisioner(restrictedConnStr)
		defer sp.Close()

		err = sp.Init(ctx)
		Expect(err).To(HaveOccurred())
		errutil.AssertErrorCode(suiteT, err, "SCHEMA_INSUFFICIENT_PRIVILEGES")
	})

	It("Init succeeds with CREATEROLE", func() {
		ctx := context.Background()

		holomushConnStr, _ := setupSchemaTestDB(suiteT)

		sp := plugins.NewSchemaProvisioner(holomushConnStr)
		defer sp.Close()

		Expect(sp.Init(ctx)).NotTo(HaveOccurred())
	})

	It("ProvisionSchema creates role and schema with expected properties", func() {
		ctx := context.Background()

		holomushConnStr, _ := setupSchemaTestDB(suiteT)

		sp := plugins.NewSchemaProvisioner(holomushConnStr)
		defer sp.Close()
		Expect(sp.Init(ctx)).NotTo(HaveOccurred())

		_, err := sp.ProvisionSchema(ctx, "test-plugin")
		Expect(err).NotTo(HaveOccurred())

		// Verify role properties.
		pool, err := pgxpool.New(ctx, holomushConnStr)
		Expect(err).NotTo(HaveOccurred())
		defer pool.Close()

		var canLogin, isSuperuser, canCreaterole bool
		err = pool.QueryRow(
			ctx,
			"SELECT rolcanlogin, rolsuper, rolcreaterole FROM pg_roles WHERE rolname = $1",
			"holomush_plugin_test_plugin",
		).Scan(&canLogin, &isSuperuser, &canCreaterole)
		Expect(err).NotTo(HaveOccurred())
		Expect(canLogin).To(BeTrue(), "plugin role should have LOGIN")
		Expect(isSuperuser).To(BeFalse(), "plugin role must not be superuser")
		Expect(canCreaterole).To(BeFalse(), "plugin role must not have CREATEROLE")

		// Verify schema ownership.
		var schemaOwner string
		err = pool.QueryRow(
			ctx, `
			SELECT r.rolname
			FROM pg_namespace n
			JOIN pg_roles r ON n.nspowner = r.oid
			WHERE n.nspname = $1`,
			"plugin_test_plugin",
		).Scan(&schemaOwner)
		Expect(err).NotTo(HaveOccurred())
		Expect(schemaOwner).To(Equal("holomush_plugin_test_plugin"))
	})

	It("plugin role can create tables in its own schema", func() {
		ctx := context.Background()

		holomushConnStr, _ := setupSchemaTestDB(suiteT)

		sp := plugins.NewSchemaProvisioner(holomushConnStr)
		defer sp.Close()
		Expect(sp.Init(ctx)).NotTo(HaveOccurred())

		connStr, err := sp.ProvisionSchema(ctx, "test-plugin")
		Expect(err).NotTo(HaveOccurred())

		// Connect as the plugin role.
		pluginConn, err := pgx.Connect(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		defer pluginConn.Close(ctx)

		_, err = pluginConn.Exec(ctx, "CREATE TABLE items (id serial PRIMARY KEY, name text)")
		Expect(err).NotTo(HaveOccurred())

		_, err = pluginConn.Exec(ctx, "INSERT INTO items (name) VALUES ('sword')")
		Expect(err).NotTo(HaveOccurred())

		var name string
		err = pluginConn.QueryRow(ctx, "SELECT name FROM items WHERE name = 'sword'").Scan(&name)
		Expect(err).NotTo(HaveOccurred())
		Expect(name).To(Equal("sword"))
	})

	It("plugin role cannot access public schema", func() {
		ctx := context.Background()

		holomushConnStr, _ := setupSchemaTestDB(suiteT)

		// Create a table in public schema as holomush.
		adminConn, err := pgx.Connect(ctx, holomushConnStr)
		Expect(err).NotTo(HaveOccurred())
		_, err = adminConn.Exec(ctx, "CREATE TABLE public.secrets (id serial, data text)")
		Expect(err).NotTo(HaveOccurred())
		_, err = adminConn.Exec(ctx, "INSERT INTO public.secrets (data) VALUES ('top-secret')")
		Expect(err).NotTo(HaveOccurred())
		adminConn.Close(ctx)

		sp := plugins.NewSchemaProvisioner(holomushConnStr)
		defer sp.Close()
		Expect(sp.Init(ctx)).NotTo(HaveOccurred())

		connStr, err := sp.ProvisionSchema(ctx, "test-plugin")
		Expect(err).NotTo(HaveOccurred())

		pluginConn, err := pgx.Connect(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		defer pluginConn.Close(ctx)

		_, err = pluginConn.Exec(ctx, "SET search_path TO public")
		Expect(err).NotTo(HaveOccurred())

		_, err = pluginConn.Exec(ctx, "SELECT * FROM secrets")
		Expect(err).To(HaveOccurred(), "plugin must not be able to read public schema tables")
	})

	It("plugins are isolated from each other's schemas", func() {
		ctx := context.Background()

		holomushConnStr, _ := setupSchemaTestDB(suiteT)

		sp := plugins.NewSchemaProvisioner(holomushConnStr)
		defer sp.Close()
		Expect(sp.Init(ctx)).NotTo(HaveOccurred())

		connStrA, err := sp.ProvisionSchema(ctx, "plugin-a")
		Expect(err).NotTo(HaveOccurred())

		connStrB, err := sp.ProvisionSchema(ctx, "plugin-b")
		Expect(err).NotTo(HaveOccurred())

		// Plugin A creates a table and inserts data.
		connA, err := pgx.Connect(ctx, connStrA)
		Expect(err).NotTo(HaveOccurred())
		defer connA.Close(ctx)

		_, err = connA.Exec(ctx, "CREATE TABLE treasure (id serial, loot text)")
		Expect(err).NotTo(HaveOccurred())
		_, err = connA.Exec(ctx, "INSERT INTO treasure (loot) VALUES ('gold')")
		Expect(err).NotTo(HaveOccurred())

		// Plugin B tries to access Plugin A's schema.
		connB, err := pgx.Connect(ctx, connStrB)
		Expect(err).NotTo(HaveOccurred())
		defer connB.Close(ctx)

		_, err = connB.Exec(ctx, "SET search_path TO plugin_plugin_a")
		Expect(err).NotTo(HaveOccurred())

		_, err = connB.Exec(ctx, "SELECT * FROM treasure")
		Expect(err).To(HaveOccurred(), "plugin B must not be able to read plugin A's tables")
	})

	It("idempotent provision refreshes password and preserves data", func() {
		ctx := context.Background()

		holomushConnStr, _ := setupSchemaTestDB(suiteT)

		sp := plugins.NewSchemaProvisioner(holomushConnStr)
		defer sp.Close()
		Expect(sp.Init(ctx)).NotTo(HaveOccurred())

		// First provision — create table.
		connStr1, err := sp.ProvisionSchema(ctx, "test-plugin")
		Expect(err).NotTo(HaveOccurred())

		conn1, err := pgx.Connect(ctx, connStr1)
		Expect(err).NotTo(HaveOccurred())
		_, err = conn1.Exec(ctx, "CREATE TABLE settings (key text PRIMARY KEY, val text)")
		Expect(err).NotTo(HaveOccurred())
		_, err = conn1.Exec(ctx, "INSERT INTO settings (key, val) VALUES ('color', 'blue')")
		Expect(err).NotTo(HaveOccurred())
		conn1.Close(ctx)

		// Second provision — password refreshed.
		connStr2, err := sp.ProvisionSchema(ctx, "test-plugin")
		Expect(err).NotTo(HaveOccurred())

		// New credentials work and table persists.
		conn2, err := pgx.Connect(ctx, connStr2)
		Expect(err).NotTo(HaveOccurred())
		defer conn2.Close(ctx)

		var val string
		err = conn2.QueryRow(ctx, "SELECT val FROM settings WHERE key = 'color'").Scan(&val)
		Expect(err).NotTo(HaveOccurred())
		Expect(val).To(Equal("blue"))

		// Old credentials must fail.
		_, err = pgx.Connect(ctx, connStr1)
		Expect(err).To(HaveOccurred(), "old connection string must fail after password refresh")
	})

	It("PurgeSchema removes role and schema", func() {
		// Use a unique plugin name. Postgres roles are cluster-level
		// (not database-level), so roles created by prior specs in this
		// same suite (e.g., "test-plugin" from the ProvisionSchema and
		// Idempotent specs above) own objects in their throwaway DBs,
		// which blocks the role drop with "cannot be dropped because
		// some objects depend on it (SQLSTATE 2BP01)". The original
		// per-Test* model isolated this because each Go test recreated
		// the suite context; Ginkgo shares state across specs in one
		// Describe. Unique-per-spec name sidesteps the cross-spec
		// dependency without weakening the assertion.
		const pluginName = "test-plugin-purge"
		const roleName = "holomush_plugin_test_plugin_purge"
		const schemaName = "plugin_test_plugin_purge"

		ctx := context.Background()

		holomushConnStr, _ := setupSchemaTestDB(suiteT)

		sp := plugins.NewSchemaProvisioner(holomushConnStr)
		defer sp.Close()
		Expect(sp.Init(ctx)).NotTo(HaveOccurred())

		connStr, err := sp.ProvisionSchema(ctx, pluginName)
		Expect(err).NotTo(HaveOccurred())

		// Create a table to prove data exists.
		conn, err := pgx.Connect(ctx, connStr)
		Expect(err).NotTo(HaveOccurred())
		_, err = conn.Exec(ctx, "CREATE TABLE ephemeral (id serial)")
		Expect(err).NotTo(HaveOccurred())
		conn.Close(ctx)

		// Purge.
		Expect(sp.PurgeSchema(ctx, pluginName)).NotTo(HaveOccurred())

		// Verify role is gone.
		pool, err := pgxpool.New(ctx, holomushConnStr)
		Expect(err).NotTo(HaveOccurred())
		defer pool.Close()

		var roleExists bool
		err = pool.QueryRow(
			ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)",
			roleName,
		).Scan(&roleExists)
		Expect(err).NotTo(HaveOccurred())
		Expect(roleExists).To(BeFalse(), "role must be removed after purge")

		// Verify schema is gone.
		var schemaExists bool
		err = pool.QueryRow(
			ctx,
			"SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)",
			schemaName,
		).Scan(&schemaExists)
		Expect(err).NotTo(HaveOccurred())
		Expect(schemaExists).To(BeFalse(), "schema must be removed after purge")
	})
})
