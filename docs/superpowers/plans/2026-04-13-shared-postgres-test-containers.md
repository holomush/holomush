<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Shared PostgreSQL Test Containers Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reduce PostgreSQL container count from ~34 to ~16 per integration test run by sharing one container per package with per-test database isolation via PostgreSQL template databases.

**Architecture:** A `sync.Once` singleton in `testutil` starts one container per test binary. A template database is created and migrated once per container. Each test gets an instant copy via `CREATE DATABASE ... TEMPLATE`. Migration tests and schema isolation tests use a `RawDatabase` helper that skips the template.

**Tech Stack:** testcontainers-go (postgres module), pgx/v5, crypto/rand, store.NewMigrator

**Spec:** `docs/superpowers/specs/2026-04-13-shared-postgres-test-containers-design.md`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `test/testutil/postgres.go` | Existing — add `SharedPostgres`, `FreshDatabase`, `RawDatabase`; keep `StartPostgres` |
| `test/testutil/postgres_test.go` | New — unit tests for the new helpers (uses `StartPostgres` internally) |
| `test/integration/plugin/schema_isolation_test.go` | Modify — switch from per-test `StartPostgres` to `SharedPostgres` + `RawDatabase` |
| `test/integration/plugin/binary_plugin_test.go` | Modify — switch `BeforeEach` containers to `SharedPostgres` + `FreshDatabase` |
| `test/integration/plugin/abac_widget_test.go` | Modify — switch `BeforeEach` containers to `SharedPostgres` + `FreshDatabase` |
| `test/integration/telnet/e2e_test.go` | Modify — lift container to `BeforeSuite`, use `FreshDatabase` in `BeforeEach` |
| `internal/store/migrations_audit_shape_integration_test.go` | Modify — switch to `SharedPostgres` + `RawDatabase` |
| `internal/store/postgres_integration_test.go` | Modify — switch to `SharedPostgres` + `FreshDatabase` |
| `internal/world/postgres/postgres_test.go` | Modify — switch `TestMain` to use `SharedPostgres` + `FreshDatabase` |
| `internal/auth/postgres/postgres_test.go` | Modify — switch `TestMain` to use `SharedPostgres` + `FreshDatabase` |
| `internal/content/postgres_store_test.go` | Modify — switch `setupPool` to use `SharedPostgres` + `FreshDatabase` |
| `plugins/core-scenes/store_integration_test.go` | Modify — switch `newTestStore` to use `SharedPostgres` + `FreshDatabase` |
| `test/integration/world/world_suite_test.go` | Modify — switch `BeforeSuite` to `SharedPostgres` + `FreshDatabase` |
| `test/integration/auth/auth_suite_test.go` | Modify — switch `BeforeSuite` to `SharedPostgres` + `FreshDatabase` |
| `test/integration/access/access_suite_test.go` | Modify — switch `BeforeSuite` to `SharedPostgres` + `FreshDatabase` |
| `test/integration/session/session_persistence_suite_test.go` | Modify — switch `BeforeSuite` to `SharedPostgres` + `FreshDatabase` |
| `test/integration/content/content_integration_test.go` | Modify — switch `BeforeSuite` to `SharedPostgres` + `FreshDatabase` |
| `test/integration/audit/audit_integration_test.go` | Modify — switch to `SharedPostgres` + `FreshDatabase` |
| `test/integration/settings/game_settings_integration_test.go` | Modify — switch to `SharedPostgres` + `FreshDatabase` |
| `test/integration/phase1_5_test.go` | Modify — switch to `SharedPostgres` + `FreshDatabase` |
| `test/integration/plugin/alias_seeder_test.go` | Modify — switch to `SharedPostgres` + `FreshDatabase` |

---

## Task 1: Implement `SharedPostgres` Singleton

**Files:**

- Modify: `test/testutil/postgres.go`

- [ ] **Step 1: Add package-level singleton state**

Add these package-level variables and the `SharedPostgres` function to `test/testutil/postgres.go`:

```go
import (
	"sync"
	"testing"
)

var (
	sharedOnce sync.Once
	sharedEnv  *PostgresEnv
	sharedErr  error
)

// SharedPostgres returns a package-level singleton PostgreSQL container.
// The container is started once per test binary via sync.Once. Cleanup is
// handled by testcontainers' Ryuk reaper when the process exits — callers
// MUST NOT call Terminate on the returned env.
//
// For tests that need a dedicated container (none currently), use
// StartPostgres instead.
func SharedPostgres(t testing.TB) *PostgresEnv {
	t.Helper()
	sharedOnce.Do(func() {
		ctx := context.Background()
		sharedEnv, sharedErr = StartPostgres(ctx)
	})
	if sharedErr != nil {
		t.Fatalf("shared postgres container: %v", sharedErr)
	}
	return sharedEnv
}
```

- [ ] **Step 2: Add `AdminConnStr` field to `PostgresEnv`**

`FreshDatabase` and `RawDatabase` need superuser access to create databases. Add a second connection string field:

