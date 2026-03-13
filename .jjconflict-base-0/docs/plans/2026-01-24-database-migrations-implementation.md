# Database Migrations Implementation Plan

> **Status: Implemented** - This plan was completed in PR #43. The code is the
> source of truth; this document is retained for historical context.
>
> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan
> task-by-task.

**Goal:** Replace hand-rolled migration system with golang-migrate for version tracking,
rollback support, and concurrent execution safety.

**Architecture:** New `Migrator` type wraps golang-migrate library. Existing SQL migrations
split into up/down pairs. CLI commands delegate to `Migrator`. Taskfile wraps CLI for
developer workflow.

**Tech Stack:** golang-migrate v4 with pgx5 driver, embed.FS for bundling migrations

**Epic:** holomush-sg7

---

## Task 1: Add golang-migrate Dependency

**Files:**

- Modify: `go.mod`

**Step 1: Add the dependency**

```bash
go get github.com/golang-migrate/migrate/v4@latest
go get github.com/golang-migrate/migrate/v4/database/pgx5@latest
go get github.com/golang-migrate/migrate/v4/source/iofs@latest
```

**Step 2: Verify dependency added**

Run: `grep golang-migrate go.mod`

Expected: `github.com/golang-migrate/migrate/v4 v4.x.x`

**Step 3: Tidy modules**

```bash
go mod tidy
```

**Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add golang-migrate dependency"
```

---

## Task 2: Convert Migration 001 (Initial Schema)

**Files:**

- Create: `internal/store/migrations/000001_initial.up.sql`
- Create: `internal/store/migrations/000001_initial.down.sql`
- Delete: `internal/store/migrations/001_initial.sql` (after all conversions)

**Step 1: Create up migration**

Copy content from `001_initial.sql` to `000001_initial.up.sql`. Keep `IF NOT EXISTS`
clauses for transition safety.

**Step 2: Create down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000001_initial.up.sql

-- Remove test data first (foreign key constraints)
DELETE FROM characters WHERE id = '01KDVDNA002MB1E60S38DHR78Y';
DELETE FROM locations WHERE id = '01KDVDNA001C60T3GF208H44RM';
DELETE FROM players WHERE id = '01KDVDNA00041061050R3GG28A';

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS sessions;
DROP INDEX IF EXISTS idx_events_stream_id;
DROP TABLE IF EXISTS events;
DROP TABLE IF EXISTS characters;
DROP TABLE IF EXISTS locations;
DROP TABLE IF EXISTS players;
```

**Step 3: Verify SQL syntax**

```bash
# Quick syntax check - will fail to connect but validates SQL parsing
psql --echo-errors -f internal/store/migrations/000001_initial.up.sql 2>&1 | head -5
psql --echo-errors -f internal/store/migrations/000001_initial.down.sql 2>&1 | head -5
```

**Step 4: Commit**

```bash
git add internal/store/migrations/000001_initial.up.sql internal/store/migrations/000001_initial.down.sql
git commit -m "refactor(migrations): convert 001_initial to up/down format"
```

---

## Task 3: Convert Migration 002 (System Info)

**Files:**

- Create: `internal/store/migrations/000002_system_info.up.sql`
- Create: `internal/store/migrations/000002_system_info.down.sql`

**Step 1: Create up migration**

Copy content from `002_system_info.sql` to `000002_system_info.up.sql`.

