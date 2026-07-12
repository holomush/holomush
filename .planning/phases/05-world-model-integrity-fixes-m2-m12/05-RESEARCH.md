<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->

# Phase 5: World-Model Integrity Fixes (M2 / M12) - Research

**Researched:** 2026-07-12
**Domain:** World-model persistence (Go / PostgreSQL / pgx) — optimistic-concurrency version guard + transactional outbox / ordered atomic feed
**Confidence:** HIGH (mechanism is NORMATIVE and pre-decided; all code sites verified against `main` at `internal/world/**`)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **The mechanism is NORMATIVE — do NOT re-open.** Option B (CRUD-canonical + optimistic-concurrency version guard + transactional outbox / ordered atomic feed), panel-ratified. ADR `holomush-i4784` (Accepted) + `model-01-consensus-onepager.md` are authoritative; where they differ in detail the **one-pager wins**.
- **Version guard:** `version INTEGER NOT NULL DEFAULT 1` on all four world tables (locations, exits, characters, objects); version-predicated CAS on **writes AND deletes** (`… WHERE id=$1 AND version=$2`); any zero-row result classified by a **locked follow-up read in the same transaction** (conflict vs concurrent-delete vs not-found); `Version` field added to the four Go structs.
- **Atomic feed:** exactly **one semantic envelope per successful externally-visible command**, committed **in the same transaction** as the state change; intent-level new-values-only payloads; one tombstone per aggregate on delete; failed/no-op commands emit nothing.
- **Ordering:** commit-ordered, gap-free `feed_position` from a **locked per-game counter** (NOT `BIGSERIAL`, NOT insert-time). A **single leased relay** publishes strictly in position order, `Nats-Msg-Id` = event ULID for dedup, `LISTEN/NOTIFY` wakeup + periodic sweep, **halt-and-alert** poison posture.
- **Enforcement:** compile-time write-requires-envelope seam (`mutate(ctx, entity, expectedVersion, envelope)` — envelope-less write is a **type error**) + census meta-test + lint fence forbidding raw world-table SQL outside `internal/world/postgres`.
- **Conflicts:** surfaced strictly as typed `WORLD_CONCURRENT_EDIT`; **no automatic retry in first slice** (compare-before-retry deferred, telemetry-gated).
- **Invariants:** register AND bind `INV-WORLD-ATOMIC-FEED`, `INV-WORLD-DELTA-PARITY`, `INV-WORLD-FEED-ORDER`, `INV-WORLD-WRITER-BOUNDARY` in **this** phase.
- **Slice order is FIXED** (see Architecture Patterns → Slice map). Delete the post-commit emit path (`EmitMoveEvent`, `EVENT_EMITTER_MISSING`) outright.
- **D-01:** full mechanical emission rollout in Phase 5 — every world write command (~15) + taxonomy schema registry + census meta-test. No allow-list of pending commands.
- **D-02:** Phase 5 stops at the typed error at the `world.Service` boundary; telnet/web UX mapping is a separate deferred slice. The typed error + its code registration ARE in scope.
- **D-03:** fold WR-01 into slice 2 — rewrite `test/integration/resilience/m2_dualwrite_test.go` to assert new outbox behavior; correct the M2 "Mechanism" paragraph in `f1-resilience-verdict.md`.
- **D-04:** one phase PR; crypto/abac gates apply to the whole diff (planner must confirm neither triggers).

### Claude's Discretion
- Internal wave decomposition within each slice; migration numbering; exact Go package placement of the outbox relay + mutation wrapper; the reference-consumer shape.