```go
type PostgresEnv struct {
	Container    testcontainers.Container
	ConnStr      string // holomush:holomush credentials
	AdminConnStr string // postgres:postgres superuser credentials
}
```

Update `startPostgresOnce` to populate it. After line 81 (`adminConnStr, err := adminConnectionString(ctx, container)`), the admin string is already available. Store it before replacing credentials:

```go
return &PostgresEnv{
	Container:    container,
	ConnStr:      connStr,
	AdminConnStr: adminConnStr,
}, nil
```

- [ ] **Step 3: Verify compilation**

Run: `task test -- -run '^$' -count=1 ./test/testutil/`
Expected: compiles with no errors (runs no tests since the pattern matches nothing)

- [ ] **Step 4: Commit**

```text
feat(testutil): add SharedPostgres singleton for container reuse

Introduces a sync.Once-guarded SharedPostgres(t) that returns
a single PostgreSQL container per test binary. Adds AdminConnStr
to PostgresEnv for superuser database creation in later tasks.
```

---

## Task 2: Implement `FreshDatabase` with Template Optimization

**Files:**

- Modify: `test/testutil/postgres.go`

- [ ] **Step 1: Add template database creation**

Add the template `sync.Once` and `FreshDatabase` to `test/testutil/postgres.go`:

```go
import (
	"crypto/rand"
	"encoding/hex"

	"github.com/jackc/pgx/v5"

	"github.com/holomush/holomush/internal/store"
)

var (
	templateOnce sync.Once
	templateErr  error
	templateName = "holomush_template"
)

// ensureTemplate creates a migrated template database on the shared
// container. Called once per container via sync.Once.
func ensureTemplate(t testing.TB, env *PostgresEnv) {
	t.Helper()
	templateOnce.Do(func() {
		ctx := context.Background()

		conn, err := pgx.Connect(ctx, env.AdminConnStr)
		if err != nil {
			templateErr = fmt.Errorf("connect as admin for template: %w", err)
			return
		}
		defer func() { _ = conn.Close(ctx) }()

		// Create template database owned by holomush.
		_, err = conn.Exec(ctx, fmt.Sprintf(
			"CREATE DATABASE %s OWNER holomush", templateName))
		if err != nil {
			templateErr = fmt.Errorf("create template database: %w", err)
			return
		}

		// Build connection string for the template database.
		tmplConnStr, err := replaceDatabase(env.ConnStr, templateName)
		if err != nil {
			templateErr = fmt.Errorf("build template connection string: %w", err)
			return
		}

		// Run migrations on the template.
		migrator, err := store.NewMigrator(tmplConnStr)
		if err != nil {
			templateErr = fmt.Errorf("create migrator for template: %w", err)
			return
		}
		if err := migrator.Up(); err != nil {
			_ = migrator.Close()
			templateErr = fmt.Errorf("migrate template database: %w", err)
			return
		}
		_ = migrator.Close()

		// Mark as template so CREATE DATABASE ... TEMPLATE works.
		_, err = conn.Exec(ctx, fmt.Sprintf(
			"ALTER DATABASE %s IS_TEMPLATE true", templateName))
		if err != nil {
			templateErr = fmt.Errorf("mark template database: %w", err)
			return
		}
	})
	if templateErr != nil {
		t.Fatalf("template database: %v", templateErr)
	}
}
```

- [ ] **Step 2: Add the `FreshDatabase` function**

```go
// FreshDatabase creates a uniquely-named, fully-migrated database on the
// shared container by copying the template database. Returns a connection
// string with holomush:holomush credentials. The database is dropped in
// t.Cleanup.
func FreshDatabase(t testing.TB, env *PostgresEnv) string {
	t.Helper()
	ensureTemplate(t, env)

	dbName := randomDBName()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, env.AdminConnStr)
	if err != nil {
		t.Fatalf("connect as admin for fresh database: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	_, err = conn.Exec(ctx, fmt.Sprintf(
		"CREATE DATABASE %s TEMPLATE %s OWNER holomush", dbName, templateName))
	if err != nil {
		t.Fatalf("create fresh database %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		dropCtx := context.Background()
		dropConn, dropErr := pgx.Connect(dropCtx, env.AdminConnStr)
		if dropErr != nil {
			t.Logf("WARN: connect for drop %s: %v", dbName, dropErr)
			return
		}
		defer func() { _ = dropConn.Close(dropCtx) }()

		// Force-drop disconnects any lingering connections.
		_, dropErr = dropConn.Exec(dropCtx, fmt.Sprintf(
			"DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName))
		if dropErr != nil {
			t.Logf("WARN: drop database %s: %v", dbName, dropErr)
		}
	})

	freshConnStr, err := replaceDatabase(env.ConnStr, dbName)
	if err != nil {
		t.Fatalf("build connection string for %s: %v", dbName, err)
	}
	return freshConnStr
}

func randomDBName() string {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return "test_" + hex.EncodeToString(b)
}

func replaceDatabase(connStr, dbName string) (string, error) {
	u, err := url.Parse(connStr)
	if err != nil {
		return "", fmt.Errorf("parse connection string: %w", err)
	}
	u.Path = "/" + dbName
	return u.String(), nil
}
```

