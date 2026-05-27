<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Bootstrap: Aliases & Admin Seeding

**Status:** Draft
**Date:** 2026-03-28
**Beads:** holomush-a3a7.3 (Seed standard command aliases), holomush-qcn6 (Admin user bootstrap)
**Depends on:** [Commands & Behaviors](2026-02-02-commands-behaviors-design.md) (alias system), [Auth & Identity](2026-01-25-auth-identity-design.md) (player/character model), [Full ABAC](2026-02-05-full-abac-design.md) (policy engine, role resolution)

## Overview

HoloMUSH needs two bootstrap operations that run automatically on core startup:
seeding standard MUSH command aliases, and creating an admin account on first
boot. Both follow the pattern established by `policy.Bootstrap()` — idempotent,
automatic, no operator action required.

This spec also introduces multi-role support for characters via a new
`character_roles` table, a system role enum, and the wiring needed to connect
role storage to the existing ABAC policy engine.

### Goals

- Seed `"` → say, `:` → pose, `;` → pose as system aliases at startup
- Remove `expandMUSHPrefix` — alias cache replaces it entirely
- Pose handler distinguishes `:` (space) from `;` (no space) via `InvokedAs`
- Create admin player + character on first boot when no players exist
- Random admin character names from a star-themed `naming.Theme`
- Multi-role character support with `character_roles` join table
- System role constants (`player`, `builder`, `admin`) as a Go enum
- Wire `RoleResolver` into the ABAC stack (currently `nil` in production)

### Non-Goals

- Role management UI or commands (future admin panel work)
- Per-player roles (roles are per-character)
- Role hierarchy or inheritance (policies define what each role can do)
- Email notification of admin credentials
- Multi-admin bootstrap (one admin account per first boot)

## Alias Seeding

### Aliases to Seed

| Alias | Target | Behavior                                  |
| ----- | ------ | ----------------------------------------- |
| `"`   | `say`  | `"hello` → `say hello`                   |
| `:`   | `pose` | `:waves` → `pose waves` (space variant)   |
| `;`   | `pose` | `;'s eyes` → `pose 's eyes` (no space)   |

Direction shortcuts (`n`, `s`, `e`, `w`, `u`, `d`) are NOT aliases — they
resolve from exit names on locations via the move command. They MUST NOT be
seeded here.

### Seeding Mechanics

A `SeedSystemAliases` function in `internal/bootstrap/` runs during core
startup, after the alias cache is created and before the dispatcher is built.

The function:

1. Reads current system aliases from the database
2. For each standard alias, checks if it already exists
3. Seeds missing aliases via `AliasRepo.SetSystemAlias()` (upsert)
4. Logs each alias seeded at info level; skips silently if already present
5. Always reloads ALL system aliases into the cache (full replace, not merge)
   regardless of whether any new aliases were seeded

This is idempotent — re-running on an existing database changes nothing.

### Removing expandMUSHPrefix

The `expandMUSHPrefix` function in `internal/grpc/server.go` currently handles
only `:` → pose (not `"` or `;`). Once aliases are seeded, the alias cache's
`Resolve()` handles all prefix expansion (it already has single-character
prefix alias logic). The function MUST be removed and its call site in
`HandleCommand` deleted.

### Pose No-Space via InvokedAs

The pose handler checks `exec.InvokedAs == ";"` and sets a `NoSpace` flag on
the event payload. Clients (telnet gateway, web) use this flag when rendering.

`PosePayload` gains a new field:

```go
type PosePayload struct {
    CharacterName string `json:"character_name"`
    Action        string `json:"action"`
    NoSpace       bool   `json:"no_space,omitempty"`
}
```

| Input            | InvokedAs | Payload NoSpace | Rendered output         |
| ---------------- | --------- | --------------- | ----------------------- |
| `:waves`         | `:`       | `false`         | `Alaric waves`          |
| `;'s eyes widen` | `;`       | `true`          | `Alaric's eyes widen`   |
| `pose laughs`    | `pose`    | `false`         | `Alaric laughs`         |

The no-space decision lives in the server (pose handler), not in clients.
Clients check `NoSpace` only to decide separator rendering — telnet uses
`"%s%s"` vs `"%s %s"`, web omits or includes the CSS gap between actor and
action spans.

**Watch point:** This is the first rendering hint on an event payload. If more
accumulate, consider a shared `RenderHints` struct rather than per-payload
flags. For now, a single `NoSpace` field is proportionate.

### InvokedAs Convention Documentation

The `InvokedAs` field on `CommandExecution` enables command handlers to alter
behavior based on how the user invoked them. This is a deliberate design
pattern: a single registered command can behave differently depending on which
alias triggered it. The pose/semipose distinction is the canonical example.

This convention MUST be documented:

- Enhance the existing `InvokedAs` doc comment in `internal/command/types.go`
  (lines 240-243) with the alias-convention pattern and the pose example
- Add a section in the developer documentation for command authors
  (`site/docs/developers/`)

## Admin Bootstrap

