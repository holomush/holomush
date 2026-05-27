<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin Manifest Alias Declarations with Loader Seeding

**Date:** 2026-04-06
**Status:** Approved
**Bead:** holomush-3ozp

## Overview

Plugins declare command aliases in their `plugin.yaml` manifests. The plugin
loader seeds them into the database via the existing `AliasSeeder` during
startup. This replaces the hardcoded alias list in
`internal/bootstrap/aliases.go`.

## RFC2119 Keywords

| Keyword        | Meaning                                    |
| -------------- | ------------------------------------------ |
| **MUST**       | Absolute requirement                       |
| **MUST NOT**   | Absolute prohibition                       |
| **SHOULD**     | Recommended, may ignore with justification |
| **SHOULD NOT** | Not recommended                            |
| **MAY**        | Optional                                   |

## Manifest Schema Change

Add an `Aliases` field to `CommandSpec`:

```go
type CommandSpec struct {
    Name         string               `yaml:"name"`
    Aliases      []string             `yaml:"aliases,omitempty"`
    Capabilities []command.Capability `yaml:"capabilities,omitempty"`
    Help         string               `yaml:"help,omitempty"`
    Usage        string               `yaml:"usage,omitempty"`
    HelpText     string               `yaml:"helpText,omitempty"`
    HelpFile     string               `yaml:"helpFile,omitempty"`
}
```

Each alias string is the trigger text. The expansion is always the parent
command's `Name`.

### YAML Example

```yaml
commands:
  - name: say
    aliases:
      - '"'
  - name: pose
    aliases:
      - ":"
      - ";"
```

### Validation

- Alias strings MUST NOT be empty. Empty strings MUST cause a manifest
  validation error and prevent the plugin from loading.
- Duplicate aliases within a single manifest MUST be rejected at parse time.
- Manifests without the `aliases` field MUST parse identically to today (zero
  aliases, no error).

## Database Schema Change

A new `source` column is added to `system_aliases` via migration `000003`:

```sql
ALTER TABLE system_aliases ADD COLUMN IF NOT EXISTS source TEXT;
```

### Why `source` Is Separate from `created_by`

The existing `system_aliases.created_by` column is declared as
`TEXT REFERENCES players(id)`. It cannot hold a plugin name — the foreign
key constraint requires either a valid player ID or NULL. Plugin-seeded
aliases have no associated player, so they cannot use `created_by` to record
provenance.

The `source` column is an unconstrained `TEXT` field that records the origin
of each alias, independent of any player FK:

| Row type | `created_by` | `source` |
|----------|-------------|---------|
| Plugin-seeded (from manifest) | `NULL` | plugin name, e.g. `"core-communication"` |
| Operator-created (via `sysalias`) | player ID FK | `"sysalias"` |
| Legacy pre-migration-000003 rows | `NULL` or player ID | `NULL` |

## Loader Seeding Flow

After `LoadAll` discovers and loads all plugins, the loader collects aliases
from every loaded manifest and seeds them through the existing persistence path:

1. **Collect:** Iterate loaded manifests in DAG/priority load order, gathering
   `{alias, commandName, pluginName}` tuples.
2. **Validate cross-plugin:** Detect duplicate aliases across plugins. Log a
   warning and skip the later one. **The Manager preserves DAG load order via
   an ordered slice (`loadedOrder`) rather than iterating its loaded map, so
   conflict resolution is deterministic across restarts.**
3. **Seed to DB:** For each tuple, call
   `AliasSeeder.SetSystemAlias(ctx, alias, commandName, "", pluginName)`.
   The fourth argument (`createdBy`) is empty (stored as NULL) because
   plugin-seeded aliases have no associated player. The fifth argument
   (`source`) is the plugin name.
4. **Skip-existing semantics:** Before calling `SetSystemAlias`, the seeder
   reads existing aliases via `GetSystemAliases` and skips any that already
   exist. This preserves operator overrides across restarts. Note: the
   repository-level `SetSystemAlias` is an UPSERT; the skip-existing behavior
   is enforced at the application layer in `SeedManifestAliases`, not in the
   DB.
