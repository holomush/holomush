# Phase 3b: Documentation Completeness & Accuracy Review

## Summary

18 findings total: 0 Critical, 4 High, 8 Medium, 6 Low.

The internal Go code (godoc) is well-documented across all new infrastructure. The major gap is in user-facing documentation: the plugin author guide (`site/docs/extending/plugin-guide.md`) predates this PR and does not cover the new `requires`/`provides`/`storage` manifest fields, the `ServiceProvider` SDK interface, the storage SDK, or the proto-first architecture. Operators have no documentation for schema provisioning, the plugin admin commands, or connection pool implications.

---

## 1. Godoc on Exported Types

### DOC-01 — Godoc coverage is comprehensive for new infrastructure

**Severity:** Low (positive finding)
**Files:** `registry.go`, `registered_service.go`, `dependency.go`, `grpc_proxy.go`, `inprocess_conn.go`, `schema_provisioner.go`, `host.go`, `grpc_server.go`, `service.go`, `storage/storage.go`, `capability.go`, `cap_*.go`, `plugin_admin.go`

All new exported types, interfaces, methods, and constants have godoc comments that describe purpose, parameters, return values, and error conditions. `ResolveDependencyOrder` is exemplary -- it documents both edge types, the role of `serverServices`, and all four error codes. `SanitizeErrorForPlugin` documents the correlation ID mechanism.

No remediation needed.

### DOC-02 — `PluginLister` interface lacks godoc on methods

**Severity:** Low
**File:** `internal/command/handlers/plugin_admin.go:19-22`

```go
type PluginLister interface {
    ListPlugins() []string
    GetLoadedPlugin(name string) (*plugins.DiscoveredPlugin, bool)
}
```

The interface type itself has a comment, but neither method has a godoc line. Since this is an ISP interface exposed to the command layer, brief method docs would help future contributors understand the contract (e.g., whether `ListPlugins` returns loaded-only or all discovered).

**Recommendation:** Add one-line godoc to each method.

### DOC-03 — `SessionCapability` missing compile-time interface check

**Severity:** Low
**File:** `internal/plugin/hostfunc/cap_session.go`

`AliasCapability`, `PropertyCapability`, and `WorldQueryCapability` all have `var _ Capability = (*)...` compile-time checks. `SessionCapability` does not. This was noted as CQ-13 in Phase 1.

**Recommendation:** Add `var _ Capability = (*SessionCapability)(nil)`.

### DOC-04 — `TypeServerInternal` accessor lacks rationale in godoc

**Severity:** Low
**File:** `internal/plugin/registered_service.go:36`

The unexported constant `typeServerInternal` and exported accessor `TypeServerInternal()` exist to prevent external packages from comparing against the raw string `"server-internal"`. The godoc says what it returns but not *why* this pattern exists (encapsulation of the constant).

**Recommendation:** Add a sentence: "This accessor is provided so external packages can construct RegisteredService values for server-internal services without depending on the internal constant value."

---

## 2. Manifest Schema Documentation

### DOC-05 — Plugin guide missing `requires`, `provides`, `storage` fields

**Severity:** High
**File:** `site/docs/extending/plugin-guide.md`

The plugin guide's manifest table (lines 44-51) lists `name`, `version`, `type`, `events`, `policies`, `lua-plugin` as fields. The new `requires`, `provides`, and `storage` fields are completely absent. A plugin author reading this guide would not know these fields exist.

The binary plugin example (lines 186-199) shows a minimal manifest without these fields. The actual `core-scenes/plugin.yaml` is the only living example that uses all three, but it is not referenced.

**Recommendation:** Add a "Service Contracts" section after "ABAC Policies" covering:
- `requires: []` -- list of fully qualified proto service names the plugin consumes
- `provides: []` -- list of proto service names the plugin implements (binary only)
- `storage: kv|postgres` -- storage tier (postgres requires binary type)
- Example manifest showing all three fields (use core-scenes as reference)
- Explanation of what happens at load time: service injection, schema provisioning