### Trigger Condition

The admin bootstrap runs during core startup, after migrations and policy
bootstrap but before gRPC server creation. It checks whether any players exist:

- If the `players` table is empty → create admin account
- If any players exist → skip silently (debug log)

This means the admin is created on first boot only. Subsequent boots with
existing players skip the check entirely.

### Account Creation Flow

1. Read configuration from environment variables:

   | Env Var                     | Default     | Description                |
   | --------------------------- | ----------- | -------------------------- |
   | `HOLOMUSH_ADMIN_USERNAME`   | `admin`     | Admin player username      |
   | `HOLOMUSH_ADMIN_PASSWORD`   | (generated) | Admin player password      |
   | `HOLOMUSH_ADMIN_CHARACTER`  | (generated) | Admin character name       |

2. If `HOLOMUSH_ADMIN_PASSWORD` is unset, generate a 24-character random
   password using `crypto/rand`
3. If `HOLOMUSH_ADMIN_CHARACTER` is unset, pick a random star name from the
   `StarTheme` name generator
4. Hash password with Argon2id (existing `auth.Hasher`)
5. Create player via `PlayerRepository.Create()`
6. Create character via `CharacterService.Create()` — the service handles
   start location assignment internally (same flow as normal character creation)
7. Assign `admin` role via `RoleStore.AddRole()`
8. Log account creation via `slog.Info` (username and character name only)
9. If password was generated: write it to stderr (not structured logs) to
   avoid persisting credentials in log aggregation systems. If password
   came from env var: do not echo it.

### Star Name Theme

Admin characters use a star-themed name generator, distinct from the
gemstone+element theme used for guests.

Star names pool (20 entries):

```text
Sirius, Vega, Rigel, Altair, Deneb,
Polaris, Antares, Betelgeuse, Capella, Arcturus,
Spica, Regulus, Procyon, Aldebaran, Fomalhaut,
Canopus, Achernar, Bellatrix, Castor, Pollux
```

The name generator implements the `naming.Theme` interface. The interface
and all name lists (gemstones, elements, stars) live in `internal/naming/`,
shared by both guest auth and admin bootstrap.

### PlayerRepository.Count

A new `Count(ctx context.Context) (int, error)` method on `PlayerRepository`
returns the total number of players. The Postgres implementation runs
`SELECT COUNT(*) FROM players`. This is the "first boot" detection mechanism.

This is a breaking interface change to `auth.PlayerRepository`. The Postgres
implementation and all mocks (`MockPlayerRepository`) MUST add the new method.

## Role Storage & Resolution

### character\_roles Table

New migration (number is illustrative — use the next sequential number at
implementation time):

```sql
CREATE TABLE character_roles (
    character_id TEXT NOT NULL REFERENCES characters(id) ON DELETE CASCADE,
    role TEXT NOT NULL,
    PRIMARY KEY (character_id, role)
);
```

A character can have multiple roles. The table is a simple join with a
composite primary key. `ON DELETE CASCADE` removes role assignments when a
character is deleted.

### System Role Constants

A role enum in `internal/access/role.go`:

```go
const (
    RolePlayer  = "player"
    RoleBuilder = "builder"
    RoleAdmin   = "admin"
)

func SystemRoles() []string {
    return []string{RolePlayer, RoleBuilder, RoleAdmin}
}
```

All existing magic strings (`"admin"`, `"builder"`, `"player"`) in Go code
MUST be replaced with these constants. Policy DSL text retains string literals
since the DSL parser does not resolve Go constants.

### RoleStore Interface

```go
type RoleStore interface {
    GetRoles(ctx context.Context, characterID string) ([]string, error)
    AddRole(ctx context.Context, characterID string, role string) error
    RemoveRole(ctx context.Context, characterID string, role string) error
}
```

Postgres implementation uses the `character_roles` table. `AddRole` uses
`INSERT ... ON CONFLICT DO NOTHING` for idempotency.

### RoleResolver Update

The existing `attribute.RoleResolver` interface changes from single-role to
multi-role:

```go
// Before
type RoleResolver interface {
    GetRole(subject string) string
}

// After
type RoleResolver interface {
    GetRoles(subject string) []string
}
```

A `PostgresRoleResolver` wraps `RoleStore` to satisfy this interface, caching
roles per request (not globally — role changes must take effect promptly).

This is a breaking interface change. The only production caller
(`BuildABACStack`) currently passes `nil`, so there are no downstream consumers
to migrate. Test mocks (`mockRoleResolver` in attribute tests, `staticRoleResolver`
in integration tests) MUST update to the new multi-role signature.

### CharacterProvider Update

`CharacterProvider` currently exposes `principal.character.role` as
`AttrTypeString`. After this change:

- `principal.character.roles` is declared as `AttrTypeStringList` in the schema
- The attribute resolves to a `[]string` slice
- The old `principal.character.role` attribute is removed
- When `RoleStore.GetRoles()` returns an empty slice, `CharacterProvider` MUST
  populate `roles` with `["player"]` as the default, preserving the existing
  fallback behavior. Without this, `"player" in principal.character.roles`
  would fail for characters with no explicit role assignments.

