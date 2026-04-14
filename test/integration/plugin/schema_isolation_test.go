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
	"github.com/stretchr/testify/assert"
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
	shared := testutil.SharedPostgres(t)
	adminConnStr = testutil.RawDatabase(t, shared)

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

func TestSchemaProvisionerInitFailsWithoutCreaterole(t *testing.T) {
	ctx := context.Background()

	_, adminConnStr := setupSchemaTestDB(t)

	// Create a restricted role WITHOUT CREATEROLE.
	adminConn, err := pgx.Connect(ctx, adminConnStr)
	require.NoError(t, err)
	defer adminConn.Close(ctx)

	_, err = adminConn.Exec(ctx, "CREATE ROLE restricted LOGIN PASSWORD 'restricted'")
	require.NoError(t, err)
	adminConn.Close(ctx)

	restrictedConnStr := replaceUser(t, adminConnStr, "restricted", "restricted")

	sp := plugins.NewSchemaProvisioner(restrictedConnStr)
	defer sp.Close()

	err = sp.Init(ctx)
	require.Error(t, err)
	errutil.AssertErrorCode(t, err, "SCHEMA_INSUFFICIENT_PRIVILEGES")
}

func TestSchemaProvisionerInitSucceedsWithCreaterole(t *testing.T) {
	ctx := context.Background()

	holomushConnStr, _ := setupSchemaTestDB(t)

	sp := plugins.NewSchemaProvisioner(holomushConnStr)
	defer sp.Close()

	err := sp.Init(ctx)
	require.NoError(t, err)
}

func TestProvisionSchemaCreatesRoleAndSchema(t *testing.T) {
	ctx := context.Background()

	holomushConnStr, _ := setupSchemaTestDB(t)

	sp := plugins.NewSchemaProvisioner(holomushConnStr)
	defer sp.Close()
	require.NoError(t, sp.Init(ctx))

	_, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	// Verify role properties.
	pool, err := pgxpool.New(ctx, holomushConnStr)
	require.NoError(t, err)
	defer pool.Close()

	var canLogin, isSuperuser, canCreaterole bool
	err = pool.QueryRow(ctx,
		"SELECT rolcanlogin, rolsuper, rolcreaterole FROM pg_roles WHERE rolname = $1",
		"holomush_plugin_test_plugin",
	).Scan(&canLogin, &isSuperuser, &canCreaterole)
	require.NoError(t, err)
	assert.True(t, canLogin, "plugin role should have LOGIN")
	assert.False(t, isSuperuser, "plugin role must not be superuser")
	assert.False(t, canCreaterole, "plugin role must not have CREATEROLE")

	// Verify schema ownership.
	var schemaOwner string
	err = pool.QueryRow(ctx, `
		SELECT r.rolname
		FROM pg_namespace n
		JOIN pg_roles r ON n.nspowner = r.oid
		WHERE n.nspname = $1`,
		"plugin_test_plugin",
	).Scan(&schemaOwner)
	require.NoError(t, err)
	assert.Equal(t, "holomush_plugin_test_plugin", schemaOwner)
}

func TestPluginRoleCanCreateTablesInOwnSchema(t *testing.T) {
	ctx := context.Background()

	holomushConnStr, _ := setupSchemaTestDB(t)

	sp := plugins.NewSchemaProvisioner(holomushConnStr)
	defer sp.Close()
	require.NoError(t, sp.Init(ctx))

	connStr, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	// Connect as the plugin role.
	pluginConn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer pluginConn.Close(ctx)

	_, err = pluginConn.Exec(ctx, "CREATE TABLE items (id serial PRIMARY KEY, name text)")
	require.NoError(t, err)

	_, err = pluginConn.Exec(ctx, "INSERT INTO items (name) VALUES ('sword')")
	require.NoError(t, err)

	var name string
	err = pluginConn.QueryRow(ctx, "SELECT name FROM items WHERE name = 'sword'").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "sword", name)
}

func TestPluginRoleCannotAccessPublicSchema(t *testing.T) {
	ctx := context.Background()

	holomushConnStr, _ := setupSchemaTestDB(t)

	// Create a table in public schema as holomush.
	adminConn, err := pgx.Connect(ctx, holomushConnStr)
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE TABLE public.secrets (id serial, data text)")
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "INSERT INTO public.secrets (data) VALUES ('top-secret')")
	require.NoError(t, err)
	adminConn.Close(ctx)

	sp := plugins.NewSchemaProvisioner(holomushConnStr)
	defer sp.Close()
	require.NoError(t, sp.Init(ctx))

	connStr, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	pluginConn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer pluginConn.Close(ctx)

	_, err = pluginConn.Exec(ctx, "SET search_path TO public")
	require.NoError(t, err)

	_, err = pluginConn.Exec(ctx, "SELECT * FROM secrets")
	require.Error(t, err, "plugin must not be able to read public schema tables")
}