### DOC-06 — Plugin guide missing `commands` field documentation

**Severity:** Medium
**File:** `site/docs/extending/plugin-guide.md`

The manifest table does not include `commands`. The `commands` field with its `capabilities`, `help`, `usage`, `helpText`/`helpFile` subfields is undocumented in the guide. Plugin authors must reverse-engineer the format from existing manifests.

**Recommendation:** Add a "Commands" subsection to the manifest documentation with the `CommandSpec` fields.

### DOC-07 — Plugin guide binary plugin example does not show `ServiceProvider`

**Severity:** High
**File:** `site/docs/extending/plugin-guide.md:169-260`

The binary plugin section shows only the `Handler` interface (`HandleEvent`). The new `ServiceProvider` interface (`RegisterServices` + `Init`) and `ServeWithServices` entry point are not documented. A plugin author wanting to build a service-providing binary plugin has no guide.

**Recommendation:** Add a "Service-Providing Binary Plugins" section after the basic binary example, showing:
1. `ServiceProvider` interface and its two methods
2. `pluginsdk.ServeWithServices()` as the entry point
3. `Init` receiving `ServiceConfig` with connection string
4. `RegisterServices` registering gRPC services on the transport
5. The `pkg/plugin/storage` SDK for database access

### DOC-08 �� Plugin guide missing `dependencies` field and version constraint documentation

**Severity:** Medium
**File:** `site/docs/extending/plugin-guide.md`

The manifest supports `dependencies: map[string]string` for named plugin dependencies with semver constraints, and `engine: string` for HoloMUSH version constraints. Neither is documented in the plugin guide.

**Recommendation:** Add to the manifest reference table and include a brief example.

---

## 3. Design Spec Accuracy

### DOC-09 — Spec section 5.3 describes PostgreSQL role creation that was not implemented

**Severity:** High
**File:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md:258-263`

The spec states (step 3): "Plugin manager creates a schema-scoped PostgreSQL role with: USAGE and CREATE on plugin_<name> schema; No access to public schema or any other plugin's schema."

The actual `SchemaProvisioner.ProvisionSchema()` only creates the schema (`CREATE SCHEMA IF NOT EXISTS`). No restricted PostgreSQL role is created. Plugins receive the server's full DB credentials. This was identified as SEC-01 in Phase 2 (CVSS 7.5).

Section 10.2 ("Schema Isolation") also states: "Plugin PostgreSQL roles are scoped to their own schema. No cross-schema access prevents plugins from reading or modifying core data."

Both sections describe a security guarantee that does not exist in the implementation.

**Recommendation:** Update the spec to mark role-based isolation as "Future" and document the current state: schema-only isolation via `search_path`, acceptable for first-party plugins, must be implemented before third-party plugin support. Add a tracking issue reference.

### DOC-10 — Spec section 7 (Lua code generation) describes auto-generation that was not implemented

**Severity:** Medium
**File:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md:305-364`

Section 7 describes a proto-to-Lua code generator that auto-generates host function bindings. The implementation uses hand-written capability modules (`cap_alias.go`, `cap_session.go`, etc.) instead. D7 ("Lua host functions auto-generated from proto") is listed as a design decision but was not achieved in this PR.

This is acceptable as a phased approach, but the spec reads as if generation is the implemented approach.

**Recommendation:** Add a note at the top of Section 7: "Phase 2 implemented hand-written capability modules as an intermediate step. Proto-generated bindings are tracked as follow-up work." Update D7 rationale to note it is aspirational.

### DOC-11 — Spec section 9 lists admin commands not yet implemented