### Seed Policy Update

All seed policies that check roles MUST update. There are two patterns:

**Pattern A — equality check** (2 policies: `admin-full-access`,
`property-admin-read`):

```text
// Before: principal.character.role == "admin"
// After:  "admin" in principal.character.roles
```

This uses `InExprCondition` (literal `in` attribute-resolved-slice), which
`evalInExpr` already handles for `string in []string`.

**Pattern B — scalar-in-literal-list** (4 policies: `builder-location-write`,
`builder-object-write`, `builder-commands`, `builder-exit-write`):

```text
// Before: principal.character.role in ["builder", "admin"]
// After:  "builder" in principal.character.roles
```

The admin case is already covered by `seed:admin-full-access` (which grants
admins full access to everything), so builder policies only need to check for
the builder role. This simplifies the rewrite and avoids needing set
intersection logic.

The DSL already supports `in` expressions (`InListCondition`,
`InExprCondition` in `dsl/ast.go`), so no parser changes are needed. The
`SeedVersion` on each updated policy MUST be incremented so `policy.Bootstrap()`
re-seeds them on next startup.

### ABAC Stack Wiring

`BuildABACStack` in `internal/access/setup/setup.go` currently passes `nil`
for the `RoleResolver` parameter to `NewCharacterProvider`. After this change:

1. `ABACConfig` gains a `RoleStore` field
2. `BuildABACStack` creates a `PostgresRoleResolver` from the store
3. `NewCharacterProvider` receives the resolver instead of `nil`
4. `core.go` creates `PostgresRoleStore` and passes it to `ABACConfig`

## Bootstrap Package

All bootstrap logic lives in `internal/bootstrap/`. This package contains:

- `aliases.go` — `SeedSystemAliases()` function
- `admin.go` — `SeedAdmin()` function with `SeedAdminDeps` struct

Both functions accept their dependencies as parameters (no globals, no
package-level state). Both are called from `core.go` during startup.

### Startup Order

The bootstrap functions slot into the existing startup sequence in `core.go`:

```text
1. Config loading & validation         (existing)
2. Auto-migration                       (existing)
3. Database connection                  (existing)
4. Policy bootstrap                     (existing)
5. Game ID initialization               (existing)
6. ABAC stack setup                     (existing, updated with RoleStore)
7. Auth services                        (existing)
8. ► Admin bootstrap                    (NEW — needs auth services + role store)
9. TLS setup                            (existing)
10. Command registry & alias cache      (existing)
11. ► Alias seeding                     (NEW — needs alias repo + cache)
12. Dispatcher & gRPC server            (existing)
```

## Naming Package Extraction

The `internal/naming/` package contains:

- `theme.go` — `Theme` interface (moved from `internal/telnet/`)
- `gemstone.go` — `GemstoneElementTheme` and name lists (moved)
- `star.go` — `StarTheme` with star name list (new)

`internal/telnet/guest_auth.go` imports from `internal/naming/` instead of
defining names locally. The `Theme` interface:

```go
type Theme interface {
    Name() string
    Generate() (firstName, secondName string)
}
```

For admin characters, `StarTheme.Generate()` returns a single star name as
`firstName` with an empty `secondName`, since admin characters use a single
name rather than a two-part compound.

## Testing Strategy

### Unit Tests

- `internal/bootstrap/aliases_test.go` — seed aliases idempotently, skip
  existing, verify cache population
- `internal/bootstrap/admin_test.go` — create admin on empty DB, skip on
  non-empty, env var overrides, password generation, role assignment
- `internal/naming/star_test.go` — theme generates valid names
- `internal/store/role_store_test.go` — CRUD operations, idempotent add,
  cascade delete
- Pose handler test — verify no-space behavior when `InvokedAs == ";"`
- Role resolver test — multi-role resolution, empty roles default to player

### Integration Tests

- Full bootstrap flow: start server with empty DB, verify admin account exists,
  verify aliases resolve correctly, verify admin character has admin role and
  can execute admin commands
- Existing ABAC integration tests updated for multi-role

### Seed Policy Tests

- Verify `"admin" in principal.character.roles` grants access
- Verify `"builder" in principal.character.roles` grants builder permissions
- Verify default (no roles) falls back to player permissions

## Migration & Backward Compatibility

The `character_roles` table is additive — no existing tables change.

**Breaking interface changes:**

- `RoleResolver`: `GetRole(string) string` → `GetRoles(string) []string`.
  Production passes `nil`; only test mocks need updating.
- `PlayerRepository`: new `Count(ctx) (int, error)` method. Postgres
  implementation and mocks need the new method.

**Attribute rename:** `principal.character.role` → `principal.character.roles`.
Seed policies re-seed on every startup via `policy.Bootstrap()` (with
incremented `SeedVersion`). Custom policies referencing the old attribute name
will fail evaluation (fail-closed, safe default).
