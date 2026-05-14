# HoloMUSH Architecture Review

- **Date:** 2026-05-13
- **Reviewer model:** Claude Opus 4.7 (1M context)
- **Scope:** system-architecture audit (read-only) of the Go core, gateway, eventbus, plugin host, ABAC engine, and admin layer
- **Follow-up tracking:** `holomush-dj95` — epic for triaging findings into child task beads (`bd show holomush-dj95`)

## Executive Summary

HoloMUSH has a clear conceptual spine — protocol gateway, JetStream event spine, plugin runtime, ABAC engine, world service — and the most-touched boundaries (`gateway-boundary`, `plugin-runtime-symmetry`) are policed by rules files and reviewer agents. The architectural problems are not where the spine is, but where the project has stopped at a "phase cutover" without finishing the cleanup.

The five strongest findings, ranked by structural impact:

1. **Two parallel history dispatchers** in `internal/eventbus/history/` (`decodeAuthorizeAndDispatch` vs `dispatcher.DispatchFor`) — the new INV-39 resolver-aware path supersedes the old, but the old function is still reachable from both hot and cold tiers, creating a permanent fork in the security-critical decrypt flow.
2. **`CoreServer` is a 24-field god object** (`internal/grpc/server.go:122`) that has accreted every cross-cutting concern (auth, ABAC, sessions, focus, identity, crypto-binding, subscriber, history, world querier, command dispatch). It is the dominant integration knot for the core process.
3. **Dual event-type universe**: `core.Event` (with legacy colon-`Stream`) and `eventbus.Event` (with dot-`Subject`) coexist, bridged ad-hoc by `subjectxlate.Legacy` and per-call kind/actor mappers. The core engine still writes via `core.EventAppender`, not the bus.
4. **Bootstrap god-knot in `cmd/holomush/core.go`** — a 1336-line `runCoreWithDeps` reaches into 35+ internal packages including `internal/eventbus/crypto/dek`, `internal/eventbus/crypto/invalidation`, `internal/eventbus/audit/chain`, and orchestrates the full subsystem DAG by hand.
5. **Stale invariant documentation**: `.claude/rules/event-interfaces.md` declares `Subscriber.OpenSession(ctx, sessionID, filters)` but the live interface at `internal/eventbus/bus.go:30` takes an extra `identity SessionIdentity`. Reviewer agents and sub-agents will read the rules and reason from a stale contract.

These are debt patterns of a project that has shipped phase-cutover after phase-cutover (F1–F7, Phase 3a–3d, sub-epics A–F) and not yet retired the prior phase's scaffolding.

## 1. Component Boundaries

### 1.1 EventBus has the right shape; the engine has not caught up

The three-role split — `Publisher` / `Subscriber` / `HistoryReader` (`internal/eventbus/bus.go:23-33`) — is the cleanest interface surface in the codebase. Bus, history, codec, crypto, audit are properly separated subpackages.

The problem sits one layer up. `core.Engine` (`internal/core/engine.go:46-62`) still holds an `EventAppender` interface that wraps a single `Append(ctx, Event) error`. `core.Event` (`internal/core/event.go:222-229`) still carries `Stream string` with colon-delimited values like `"location:01ABC"`. The bus event (`internal/eventbus/types.go:133`) uses `Subject Subject` with dot-delimited NATS form. These are bridged by `subjectxlate.Legacy(subjectRaw, gameID)` at `internal/plugin/event_emitter.go:207`, and `coreActorToEventbusActor()` at `:313`.

Per `.claude/rules/event-interfaces.md`, "the old `EventStore` interface is deleted." It is, but its successor `EventAppender` is a one-method shim that exists solely to let `core.Engine` keep emitting `core.Event{Stream: "location:..."}` without knowing the bus. The actual engine emit sites (`HandleSay`, `:84-95`) hard-code the `core-communication:say` event type and a legacy `Stream: "location:" + char.LocationID.String()` subject. The full `core.Event` ⇄ `eventbus.Event` translation is performed inside a `MemoryEventStore` for tests and an undisclosed production adapter — the architectural debt is the type duality itself.

