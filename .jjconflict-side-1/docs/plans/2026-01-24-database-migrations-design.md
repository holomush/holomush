# Database Migrations Design

**Status:** Implemented (PR #43)
**Date:** 2026-01-24
**Task:** holomush-c83

## Overview

This document defines the migration system for HoloMUSH using golang-migrate. The design
replaces the current hand-rolled migration approach with a production-ready library that
provides version tracking, rollback support, and concurrent execution safety.

### Goals

- CLI tooling for migration management (`up`, `down`, `status`, `create`)
- Version tracking with dirty state detection
- Rollback capability with required down migrations
- Advisory locking for safe concurrent execution
- Task wrapper for developer workflow consistency
- Auto-migrate option for simplified deployments

### Non-Goals

- Plugin schema migrations (deferred — no concrete use case yet)
- Migration squashing (not needed until 50+ migrations)
- Go-based data migrations (SQL-only for v1)
- Blue/green zero-downtime migrations (address for production deployment)

## Architecture

### Library Choice: golang-migrate

Selected over alternatives (goose, hand-rolled) for:

- PostgreSQL advisory lock support — prevents concurrent migration runs
- pgx5 driver — no abstraction penalty over current pgxpool usage
- Dirty state handling — recovers from failed migrations
- Mature ecosystem — 13k+ GitHub stars, active maintenance

### Migration File Structure

```text
internal/store/migrations/
  000001_initial.up.sql
  000001_initial.down.sql
  000002_system_info.up.sql
  000002_system_info.down.sql
  000003_world_model.up.sql
  000003_world_model.down.sql
  ...
```

**Naming convention:** `{version}_{description}.{direction}.sql`

- Version: 6-digit zero-padded sequential number
- Description: snake_case, descriptive
- Direction: `up` or `down`

### Version Tracking

golang-migrate creates a `schema_migrations` table:

```sql
CREATE TABLE schema_migrations (
    version bigint NOT NULL PRIMARY KEY,
    dirty boolean NOT NULL
);
```

The `dirty` flag marks failed migrations. Further migrations are blocked until the dirty
state is resolved manually with `migrate force VERSION`.

### Package Structure

```text
internal/store/
  migrations/
    000001_initial.up.sql
    000001_initial.down.sql
    ...
  migrate.go          # Migration runner using golang-migrate
  postgres.go         # Remove current Migrate() method
```

## API Design

### Migrator Interface

```go
// migrate.go
package store

import (
    "embed"
    "github.com/golang-migrate/migrate/v4"
    _ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
    "github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type Migrator struct {
    m *migrate.Migrate  // Note: Implementation uses migrateIface interface for testability
}

func NewMigrator(databaseURL string) (*Migrator, error)
func (m *Migrator) Up() error                     // Apply all pending
func (m *Migrator) Down() error                   // Rollback all to version 0
func (m *Migrator) Steps(n int) error             // +n up, -n down
func (m *Migrator) Version() (uint, bool, error)  // Current version, dirty flag
func (m *Migrator) Force(version int) error       // Force set version
func (m *Migrator) Close() error
```

> **Note:** The `Down()` method rolls back **all** migrations to version 0. However, the
> CLI `migrate down` command defaults to rolling back **one** migration (using `Steps(-1)`)
> for safety. Use `migrate down --all` to match `Down()` behavior.

### CLI Commands

```text
holomush migrate              # Apply all pending migrations (default: up)
holomush migrate up           # Apply all pending migrations
holomush migrate down         # Rollback one migration
holomush migrate down --all   # Rollback all migrations
holomush migrate status       # Show current version and pending count
holomush migrate version      # Print current schema version
holomush migrate force N      # Force set version (dirty state recovery)
```

### Task Wrapper

```yaml
vars:
  MIGRATE_CLI: migrate
  MIGRATIONS_DIR: internal/store/migrations

tasks:
  migrate:
    desc: Apply all pending migrations
    cmds:
      - go run {{.MAIN_PKG}} migrate up

  migrate:up:
    desc: Apply all pending migrations
    cmds:
      - go run {{.MAIN_PKG}} migrate up

  migrate:down:
    desc: Rollback one migration
    cmds:
      - go run {{.MAIN_PKG}} migrate down

  migrate:status:
    desc: Show migration status
    cmds:
      - go run {{.MAIN_PKG}} migrate status

  migrate:create:
    desc: Create a new migration (usage: task migrate:create -- migration_name)
    cmds:
      - "{{.MIGRATE_CLI}} create -ext sql -dir {{.MIGRATIONS_DIR}} -seq {{.CLI_ARGS}}"
    preconditions:
      - sh: command -v {{.MIGRATE_CLI}}
        msg: "migrate CLI not found. Install: go install -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@latest"

  migrate:force:
    desc: Force set migration version (usage: task migrate:force -- VERSION)
    cmds:
      - go run {{.MAIN_PKG}} migrate force {{.CLI_ARGS}}
```

### Auto-Migrate on Startup

Optional flag for simplified deployments:

```go
// cmd/holomush/root.go
var autoMigrate bool

// In server startup
if autoMigrate {
    migrator, _ := store.NewMigrator(databaseURL)
    if err := migrator.Up(); err != nil && err != migrate.ErrNoChange {
        return err
    }
}
```

Default: auto-migration enabled (set `HOLOMUSH_DB_AUTO_MIGRATE=false` to disable).

## Migration Conversion

### Current State

Seven migrations using `CREATE IF NOT EXISTS` pattern, embedded individually with
`//go:embed` per file.

### Conversion Plan

| Current File                             | New Up File                                    | New Down File                                    |
| ---------------------------------------- | ---------------------------------------------- | ------------------------------------------------ |
| `001_initial.sql`                        | `000001_initial.up.sql`                        | `000001_initial.down.sql`                        |
| `002_system_info.sql`                    | `000002_system_info.up.sql`                    | `000002_system_info.down.sql`                    |
| `003_world_model.sql`                    | `000003_world_model.up.sql`                    | `000003_world_model.down.sql`                    |
| `004_pg_trgm.sql`                        | `000004_pg_trgm.up.sql`                        | `000004_pg_trgm.down.sql`                        |
| `005_pg_stat_statements.sql`             | `000005_pg_stat_statements.up.sql`             | `000005_pg_stat_statements.down.sql`             |
| `006_object_containment_constraint.sql`  | `000006_object_containment_constraint.up.sql`  | `000006_object_containment_constraint.down.sql`  |
| `007_exit_self_reference_constraint.sql` | `000007_exit_self_reference_constraint.up.sql` | `000007_exit_self_reference_constraint.down.sql` |

**Up migrations:** Copy existing content. Keep `IF NOT EXISTS` for transition safety.

**Down migrations:** Write reversals:

- `CREATE TABLE` → `DROP TABLE IF EXISTS`
- `CREATE INDEX` → `DROP INDEX IF EXISTS`
- `INSERT ... ON CONFLICT DO NOTHING` → `DELETE FROM ... WHERE id = ...`

### Bootstrapping Existing Databases

For databases already migrated with the old system:

```sql
-- One-time bootstrap script
INSERT INTO schema_migrations (version, dirty) VALUES (7, false);
```

Fresh databases run `task migrate` from version 0.

### Transition Steps

1. Convert all 7 migrations to up/down format
2. Add bootstrap SQL script for existing deployments
3. Remove old `Migrate()` method from `PostgresEventStore`
4. Update `cmd/holomush/migrate.go` to use new `Migrator`
5. Update Taskfile with migration tasks

## Testing Strategy

### Unit Tests

| Test                          | Purpose                                |
| ----------------------------- | -------------------------------------- |
| `TestMigrator_Up`             | Applies pending migrations             |
| `TestMigrator_Down`           | Rolls back one migration               |
| `TestMigrator_Version`        | Returns correct version and dirty flag |
| `TestMigrator_DirtyState`     | Handles dirty flag correctly           |
| `TestMigrator_ConcurrentLock` | Advisory lock prevents concurrent runs |

### Integration Tests

Use testcontainers PostgreSQL:

- Apply all migrations, verify schema objects exist
- Rollback all, verify clean state
- Test concurrent migration attempts (expect one to wait or fail gracefully)

### CI Validation

- `task migrate:status` verifies migrations are valid SQL
- Consider `migrate validate` step to check up/down pairs exist

## Dependencies

```go
require (
    github.com/golang-migrate/migrate/v4 v4.17.0
)
```

## Future Considerations

| Feature                  | Status   | Rationale                                                       |
| ------------------------ | -------- | --------------------------------------------------------------- |
| Plugin schema migrations | Deferred | No concrete use case; Lua plugins use events, not custom tables |
| Migration squashing      | Deferred | Not needed until migration count exceeds 50                     |
| Go-based data migrations | Deferred | SQL-only sufficient for schema changes                          |
| Blue/green migrations    | Deferred | Requires zero-downtime patterns for production                  |

## References

- [golang-migrate documentation](https://github.com/golang-migrate/migrate)
- [PostgreSQL advisory locks](https://www.postgresql.org/docs/current/explicit-locking.html#ADVISORY-LOCKS)
