---
name: new-migration
description: Create a new database migration with up/down SQL files following project conventions
disable-model-invocation: true
---

# New Migration

Create a PostgreSQL migration pair using golang-migrate conventions.

## Usage

```text
/new-migration <name>
```

Where `<name>` is a snake_case description (e.g., `add_player_inventory`).

## Steps

1. **Create migration files** using the task runner:

   ```bash
   task migrate:create -- <name>
   ```

   This creates sequentially numbered `NNNNNN_<name>.up.sql` and `NNNNNN_<name>.down.sql` in `internal/store/migrations/`.

2. **Add SPDX headers** to both files:

   ```sql
   -- SPDX-License-Identifier: Apache-2.0
   -- Copyright 2026 HoloMUSH Contributors
   ```

3. **Populate the up migration** with the requested schema changes. Follow these conventions:
   - Use `IF NOT EXISTS` for `CREATE TABLE`, `CREATE INDEX`
   - Use `NOT NULL` with sensible defaults
   - Use `ULID` (CHAR(26)) for entity IDs, matching the Go `ulid.ULID` type
   - Use `TIMESTAMPTZ` for all timestamps with `DEFAULT NOW()`
   - Add appropriate indexes for foreign keys and query patterns
   - Add `COMMENT ON TABLE/COLUMN` for non-obvious fields
   - Wrap multi-statement migrations in `BEGIN; ... COMMIT;`

4. **Populate the down migration** with the exact reverse:
   - `DROP TABLE IF EXISTS` in reverse order of creation
   - `DROP INDEX IF EXISTS` for any standalone indexes
   - The down migration MUST cleanly reverse the up migration

5. **Verify** the migration compiles into the embed:

   ```bash
   task build
   ```

6. **Show the user** the migration number and both file paths for review.

## Conventions

- Migration names: `snake_case`, descriptive (e.g., `add_scene_tags`, `alter_objects_add_properties`)
- Foreign keys: always name them explicitly (`CONSTRAINT fk_<table>_<column>`)
- Indexes: `idx_<table>_<column(s)>`
- Existing migrations are in `internal/store/migrations/` — check them for patterns