**Direction:** retire `core.Event` and `core.EventAppender`. Move the host engine's emits to `eventbus.Publisher` directly, deleting `subjectxlate` once plugins also emit JetStream-native subjects (already telegraphed in the emitter comment at `event_emitter.go:42-46`).

### 1.2 `CoreServer` is the project's largest accreted god object

`internal/grpc/server.go:122-183` declares `CoreServer` with 24 fields covering: command engine, session store, event appender, world querier, ABAC engine, four auth providers (player, character, session, guest), three repository handles, a transactor and binding repo, plugin stream contributor, stream registry, focus coordinator, plugin identity registry, subscriber, history reader, and a configurable session-ID generator. `NewCoreServer` (`:279-297`) takes four positional args plus a variadic of 20+ `CoreServerOption`s.

This struct is the integration knot where the JetStream cutover landed: every new cross-cutting concern (rendering, focus, ABAC stream-read auth, crypto-binding, plugin identity) has been added as one more field + one more option rather than as a smaller service composed in. Every test in `internal/grpc/*_test.go` (11620 lines total) has to fake or partially populate this struct.

**Direction:** split along functional lines — an "authn/authz wing" (auth providers, ABAC engine, binding repos), a "subscribe/history wing" (subscriber, historyReader, gameID provider, identityRegistry), and a "command-pipeline wing" (engine, dispatcher, services, world querier). The session-management surface (Subscribe, QueryStreamHistory, HandleCommand, EndSession) wants to be three independent handlers.

### 1.3 Plugin manager has outgrown its package

`internal/plugin/manager.go` is 1557 lines with 50+ exported methods on `Manager`. Responsibilities include: discovery, host registry, DAG load ordering, identity registry (ULID ↔ name), retention sweeping, capability discovery via reflection helpers, audit subject declaration tracking, plugin attribute provider registration/unregistration with rollback, trust allowlist tracking, plugin command registration with the command registry, and session-stream contribution (`QuerySessionStreams`).

`trustAllowlist` is duplicated (`manager.go:81` and `policy_installer.go:40`). The manager only uses its copy to log warnings (`warnUnknownTrustAllowlistEntries`, `:497-525`); the policy installer holds the second copy and is the one that actually consults it during compilation (`policy_installer.go:122`).

**Direction:** split `Manager` along the orthogonal axes — `Loader` (discover/DAG/load/unload/rollback), `Registry` (identity, audit subjects, attribute providers, command registration), `Lifecycle` (retention, host registration). Hold `trustAllowlist` in exactly one place (the installer) and pass it down.

### 1.4 `internal/admin/policy` is straddling two domains

`internal/admin/policy/` (2111 LOC) implements policy-chain verification, chain reduction, emitter, and a Bootstrap-level `Subsystem`. The lifecycle ID it claims (`SubsystemCryptoPolicy`, `internal/lifecycle/subsystem.go:34`) is in the crypto family, but the package lives under `admin`. The package depends on `auditchain` and is the verifier subsystem for policy-set chains. Either the policy-chain verifier belongs under `internal/eventbus/crypto/` (it is fundamentally a chain check), or under `internal/auditchain`. Its placement under `admin` makes it look like an admin-CLI feature.

**Direction:** move `internal/admin/policy/` to `internal/auditchain/policy/` or `internal/eventbus/crypto/policy/`. Leave only the admin-CLI thin client under `internal/admin/`.

## 2. Cross-Cutting Concerns

### 2.1 ID generation: one prod-code drift case, otherwise disciplined

The repo enforces the `core.NewULID` (monotonic, for event IDs and session IDs) vs `idgen.New` (entropy-fresh, for entity primary keys) split per `CLAUDE.md` and the `no_legacy_id_grep_test`/`proto_legacy_id_grep_test` regression tests in `internal/eventbus/`. Production usage of `ulid.Make()` outside `internal/testsupport/` is zero. One production drift:

- `internal/store/postgres.go:108` — `gameID = core.NewULID().String()` is used as an entity primary key in `InitGameID`. Per the rule it should be `idgen.New()`; the gameID is identity, not ordering. The stale comment `internal/cluster/registry.go:71` ("production uses `ulid.Make()`") references a code path that already uses `idgen.New` (`registry.go:97`).

