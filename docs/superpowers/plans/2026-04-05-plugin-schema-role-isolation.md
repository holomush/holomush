<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin Schema Role Isolation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add per-plugin PostgreSQL roles so each plugin can only access its own schema, closing the privilege escalation vulnerability in SchemaProvisioner.

**Architecture:** SchemaProvisioner gains role lifecycle management (create/refresh/drop) alongside its existing schema management. Ephemeral passwords are regenerated on every server start. Test infrastructure switches from superuser to a CREATEROLE role matching production.

**Tech Stack:** Go, PostgreSQL 18, pgx/v5, crypto/rand, testcontainers-go, Ginkgo/Gomega

**Spec:** `docs/superpowers/specs/2026-04-05-plugin-schema-role-isolation-design.md`

---

## Task 1: Docker Init Script and Compose Changes

**Files:**

- Create: `docker/postgres/init-role.sh`
- Modify: `compose.yaml:8-19` (postgres service)

- [ ] **Step 1: Create the init script**

```bash
#!/bin/bash
# SPDX-License-Identifier: Apache-2.0
# Copyright 2026 HoloMUSH Contributors
#
# Creates the holomush application role with CREATEROLE privilege.
# Runs as the postgres superuser during container first start only.
# Re-runs require: docker compose down -v

set -euo pipefail

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<EOSQL
-- Application role: not superuser, but can create roles for plugin isolation
CREATE ROLE holomush LOGIN PASSWORD 'holomush' CREATEROLE;

-- Transfer database and schema ownership to application role
ALTER DATABASE $POSTGRES_DB OWNER TO holomush;
ALTER SCHEMA public OWNER TO holomush;
EOSQL
```

Write to `docker/postgres/init-role.sh` and `chmod +x`.

- [ ] **Step 2: Make the init script executable**

Run: `chmod +x docker/postgres/init-role.sh`

- [ ] **Step 3: Update compose.yaml postgres service**

Replace the postgres service environment and add the volume mount:

```yaml
  postgres:
    image: postgres:18-alpine
    environment:
      POSTGRES_PASSWORD: postgres
      POSTGRES_DB: holomush
    volumes:
      - ./docker/postgres/init-role.sh:/docker-entrypoint-initdb.d/01-init-role.sh
      - pgdata:/var/lib/postgresql/data
    networks:
      - backend
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U holomush"]
      interval: 2s
      timeout: 5s
      retries: 10
```

Key changes: removed `POSTGRES_USER: holomush` (defaults to `postgres` superuser), changed `POSTGRES_PASSWORD` to `postgres`, added init script volume mount.

- [ ] **Step 4: Verify compose starts correctly**

Run: `docker compose down -v && docker compose up -d postgres && sleep 3 && docker compose exec postgres psql -U holomush -d holomush -c "SELECT current_user, rolcreaterole FROM pg_roles WHERE rolname = 'holomush'"`

Expected: `holomush | t`

Run: `docker compose down -v`

- [ ] **Step 5: Commit**

```text
jj commit -m "infra(postgres): switch compose to non-superuser holomush role with CREATEROLE

Add docker/postgres/init-role.sh that creates the holomush application
role with CREATEROLE privilege during container initialization. The
postgres superuser is used only for bootstrap; the application connects
as holomush with restricted privileges matching production deployment.

Existing developers: run 'docker compose down -v' to reinitialize."
```

---

## Task 2: Shared Test Helper

**Files:**

- Create: `test/testutil/postgres.go`

- [ ] **Step 1: Create test/testutil directory**

Run: `mkdir -p test/testutil`

- [ ] **Step 2: Write the shared helper**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Package testutil provides shared test infrastructure for integration tests.
package testutil

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// PostgresEnv holds a running PostgreSQL container with a non-superuser
// holomush role that has CREATEROLE privilege, matching production deployment.
type PostgresEnv struct {
	Container testcontainers.Container
	ConnStr   string
}

// Terminate stops and removes the PostgreSQL container.
func (e *PostgresEnv) Terminate(ctx context.Context) error {
	if e.Container != nil {
		return e.Container.Terminate(ctx)
	}
	return nil
}