- [ ] **Step 3: Verify compilation**

Run: `task test -- -run '^$' -count=1 ./test/testutil/`
Expected: compiles with no errors

- [ ] **Step 4: Commit**

```text
feat(testutil): add FreshDatabase with template optimization

FreshDatabase creates a per-test database by copying a pre-migrated
template database. Migrations run once per container; each test gets
an instant copy (~5ms vs ~200-500ms). Database is auto-dropped in
t.Cleanup.
```

---

## Task 3: Implement `RawDatabase`

**Files:**

- Modify: `test/testutil/postgres.go`

- [ ] **Step 1: Add `RawDatabase` function**

```go
// RawDatabase creates a uniquely-named blank database on the shared
// container with no migrations and no holomush role grants. Returns a
// superuser (postgres) connection string. Use for migration tests and
// schema isolation tests that need full control.
func RawDatabase(t testing.TB, env *PostgresEnv) string {
	t.Helper()
	dbName := randomDBName()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, env.AdminConnStr)
	if err != nil {
		t.Fatalf("connect as admin for raw database: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	if err != nil {
		t.Fatalf("create raw database %s: %v", dbName, err)
	}

	t.Cleanup(func() {
		dropCtx := context.Background()
		dropConn, dropErr := pgx.Connect(dropCtx, env.AdminConnStr)
		if dropErr != nil {
			t.Logf("WARN: connect for drop %s: %v", dbName, dropErr)
			return
		}
		defer func() { _ = dropConn.Close(dropCtx) }()

		_, dropErr = dropConn.Exec(dropCtx, fmt.Sprintf(
			"DROP DATABASE IF EXISTS %s WITH (FORCE)", dbName))
		if dropErr != nil {
			t.Logf("WARN: drop database %s: %v", dbName, dropErr)
		}
	})

	rawConnStr, err := replaceDatabase(env.AdminConnStr, dbName)
	if err != nil {
		t.Fatalf("build connection string for raw %s: %v", dbName, err)
	}
	return rawConnStr
}
```

- [ ] **Step 2: Verify compilation**

Run: `task test -- -run '^$' -count=1 ./test/testutil/`
Expected: compiles with no errors

- [ ] **Step 3: Commit**

```text
feat(testutil): add RawDatabase for migration and schema isolation tests

RawDatabase creates a blank database with superuser access — no
template, no migrations, no holomush role grants. For tests that
need full control over the PostgreSQL lifecycle.
```

---

## Task 4: Integration Tests for New Helpers

**Files:**

- Create: `test/testutil/postgres_integration_test.go`

- [ ] **Step 1: Write test file**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

//go:build integration

package testutil_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/holomush/holomush/test/testutil"
)

func TestSharedPostgresReturnsSameInstanceAcrossCalls(t *testing.T) {
	env1 := testutil.SharedPostgres(t)
	env2 := testutil.SharedPostgres(t)
	assert.Same(t, env1, env2, "SharedPostgres must return the same pointer")
}

func TestSharedPostgresHasAdminConnStr(t *testing.T) {
	env := testutil.SharedPostgres(t)
	require.NotEmpty(t, env.AdminConnStr, "AdminConnStr must be populated")

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, env.AdminConnStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	var user string
	err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
	require.NoError(t, err)
	assert.Equal(t, "postgres", user, "AdminConnStr must connect as postgres superuser")
}

func TestFreshDatabaseReturnsMigratedDatabase(t *testing.T) {
	env := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, env)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	// Verify we're connected as holomush, not postgres.
	var user string
	err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
	require.NoError(t, err)
	assert.Equal(t, "holomush", user)

	// Verify migrations ran — the players table should exist.
	var exists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'players'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	assert.True(t, exists, "players table should exist after migration")
}

func TestFreshDatabaseReturnsIsolatedDatabases(t *testing.T) {
	env := testutil.SharedPostgres(t)
	connStr1 := testutil.FreshDatabase(t, env)
	connStr2 := testutil.FreshDatabase(t, env)

	assert.NotEqual(t, connStr1, connStr2, "each call must return a different database")

	ctx := context.Background()

	// Insert into db1.
	conn1, err := pgx.Connect(ctx, connStr1)
	require.NoError(t, err)
	defer conn1.Close(ctx)
	_, err = conn1.Exec(ctx, "INSERT INTO players (id, username, password_hash) VALUES ('test-id-1', 'user1', 'hash1')")
	require.NoError(t, err)

	// db2 should not see db1's data.
	conn2, err := pgx.Connect(ctx, connStr2)
	require.NoError(t, err)
	defer conn2.Close(ctx)
	var count int
	err = conn2.QueryRow(ctx, "SELECT count(*) FROM players").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "databases must be isolated")
}