### 2.2 Error handling: `oops` is consistent

`oops` usage is uniform: `oops.Code(...).With(...).Wrap(err)` at every error site checked (`internal/eventbus/history/dispatcher.go`, `internal/plugin/event_emitter.go`, `internal/access/grants.go`). Error codes are constants, asserted in tests via `errutil.AssertErrorCode`.

### 2.3 Heavy use of `panic` at API boundaries

45+ non-test, non-mock files contain `panic(...)`. Reviewed concentrations:

- `internal/access/prefix.go:62-178` — every resource/subject constructor panics on empty input. Justified rationale (empty prefix would silently bypass ABAC), but ten panic sites that callers must remember about is brittle. A typed wrapper (`type CharacterID string` with a `Subject()` method returning `(string, error)`) would push the check to construction.
- `internal/grpc/server.go:293` — `NewCoreServer` panics if `dispatcher` or `cmdServices` is nil.
- `internal/core/engine.go:58-75` — `NewEngine` panics on nil `EventAppender` with a reflection-based typed-nil check.
- `internal/world/service.go:60` — `NewService` panics on nil engine.

The pattern is "fail fast at construction" rather than at first use, which is defensible, but the volume of panic sites is a structural smell. Consider promoting all of them through a single `mustNotNil(name, value)` helper in `pkg/errutil` so audit and lint can target them uniformly.

### 2.4 Context propagation

Spot-checked — every public method takes `ctx context.Context` first. `internal/eventbus/audit/projection.go:55` documents the rationale for storing a long-lived `workerCtx` field (`//nolint:containedctx // lifecycle ctx, not request ctx`), which is the project's pattern for subsystem-owned cancellation scope. Good shape.

## 3. Plugin Runtime Symmetry

The `plugin-runtime-symmetry` rule states: "Binary and Lua plugins MUST be treated identically by the host." The emit gate at `internal/plugin/event_emitter.go::Emit` (`:112`) is correctly the single shared codepath; the `actor_kinds_claimable` check (`:150-157`) and the Phase 3a sensitivity fence (`:169-180`) both fire here for both runtimes. That is the invariant the rule cares about most and it holds.

However, the **manifest-validation surface** has a meaningful asymmetry. `internal/plugin/manifest.go` rejects Lua plugins from declaring:

- `audit` blocks (`:511-514`) — requires gRPC PluginAuditService
- `provides` services (`:523-527`)
- `storage: postgres` (`:530-534`)
- `resource_types` (`:543-545`)