**Step 2: Create down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000002_system_info.up.sql
DROP TABLE IF EXISTS holomush_system_info;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000002_system_info.up.sql internal/store/migrations/000002_system_info.down.sql
git commit -m "refactor(migrations): convert 002_system_info to up/down format"
```

---

## Task 4: Convert Migration 003 (World Model)

**Files:**

- Create: `internal/store/migrations/000003_world_model.up.sql`
- Create: `internal/store/migrations/000003_world_model.down.sql`

**Step 1: Create up migration**

Copy content from `003_world_model.sql` to `000003_world_model.up.sql`.

**Step 2: Create down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000003_world_model.up.sql

-- Drop indexes first
DROP INDEX IF EXISTS idx_characters_location;
DROP INDEX IF EXISTS idx_scene_participants_character;

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS scene_participants;
DROP INDEX IF EXISTS idx_objects_contained;
DROP INDEX IF EXISTS idx_objects_held_by;
DROP INDEX IF EXISTS idx_objects_location;
DROP TABLE IF EXISTS objects;
DROP INDEX IF EXISTS idx_exits_to;
DROP INDEX IF EXISTS idx_exits_from;
DROP TABLE IF EXISTS exits;

-- Remove columns added to locations (in reverse order)
DROP INDEX IF EXISTS idx_locations_shadows;
DROP INDEX IF EXISTS idx_locations_type;
ALTER TABLE locations DROP COLUMN IF EXISTS archived_at;
ALTER TABLE locations DROP COLUMN IF EXISTS replay_policy;
ALTER TABLE locations DROP COLUMN IF EXISTS owner_id;
ALTER TABLE locations DROP COLUMN IF EXISTS shadows_id;
ALTER TABLE locations DROP COLUMN IF EXISTS type;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000003_world_model.up.sql internal/store/migrations/000003_world_model.down.sql
git commit -m "refactor(migrations): convert 003_world_model to up/down format"
```

---

## Task 5: Convert Migration 004 (pg_trgm)

**Files:**

- Create: `internal/store/migrations/000004_pg_trgm.up.sql`
- Create: `internal/store/migrations/000004_pg_trgm.down.sql`

**Step 1: Create up migration**

Copy content from `004_pg_trgm.sql` to `000004_pg_trgm.up.sql`.

**Step 2: Create down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000004_pg_trgm.up.sql

DROP INDEX IF EXISTS idx_locations_name_trgm;
DROP INDEX IF EXISTS idx_objects_name_trgm;
DROP INDEX IF EXISTS idx_exits_name_trgm;

-- Note: We don't DROP EXTENSION pg_trgm as other schemas might use it.
-- Extensions are shared across the database, not schema-specific.
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000004_pg_trgm.up.sql internal/store/migrations/000004_pg_trgm.down.sql
git commit -m "refactor(migrations): convert 004_pg_trgm to up/down format"
```

---

## Task 6: Convert Migration 005 (pg_stat_statements)

**Files:**

- Create: `internal/store/migrations/000005_pg_stat_statements.up.sql`
- Create: `internal/store/migrations/000005_pg_stat_statements.down.sql`

**Step 1: Create up migration**

Copy content from `005_pg_stat_statements.sql` to `000005_pg_stat_statements.up.sql`.

**Step 2: Create down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000005_pg_stat_statements.up.sql

-- Note: We don't DROP EXTENSION pg_stat_statements as:
-- 1. It may be used by other monitoring tools
-- 2. It requires shared_preload_libraries anyway
-- 3. Dropping it would lose valuable query statistics

-- This is intentionally a no-op for safety.
SELECT 1;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000005_pg_stat_statements.up.sql internal/store/migrations/000005_pg_stat_statements.down.sql
git commit -m "refactor(migrations): convert 005_pg_stat_statements to up/down format"
```

---

## Task 7: Convert Migration 006 (Object Containment Constraint)

**Files:**

- Create: `internal/store/migrations/000006_object_containment_constraint.up.sql`
- Create: `internal/store/migrations/000006_object_containment_constraint.down.sql`

**Step 1: Create up migration**

Copy content from `006_object_containment_constraint.sql` to
`000006_object_containment_constraint.up.sql`.