func TestRawDatabaseReturnsSuperuserConnectionToBlankDB(t *testing.T) {
	env := testutil.SharedPostgres(t)
	connStr := testutil.RawDatabase(t, env)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, connStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	// Verify superuser access.
	var user string
	err = conn.QueryRow(ctx, "SELECT current_user").Scan(&user)
	require.NoError(t, err)
	assert.Equal(t, "postgres", user)

	// Verify no migrations — players table should not exist.
	var exists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_name = 'players'
		)
	`).Scan(&exists)
	require.NoError(t, err)
	assert.False(t, exists, "raw database should have no migrations")
}
```

- [ ] **Step 2: Run the tests**

Run: `task test:int -- -run 'TestSharedPostgres|TestFreshDatabase|TestRawDatabase' ./test/testutil/`
Expected: all 5 tests pass

- [ ] **Step 3: Commit**

```text
test(testutil): add integration tests for SharedPostgres, FreshDatabase, RawDatabase

Verifies singleton behavior, template-based database creation,
cross-database isolation, superuser access for raw databases,
and migration state.
```

---

## Task 5: Convert `test/integration/plugin/schema_isolation_test.go` (8 → 0 containers)

**Files:**

- Modify: `test/integration/plugin/schema_isolation_test.go`

Schema isolation tests need raw database access (superuser, no migrations) because they test CREATEROLE behavior and cross-schema denial. Each test switches from `StartPostgres` + `Terminate` to `SharedPostgres` + `RawDatabase`.

However, the current tests use `pgEnv.ConnStr` which is the **holomush** role connection — needed for `SchemaProvisioner` which requires CREATEROLE. `RawDatabase` returns a **postgres** superuser string. The tests need both: a superuser connection to create the initial `holomush` role in the raw database, and an `holomush`-role connection to pass to `SchemaProvisioner`.

The cleanest approach: `RawDatabase` gives superuser access. Each test creates the `holomush` role in its raw database (mirroring what `StartPostgres` does at the cluster level), then builds the holomush connection string. Extract this into a local helper.

- [ ] **Step 1: Add a local helper for schema isolation setup**

At the top of `schema_isolation_test.go`, replace the existing `replaceUser` helper and add a new `setupSchemaTestDB` helper:

```go
// setupSchemaTestDB creates a raw database with the holomush role
// configured, matching the production StartPostgres setup. Returns both
// a holomush-role connection string (for SchemaProvisioner) and the
// superuser connection string (for direct assertions).
func setupSchemaTestDB(t *testing.T) (holomushConnStr, adminConnStr string) {
	t.Helper()
	shared := testutil.SharedPostgres(t)
	adminConnStr = testutil.RawDatabase(t, shared)

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, adminConnStr)
	require.NoError(t, err)
	defer conn.Close(ctx)

	// Create the holomush role in this database's cluster — but the
	// shared container already has it from StartPostgres. So we just
	// need to grant it access to this specific database.
	dbName := extractDBName(t, adminConnStr)
	_, err = conn.Exec(ctx, fmt.Sprintf("GRANT ALL ON DATABASE %s TO holomush", dbName))
	require.NoError(t, err)

	// Transfer public schema ownership to holomush.
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
```

- [ ] **Step 2: Convert each test function**

Replace `StartPostgres` + `t.Cleanup(Terminate)` in each test with the new helper.

Example for `TestSchemaProvisionerInitFailsWithoutCreaterole`:

Before:

```go
func TestSchemaProvisionerInitFailsWithoutCreaterole(t *testing.T) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgEnv.Terminate(ctx) })

	adminConn, err := pgx.Connect(ctx, pgEnv.ConnStr)
	// ...
```

After:

```go
func TestSchemaProvisionerInitFailsWithoutCreaterole(t *testing.T) {
	ctx := context.Background()

	_, adminConnStr := setupSchemaTestDB(t)

	adminConn, err := pgx.Connect(ctx, adminConnStr)
	// ...
```

Apply the same pattern to all 8 test functions:

- `TestSchemaProvisionerInitFailsWithoutCreaterole` — uses `adminConnStr` to create a restricted role
- `TestSchemaProvisionerInitSucceedsWithCreaterole` — uses `holomushConnStr` for SchemaProvisioner
- `TestProvisionSchemaCreatesRoleAndSchema` — uses `holomushConnStr` for SchemaProvisioner + assertions via `holomushConnStr`
- `TestPluginRoleCanCreateTablesInOwnSchema` — uses `holomushConnStr`
- `TestPluginRoleCannotAccessPublicSchema` — uses `holomushConnStr`
- `TestCrossPluginIsolation` — uses `holomushConnStr`
- `TestIdempotentProvisionRefreshesPassword` — uses `holomushConnStr`
- `TestPurgeSchemaRemovesRoleAndSchema` — uses `holomushConnStr`

For tests that only need `holomushConnStr`, the conversion is:

Before:

```go
pgEnv, err := testutil.StartPostgres(ctx)
require.NoError(t, err)
t.Cleanup(func() { _ = pgEnv.Terminate(ctx) })

sp := plugins.NewSchemaProvisioner(pgEnv.ConnStr)
```

After:

```go
holomushConnStr, _ := setupSchemaTestDB(t)

sp := plugins.NewSchemaProvisioner(holomushConnStr)
```

For tests that also inspect via a pool (e.g., `TestProvisionSchemaCreatesRoleAndSchema` uses `pgxpool.New(ctx, pgEnv.ConnStr)`), replace `pgEnv.ConnStr` with `holomushConnStr`.

- [ ] **Step 3: Remove the `testutil.StartPostgres` import if no longer used**

Check that no remaining code in this file calls `testutil.StartPostgres` directly. The `testutil` import is still needed for `SharedPostgres` and `RawDatabase`.

- [ ] **Step 4: Add `"fmt"`, `"strings"`, and `"net/url"` to imports if not already present**

The `setupSchemaTestDB` and `extractDBName` helpers use these packages.

- [ ] **Step 5: Run the tests**

Run: `task test:int -- -run 'TestSchemaProvisioner|TestProvisionSchema|TestPluginRole|TestCrossPlugin|TestIdempotent|TestPurgeSchema' ./test/integration/plugin/`
Expected: all 8 tests pass

- [ ] **Step 6: Commit**

```text
refactor(test): share container in schema isolation tests (8 → 0 new containers)

All schema isolation tests now use SharedPostgres + RawDatabase
instead of creating individual containers. Each test gets a blank
database with the holomush role granted, preserving full control
over role creation and schema provisioning.
```

---

## Task 6: Convert `test/integration/plugin/binary_plugin_test.go` (4 → 0 containers)

**Files:**

- Modify: `test/integration/plugin/binary_plugin_test.go`

This file has multiple Ginkgo `Describe` blocks, each with its own `BeforeEach`/`AfterEach` that starts and terminates a container. The conversion lifts the container to a package-level shared singleton and uses `FreshDatabase` in each `BeforeEach`.

- [ ] **Step 1: Check if the plugin package already has a suite-level setup**

Look for any existing `BeforeSuite`/`AfterSuite` in the `test/integration/plugin/` package. If there isn't one, the `SharedPostgres` call can go at the top of each `Describe` block's `BeforeEach`.

Since there's no `BeforeSuite` in this package (it's all standalone `testing.T` and Ginkgo `Describe` blocks), use `SharedPostgres(GinkgoT())` inside each `BeforeEach`.

- [ ] **Step 2: Convert each `Describe` block**

For each block that has the pattern:

```go
BeforeEach(func() {
    // ...
    pgEnv, err := testutil.StartPostgres(ctx)
    Expect(err).NotTo(HaveOccurred())
    container = pgEnv.Container
    connStr = pgEnv.ConnStr

    migrator, err := store.NewMigrator(connStr)
    Expect(err).NotTo(HaveOccurred())
    Expect(migrator.Up()).To(Succeed())
    _ = migrator.Close()
})

AfterEach(func() {
    if container != nil {
        _ = container.Terminate(context.Background())
    }
    // ...
})
```

Replace with:

```go
BeforeEach(func() {
    // ...
    shared := testutil.SharedPostgres(GinkgoT())
    connStr = testutil.FreshDatabase(GinkgoT(), shared)
})

AfterEach(func() {
    // Remove container termination — FreshDatabase handles DB cleanup via t.Cleanup
    // ...
})
```

Remove the `container` variable declaration from each block's `var` section if it's no longer referenced.

Apply this to all 4 `Describe` blocks that create containers (lines ~107, ~396, ~568, ~872).

- [ ] **Step 3: Remove `testcontainers` import if no longer used**

If no code in this file references `testcontainers.Container` anymore, remove the import.

- [ ] **Step 4: Run the tests**

Run: `task test:int -- ./test/integration/plugin/ -v --timeout 5m`
Expected: all tests pass (may take a while due to binary plugin compilation)

- [ ] **Step 5: Commit**

```text
refactor(test): share container in binary plugin tests (4 → 0 new containers)

All Describe blocks in binary_plugin_test.go now use SharedPostgres +
FreshDatabase instead of per-block container creation. Each block
gets a fresh migrated database via template copy.
```

---

## Task 7: Convert `test/integration/plugin/abac_widget_test.go` (4 → 0 containers)

**Files:**

- Modify: `test/integration/plugin/abac_widget_test.go`

Same pattern as Task 6. Each `Describe` block's `BeforeEach` currently starts a container.

- [ ] **Step 1: Convert each `Describe` block**

Replace each `StartPostgres` + migration + `AfterEach` terminate pattern with `SharedPostgres` + `FreshDatabase`, same as Task 6. Apply to all 4 blocks (~lines 139, 254, 415, 553).

- [ ] **Step 2: Remove `container` variables and `testcontainers` import if unused**

- [ ] **Step 3: Run the tests**