The justification each time is "binary plugins implement a gRPC server, Lua plugins do not." This is reasonable runtime-specific gating, but it is a real capability gradient. A Lua plugin cannot own its own audit table, register ABAC resource types, or expose a service to other plugins. If the symmetry invariant is meant to apply only to *policy/trust/manifest-gate* dimensions (per the rule's clarification), this is in-bounds — but the rule's example (`actor_kinds_claimable`) does not make clear how broad "policy/trust" is. The asymmetry should be documented explicitly in the rule file rather than implied by absence.

**Direction:** either widen Lua to own these capabilities (by routing through a host-provided shim) or update `.claude/rules/plugin-runtime-symmetry.md` with a "permitted runtime-specific asymmetries" table so reviewers know what to flag and what to accept.

## 4. Gateway Boundary

The gateway-boundary invariant — `cmd/holomush/gateway.go` + `internal/web/` are protocol translation only — is largely upheld.

- `cmd/holomush/gateway.go` imports only `holoGRPC` (client), `web`, `telnet`, `control`, `tls`, `xdg`, `observability`, `telemetry`, `config`, `logging`. No `internal/world`, `internal/store`, `internal/session`, `internal/core`, `internal/access` imports.
- `internal/web/handler.go` imports `internal/core` exactly once — at `:18` — and uses it solely for `core.NewULID()` at `:156` to mint a connection ID. The ULID is identity, generated locally, not a query into the core domain. Acceptable per the invariant ("connection management is allowed").

The `cmd/holomush` *core* command, however, has the opposite shape: it reaches deep into internals. `cmd/holomush/core.go` imports 35 holomush packages, including:

- `internal/eventbus/audit` and `internal/eventbus/audit/chain` (lines 41-42)
- `internal/eventbus/crypto/dek` and `internal/eventbus/crypto/invalidation` (lines 43-44)

`runCoreWithDeps` is 1336 lines. It instantiates the DB subsystem, runs the bootstrap orphan check, mints TLS, constructs every subsystem and wires their `DependsOn` graph by hand, threads the `dek.Manager` into the gRPC subsystem, constructs the audit-chain verifier, lifts ctx/cancel above the admin subsystem. The orchestration logic that the `lifecycle` package was designed to own (`internal/lifecycle/orchestrator.go`) is partially bypassed — `runCoreWithDeps` does explicit ordering ("Lifted ahead of admin subsystem construction (holomush-jxo8.9)", `core.go:911`) rather than declaring `DependsOn` and letting the orchestrator schedule.

**Direction:** every subsystem already declares `lifecycle.SubsystemID` and `DependsOn()`. Push the manual ordering in `core.go` down into those declarations; let the orchestrator handle the topological start. The bootstrap-orphan check, TLS cert ensure, and DB-gameID dance are the legitimate manual steps; the rest is what `lifecycle.Orchestrator` exists for.

## 5. Async / Event Flow

### 5.1 Two parallel history dispatchers

`internal/eventbus/history/dispatcher.go` exports both:

- `decodeAuthorizeAndDispatch` (`:252-378`) — the legacy header-free dispatcher, called inline with a `dekMgr.Resolve(ctx, keyID, keyVersion)`.
- `dispatcher.DispatchFor` (`:74-235`) — the new resolver-aware path that supports the INV-39 hot→cold-tier fallback.

Both functions are reachable. `hot_jetstream.go:548` and `cold_postgres.go:447` still call `decodeAuthorizeAndDispatch` as their fallback when no `SourceResolver` is configured (`:441-447`):

```go
if d != nil {
    return d.DispatchFor(...)
}
return decodeAuthorizeAndDispatch(...)
```

That branch is the cold-tier-production case described in the inline comment: "production wiring currently leaves authGuard nil (pre-Phase-3b passthrough; see Step 5.0a in the Phase 3d grounding doc and the holomush-ojw1 follow-up bead 'plumb cold-tier auth options through history.NewReader')" (`dispatcher.go:274-279`). In other words: the new dispatcher is the intended path, the old one is the safety net for an incomplete cold-tier auth wiring.

The danger is that the security logic — AuthGuard denial, audit-emit-on-decrypt, plaintext zeroization on `AUDIT_QUEUE_FULL` — exists *twice*, in subtly different shapes. INV-19 (plugin decrypt audit) is enforced in both; INV-39 (hot→cold fallback) is enforced only in the new one. A behavioral drift between the two is invisible from outside the dispatcher package.

**Direction:** complete the holomush-ojw1 follow-up — plumb cold-tier auth options through `history.NewReader` so every cold-tier read goes through `DispatchFor`, then delete `decodeAuthorizeAndDispatch`. Until that lands, add a contract test that fuzzes inputs through both functions and asserts identical (event, metadataOnly, error) outputs for the overlapping invariants.

### 5.2 Audit projection lives in `eventbus/audit` but imports plugin proto

`internal/eventbus/audit/plugin_router.go:18-19` imports both `eventbusv1` and `pluginv1` proto, and at `:50` implements `history.PluginHistoryRouter` (a `history`-package interface) inside the `audit` package. The comment at `:40-42` says "Keeping the concrete type in this package keeps the audit-domain imports contained; the history package depends on audit for OwnerMap anyway." That's a defensible choice (audit owns the plugin client provider), but it creates a four-way coupling: `audit → eventbus + history + pluginv1`. The `history` package's `PluginHistoryRouter` interface is satisfied structurally by a type in `audit`. Cross-package interface satisfaction is fine — Go-idiomatic — but the comment acknowledges this is a workaround for an existing dependency, not a clean design.

**Direction:** move `PluginHistoryRouter` interface into `internal/eventbus/types` (or a shared `internal/eventbus/router` package), then both `history` and `audit` depend on the contract, not on each other.

### 5.3 Back-pressure points

The audit projection (`internal/eventbus/audit/projection.go`) caps redeliveries via `MaxDeliver`; the TODO at `:74-77` acknowledges that exhausted messages drop on the floor without a DLQ. The `authguard/audit/emitter.go:216,246` carry two `TODO(metrics):` markers for drain-failed and marshal-failed counters. Both are visibility gaps in a security-critical path (decrypt audit) — silent failure of the audit emit is currently bounded by `INV-19` failing the read closed, but the operator-side visibility on *why* (queue full vs marshal failure vs DB error) is not wired.

## 6. Structural Debt and Unfinished Epics

### 6.1 Naming drift in `internal/store`

`internal/store/postgres.go:33-35` declares:

```go
// PostgresEventStore provides system-info and game-ID persistence backed by PostgreSQL.
type PostgresEventStore struct {
```

The type stores no events (post-F7 the `events` table is gone). It is a `SystemInfo` + `GameID` store. The name is a fossil. `internal/core/store_memory.go:21` `MemoryEventStore` is also a fossil — its `Replay`/`ReplayTail`/`LastEventID` methods are flagged "Test-inspection helper; not part of any production interface" (`:46,82,98`). It implements only `EventAppender`, which is the one-method shim noted in §1.1.

**Direction:** rename `PostgresEventStore` → `SystemInfoStore` (or fold into `internal/bootstrap`). Delete `MemoryEventStore` once `core.Engine` migrates to `eventbus.Publisher`.

### 6.2 Stale invariant rule file

`.claude/rules/event-interfaces.md` lines 11-32 declares the `Subscriber` interface as `OpenSession(ctx, sessionID, filters)`. The actual interface at `internal/eventbus/bus.go:30` is:

```go
OpenSession(ctx context.Context, sessionID string, identity SessionIdentity, filters []Subject) (SessionStream, error)
```

The `identity` parameter was added (per `internal/eventbus/authguard/adapter_identity.go:9` comment) for the AuthGuard wiring. Reviewer agents and sub-agents load this rule file via the `paths:` frontmatter — they will reason from the stale signature.

**Direction:** regenerate the rule file from `bus.go` directly (or assert it in a `doc_consistency_test.go`).

### 6.3 In-flight phase comments

Production code carries explicit phase markers that signal unfinished work:

- `internal/plugin/event_emitter.go:38-46` "Subject compatibility (F1 transitional)" — F5 was supposed to retire `subjectxlate`; it's still there.
- `internal/eventbus/audit/subsystem.go:59` "TODO (Phase B): wire a DLQ"
- `internal/eventbus/history/cold_postgres.go:355` "TODO(F5) stub to be fleshed out when plugin-owned audit schemas..."
- `internal/grpc/auth_handlers.go:244` "TODO: Thread connID through SelectCharacterResponse proto once the field is added."

Each is small, but cumulatively they paint the same picture: the phase that introduced JetStream (F1-F7) shipped, but the cleanup phases (F5 subject migration, F-DLQ, plugin-owned audit) did not.

### 6.4 `WithCryptoEnabled` gate is a phased-rollout fossil

`internal/plugin/event_emitter.go:82-84,170` introduces `cryptoEnabled` as a runtime flag that bypasses the manifest sensitivity fence. The comment at `:67-81` documents this clearly: without `cryptoEnabled`, manifest sensitivity declarations are *ignored* and every event is stamped `Sensitive=false`. The justification is that production manifests already declare `sensitivity: always` for events whose plugin SDK doesn't carry `EmitIntent.Sensitive` over the wire. This means there are two behaviors of the emit codepath in the same binary, gated by config. Once the SDK lands the `Sensitive` field, this gate becomes a footgun (someone forgetting to set it to `true` in a new game config silently disables a security control).

**Direction:** finish the SDK plumbing, then delete the gate.

## 7. Inter-Module Dependency Health

Spot-checked: no `go list -deps` cycles (Go enforces this), and the package graph is reasonably shaped (`internal/eventbus` does not import `internal/grpc`; `internal/access` does not import `internal/plugin`). Specific health concerns:

- **`internal/eventbus/audit` depends on `pluginv1` proto** (§5.2). The audit subpackage is the only eventbus subpackage that knows about plugins. Justified by `OwnerMap` + `PluginHistoryRouter` living together, but worth surfacing as a deliberate exception.
- **`internal/plugin/manager.go` imports `internal/grpc/focus`** (line 26). The plugin manager passes `focus.Coordinator` to `ConfigureFocusDeps`. The focus coordinator lives under `internal/grpc/` but is consumed by `plugin`. Either it isn't a gRPC-layer concern (lift it to `internal/focus/`), or the plugin manager shouldn't hold it directly.
- **`cmd/holomush` imports 35 internal packages from `core.go` alone** (§4). The bootstrap surface is the project's largest single fan-in.
- **`internal/grpc/server.go` imports `internal/plugin`** (`plugins.IdentityRegistry`, `plugins.SessionStreamsRequest`). Acceptable, but reinforces the `CoreServer` god-object pattern: every cross-cutting plugin concern shows up as a field on the gRPC server.

## 8. Prioritized Architectural Cleanups

### P0 — security/correctness

1. **Eliminate dual history dispatchers.** Complete `history.NewReader` cold-tier auth wiring (`internal/eventbus/history/dispatcher.go:274`, follow-up bead `holomush-ojw1`), delete `decodeAuthorizeAndDispatch`. Until then, add a contract test that asserts identical outputs for the overlapping invariants.
2. **Resolve `WithCryptoEnabled` fossil.** Plumb `EmitIntent.Sensitive` through the Lua and binary SDKs; remove the gate at `internal/plugin/event_emitter.go:82`.
3. **Update `.claude/rules/event-interfaces.md`** to match the live `Subscriber.OpenSession` signature; add a doc-consistency test that parses the rule against `bus.go`.

### P1 — structural

4. **Decompose `CoreServer`** (`internal/grpc/server.go:122`) into three handlers along the auth / subscribe-history / command-pipeline axes. Drives down the 11 620-line test surface that currently has to construct a fully populated server.
5. **Retire `core.Event` + `core.EventAppender`.** Migrate `core.Engine` to publish via `eventbus.Publisher` directly. Delete `subjectxlate.Legacy` once plugins emit JetStream-native subjects. Delete `MemoryEventStore`.
6. **Split `internal/plugin/manager.go`** into `Loader` / `Registry` / `Lifecycle`. Eliminate duplicated `trustAllowlist` in favor of a single home in `PolicyInstaller`.
7. **Move bootstrap orchestration** out of `cmd/holomush/core.go` and into `internal/lifecycle/orchestrator.go`. Subsystems already declare `DependsOn`; let the orchestrator schedule.

### P2 — hygiene

8. **Rename `PostgresEventStore` → `SystemInfoStore`** (or merge into `internal/bootstrap`). Update `InitGameID` to use `idgen.New()` per the ID-generation rule.
9. **Reclassify `internal/admin/policy/`** under `internal/auditchain/` or `internal/eventbus/crypto/policy/`; leave only the admin-CLI thin client under `internal/admin/`.
10. **Document Lua/binary asymmetries** in `.claude/rules/plugin-runtime-symmetry.md`: a table of "permitted runtime-specific asymmetries" (audit blocks, provides, postgres storage, resource_types) so reviewers know what to flag and what to accept.
11. **Land the deferred TODOs** in audit DLQ (`internal/eventbus/audit/subsystem.go:59`) and AuthGuard audit metrics (`internal/eventbus/authguard/audit/emitter.go:216,246`) — they are visibility gaps in security-critical paths.
12. **Lift `focus.Coordinator`** out of `internal/grpc/focus/` into `internal/focus/` if it is consumed by the plugin manager (`internal/plugin/manager.go:26`); otherwise hide the import behind an interface defined in `internal/plugin`.

The repo's spine is sound. The work is finishing what was started.