// StartPostgres starts a PostgreSQL 18 container with a non-superuser
// holomush role (LOGIN, CREATEROLE). The returned ConnStr uses
// holomush:holomush credentials against the holomush_test database.
//
// Callers are responsible for running migrations via store.NewMigrator.
func StartPostgres(ctx context.Context) (*PostgresEnv, error) {
	container, err := postgres.Run(ctx,
		"postgres:18-alpine",
		postgres.WithDatabase("holomush_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("start postgres container: %w", err)
	}

	adminConnStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("get admin connection string: %w", err)
	}

	if err := initHolomushRole(ctx, adminConnStr); err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("init holomush role: %w", err)
	}

	connStr, err := replaceCredentials(adminConnStr, "holomush", "holomush")
	if err != nil {
		_ = container.Terminate(ctx)
		return nil, fmt.Errorf("build holomush connection string: %w", err)
	}

	return &PostgresEnv{Container: container, ConnStr: connStr}, nil
}

func initHolomushRole(ctx context.Context, adminConnStr string) error {
	conn, err := pgx.Connect(ctx, adminConnStr)
	if err != nil {
		return fmt.Errorf("connect as superuser: %w", err)
	}
	defer conn.Close(ctx)

	stmts := []string{
		"CREATE ROLE holomush LOGIN PASSWORD 'holomush' CREATEROLE",
		"ALTER SCHEMA public OWNER TO holomush",
		"GRANT ALL ON DATABASE holomush_test TO holomush",
	}
	for _, stmt := range stmts {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt, err)
		}
	}
	return nil
}

func replaceCredentials(connStr, user, password string) (string, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword(user, password)
	return u.String(), nil
}
```

- [ ] **Step 3: Verify it compiles**

Run: `task build`

- [ ] **Step 4: Commit**

```text
jj commit -m "test(testutil): add shared PostgreSQL helper with non-superuser role

StartPostgres() starts a testcontainer with a postgres superuser for
bootstrap, then creates a holomush role with CREATEROLE privilege.
Returns connection string using holomush credentials, matching the
production privilege model."
```

---

## Task 3: New Pure Helper Functions (TDD)

**Files:**

- Modify: `internal/plugin/schema_provisioner.go`
- Modify: `internal/plugin/schema_provisioner_test.go`

- [ ] **Step 1: Write failing tests for pluginRoleName**

Add to `internal/plugin/schema_provisioner_test.go`:

```go
func TestPluginRoleName(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"prepends holomush_plugin prefix", "scenes", "holomush_plugin_scenes"},
		{"converts hyphens to underscores", "core-scenes", "holomush_plugin_core_scenes"},
		{"handles multiple hyphens", "my-cool-plugin", "holomush_plugin_my_cool_plugin"},
		{"preserves underscores", "core_utils", "holomush_plugin_core_utils"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, pluginRoleName(tt.input))
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `task test -- -run TestPluginRoleName ./internal/plugin/`
Expected: FAIL — `pluginRoleName` undefined

- [ ] **Step 3: Implement pluginRoleName**

Add to `internal/plugin/schema_provisioner.go`:

```go
// pluginRoleName converts a plugin name to a PostgreSQL role name.
// Uses the same sanitization as pluginSchemaName but with the
// "holomush_plugin_" prefix for role namespace isolation.
func pluginRoleName(name string) string {
	return "holomush_plugin_" + strings.ReplaceAll(name, "-", "_")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `task test -- -run TestPluginRoleName ./internal/plugin/`
Expected: PASS

- [ ] **Step 5: Write failing test for generatePassword**

Add to `internal/plugin/schema_provisioner_test.go`:

```go
func TestGeneratePasswordProduces256BitsOfEntropy(t *testing.T) {
	pw, err := generatePassword()
	require.NoError(t, err)

	// 32 bytes base64url-encoded = 43 chars (no padding)
	assert.Len(t, pw, 43)
}