**Severity:** Medium
**File:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md:397-410`

The spec lists 7 admin commands: `plugin list`, `plugin info`, `plugin reload`, `plugin disable`, `plugin enable`, `plugin reset-data`, `plugin purge`. Only `plugin list` and `plugin info` were implemented in Phase 4. The other 5 are missing.

**Recommendation:** Add a "Status" column to the table marking which commands are implemented. Track the remaining 5 as follow-up work.

### DOC-12 — Spec section 2.2 lists services not yet implemented

**Severity:** Low
**File:** `docs/superpowers/specs/2026-04-05-plugin-architecture-rework-design.md:96-100`

The spec lists 5 server-provided services. Only `WorldService` was implemented. `SessionService`, `AuthService`, `ContentService`, and `EventService` protos do not exist. This is acceptable for incremental delivery but the table implies all five exist.

**Recommendation:** Add a status column or note indicating only WorldService is implemented.

---

## 4. CLAUDE.md Updates

### DOC-13 — CLAUDE.md does not reflect new plugin architecture

**Severity:** Medium
**File:** `CLAUDE.md`

Several sections are stale or incomplete:

1. **Line 11:** "Lua plugin system (gopher-lua) with go-plugin for complex extensions" -- accurate but understates the architecture change. No mention of service registry, proto-first contracts, or DAG resolution.

2. **Line 537:** `plugin/` described as "Plugin system (Lua host, manifests, subscribers)" -- missing service registry, capability modules, schema provisioner, grpc proxy.

3. **Line 547:** `plugins/` described as "Lua plugins" -- now includes binary plugins (`core-scenes`).

4. **No mention** of `pkg/plugin/storage/` in the directory tree.

5. **No mention** of the `requires`/`provides`/`storage` manifest fields or the service contract pattern.

6. **Plugin Tests section (line 359-367)** only describes Lua VM state isolation. No guidance on testing binary plugins or the goplugin host.

No stale references to `ServiceProxy`, `LocalPluginHost`, or `type:core` were found (those were already cleaned up).

**Recommendation:** Update CLAUDE.md:
- Expand the plugin directory description to include registry, capabilities, goplugin, schema provisioner
- Change `plugins/` description to "Lua and binary plugins"
- Add `pkg/plugin/storage/` to the tree
- Add a brief note in the Plugin Tests section about binary plugin testing patterns
- Mention the proto-first service contract model in the architecture or patterns section

---

## 5. Proto Documentation

### DOC-14 — `scene.proto` messages lack field-level comments

**Severity:** Medium
**File:** `api/proto/holomush/scene/v1/scene.proto`

`world.proto` has excellent per-field comments on every message and field. `scene.proto` has almost none. `SceneInfo` fields like `state`, `pose_order_mode`, `visibility` have no comments explaining valid values. Request messages like `GetSceneRequest.session_id` do not explain what `session_id` represents (it functions as the character/subject ID for authorization).

Compare with `world.proto:30-41` where every field of `LocationInfo` has a comment.

**Recommendation:** Add per-field comments to all `scene.proto` messages, matching the standard set by `world.proto`. Specifically document:
- Valid `state` values (open, closed, etc.)
- Valid `visibility` values
- Valid `pose_order_mode` values
- What `session_id` means in request messages (the acting character's session)
- Valid `role` values in `ParticipantInfo`

### DOC-15 — `scene.proto` service RPCs lack comments

**Severity:** Medium
**File:** `api/proto/holomush/scene/v1/scene.proto:12-22`

The `SceneService` service definition lists 9 RPCs with no comments. `WorldService` and `PluginService` both document every RPC. The scene service should follow the same pattern.

**Recommendation:** Add one-line comments to each RPC (e.g., `// CreateScene creates a new RP scene at the caller's location.`).

---

## 6. Plugin Author Guide

### DOC-16 ��� No documentation for plugin storage SDK

**Severity:** High
**File:** `site/docs/extending/plugin-guide.md` (missing section)

The `pkg/plugin/storage` package provides `Connect`, `RunMigrations`, and `ParseSchemaFromConnString`. These are the primary APIs a binary plugin author uses for database access. None are documented in the plugin guide.