**Step 2: Create down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000006_object_containment_constraint.up.sql
ALTER TABLE objects DROP CONSTRAINT IF EXISTS chk_exactly_one_containment;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000006_object_containment_constraint.up.sql internal/store/migrations/000006_object_containment_constraint.down.sql
git commit -m "refactor(migrations): convert 006_object_containment_constraint to up/down format"
```

---

## Task 8: Convert Migration 007 (Exit Self-Reference Constraint)

**Files:**

- Create: `internal/store/migrations/000007_exit_self_reference_constraint.up.sql`
- Create: `internal/store/migrations/000007_exit_self_reference_constraint.down.sql`

**Step 1: Create up migration**

Copy content from `007_exit_self_reference_constraint.sql` to
`000007_exit_self_reference_constraint.up.sql`.

**Step 2: Create down migration**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Reverse 000007_exit_self_reference_constraint.up.sql
ALTER TABLE exits DROP CONSTRAINT IF EXISTS chk_not_self_referential;
```

**Step 3: Commit**

```bash
git add internal/store/migrations/000007_exit_self_reference_constraint.up.sql internal/store/migrations/000007_exit_self_reference_constraint.down.sql
git commit -m "refactor(migrations): convert 007_exit_self_reference_constraint to up/down format"
```

---

## Task 9: Implement Migrator Type

**Files:**

- Create: `internal/store/migrate.go`
- Test: `internal/store/migrate_test.go`

**Step 1: Write failing test for NewMigrator**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestNewMigrator_InvalidURL(t *testing.T) {
    _, err := NewMigrator("invalid://url")
    require.Error(t, err)
}

func TestNewMigrator_ValidURL(t *testing.T) {
    // This test requires a real database - skip in unit tests
    t.Skip("requires database connection")
}
```

**Step 2: Run test to verify it fails**

```bash
task test -- -run TestNewMigrator_InvalidURL ./internal/store/...
```

Expected: FAIL (NewMigrator not defined)

**Step 3: Implement Migrator**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store

import (
    "embed"

    "github.com/golang-migrate/migrate/v4"
    _ "github.com/golang-migrate/migrate/v4/database/pgx5"
    "github.com/golang-migrate/migrate/v4/source/iofs"
    "github.com/samber/oops"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrator wraps golang-migrate for database schema management.
type Migrator struct {
    m *migrate.Migrate
}

// NewMigrator creates a new Migrator instance.
// The databaseURL should be a PostgreSQL connection string.
func NewMigrator(databaseURL string) (*Migrator, error) {
    // Create source driver from embedded filesystem
    source, err := iofs.New(migrationsFS, "migrations")
    if err != nil {
        return nil, oops.Code("MIGRATION_SOURCE_FAILED").Wrap(err)
    }

    // Create migrate instance
    m, err := migrate.NewWithSourceInstance("iofs", source, databaseURL)
    if err != nil {
        return nil, oops.Code("MIGRATION_INIT_FAILED").Wrap(err)
    }

    return &Migrator{m: m}, nil
}

// Up applies all pending migrations.
func (m *Migrator) Up() error {
    if err := m.m.Up(); err != nil && err != migrate.ErrNoChange {
        return oops.Code("MIGRATION_UP_FAILED").Wrap(err)
    }
    return nil
}

// Down rolls back all migrations to version 0, effectively removing all schema objects.
// WARNING: This is a destructive operation that drops all tables and data.
func (m *Migrator) Down() error {
    if err := m.m.Down(); err != nil && err != migrate.ErrNoChange {
        return oops.Code("MIGRATION_DOWN_FAILED").Wrap(err)
    }
    return nil
}

// Steps applies n migrations. Positive n migrates up, negative n migrates down.
func (m *Migrator) Steps(n int) error {
    if err := m.m.Steps(n); err != nil && err != migrate.ErrNoChange {
        return oops.Code("MIGRATION_STEPS_FAILED").With("steps", n).Wrap(err)
    }
    return nil
}

// Version returns the current migration version and dirty state.
// Returns 0 if no migrations have been applied.
func (m *Migrator) Version() (version uint, dirty bool, err error) {
    version, dirty, err = m.m.Version()
    if err == migrate.ErrNilVersion {
        return 0, false, nil
    }
    if err != nil {
        return 0, false, oops.Code("MIGRATION_VERSION_FAILED").Wrap(err)
    }
    return version, dirty, nil
}

// Force sets the migration version without running migrations.
// Use this to recover from a dirty state.
func (m *Migrator) Force(version int) error {
    if err := m.m.Force(version); err != nil {
        return oops.Code("MIGRATION_FORCE_FAILED").With("version", version).Wrap(err)
    }
    return nil
}

// Close releases resources.
func (m *Migrator) Close() error {
    srcErr, dbErr := m.m.Close()
    if srcErr != nil {
        return oops.Code("MIGRATION_CLOSE_FAILED").With("component", "source").Wrap(srcErr)
    }
    if dbErr != nil {
        return oops.Code("MIGRATION_CLOSE_FAILED").With("component", "database").Wrap(dbErr)
    }
    return nil
}
```