5. **Load cache:** Call `cache.LoadSystemAliases()` to hydrate the in-memory
   `AliasCache` from the database.

### Integration Points

- The `Manager` MUST receive an `AliasSeeder` and `*AliasCache` via
  `WithAliasSeeder(seeder, cache)` dependency injection.
- Alias seeding MUST happen inside `Manager.LoadAll`, after every plugin has
  loaded successfully, so that any plugin's `commands[].aliases` can contribute
  regardless of load order.
- The plugin subsystem (`internal/plugin/setup`) — which starts at lifecycle
  priority 5, before the bootstrap subsystem — creates the `AliasRepo` and
  `AliasCache` and passes them to the Manager. Other subsystems (notably
  gRPC) retrieve them from the plugin subsystem via getter methods.

### Replaces

This seeding flow replaces the `AliasBootstrapper` bootstrap step entirely.
The `SeedSystemAliases` function in `internal/bootstrap/aliases.go` and the
`AliasBootstrapper` adapter in `internal/bootstrap/alias_bootstrap.go` are
removed. The `BootstrapPriorityAlias` constant is no longer used.

## Migration Plan

### Manifest Additions

| Plugin               | Command  | Aliases Added |
| -------------------- | -------- | ------------- |
| `core-communication` | say      | `"`           |
| `core-communication` | pose     | `:`, `;`      |
| `core-communication` | page     | `p`           |
| `core-communication` | whisper  | `w`           |
| `core-objects`       | describe | `desc`        |

### Removed Files

- `internal/bootstrap/aliases.go` — the hardcoded alias list
- `internal/bootstrap/alias_bootstrap.go` — the `AliasBootstrapper` adapter
- Any wiring that registers `AliasBootstrapper` in the bootstrap sequence

### Preserved

- `AliasSeeder` interface — still used by the loader
- `AliasCache` and all its methods — still the runtime resolution layer

### Out of Scope

- `core-channels` plugin and `=` alias — that plugin does not exist yet.
  The `=` alias will be added when `core-channels` is created.
- Top-level `aliases` section in manifests for cross-plugin aliasing — not
  needed now, additive if needed later.

## Testing

### Unit Tests

- **Manifest parsing:** YAML with aliases round-trips correctly; empty alias
  strings MUST be rejected; duplicate aliases within a manifest MUST be
  rejected.
- **Cross-plugin validation:** Two manifests declaring the same alias — the
  second MUST be skipped with a logged warning; the first by load order wins.
- **Loader seeding:** Given loaded manifests with aliases, `SetSystemAlias`
  MUST be called with correct `(alias, command, pluginName)` tuples.
- **Backward compatibility:** Manifests without an `aliases` field MUST parse
  and load identically to today.

### E2E Tests (Integration, Ginkgo/Gomega)

E2E tests live in `test/integration/plugin/` so they run as part of
`task test:int` (the `./internal/plugin/` package is excluded from the
integration test target because of pre-existing orphaned test files
unrelated to this work).

- **Full startup with DB verification:** Boot the plugin Manager with
  `core-communication` and `core-objects` plugins loaded, then query the
  system aliases table **by column name** (not positional) and assert
  all six aliases are present with `command` matching the aliased command
  name (e.g. `say`, `pose`, `describe`), `created_by` IS NULL, and `source`
  equal to the owning plugin name (`core-communication` or `core-objects`).
- **Operator override via sysalias:** Seed aliases, operator overrides one
  (e.g., `" → shout`) with a player ID via `SetSystemAlias`, restart
  Manager, verify the override is preserved in `command`, `created_by`,
  and `source` (= `"sysalias"`) — confirming both the skip-existing
  semantics and the provenance distinction.
- **Idempotent seeding:** Load twice against the same database, verify no
  errors and aliases remain correct.

### Error Cases

- Empty alias string — manifest validation error, plugin fails to load.
- Cross-plugin collision — warning logged, later plugin's alias skipped
  (non-fatal, graceful degradation).
- DB failure on `SetSystemAlias` — logged, non-fatal, startup continues.