func TestCrossPluginIsolation(t *testing.T) {
	ctx := context.Background()

	holomushConnStr, _ := setupSchemaTestDB(t)

	sp := plugins.NewSchemaProvisioner(holomushConnStr)
	defer sp.Close()
	require.NoError(t, sp.Init(ctx))

	connStrA, err := sp.ProvisionSchema(ctx, "plugin-a")
	require.NoError(t, err)

	connStrB, err := sp.ProvisionSchema(ctx, "plugin-b")
	require.NoError(t, err)

	// Plugin A creates a table and inserts data.
	connA, err := pgx.Connect(ctx, connStrA)
	require.NoError(t, err)
	defer connA.Close(ctx)

	_, err = connA.Exec(ctx, "CREATE TABLE treasure (id serial, loot text)")
	require.NoError(t, err)
	_, err = connA.Exec(ctx, "INSERT INTO treasure (loot) VALUES ('gold')")
	require.NoError(t, err)

	// Plugin B tries to access Plugin A's schema.
	connB, err := pgx.Connect(ctx, connStrB)
	require.NoError(t, err)
	defer connB.Close(ctx)

	_, err = connB.Exec(ctx, "SET search_path TO plugin_plugin_a")
	require.NoError(t, err)

	_, err = connB.Exec(ctx, "SELECT * FROM treasure")
	require.Error(t, err, "plugin B must not be able to read plugin A's tables")
}

func TestIdempotentProvisionRefreshesPassword(t *testing.T) {
	ctx := context.Background()

	holomushConnStr, _ := setupSchemaTestDB(t)

	sp := plugins.NewSchemaProvisioner(holomushConnStr)
	defer sp.Close()
	require.NoError(t, sp.Init(ctx))

	// First provision — create table.
	connStr1, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	conn1, err := pgx.Connect(ctx, connStr1)
	require.NoError(t, err)
	_, err = conn1.Exec(ctx, "CREATE TABLE settings (key text PRIMARY KEY, val text)")
	require.NoError(t, err)
	_, err = conn1.Exec(ctx, "INSERT INTO settings (key, val) VALUES ('color', 'blue')")
	require.NoError(t, err)
	conn1.Close(ctx)

	// Second provision — password refreshed.
	connStr2, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	// New credentials work and table persists.
	conn2, err := pgx.Connect(ctx, connStr2)
	require.NoError(t, err)
	defer conn2.Close(ctx)

	var val string
	err = conn2.QueryRow(ctx, "SELECT val FROM settings WHERE key = 'color'").Scan(&val)
	require.NoError(t, err)
	assert.Equal(t, "blue", val)

	// Old credentials must fail.
	_, err = pgx.Connect(ctx, connStr1)
	assert.Error(t, err, "old connection string must fail after password refresh")
}

func TestPurgeSchemaRemovesRoleAndSchema(t *testing.T) {
	ctx := context.Background()

	holomushConnStr, _ := setupSchemaTestDB(t)

	sp := plugins.NewSchemaProvisioner(holomushConnStr)
	defer sp.Close()
	require.NoError(t, sp.Init(ctx))

	connStr, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	// Create a table to prove data exists.
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	_, err = conn.Exec(ctx, "CREATE TABLE ephemeral (id serial)")
	require.NoError(t, err)
	conn.Close(ctx)

	// Purge.
	err = sp.PurgeSchema(ctx, "test-plugin")
	require.NoError(t, err)

	// Verify role is gone.
	pool, err := pgxpool.New(ctx, holomushConnStr)
	require.NoError(t, err)
	defer pool.Close()

	var roleExists bool
	err = pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)",
		"holomush_plugin_test_plugin",
	).Scan(&roleExists)
	require.NoError(t, err)
	assert.False(t, roleExists, "role must be removed after purge")

	// Verify schema is gone.
	var schemaExists bool
	err = pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)",
		"plugin_test_plugin",
	).Scan(&schemaExists)
	require.NoError(t, err)
	assert.False(t, schemaExists, "schema must be removed after purge")
}