func TestGeneratePasswordIsUnique(t *testing.T) {
	pw1, err := generatePassword()
	require.NoError(t, err)
	pw2, err := generatePassword()
	require.NoError(t, err)

	assert.NotEqual(t, pw1, pw2)
}
```

Add `"encoding/base64"` to the test imports if not present (not needed — just checking length and uniqueness).

- [ ] **Step 6: Run test to verify it fails**

Run: `task test -- -run TestGeneratePassword ./internal/plugin/`
Expected: FAIL — `generatePassword` undefined

- [ ] **Step 7: Implement generatePassword**

Add to `internal/plugin/schema_provisioner.go`:

```go
// generatePassword returns 32 bytes (256 bits) of cryptographic randomness,
// base64url-encoded without padding. The charset (A-Za-z0-9-_) is safe for
// inclusion in PostgreSQL password literals without escaping.
func generatePassword() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", oops.Code("PASSWORD_GENERATION_FAILED").Wrap(err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
```

Add `"crypto/rand"` and `"encoding/base64"` to the imports in `schema_provisioner.go`.

- [ ] **Step 8: Run test to verify it passes**

Run: `task test -- -run TestGeneratePassword ./internal/plugin/`
Expected: PASS

- [ ] **Step 9: Write failing test for pluginConnString**

Add to `internal/plugin/schema_provisioner_test.go`:

```go
func TestPluginConnStringReplacesCredentialsAndSearchPath(t *testing.T) {
	base := "postgres://serveruser:serverpass@localhost:5432/dbname?sslmode=disable"
	got, err := pluginConnString(base, "plugin_core_scenes", "holomush_plugin_core_scenes", "s3cret")
	require.NoError(t, err)

	u, err := url.Parse(got)
	require.NoError(t, err)

	assert.Equal(t, "holomush_plugin_core_scenes", u.User.Username())
	pw, ok := u.User.Password()
	assert.True(t, ok)
	assert.Equal(t, "s3cret", pw)
	assert.Equal(t, "plugin_core_scenes", u.Query().Get("search_path"))
	assert.Equal(t, "disable", u.Query().Get("sslmode"))
	assert.Equal(t, "localhost:5432", u.Host)
	assert.Equal(t, "/dbname", u.Path)
}

