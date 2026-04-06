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

## Loader Seeding Flow

After `LoadAll` discovers and loads all plugins, the loader collects aliases
from every loaded manifest and seeds them through the existing persistence path:

1. **Collect:** Iterate loaded manifests, gather `{alias, commandName,
   pluginName}` tuples.
2. **Validate cross-plugin:** Detect duplicate aliases across plugins. Log a
   warning and skip the later one. DAG load order determines the winner.
3. **Seed to DB:** Call `AliasSeeder.SetSystemAlias(ctx, alias, commandName,
   pluginName)` for each tuple. This uses "insert if absent" semantics — an
   alias that already exists in the database is not overwritten.
4. **Load cache:** Call `cache.LoadSystemAliases()` to hydrate the in-memory
   `AliasCache` from the database.

### Integration Points

- The `Manager` MUST receive an `AliasSeeder` and `*AliasCache` (or a small
  interface wrapping both) via dependency injection.
- Alias seeding MUST happen at `BootstrapPriorityAlias` (500), after content
  plugins are loaded.
- The `createdBy` field in `SetSystemAlias` MUST be set to the plugin name
  (e.g., `"core-communication"`), replacing the current `"system"` value.

### Replaces

This seeding flow replaces the `AliasBootstrapper` bootstrap step entirely.
The `SeedSystemAliases` function in `internal/bootstrap/aliases.go` is removed.

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

- **Full startup with DB verification:** Boot the server with
  `core-communication` and `core-objects` plugins loaded, then query the
  system aliases table and assert all six aliases are present with correct
  `command` and `created_by` values.
- **Operator override survives restart:** Seed aliases, operator changes one
  via `sysalias` (e.g., `" → shout`), restart server, verify the changed
  alias is not overwritten (confirming "skip existing" semantics).
- **Idempotent seeding:** Load twice against the same database, verify no
  errors and aliases remain correct.

### Error Cases

- Empty alias string — manifest validation error, plugin fails to load.
- Cross-plugin collision — warning logged, later plugin's alias skipped
  (non-fatal, graceful degradation).
- DB failure on `SetSystemAlias` — logged, non-fatal, startup continues.
