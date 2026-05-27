<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# `legacy_id` Elimination — Design

## Status

**REVISION 4 (READY — design-reviewer cleared 2026-05-04).** Top-level epic `holomush-w9ml`. Eliminates `eventbusv1.Actor.legacy_id` and the upstream `core.Actor.ID` overloading in favor of uniform ULID identity for plugin actors at every layer, per Phase 3d Decision 6. Cross-cutting refactor with its own design + plan + execution cycle.

**Revision history:**

- **Revision 1** (2026-05-04 initial) — caught NOT READY by `design-reviewer` with seven blocking findings: (1) migration number 000016 was already taken; (2) `internal/plugin/registry.go` already exists with `ServiceRegistry`; (3) bootstrap orphan check used wrong literal (`'ACTOR_KIND_PLUGIN'` vs. actual `'plugin'`); (4) wire-format removal list omitted four production callsites and the `Actor.LegacyID` field declaration; (5) Phase 3d INV-49 retarget under-specified the test changes (one of three `It` blocks, plus a parallel block in `e2e_test.go`, plus a helper); (6) the spec proposed lookup-at-bus-boundary, but the actual upstream stamp site is `core.Actor.ID = pluginName` — `core.Actor` itself carries the mixed-semantic identifier and must be addressed; (7) AAD migration window: removing `legacy_id` from the proto changes the marshaled Actor bytes for plugin actors, which would break AEAD verification on pre-cutover JetStream messages.
- **Revision 2** (2026-05-04) — addressed all seven Revision 1 blockers. Migration renumbered to 000018; new file at `internal/plugin/identity_registry.go` with type `IdentityRegistry`; orphan check uses `eventbus.ActorKindPlugin.String()` constant; wire-format removal list expanded to enumerate every grep-confirmed callsite plus the `eventbus.Actor.LegacyID` field; INV-49 retarget enumerates all three `It` blocks, the parallel e2e block, and the `publishSensitiveWithLegacyActor` helper; Option A adopted for the stamp-site question (plugin core actors carry a ULID-string in `core.Actor.ID` post-epic); AAD migration handled by purging the JetStream stream alongside the `events_audit` TRUNCATE. Caught NOT READY again with one new blocking finding: the universal "every stamp site must produce a ULID" contract collided with three `ActorSystem` production stamp sites that carry non-ULID categorical labels (`"system"`, `"world-service"`).
- **Revision 3** (2026-05-04) — addressed Revision 2's blocker by extending uniformity: INV-W9ML-1 strengthened to apply at every kind, sentinel ULID constants introduced for system actors, four non-blocking observations addressed. Caught NOT READY again with five new blocking findings: (1) system stamp-site enumeration omitted a fourth production site at `internal/core/engine_end_session.go:56`; (2) the proposed sentinel file `internal/core/actor.go` did not exist (the actual location is `internal/core/event.go`) and the pre-existing `core.ActorSystemID = "system"` constant was unaddressed; (3) the stamp-site table erroneously listed `subscriber.go:159` as a stamp site (it is a wire-format reconstruction site, not a stamp site — Lua plugins cascade via `actorFromContext`); (4) INV-W9ML-1's "no exemptions" wording collided with `ActorKindUnknown` (the legitimate absence-of-identity bucket) and Player ULID source was under-specified; (5) `IdentityRegistry.NameByID` doc comment did not enumerate the now-three resolution populations (active plugins + historical plugins + system sentinels).
- **Revision 4** (current, 2026-05-04) — addresses all five Revision 3 blockers and three non-blocking observations: (1) `engine_end_session.go:56` added to the migration enumeration as the fourth system stamp site; (2) sentinel constants placed at the correct file `internal/core/event.go`; the existing `core.ActorSystemID` constant repurposed as `var ActorSystemID = SystemActorULID.String()` so its 1 production + 4 test call sites compile unchanged; (3) `subscriber.go:159` removed from the stamp-site table with explanation that Lua plugins inherit via cascade; (4) INV-W9ML-1 explicitly excepts `ActorKindUnknown` as the absence-of-identity bucket and clarifies Player ULIDs come from the user store; (5) `NameByID` doc comment expanded to enumerate all three resolution populations. Tag-byte allocation policy added; bootstrap ordering pinned (sentinels before `ListAll`, with sentinel-collision detection on plugin row load).

Normative requirements use RFC2119 keywords (MUST, MUST NOT, SHOULD, SHOULD NOT, MAY) per the project's `CLAUDE.md` "RFC2119 Keywords" convention. Descriptive passages explaining decisions, alternatives, and future phases are not normative.

## Authors

- Sean Brandt
- Claude (collaborator)

## Date

2026-05-04

## Context

`Actor.legacy_id` (`api/proto/holomush/eventbus/v1/eventbus.proto:23-30`) is a wire-format hack that bridges plugin names (string) into the otherwise-uniform-ULID `Actor` identity model. Phase 3d Decision 6 (merged 2026-05-04) tagged it as tech debt: every `ActorKind` value should carry a ULID in `Actor.id`. Plugin name should become a display attribute, not an identity field, looked up via the plugin registry.

The tech debt extends one layer up. `core.Actor.ID` (a string field) carries a ULID for character/player actors and a plugin **name** for plugin actors today. Stamp sites at `internal/plugin/goplugin/host.go:560,566,631,637` and `internal/plugin/subscriber.go:159` set `core.Actor{Kind: ActorPlugin, ID: name}` — the same mixed-semantic-identifier antipattern as `eventbusv1.Actor.legacy_id`, just at the core layer. The `coreActorToEventbusActor` conversion at `internal/eventbus/types.go` then can't parse the name as a ULID and falls back to populating `eventbus.Actor.LegacyID`.

Phase 3d's cold-tier work (Decision 5 — envelope-bytes-faithful unmarshal) papered over the immediate AAD-divergence problem so `legacy_id` round-trips byte-for-byte through the cold tier even though it is structurally wrong. This epic fixes the underlying tech debt at every layer.

The codebase has no production deployments and no external API consumers. The design intentionally omits production-shape migration discipline (deprecation windows, backfill commands, fallback paths, reserved proto field numbers, multi-PR staging) because none of those tools protect anything in this context. INV-W9ML-6 below documents this so future readers do not interpret the omissions as oversights.

## Goals

The primary goal is to eliminate the mixed-semantic-identifier antipattern wherever plugin actor identity flows: `core.Actor.ID`, `eventbusv1.Actor.legacy_id`, the `App-Actor-Legacy-ID` JetStream header, audit projection, history reads, rendering, and tests. After this epic, **every Actor at every layer carries a ULID-as-identity**: `core.Actor.ID` is a ULID-string, `eventbusv1.Actor.id` is 16 bytes of ULID, plugin name is exclusively a display attribute resolved via the registry.

The secondary goal is to introduce the plugin registry persistence that makes uniform ULID identity possible: a `plugins` table that mints and retains a ULID per plugin name, exposes an `IdentityRegistry` interface for ULID↔name lookup, and integrates with the existing plugin lifecycle (`PluginPolicyInstaller` install/remove paths).

**Approach decision (Option A).** The registry lookup happens at the *upstream* stamp sites where `core.Actor` is constructed (`internal/plugin/goplugin/host.go` and `internal/plugin/subscriber.go`), not at the bus translation boundary. Result: `core.Actor.ID` carries a ULID-string for plugin actors uniformly with characters, the bus conversion functions simplify (no kind-special-case), and downstream consumers of `core.Actor` stop having to know whether `Actor.ID` is a ULID or a name. The alternative — lookup at the bus boundary, leaving `core.Actor.ID = pluginName` intact — was rejected because it preserves at the core layer the exact defect we are eliminating at the wire layer.

## Invariants

The following invariants MUST hold post-epic. Each is named, testable (except where explicitly documentary), and gated by at least one test before the PR may merge.