### Deferred Ideas (OUT OF SCOPE)
- Conflict-surfacing UX slice (telnet message + web retry affordance mapping table).
- Compare-before-retry conflict semantics.
- Product feed consumers/projections (Phase 5 ships only the *reference* consumer).
- ARCH-04 unified event-model collapse (Phase 7 — consumes Phase 5's taxonomy registry).
- Real event sourcing / world-state rebuild (permanently forgone).
- OPS-02 retention bounding of the outbox (Phase 6 — Phase 5 lands outbox + prune-after-PubAck only).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| MODEL-02 | Correct the ~6 doc sites stating the false "event sourcing / state derives from replay" principle | Exact site inventory in **MODEL-02 Doc Sites** below (grep-verified with `path:line`). |
| MODEL-03 | World writes carry an optimistic-concurrency version guard; verified under two-replica deployment | Version-column migration precedent (`access_policies`), repo CAS shape, RMW threading sites, and resilience harness all mapped below. |
| MODEL-04 | Eliminate the dual-write window per MODEL-01 mechanism (transactional outbox) | Outbox/relay shape, `Transactor.InTransaction` seam, `execerFromCtx` tx-enrollment gap, and DLQ/poison precedent all mapped below. |
</phase_requirements>

## Summary

Phase 5 is a **mechanism-locked refactor** of `internal/world`, not a design problem. The world model is direct-write CRUD in Postgres with **no version column** (`internal/store/migrations/000001_baseline.up.sql`) and **no outbox**; the notification leg is dead code (`internal/world/setup/subsystem.go:66-75` constructs `world.Service` with no `EventEmitter`). The two proven findings (M12 last-write-wins, M2 dual-write non-atomicity) both flow from that shape. The ADR/one-pager prescribe the exact fix; this research maps every code site the plan must touch and every reusable precedent.

Three load-bearing structural facts drive the plan:

1. **The version guard is a repo + struct + RMW-threading change.** The four `world.Location/Exit/Character/Object` structs carry **no `Version` field today** (verified). The repo `Update`/`Delete` SQL has **no version predicate** (`location_repo.go:72-85`). The read-modify-write callers (`internal/property/entity_mutator.go` `SetName`/`SetDescription`, `internal/world/service.go:656` `UpdateCharacterDescription`) must thread the read version into the guarded write.
2. **The outbox demands transaction enrollment the repos don't do yet.** `Transactor.InTransaction` stores a `pgx.Tx` in context (`transactor.go:27-42`); only `Delete` reads it back via `execerFromCtx` (`helpers.go:42`). Every `Update`/`Create`/`Move` currently uses `r.pool.Exec` **directly** (`location_repo.go:72`), so it will NOT enroll in the outbox transaction unless switched to `execerFromCtx`. This is the single most under-appreciated change in the diff.
3. **`WORLD` is a brand-new invariant scope.** No `INV-WORLD-*` exists in `docs/architecture/invariants.yaml` and `WORLD` is not among the migrated boundaries (CRYPTO/SCENE/PLUGIN/EVENTBUS/CLUSTER/ACCESS/SESSION/STORE/TELEMETRY/PRIVACY/PRESENCE/COMMAND). The four invariants are greenfield registrations that must each be *bound* by a `// Verifies:` test in this phase.

**Primary recommendation:** Structure the plan as the four fixed slices; place the mutation wrapper + envelope type in `internal/world` (the seam that makes envelope-less writes a compile error must live where the write methods live), and the outbox relay in a new `internal/world/outbox` package that mirrors the DLQ/halt-and-alert posture of `internal/eventbus/audit/projection.go`. No new external dependency is required.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Version-guarded CAS write/delete | Database / `internal/world/postgres` | — | The predicate lives in SQL; the repo owns the zero-row classification via the locked follow-up read. |
| Version threading (read→guarded write) | API/service (`internal/world` + `internal/property`) | — | Read-modify-write callers must carry `expectedVersion`; the struct field is the transport. |
| Envelope construction + cardinality | API/service (`internal/world` mutation wrapper) | — | One envelope per externally-visible command; the wrapper is the compile-time chokepoint. |
| Outbox row write (same tx as state) | Database / `internal/world/postgres` | `Transactor` | Must share the `pgx.Tx` in context — the atomicity guarantee. |
| `feed_position` allocation | Database (locked per-game counter) | — | Commit-order proof depends on `SELECT … FOR UPDATE` on a counter row, not insert-time seq. |
| Relay publish (position order, dedup, halt-alert) | Backend worker (`internal/world/outbox` relay) | JetStream / `lifecycle.Subsystem` | Single leased publisher; `Nats-Msg-Id` dedup; poison → halt-and-alert. |
| Conflict surfacing (`WORLD_CONCURRENT_EDIT`) | API/service boundary | — | D-02: stops at `world.Service`; UX mapping deferred. |
| ABAC authorization | `internal/world` `checkAccess` (unchanged, pre-write) | `internal/access` | Relay publishes already-authorized, already-committed facts — never re-checks. |

## Standard Stack

No new external packages. Phase 5 is built entirely on existing internal packages + already-vendored libraries.

### Core (existing, reused)
| Package / symbol | Path | Purpose in Phase 5 |
|---|---|---|
| `world.Service` | `internal/world/service.go:45` | Owns the ~15 write commands; gains the mutation-wrapper chokepoint. |
| `Transactor.InTransaction` | `internal/world/postgres/transactor.go:27` | Same-tx seam for state-change + outbox write (already used by `DeleteLocation`). |
| `execerFromCtx` / `txKey{}` | `internal/world/postgres/helpers.go:42` | Pulls active `pgx.Tx` from context; repos MUST use it so writes enroll in the outbox tx. |
| `*pgxpool.Pool` (pgx/v5) | `internal/world/postgres/*` | DB access; `SELECT … FOR UPDATE` for the locked counter. |
| `core.NewEvent()` / `core.NewULID()` | `internal/core` | Envelope `event_id` (ULID identity/dedup, = `Nats-Msg-Id`). Never hand-mint IDs (INV per `.claude/rules/event-conventions.md`). |
| `internal/eventbus/audit/projection.go` DLQ posture | `:61,121,131,255-268` | Poison / halt-and-alert / bounded DLQ pattern the relay mirrors. |
| `lifecycle.Subsystem` + `SubsystemID` | `internal/lifecycle` | The relay registers as a subsystem (mirror `WorldSubsystem`, `subsystem.go:51-56`). |
| `eventbus.Qualify` | `internal/eventbus` | Prepends `events.<game_id>.` — the relay's publish subject boundary. |

### To DELETE (not extend)
| Symbol | Path | Disposition |
|---|---|---|
| `EmitMoveEvent`, `EmitObjectCreateEvent`, `EmitExamineEvent`, `EmitObjectGiveEvent`, `emitWithRetry`, `EventEmitter` iface | `internal/world/events.go` (whole file, 285 lines) | Post-commit emit path — **deleted outright** (one-pager Core mechanics §2). `EVENT_EMITTER_MISSING` / `EVENT_EMIT_FAILED` disappear with it. |
| `s.eventEmitter` field + `EmitMoveEvent` call sites | `internal/world/service.go:53,587,841,904,962,1017` | Replaced by outbox-envelope emission inside the mutation wrapper. |
| `EventStoreAdapter` production wiring | `internal/world/event_store_adapter.go` | Verify callers; ADR notes `NewEventStoreAdapter` has zero production callers (only test harness `newEmittingWorldService`, `chaos_helpers_test.go:136-151`). |
| `go-retry` import (in `events.go`) | `internal/world/events.go:15` | Removed with the emit path (the ~350ms retry window is the WR-01 accident, D-03). |

### Alternatives Considered
| Instead of | Could use | Tradeoff / why not |
|---|---|---|
| Locked per-game counter | `BIGSERIAL` / insert-time seq | **Forbidden** — commit-order proof breaks; gap-free ordering requires the counter allocated inside the commit tx (one-pager Feed ordering §3). |
| New `internal/world/outbox` pkg for relay | Put relay in `internal/eventbus` | Discretion (D). Recommend `internal/world/outbox` — the feed is a world-model concern and this keeps the gateway-boundary clean; but relay reuses eventbus publish primitives. |

**Version verification:** No `npm`/`pip`/`cargo` packages introduced. Go module deps (pgx/v5, oklog/ulid/v2, samber/oops) are already vendored and in use across `internal/world`. `go-retry` is *removed*, not added.

## Package Legitimacy Audit

**Not applicable** — Phase 5 installs no external packages (npm/PyPI/crates). All work is internal Go packages + already-vendored modules. No legitimacy gate required.

## Architecture Patterns

### Slice map (FIXED order — one-pager "Phase 5 first slice (ordered)")

```
Slice 1  ── Version guard (MODEL-03)
   version cols on 4 tables (migration 000049) → Version field on 4 structs
   → repo Update/Delete become version-predicated CAS + locked follow-up read
   → thread version through RMW callers (entity_mutator, UpdateCharacterDescription)
   → typed WORLD_CONCURRENT_EDIT + code registration
   → flip resilience M12 specs to assert surfaced conflicts

Slice 2  ── Outbox + relay + MoveCharacter (MODEL-04, folds WR-01/D-03)
   outbox table + per-game feed_position counter (migration)
   → mutation wrapper mutate(ctx, entity, expectedVersion, envelope) (compile-time seam)
   → repos switch pool.Exec → execerFromCtx (tx enrollment)
   → single leased relay (position order, Nats-Msg-Id dedup, LISTEN/NOTIFY + sweep, halt-and-alert)
   → MoveCharacter end-to-end through outbox; DELETE events.go emit path
   → reference idempotent consumer + bootstrap harness
   → relay lag/halt ALERTING (in THIS slice — a halted feed is otherwise silent)
   → rewrite m2_dualwrite_test.go; correct f1-resilience-verdict.md M2 paragraph

Slice 3  ── Taxonomy registry + census + rollout (MODEL-04)
   versioned schema registry (~15 types, App-Schema-Version) = ARCH-04 input
   → census meta-test (every write command → declared envelope kind)
   → register + BIND the 4 INV-WORLD-* invariants
   → mechanical emission rollout across remaining ~14 commands
   → genesis snapshot emission at cutover + feed epoch/reset procedure

Slice 4  ── MODEL-02 doc downgrade (6 sites)
```

### Pattern 1: Version-predicated CAS + locked follow-up read (the zero-row classifier)
**What:** The guarded `UPDATE`/`DELETE` returns zero rows on BOTH a stale version and a missing row. A bare `RETURNING` cannot distinguish them, so a second read *in the same transaction* classifies the outcome.
**When:** Every `Update`/`Delete` in `internal/world/postgres/*_repo.go`.
**Precedent to mirror:** `access_policies.version INTEGER NOT NULL DEFAULT 1` + `access_policy_versions` audit table (`internal/store/migrations/000001_baseline.up.sql:255-282`).
**Shape (replaces `location_repo.go:72-85`):**
```go
// Guarded CAS — note execerFromCtx so it enrolls in the outbox tx (currently pool.Exec — a bug for outbox atomicity)
tag, err := execerFromCtx(ctx, r.pool).Exec(ctx, `
    UPDATE locations SET type=$2, ..., version = version + 1
    WHERE id = $1 AND version = $3`, loc.ID.String(), /*fields*/, loc.Version)
if err != nil { return oops.With(...).Wrap(err) }
if tag.RowsAffected() == 0 {
    // Locked follow-up read in the SAME tx: SELECT version FROM locations WHERE id=$1 FOR UPDATE
    // exists+differs → WORLD_CONCURRENT_EDIT ; missing → LOCATION_NOT_FOUND ; (delete-in-flight → concurrent-delete)
}
```

### Pattern 2: Compile-time write-requires-envelope seam
**What:** A `mutate(ctx, entity, expectedVersion, envelope)` wrapper where the `envelope` parameter is non-optional; an envelope-less state write does not type-check. The wrapper enforces envelope cardinality (exactly one) and writes both the CAS'd row and the outbox row inside one `Transactor.InTransaction`.
**Where:** `internal/world` (must co-locate with the write methods so raw-repo writes are unreachable from `world.Service` without an envelope). The `Mutator` interface (`internal/world/mutator.go:18`) is the natural home to reshape.
**Enforced by three fences (one-pager Enforcement):**
- Compile-time: the wrapper signature.
- Static: census meta-test (`test/meta/`) enumerating every write command → declared envelope kind + a **depguard/lint fence** forbidding raw world-table SQL outside `internal/world/postgres`. Model: `test/meta/depguard_config_test.go` asserts `.golangci.yaml` deny rules (`:30`, holomush-1eps2), `.golangci.yaml:136-148` `depguard.deny` block.
- Runtime: the 4 invariants (see below).

### Pattern 3: Locked per-game `feed_position` counter
**What:** A `world_feed_counter(game_id, next_position)` row (or equivalent); allocation is `SELECT next_position FROM world_feed_counter WHERE game_id=$1 FOR UPDATE` → use → `UPDATE … SET next_position = next_position + 1`, all inside the mutation tx. Gap-free and commit-ordered because the row lock serializes writers per game.
**Accepted tradeoff:** serializes world writes per game — a throughput ceiling acceptable at MUSH write rates. Do NOT revert to insert-time allocation.
**Game-id sourcing:** today single-game `main` (M2 verdict subjects read `events.main.location.<id>`). Confirm the `game_id` provenance for the counter key (see Open Questions).

### Pattern 4: Single leased relay (mirror the audit DLQ posture)
**What:** One leased publisher tails the outbox strictly in `feed_position` order, publishes to JetStream with `Nats-Msg-Id` = envelope ULID (JetStream `DupeWindow` + consumer-side dedup beyond the window), woken by `LISTEN/NOTIFY` with a periodic sweep fallback, marks rows published after PubAck and prunes. Poison envelope → **halt-and-alert** (does not skip — an ordered feed must not have holes).
**Precedent:** `internal/eventbus/audit/projection.go` — bounded DLQ provisioned at construction (`:131-136`), final-attempt capture + `Term` (`:255-268`). Relay differs: it must HALT on poison (feed order), not DLQ-and-continue like the audit projection. Reconcile this difference explicitly in the plan.
**Alerting:** relay lag/halt metric + alert lands in slice 2 (a halted ordered feed is silent otherwise). Mirror `observability.RecordEngineFailure` counter style (`internal/world/service.go:149`).

### Anti-Patterns to Avoid
- **`r.pool.Exec` inside the mutation tx.** Silently escapes the outbox transaction → atomicity lost, no compile/test failure. Every write repo method must use `execerFromCtx(ctx, r.pool)`.
- **`BIGSERIAL` / insert-time `feed_position`.** Breaks the commit-order proof.
- **DLQ-and-continue on relay poison.** Creates a feed gap; the relay must halt-and-alert.
- **Emitting on failed/no-op commands.** One envelope per *successful* externally-visible command only.
- **Hand-minting `event_id`.** Use `core.NewEvent()`/`core.NewULID()` (event conventions rule).

## Runtime State Inventory

> Refactor phase — the version-column migration and emit-path deletion have runtime blast radius beyond source files.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `locations`, `exits`, `characters`, `objects` tables gain `version` (DEFAULT 1 backfills existing rows atomically — no data migration needed). New `outbox` + `world_feed_counter` tables start empty. **Genesis snapshot** must emit one envelope per existing aggregate at cutover to give the feed a defined origin. | Migration 000049+ (paired up/down, idempotent); genesis emission job (slice 3). |
| Live service config | The JetStream EVENTS stream / subjects (`events.main.location.<id>` etc.) — the relay publishes here. No UI-only config; the feed subject taxonomy is code-owned. | Confirm relay publish subjects vs existing `LocationStream`/`CharacterStream` builders being deleted (`events.go:29-42`). |
| OS-registered state | None — no OS-level task/registration embeds world-model identifiers. | None (verified: relay is an in-process `lifecycle.Subsystem`). |
| Secrets / env vars | `HOLOMUSH_RUN_QUARANTINED` gates the resilience suite (`f1-resilience-verdict.md`). No secret-key rename. | None. |
| Build artifacts / installed packages | Deleting `internal/world/events.go` removes exported `EmitMoveEvent` etc.; any importer breaks at compile. `NewEventStoreAdapter` production callers = zero (ADR); test harness `chaos_helpers_test.go:136-151` uses it — rewrite under D-03. | Grep importers of `world.EmitMoveEvent`/`world.EventEmitter`/`world.EventStoreAdapter` before deletion; `task test:int` after (integration files not compiled by `task test`). |

**Canonical question — after every file is updated, what still references the old shape?** The `EventEmitter` interface and `Emit*` helpers; the `chaos_helpers_test.go` emitter wiring; and any plugin/gRPC caller expecting `move_succeeded=true` semantics. All must be swept in slice 2.

## World Write Command Census (D-01 input)

The ~15 externally-visible world write commands the outbox envelope must be wired through (all in `internal/world/service.go`), plus their emission status today:

| # | Command | Line | Emits today? | Aggregate(s) |
|---|---------|------|-------------|--------------|
| 1 | `CreateLocation` | :205 | no | location |
| 2 | `UpdateLocation` | :230 | no | location (RMW via entity_mutator) |
| 3 | `DeleteLocation` | :256 | no (tx cascade) | location + tombstone |
| 4 | `CreateExit` | :315 | no | exit |
| 5 | `UpdateExit` | :343 | no | exit |
| 6 | `DeleteExit` | :370 | no (bidirectional cascade) | exit + tombstone(s) |
| 7 | `CreateObject` | :445 | **yes** (`EmitObjectCreateEvent`) | object |
| 8 | `UpdateObject` | :473 | no (RMW via entity_mutator) | object |
| 9 | `DeleteObject` | :502 | no (tx cascade) | object + tombstone |
| 10 | `MoveObject` | :549 | **yes** (`EmitMoveEvent`) | object |
| 11 | `DeleteCharacter` | :602 | no (tx cascade) | character + tombstone |
| 12 | `UpdateCharacterDescription` | :656 | no (RMW inline) | character |
| 13 | `MoveCharacter` | :773 | **yes** (`EmitMoveEvent`) — the M2 window; slice-2 first mover | character |
| 14 | `AddSceneParticipant` | :703 | no | scene (participant) |
| 15 | `RemoveSceneParticipant` | :724 | no | scene (participant) |

Plus the property write path (`internal/property/entity_mutator.go` `SetName`/`SetDescription` → funnel into `UpdateLocation`/`UpdateObject`) and `PropertyRepository.Create/Update/Delete` (`internal/world/postgres/property_repo.go:35,115,153`). **Examine commands (`ExamineLocation/Object/Character`, service.go:859/917/975) are reads that emit — NOT state mutations.** Per one-pager the feed carries *state-change* envelopes; the plan must decide whether examine emissions survive as feed events or are dropped (see Open Questions). **`CreateCharacter` is not a `world.Service` method** — character creation lives in the registration/binding flow; enumerate it during census (it is a world-state write that needs an envelope).

**Version columns go on 4 tables only** (locations, exits, characters, objects). `entity_properties` is a child row guarded by its parent's version + delta-parity, not a 5th version column (one-pager §1 names exactly four).

## Don't Hand-Roll

| Problem | Don't build | Use instead | Why |
|---------|-------------|-------------|-----|
| Same-tx state + outbox write | A new tx manager | `Transactor.InTransaction` + `execerFromCtx` | Already the cascade-delete seam (`service.go:270-287`); proven. |
| Poison / dead-letter handling | Bespoke DLQ | Mirror `internal/eventbus/audit/projection.go` DLQ posture | Bounded DLQ + final-attempt `Term` pattern already reviewed. |
| Publish dedup | App-level idempotency table | JetStream `Nats-Msg-Id` = ULID + `DupeWindow` | Event conventions rule; already used by audit. |
| Relay lifecycle | Bare goroutine | `lifecycle.Subsystem` (mirror `WorldSubsystem`) | Ordered start/stop, `DependsOn`, `SubsystemID` iota. |
| ULID / event identity | `idgen.New()` for events | `core.NewEvent()` / `core.NewULID()` | `idgen.New()` is for entity PKs; events use monotonic ULID (CLAUDE.md ULID table). |
| Optimistic version audit | New pattern | `access_policies` + `access_policy_versions` shape | In-schema precedent to mirror. |

## Common Pitfalls

### Pitfall 1: Repos not enrolled in the outbox transaction
**What goes wrong:** `Update`/`Create`/`Move` use `r.pool.Exec` directly (`location_repo.go:72`), so they open their own connection and commit independently of the `Transactor.InTransaction` wrapping the outbox insert — the atomicity guarantee silently fails.
**Why:** Only `Delete` currently reads the tx from context (`execerFromCtx`, `helpers.go:42`). The gap is invisible to `task test` (unit) because mocks don't exercise real tx.
**How to avoid:** Switch every write repo method to `execerFromCtx(ctx, r.pool)`; assert atomicity in an integration test (broker/DB fault between row write and outbox write must roll back both).
**Warning signs:** an outbox row exists without its state change, or vice-versa, after a mid-tx fault.

### Pitfall 2: Zero-row ambiguity collapsed to NOT_FOUND
**What goes wrong:** Reusing today's `RowsAffected()==0 → LOCATION_NOT_FOUND` (`location_repo.go:81-83`) after adding the version predicate reports a **stale-version conflict as not-found**, defeating MODEL-03.
**How to avoid:** The locked follow-up read must run and classify; only a genuinely absent row is `NOT_FOUND`, an existing-but-different-version row is `WORLD_CONCURRENT_EDIT`.

### Pitfall 3: Deleting `events.go` breaks the resilience harness before slice 2 rewrites it
**What goes wrong:** `chaos_helpers_test.go:136-151` (`newEmittingWorldService`) wires `world.NewEventStoreAdapter` + `EventEmitter`; deleting the emit path breaks compilation of an `//go:build integration` file that `task test` does NOT compile — so it passes locally and fails in CI `Integration Test`.
**How to avoid:** Rewrite `m2_dualwrite_test.go` + chaos helpers in the SAME slice-2 commit that deletes the emit path (D-03). Run `task test:int` (per CLAUDE.md refactor rule).

### Pitfall 4: Relay DLQ-and-continue creates a feed gap
**What goes wrong:** Copying the audit projection's DLQ-and-continue verbatim would let the relay skip a poison envelope, violating `INV-WORLD-FEED-ORDER` (gap-free).
**How to avoid:** Relay HALTS and alerts on poison; it does not advance past an unpublishable position.

### Pitfall 5: Binding an invariant to a test that doesn't prove it
**What goes wrong:** A `// Verifies: INV-WORLD-*` on a test that merely touches the code is a false-green (the INV-RB-3 bug class, `.claude/rules/invariants.md`).
**How to avoid:** Each of the four invariants needs a test that genuinely asserts it (delta-parity must prove the manifest *matches* the committed delta — presence is insufficient). `TestBoundInvariantsAreGenuinelyAsserted` guards Skip-only stubs but cannot catch partial bindings.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` + testify (unit); Ginkgo/Gomega (integration, `//go:build integration`) |
| Quick run | `task test -- ./internal/world/...` (unit; does NOT compile integration files) |
| Full suite | `task test:int` (embedded NATS + Postgres testcontainers) |
| Resilience gate | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` (D-05 standing gate) |
| Census / lint fences | `test/meta/` meta-tests + `task lint` (depguard) |

### Success criterion → proof map
| Criterion (MODEL-) | Proof | Test type | Where |
|---|---|---|---|
| 03: concurrent writer cannot silently lose an update | M12 spec flipped to assert `WORLD_CONCURRENT_EDIT` surfaced (was: both return nil) | integration (resilience) | `test/integration/resilience/m12_lastwritewins_test.go` |
| 03: guard holds under two-replica | per-aggregate race tests, two replicas / one broker / one DB | integration (resilience) | `m12_*` + new race specs |
| 03: zero-row classifier correct | conflict vs not-found vs concurrent-delete unit/integration cases | unit + integration | `internal/world/postgres/*_repo_test.go` |
| 04: mutation + emission atomic | fault between row write and outbox write rolls back both; NATS blip after commit cannot lose the envelope (relay redelivers) | integration (resilience) | rewritten `m2_dualwrite_test.go` |
| 04: feed ordered + gap-free | relay publishes strictly in `feed_position` order; poison halts | integration | new relay spec + `INV-WORLD-FEED-ORDER` binding |
| 04: one envelope per write command | census meta-test enumerates command → envelope kind | meta-test | `test/meta/` |
| 04: writer boundary | lint fence: raw world-table SQL only in `internal/world/postgres`; `INV-WORLD-WRITER-BOUNDARY` bound | static + runtime | `.golangci.yaml` depguard + invariant test |
| 02: no false ES doc claim | grep assertion that the 6 sites no longer state replay-derived world state | doc-grep / meta | slice 4 |
| Invariants bound not pending | `task test -- -run 'TestEveryRegistryInvariantHasBinding\|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/` green | meta-test | `test/meta/invariant_registry_test.go` |

### Fault-injection needed (extend `test/integration/resilience/`)
Relay crash around PubAck (envelope published but row not marked → redeliver, consumer dedups on `Nats-Msg-Id`); dual relay (two leases — only one publishes); duplicate delivery (consumer idempotency); broker downtime (relay backs off, resumes in order); per-aggregate concurrent writers (conflict surfaced). Harness seams: `integrationtest.WithExternalNATS(url)` (`options.go:34`), `WithSharedDatabase(connStr)` (`:52`), `WithInTreePlugins()` (`plugins.go:163`); replica bring-up `startReplica` (`chaos_helpers_test.go:172`), broker pause/unpause (`pauseBroker`/`unpauseBroker` `:235/:250`).

### Codecov gotcha
Quarantine-gated resilience suites (`quarantinetest.Enabled()`, D-05) read as **uncovered** by codecov/patch. **Seam-existence tests must be non-quarantined** — precedent: `internal/testsupport/integrationtest/resilience_seams_test.go` self-skips off the gating lane but gives the seams gating-CI coverage. Any new outbox/relay seam needs a matching non-quarantined seam test.

## Invariant Registration Mechanics (the 4 INV-WORLD-*)

`WORLD` is a **new scope** — not present in `docs/architecture/invariants.yaml` (verified) nor among migrated boundaries. Register/bind ratchet (`.claude/rules/invariants.md`):

1. Add four entries to `docs/architecture/invariants.yaml` with `id: INV-WORLD-<NAME>`, `boundary: WORLD`, `scope`, `origin_spec` (Phase 5 spec), `summary`, `binding: pending`. Consult existing scopes first; do NOT mint an ad-hoc family.
2. Write/locate the genuinely-asserting `*_test.go` for each; annotate `// Verifies: INV-WORLD-<NAME>`.
3. Flip `binding: bound` + add `asserted_by:` listing the test file(s). (A `pending` entry MUST NOT carry `asserted_by`.)
4. Regenerate: `go run ./cmd/inv-render` (never hand-edit the generated regions of `invariants.md`).
5. Confirm: `task test -- -run 'TestEveryRegistryInvariantHasBinding|TestProvenanceGuard|TestBoundInvariantsAreGenuinelyAsserted' ./test/meta/`.

The four:
- **`INV-WORLD-ATOMIC-FEED`** — state change + its one envelope commit atomically (same tx). Bind: fault between the two rolls back both.
- **`INV-WORLD-DELTA-PARITY`** — the envelope's affected-aggregates manifest provably **matches** the committed delta (presence insufficient). Bind: assert manifest before/after versions equal the row's actual version transition.
- **`INV-WORLD-FEED-ORDER`** — relay publishes strictly in `feed_position` order, gap-free; poison halts. Bind: relay spec asserting order + halt-on-poison.
- **`INV-WORLD-WRITER-BOUNDARY`** — no world-table writes outside the mutation wrapper; admin repairs AND migrations/backfills either emit normal mutations or advance the feed epoch. Bind: lint fence + a test proving raw-SQL writes are unreachable.

**Orphan-check caveat:** the meta-test's spec-orphan scan walks only `docs/superpowers/specs/`. If the Phase 5 spec lives under `.planning/` or `docs/specs/`, the four `INV-WORLD-*` will NOT be auto-caught — they must be registered by hand (`.claude/rules/invariants.md` "Known escape hatches").

## MODEL-02 Doc Sites (grep-verified, slice 4)

| # | Site | Line(s) | Current false claim |
|---|------|---------|---------------------|
| 1 | `CLAUDE.md` | 274 | "**Event sourcing** — actions produce immutable ordered events; state derives from replay." |
| 2 | `README.md` | 18, 22 | "event-sourced architecture"; "session persistence and event replay" |
| 3 | `site/src/content/docs/contributing/reference/coding-standards.md` | 344 (`### Event Sourcing`), 348 ("State is derived from event replay") | ES stated as coding principle |
| 4 | `site/src/content/docs/contributing/explanation/architecture.md` | 79 ("All actions are stored and replayable"), 295 ("Append-only, replayable"), 305 (`### Event Sourcing`), 309 ("Current state is derived from event replay") | ES stated as architecture principle |

Downgrade target phrasing (ADR/one-pager): **"event-driven with an append-only audit log."** Keep the *client catch-up / Subscribe replay* language intact — that IS real (`architecture.md:74` "Reconnecting clients catch up from their last seen event" is correct and MUST NOT be downgraded). The "~6 sites" in the requirement counts individual lines; the enumerated set above is 4 files / ~8 lines. **Public marketing `index.mdx`:** the requirement names a public `index.mdx` — grep did not surface an ES/offline-PWA claim in `site/src/content/docs/index.mdx` on this branch (the PWA claim is F6/PWA-01, deferred). Planner: re-grep `site/src/content/docs/**/*.mdx` for `event.sourc`/`derives from replay` at plan time to confirm the exact site count (see Open Questions).

## Security Domain (D-04 gate confirmation)

| Gate | Triggers? | Evidence |
|------|-----------|----------|
| `crypto-reviewer` | **No (expected)** | Phase 5 touches `internal/world/**`, `internal/property/**`, `internal/store/migrations/**`, `test/integration/resilience/**`, `test/meta/**`, docs. It does NOT touch `internal/eventbus/crypto/`, `internal/eventbus/codec/`, `history/dispatcher.go`, `cold_postgres.go`, `event_emitter.go::Emit`, `audit/projection.go` (mirrored, not modified), plugin `crypto.emits`, or `crypto_keys`/`events_audit` migrations. The relay publishes cleartext world-change envelopes (intent-level, new-values-only) — no per-event DEK. |
| `abac-reviewer` | **No (expected)** | No change to `internal/access/`. `checkAccess` stays in `internal/world/service.go` (pre-write, unchanged); the relay never re-authorizes. |

**Planner MUST confirm** the outbox relay package placement does not import or modify a crypto-gated file. If the relay is placed such that it touches `internal/eventbus/audit/projection.go` (rather than mirroring it in a new package), the crypto gate could trigger — recommend the new `internal/world/outbox` package to keep the diff off gated surfaces. ASVS: V5 (input validation) applies to envelope payload construction; no V2/V3/V6 surface (no auth/session/crypto change).

## Environment Availability

| Dependency | Required by | Available | Notes |
|------------|------------|-----------|-------|
| PostgreSQL (testcontainer) | version migration, outbox, resilience | ✓ | via `task test:int` / integrationtest harness |
| NATS JetStream (testcontainer) | relay publish, resilience | ✓ | embedded (`eventbustest`) + real container (`natstest`) for external-mode |
| Docker | `task test:int`, resilience suite | ✓ (assumed) | resilience suite needs it (`f1-resilience-verdict.md`) |
| `go run ./cmd/inv-render` | invariant binding regen | ✓ | in-tree tool |

No missing dependencies with no fallback.

## Assumptions Log

| # | Claim | Section | Risk if wrong |
|---|-------|---------|---------------|
| A1 | `game_id` is effectively single-value (`main`) today; the per-game counter keys on it | Pattern 3 | If multi-game is live, counter keying + genesis scope changes. Low — M2 subjects show `main`. |
| A2 | Examine emissions are reads, not feed state-changes, and may be dropped from the outbox | Census | If examine must stay a feed event, add ~3 command envelopes. Medium. |
| A3 | The public `index.mdx` ES claim named in MODEL-02 does not exist on this branch (only 4 files carry it) | Doc Sites | If it exists elsewhere, one more doc task. Low — grep clean; planner re-greps. |
| A4 | `CreateCharacter` exists in a non-`world.Service` flow and needs an envelope | Census | If character creation is out of world-write scope, one fewer command. Medium — enumerate at plan time. |
| A5 | `NewEventStoreAdapter` has zero production callers (only test harness) | To DELETE | If a production caller exists, deletion breaks it. Low — ADR states zero; grep before delete. |
| A6 | Relay belongs in a new `internal/world/outbox` pkg (discretion) | Standard Stack | Placement is Claude's discretion; different choice changes crypto-gate confirmation. Low. |

## Open Questions

1. **Examine-event fate.** Do `ExamineLocation/Object/Character` emissions survive as feed events or drop entirely under the state-change-only feed? *Recommendation:* drop from the world-change feed (they are reads); if plugins depend on examine notifications, that is a separate notification concern, not a world-state envelope.
2. **`game_id` provenance for the counter.** Where does the relay/counter obtain `game_id`? *Recommendation:* single `main` for now; make the counter keyed on `game_id` so multi-game is a data change, not a schema change.
3. **`CreateCharacter` command site.** Enumerate the character-creation write path (registration/binding flow) during census — it is a world-state write needing an envelope.
4. **Exact MODEL-02 site count.** Requirement says ~6 sites incl. a public `index.mdx`; grep found 4 files/~8 lines and no `index.mdx` ES claim. Re-grep `site/src/content/docs/**/*.mdx` at plan time.
5. **Relay poison posture vs audit projection.** Audit DLQ-and-continues; relay must halt-and-alert. Confirm the DLQ machinery can be reused in *halt* mode or needs a distinct implementation.

## Sources

### Primary (HIGH confidence — verified this session)
- `docs/adr/holomush-i4784-world-state-model-decision.md` — Decision + Consequences (NORMATIVE input contract).
- `docs/reviews/arch-review/2026-07-11/verification/model-01-consensus-onepager.md` — NORMATIVE mechanism shape.
- `docs/reviews/arch-review/2026-07-11/verification/f1-resilience-verdict.md` — M12/M2 empirical verdict + citation chains.
- `internal/world/service.go`, `mutator.go`, `events.go`, `setup/subsystem.go`; `internal/world/postgres/{location_repo,transactor,helpers}.go`; `internal/property/entity_mutator.go` — read in full / grepped.
- `internal/store/migrations/000001_baseline.up.sql:255-282` (access_policies version precedent); migrations dir (next = 000049).
- `internal/eventbus/audit/projection.go:61,121,131,255-268` (DLQ/poison posture).
- `.claude/rules/invariants.md`, `event-conventions.md`, `database-migrations.md`; `test/meta/depguard_config_test.go`; `.golangci.yaml:136-148`.
- `internal/testsupport/integrationtest/options.go:34,52`, `plugins.go:163`, `resilience_seams_test.go`; `test/integration/resilience/chaos_helpers_test.go`.
- MODEL-02 doc grep: `CLAUDE.md:274`, `README.md:18,22`, `coding-standards.md:344,348`, `architecture.md:79,295,305,309`.

### Secondary (MEDIUM)
- `05-CONTEXT.md`, `.planning/REQUIREMENTS.md` — user decisions + acceptance wording.

## Metadata

**Confidence breakdown:**
- Version guard sites & precedent: HIGH — repos and access_policies precedent read directly.
- Outbox/relay shape: HIGH on mechanism (one-pager NORMATIVE) / MEDIUM on package placement (Claude's discretion).
- Command census: HIGH for `world.Service` methods; MEDIUM on `CreateCharacter`/examine/property inclusion (Open Questions).
- MODEL-02 sites: HIGH for the 4 enumerated; MEDIUM on total count vs the requirement's "~6 incl. index.mdx".
- Invariant mechanics: HIGH — ratchet rule read directly; WORLD confirmed new scope.

**Research date:** 2026-07-12
**Valid until:** ~2026-08-11 (stable — internal refactor against a decided mechanism; re-verify line numbers if `internal/world` churns before planning).
