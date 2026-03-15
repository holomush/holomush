# ABAC Plugin Enforcement Design

**Status:** Draft
**Date:** 2026-03-13
**PR:** #106 (feat/abac-t29-remove-capability-enforcer)

## Problem

PR #106 removes `capability.Enforcer` and its `wrap()` gating from host
functions. This eliminates the per-plugin capability enforcement boundary.
The PR claims ABAC seed policies handle enforcement at the service layer,
but:

1. No seed policies exist for `plugin` principal types — only `character`.
2. Plugin subjects use `"system:plugin:<name>"` which the engine parses as
   entity type `"system"`, matching no seed policies. World queries from
   plugins are silently denied (tests pass only because they use
   `AllowAllEngine()`).
3. KV operations (`kv_get`, `kv_set`, `kv_delete`) have no service layer —
   the `KVStore` interface takes `(ctx, namespace, key)` with no subject.
   No ABAC check occurs.
4. `get_command_help` performs no access check, while `list_commands`
   enforces ABAC per-capability.

## Design

### Principles

- Plugins define their own ABAC policies in their manifest.
- Policies are the single source of truth for plugin access. The
  `Capabilities` field is removed.
- Trust-on-install: plugin policies are installed automatically when the
  plugin loads. Operators trust a plugin by placing it in the plugins
  directory.
- Fail-closed: missing engine or missing policies result in denial.

### 1. Subject Format Fix

**Change:** Plugin subjects from `"system:plugin:<name>"` to
`"plugin:<name>"`.

**Rationale:** `access.SubjectPlugin = "plugin:"` is already defined but
unused. The engine's `parseEntityType()` returns the first segment before
`:`, so `"plugin:echo-bot"` yields entity type `"plugin"` — matchable by
`principal is plugin` in policy DSL.

**`PluginSubject` helper:** MUST panic on empty name, consistent with
`CharacterSubject`, `LocationResource`, and all other prefix helpers.

**Files:**

- `internal/access/prefix.go` — add `PluginSubject(name string) string`
  helper (with panic guard)
- `internal/plugin/hostfunc/adapter.go` — update `SubjectID()`
- `internal/plugin/hostfunc/helpers.go` — update subject construction
- `internal/plugin/hostfunc/world_write.go` — update subject construction
- `internal/plugin/hostfunc/functions.go` — update comment
- `internal/world/mutator.go` — update comment
- All corresponding `_test.go` files

### 2. Manifest Schema Change

**Remove:** `Capabilities []string` field from top-level `Manifest` struct.

**Retain:** `CommandSpec.Capabilities []string` — per-command capabilities
are used by `list_commands` for character-level ABAC filtering and serve a
different purpose than plugin-level policies. These are NOT removed.

**Add:** `Policies []ManifestPolicy` field:

```go
type ManifestPolicy struct {
    Name string `yaml:"name"`
    DSL  string `yaml:"dsl"`
}
```

**Policy naming convention:** `"plugin:<pluginName>:<policyName>"` where
`<policyName>` is the `Name` field from the manifest entry. The `"plugin:"`
prefix in policy names shares the string with `SubjectPlugin` but these
operate in different namespaces (policy names vs. entity references).

**Source naming validation:** Extend `ValidateSourceNaming` in
`store.go` to enforce that `plugin:`-prefixed policy names require
`source = "plugin"`, consistent with the ADR 35 pattern for `seed:` and
`lock:` prefixes.

**Migration:** All 4 existing plugin manifests (building, communication,
echo-bot, help) MUST be updated to replace `capabilities:` with
`policies:`. This is a breaking change to the manifest schema. No backward
compatibility shim — clean break.

**Example manifest (echo-bot):**

```yaml
name: echo-bot
version: "1.0.0"
description: Echo bot for testing

policies:
  - name: "emit-events"
    dsl: |
      permit(principal, action, resource) when {
        principal.plugin.name is "echo-bot"
        and action is "emit"
        and resource.type is "stream"
      };

lua-plugin:
  entry: main.lua
```

### 3. Plugin Policy Lifecycle

**On Load():**

1. Parse manifest policies
2. Compile each policy DSL (validate syntax)
3. **If compilation fails, the plugin MUST fail to load.** A plugin with
   invalid policies cannot function safely under default-deny.