- **INV-W9ML-1 — uniform identity at every layer for every identifiable kind.** Every `Actor` value at every layer (`core.Actor`, `eventbusv1.Actor`, `corev1.Actor`) MUST carry a 16-byte ULID identity for `ActorKind ∈ {character, player, plugin, system}`. For `core.Actor`, `ID` is a string (the ULID's canonical 26-char base32 form). For `eventbusv1.Actor` and `corev1.Actor`, `id` is 16 raw bytes. ULID sources by kind:
  - **Character / Player:** the user store's ULIDs (already in place pre-epic, no change).
  - **Plugin:** registry-minted ULIDs (introduced by this epic; `IdentityRegistry.IDByName` resolves at stamp time).
  - **System:** compile-time sentinel ULID constants (`core.SystemActorULID`, `core.WorldServiceActorULID`, etc.; introduced by this epic).

  **`ActorKindUnknown` (the zero value) is the explicit absence-of-identity bucket** and MAY carry empty `id` at every layer. Production code MUST NOT stamp `ActorKindUnknown` actors; the kind exists exclusively as a defensive default at the wire-decode boundary (`internal/eventbus/subscriber.go:789` returns it for unmapped proto values, mapping to `ACTOR_KIND_UNSPECIFIED` per `publisher.go:438`). The `eventbusv1.Actor.legacy_id` proto field MUST NOT exist post-epic, and the `eventbus.Actor.LegacyID` Go field MUST NOT exist on the in-memory struct. Plugin names and system labels are exclusively display attributes resolved via the `IdentityRegistry`, never identifiers.
- **INV-W9ML-2 — `IdentityRegistry` is the resolution path.** `NameByID` and `IDByName` MUST resolve via the in-memory cache populated from the `plugins` table. Plugin emit-side stamp sites MUST consume `IDByName`. Rendering surfaces MUST consume `NameByID`. (This invariant scopes the registry's *contract* without making the universal-negative claim that no other layer holds plugin-name maps; the latter is verified by code review and the meta-grep test below, not at runtime.)
- **INV-W9ML-3 — name uniqueness.** No two currently-active registered plugins MAY share a name. Enforced at the database layer by a partial UNIQUE index on `plugins(name)` filtered by `gc_at IS NULL`.
- **INV-W9ML-4 — stable ULID across the retention window.** A plugin's ULID MUST be minted once at first registration and persist across version updates, manifest changes, content changes, transient unloads/reloads, and disk re-discovery within `RetentionDays`. After GC expiry, a re-added plugin receives a fresh ULID; prior ULIDs remain resolvable via `NameByID`.
- **INV-W9ML-5 — lifecycle-coupled policies.** Plugin-source ABAC policies MUST remain installed-on-load / removed-on-unload via the existing `PluginPolicyInstaller` path. No orphaned `plugin:*` policies may exist in the policy store.
- **INV-W9ML-6 — no production-shape migration discipline.** This invariant is documentary, not runtime-testable. The design intentionally omits backfill commands, deprecation windows, fallback read paths, reserved proto field numbers, multi-PR staging, and operator coordination steps because the codebase has no production deployments. Future readers MUST NOT introduce such tools to this design without first establishing that production deployments have been added.
- **INV-W9ML-7 — clean wire format.** The `App-Actor-Legacy-ID` JetStream header, the `eventbus.Actor.LegacyID` Go field, the `Actor.legacy_id` proto field, and the `Actor.LegacyId` generated Go field MUST NOT exist post-epic in any production Go code or proto file. Verified by: `git grep -E '\bLegacyID\b|\blegacy_id\b|App-Actor-Legacy-ID' -- '*.go' '*.proto' ':!docs/' ':!*.pb.go'` returns zero matches; AND `grep -L 'LegacyId' pkg/proto/holomush/eventbus/v1/eventbus.pb.go` confirms the regenerated file does not contain the field.
- **INV-W9ML-8 — sweep ordering.** Plugin row GC (deactivation via `gc_at`) MUST run only at end of `Manager.LoadAll`, after all currently-loaded plugins have refreshed `last_seen_at`. A plugin loading in the current process MUST NOT be GC'd in the same cycle.
- **INV-W9ML-9 — no deletion.** `internal/store/plugin_repo.go` and any callers MUST NOT issue `DELETE FROM plugins`. Verified statically by a CI grep: `git grep -n 'DELETE FROM plugins\b' -- '*.go'` returns zero matches outside test fixtures that explicitly verify the absence.

## Architecture

### Component overview

The architecture is hub-and-spoke around the plugin registry. The registry owns identity (name ↔ ULID); two layers consume the lookup.

```text
                 ┌──────────────────────────────┐
                 │     IdentityRegistry         │
                 │  (internal/plugin)           │
                 │                              │
                 │  - plugins table (Postgres)  │
                 │  - in-memory caches          │
                 │  - mutated on load/unload    │
                 │                              │
                 │  Public lookups:             │
                 │    NameByID(ULID)            │
                 │    IDByName(string)          │
                 └──────┬───────────────────┬───┘
                        │                   │
                        ▼                   ▼
              ┌──────────────┐    ┌────────────────────┐
              │ Plugin emit  │    │  Rendering layer   │
              │ stamp sites  │    │  (telnet, web)     │
              │              │    │                    │
              │ At stamp:    │    │ At render:         │
              │  core.Actor  │    │  parse Actor.ID    │
              │  .ID =       │    │   as ULID          │
              │  IDByName    │    │   -> NameByID      │
              │  (name)      │    │  for display       │
              │  .String()   │    │                    │
              └──────────────┘    └────────────────────┘
```

The lookup happens at the *upstream* stamp sites where `core.Actor` is constructed. Post-stamp, the ULID flows uniformly through every layer (core → eventbus → audit → JetStream → subscribe → core → render). Bus conversion functions simplify because they always parse `core.Actor.ID` as a ULID, never as a name.

The ABAC engine is **not** a registry consumer. `AccessRequest.Subject` is constructed at every call site by code that already has the plugin name (verified at `internal/eventbus/authguard/guard.go:127`, `internal/plugin/policy_installer.go:95-233`, `internal/access/prefix.go:64`, and all `NewAccessRequest` call sites). The engine matches Subject strings; it never sees an `Actor`. The registry's contribution to ABAC is *indirect*: the partial UNIQUE index on `plugins(name)` (INV-W9ML-3) guarantees that callers constructing `plugin:<name>` Subject strings reference at most one currently-active plugin.

### Data flow

A plugin emits an event: the host's plugin invocation path (`goplugin/host.go` for binary plugins, `subscriber.go` for Lua plugins) calls `registry.IDByName(pluginName)` and stamps `core.Actor{Kind: ActorPlugin, ID: pluginID.String()}`. The actor is set on the request context. `event_emitter.go::Emit` reads it via `actorFromContext`, the bus conversion parses `Actor.ID` as a ULID, and the eventbus event is published with `Actor.id = <ULID bytes>`. The audit projection writes `actor_id = <ULID>` to the `events_audit` row.

A reader queries history: events return with `Actor.id` populated; the bus→core conversion at the gRPC subscribe boundary reconstructs `core.Actor{Kind: ActorPlugin, ID: pluginID.String()}`. Rendering surfaces (telnet, web) parse `core.Actor.ID` as a ULID and call `registry.NameByID` to resolve the display name.

### Failure modes

- **`IDByName` returns `(_, false)` at stamp time.** A plugin invocation arrives for a name that is not in the registry. This is a contract violation; the host MUST refuse to dispatch and surface structured error code `PLUGIN_UNREGISTERED_INVOKE`. No silent fallback, no auto-registration.
- **`NameByID` returns `(_, false)` at render time.** Display layer renders `<unknown plugin>` and logs at INFO. Triggered only if a ULID is presented that the registry has never minted (corrupt data or query against a different cluster).
- **`coreActorToEventbusActor` receives a `core.Actor.ID` that fails ULID parse for `Kind ∈ {character, player, plugin, system}`.** Post-epic this is a contract violation; the function MUST return structured error code `ACTOR_ID_NOT_ULID` rather than silently emitting an Actor with empty `id`. For `Kind == ActorKindUnknown` (the absence-of-identity bucket), empty `core.Actor.ID` is permitted and produces `eventbusv1.Actor{Kind: ActorKindUnspecified}` with empty `id`.

### Dependency direction

- `IdentityRegistry` has zero dependencies on ABAC engine, rendering, or audit projection.
- Plugin host code (`goplugin/host.go`, `subscriber.go`) gains a dependency on `IdentityRegistry` (new edge introduced by this epic — registry is constructed at Manager bootstrap and injected into the host).
- Rendering layer gains a dependency on `IdentityRegistry` (new dependency edge).
- ABAC engine has no dependency on the registry (unchanged from current state).
- Bus conversion functions (`coreActorToEventbusActor`, `busActorToCoreActor` and equivalents) lose all kind-special-case logic; they parse `Actor.ID` uniformly as ULID.

The dependency graph is acyclic; the registry is testable in isolation.

## Plugin Registry

### Schema

Migration `internal/store/migrations/000018_create_plugins.up.sql` (next-available number after the landed `000016_crypto_keys_destroyed_at` and `000017_events_audit_envelope_rename`):

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

CREATE TABLE IF NOT EXISTS plugins (
    id              BYTEA       PRIMARY KEY,
    name            TEXT        NOT NULL,
    display_name    TEXT        NOT NULL,
    version         TEXT        NOT NULL,
    manifest_hash   BYTEA       NOT NULL,
    content_hash    BYTEA,
    first_seen_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    gc_at           TIMESTAMPTZ
);

-- Name uniqueness applies only to currently-active rows. A plugin can be
-- deactivated and later re-registered with the same name; the new row gets
-- a fresh ULID. Historical rows for that name remain resolvable via
-- NameByID but are excluded from IDByName.
CREATE UNIQUE INDEX IF NOT EXISTS plugins_name_active
    ON plugins(name)
    WHERE gc_at IS NULL;

-- Eliminate legacy plugin-actor events from events_audit. Pre-migration rows
-- carried Actor.legacy_id (string) inside the envelope blob; post-migration
-- the proto field is gone, so old envelopes cannot round-trip cleanly. This
-- is irreversible at the data layer (the down migration restores schema but
-- cannot restore truncated rows).
TRUNCATE events_audit;
```

Down migration `000018_create_plugins.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors

DROP TABLE IF EXISTS plugins;
-- Note: the events_audit TRUNCATE in the up migration is irreversible. This
-- down migration only rolls back the schema; truncated rows are not restored.
```

`id` is the ULID (16 bytes) primary key. `name` is the canonical plugin identifier (matching `manifest.go:72`'s pattern `^[a-z](-?[a-z0-9])*$`, max 64 chars). `display_name` is operator-facing. `version` is the semver string from the manifest. `manifest_hash` is sha256 of the manifest.yaml bytes. `content_hash` is sha256 of the plugin's executable artifact (binary file for binary plugins; deterministic concatenation of source files for Lua plugins; NULL for setting plugins). `first_seen_at` and `last_seen_at` track the registration lifetime. `gc_at` is non-NULL once the row is deactivated by the TTL sweep.

### `IdentityRegistry` interface

A new public interface in `internal/plugin/identity_registry.go` (package `plugins` — same as the existing `internal/plugin/registry.go` which holds the unrelated `ServiceRegistry` for proto-service routing). The two registries are deliberately separate types: `ServiceRegistry` resolves proto-service-name → service-implementation; `IdentityRegistry` resolves plugin-name ↔ plugin-ULID.

```go
// IdentityRegistry resolves between a plugin's stable ULID and its registered
// name. Both lookups are O(1) in-memory map accesses backed by the plugins
// table.
//
// Consumers (plugin emit stamp sites, rendering layer) depend on this
// interface, not on the full Manager. The ABAC engine is NOT an
// IdentityRegistry consumer (Subject strings are constructed at call sites
// by code that already has the plugin name).
type IdentityRegistry interface {
    // NameByID returns the name registered for the given ULID. Resolves
    // THREE populations:
    //   1. Currently-active plugins (rows with gc_at IS NULL).
    //   2. Historically-registered plugins that have since been deactivated
    //      (rows with gc_at IS NOT NULL — preserved across the registry's
    //      lifetime per INV-W9ML-9, never deleted).
    //   3. Compile-time system actor sentinels registered at Manager
    //      bootstrap (e.g., SystemActorULID -> "system",
    //      WorldServiceActorULID -> "world-service"). Sentinels are NOT
    //      subject to the GC sweep.
    //
    // ok=false only if the ULID has never been minted/registered.
    NameByID(id ulid.ULID) (name string, ok bool)

    // IDByName returns the ULID for the currently-active plugin with the
    // given name. Does NOT resolve to historical (deactivated) ULIDs —
    // emit stamp sites only care about live registrations.
    //
    // ok=false if no currently-active plugin with that name is registered.
    IDByName(name string) (id ulid.ULID, ok bool)
}
```

The asymmetry between `NameByID` (active + historical) and `IDByName` (active only) encodes the design correctly: emit stamp sites want a live plugin's ULID; rendering/subscribe paths want any ULID the registry has ever minted.

The `Manager` type implements `IdentityRegistry`. The interface lives in `internal/plugin/`; consumers import it from there.

### Repository

A new repository `internal/store/plugin_repo.go`:

```go
type PluginRepo interface {
    // Upsert inserts a new row or updates hashes/version/last_seen_at on an
    // existing row. Returns the row's id (ULID) — newly minted on INSERT,
    // preserved on UPDATE. DriftReport is non-nil when manifest_hash or
    // content_hash differs from the stored values; caller is responsible
    // for logging.
    Upsert(ctx context.Context, in PluginUpsertInput) (id ulid.ULID, drift *DriftReport, err error)

    // ListAll returns all registered plugins (including deactivated rows);
    // used at startup to rebuild the in-memory cache.
    ListAll(ctx context.Context) ([]PluginRow, error)

    // SweepInactive sets gc_at = now() on all active rows whose last_seen_at
    // is older than retentionDays days. Returns the swept rows.
    //
    // No DELETE is ever issued from this repo (INV-W9ML-9).
    SweepInactive(ctx context.Context, retentionDays int) ([]PluginRow, error)
}
```

Upsert semantics implement the three-state behavior:

| State | Action |
|-------|--------|
| Row not found | INSERT with freshly minted `idgen.New()` ULID, hashes, `first_seen_at = last_seen_at = now()`, `gc_at = NULL` |
| Found, hashes match | UPDATE `last_seen_at = now()` only |
| Found, hashes differ | UPDATE changed hashes + version + `last_seen_at`, populate `*DriftReport` for caller logging |

A name collision against an active row (different `id`, same `name`, target row has `gc_at IS NULL`) MUST surface a hard error from the partial UNIQUE constraint; the caller MUST refuse to start.

### Lifecycle integration

In `internal/plugin/manager.go::loadPlugin`, post-Discover, pre-Load:

1. Compute `manifest_hash` (sha256 of manifest.yaml bytes).
2. Compute `content_hash` per plugin type:
   - Binary plugin: sha256 of binary file at the manifest's declared path.
   - Lua plugin: sha256 of deterministic concatenation of `*.lua` files (sorted by relative path within plugin dir, separated by a fixed delimiter).
   - Setting plugin: nil.
3. Call `repo.Upsert(...)`. Receive ULID and optional `*DriftReport`.
4. Mutate the in-memory cache: `nameByID[id] = name`; `activeByName[name] = id`. The cache MUST be updated *before* `host.Load` is invoked (downstream code may emit during Load via the `IDByName` lookup).
5. If `*DriftReport` is non-nil, emit a structured log: `plugin.drift` with `name`, old + new `manifest_hash`, old + new `content_hash`, `version_before`, `version_after`. INFO level. No decision logic.
6. Continue with the existing `host.Load(ctx, manifest, dir)` path. On `host.Load` error, the cache mutation from step 4 MUST be rolled back (`delete(activeByName, name)` and `delete(nameByID, id)`) so a failed-to-load plugin does not appear active in the registry.

In `internal/plugin/manager.go::unloadPlugin`:

1. Existing path runs (`host.Unload`, `RemovePluginPolicies`, etc.).
2. Mutate the in-memory cache: `delete(activeByName, name)`. The `nameByID` entry is intentionally retained; historical resolution is preserved.
3. The `plugins` table row is NOT modified. Unload is transient (e.g., manifest temporarily removed during dev). Only the TTL-driven sweep at end of `LoadAll` sets `gc_at`.

### Bootstrap

In `Manager` construction (or as the first step of `LoadAll`):

1. Call `repo.ListAll(ctx)` to retrieve all known plugin rows.
2. Populate the in-memory `nameByID` map from every row.
3. Populate `activeByName` only from rows where `gc_at IS NULL`.
4. Each subsequent `loadPlugin` mutates both maps via Upsert.

### Garbage collection

Configuration in `internal/plugin/config.go`:

```go
type PluginConfig struct {
    // ... existing fields ...

    // RetentionDays controls how long a plugin row persists as active in
    // the plugins table after its last successful load. Plugins not seen
    // for longer than this are deactivated (gc_at set) at the end of
    // Manager.LoadAll.
    //
    // Default: 3. Set to 0 to disable GC (active rows never expire).
    //
    // Operator note (informational): NameByID still resolves deactivated
    // plugins (asymmetric semantics — see IdentityRegistry interface), so
    // this knob does not gate audit attribution. It only controls when an
    // inactive plugin's name becomes available for re-registration with a
    // fresh ULID.
    RetentionDays int `yaml:"retention_days"`
}
```

In `Manager.LoadAll`, after all per-plugin loads complete (INV-W9ML-8 ordering):

1. Call `repo.SweepInactive(ctx, RetentionDays)`.
2. For each swept row, mutate the cache: `delete(activeByName, row.name)`. The `nameByID` entry is retained.
3. Emit a structured `plugin.gc` log per swept row with `name`, `id`, `last_seen_at`. INFO level.

### Concurrency

The in-memory cache is protected by an `sync.RWMutex`. Reads (`NameByID`, `IDByName`) take RLock; writes (Upsert, sweep, unload) take Lock. The cache is small (a few hundred plugins maximum); contention is negligible.

## Wire-Format Change & Code-Path Removal

The wire-format change is one PR with two components: (a) eliminate `legacy_id` everywhere it appears, and (b) update upstream stamp sites to feed ULIDs into `core.Actor.ID`. The removals are exhaustively enumerated below from `git grep -E 'LegacyID|legacy_id|App-Actor-Legacy-ID' -- '*.go' '*.proto'`.

### Proto schema

`api/proto/holomush/eventbus/v1/eventbus.proto`:

```proto
// Actor identifies who caused an event.
message Actor {
  ActorKind kind = 1;
  bytes id = 2; // ULID (16 bytes); MUST be set for every ActorKind value.
}
```

The `legacy_id` field is removed entirely. Field number 3 is **not** declared `reserved` because there are no historical wire bytes anywhere (events_audit is TRUNCATEd; the JetStream stream is purged at the same boundary; no external consumers exist). Future devs may freely reuse field number 3 for any new purpose.

`pkg/proto/holomush/eventbus/v1/eventbus.pb.go` is regenerated from the updated schema; the `LegacyId` Go field is removed. The doc comment on `id` is updated to drop the prior "empty for system/unknown" caveat. Post-epic, `id` is required for every `ActorKind`.

### In-memory `eventbus.Actor` struct

`internal/eventbus/types.go:53-57` — the `LegacyID string` field is removed from the Go struct entirely. Any test that constructs `eventbus.Actor{LegacyID: ...}` is updated to construct `eventbus.Actor{ID: <ULID-bytes>}` (see test fixture impact).

### JetStream header

`internal/eventbus/publisher.go` removals:

| Lines | Removed |
|-------|---------|
| 50-55 | `HeaderActorLegacyID = "App-Actor-Legacy-ID"` constant |
| 316-317 | `else if event.Actor.LegacyID != "" { ... }` branch |
| 351 | Header in the include-set map |
| 431-432 | `else if a.LegacyID != "" { p.LegacyId = a.LegacyID }` branch in actor conversion |

Post-epic, plugin events carry exactly one identifier on the wire: `App-Actor-ID` (the ULID).

### Read / fallback paths (production)

| File | Lines | Disposition |
|------|-------|-------------|
| `internal/eventbus/subscriber.go` | 770-773 | Remove `out.LegacyID = legacy` fallback |
| `internal/grpc/server.go` | 598-608 | Remove `if a.LegacyID != ""` fallback in actor-id-to-string formatter; the function returns `actor.ID.String()` always |
| `internal/grpc/query_stream_history.go` | 512-522 | Remove `else if e.Actor.LegacyID != ""` fallback in `eventbusEventToEventFrame`; ULID branch is the only branch |
| `internal/eventbus/history/hot_jetstream.go` | 592-598 | Remove `out.LegacyID = legacy` in `actorFromProto` |
| `internal/eventbus/history/cold_postgres.go` | 417-440 | Remove the `TODO(holomush-u5bb)` comment and any LegacyID-related handling in `actorFromAuditRow`; close `holomush-u5bb` as superseded (the dedicated column it proposed is no longer needed) |
| `cmd/holomush/sub_grpc.go` | 499-507 | Remove `coreActorToBusActor` non-ULID-stash branch — the function simplifies to ULID-only mapping. Non-ULID input MUST surface `ACTOR_ID_NOT_ULID` |
| `cmd/holomush/sub_grpc.go` | 598-606 | Remove `busEventToCoreEvent` LegacyID fallback. Only `case len(e.Actor.ID) > 0:` remains |

### System actor sentinel ULIDs

System actors today carry non-ULID categorical labels in `core.Actor.ID`. Production stamp sites:

| File | Line | Today's value |
|------|------|---------------|
| `internal/world/event_store_adapter.go` | 34-37 | `core.Actor{Kind: core.ActorSystem, ID: "world-service"}` |
| `internal/grpc/server.go` | 531-534 | `core.Actor{Kind: core.ActorSystem, ID: "system"}` |
| `internal/command/types.go` | 619-622 | `core.Actor{Kind: core.ActorSystem, ID: "system"}` |
| `internal/core/engine_end_session.go` | 56 | `core.Actor{Kind: ActorSystem, ID: ActorSystemID}` (where `ActorSystemID = "system"`) |

The string constant `core.ActorSystemID = "system"` is declared at `internal/core/event.go:164-165` and referenced from one production site (`engine_end_session.go:56`) and four test sites (`engine_end_session_test.go:78`, `subscriber_actor_test.go:112,125`, `goplugin/host_test.go:1896,1969`).

Per INV-W9ML-1's uniform-ULID-at-every-layer requirement, sentinel ULID constants are introduced and the existing string constant is repurposed:

```go
// internal/core/event.go (new sentinels added; existing ActorSystemID
// repurposed):

// SystemActorULID is the canonical identity for the host's "system"
// actor — the categorical bucket for events emitted by the host itself
// rather than by a character, player, or plugin. Defined as a fixed
// byte pattern (not entropy-generated) so audit rows and history
// queries reliably round-trip the same identity. The all-zero leading
// 15 bytes plus a low-numbered tag byte make sentinels visually
// distinguishable from real entropy ULIDs in logs.
var SystemActorULID = ulid.ULID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01}

// WorldServiceActorULID is the identity for events emitted by the
// world service subsystem (location/object/exit lifecycle).
var WorldServiceActorULID = ulid.ULID{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x02}

// ActorSystemID retains its name for ergonomic backward-source-compatibility
// (the existing 1 production + 4 test call sites compile unchanged), but
// post-w9ml the value is the canonical ULID-string form of SystemActorULID
// rather than the literal "system". The doc comment is updated accordingly.
var ActorSystemID = SystemActorULID.String() // "00000000000000000000000001"
```

(Note the change from `const ActorSystemID = "system"` to `var ActorSystemID = SystemActorULID.String()` — `SystemActorULID.String()` is not const-evaluable in Go, so a `var` is required. The `Actor.ID` field's inline doc comment at `event.go:170` is updated from `// Character ID, plugin name, or ActorSystemID` to `// Canonical ULID-string for the actor's identity (character/player/plugin/system).`)

Sentinels render in canonical Crockford-base32 form as `"00000000000000000000000001"` and `"00000000000000000000000002"` — visually obvious sentinels, grep-friendly, distinguishable from entropy-generated ULIDs (which carry a non-zero ms-since-epoch in the top 6 bytes per `core.NewULID()` and `idgen.New()`).

**Tag-byte allocation policy.** Sentinel ULIDs use the 16th byte (last byte) as a tag distinguishing system actor categories. Tags MUST be unique across the codebase and MUST be allocated via PR review of `internal/core/event.go` (the single source of truth). The reserved range `0x01-0xFF` provides 255 future sentinel slots; tag `0x00` is reserved as "no sentinel" and MUST NOT be used (the all-zero ULID is the proto3 zero-value and would be wire-indistinguishable from absence-of-id). Existing allocations: `0x01 = SystemActorULID`, `0x02 = WorldServiceActorULID`.

A package-private helper `isSentinelULID(id ulid.ULID) bool` in `internal/core/event.go` returns true iff the first 15 bytes are zero and the 16th byte is in range `[0x01, 0xFF]`. Used by the `IdentityRegistry` bootstrap (sentinel-collision detection on plugin row load) and by the sentinel-tag-uniqueness test below.

A `TestSentinelTagsUnique` unit test in `internal/core/event_test.go` enumerates every sentinel constant declared in the package and asserts no two share a tag byte. This is the forward-defense against silent shadowing: if a future PR adds a third sentinel with a duplicate tag, the test fails before the bug ships.

The four system-actor production stamp sites are migrated:

| File | Lines | Today | Post-epic |
|------|-------|-------|-----------|
| `internal/world/event_store_adapter.go` | 34-37 | `ID: "world-service"` | `ID: core.WorldServiceActorULID.String()` |
| `internal/grpc/server.go` | 531-534 | `ID: "system"` | `ID: core.SystemActorULID.String()` (or `ID: core.ActorSystemID` — equivalent) |
| `internal/command/types.go` | 619-622 | `ID: "system"` | `ID: core.SystemActorULID.String()` (or `ID: core.ActorSystemID`) |
| `internal/core/engine_end_session.go` | 56 | `ID: ActorSystemID` | `ID: ActorSystemID` (no change at the call site — the constant's value changes) |

The four test sites that reference `core.ActorSystemID` (`engine_end_session_test.go:78`, `subscriber_actor_test.go:112,125`, `goplugin/host_test.go:1896,1969`) compile unchanged; their assertions continue to compare against `core.ActorSystemID`, which now holds the ULID-string.

The `IdentityRegistry` registers the sentinels at `Manager` bootstrap as in-memory entries (NOT persisted to the `plugins` table — system actors are not plugins and don't belong in plugin schema):

```go
func NewManager(...) *Manager {
    m := &Manager{...}
    // Step 1: populate sentinels into nameByID FIRST. They are not in
    // activeByName (system actors are not "looked up by name"), not in
    // the plugins table (different identity domain).
    m.nameByID[core.SystemActorULID]       = "system"
    m.nameByID[core.WorldServiceActorULID] = "world-service"
    // Step 2: load plugins from the persistence layer. The loader MUST
    // skip any row whose id matches a sentinel — defense-in-depth against
    // a corrupt or hand-inserted plugin row that would silently overwrite
    // a sentinel. Such a row is treated as a startup error.
    rows := m.repo.ListAll(ctx)
    for _, row := range rows {
        if isSentinelULID(row.id) {
            return nil, oops.Code("PLUGIN_ROW_USES_SENTINEL_ID").
                With("name", row.name).Errorf("plugin row uses a reserved sentinel ULID")
        }
        m.nameByID[row.id] = row.name
        if row.gc_at == nil { m.activeByName[row.name] = row.id }
    }
    return m, nil
}
```

`NameByID(SystemActorULID) → ("system", true)`. `NameByID(WorldServiceActorULID) → ("world-service", true)`. Renderers consume the same lookup as for plugins — no kind dispatch at the renderer level.

`IDByName` is *not* the path for system actors. Stamp sites use the compile-time constants directly. This matches the architectural distinction: plugin names are operator-supplied data (registry-mediated); system labels are compile-time-defined (constant-mediated).

### Stamp-site changes (Option A — feeding ULIDs into `core.Actor.ID`)

The lookup of plugin name → ULID happens at the upstream stamp sites where `core.Actor` is first constructed for plugin actors. Four sites, all in the binary plugin host:

| File | Lines | Today | Post-epic |
|------|-------|-------|-----------|
| `internal/plugin/goplugin/host.go` | 560 | `storedActor := core.Actor{Kind: core.ActorPlugin, ID: name}` | Looks up `pluginID := registry.IDByName(name)` (refusing dispatch with `PLUGIN_UNREGISTERED_INVOKE` on miss); stamps `core.Actor{Kind: core.ActorPlugin, ID: pluginID.String()}` |
| `internal/plugin/goplugin/host.go` | 566 | `storedActor = core.Actor{Kind: core.ActorPlugin, ID: name} // re-anchor` | Same lookup pattern |
| `internal/plugin/goplugin/host.go` | 631 | `storedActor := core.Actor{Kind: core.ActorPlugin, ID: name}` | Same lookup pattern |
| `internal/plugin/goplugin/host.go` | 637 | `storedActor = core.Actor{Kind: core.ActorPlugin, ID: name}` | Same lookup pattern |

**Lua plugins do not have a dedicated stamp site.** Lua plugin emits inherit the upstream actor via the cascade mechanism in `actorFromContext` (`internal/plugin/manager.go:1242`), which reads whatever `core.Actor` is on the dispatch ctx. The ctx-level actor is set at `internal/plugin/subscriber.go:117` via `core.WithActor(tctx, actorFromIncomingEvent(event))`, where `actorFromIncomingEvent` (line 158-171) **reconstructs** a `core.Actor` from the wire-format `event.ActorID`. Post-epic, the wire-format `ActorID` carries a ULID-string for plugin actors (because the upstream binary-plugin emit site has already stamped a ULID-string), so the Lua-side reconstruction picks up the ULID directly — no name → ULID lookup is needed at the Lua reconstruction site, and `actorFromIncomingEvent` requires no `IdentityRegistry` dependency.

The `IdentityRegistry` is constructed at `Manager` bootstrap and injected into the binary plugin host (`goplugin/host.go`). The shared gate point for the runtime-symmetry invariant is the contract enforced at all stamp sites: every plugin-actor `core.Actor` carries a ULID-string in `Actor.ID`. (This is symmetric with the existing `actor_kinds_claimable` gate at `internal/plugin/event_emitter.go:145-157`, which enforces a runtime-uniform invariant at a shared gate point.)

**Cascade preservation.** The cascade branches at `host.go:563-564` and `host.go:635` (`case core.ActorCharacter, core.ActorPlugin: storedActor = upstream // verbatim`) preserve an upstream-stamped `core.Actor` when one already exists in context. These branches require no change because every upstream entry point is migrated to the registry-lookup pattern above; INV-W9ML-1 is the load-bearing invariant making cascade-preservation correct. No new stamp logic is added in the cascade branch.

**Plugin SDK actor_id semantic shift.** Plugin code that reads `e.Actor.ID` from delivered events (Lua: `event.actor_id` field; binary: `pluginv1.Event.ActorId` via `internal/plugin/goplugin/host_service.go:407`) currently sees a plugin name string for `ActorKind == plugin`. Post-epic those consumers see a ULID-string. Plugin code that compares this field against a known plugin name (e.g., `if event.actor_id == "core-scenes"`) will silently break. An in-tree grep at planning time confirmed no plugin source code (`plugins/*/`) currently does this comparison; the surface is empty today. The shift is documented here for future plugin authors who consume actor_id; if they need plugin name, they should call a new SDK helper (out-of-scope future work) or accept that actor_id is opaque.

### Bus conversion simplifications

With Option A, the bus conversion functions stop having kind-special-case logic.

`internal/plugin/event_emitter.go:310-318::coreActorToEventbusActor` — today does `if a.ID != "" { if parsed, err := ulid.Parse(a.ID); err == nil { out.ID = parsed.Bytes() } }` (silently emits empty `Actor.ID` if parse fails). Post-epic the function is uniform across all kinds: it parses `a.ID` as ULID and surfaces `ACTOR_ID_NOT_ULID` on parse failure. No kind dispatch; no fallback. The function takes no `IdentityRegistry` dependency.

`cmd/holomush/sub_grpc.go::coreActorToBusActor` and `busActorToCoreActor` — kind-special-case branches that handled `LegacyID` removed; ULID is parsed/copied uniformly across every kind.

### Audit projection

`internal/eventbus/audit/projection.go` — already reads `App-Actor-ID` for ULID at lines 217-224. No code change required: post-epic, plugin events arrive with `App-Actor-ID` populated, so `actor_id` lands in the column naturally. The projection's header allowlist, if it includes `App-Actor-Legacy-ID`, MUST be updated to drop it.

### AAD migration window

Removing `legacy_id` from the proto changes the marshaled `Actor` bytes for any actor that today carries a non-empty `LegacyID` (i.e., plugin actors). `internal/eventbus/crypto/aad/aad.go:62-67` builds AAD from `proto.MarshalOptions{Deterministic: true}.Marshal(event.GetActor())`. JetStream message headers (including `App-Actor-Legacy-ID`) are NOT part of AAD — only the proto-marshaled `Actor` contributes — so removing the header itself has no AAD impact. The Actor proto bytes do change for plugin actors: pre-cutover encrypted plugin events in JetStream were sealed against an AAD that included the `legacy_id` field bytes; post-cutover code computing AAD from the same Actor (now with no `legacy_id` field) computes a different AAD; AEAD verification fails.

Resolution:

1. **Proto3 zero-value safety for character/player/system actors.** Their `LegacyID` is always empty string. Proto3 serializes empty-string scalars as zero bytes (default-omit). Removing the field from the schema produces identical marshaled bytes for every Actor that had `LegacyID = ""`. Therefore, AAD for character/player/system actors is byte-identical pre and post cutover; their pre-cutover encrypted events continue to verify.

2. **JetStream stream purge for plugin actors.** Plugin actors today have non-empty `LegacyID`; their AAD changes at cutover. The migration MUST purge the JetStream stream alongside `events_audit` TRUNCATE so no pre-cutover plugin-actor encrypted messages survive the cutover. The purge step is part of the deploy procedure (a `task migrate:plugin-actors-cutover` command, implemented in this PR, runs the `events_audit` TRUNCATE in a transaction with a NATS `JetStream.PurgeStream` call). Both must succeed or both must roll back.

3. **No deprecation window.** Atomic cutover only — no period during which old-AAD and new-AAD events coexist in the stream.

For HoloMUSH today, the dev environment's JetStream state is small and routinely destroyed during development; the purge has no observable cost. The procedure is documented in the operator runbook addition (Section "Migration").

### Atomic rollout

The wire-format change MUST land in a single PR with no deprecation window, no feature flag, and no gradual rollout. Per INV-W9ML-6, this codebase has no production deployments; staged transition tooling protects nothing in this context.

## Consumer Integration

### Plugin emit (stamp sites)

Two upstream stamp boundaries handle plugin-authored events. Both MUST be migrated symmetrically per the project's Plugin Runtime Symmetry rule (CLAUDE.md):

**A. Binary plugin host** — `internal/plugin/goplugin/host.go` at the four stamp sites enumerated above (lines 560, 566, 631, 637). The host already holds plugin-name context for every dispatch; adding the registry lookup at these points is a localized change.

**B. Lua plugin subscriber** — `internal/plugin/subscriber.go:158-170` (`actorFromIncomingEvent`). Same lookup pattern.

Both runtimes flow into `event_emitter.go::Emit` once `core.Actor` is in context (the "shared gate point" referenced in the architecture section). Because Option A lifts the lookup *upstream* of `Emit`, both runtimes reach `Emit` with `core.Actor.ID` already populated as a ULID-string — symmetry is enforced at the stamp boundary, not at the emit boundary.

`IDByName` returning false at any stamp boundary is a contract violation — the system MUST refuse to dispatch and surface `PLUGIN_UNREGISTERED_INVOKE`.

### Subscribe path (bus → core)

The gRPC subscribe handler reconstructs `core.Actor` from `eventbusv1.Actor`. Post-epic, every `eventbusv1.Actor.id` is a 16-byte ULID; the conversion just constructs `core.Actor{Kind: ..., ID: ulid.MustParse(actor.id).String()}` uniformly. No `IdentityRegistry` dependency at the subscribe seam; the ULID flows through unchanged.

### Rendering integration

Two rendering surfaces consume the registry:

**A. Telnet renderer** — `internal/telnet/`. At the rendering boundary for plugin-actor events:

```go
// Pseudocode
func renderActor(actor core.Actor, registry plugin.IdentityRegistry) string {
    switch actor.Kind {
    case core.ActorPlugin:
        id, err := ulid.Parse(actor.ID)
        if err != nil {
            return "<malformed actor id>"
        }
        if name, ok := registry.NameByID(id); ok {
            return name
        }
        return "<unknown plugin>"
    case core.ActorCharacter:
        // ... existing path: lookup display name from session/character store ...
    }
}
```

The renderer holds an `IdentityRegistry` reference, injected at construction time. The `<unknown plugin>` fallback is reached only when a ULID has never been minted by the registry. This is symmetric with the existing character-name lookup pattern (rendering-time lookup from an authoritative source).

**B. Web renderer** — `internal/web/` plus the SvelteKit client. The server side calls `NameByID` at the same rendering boundary as telnet. The client receives display-resolved names; it does not need its own registry.

### ABAC engine — no changes required

The ABAC engine does not consume the registry. `AccessRequest.Subject` is constructed at every call site by code that already has the plugin name. No code in `internal/access/policy/` is modified by this epic. Plugin-source ABAC policies continue to be installed via the existing `PluginPolicyInstaller` path on plugin load and removed on plugin unload. The DSL's `plugin:<name>` Subject convention is unchanged.

## Migration

### Schema migration

Migration 000018 (above) creates the `plugins` table, its partial UNIQUE index, and TRUNCATEs `events_audit`. The schema and data changes are bundled in one migration file per project precedent (compare `000012_events_audit_rendering.up.sql` which combines schema + data + schema in a single file).

### JetStream purge

The migration's data-cleanup half is two operations: (a) TRUNCATE `events_audit` in Postgres, (b) PurgeStream the JetStream stream. The combined operation is implemented as a Go bootstrap step (not a SQL migration, because (b) is a NATS API call) gated to run *once* at the same boundary as the migration. Both operations MUST succeed; on any failure the deploy MUST be rolled back manually.

For HoloMUSH dev environments, the JetStream purge is destructive but inconsequential (dev streams are short-lived). The combined cleanup procedure is documented in `site/docs/operating/` as a one-time deploy step for this epic.

### Migration ordering invariants

- Migration 000018 MUST run before any plugin load. The migration runner already executes pending migrations at startup before bootstrap.
- The `plugins` table MUST be queryable by the time `Manager.LoadAll` runs. `Manager` construction calls `repo.ListAll` to populate the cache; this requires the table to exist.
- The JetStream purge MUST run after the Postgres TRUNCATE and before the first plugin emit of the new process.
- No legacy plugin-actor events MAY exist in `events_audit` post-migration. Asserted by a startup invariant check in the bootstrap path:

  ```go
  count := pool.QueryRow(ctx,
      `SELECT COUNT(*) FROM events_audit
       WHERE actor_kind = $1 AND actor_id IS NULL`,
      eventbus.ActorKindPlugin.String(), // returns "plugin" — see ActorKind.String() at publisher.go:409
  ).Scan(...)
  if count > 0 {
      return oops.Code("PLUGIN_ACTOR_ORPHAN_DETECTED").
          With("count", count).
          Errorf("legacy plugin-actor events present after w9ml migration")
  }
  ```

  The literal `"plugin"` is sourced from the `ActorKind.String()` constant rather than hard-coded to avoid drift if the enum-string mapping ever changes. This check is defense-in-depth; the TRUNCATE makes orphans impossible from a clean install, but the check forward-defends against future scenarios (manual restore from old backup, partial migration recovery).

### Plugin-owned audit tables

Plugin manifests can declare their own audit tables (CLAUDE.md cites `plugin_core_scenes.scene_log` as the example; the storage type is `postgres` per the manifest). Verified by inspection: `core-scenes` `scene_log` stores plugin-domain rows (scene_id, character_id, action, timestamp), NOT `eventbusv1.Actor` envelopes. Plugin authorship is implicit (the row lives in the plugin's own schema).

Therefore plugin-owned audit tables are NOT truncated by this migration. The proto change is invisible to them. A planning-time verification step MUST grep all plugin manifests' `storage` declarations to confirm none stores `eventbusv1.Event` envelopes verbatim; if any does, file a sub-task before proceeding.

## Test Strategy

### Acceptance criteria

The epic is complete when all of the following hold:

1. Migration 000018 creates the `plugins` table with the partial UNIQUE index. Migration is idempotent and applies cleanly to a fresh testcontainer DB.
2. Post-migration, `events_audit` is empty on first run and the JetStream stream is purged. The bootstrap orphan check (using `eventbus.ActorKindPlugin.String()`) passes.
3. `git grep -E '\bLegacyID\b|\blegacy_id\b|App-Actor-Legacy-ID' -- '*.go' '*.proto' ':!docs/' ':!*.pb.go'` returns zero matches. `grep -L 'LegacyId' pkg/proto/holomush/eventbus/v1/eventbus.pb.go` confirms the regenerated proto file is clean.
4. Plugin emit stamp sites — exactly four, all in `internal/plugin/goplugin/host.go` (lines 560, 566, 631, 637) — call `IDByName` and stamp `core.Actor{ID: pluginID.String()}`. Lua plugins inherit via cascade (no dedicated stamp site). System stamp sites — four, in `internal/world/event_store_adapter.go`, `internal/grpc/server.go`, `internal/command/types.go`, and `internal/core/engine_end_session.go` — use the sentinel constants (`core.SystemActorULID.String()` / `core.WorldServiceActorULID.String()`, or `core.ActorSystemID` which now resolves to the SystemActorULID string). Telnet and web renderers call `NameByID` (resolves plugin + system names through the same path). ABAC engine code is unchanged.
5. End-to-end happy path: a plugin emits an event → `core.Actor.ID` is a ULID-string → bus conversion parses it as ULID → JetStream message has `App-Actor-ID` populated → audit projection writes ULID to `actor_id` column → history query returns the event with `Actor.id` intact → bus→core conversion reconstructs `core.Actor.ID = ULID-string` → renderer resolves ULID back to plugin name for display. One integration test exercises this full chain.
6. GC cycle works: a plugin not seen for `RetentionDays` days has `gc_at` set on next `LoadAll` end-of-cycle sweep; subsequent re-add mints a fresh ULID; old events from the prior incarnation still display the historical name via `NameByID`.
7. **Phase 3d INV-49 retarget — fully enumerated.** All references to `LegacyID` in `test/integration/crypto/inv49_envelope_roundtrip_test.go` and `test/integration/crypto/e2e_test.go` are updated:
   - `inv49_envelope_roundtrip_test.go:59` (`It("byte-equal envelope ... for character-binding actor")`) — already uses ULID; no LegacyID reference; verify nothing breaks.
   - `inv49_envelope_roundtrip_test.go:111` (`It("byte-equal envelope for plugin actor with Actor.legacy_id (Decision 5 lock)")`) — renamed to `It("byte-equal envelope for plugin actor with Actor.id ULID (Decision 5 + w9ml lock)")`. The fixture switches from `LegacyID: "core-scenes"` to `ID: <core-scenes-plugin's-ULID>`. Assertion `Expect(ev.Actor.LegacyID).To(Equal("core-scenes"))` becomes `Expect(ev.Actor.ID).To(Equal(corePluginULIDBytes))`.
   - `inv49_envelope_roundtrip_test.go:165` (`It("cold-read decrypts correctly for both actor kinds via dispatcher chain")`) — the `useLegacyActor: true` case branch is **deleted** (with Option A's uniform identity, there is no "second case"; the test collapses to a single iteration over actor kinds, all of which carry ULIDs).
   - `e2e_test.go:647` (`It("round-trips through cold tier with Actor.legacy_id preserved")`) — **deleted** as redundant with the retargeted `inv49_envelope_roundtrip_test.go:111` block; the e2e parallel was specifically guarding the legacy-bearing cold path.
   - `publishSensitiveWithLegacyActor` helper at `e2e_test.go:225` (call site at line 657) — **deleted**; replaced with calls to the standard plugin emit path which now naturally populates `Actor.ID`. (Note: lines 121 and 189 in `inv49_envelope_roundtrip_test.go` also reference the helper — those are the per-test calls that are deleted alongside the It-block deletions enumerated above.)
8. `holomush-ojw1.8` (propagate Actor.LegacyID through plugin actor resolver) is closed as superseded with cross-reference comment. `holomush-u5bb` (LegacyID column on events_audit) is closed as superseded — the dedicated column is no longer needed because LegacyID is gone entirely.
9. **All `LegacyID`-specific tests deleted**, fully enumerated:
   - `cmd/holomush/sub_grpc_adapters_test.go:154-236` — `TestCoreToBusActorStashesNonULIDAsLegacyID`, `TestBusEventToCoreEventFallsBackToLegacyID`, plus `assert.Empty(t, a.LegacyID)` cases.
   - `internal/eventbus/publisher_test.go:284-299, 350` — LegacyID fallback test cases.
   - `internal/eventbus/history/actor_from_envelope_test.go:47-67` — `TestActorFromEnvelopeFallsBackToLegacyID`.
   - `internal/eventbus/history/cold_postgres_test.go:23-59` — the test that asserts "legacy_id must be recovered via envelope unmarshal" is retargeted to assert ULID round-trip (Decision 5's regression coverage stays; the field changes).
10. All other existing tests pass after fixture migration (see Test fixture impact below).

### Invariant → test mapping

Each invariant has at least one named enforcing test. (`holomush-s6wp` is the project-wide invariant centralization epic; this spec adopts its discipline.)

| Invariant | Statement | Enforcing test(s) |
|-----------|-----------|-------------------|
| INV-W9ML-1 | Every Actor at every layer, every kind, carries a ULID identity | `TestActorIDPopulatedForEveryEmit` (integration) — exercises `core.Actor` stamp at every site for each `ActorKind`, asserts `Actor.ID` parses as ULID; AND `TestEventbusActorIDIsULIDBytes` (unit) — for emitted `eventbusv1.Actor` values, asserts `len(id) == 16` and `ulid.Parse(...)` succeeds; AND `TestSystemSentinelsResolveViaNameByID` (unit) — `NameByID(SystemActorULID) == "system"` and `NameByID(WorldServiceActorULID) == "world-service"` after Manager bootstrap |
| INV-W9ML-2 | `IdentityRegistry` is the resolution path | `TestIDByNameAtStampSites` and `TestNameByIDAtRenderSites` (integration) — verify the call paths via mock registry with sentinel error |
| INV-W9ML-3 | Name uniqueness | `TestDuplicateNameLoadFails` (integration) — two active plugins with same name; second load fails with constraint violation |
| INV-W9ML-4 | Stable ULID across changes | `TestULIDStableAcrossManifestUpdate`, `TestULIDStableAcrossContentUpdate`, `TestULIDStableAcrossUnloadReloadInRetentionWindow` (integration) |
| INV-W9ML-5 | Lifecycle-coupled policies | Existing `internal/plugin/policy_installer_test.go` cases cover this; no new tests needed |
| INV-W9ML-6 | No prod-shape discipline | Documentary; verified by spec review, not runtime test |
| INV-W9ML-7 | Clean wire format | `TestNoLegacyIDReferencesInProductionCode` (CI script) — runs the grep commands from the invariant statement; expected to return zero matches in non-doc, non-generated files |
| INV-W9ML-8 | Sweep ordering | `TestSweepDoesNotGCPluginLoadedThisCycle` (integration) |
| INV-W9ML-9 | No deletion | `TestNoDeleteFromPluginsInCodebase` (CI script) — runs `git grep -n 'DELETE FROM plugins\b' -- '*.go'`; asserts zero matches outside any explicit test fixture |

### Test layers

**Unit tests:**

- `PluginRepo` against Postgres testcontainer: Upsert insert path, Upsert update-no-drift path, Upsert update-drift path, partial UNIQUE constraint behavior, `ListAll`, `SweepInactive`.
- `Manager` cache mutation logic: load updates both maps; load failure rolls back; unload updates `activeByName` only; sweep updates both with `gc_at` semantics.
- `EventEmitter` and `coreActorToEventbusActor`: success path (ULID-string parses cleanly); `ACTOR_ID_NOT_ULID` path (defense-in-depth — should never fire post-epic if stamp sites do their job).
- Stamp-site lookup at `goplugin/host.go` and `subscriber.go`: success path; `PLUGIN_UNREGISTERED_INVOKE` path with mocked `IDByName` returning false.
- Renderer: `<unknown plugin>` fallback path; active-name resolution; deactivated-name (historical) resolution.

**Integration tests:**

- End-to-end happy path (acceptance criterion 5).
- Plugin reload preserves ULID within retention window.
- Plugin GC + re-add mints fresh ULID; old events still resolve names via `NameByID`.
- Drift detection: load with new `manifest_hash` emits a `plugin.drift` log entry exactly once.
- Concurrent reads under load mutation (race detector enabled).
- AAD safety: encrypted character-actor events from before the cutover decrypt successfully (proto3 zero-value safety); plugin-actor encrypted events from before the cutover are gone (purged).

**Migration tests:**

- Migration 000018 applies cleanly to a fresh DB.
- Down migration drops the table.
- Bootstrap orphan check passes on clean install; fails (refuses-to-start) on a synthesized orphan row inserted with `actor_kind = 'plugin'`.
- Combined PG TRUNCATE + JS PurgeStream: both succeed atomically; on JS purge failure, PG transaction rolls back (or vice versa).

**Cross-cutting:**

- The "no LegacyID references" CI check (acceptance criterion 3 / INV-W9ML-7).
- The "no DELETE FROM plugins" CI check (INV-W9ML-9).
- Race-detector-clean for cache mutations (`go test -race`).
- Coverage gates per CLAUDE.md: >80% per package; >90% target for `internal/plugin/identity_registry.go`.

### Test fixture impact

Categories of test changes (each file enumerated):

**a. Production-test fixtures that hand-build plugin Actors with `LegacyID`** — replace `LegacyID: "core-scenes"` with `ID: <ulid-bytes>` via a new test helper:

```go
// internal/plugin/plugintest/registry.go (new):

// RegisterTestPlugin mints a deterministic ULID for the named plugin and
// inserts it into the registry's cache + plugins table. Returns the ULID
// for use in fixtures.
func RegisterTestPlugin(t *testing.T, mgr *plugin.Manager, name string) ulid.ULID
```

Affected files:

- `cmd/holomush/sub_grpc_adapters_test.go` (multiple cases at 154-236)
- `internal/eventbus/publisher_test.go` (LegacyID fallback at 284-299, 350)
- `internal/eventbus/actor_conversion_test.go` (any LegacyID branches)
- `internal/eventbus/history/actor_from_envelope_test.go:47-67` (replace with ULID test)
- `internal/eventbus/history/cold_postgres_test.go:23-59` (retarget Decision 5 lock to ULID round-trip)
- `internal/plugin/event_emitter_crypto_test.go:40, 67` (`ID: "test-plugin"` → `ID: <ulid-string>`)
- `internal/plugin/event_emitter_test.go:47, 146, 524, 554, 583` (multiple sites)
- `internal/plugin/manager_routing_test.go:192, 332` (`ID: manifest.Name` → `ID: <ulid-string>`)
- `internal/plugin/manager_test.go:1950` (similar)
- `test/integration/eventbus_e2e/cross_tier_query_test.go:488` (LegacyID arg removed)
- `test/integration/plugin/actor_authentication_test.go:164, 174` (ActorLegacyID field removed)
- `test/integration/crypto/inv49_envelope_roundtrip_test.go` (per acceptance criterion 7)
- `test/integration/crypto/e2e_test.go` (per acceptance criterion 7)

**b. Tests that exercise the legacy fallback specifically** — deleted; the behaviors they assert no longer exist. See acceptance criterion 9.

**c. Tests driving the real plugin load path** (e.g., `test/integration/plugin/abac_widget_test.go`) — already exercise the canonical path. Post-epic, the canonical path also Upserts the plugin row; these tests need the `plugins` table to exist (covered by the testcontainer setup that runs migrations).

## PR Scope

The epic ships in **one PR**. The deliverable is logically atomic: proto field removal forces emit and read paths to migrate together; the upstream stamp sites and the bus conversion functions are mutually dependent; the `plugins` table and its consumers are mutually dependent. Splitting the work would introduce transient compat shims between PRs that have no protective value (no users depend on intermediate states).

**LOC estimate** (from grep + new-file projection):

- Production Go diff: ~600-900 LOC (touch + delete across 18 files; new `identity_registry.go` ~200 LOC; new `plugin_repo.go` ~250 LOC; `manager.go` integration ~100 LOC; stamp-site updates ~50 LOC; bus-conversion simplifications ~50 LOC).
- Test diff: ~500-800 LOC (helper ~100 LOC; ~12 test files updated with ~30-60 LOC each; new integration tests ~300 LOC).
- Migration files: ~50 LOC (up + down).
- Proto + generated: ~10 LOC source change; regen of `*.pb.go` is mechanical.

**Total: ~1200-1750 LOC.** Reviewable as one PR for a single-developer codebase.

Implementation order *within* the PR is meaningful for incremental local development but not a deliverable boundary. Suggested order: schema migration → repo → cache → stamp-site updates → wire-format removal → bus-conversion simplification → renderer integration → test fixture migration → INV-49 retarget → ojw1.8 + u5bb closure.

Each step's completion is gated by `task pr-prep` producing green output. The PR runs `task pr-prep` once on its final state before merge.

## Out of Scope

The following are deferred and remain out of scope for `holomush-w9ml`:

- **First-class plugin rename support.** Renaming a plugin is *defined* under this design (deactivation of the old name via TTL, re-add of the new name with a fresh ULID, with the GC-window gap during which both names resolve). What is out of scope is *first-class* rename support with a `previous_names` JSONB column or `plugin_aliases` table that explicitly links old and new names. A future epic adds this if needed.
- **Per-event hash stamping for tamper forensics.** Audit events do not record which plugin revision (manifest_hash, content_hash) emitted them. The question "what was the plugin's behavior when event X was emitted?" is not answerable from audit alone post-epic. A future plugin-forensics epic would add either a hash stamp on each event or a `plugin_revisions` append-only join table.
- **Hash-based plugin trust enforcement.** `content_hash` drift is logged, not enforced. A future plugin-trust epic builds on this signal to add operator-approved-set checks at load time.
- **Reworking `core.Actor` structure.** The fields of `core.Actor` (`Kind`, `ID`) are unchanged; only the *invariant* on `Actor.ID` for plugin actors changes (now a ULID-string).
- **Reworking other `eventbusv1.Actor` proto fields.** Only `legacy_id` is removed; `kind` and `id` are unchanged.
- **The Phase 4-7 crypto epic line.** `legacy_id` elimination is orthogonal to lifecycle ops, Rekey, Vault provider, and plugin-owned audit.

## References

- Phase 3d Decision 6: [`2026-05-03-event-payload-crypto-phase3d-grounding.md`](2026-05-03-event-payload-crypto-phase3d-grounding.md) §"Decision 6 — `legacy_id` is tech debt; eliminate in a separate epic"
- Master crypto spec: [`2026-04-25-event-payload-crypto-design.md`](2026-04-25-event-payload-crypto-design.md)
- Closed by supersession (at execution): `holomush-ojw1.8` "propagate Actor.LegacyID through plugin actor resolver"; `holomush-u5bb` "persist Actor.LegacyID in events_audit"
- Related (parallel concern): `holomush-s6wp` "Centralize invariant capture, storage, and cross-reference across specs"
- Design-reviewer Revision 1 report: `.claude/agent-memory/design-reviewer/reports/2026-05-04-1325-legacy-id-elimination-design.md`
- Project conventions: [`CLAUDE.md`](../../../CLAUDE.md) — RFC2119 keywords, Plugin Runtime Symmetry, Test Names Should Be Sentences