Run: `task test:int -- -run '' ./test/integration/plugin/ -v --timeout 5m`
Expected: all tests pass

- [ ] **Step 4: Commit**

```text
refactor(test): share container in ABAC widget tests (4 → 0 new containers)
```

---

## Task 8: Convert `test/integration/telnet/e2e_test.go` (N → 0 new containers)

**Files:**

- Modify: `test/integration/telnet/e2e_test.go`

This is the E2E telnet test that creates a container in `BeforeEach` — once per test case. Lift to suite level.

- [ ] **Step 1: Add a suite-level shared container**

Add a package-level variable and `BeforeSuite`:

```go
var sharedPG *testutil.PostgresEnv

var _ = BeforeSuite(func() {
    sharedPG = testutil.SharedPostgres(GinkgoT())
})
```

- [ ] **Step 2: Convert `BeforeEach` to use `FreshDatabase`**

In the `BeforeEach` (around line 276), replace:

```go
pgEnv, err := testutil.StartPostgres(testCtx)
Expect(err).NotTo(HaveOccurred())
container = pgEnv.Container
connStr := pgEnv.ConnStr

migrator, err := store.NewMigrator(connStr)
Expect(err).NotTo(HaveOccurred())
Expect(migrator.Up()).To(Succeed())
_ = migrator.Close()
```

With:

```go
connStr := testutil.FreshDatabase(GinkgoT(), sharedPG)
```

- [ ] **Step 3: Remove container termination from `AfterEach`**

The `AfterEach` currently terminates `container`. Remove that block. The `container` variable can be removed from the `var` declaration.

- [ ] **Step 4: Remove `testcontainers` import if unused**

- [ ] **Step 5: Run the tests**

Run: `task test:int -- ./test/integration/telnet/ -v --timeout 5m`
Expected: all E2E tests pass

- [ ] **Step 6: Commit**

```text
refactor(test): share container in telnet E2E tests (N → 0 new containers)

Lifts PostgreSQL container to BeforeSuite. Each test case gets a
fresh migrated database via FreshDatabase (~5ms vs ~3-5s per
container start).
```

---

## Task 9: Convert `internal/store/migrations_audit_shape_integration_test.go` (3 → 0 containers)

**Files:**

- Modify: `internal/store/migrations_audit_shape_integration_test.go`

These migration tests need `RawDatabase` since they test migration shape at specific versions.

- [ ] **Step 1: Convert all 3 test functions**

Each test currently does:

```go
pgEnv, err := testutil.StartPostgres(ctx)
require.NoError(t, err)
t.Cleanup(func() { _ = pgEnv.Terminate(context.Background()) })

migrator, err := store.NewMigrator(pgEnv.ConnStr)
```

Replace with:

```go
shared := testutil.SharedPostgres(t)
connStr := testutil.RawDatabase(t, shared)

migrator, err := store.NewMigrator(connStr)
```

Apply to:

- `TestMigration000005AuditSourceComponentAppliesCleanly` (line 28)
- `TestMigration000005AuditSourceComponentRollbackReturnsSchemaToOriginalShape` (line 69)
- `TestMigration000005AuditSourceComponentBackfillsExistingRows` (line 134)

Note: the `NewMigrator` call takes a connection string. `RawDatabase` returns a postgres-superuser connection string. `NewMigrator` converts `postgres://` to `pgx5://` internally, so this works. But the `sql.Open("pgx", pgEnv.ConnStr)` calls later in each test also need the raw database's connection string. Replace `pgEnv.ConnStr` with `connStr` throughout each test.

- [ ] **Step 2: Run the tests**

Run: `task test:int -- -run 'TestMigration000005' ./internal/store/`
Expected: all 3 tests pass

- [ ] **Step 3: Commit**

```text
refactor(test): share container in migration shape tests (3 → 0 new containers)

Migration tests use SharedPostgres + RawDatabase for full control
over migration lifecycle while sharing the container.
```

---

## Task 10: Convert `internal/store/postgres_integration_test.go` (1 → 0 containers)

**Files:**

- Modify: `internal/store/postgres_integration_test.go`

This file has a `setupPostgresContainer` helper used by Ginkgo specs.

- [ ] **Step 1: Rewrite `setupPostgresContainer` to use shared helpers**

Replace the body of `setupPostgresContainer`:

Before:

```go
func setupPostgresContainer() (*store.PostgresEventStore, func(), error) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		return nil, nil, err
	}

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	// ... migrations, create event store, build cleanup ...
```

After:

```go
func setupPostgresContainer() (*store.PostgresEventStore, func(), error) {
	ctx := context.Background()

	shared := testutil.SharedPostgres(GinkgoT())
	connStr := testutil.FreshDatabase(GinkgoT(), shared)

	eventStore, err := store.NewPostgresEventStore(ctx, connStr)
	if err != nil {
		return nil, nil, err
	}

	cleanup := func() {
		eventStore.Close()
		// Database cleanup handled by FreshDatabase's t.Cleanup
	}
	return eventStore, cleanup, nil
}
```