4. Install via `PolicyStore.Create()` with `source = "plugin"`
5. Policy name stored as `"plugin:<pluginName>:<policyName>"`

**Policy scope validation:** During installation, each plugin policy MUST be
validated to ensure it only grants permissions to plugin principals. The
installer MUST check `CompiledTarget.PrincipalType != nil &&
*CompiledTarget.PrincipalType == "plugin"`. Any other value (`nil` or a
different type string like `"character"`) MUST cause
`InstallPluginPolicies` to return an error naming the failing policy. This
prevents a plugin from installing policies that affect character access or
other plugins — defense-in-depth on top of trust-on-install.

**On Unload():**

1. Delete all plugin policies via
   `PolicyStore.DeleteBySource(ctx, "plugin", "plugin:" + pluginName + ":")`

**On Reload (Unload + Load):**

1. Use `ReplacePluginPolicies` for atomic replacement — delete old and
   insert new within a single transaction. This eliminates the denial
   window between unload and load.

**PolicyStore extension:** Add `DeleteBySource(ctx, source, namePrefix)` to
the `PolicyStore` interface. The `namePrefix` parameter is the full prefix
including the naming convention (e.g., `"plugin:echo-bot:"`).
Implementation: single SQL `DELETE WHERE source = $1 AND name LIKE $2 || '%'`.
This avoids multi-round-trip partial-failure scenarios.

**Interface:** `PluginPolicyInstaller` is injected into `Manager` (not
individual hosts) via a `WithPolicyInstaller(PluginPolicyInstaller)` option.
The `Manager` calls `InstallPluginPolicies` after `Host.Load()` succeeds
and `RemovePluginPolicies` before `Host.Unload()`. The `Host` interface
does not change. This keeps policy lifecycle orchestration in one place:

```go
type PluginPolicyInstaller interface {
    InstallPluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error
    RemovePluginPolicies(ctx context.Context, pluginName string) error
    ReplacePluginPolicies(ctx context.Context, pluginName string, policies []ManifestPolicy) error
}
```

This keeps the plugin system decoupled from the full `PolicyStore`.

### 4. KV Resource Type

**Add** to `internal/access/prefix.go`:

```go
ResourceKV = "kv:"
```

With helper:

```go
func KVResource(namespace, key string) string
```

KV access requests use `"kv:<namespace>:<key>"` as the resource identifier.
The engine's `parseEntityType()` extracts `"kv"` from the resource, enabling
type-based matching. Example policy using only prefix matching (no attribute
provider needed):

```text
permit(principal, action, resource) when {
  principal is plugin
  and resource like "kv:echo-bot:*"
};
```

**`knownPrefixes` update:** Add `ResourceKV` to `knownPrefixes` in
`prefix.go` so that `ParseEntityRef("kv:echo-bot:counter")` succeeds.
Without this, `types.NewAccessRequest` will reject KV resources as
`INVALID_ENTITY_REF` before the engine is ever consulted.

**Note:** In hostfunc KV checks, the namespace argument to `KVResource` is
always `pluginName` — KV operations are namespaced per-plugin by the host
function layer, so the resource is always `"kv:<pluginName>:<key>"`.

Attribute-based KV matching (e.g., `resource.kv.namespace`) requires
a `KVAttributeProvider` — deferred to Out of Scope. Policies in this PR MUST
use `resource like` glob matching instead.

### 5. ABAC Checks in Hostfunc

**KV functions** (`kvGetFn`, `kvSetFn`, `kvDeleteFn`):

- Before calling `f.kvStore`, evaluate:
  `engine.Evaluate(ctx, {Subject: "plugin:<name>", Action: "read"|"write"|"delete", Resource: "kv:<namespace>:<key>"})`
- On denial or engine error: return Lua error string
- On nil engine: return Lua error "access engine not available"
- Pattern matches `list_commands` circuit-breaker approach

**`get_command_help`:**

- Add access check consistent with `list_commands` — evaluate whether the
  calling character can execute the named command before returning help
  details.
- **Breaking Lua API change:** `get_command_help(name)` becomes
  `get_command_help(name, character_id)`. The `character_id` parameter is
  required to identify the calling character for the ABAC evaluation.
  Plugins that call `get_command_help` MUST be updated. This is acceptable
  because only the `help` plugin uses this function, and it already receives
  `character_id` from the event payload.