**Step 4: Run test to verify it passes**

```bash
task test -- -run TestNewMigrator_InvalidURL ./internal/store/...
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/store/migrate.go internal/store/migrate_test.go
git commit -m "feat(store): implement Migrator type with golang-migrate"
```

---

## Task 10: Update CLI migrate Command

**Files:**

- Modify: `cmd/holomush/migrate.go`

**Step 1: Read current implementation**

Review current `cmd/holomush/migrate.go` structure.

**Step 2: Update to use new Migrator**

```go
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package main

import (
    "fmt"
    "os"

    "github.com/samber/oops"
    "github.com/spf13/cobra"

    "github.com/holomush/holomush/internal/store"
)

// NewMigrateCmd creates the migrate subcommand.
func NewMigrateCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "migrate",
        Short: "Database migration management",
        Long:  `Manage database schema migrations using golang-migrate.`,
    }

    cmd.AddCommand(newMigrateUpCmd())
    cmd.AddCommand(newMigrateDownCmd())
    cmd.AddCommand(newMigrateStatusCmd())
    cmd.AddCommand(newMigrateForceCmd())
    cmd.AddCommand(newMigrateVersionCmd())

    return cmd
}

func getDatabaseURL() (string, error) {
    url := os.Getenv("DATABASE_URL")
    if url == "" {
        return "", oops.Code("CONFIG_INVALID").Errorf("DATABASE_URL environment variable is required")
    }
    return url, nil
}

func newMigrateUpCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "up",
        Short: "Apply all pending migrations",
        RunE: func(cmd *cobra.Command, _ []string) error {
            url, err := getDatabaseURL()
            if err != nil {
                return err
            }

            migrator, err := store.NewMigrator(url)
            if err != nil {
                return err
            }
            defer migrator.Close()

            cmd.Println("Applying migrations...")
            if err := migrator.Up(); err != nil {
                return err
            }

            version, _, _ := migrator.Version()
            cmd.Printf("Migrations complete. Current version: %d\n", version)
            return nil
        },
    }
}

func newMigrateDownCmd() *cobra.Command {
    var all bool

    cmd := &cobra.Command{
        Use:   "down",
        Short: "Rollback migrations",
        Long:  `Rollback one migration, or all migrations with --all flag.`,
        RunE: func(cmd *cobra.Command, _ []string) error {
            url, err := getDatabaseURL()
            if err != nil {
                return err
            }

            migrator, err := store.NewMigrator(url)
            if err != nil {
                return err
            }
            defer migrator.Close()

            if all {
                cmd.Println("Rolling back all migrations...")
                if err := migrator.Down(); err != nil {
                    return err
                }
            } else {
                cmd.Println("Rolling back one migration...")
                if err := migrator.Steps(-1); err != nil {
                    return err
                }
            }

            version, _, _ := migrator.Version()
            cmd.Printf("Rollback complete. Current version: %d\n", version)
            return nil
        },
    }

    cmd.Flags().BoolVar(&all, "all", false, "Rollback all migrations")
    return cmd
}

func newMigrateStatusCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "status",
        Short: "Show migration status",
        RunE: func(cmd *cobra.Command, _ []string) error {
            url, err := getDatabaseURL()
            if err != nil {
                return err
            }

            migrator, err := store.NewMigrator(url)
            if err != nil {
                return err
            }
            defer migrator.Close()

            version, dirty, err := migrator.Version()
            if err != nil {
                return err
            }

            cmd.Printf("Current version: %d\n", version)
            if dirty {
                cmd.Println("Status: DIRTY (migration failed, manual intervention required)")
                cmd.Println("Use 'holomush migrate force VERSION' to reset")
            } else {
                cmd.Println("Status: OK")
            }
            return nil
        },
    }
}

func newMigrateVersionCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "version",
        Short: "Print current schema version",
        RunE: func(cmd *cobra.Command, _ []string) error {
            url, err := getDatabaseURL()
            if err != nil {
                return err
            }

            migrator, err := store.NewMigrator(url)
            if err != nil {
                return err
            }
            defer migrator.Close()

            version, _, err := migrator.Version()
            if err != nil {
                return err
            }

            fmt.Println(version)
            return nil
        },
    }
}

func newMigrateForceCmd() *cobra.Command {
    return &cobra.Command{
        Use:   "force VERSION",
        Short: "Force set migration version (for dirty state recovery)",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            url, err := getDatabaseURL()
            if err != nil {
                return err
            }

            var version int
            if _, err := fmt.Sscanf(args[0], "%d", &version); err != nil {
                return oops.Code("INVALID_VERSION").Errorf("invalid version: %s", args[0])
            }

            migrator, err := store.NewMigrator(url)
            if err != nil {
                return err
            }
            defer migrator.Close()

            cmd.Printf("Forcing version to %d...\n", version)
            if err := migrator.Force(version); err != nil {
                return err
            }

            cmd.Println("Version forced successfully")
            return nil
        },
    }
}
```