- [ ] **Step 2: Run the tests**

Run: `task test:int -- ./internal/store/ -v`
Expected: all tests pass

- [ ] **Step 3: Commit**

```text
refactor(test): share container in postgres event store tests (1 → 0 new containers)
```

---

## Task 11: Convert `internal/world/postgres/postgres_test.go` (1 → 0 containers)

**Files:**

- Modify: `internal/world/postgres/postgres_test.go`

This uses `TestMain` to start one container for the package.

- [ ] **Step 1: Rewrite `TestMain`**

Before:

```go
func TestMain(m *testing.M) {
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	if err != nil {
		panic("failed to start postgres container: " + err.Error())
	}

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	// ... migrations, create pool, build cleanup ...

	testPool = pool
	testCleanup = func() {
		pool.Close()
		_ = pgEnv.Terminate(ctx)
	}

	code := m.Run()
	testCleanup()
	os.Exit(code)
}
```

`TestMain` runs before any `testing.T` exists, so we can't call `SharedPostgres(t)`. Instead, call `StartPostgres` directly (this package gets its own container regardless since it's a separate binary). But we can still use `FreshDatabase` in individual tests if we want per-test isolation.

For this package, the simplest conversion: keep `TestMain` but have it use the `SharedPostgres`-style setup internally. Since `TestMain` doesn't have a `testing.TB`, call `StartPostgres` and store the env. Then each test that needs isolation can call `FreshDatabase`.

Actually, the cleanest approach for `TestMain`-based packages: keep the existing pattern but aware that `SharedPostgres` won't work from `TestMain`. These packages already use one container per package — no container reduction possible. The only value would be per-test database isolation, but these packages share a single pool and clean up between tests via `DELETE FROM` statements.

**Decision:** Skip conversion for `TestMain`-based packages (`internal/world/postgres/`, `internal/auth/postgres/`) — they already use one container per package and the `TestMain` pattern doesn't have a `testing.TB` for `SharedPostgres`. No container savings. Mark as no-op.

- [ ] **Step 1: No changes needed**

These packages already achieve one container per binary. Mark this task complete.

- [ ] **Step 2: Commit**

No commit needed — no changes.

---

## Task 12: Convert `internal/auth/postgres/postgres_test.go` (1 → 0 containers)

Same as Task 11 — `TestMain` pattern, already one container per package, no savings.

- [ ] **Step 1: No changes needed**

---

## Task 13: Convert `internal/content/postgres_store_test.go` (1 → 0 containers)

**Files:**

- Modify: `internal/content/postgres_store_test.go`

- [ ] **Step 1: Rewrite `setupPool` helper**

Before:

```go
func setupPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()

	pgEnv, err := testutil.StartPostgres(ctx)
	require.NoError(t, err)

	migrator, err := store.NewMigrator(pgEnv.ConnStr)
	// ...

	cleanup := func() {
		pool.Close()
		_ = pgEnv.Terminate(ctx)
	}
	return pool, cleanup
}
```

After:

```go
func setupPool(t *testing.T) (*pgxpool.Pool, func()) {
	t.Helper()
	ctx := context.Background()

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)

	cleanup := func() {
		pool.Close()
		// Database cleanup handled by FreshDatabase's t.Cleanup
	}
	return pool, cleanup
}
```

Remove the `store` import if `NewMigrator` is no longer called in this file.

- [ ] **Step 2: Run the tests**

Run: `task test:int -- ./internal/content/ -v`
Expected: all tests pass

- [ ] **Step 3: Commit**

```text
refactor(test): share container in content store tests (1 → 0 new containers)
```

---

## Task 14: Convert `plugins/core-scenes/store_integration_test.go` (1 → 0 containers)

**Files:**

- Modify: `plugins/core-scenes/store_integration_test.go`

- [ ] **Step 1: Rewrite `newTestStore` helper**

Replace the `StartPostgres` + migrations + `t.Cleanup(Terminate)` pattern with:

```go
func newTestStore(t *testing.T) *SceneStore {
	t.Helper()

	setupCtx, cancelSetup := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancelSetup)

	shared := testutil.SharedPostgres(t)
	connStr := testutil.FreshDatabase(t, shared)

	store, err := NewSceneStore(setupCtx, connStr)
	require.NoError(t, err, "failed to open scene store")
	t.Cleanup(store.Close)

	return store
}
```

Note: `NewSceneStore` runs its own migrations (plugin-specific tables). Since `FreshDatabase` provides a database with the core migrations already applied, and `NewSceneStore` adds plugin-specific tables on top, this should work. Verify by checking what `NewSceneStore` does internally.

- [ ] **Step 2: Run the tests**

Run: `task test:int -- ./plugins/core-scenes/ -v`
Expected: all tests pass

- [ ] **Step 3: Commit**

```text
refactor(test): share container in core-scenes plugin tests (1 → 0 new containers)
```

---

