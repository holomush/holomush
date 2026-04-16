# Shared PostgreSQL Test Containers

**Status:** Draft
**Date:** 2026-04-13
**Author:** Sean Brandt + Claude Opus 4.6

## Problem

Integration and E2E tests currently spin up ~34 PostgreSQL containers across
a full test run. Each container takes 3-5 seconds to start (image pull cached),
plus role creation and migration time. This causes significant CPU churn and
wall-clock overhead in both CI and local development.

The worst offenders:

| Package | Containers |
|---------|-----------|
| `test/integration/plugin/` (schema isolation, binary, ABAC widget) | ~16 |
| `test/integration/telnet/` (E2E, one per `BeforeEach`) | N per test case |
| `internal/store/` (migrations audit shape) | 4 |
| Remaining scattered callers | ~7 |

## Solution

Share a single PostgreSQL container per Go test binary (per package) using a
`sync.Once` singleton. Provide per-test database isolation via PostgreSQL's
`CREATE DATABASE ... TEMPLATE` mechanism.

## Design

### API Surface

Three helpers in `test/testutil/`, ordered by most to least common usage:

#### `FreshDatabase(t testing.TB, env *PostgresEnv) string`

The primary API for 90% of tests. Returns a connection string to a freshly
created, fully migrated database on the shared container.

- Connects as `postgres` superuser to the shared container
- On first call (per container): creates a **template database**
  (`holomush_template`), runs all migrations via `store.NewMigrator`, marks
  it with `ALTER DATABASE holomush_template IS_TEMPLATE true`
  (protected by `sync.Once`)
- Creates `test_<8-hex-chars>` database using `TEMPLATE holomush_template
  OWNER holomush`
- Registers `t.Cleanup` to `DROP DATABASE ... WITH (FORCE)`
- Returns connection string with `holomush:holomush` credentials

Database names use 8 random hex characters from `crypto/rand`. These are
ephemeral test databases, not domain entities — ULID/`idgen` conventions do
not apply.

Template copy is ~5-10ms vs ~200-500ms for running migrations. This means
migrations execute **once per container** regardless of test count.

#### `RawDatabase(t testing.TB, env *PostgresEnv) string`

For tests that need full control: migration tests, schema isolation tests,
CREATEROLE tests.

- Creates a blank database owned by `postgres` (no template, no migrations,
  no `holomush` role)
- Returns a **superuser** (`postgres`) connection string
- Registers `t.Cleanup` to drop the database
- Caller is responsible for running migrations, creating roles, etc.
- The shared container's `holomush` role exists at the cluster level but has
  no privileges on the raw database — callers MUST grant access explicitly
  if needed

Use cases:

- `internal/store/migrations_audit_shape_integration_test.go` — tests
  migration shape at specific versions
- `test/integration/plugin/schema_isolation_test.go` — tests CREATEROLE
  behavior and cross-schema access denial
- Any future test that validates `Down()` migrations or partial migration
  state

#### `SharedPostgres(t testing.TB) *PostgresEnv`

Package-level singleton. Starts one PostgreSQL 18 Alpine container per test
binary via `sync.Once`. All callers within the same binary receive the same
`*PostgresEnv`.

- Uses the same `startPostgresOnce` logic as today (retry with backoff,
  wait for ready, create `holomush` role with CREATEROLE)
- Container cleanup relies on testcontainers' **Ryuk reaper**, which
  auto-removes containers when the test process exits. `SharedPostgres`
  MUST NOT register `t.Cleanup(env.Terminate)` because the first caller's
  `t` may finish before the binary exits, killing the container prematurely.
- The `t` parameter is used for `t.Helper()` and `t.Fatal()` on startup
  failure only

#### `StartPostgres(ctx context.Context) (*PostgresEnv, error)` (existing)

Kept as-is for the rare case that truly needs process-level container
isolation. No current tests require this after migration, but the function
remains as an escape hatch. Doc comment updated to direct callers toward
`SharedPostgres` + `FreshDatabase` as the default.

### Container Lifecycle

```text
Test binary starts
  └─ First call to SharedPostgres(t)
       └─ sync.Once: startPostgresOnce(ctx) → *PostgresEnv
            └─ postgres:18-alpine container started
            └─ holomush role created (LOGIN, CREATEROLE)
            └─ Ryuk reaper registered for cleanup

  └─ First call to FreshDatabase(t, env)
       └─ sync.Once: create holomush_template database
            └─ Run all migrations
            └─ ALTER DATABASE holomush_template IS_TEMPLATE true

  └─ Each test: FreshDatabase(t, env)
       └─ CREATE DATABASE test_<hex> TEMPLATE holomush_template OWNER holomush
       └─ t.Cleanup: DROP DATABASE test_<hex> WITH (FORCE)

Test binary exits
  └─ Ryuk reaper removes the container
```