**Step 3: Verify it compiles**

```bash
task build
```

**Step 4: Commit**

```bash
git add cmd/holomush/migrate.go
git commit -m "feat(cli): update migrate command to use golang-migrate"
```

---

## Task 11: Add Taskfile Migration Commands

**Files:**

- Modify: `Taskfile.yaml`

**Step 1: Add migration task variables and tasks**

Add to `Taskfile.yaml`:

```yaml
vars:
  # ... existing vars ...
  MIGRATE_CLI: migrate
  MIGRATIONS_DIR: internal/store/migrations

tasks:
  # ... existing tasks ...

  # Migration tasks
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

  migrate:version:
    desc: Print current schema version
    cmds:
      - go run {{.MAIN_PKG}} migrate version

  migrate:create:
    desc: Create a new migration (usage: task migrate:create -- migration_name)
    cmds:
      - "{{.MIGRATE_CLI}} create -ext sql -dir {{.MIGRATIONS_DIR}} -seq {{.CLI_ARGS}}"
    preconditions:
      - sh: command -v {{.MIGRATE_CLI}}
        msg: |
          migrate CLI not found. Install with:
          go install -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

  migrate:force:
    desc: Force set migration version (usage: task migrate:force -- VERSION)
    cmds:
      - go run {{.MAIN_PKG}} migrate force {{.CLI_ARGS}}
```

**Step 2: Verify tasks are listed**

```bash
task --list | grep migrate
```

Expected output shows all migrate:* tasks

**Step 3: Commit**

```bash
git add Taskfile.yaml
git commit -m "build(task): add migration management tasks"
```

---

## Task 12: Remove Old Migration Code from PostgresEventStore

**Files:**

- Modify: `internal/store/postgres.go`
- Modify: `internal/store/postgres_test.go` (if tests reference old Migrate)

**Step 1: Remove embed directives and Migrate method**