## Task 15: Convert Ginkgo Suite Files (7 suites, mechanical)

**Files:**

- Modify: `test/integration/world/world_suite_test.go`
- Modify: `test/integration/auth/auth_suite_test.go`
- Modify: `test/integration/access/access_suite_test.go`
- Modify: `test/integration/session/session_persistence_suite_test.go`
- Modify: `test/integration/content/content_integration_test.go`
- Modify: `test/integration/audit/audit_integration_test.go`
- Modify: `test/integration/settings/game_settings_integration_test.go`

All follow the same pattern. Each suite's `BeforeSuite` calls `StartPostgres`, runs migrations, creates stores. `AfterSuite` calls `container.Terminate`.

- [ ] **Step 1: Convert world suite (`world_suite_test.go`)**

In `setupWorldTestEnv`, replace:

```go
pgEnv, err := testutil.StartPostgres(ctx)
if err != nil {
    return nil, err
}
container := pgEnv.Container
connStr := pgEnv.ConnStr

migrator, err := store.NewMigrator(connStr)
// ... migrator.Up() ...
```

With:

```go
shared := testutil.SharedPostgres(GinkgoT())
connStr := testutil.FreshDatabase(GinkgoT(), shared)
```

Remove `container` from the `testEnv` struct and `cleanup()` method. Remove `testcontainers` import.

Update `cleanup()`:

```go
func (e *testEnv) cleanup() {
    if e.pool != nil {
        e.pool.Close()
    }
    if e.eventStore != nil {
        e.eventStore.Close()
    }
    // Database and container cleanup handled by testutil
}
```

- [ ] **Step 2: Convert auth suite (`auth_suite_test.go`)**

Same pattern — replace `StartPostgres` + migrations with `SharedPostgres` + `FreshDatabase`. Remove container termination from `AfterSuite`.

- [ ] **Step 3: Convert access suite (`access_suite_test.go`)**

Same pattern.

- [ ] **Step 4: Convert session suite (`session_persistence_suite_test.go`)**

Same pattern. Remove `container` from `suiteEnv` struct and `AfterSuite`.

- [ ] **Step 5: Convert content integration test (`content_integration_test.go`)**

Same pattern.

- [ ] **Step 6: Convert audit integration test (`audit_integration_test.go`)**

Same pattern.

- [ ] **Step 7: Convert settings integration test (`game_settings_integration_test.go`)**

Same pattern.

- [ ] **Step 8: Run all integration tests**

Run: `task test:int`
Expected: all integration tests pass

- [ ] **Step 9: Commit**

```text
refactor(test): share container across all Ginkgo integration suites

Converts 7 Ginkgo suites to use SharedPostgres + FreshDatabase.
Each suite gets a fresh migrated database from the shared container.
Container and database lifecycle managed by testutil.
```

---

## Task 16: Convert Remaining Standalone Tests

**Files:**

- Modify: `test/integration/phase1_5_test.go`
- Modify: `test/integration/plugin/alias_seeder_test.go`

- [ ] **Step 1: Convert `phase1_5_test.go`**

Replace `StartPostgres` + migrations + `Terminate` with `SharedPostgres` + `FreshDatabase`.

- [ ] **Step 2: Convert `alias_seeder_test.go`**

Same pattern.

- [ ] **Step 3: Run the tests**

Run: `task test:int -- ./test/integration/... -v --timeout 10m`
Expected: all tests pass

- [ ] **Step 4: Commit**

```text
refactor(test): share container in remaining integration tests
```

---

## Task 17: Update `StartPostgres` Documentation

**Files:**

- Modify: `test/testutil/postgres.go`

- [ ] **Step 1: Update `StartPostgres` doc comment**

```go
// StartPostgres starts a dedicated PostgreSQL 18 container with a
// non-superuser holomush role (LOGIN, CREATEROLE). The returned
// ConnStr uses holomush:holomush credentials.
//
// Most tests should use SharedPostgres + FreshDatabase instead,
// which shares a single container per test binary and creates
// per-test databases via template copy. Use StartPostgres only
// when a test needs complete process-level container isolation
// (none currently).
//
// Callers are responsible for calling Terminate and, if needed,
// running migrations via store.NewMigrator.
func StartPostgres(ctx context.Context) (*PostgresEnv, error) {
```

- [ ] **Step 2: Commit**

```text
docs(testutil): update StartPostgres comment to recommend SharedPostgres
```

---

## Task 18: Full Test Run & Verification

- [ ] **Step 1: Run full integration test suite**

Run: `task test:int`
Expected: all tests pass

- [ ] **Step 2: Run `task pr-prep`**

Run: `task pr-prep`
Expected: all CI checks pass (lint, format, schema, license, unit, integration, E2E)

- [ ] **Step 3: Verify container reduction**

Add temporary logging or observe Docker container count during test run. The expected result: ~16 containers (one per package binary) instead of ~34.

- [ ] **Step 4: Commit any remaining fixes**

If any tests needed adjustment, commit the fixes.