A plugin author would need to:
1. Know that `storage: postgres` in their manifest triggers schema provisioning
2. Know that `Init(ctx, config)` receives the connection string in `config.ConnectionString`
3. Know to embed migrations and call `storage.RunMigrations`
4. Understand the sequential naming convention (`000001_name.up.sql`)

**Recommendation:** Add a "Plugin Storage" section covering the full flow from manifest declaration through Init to migration execution, referencing `core-scenes` as the canonical example.

---

## 7. Operator Documentation

### DOC-17 — No operator documentation for plugin management

**Severity:** Medium
**File:** `site/docs/operating/` (missing content)

Operators have no documentation covering:
- Plugin discovery: where plugins are found, auto-discovery behavior
- Schema provisioning: what happens to the database when a `storage: postgres` plugin loads
- Connection pool implications: 3 independent `pgxpool.Pool` instances (core, provisioner, per-plugin) noted in PERF-4
- The `plugin list` and `plugin info` commands
- How to diagnose plugin load failures (error codes, log patterns)
- SEC-06: subprocess environment inheritance -- operators should know binary plugin processes inherit the full server environment

**Recommendation:** Add a "Plugin Operations" page to `site/docs/operating/` covering discovery, schema lifecycle, admin commands, monitoring, and security considerations for binary plugins.

### DOC-18 — Plugin guide `host functions` section is stale

**Severity:** Medium
**File:** `site/docs/extending/plugin-guide.md:103-148`

The "Host Functions" section documents `holomush.log`, `holomush.kv_get/set/delete`, `holomush.query_location/character/object`, etc. With the capability module decomposition (Phase 2), new host functions are available under different namespaces:

- `alias.*` (7 functions) from `AliasCapability`
- `session.*` (5 functions) from `SessionCapability`
- `property.*` (3 functions) from `PropertyCapability`
- `world_ext.*` (2 functions) from `WorldQueryCapability`

None of these are documented in the plugin guide. The guide only shows the base `holomush.*` functions.

Furthermore, the capability modules are injected based on `requires` declarations, which is not explained. A Lua plugin author cannot discover that declaring `requires: [holomush.session.v1.SessionService]` gives them access to `session.find_by_name()`.

**Recommendation:** Add a "Capability Host Functions" section documenting each namespace, its proto service trigger, and the available functions with signatures and return types.

---

## Findings Summary

| ID | Severity | Category | Description |
|----|----------|----------|-------------|
| DOC-01 | Low (+) | Godoc | Infrastructure godoc is comprehensive |
| DOC-02 | Low | Godoc | `PluginLister` methods lack godoc |
| DOC-03 | Low | Godoc | `SessionCapability` missing compile-time check |
| DOC-04 | Low | Godoc | `TypeServerInternal` accessor lacks rationale |
| DOC-05 | High | Plugin guide | Missing `requires`/`provides`/`storage` fields |
| DOC-06 | Medium | Plugin guide | Missing `commands` field documentation |
| DOC-07 | High | Plugin guide | Missing `ServiceProvider` and `ServeWithServices` docs |
| DOC-08 | Medium | Plugin guide | Missing `dependencies` and `engine` fields |
| DOC-09 | High | Design spec | Spec claims PostgreSQL role isolation that was not implemented |
| DOC-10 | Medium | Design spec | Spec describes auto-generation not implemented |
| DOC-11 | Medium | Design spec | Admin commands table shows 7, only 2 implemented |
| DOC-12 | Low | Design spec | Server services table shows 5, only 1 implemented |
| DOC-13 | Medium | CLAUDE.md | Does not reflect new plugin architecture |
| DOC-14 | Medium | Proto | `scene.proto` messages lack field-level comments |
| DOC-15 | Medium | Proto | `scene.proto` RPCs lack comments |
| DOC-16 | High | Plugin guide | No documentation for storage SDK |
| DOC-17 | Medium | Operator docs | No operator documentation for plugin management |
| DOC-18 | Medium | Plugin guide | Capability host functions undocumented |