Remove from `postgres.go`:

- All `//go:embed migrations/*.sql` lines (lines 23-42)
- All `var migration00XSQL string` declarations
- The `Migrate(ctx context.Context) error` method (lines 98-107)

**Step 2: Update any tests that called Migrate**

Search for `Migrate` in test files and update to use new `Migrator` type.

**Step 3: Verify tests pass**

```bash
task test
```

**Step 4: Commit**

```bash
git add internal/store/postgres.go internal/store/postgres_test.go
git commit -m "refactor(store): remove old migration code from PostgresEventStore"
```

---

## Task 13: Delete Old Migration Files

**Files:**

- Delete: `internal/store/migrations/001_initial.sql`
- Delete: `internal/store/migrations/002_system_info.sql`
- Delete: `internal/store/migrations/003_world_model.sql`
- Delete: `internal/store/migrations/004_pg_trgm.sql`
- Delete: `internal/store/migrations/005_pg_stat_statements.sql`
- Delete: `internal/store/migrations/006_object_containment_constraint.sql`
- Delete: `internal/store/migrations/007_exit_self_reference_constraint.sql`

**Step 1: Remove old files**

```bash
rm internal/store/migrations/001_initial.sql
rm internal/store/migrations/002_system_info.sql
rm internal/store/migrations/003_world_model.sql
rm internal/store/migrations/004_pg_trgm.sql
rm internal/store/migrations/005_pg_stat_statements.sql
rm internal/store/migrations/006_object_containment_constraint.sql
rm internal/store/migrations/007_exit_self_reference_constraint.sql
```

**Step 2: Verify only new format files remain**

```bash
ls internal/store/migrations/
```

Expected: Only `000001_*.up.sql`, `000001_*.down.sql`, etc.

**Step 3: Verify build still works**

```bash
task build
```

**Step 4: Commit**

```bash
git add -A internal/store/migrations/
git commit -m "chore(migrations): remove old single-file migration format"
```

---

## Task 14: Add Bootstrap Script for Existing Databases

**Files:**

- Create: `scripts/bootstrap-migrations.sql`

**Step 1: Create bootstrap script**

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

-- Bootstrap script for databases that were migrated with the old system.
-- Run this ONCE on existing databases before using golang-migrate.
--
-- Usage:
--   psql -d holomush -f scripts/bootstrap-migrations.sql
--
-- This tells golang-migrate that migrations 1-7 have already been applied.

CREATE TABLE IF NOT EXISTS schema_migrations (
    version bigint NOT NULL PRIMARY KEY,
    dirty boolean NOT NULL
);

INSERT INTO schema_migrations (version, dirty)
VALUES (7, false)
ON CONFLICT (version) DO NOTHING;

-- Verify
SELECT version, dirty FROM schema_migrations;
```

**Step 2: Commit**

```bash
git add scripts/bootstrap-migrations.sql
git commit -m "docs(scripts): add bootstrap script for existing databases"
```

---

## Task 15: Integration Test for Migration Cycle

**Files:**

- Create: `internal/store/migrate_integration_test.go`

**Step 1: Write integration test**

```go
//go:build integration

// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

package store_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/testcontainers/testcontainers-go"
    "github.com/testcontainers/testcontainers-go/modules/postgres"
    "github.com/testcontainers/testcontainers-go/wait"

    "github.com/holomush/holomush/internal/store"
)