func TestPluginConnStringRejectsInvalidURL(t *testing.T) {
	_, err := pluginConnString("://bad", "s", "r", "p")
	require.Error(t, err)
}
```

- [ ] **Step 10: Run test to verify it fails**

Run: `task test -- -run TestPluginConnString ./internal/plugin/`
Expected: FAIL — `pluginConnString` undefined

- [ ] **Step 11: Implement pluginConnString**

Add to `internal/plugin/schema_provisioner.go`:

```go
// pluginConnString builds a connection string with plugin-specific credentials
// and search_path. Replaces user, password, and search_path in the base URL.
func pluginConnString(baseConnString, schemaName, roleName, password string) (string, error) {
	u, err := url.Parse(baseConnString)
	if err != nil {
		return "", oops.Code("SCHEMA_CONNSTRING_PARSE_FAILED").Wrap(err)
	}
	u.User = url.UserPassword(roleName, password)
	q := u.Query()
	q.Set("search_path", schemaName)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
```

- [ ] **Step 12: Run test to verify it passes**

Run: `task test -- -run TestPluginConnString ./internal/plugin/`
Expected: PASS

- [ ] **Step 13: Run all schema provisioner tests**

Run: `task test -- ./internal/plugin/`
Expected: all PASS

- [ ] **Step 14: Commit**

```text
jj commit -m "feat(plugin): add pluginRoleName, generatePassword, pluginConnString helpers

Pure functions for per-plugin role management:
- pluginRoleName: holomush_plugin_ prefix with hyphen sanitization
- generatePassword: 256-bit crypto/rand, base64url-encoded
- pluginConnString: replaces credentials and search_path in base URL"
```

---

## Task 4: SchemaProvisioner Role Management

**Files:**

- Modify: `internal/plugin/schema_provisioner.go`
- Create: `test/integration/plugin/schema_isolation_test.go`

This task implements Init validation, ProvisionSchema role creation, and
PurgeSchema — all driven by integration tests against a real PostgreSQL
container.

- [ ] **Step 1: Write the integration test file with the first test**

Create `test/integration/plugin/schema_isolation_test.go`:

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package plugin_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	plugins "github.com/holomush/holomush/internal/plugin"
	"github.com/holomush/holomush/test/testutil"
)

func TestSchemaProvisionerInitFailsWithoutCreaterole(t *testing.T) {
	ctx := context.Background()
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	defer func() { _ = pgEnv.Terminate(ctx) }()

	// Create a restricted role without CREATEROLE
	adminConn, err := pgx.Connect(ctx, pgEnv.ConnStr)
	require.NoError(t, err)
	_, err = adminConn.Exec(ctx, "CREATE ROLE restricted_user LOGIN PASSWORD 'restricted'")
	require.NoError(t, err)
	adminConn.Close(ctx)

	// Build connection string for restricted user
	restrictedConnStr := replaceUser(t, pgEnv.ConnStr, "restricted_user", "restricted")

	sp := plugins.NewSchemaProvisioner(restrictedConnStr)
	err = sp.Init(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SCHEMA_INSUFFICIENT_PRIVILEGES")
	sp.Close()
}

func TestSchemaProvisionerInitSucceedsWithCreaterole(t *testing.T) {
	ctx := context.Background()
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	defer func() { _ = pgEnv.Terminate(ctx) }()

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	err = sp.Init(ctx)
	require.NoError(t, err)
	sp.Close()
}

func TestProvisionSchemaCreatesRoleAndSchema(t *testing.T) {
	ctx := context.Background()
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	defer func() { _ = pgEnv.Terminate(ctx) }()

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	defer sp.Close()

	connStr, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)
	require.NotEmpty(t, connStr)

	// Verify role exists with LOGIN and no superuser
	pool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	require.NoError(t, err)
	defer pool.Close()

	var rolLogin, rolSuper, rolCreateRole bool
	err = pool.QueryRow(ctx,
		"SELECT rolcanlogin, rolsuper, rolcreaterole FROM pg_roles WHERE rolname = $1",
		"holomush_plugin_test_plugin",
	).Scan(&rolLogin, &rolSuper, &rolCreateRole)
	require.NoError(t, err)

	assert.True(t, rolLogin, "plugin role must have LOGIN")
	assert.False(t, rolSuper, "plugin role must not be superuser")
	assert.False(t, rolCreateRole, "plugin role must not have CREATEROLE")

	// Verify schema exists and is owned by plugin role
	var schemaOwner string
	err = pool.QueryRow(ctx,
		`SELECT r.rolname FROM pg_namespace n
		 JOIN pg_roles r ON n.nspowner = r.oid
		 WHERE n.nspname = $1`,
		"plugin_test_plugin",
	).Scan(&schemaOwner)
	require.NoError(t, err)
	assert.Equal(t, "holomush_plugin_test_plugin", schemaOwner)
}

func TestPluginRoleCanCreateTablesInOwnSchema(t *testing.T) {
	ctx := context.Background()
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	defer func() { _ = pgEnv.Terminate(ctx) }()

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	defer sp.Close()

	pluginConnStr, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	// Connect as plugin role
	conn, err := pgx.Connect(ctx, pluginConnStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	// Plugin can create tables in its own schema
	_, err = conn.Exec(ctx, "CREATE TABLE test_table (id SERIAL PRIMARY KEY, name TEXT)")
	require.NoError(t, err)

	_, err = conn.Exec(ctx, "INSERT INTO test_table (name) VALUES ('hello')")
	require.NoError(t, err)

	var name string
	err = conn.QueryRow(ctx, "SELECT name FROM test_table WHERE id = 1").Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "hello", name)
}

func TestPluginRoleCannotAccessPublicSchema(t *testing.T) {
	ctx := context.Background()
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	defer func() { _ = pgEnv.Terminate(ctx) }()

	// Create a table in public schema as holomush
	adminPool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	require.NoError(t, err)
	_, err = adminPool.Exec(ctx, "CREATE TABLE public.sensitive_data (secret TEXT)")
	require.NoError(t, err)
	_, err = adminPool.Exec(ctx, "INSERT INTO public.sensitive_data (secret) VALUES ('password123')")
	require.NoError(t, err)
	adminPool.Close()

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	defer sp.Close()

	pluginConnStr, err := sp.ProvisionSchema(ctx, "evil-plugin")
	require.NoError(t, err)

	// Connect as plugin and try to read public schema
	conn, err := pgx.Connect(ctx, pluginConnStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, "SET search_path TO public")
	require.NoError(t, err)

	var secret string
	err = conn.QueryRow(ctx, "SELECT secret FROM sensitive_data LIMIT 1").Scan(&secret)
	require.Error(t, err, "plugin must not read public schema tables")
}

func TestCrossPluginIsolation(t *testing.T) {
	ctx := context.Background()
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	defer func() { _ = pgEnv.Terminate(ctx) }()

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	defer sp.Close()

	connA, err := sp.ProvisionSchema(ctx, "plugin-a")
	require.NoError(t, err)
	connB, err := sp.ProvisionSchema(ctx, "plugin-b")
	require.NoError(t, err)

	// Plugin A creates a table
	cA, err := pgx.Connect(ctx, connA)
	require.NoError(t, err)
	_, err = cA.Exec(ctx, "CREATE TABLE secrets (val TEXT)")
	require.NoError(t, err)
	_, err = cA.Exec(ctx, "INSERT INTO secrets (val) VALUES ('a-secret')")
	require.NoError(t, err)
	cA.Close(ctx)

	// Plugin B tries to read plugin A's table
	cB, err := pgx.Connect(ctx, connB)
	require.NoError(t, err)
	defer cB.Close(ctx)

	_, err = cB.Exec(ctx, "SET search_path TO plugin_plugin_a")
	require.NoError(t, err)

	var val string
	err = cB.QueryRow(ctx, "SELECT val FROM secrets LIMIT 1").Scan(&val)
	require.Error(t, err, "plugin B must not read plugin A's tables")
}

func TestIdempotentProvisionRefreshesPassword(t *testing.T) {
	ctx := context.Background()
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	defer func() { _ = pgEnv.Terminate(ctx) }()

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	defer sp.Close()

	conn1, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	// Create a table with the first connection
	c, err := pgx.Connect(ctx, conn1)
	require.NoError(t, err)
	_, err = c.Exec(ctx, "CREATE TABLE persist_test (id INT)")
	require.NoError(t, err)
	c.Close(ctx)

	// Re-provision (simulates server restart)
	conn2, err := sp.ProvisionSchema(ctx, "test-plugin")
	require.NoError(t, err)

	// New connection string works and table persists
	c2, err := pgx.Connect(ctx, conn2)
	require.NoError(t, err)
	defer c2.Close(ctx)

	var count int
	err = c2.QueryRow(ctx, "SELECT COUNT(*) FROM persist_test").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)

	// Old connection string should NOT work (password changed)
	_, err = pgx.Connect(ctx, conn1)
	assert.Error(t, err, "old password must be invalidated")
}

func TestPurgeSchemaRemovesRoleAndSchema(t *testing.T) {
	ctx := context.Background()
	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	defer func() { _ = pgEnv.Terminate(ctx) }()

	sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
	require.NoError(t, sp.Init(ctx))
	defer sp.Close()

	pluginConnStr, err := sp.ProvisionSchema(ctx, "doomed-plugin")
	require.NoError(t, err)

	// Create a table
	c, err := pgx.Connect(ctx, pluginConnStr)
	require.NoError(t, err)
	_, err = c.Exec(ctx, "CREATE TABLE doomed_table (id INT)")
	require.NoError(t, err)
	c.Close(ctx)

	// Purge
	require.NoError(t, sp.PurgeSchema(ctx, "doomed-plugin"))

	// Verify role gone
	pool, err := pgxpool.New(ctx, pgEnv.ConnStr)
	require.NoError(t, err)
	defer pool.Close()

	var exists bool
	err = pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)",
		"holomush_plugin_doomed_plugin",
	).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "role must be dropped after purge")

	// Verify schema gone
	err = pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = $1)",
		"plugin_doomed_plugin",
	).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "schema must be dropped after purge")
}

// replaceUser is a test helper that swaps credentials in a connection string.
func replaceUser(t *testing.T, connStr, user, password string) string {
	t.Helper()
	u, err := parseConnStr(connStr)
	require.NoError(t, err)
	u.User = url.UserPassword(user, password)
	return u.String()
}

func parseConnStr(connStr string) (*url.URL, error) {
	return url.Parse(connStr)
}
```

Add `"net/url"` to imports.

- [ ] **Step 2: Run tests to verify they fail**

Run: `task test:int -- -run TestSchemaProvisioner -count=1 ./test/integration/plugin/`
Expected: FAIL — methods not implemented

- [ ] **Step 3: Implement Init CREATEROLE validation**

Modify `SchemaProvisioner.Init()` in `internal/plugin/schema_provisioner.go`:

```go
func (sp *SchemaProvisioner) Init(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, sp.baseConnString)
	if err != nil {
		return oops.Code("SCHEMA_POOL_INIT_FAILED").Wrap(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return oops.Code("SCHEMA_POOL_PING_FAILED").Wrap(err)
	}

	// Validate the server role has CREATEROLE privilege
	var hasCreaterole bool
	err = pool.QueryRow(ctx,
		"SELECT rolcreaterole FROM pg_roles WHERE rolname = current_user",
	).Scan(&hasCreaterole)
	if err != nil {
		pool.Close()
		return oops.Code("SCHEMA_ROLE_NOT_FOUND").
			Wrap(fmt.Errorf("cannot query current role privileges: %w", err))
	}
	if !hasCreaterole {
		pool.Close()
		var currentUser string
		// Best-effort: try to get the username for the error message
		_ = pool.QueryRow(ctx, "SELECT current_user").Scan(&currentUser)
		return oops.Code("SCHEMA_INSUFFICIENT_PRIVILEGES").
			Errorf("server database role %q lacks CREATEROLE privilege; run: ALTER ROLE %s CREATEROLE",
				currentUser, currentUser)
	}

	sp.pool = pool
	return nil
}
```

Wait — there's a bug. After `pool.Close()` on the `!hasCreaterole` branch, the `pool.QueryRow` for `currentUser` would fail. Fix: query current_user before closing.

```go
func (sp *SchemaProvisioner) Init(ctx context.Context) error {
	pool, err := pgxpool.New(ctx, sp.baseConnString)
	if err != nil {
		return oops.Code("SCHEMA_POOL_INIT_FAILED").Wrap(err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return oops.Code("SCHEMA_POOL_PING_FAILED").Wrap(err)
	}

	var currentUser string
	var hasCreaterole bool
	err = pool.QueryRow(ctx,
		"SELECT current_user, rolcreaterole FROM pg_roles WHERE rolname = current_user",
	).Scan(&currentUser, &hasCreaterole)
	if err != nil {
		pool.Close()
		return oops.Code("SCHEMA_ROLE_NOT_FOUND").
			Wrap(fmt.Errorf("cannot query current role privileges: %w", err))
	}
	if !hasCreaterole {
		pool.Close()
		return oops.Code("SCHEMA_INSUFFICIENT_PRIVILEGES").
			Errorf("server database role %q lacks CREATEROLE privilege; run: ALTER ROLE %s CREATEROLE",
				currentUser, currentUser)
	}

	slog.Info("schema provisioner initialized", "role", currentUser, "createrole", true)
	sp.pool = pool
	return nil
}
```

- [ ] **Step 4: Implement ProvisionSchema role creation**

Replace the existing `ProvisionSchema` method:

```go
func (sp *SchemaProvisioner) ProvisionSchema(ctx context.Context, pluginName string) (string, error) {
	schemaName := pluginSchemaName(pluginName)
	roleName := pluginRoleName(pluginName)

	password, err := generatePassword()
	if err != nil {
		return "", oops.Code("SCHEMA_PASSWORD_FAILED").
			With("plugin", pluginName).Wrap(err)
	}

	// Create or refresh role
	if err := sp.ensureRole(ctx, roleName, password); err != nil {
		return "", oops.Code("SCHEMA_ROLE_FAILED").
			With("plugin", pluginName).
			With("role", roleName).Wrap(err)
	}

	// Create schema
	schemaID := pgx.Identifier{schemaName}
	ddl := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schemaID.Sanitize())
	if _, err := sp.pool.Exec(ctx, ddl); err != nil {
		return "", oops.Code("SCHEMA_CREATE_FAILED").
			With("plugin", pluginName).
			With("schema", schemaName).Wrap(err)
	}

	// Transfer schema ownership to plugin role
	roleID := pgx.Identifier{roleName}
	ownerDDL := fmt.Sprintf("ALTER SCHEMA %s OWNER TO %s", schemaID.Sanitize(), roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, ownerDDL); err != nil {
		return "", oops.Code("SCHEMA_OWNER_FAILED").
			With("plugin", pluginName).Wrap(err)
	}

	// Grant schema access to plugin role
	grantDDL := fmt.Sprintf("GRANT USAGE, CREATE ON SCHEMA %s TO %s",
		schemaID.Sanitize(), roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, grantDDL); err != nil {
		return "", oops.Code("SCHEMA_GRANT_FAILED").
			With("plugin", pluginName).Wrap(err)
	}

	// Revoke public schema access
	revokeDDL := fmt.Sprintf("REVOKE ALL ON SCHEMA public FROM %s", roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, revokeDDL); err != nil {
		return "", oops.Code("SCHEMA_REVOKE_FAILED").
			With("plugin", pluginName).Wrap(err)
	}

	slog.Info("provisioned plugin schema with isolated role",
		"plugin", pluginName, "schema", schemaName, "role", roleName)

	connStr, err := pluginConnString(sp.baseConnString, schemaName, roleName, password)
	if err != nil {
		return "", oops.Code("SCHEMA_CONNSTRING_FAILED").
			With("plugin", pluginName).Wrap(err)
	}
	return connStr, nil
}

func (sp *SchemaProvisioner) ensureRole(ctx context.Context, roleName, password string) error {
	roleID := pgx.Identifier{roleName}

	var exists bool
	err := sp.pool.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)",
		roleName,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check role existence: %w", err)
	}

	if exists {
		// Refresh ephemeral password
		// Password is base64url (A-Za-z0-9-_), safe for literal inclusion.
		ddl := fmt.Sprintf("ALTER ROLE %s PASSWORD '%s'", roleID.Sanitize(), password)
		if _, err := sp.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("refresh role password: %w", err)
		}
	} else {
		ddl := fmt.Sprintf("CREATE ROLE %s LOGIN PASSWORD '%s'", roleID.Sanitize(), password)
		if _, err := sp.pool.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("create role: %w", err)
		}
	}
	return nil
}
```

- [ ] **Step 5: Implement PurgeSchema**

Add to `internal/plugin/schema_provisioner.go`:

```go
// PurgeSchema drops all objects owned by the plugin role and then drops
// the role itself. This permanently destroys the plugin's data.
func (sp *SchemaProvisioner) PurgeSchema(ctx context.Context, pluginName string) error {
	roleName := pluginRoleName(pluginName)
	roleID := pgx.Identifier{roleName}

	// DROP OWNED BY cascades through schema, tables, sequences, etc.
	dropOwned := fmt.Sprintf("DROP OWNED BY %s", roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, dropOwned); err != nil {
		return oops.Code("SCHEMA_DROP_OWNED_FAILED").
			With("plugin", pluginName).
			With("role", roleName).Wrap(err)
	}

	dropRole := fmt.Sprintf("DROP ROLE IF EXISTS %s", roleID.Sanitize())
	if _, err := sp.pool.Exec(ctx, dropRole); err != nil {
		return oops.Code("SCHEMA_DROP_ROLE_FAILED").
			With("plugin", pluginName).
			With("role", roleName).Wrap(err)
	}

	slog.Info("purged plugin schema and role", "plugin", pluginName, "role", roleName)
	return nil
}
```

- [ ] **Step 6: Remove the now-unused scopedConnString function**

`scopedConnString` is replaced by `pluginConnString`. Remove it from
`schema_provisioner.go` and update/remove its tests in
`schema_provisioner_test.go`. The tests `TestScopedConnString*` should be
replaced by the `TestPluginConnString*` tests from Task 3.

- [ ] **Step 7: Run unit tests**

Run: `task test -- ./internal/plugin/`
Expected: PASS

- [ ] **Step 8: Run integration tests**

Run: `task test:int -- -run "TestSchemaProvisioner|TestProvision|TestPlugin|TestCross|TestIdempotent|TestPurge" -count=1 ./test/integration/plugin/`
Expected: all PASS

- [ ] **Step 9: Commit**

```text
jj commit -m "feat(plugin): per-plugin PostgreSQL role isolation in SchemaProvisioner

Init() validates CREATEROLE privilege at startup and fails fast if missing.
ProvisionSchema() creates a per-plugin role with LOGIN, sets ephemeral
password from crypto/rand, transfers schema ownership, and revokes public
schema access. PurgeSchema() drops all owned objects and the role.

Closes: holomush-fwan (SEC-01)"
```

---

## Task 5: Migrate Integration Tests to Non-Superuser

**Files (13 total):**

All files below currently start a testcontainer with `postgres.WithUsername("holomush")` which creates `holomush` as a superuser. Each must switch to `testutil.StartPostgres()` which creates `holomush` as a non-superuser with CREATEROLE.

**Transformation pattern:**

Before (varies by file — shown for Ginkgo BeforeSuite):

```go
container, err := postgres.Run(ctx,
    "postgres:18-alpine",
    postgres.WithDatabase("holomush_test"),
    postgres.WithUsername("holomush"),
    postgres.WithPassword("holomush"),
    testcontainers.WithWaitStrategy(
        wait.ForLog("database system is ready to accept connections").
            WithOccurrence(2).
            WithStartupTimeout(30*time.Second),
    ),
)
// error handling...
connStr, err := container.ConnectionString(ctx, "sslmode=disable")
```

After:

```go
pgEnv, err := testutil.StartPostgres(ctx)
// error handling...
container := pgEnv.Container // keep reference for Terminate()
connStr := pgEnv.ConnStr
```

Add import: `"github.com/holomush/holomush/test/testutil"`

Remove unused imports: `"github.com/testcontainers/testcontainers-go/modules/postgres"`,
`"github.com/testcontainers/testcontainers-go/wait"`. Keep
`"github.com/testcontainers/testcontainers-go"` if `testcontainers.Container`
is referenced by type.

- [ ] **Step 1: Migrate test/integration/ suites (8 files)**

Apply the transformation to each file. The setup structure varies:

| File | Setup pattern |
|------|---------------|
| `test/integration/world/world_suite_test.go` | `setupWorldTestEnv()` |
| `test/integration/auth/auth_suite_test.go` | `setupTestEnv()` |
| `test/integration/access/access_suite_test.go` | `setupAccessTestEnv()` |
| `test/integration/content/content_integration_test.go` | inline BeforeSuite |
| `test/integration/session/session_persistence_integration_test.go` | inline BeforeEach |
| `test/integration/telnet/e2e_test.go` | inline BeforeEach |
| `test/integration/plugin/binary_plugin_test.go` | inline BeforeEach |
| `test/integration/phase1_5_test.go` | `setupPhase1_5Env()` |

- [ ] **Step 2: Migrate internal/ integration tests (5 files)**

| File | Setup pattern |
|------|---------------|
| `internal/store/postgres_integration_test.go` | `setupPostgresContainer()` |
| `internal/auth/postgres/postgres_test.go` | `TestMain` |
| `internal/content/postgres_store_test.go` | `setupPool(t)` helper |
| `internal/world/postgres/postgres_test.go` | `TestMain` |
| `internal/access/policy/store/postgres_integration_test.go` | inline BeforeSuite |

For `TestMain` pattern, the transformation is:

```go
// Before:
container, err := postgres.Run(ctx, "postgres:18-alpine",
    postgres.WithDatabase("holomush_test"),
    postgres.WithUsername("holomush"),
    postgres.WithPassword("holomush"),
    testcontainers.WithWaitStrategy(...),
)
connStr, err := container.ConnectionString(ctx, "sslmode=disable")

// After:
pgEnv, err := testutil.StartPostgres(ctx)
connStr := pgEnv.ConnStr
// In cleanup: pgEnv.Terminate(ctx)
```

- [ ] **Step 3: Run all integration tests**

Run: `task test:int`
Expected: all PASS

If any test fails with permission errors (e.g., `CREATE EXTENSION`), add
the extension to `testutil.initHolomushRole()` before role creation.

- [ ] **Step 4: Commit**

```text
jj commit -m "test: migrate all integration tests to non-superuser holomush role

All 13 integration test suites now use testutil.StartPostgres() which
creates a holomush role with CREATEROLE (not superuser). This matches
production deployment and catches permission bugs that superuser testing
would silently allow."
```

---

## Task 6: E2E Concurrency Guard

**Files:**

- Modify: `Taskfile.yaml:90-101` (test:e2e task)

- [ ] **Step 1: Add guard check to test:e2e**

Add a precondition command before the existing commands:

```yaml
  test:e2e:
    desc: Run Playwright E2E tests in Docker
    deps: ['docker:build']
    cmds:
      - cmd: |
          if docker compose -p holomush-e2e ps -q 2>/dev/null | grep -q .; then
            echo "ERROR: E2E infrastructure already running (project: holomush-e2e)."
            echo "Stop it with: docker compose -p holomush-e2e down -v"
            exit 1
          fi
      - rm -rf web/test-results
      - defer: docker compose -p holomush-e2e -f compose.yaml -f compose.e2e.yaml down -v
      - cmd: |
          set +e
          docker compose -p holomush-e2e -f compose.yaml -f compose.e2e.yaml run --rm playwright npx playwright test {{.CLI_ARGS}}
          E2E_EXIT=$?
          ./scripts/e2e-summary.sh
          exit $E2E_EXIT
```

- [ ] **Step 2: Add same guard to test:e2e:cover**

Add the same guard block as the first command in `test:e2e:cover`.

- [ ] **Step 3: Verify guard works**

Run: `docker compose -p holomush-e2e -f compose.yaml -f compose.e2e.yaml up -d postgres`
Run: `task test:e2e 2>&1 | head -5`
Expected: "ERROR: E2E infrastructure already running"

Run: `docker compose -p holomush-e2e down -v`

- [ ] **Step 4: Commit**

```text
jj commit -m "ci(e2e): add concurrency guard to prevent overlapping E2E runs

Checks for existing holomush-e2e containers before starting. Fails fast
with instructions to stop the running instance. Applies to both
test:e2e and test:e2e:cover tasks."
```

---

## Task 7: Verification

- [ ] **Step 1: Run full unit test suite**

Run: `task test`
Expected: all PASS

- [ ] **Step 2: Run full integration test suite**

Run: `task test:int`
Expected: all PASS

- [ ] **Step 3: Run linter**

Run: `task lint`
Expected: no errors

- [ ] **Step 4: Run E2E tests**

Run: `task test:e2e`
Expected: all PASS

- [ ] **Step 5: Run full pr-prep**

Run: `task pr-prep`
Expected: all checks pass

- [ ] **Step 6: Close the bead**

Run: `bd close holomush-fwan --reason "Per-plugin PostgreSQL role isolation implemented and verified"`

- [ ] **Step 7: Final commit (if any formatting/lint fixes needed)**

```text
jj commit -m "chore: fix lint/format issues from pr-prep"
```