### Caller Migration Patterns

#### Ginkgo Suites (world, auth, access, session, content, audit, settings)

```go
var sharedPG *testutil.PostgresEnv
var suiteConnStr string

var _ = BeforeSuite(func() {
    sharedPG = testutil.SharedPostgres(GinkgoT())
    suiteConnStr = testutil.FreshDatabase(GinkgoT(), sharedPG)
    // ... create stores, engines, etc. using suiteConnStr
})

// AfterSuite no longer needs explicit Terminate
```

These suites already share one container per package. The change is mechanical:
`StartPostgres` → `SharedPostgres`, and `AfterSuite` drops the `Terminate`
call.

#### Standalone `testing.T` Tests (plugin, store, auth/postgres, world/postgres)

```go
func TestSomething(t *testing.T) {
    shared := testutil.SharedPostgres(t)
    connStr := testutil.FreshDatabase(t, shared)
    // ... test logic ...
}
```

This is the high-impact change. Plugin tests go from 16 containers to 1.

#### E2E Telnet Tests (Ginkgo `BeforeEach`)

```go
var sharedPG *testutil.PostgresEnv

var _ = BeforeSuite(func() {
    sharedPG = testutil.SharedPostgres(GinkgoT())
})

BeforeEach(func() {
    connStr := testutil.FreshDatabase(GinkgoT(), sharedPG)
    // ... boot server with this connStr ...
})
```

Each E2E test case gets a clean database via template copy (~5-10ms) instead
of a new container (~3-5s).

#### Migration Tests

```go
func TestMigrationAuditTableShape(t *testing.T) {
    shared := testutil.SharedPostgres(t)
    connStr := testutil.RawDatabase(t, shared)

    migrator, err := store.NewMigrator(connStr)
    require.NoError(t, err)
    require.NoError(t, migrator.Up())
    // ... assert schema shape ...
}
```

Full control over migration lifecycle. Can run partial migrations, test
`Down()`, etc.

#### Schema Isolation Tests

```go
func TestSchemaProvisionerCreatesIsolatedSchema(t *testing.T) {
    shared := testutil.SharedPostgres(t)
    connStr := testutil.RawDatabase(t, shared)  // superuser, no migrations

    // Create custom roles, test CREATEROLE, cross-schema denial, etc.
}
```

These tests manipulate PostgreSQL roles and schemas directly. They share the
container but get a blank database with superuser access.

### Expected Impact

| Package | Before | After | Savings |
|---------|--------|-------|---------|
| `test/integration/plugin/` | ~16 | 1 | 15 containers |
| `test/integration/telnet/` | N | 1 | N-1 containers |
| `internal/store/` | 4 | 1 | 3 containers |
| `internal/world/postgres/` | 1 | 1 | 0 |
| `internal/auth/postgres/` | 1 | 1 | 0 |
| `internal/content/` | 1 | 1 | 0 |
| `internal/access/policy/store/` | 1 | 1 | 0 |
| `plugins/core-scenes/` | 1 | 1 | 0 |
| Ginkgo suites (7 packages) | 7 | 7 | 0 |
| `test/integration/phase1_5_test.go` | 1 | 1 | 0 |
| **Total** | **~34** | **~16** | **~18 containers** |

The 16 remaining containers are one per package binary — the irreducible
minimum with per-package singleton approach. Migration runtime drops from
~34 executions to ~16 (one per container via template).

### Concurrency Safety

- `SharedPostgres`: `sync.Once` ensures exactly one container per binary
- Template creation: second `sync.Once` inside the shared env
- `FreshDatabase`/`RawDatabase`: each creates a uniquely-named database;
  PostgreSQL serializes `CREATE DATABASE` internally
- No shared mutable state between tests beyond the container process

### Non-Goals

- Cross-package container sharing (Approach B) — deferred, evaluate later
  if per-package singleton is insufficient
- Docker Compose orchestration (Approach C) — deferred
- Changing the `postgres:18-alpine` image or PostgreSQL configuration
- Connection pooling changes (pgxpool usage within tests stays as-is)

### Migration Strategy

Incremental, one package at a time. Each conversion is a standalone PR.

**Order** (highest impact first):

1. Implement `SharedPostgres`, `FreshDatabase`, `RawDatabase` in `testutil`
2. Convert `test/integration/plugin/` (16 → 1 containers)
3. Convert `test/integration/telnet/` (N → 1)
4. Convert `internal/store/` (4 → 1)
5. Convert remaining `internal/` callers (auth, world, content)
6. Convert Ginkgo suites (mechanical, low impact)
7. Update `StartPostgres` doc comment

Each step is independently shippable. Tests MUST pass after each conversion.