func TestMigrator_FullCycle(t *testing.T) {
    ctx := context.Background()

    // Start PostgreSQL container
    pgContainer, err := postgres.Run(ctx,
        "postgres:16-alpine",
        postgres.WithDatabase("test"),
        postgres.WithUsername("test"),
        postgres.WithPassword("test"),
        testcontainers.WithWaitStrategy(
            wait.ForLog("database system is ready to accept connections").
                WithOccurrence(2)),
    )
    require.NoError(t, err)
    defer pgContainer.Terminate(ctx)

    connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
    require.NoError(t, err)

    // Test: Create migrator
    migrator, err := store.NewMigrator(connStr)
    require.NoError(t, err)
    defer migrator.Close()

    // Test: Initial version is 0
    version, dirty, err := migrator.Version()
    require.NoError(t, err)
    assert.Equal(t, uint(0), version)
    assert.False(t, dirty)

    // Test: Apply all migrations
    err = migrator.Up()
    require.NoError(t, err)

    version, dirty, err = migrator.Version()
    require.NoError(t, err)
    assert.Equal(t, uint(7), version)
    assert.False(t, dirty)

    // Test: Rollback one
    err = migrator.Steps(-1)
    require.NoError(t, err)

    version, _, err = migrator.Version()
    require.NoError(t, err)
    assert.Equal(t, uint(6), version)

    // Test: Apply one
    err = migrator.Steps(1)
    require.NoError(t, err)

    version, _, err = migrator.Version()
    require.NoError(t, err)
    assert.Equal(t, uint(7), version)
}
```

**Step 2: Run integration test**

```bash
go test -race -v -tags=integration ./internal/store/... -run TestMigrator_FullCycle
```

**Step 3: Commit**

```bash
git add internal/store/migrate_integration_test.go
git commit -m "test(store): add integration test for migration cycle"
```

---

## Task 16: Update Documentation

**Files:**

- Modify: `site/docs/operators/database.md` (or create if missing)
- Modify: `site/docs/contributors/development-setup.md` (if exists)

**Step 1: Document migration commands for operators**

Add section on running migrations:

````markdown
## Database Migrations

HoloMUSH uses golang-migrate for schema management.

### Running Migrations

```bash
# Apply all pending migrations
task migrate

# Check current migration status
task migrate:status

# Rollback one migration
task migrate:down

# Force version (for dirty state recovery)
task migrate:force -- 7
```
````

### Creating New Migrations

```bash
# Install migrate CLI (one-time)
go install -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@latest

# Create migration files
task migrate:create -- add_player_email
# Creates: 000008_add_player_email.up.sql and 000008_add_player_email.down.sql
```

### Bootstrapping Existing Databases

If migrating from an older HoloMUSH version:

```bash
psql -d holomush -f scripts/bootstrap-migrations.sql
```

**Step 2: Commit**

```bash
git add site/docs/
git commit -m "docs(operators): document database migration commands"
```

---

## Task 17: Final Cleanup and PR

**Step 1: Run full test suite**

```bash
task test
task lint
```

**Step 2: Verify all migrations work on fresh database**

```bash
# With a test database
DATABASE_URL=postgres://... task migrate:status
DATABASE_URL=postgres://... task migrate
```

**Step 3: Create final commit if any loose changes**

```bash
git status
# If changes exist:
git add -A && git commit -m "chore: final migration system cleanup"
```

**Step 4: Sync beads and push**

```bash
bd sync --from-main
git push -u origin feat/migrations
```

---

## Summary

| Task | Description                          | Files                                      |
| ---- | ------------------------------------ | ------------------------------------------ |
| 1    | Add golang-migrate dependency        | go.mod, go.sum                             |
| 2-8  | Convert migrations to up/down format | internal/store/migrations/*.sql            |
| 9    | Implement Migrator type              | internal/store/migrate.go                  |
| 10   | Update CLI migrate command           | cmd/holomush/migrate.go                    |
| 11   | Add Taskfile migration tasks         | Taskfile.yaml                              |
| 12   | Remove old migration code            | internal/store/postgres.go                 |
| 13   | Delete old migration files           | internal/store/migrations/*.sql            |
| 14   | Add bootstrap script                 | scripts/bootstrap-migrations.sql           |
| 15   | Integration test                     | internal/store/migrate_integration_test.go |
| 16   | Update documentation                 | site/docs/                                 |
| 17   | Final cleanup and PR                 | -                                          |
