<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Plugin ABAC Trust Boundary & Attribute Resolution

**Date:** 2026-04-06
**Status:** Draft
**Beads:** holomush-wdge, holomush-lbis, holomush-nrze
**Blocks:** Channel PR (#191), Epic 10

## Problem Statement

Plugins that introduce new resource types (channels, widgets, etc.) need three
capabilities the current ABAC system doesn't provide:

1. **Install character-level policies** scoped to their own resources
   (holomush-nrze)
2. **Pass Layer 2 capability pre-flight** for plugin-owned resource types
   (holomush-lbis)
3. **Provide attribute resolution** so the ABAC engine can evaluate
   instance-level conditions against plugin-owned resources

Additionally, seed policies currently grant execute permits for commands that
have migrated to plugins. Each plugin SHOULD own its own execute policies.

### Root Cause Analysis

The three beads collapse into one core issue: the policy installer
(`internal/plugin/policy_installer.go:58`) rejects any policy with
`principal is character` — only `principal is plugin` is allowed. This was an
intentional security choice, but it forces all character-level authorization
for plugin resource types into `internal/access/policy/seed.go`, coupling core
to plugin knowledge.

Once plugins can install character-level policies scoped to their own resource
types:

- **holomush-wdge** is solved: plugins write their own `execute:command:<name>`
  policies
- **holomush-lbis** is solved: installed policies become candidates for
  `CanPerformAction` pre-flight, and the engine's existing
  `ReferencesResourceAttrs` optimistic-pass logic handles resource conditions
- **holomush-nrze** is the direct fix: expand the trust boundary

## RFC 2119 Keywords

| Keyword         | Meaning                                    |
|-----------------|--------------------------------------------|
| **MUST**        | Absolute requirement                       |
| **MUST NOT**    | Absolute prohibition                       |
| **SHOULD**      | Recommended, may ignore with justification |
| **SHOULD NOT**  | Not recommended                            |
| **MAY**         | Optional                                   |

## Design

### 1. Manifest: `resource_types` Field

Plugins declare the resource types they own:

```yaml
name: core-channels
version: 1.0.0
type: binary
resource_types: [channel]
```

`resource_types` is a list of strings. Each string is a resource type name that
the plugin introduces (e.g., `channel`, `widget`). The plugin MAY install
character-level policies scoped to these types and MUST implement the
`AttributeResolver` gRPC service (§3) to provide attribute resolution.

Only binary plugins MAY declare `resource_types`. Lua plugins MUST NOT declare
`resource_types` — they operate on core resource types which already have
core-side attribute providers. If a plugin needs custom resource types with
attribute resolution, it MUST be a binary plugin. This keeps the boundary
clean: Lua is for lightweight command plugins, binary is for plugins with
their own resource types, storage, and services.

**Validation rules:**

- `resource_types` MUST only be declared by binary plugins (`type: binary`);
  Lua and setting plugins MUST NOT declare it
- Resource type names MUST match the existing plugin name pattern
  (`^[a-z](-?[a-z0-9])*$`)
- Resource type names MUST NOT collide with core resource types (see
  §1.1 Protected Types)
- Resource type names MUST be unique across all loaded plugins — the second
  plugin to declare the same type fails to load

#### 1.1 Protected Resource Types

The following resource types are reserved for core and MUST NOT appear in any
plugin's `resource_types` declaration:

```text
character, location, exit, object, stream, property, scene, command, system,
server, player
```

This list is maintained as a constant set in the policy installer. New core
resource types MUST be added to this set.

The `command` resource type is a special case — plugins do not need to declare
it in `resource_types` to install execute policies for their own commands. See
§2.2.

### 2. Expanded Policy Installer Trust Boundary

The check at `policy_installer.go:58` is replaced with a multi-rule validation
table.

#### 2.1 Validation Rules

| Principal type | Resource type in policy | Condition | Result |
|---|---|---|---|
| `plugin` | any | — | **Allowed** (existing behavior) |
| `character` | in plugin's `resource_types` | — | **Allowed** |
| `character` | `command` | policy targets only plugin's own command names | **Allowed** |
| `character` | core protected type | — | **Rejected** |
| `character` | unrecognized type, not in `resource_types` | — | **Rejected** |
| `character` | any | `trust.all_principals` + server allowlist | **Allowed** |
| `system` | any | — | **Rejected** |

The `command` resource type exception (row 3) allows plugins to write execute
policies for their own commands without declaring `command` as a resource type.
The validator MUST verify that command-targeting policies only reference command
names declared in the plugin's `commands:` section.

#### 2.2 Command Policy Scoping

When a plugin installs a policy with `resource is command`, the policy
installer MUST extract command names from the policy DSL (from `when`
conditions referencing `resource.command.name`) and verify each name appears
in the plugin's `commands:` section. A plugin MUST NOT grant execute permits
for commands it doesn't declare.

#### 2.3 Trust Escalation

For third-party plugins that need policies outside the normal trust boundary:

**Manifest declaration:**

```yaml
trust:
  all_principals: true
```

**Server-side allowlist** (in server config, format TBD — could be
`holomush.yaml`, environment variable, or similar):

```yaml
plugins:
  trust_allowlist:
    - exotic-plugin-name
```

Both sides MUST agree:

- Manifest declares `trust.all_principals: true` AND plugin name appears in
  server allowlist → trust granted
- Either side missing → trust denied, policies outside normal boundary rejected

No core/in-tree plugin SHOULD ever require trust escalation.

#### 2.4 Trust Logging

Trust-related events MUST be logged clearly for operator visibility:

| Event | Log level | Message |
|---|---|---|
| Plugin loaded with elevated trust (both sides agree) | **WARN** | `plugin %q loaded with elevated trust (all_principals) — server allowlist match` |
| Plugin requests trust but not in server allowlist | **WARN** | `plugin %q requests elevated trust but is not in server allowlist; trust escalation denied` |
| Plugin installs `principal is character` policy on declared resource type | **INFO** | `plugin %q installing character-level policy %q on resource type %q` |
| Plugin installs `principal is character` policy on `command` resource | **INFO** | `plugin %q installing command execute policy %q` |
| Plugin policy rejected (targets core type without trust) | **ERROR** | `plugin %q policy %q rejected: targets protected resource type %q` |

The WARN on elevated trust fires on every load, not just the first — operators
reviewing logs after an incident MUST see it regardless of when the server
last restarted.

### 3. Plugin Attribute Resolution

Binary plugins that declare `resource_types` MUST implement the
`AttributeResolver` gRPC service so the ABAC engine can resolve attributes
for policy evaluation. Since only binary plugins may declare `resource_types`
(§1), this service is exclusively a binary plugin concern.

#### 3.1 Proto Definition

New service in `api/proto/plugin/v1/` (or extension of the existing plugin
proto):

```protobuf
service AttributeResolver {
  // GetSchema returns the attribute schema for resource types this plugin owns.
  // Called once during plugin load.
  rpc GetSchema(GetSchemaRequest) returns (GetSchemaResponse);

  // ResolveResource returns attributes for a specific resource instance.
  // Called during ABAC policy evaluation.
  rpc ResolveResource(ResolveResourceRequest) returns (ResolveResourceResponse);
}

message GetSchemaRequest {}

message GetSchemaResponse {
  // Keyed by resource type name (e.g., "channel").
  map<string, ResourceTypeSchema> resource_types = 1;
}

message ResourceTypeSchema {
  map<string, AttributeType> attributes = 1;
}

enum AttributeType {
  ATTRIBUTE_TYPE_UNSPECIFIED = 0;
  ATTRIBUTE_TYPE_STRING = 1;
  ATTRIBUTE_TYPE_BOOL = 2;
  ATTRIBUTE_TYPE_FLOAT = 3;
  ATTRIBUTE_TYPE_STRING_LIST = 4;
}

message ResolveResourceRequest {
  string resource_type = 1;  // e.g., "channel"
  string resource_id = 2;    // e.g., "01ABC..."
}

message ResolveResourceResponse {
  map<string, AttributeValue> attributes = 1;
}

message AttributeValue {
  oneof kind {
    string string_value = 1;
    double number_value = 2;
    bool bool_value = 3;
    StringList string_list_value = 4;
  }
}

message StringList {
  repeated string values = 1;
}
```

#### 3.2 Proxy AttributeProvider

For each resource type in `resource_types`, core registers a
`PluginAttributeProvider` during `loadPlugin` that:

1. Implements the standard `AttributeProvider` interface
   (`internal/access/policy/attribute/provider.go`)
2. Routes `ResolveResource` calls over gRPC to the plugin's
   `AttributeResolver` service
3. Returns `nil, nil` for `ResolveSubject` — plugins do not resolve principal
   attributes (that's core's responsibility)
4. Returns the `Schema()` from the `GetSchema` RPC response, converted to a
   `types.NamespaceSchema`
5. Participates in the existing circuit breaker and caching infrastructure
   (the resolver already wraps all providers with these)

#### 3.3 Load Sequence

The `loadPlugin` sequence in `manager.go` becomes:

1. `host.Load()` — start the plugin process
2. `GetSchema()` — discover resource type schemas via gRPC (new)
3. Validate schema matches `resource_types` declaration (new)
4. Register proxy `AttributeProvider` per resource type with discovered
   schema (new)
5. `InstallPluginPolicies()` — validate and install policies (existing,
   expanded validation)
6. Register commands in the command registry (existing)
7. Register provided services in the service registry (existing)

If `GetSchema` fails or returns types not in `resource_types`, the plugin
load MUST fail.

#### 3.4 Cross-Validation

At load time, the policy installer SHOULD cross-check installed policies
against the discovered schema: if a policy references `resource.<type>.<attr>`
and `<attr>` is not in the schema for `<type>`, log a WARN. This is a
non-fatal diagnostic — the policy still installs, but operators are alerted
to potential mismatches.

### 4. Manifest Validation Warnings

Non-fatal warnings logged at INFO during plugin load:

| Condition | Warning |
|---|---|
| Command in `commands:` has no execute policy in `policies:` | `plugin %q: command %q has no execute policy — command will be blocked by ABAC` |
| Command declares capabilities on resource type with no matching policy | `plugin %q: command %q declares capability on %q but no policy covers that resource type` |

These warnings help plugin authors catch common mistakes without making the
system brittle.

### 5. Test Plugin: `test-abac-widget`

A minimal binary plugin that exercises the full pipeline. Lives in `test/`
(test infrastructure, not a real plugin).

#### 5.1 Manifest

```yaml
name: test-abac-widget
version: 1.0.0
type: binary
resource_types: [widget]
binary-plugin:
  executable: test-abac-widget
commands:
  - name: widget
    capabilities:
      - action: read
        resource: widget
        scope: self
    help: "Test widget command"
    usage: "widget <id>"
policies:
  - name: widget-execute
    dsl: >-
      permit(principal is character, action in ["execute"],
      resource is command) when { resource.command.name == "widget" }
  - name: widget-read-normal
    dsl: >-
      permit(principal is character, action in ["read"],
      resource is widget) when { resource.widget.type == "normal" }
  - name: widget-forbid-restricted
    dsl: >-
      forbid(principal is character, action in ["read"],
      resource is widget) when { resource.widget.type == "restricted" }
```

#### 5.2 AttributeResolver Implementation

`GetSchema` returns:

```text
widget:
  type: string       ("normal", "restricted")
  owner: string      (character ID)
```

`ResolveResource` returns hardcoded attributes based on resource ID for
testing determinism.

#### 5.3 E2E Test Coverage

| # | Test | Validates |
|---|---|---|
| 1 | Plugin loads with `resource_types: [widget]` | Manifest parsing, schema discovery |
| 2 | Character-level policies installed | Policy installer trust boundary expansion |
| 3 | Command dispatch passes Layer 1 | Execute policy from plugin |
| 4 | Command dispatch passes Layer 2 pre-flight | Capability on widget resource + optimistic pass |
| 5 | ABAC evaluation: normal widget → permit | Full pipeline with plugin-resolved attributes |
| 6 | ABAC evaluation: restricted widget → forbid | Forbid policy with plugin-resolved attributes |
| 7 | Policy targeting `resource is location` → rejected | Trust boundary enforcement |
| 8 | Missing execute policy → warning logged | Manifest validation warnings |
| 9 | Trust escalation without server allowlist → rejected | Trust escape hatch safety |

### 6. Seed Policy Migration

Each plugin owns its execute policies. The seed retains only core
resource-type policies and permits for unimplemented commands.

#### 6.1 Plugin Policy Additions

Each core plugin's `plugin.yaml` gains a `policies:` section with execute
policies for its commands:

| Plugin | Commands | Policy type |
|---|---|---|
| core-communication | say, pose, page, whisper, emit, ooc, wall | All-player execute permit |
| core-communication | pemit | Storyteller/admin-restricted execute permit |
| core-objects | examine, set | All-player execute permit |
| core-objects | create, describe | Builder-restricted execute permit |
| core-help | help | All-player execute permit |
| core-aliases | alias, unalias, aliases | All-player execute permit |
| core-aliases | sysalias, sysunsalias, sysaliases | Admin-restricted execute permit |
| core-building | dig, link | Builder-restricted execute permit |
| core-scenes | scene, scenes | Execute permit (role-gating per scene design) |

#### 6.2 Seed Removals

| Seed policy | Action | Reason |
|---|---|---|
| `seed:player-basic-commands` | Trim to `quit`, `look`, `go`, `who` | `quit` is compiled-in; `look`, `go`, `who` are unimplemented |
| `seed:builder-commands` | Remove entirely | dig, create, describe, link all in plugins |
| `seed:pemit-storyteller` | Remove entirely | pemit is in core-communication |
| `seed:player-teleport` | Keep | teleport, home are unimplemented |
| All spatial/property/scene/exit/stream policies | Keep | Core resource types |
| Builder/admin role policies (non-command) | Keep | Core access control |
| System bootstrap policies | Keep | Server startup |

#### 6.3 Seed After Migration

The seed drops from 27 policies to 24 (removes `seed:builder-commands`,
`seed:pemit-storyteller`, trims `seed:player-basic-commands`).
`seed:player-basic-commands` version bumps to v5.

#### 6.4 Channel Plugin Cleanup (on channels branch)

Once this infrastructure lands, the channel PR (#191) rebases and:

- Adds `resource_types: [channel]` to plugin.yaml
- Moves 11 channel policies from seed.go to plugin.yaml `policies:` section
- Adds execute policy for the `channel` command
- Restores capabilities on the `channel` command (previously stripped)
- Channel plugin does NOT yet implement `AttributeResolver` — continues
  handling authorization internally. Policies with resource conditions pass
  pre-flight via optimistic matching. Implementing `AttributeResolver` for
  channels is a future task.

### 7. Files Changed

| File | Change |
|---|---|
| `internal/plugin/manifest.go` | Add `ResourceTypes`, `Trust` fields; validation |
| `internal/plugin/policy_installer.go` | Replace principal-type check with §2.1 rules |
| `internal/plugin/manager.go` | Schema discovery, proxy provider registration in `loadPlugin` |
| `api/proto/plugin/v1/*.proto` | `AttributeResolver` service definition |
| `internal/plugin/attribute_proxy.go` | New: proxy `AttributeProvider` implementation |
| `internal/access/policy/seed.go` | Trim per §6.2 |
| `plugins/core-communication/plugin.yaml` | Add policies section |
| `plugins/core-objects/plugin.yaml` | Add policies section |
| `plugins/core-help/plugin.yaml` | Add policies section |
| `plugins/core-aliases/plugin.yaml` | Add policies section |
| `plugins/core-building/plugin.yaml` | Add policies section |
| `plugins/core-scenes/plugin.yaml` | Add policies section |
| `test/plugins/test-abac-widget/` | New: test binary plugin |
| `test/integration/plugin_abac/` | New: E2E test suite |

### 7a. Plugin Lifecycle Validation Split

**Problem:** `Capability.Validate()` in `internal/command/types.go` checks
resource types against a hardcoded `validResourceTypes` set. This is called
during `ParseManifest` → `CommandSpec.Validate()` → `cap.Validate()`, which
runs in `Discover()` — before any plugin resource types are known. Plugins
declaring capabilities on their own resource types (e.g., `widget`) are
rejected before loading starts.

**Root cause:** Structural validation (is the YAML well-formed?) and semantic
validation (does this resource type exist?) are tangled in the same call
path. Semantic validation requires cross-plugin context that Discover doesn't
have.

#### 7a.1 Capability Validation Split

`Capability.Validate()` is split into two methods:

| Method | Called during | Checks |
|---|---|---|
| `Validate()` (modified) | `ParseManifest` / Discover | action non-empty + known, resource non-empty, scope valid |
| `ValidateResourceType(known map[string]bool)` (new) | `loadPlugin` | resource type is in provided set (core + plugin-declared) |

`Validate()` no longer checks resource type membership. Resource type
validation is deferred to load time when the full context is available.

#### 7a.2 LoadAll Lifecycle Phases

`LoadAll` becomes explicitly multi-phase:

1. **Discover** — scan filesystem, structural validation only
2. **Collect context** — `collectResourceTypes(discovered)` merges core
   `validResourceTypes` with all `resource_types` from discovered manifests
3. **Resolve order** — dependency resolution (existing)
4. **Load each** — `loadPlugin(ctx, plugin, knownResourceTypes)` performs
   semantic validation, starts process, discovers schema, installs policies,
   registers commands

Phase 2 is new. It assembles cross-plugin knowledge before any individual
plugin is loaded.

#### 7a.3 Semantic Validation in loadPlugin

Before policy installation, `loadPlugin` validates capabilities:

```go
for _, cmd := range dp.Manifest.Commands {
    for _, cap := range cmd.Capabilities {
        if err := cap.ValidateResourceType(knownResourceTypes); err != nil {
            return oops.In("manager").With("plugin", dp.Manifest.Name).
                With("command", cmd.Name).Wrap(err)
        }
    }
}
```

#### 7a.4 Files Changed

| File | Change |
|---|---|
| `internal/command/types.go` | `Validate()` drops resource type check; add `ValidateResourceType(known)` |
| `internal/command/types_test.go` | Update tests for split |
| `internal/plugin/manager.go` | `LoadAll` gains phase 2; `loadPlugin` gains semantic validation |
| `plugins/test-abac-widget/plugin.yaml` | Restore capabilities |

### 8. Out of Scope

- `ChannelAttributeProvider` — channel plugin continues internal
  authorization until it implements `AttributeResolver`
- Server-side plugin marketplace/approval flow
- Server config format for trust allowlist (documented as TBD; a simple
  environment variable or config key suffices for now)
- Plugin-contributed principal attribute providers (plugins only resolve
  resource attributes for types they own)

### 9. Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Plugin installs overly permissive character policy | Resource-type scoping limits blast radius; command scoping validates names |
| Schema discovery RPC fails at load time | Plugin load fails hard — no partial state |
| Proxy provider latency affects ABAC evaluation | Existing circuit breaker + caching infrastructure applies automatically |
| Seed migration breaks existing E2E tests | Full test suite (59 E2E + unit) must pass after migration |
| Trust escalation abused by malicious plugin | Requires server-side allowlist — admin must explicitly opt in |

---

## Hardening (2026-04-07)

This section documents hardening work that landed after the original spec and
resolved two architectural sharp edges identified during the subsequent plugin
ABAC review. See `docs/superpowers/specs/2026-04-07-plugin-abac-hardening-design.md`
for the full design.

### Sharp Edge 1 — synthetic `__preflight__` resource ID → eliminated

The original design had `engine.CanPerformAction` construct a synthetic
resource ID of the form `<resourceType>:__preflight__` and call
`resolver.Resolve`. This caused plugin `ResolveResource` RPCs to be invoked
with fake instance IDs during type-level capability pre-flight, with no
documented contract for how plugins should handle the case.

**Resolution (C1):** `Resolver.ResolveSubjectAttributes(ctx, subject, action)`
was added as a new entry point that resolves only subject, environment, and
action attributes — resource providers are never called. `CanPerformAction`
was rewritten to use this method. The `__preflight__` literal was deleted
from `internal/access/policy/engine.go` (enforced by a static test in
`internal/access/policy/hardening_invariants_test.go`).

**New invariant:** `PluginAttributeProvider.ResolveResource` is called if and
only if the host has a real resource instance ID that corresponds to a
resource owned by that namespace. There is no synthetic ID, no sentinel, no
preflight-aware code path, and no documented plugin contract for handling
non-existent IDs.

**Guidance superseded:** any guidance in this original spec suggesting that
plugins "should handle non-instance IDs gracefully" is obsolete. Plugins only
receive real instance IDs.

### Sharp Edge 2 — silent schema validation drops → load-time fatal

The original design logged a `slog.Info` warning when a manifest policy
referenced an attribute not in the plugin's `GetSchema` response. A typo in
the policy DSL (e.g., `resource.widget.tipe` instead of `resource.widget.type`)
would produce a silent always-false condition discoverable only by reading
load-time log lines.

**Resolution (S1):** the cross-validation logic was extracted from
`CheckManifestWarnings` Warning 3 into a new function
`ValidateManifestPolicySchemas` in `internal/plugin/policy_schema_validator.go`.
This function is called from `Manager.loadPlugin` before policy installation,
and a non-nil return fails the load via the existing rollback path. Runtime
`mergeAttributes` drop behavior (warn + Prometheus counter, non-fatal) is
unchanged — the runtime path catches a different failure mode (plugin returns
keys outside its declared schema) and remains lenient to avoid breaking
healthy plugin traffic.

The resolver also gained an `UnregisterProvider` method so that failed
validation cleanly removes the plugin's attribute provider from the registry,
allowing a fixed manifest to be re-loaded without "already registered" errors.
The Task 14 investigation also closed four additional rollback leak paths in
`Manager.loadPlugin` beyond just the validator-failure branch.

### Test coverage added

- 11 unit tests on `Resolver.ResolveSubjectAttributes` (internal/access/policy/attribute/resolver_test.go)
- 1 engine unit test (T5) + 1 optimistic-permit test (T6) + reuse of 4 pre-existing CanPerformAction tests as regression coverage (T7/T22/T23/T24)
- 1 static invariant test (T32 — asserts `__preflight__` is absent from engine.go)
- 12 unit tests on `ValidateManifestPolicySchemas`
- 4 integration tests on load-time validation failure with real plugin binary + PostgreSQL
- 3 integration tests on the C1 invariant with counting proxy over the real plugin client
- 1 unit test on `Resolver.UnregisterProvider` and 1 manager-level test (T35) on rollback

### Documentation

- Plugin author guide: `site/docs/extending/abac-attribute-resolver.md`
- Internal doc comments updated in `pkg/plugin/service.go` and `plugins/test-abac-widget/main.go`