- Implementation MUST include a nil-context guard matching `listCommandsFn`
  — fall back to `context.Background()` when `L.Context()` is nil.

**World operations:**

- Already enforced at `world.Service.checkAccess()`. The subject format fix
  (§1) makes plugin subjects evaluable against seed policies. No additional
  hostfunc changes needed.

### 6. Plugin Attribute Provider

**New file:** `internal/access/policy/attribute/plugin_provider.go`

Implements `AttributeProvider` with namespace `"plugin"`:

- `ResolveSubject("plugin:echo-bot")` → `{"name": "echo-bot"}`
- `ResolveResource(...)` → `nil, nil` (not a resource provider)

Requires a `PluginRegistry` interface to look up plugin names:

```go
type PluginRegistry interface {
    IsPluginLoaded(name string) bool
}
```

Registered with the attribute resolver at server startup.

### 7. Seed Policies for Plugins

No seed policies are added for plugins. The engine is already default-deny —
an explicit `seed:plugin-default-deny` forbid would be redundant and add
maintenance burden without providing additional security.

Plugin-specific policies come from manifests (§2-3), not from seeds. This
keeps the seed set focused on character access patterns.

### 8. Tests

**Unit tests:**

- `PluginSubject()` helper
- `ManifestPolicy` parsing and validation
- `PluginPolicyInstaller` — install on load, remove on unload
- `PluginAttributeProvider` — resolves plugin name from subject
- KV hostfunc ABAC enforcement: denied without policy, allowed with policy
- `get_command_help` access check

**Integration tests:**

- Plugin loads → policies appear in store → engine permits declared
  operations
- Plugin unloads → policies removed → engine denies
- Plugin with no policies → all operations denied (default-deny)
- Integration fixtures updated to wire engine with plugin policies

### 9. Documentation

- Update `internal/plugin/hostfunc/functions.go` package docstring
- Update `site/docs/developers/plugin-guide.md` — replace Capabilities
  section with Policies section
- Update `site/docs/contributors/architecture.md` — reflect ABAC
  enforcement model

### 10. CI: S8 Static Analysis Check

Add a `go vet`-style analyzer or a `golangci-lint` custom rule to detect
`AccessControl.Check` in production code. A grep-based approach is fragile
(matches in comments, strings, struct fields). Options:

1. **Preferred:** Add a `nolint` directive linter rule via `golangci-lint`
   custom config, using `forbidigo` to ban `AccessControl.Check`.
2. **Fallback:** AST-based grep using `go/ast` in a test file that walks
   production packages.

If neither is feasible within this PR, defer CI enforcement and document
the manual verification step.

### 11. T28.5 Seed Policy Validation

Create `internal/access/policy/seed_validation_test.go`:

- For each seed policy, verify the engine produces the expected decision for
  a set of known-good test cases
- Replaces the migration equivalence test that could not be written (old
  engine already removed)

## Out of Scope

- Per-key KV restrictions — the policy language supports it, but no seed or
  plugin policies implement key-level granularity in this PR. Operators or
  plugins can add custom policies later.
- Dynamic attribute resolution for KV resources (e.g., `resource.kv.namespace`)
  — deferred until a KV attribute provider is needed.
- Operator approval flow for plugin policies — trust-on-install model.

## Files Changed (Estimated)

| Area | Files | Type |
|------|-------|------|
| Subject format | ~10 | Modify |
| Manifest schema | ~6 | Modify |
| Policy lifecycle (incl. Manager) | ~5 | New + Modify |
| KV resource type | 1 | Modify |
| Hostfunc ABAC | ~3 | Modify |
| Attribute provider | 2 | New |
| Plugin manifests | 4 | Modify |
| Tests | ~8 | New + Modify |
| Documentation | 3 | Modify |
| CI | 1 | Modify |
| **Total** | **~42** | |

## Dependencies

This design depends on:

- `PolicyStore` interface (exists — `internal/access/policy/store/`)
- ABAC DSL compiler (exists — `internal/access/policy/compiler.go`)
- Attribute resolver (exists — `internal/access/policy/attribute/`)
- `AccessPolicyEngine` interface (exists — `internal/access/policy/types/`)

No new external dependencies required.
