# Phase 4: World-Model Resilience Investigation & Decision (F1) - Research

**Researched:** 2026-07-11
**Domain:** Go concurrency/chaos test harness (two-replica, external NATS + Postgres) + architecture decision (event sourcing vs CRUD-canonical)
**Confidence:** HIGH (nearly all findings verified by direct code reads in this worktree)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** The ADR is **genuinely open — (A) build real event sourcing vs. (B) CRUD-canonical weighed equally, with no starting lean.** The user explicitly overrode the F1 doc's recommended lean toward (B). Consequence: research MUST genuinely cost out option (A) — what a real projection/rebuild path for world state (locations, exits, characters, objects) would actually entail, whether replayability / auditable state-reconstruction is wanted, and its effort — not merely confirm CRUD. The harness evidence and the (A)-cost analysis together drive the decision.
- **D-02:** The ADR MUST record *intent* (was world-state event sourcing ever meant to be real, or was "event sourcing" always shorthand for "event-driven + audit log"?). The F1 archaeology already concludes "architectural **drift by default** — a stated principle never realized for the world model, no ADR ever recorded the divergence"; the ADR formalizes and closes that gap (the missing ADR *is* the finding).
- **D-03:** Reproduce the two-replica deployment with a **real single-node NATS JetStream container (`internal/testsupport/natstest`) + a Postgres testcontainer + two in-process `CoreServer` replicas** sharing the one broker/DB. This is the documented external-NATS integration tier — multi-replica / external-broker behavior MUST use a real NATS container per `.claude/rules/testing.md`, NOT the shared embedded `eventbustest` connection. Broker flap = pause/restart the NATS container; replica restart = recreate one `CoreServer`; client reconnect = re-open the gRPC/session stream.
- **D-04 (rejected):** A full two-OS-process / docker-compose stack (higher fidelity but slower + flakier to orchestrate) and a lighter single-process simulated-concurrency harness (cheaper but doesn't exercise true two-replica separation) were both considered and NOT chosen.
- **D-05:** The harness is **kept in-tree but excluded from the gating CI / PR lane** — opt-in via the quarantine mechanism (`HOLOMUSH_RUN_QUARANTINED=1` / `quarantinetest.Enabled()`; runs nightly + locally), documented and reproducible on demand. It survives as a standing regression check for the Phase 5 MODEL-03 version guard **without** flaking the required `Integration Test` PR gate. Rationale: CONCERNS.md flags two-replica chaos as CI-resource-sensitive (the `ConcurrentUp`/`holomush-pqzv` testcontainer-port-map timeout quarantine entry) and thin CI scale headroom.
  - NB per `.claude/rules/testing.md`: quarantine is normally for *known-flaky specs with an open issue*, not for a deliberately-gated investigation harness. Planner MUST decide the exact opt-in seam (a dedicated `HOLOMUSH_*` env flag vs. reusing the quarantine marker + a `test/quarantine.yaml` row citing #4791) and honor the marker↔registry bijection meta-test if the quarantine idiom is reused.
- **D-06:** The harness MUST **empirically reproduce actual last-write-wins state corruption for M12** (concurrent commands mutating the same world entity → a silently lost update) — OR prove it cannot occur — under the two-replica deployment. This is success-criterion #2's load-bearing verdict.
- **D-07:** For **M2 (dual-write non-atomicity — a world change commits to the DB but its post-commit event/notification is lost on a NATS blip, `move_succeeded:true`)**, the harness **characterizes / proves the race window exists** on a broker flap; it need NOT force a deterministic observable lost move. M2's mechanism is already well-established by the F1 archaeology (events are a post-commit notification, so a flap loses the notification while the DB write persists).

### Claude's Discretion

- ADR file naming/slug follows the existing `docs/adr/NNNN-<slug>.md` convention (197 ADRs; sequential 4-digit prefix) — planner/executor pick the next number + slug. *(NB: research below verifies the directory's actual naming — see "ADR Convention (verified)".)*
- Which world entities the harness exercises (locations/exits/characters/objects) and the concrete concurrent-command pair used to trigger M12 — planner's call, grounded in the actual `world.Service` write path.
- Whether the ADR also proposes the (A)-vs-(B) decision framework / scoring rubric shape.

### Deferred Ideas (OUT OF SCOPE)

None — discussion stayed within phase scope.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OPS-05 | Resilience/concurrency pass: concurrent commands + NATS broker flap + replica restart + client reconnect; empirically establish whether M12 corrupts state under two-replica concurrency | Harness substrate verified (natstest, ModeExternal eventbus, integrationtest extension points, Detach/ReattachTransport); concrete M12 command pair identified (`describe`/`set` → entity_mutator read-modify-write → full-row UPDATE); M2 window mechanics quantified (~350ms emit-retry window, `move_succeeded=true`); flap mechanics verified (docker pause/unpause vs Stop/Start) |
| MODEL-01 | Committed ADR resolving event-sourcing-vs-CRUD, grounded in F1 + resilience evidence, naming Phase 5's mechanism | Option (A) genuinely costed against actual code (event coverage gaps, retention conflict, unwired emitter, ARCH-04 dual models); Option (B) mechanics grounded (no version columns today, existing versioned-row precedent, Transactor precedent, no outbox exists); ADR naming convention verified against docs/adr/ |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **MUST use `task` for all build/test/lint/format** — never `go test` directly; verbose runs delegated to `local-check` (research/plan docs exempt; executors must comply).
- **TDD mode is ON** (`.planning/config.json workflow.tdd_mode: true`) — tests before implementation; RED/GREEN/REFACTOR gates.
- **Test tiers** (`.claude/rules/testing.md`): external-mode / multi-replica behavior **MUST** use a real NATS container via `internal/testsupport/natstest`; embedded `eventbustest` is wrong for this phase's harness. Integration tests: Ginkgo/Gomega, `//go:build integration`, run via `task test:int`.
- **Depguard:** production code MUST NOT import `eventbustest`/`coretest`/`natstest`/`quarantinetest` — all harness code stays under `test/` or `internal/testsupport/` with the `integration` build tag.
- **`task test:int` compiles `./...` with `-tags=integration`** (Taskfile.yaml:165-189) — any new integration package is auto-included in the gating `Integration Test` CI check unless it self-skips or carries an extra build tag (load-bearing for D-05).
- **SPDX headers** required on new `.go` files (Apache-2.0).
- **ACE test naming**; table-driven tests; `require` for preconditions.
- **Spec-driven:** the ADR is the spec artifact here. If the ADR introduces a system-level invariant, register it in `docs/architecture/invariants.yaml` per `.claude/rules/invariants.md` (do NOT mint ad-hoc families). NB: the MODEL-03 guard invariant more naturally belongs to Phase 5's spec; the Phase 4 ADR should *name* the mechanism, Phase 5 registers/binds its invariants.
- **ADRs are immutable once accepted**; supersession via a new ADR + `**Status:**` update.
- **Protected main; squash-merge PRs; `task pr-prep` before push; `task fmt` mutates files — commit its output.**
- **Judge command success by exit code, never by grepping output** (`.claude/rules/search-tools.md`).
- **Worktree isolation:** all work in `/Users/sean/Code/github.com/holomush/.worktrees/v0.12-phase4`.
- **Terminology:** `location` never `room`; `character` vs `player` distinction (matters for ADR prose and doc corrections deferred to MODEL-02).

## Summary

This phase has two deliverables: (1) a two-replica resilience harness producing an empirical M12/M2 verdict, and (2) a committed ADR deciding the world-state model. Research verified every load-bearing substrate claim against the actual code.

**The M12 lost-update is real and concretely reachable through in-tree commands.** The property write path (`internal/property/entity_mutator.go`) is a textbook read-modify-write: `GetLocation → mutate one field → UpdateLocation(full object)`, and every world repo `Update` is a **full-row UPDATE with no version guard** (`UPDATE locations SET type, shadows_id, name, description, … WHERE id=$1` at `internal/world/postgres/location_repo.go:73`; same shape for characters:64, objects:79, exits:143). The world tables have **no version column and no updated_at** (migration `000001_baseline.up.sql:68-160`), so no CAS is possible today. Two concurrent plugin commands against the same entity from two replicas — e.g. core-objects `describe here <text>` racing `set here/name=<value>` on one location — interleave as read/read/write/write and silently drop one field's change while both commands report success. That is the demonstrable corruption D-06 demands.

**M2 is structurally worse than a race today:** `world.Service.MoveCharacter` commits the row, then fires the movement hook, then emits post-commit with a ~350ms retry window (`emitWithRetry`: 3 retries, exponential from 50ms) and returns an error tagged `move_succeeded=true` on emit failure — but **production never wires an EventEmitter into `world.Service` at all** (`internal/world/setup/subsystem.go:66-77` omits it; `NewEventStoreAdapter` has zero production callers), and no in-tree command invokes `MoveCharacter` (F5 #4788). The harness must deliberately wire the emitter to characterize the window; the ADR should record that the notification leg is currently dead code in production wiring.

**The harness substrate exists but needs a specific extension:** `integrationtest.Start` hard-codes the embedded bus (`eventbustest.New(t)`) and a fresh per-call database — two `Start` calls today produce two *disjoint* stacks, not two replicas of one deployment. The extension is new `StartOption`s that (a) swap the bus for an external-mode `eventbus.Subsystem` (`Config{Mode: ModeExternal, URL: natsEnv.URL}` — the production dial path, already proven against natstest in `test/integration/eventbus_external/`) and (b) share one `connStr`/pool across both replicas. Client reconnect already exists (`Session.DetachTransport`/`ReattachTransport`); broker flap should use docker pause/unpause (port-stable) with container Stop/Start as the restart-fidelity variant.

**Primary recommendation:** Plan three work streams — (1) extend `integrationtest` with `WithExternalNATS(url)`/`WithSharedDatabase(connStr)`-style options + a two-replica suite under `test/integration/resilience/` gated by `quarantinetest.Enabled()` (no quarantine.yaml marker — see D-05 analysis); (2) run the M12 corruption experiment (describe-vs-set on one location) and the M2 flap characterization (wired emitter + docker-pause during a move); (3) write the ADR as `docs/adr/holomush-i4784-<slug>.md` (dominant convention + post-beads token grammar) using the verified Option-A/Option-B cost inputs below.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Two-replica harness (replica assembly, chaos control) | Test infrastructure (`internal/testsupport/integrationtest`, `//go:build integration`) | `test/integration/resilience/` suite | D-03 fixes the substrate; depguard forbids production imports |
| External broker lifecycle (start/pause/restart) | Test infrastructure (`internal/testsupport/natstest` + docker client) | — | Existing external-NATS tier owner |
| Concurrent command execution | In-process `CoreServer` + command dispatcher + Lua plugin runtime (per replica) | `world.Service` write path | Commands are the production-fidelity entry point per CONTEXT |
| Corruption observation (read-back) | Direct Postgres reads (shared pool) | `world.Service` getters | Verdict must observe committed DB state, not caches |
| M2 event-loss observation | JetStream stream inspection (`jetstream.JetStream` over harness conn) | — | Compare DB commit vs stream contents after flap |
| ADR + decision record | `docs/adr/` | README.md index row | Repo docs tier; MODEL-02 doc corrections stay in Phase 5 |
| Opt-in gating (nightly lane) | `quarantinetest` env mechanism + `.github/workflows/nightly-soak.yml` | `test/quarantine.yaml` (only if marker seam chosen) | D-05 |

## Standard Stack

All in-repo / already-pinned — **this phase installs no new external packages.**

### Core

| Library / Package | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `internal/testsupport/integrationtest` | in-repo | In-process Postgres + NATS + production `CoreServer` stack; base to extend to two replicas | D-03 locked; canonical harness [VERIFIED: harness.go read] |
| `internal/testsupport/natstest` | in-repo | Real single-node NATS JetStream container (`nats:2-alpine`, `-js -sd /data`); `StartNATS(ctx) (*NATSEnv, error)`, `env.URL`, `env.Conn(t)`, `env.Terminate(ctx)` | D-03 locked; the external-mode tier substrate [VERIFIED: nats.go read] |
| `internal/eventbus` `Subsystem` ModeExternal | in-repo | Per-replica production bus over the shared broker: `eventbus.NewSubsystem(eventbus.Config{Mode: eventbus.ModeExternal, URL: env.URL}.Defaults())` | Production external dial path; proven against natstest in `test/integration/eventbus_external/` [VERIFIED: subsystem.go, config.go, external_boot_test.go] |
| `internal/testsupport/quarantinetest` | in-repo | `EnvVar = "HOLOMUSH_RUN_QUARANTINED"`, `Enabled()`, `Skip(t, token)` — the D-05 opt-in mechanism | D-05 locked [VERIFIED: quarantinetest.go read] |
| `github.com/testcontainers/testcontainers-go` | v0.43.0 (go.mod:38) | Container lifecycle incl. `Container.Stop(ctx, *time.Duration)`, `Start(ctx)`, `NewDockerClientWithOpts` for pause/unpause | Already pinned [VERIFIED: go.mod + `go doc` against pinned version] |
| Ginkgo/Gomega | pinned in go.mod | Full-stack integration suite style (`//go:build integration`, `RegisterFailHandler` + `RunSpecs` entry with stable `TestXxx` name) | Repo MUST for full-stack integration tests [CITED: CLAUDE.md testing table] |

### Supporting

| Library / Package | Purpose | When to Use |
|---------|---------|-------------|
| `test/testutil` `SharedPostgres(t)` / `FreshDatabase(t, env)` | Shared PG container, per-test DB (postgres.go:206/221) | Replica 1 creates the DB; replica 2 must REUSE the same connStr (new option) [VERIFIED] |
| `internal/cluster/clustertest` `NewExternal(t, env, clusterID, n, opts...)` | N-member-over-one-broker harness precedent | Pattern reference for per-replica independent conns, NOT a direct dependency [VERIFIED: external.go:53] |
| `internal/world.NewEventStoreAdapter(store)` | Bridges `world.EventEmitter` → `core.Event` append; unused in production | Harness wires it (or a bus-backed appender mirroring `busEventAppenderAdapter`, harness.go:607) to make M2 observable [VERIFIED: event_store_adapter.go; zero production callers by rg] |
| `Session.DetachTransport` / `ReattachTransport` | Client-reconnect primitive (tear down + re-open the Subscribe stream) | The "client reconnect" chaos step — already exists [VERIFIED: session.go:73-171] |
| `test/integration/crypto/cache_invalidation_test.go` | 3-replica-over-natstest Ginkgo precedent (per-member conns, BeforeAll container, DeferCleanup) | Copy its suite topology [VERIFIED: read] |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| docker pause/unpause for broker flap | Container Stop→Start | Stop/Start = true restart fidelity (connection RESET, server restart) but the mapped host port is allocated at container start and NATS clients auto-reconnect to the ORIGINAL URL — port drift would strand both replicas. Pause freezes the broker with identical networking (models a hang; JetStream publishes time out during the freeze). Recommend pause as primary flap; Stop/Start as a secondary experiment that re-resolves `MappedPort` and asserts it unchanged before trusting reconnect [VERIFIED: go doc v0.43.0; CITED: testcontainers networking docs "mapping process occurs during container startup"] |
| Driving writes via plugin commands on both replicas | Driving replica-2 writes via `world.Service` directly | Commands are higher fidelity (D-03 spirit) but require `WithInTreePlugins` on BOTH replicas (two full plugin subsystems, binary subprocesses — the CI-resource concern behind D-05). Direct `world.Service` calls on one replica exercise the identical repo write path (the race is in entity_mutator/repo SQL, not the dispatcher). Planner's fidelity/cost call; the corruption verdict is equally valid either way because both funnel into the same full-row UPDATE |
| toxiproxy (testcontainers module) for network chaos | plain docker pause | toxiproxy adds a dependency and D-03 fixed the substrate to a plain NATS container — do not add it |

**Installation:** none — no new dependencies.

## Package Legitimacy Audit

**No new packages are installed by this phase.** All libraries above are already pinned in `go.mod` on `main`. Audit not applicable; nothing to remove or flag.

## World-Model Write Path: Verified Facts (the evidence base)

These are the grounded facts the harness and ADR build on. Every claim was verified by reading the cited file in this worktree.

### M12 — last-write-wins lost update

1. **Every world repo `Update` is a full-row UPDATE with no guard** [VERIFIED]:
   - `internal/world/postgres/character_repo.go:64` — `UPDATE characters SET name = $2, description = $3, location_id = $4 WHERE id = $1`
   - `internal/world/postgres/location_repo.go:73` — `UPDATE locations SET type = $2, shadows_id = $3, name = $4, description = $5, …`
   - `internal/world/postgres/object_repo.go:79` — `UPDATE objects SET name = $2, description = $3, location_id = $4, …`
   - `internal/world/postgres/exit_repo.go:143` — full-row exits update
2. **No version column, no updated_at on any world table** (`internal/store/migrations/000001_baseline.up.sql:68-160`) — no CAS is possible against current schema [VERIFIED].
3. **The read-modify-write is in `internal/property/entity_mutator.go`** — e.g. `locationEntityMutator.SetDescription` (≈line 157): `querier.GetLocation → loc.Description = value → mutator.UpdateLocation(ctx, subjectID, loc)` (full object). Same pattern for `SetName`, and for objects/characters [VERIFIED].
4. **Real command chain to that path** [VERIFIED]: core-objects Lua plugin `describe me|here|<target>=<text>` and `set` commands (`plugins/core-objects/main.lua:63-107`) → Lua `set_property` hostfunc / binary `PropertyService.SetProperty` (`internal/plugin/hostcap/property.go:68`) → `PropertyDefinition.Set` → `entity_mutator` → `world.Service.UpdateLocation/UpdateObject` (ABAC-checked, `service.go:230`) → full-row UPDATE.
5. **The concrete concurrent-command pair (recommended):** replica A runs `describe here <new desc>`, replica B concurrently runs `set here/name=<new name>` against the SAME location (or two `describe` on the same object). Interleaving read(A), read(B), write(A), write(B) → B's stale `description` overwrites A's committed write; both commands return success. Cross-field silent loss = corruption, not mere "last description wins". A same-field race (`describe` vs `describe`) shows LWW but is arguably acceptable UX; the cross-field race is the unambiguous verdict.
6. **`UpdateCharacterDescription`** (`internal/world/service.go:656-680`) is the same Get→mutate→full-row-Update shape on characters — racing it against `characterRepo.UpdateLocation` (targeted, `character_repo.go:111`) would resurrect a stale `location_id` (a character snapping back to a previous location). Higher drama, but note `MoveCharacter` is not command-reachable today (see M2) — the location-revert variant needs a direct `world.Service.MoveCharacter` call on one side.
7. **Objects containment constraint** (`chk_exactly_one_containment`, baseline.up.sql:146-151) does NOT prevent the stale write — a full-row UPDATE from a stale read writes a *consistent but stale* containment; the corruption is silent.
8. **Timing note:** the race window is between Get and Update inside one SetProperty call — milliseconds wide. To make the harness deterministic rather than probabilistic, run N iterations of the pair with a barrier (e.g. `sync.WaitGroup` + start-gun channel) and assert at least one lost field across N; or widen the window by racing many concurrent `describe`/`set` pairs. Do NOT add test-only sleeps to production code. A probabilistic verdict is acceptable if the loss reproduces reliably at N ≤ a few hundred iterations (the report needs "documented, reproducible", not single-shot determinism).

### M2 — dual-write non-atomicity

1. **`world.Service.MoveCharacter`** (`internal/world/service.go:773-858`): commits `characterRepo.UpdateLocation` → fires `movementHook.OnCharacterMoved` (session-store propagation) → `EmitMoveEvent` post-commit. Both hook-failure and emit-failure paths return errors carrying `move_succeeded=true` (service.go:817-826, 841-849) [VERIFIED].
2. **Emit retry window:** `emitWithRetry` (`internal/world/events.go:74-117`) = 3 retries, exponential backoff from 50ms → total ≈350ms + publish timeouts. A broker outage longer than ~1s deterministically exhausts retries → the DB write persists, the notification is lost, the caller sees `EVENT_EMIT_FAILED`/`CHARACTER_MOVE_EVENT_FAILED` with `move_succeeded=true` [VERIFIED].
3. **CRITICAL WIRING FACT:** production constructs `world.Service` with **no EventEmitter** — `internal/world/setup/subsystem.go:66-77` omits the field (as does the integrationtest harness, `plugins.go:264-275`, with an explicit comment); `world.NewEventStoreAdapter` has **zero production callers** (rg across non-test Go). `world.NewService` logs a warning and `EmitMoveEvent` returns `ErrNoEventEmitter`. So the M2 "post-commit notification" leg is currently **dead code in production wiring**, and no in-tree command invokes `MoveCharacter` (F5 #4788 deferred) [VERIFIED].
4. **Consequence for the harness (D-07):** to characterize the M2 window the harness must wire an emitter into its `world.Service` — either `world.NewEventStoreAdapter` over a bus appender or the `busEventAppenderAdapter` + `RenderingPublisher` pattern the harness already uses for the CoreServer (`harness.go:607-610`). Then: pause the broker → call `MoveCharacter` (direct service call is fine; there is no command) → assert DB row moved + error carries `move_succeeded=true` + after unpause the move event never appears on the stream. That *proves the window exists* without needing a lost-event race.
5. **Consequence for the ADR:** M2 as written in F1 describes the *designed* behavior; the *current* behavior is stronger evidence of drift — the notification half of "event-driven CRUD" is not even wired for world mutations. The ADR should record both.

### Event emission coverage (Option-A input)

`internal/world/events.go` defines emit helpers for exactly four event families: `EmitMoveEvent` (:125), `EmitObjectCreateEvent` (:178), `EmitExamineEvent` (:223), `EmitObjectGiveEvent` (:258). **Create/Update/Delete of locations, exits, characters, objects, and all property writes emit nothing** (verified by reading every `world.Service` write method: only Move*/Examine* call emitters). Today's event stream could not reconstruct world state even if it were consumed [VERIFIED].

## Architecture Patterns

### System Architecture Diagram (target harness)

```
                         ┌──────────────────────────────────────────────┐
                         │  Test process (one Go test, //go:build       │
                         │  integration, gated by quarantine env)       │
                         │                                              │
  natstest.StartNATS ──▶ │  NATS JetStream container (nats:2-alpine)    │◀── docker pause/unpause
  (real broker)          │        ▲ nats://host:mappedPort ▲            │    (broker flap)
                         │        │                        │            │
                         │  ┌─────┴──────┐          ┌──────┴─────┐      │
                         │  │ Replica A  │          │ Replica B  │      │  replica restart =
                         │  │ CoreServer │          │ CoreServer │      │  rebuild replica B
                         │  │ eventbus   │          │ eventbus   │      │  (new Subsystem +
                         │  │ Subsystem  │          │ Subsystem  │      │   NewCoreServer over
                         │  │ ModeExternal│         │ ModeExternal│     │   the same pool/URL)
                         │  │ + dispatcher│         │ + dispatcher│     │
                         │  │ + world.Svc │         │ + world.Svc │     │
                         │  └─────┬──────┘          └──────┬─────┘      │
                         │        │      shared pgxpool    │            │
                         │        ▼                        ▼            │
  testutil.SharedPostgres│  Postgres container — ONE database           │
  + ONE FreshDatabase    │  (locations/characters/objects/exits,        │
                         │   sessions, plugin tables, events_audit)     │
                         └──────────────────────────────────────────────┘

  Drivers:  SessionA.SendCommand("describe here …")  ∥  SessionB.SendCommand("set here/name=…")
  Chaos:    pause broker → MoveCharacter (wired emitter) → unpause → inspect stream vs DB
  Reconnect: Session.DetachTransport() → ReattachTransport()
  Verdict:  read location/object rows back via shared pool; compare against both commands' claims
```

### Recommended Project Structure

```
internal/testsupport/integrationtest/
├── harness.go            # extend Start: option seams for external bus + shared DB (D-03 "extend", not fork)
├── options.go            # + WithExternalNATS(url string), WithSharedDatabase(connStr string) (names illustrative)
└── (existing files)      # session.go already has Detach/ReattachTransport

test/integration/resilience/          # new package (name planner's call)
├── resilience_suite_test.go          # TestWorldModelResilience entry (RegisterFailHandler/RunSpecs; stable -run name)
├── m12_lastwritewins_test.go         # concurrent-command corruption experiment
├── m2_dualwrite_test.go              # broker-flap window characterization
└── chaos_helpers_test.go             # pause/unpause, replica restart, barrier helpers

docs/adr/holomush-i4784-<slug>.md     # the ADR (+ README.md index row)
docs/reviews/arch-review/2026-07-11/… # optional: harness verdict write-up cross-linked from ADR/#4791
```

### Pattern 1: Per-replica external-mode eventbus over one broker

**What:** each replica gets its own production `eventbus.Subsystem` dialing the shared container — never a shared conn.
**When to use:** both replicas in this harness.

```go
// Source: internal/eventbus/config.go + test/integration/eventbus_external/external_boot_test.go:69-79 (verbatim pattern)
cfg := eventbus.Config{
    Mode:         eventbus.ModeExternal,
    URL:          env.URL,          // natstest.NATSEnv.URL
    StreamMaxAge: 24 * time.Hour,
    DupeWindow:   30 * time.Minute,
}.Defaults()                        // GameID defaults to "main" — both replicas share it (config.go:28,153)
bus := eventbus.NewSubsystem(cfg)
require.NoError(t, bus.Start(ctx))  // fail-closed on unreachable URL (EVENTBUS_EXTERNAL_CONNECT_FAILED)
```

Replica restart = `bus.Stop(ctx)` + drop the `CoreServer`, then rebuild both over the same `connStr`/URL. Note: BOTH replicas run `Provision` (default true) against one broker — the first creates the EVENTS stream, the second encounters an existing stream; verify create-or-update semantics in `internal/eventbus/subsystem.go` (`ensureStream`) during planning, or start replica B with `Provision: boolPtr(false)` (verify-only path proven in external_boot_test D-03 specs).

### Pattern 2: Broker flap via docker pause (port-stable)

```go
// Source: go doc testcontainers-go v0.43.0 (NewDockerClientWithOpts, Container.GetContainerID) — verified against pinned version
cli, err := testcontainers.NewDockerClientWithOpts(ctx)
require.NoError(t, err)
require.NoError(t, cli.ContainerPause(ctx, env.Container.GetContainerID()))
// … issue the write whose notification should be lost …
require.NoError(t, cli.ContainerUnpause(ctx, env.Container.GetContainerID()))
```

Pause freezes the broker with networking intact: JetStream publishes time out (the ~350ms emit-retry window exhausts), while both replicas' `nats.Conn`s survive and resume after unpause — no URL re-resolution needed. For the *restart* variant use `env.Container.Stop(ctx, &d)` / `Start(ctx)` and re-resolve `MappedPort` afterwards, asserting it unchanged before trusting client auto-reconnect (see Pitfall 4).

### Pattern 3: Deterministic-enough concurrency (start-gun + iterations)

```go
// Repo-idiomatic shape (no production sleeps; assert on observed loss across N rounds)
start := make(chan struct{})
var wg sync.WaitGroup
wg.Add(2)
go func() { defer wg.Done(); <-start; errA = sessA.SendCommand(ctx, `describe here Round `+n) }()
go func() { defer wg.Done(); <-start; errB = sessB.SendCommand(ctx, `set here/name=Round`+n) }()
close(start); wg.Wait()
// read the location row back via the shared pool; record whether either committed field is stale
```

Run N rounds; the verdict is "lost update reproduced in k/N rounds" (documented, reproducible) or "0/N with analysis of why it cannot occur". Both replicas' commands go through the real dispatcher → plugin → SetProperty → entity_mutator path.

### Pattern 4: Quarantine-env gating without a registry marker (D-05 option B)

```go
// quarantinetest.Enabled() is exported (quarantinetest.go:23). The bijection meta-test's marker regex is
// `quarantinetest\.Skip\(|quarantined:|@quarantine|Label\("quarantine"` (test/meta/quarantine_registry_test.go:31)
// — Enabled()-gating with a non-"quarantined:" message matches NONE of them, so no test/quarantine.yaml row is required.
func TestWorldModelResilience(t *testing.T) {
    if !quarantinetest.Enabled() {
        t.Skipf("resilience harness is nightly/opt-in: set %s=1 to run (#4791)", quarantinetest.EnvVar)
    }
    RegisterFailHandler(Fail)
    RunSpecs(t, "World-Model Resilience Suite")
}
```

### Anti-Patterns to Avoid

- **Calling `integrationtest.Start(t)` twice and calling it "two replicas"** — that yields two disjoint databases and two embedded buses (harness.go:322-323, 368). The whole point is one DB + one broker.
- **Sharing one `*nats.Conn` between replicas** — the documented gap `eventbustest` cannot express; each replica dials independently (natstest doc comment, nats.go:5-14; cache_invalidation_test.go precedent).
- **Grepping command output for "success"** — judge by returned error and by reading the DB row back.
- **Adding sleeps to production code or the write path to widen the race** — iterate instead.
- **Putting the suite in `test/integration/` without a gate** — `task test:int` compiles and runs `./...` under `-tags=integration`; an ungated two-replica chaos suite lands directly in the required `Integration Test` PR check (the exact thing D-05 forbids).
- **`quarantinetest.Skip(t, "holomush-q55b"-style token)` without a `test/quarantine.yaml` row** — `TestQuarantineRegistryBijection` fails the build (marker ↔ row must be identical sets).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Real NATS container lifecycle | custom docker exec / compose | `natstest.StartNATS` (retry + reclaim built in) | Handles half-started container reclamation (nats.go:77-100) |
| Per-test Postgres | new container per test | `testutil.SharedPostgres` + one `FreshDatabase` | Existing template-DB fast path; ConcurrentUp port-map flake (holomush-pqzv) shows container churn is the enemy |
| External-mode bus | raw `nats.Connect` + manual stream admin | `eventbus.NewSubsystem(ModeExternal)` | Production dial/provision path incl. fail-closed boot; the harness should exercise the real thing |
| Client reconnect | new transport plumbing | `Session.DetachTransport`/`ReattachTransport` | Already built, race-hardened (session.go:73-90) |
| Opt-in nightly gating | new CI lane | `HOLOMUSH_RUN_QUARANTINED` env + existing Quarantine Health nightly job | nightly-soak.yml:84-116 already runs `task test:int` with the env set — zero workflow changes for option B |
| Docker pause | shelling out to `docker pause` | `testcontainers.NewDockerClientWithOpts` → `ContainerPause/Unpause` | In-process, no PATH dependency [VERIFIED: go doc] |

**Key insight:** every substrate this harness needs already exists in-tree except (a) the two `StartOption` seams on `integrationtest.Start` and (b) the chaos helpers — the plan should be mostly *composition*, not construction.

## D-05 Opt-In Seam Analysis (planner must choose)

| Seam | Mechanics | Runs nightly? | Registry obligations | Risks |
|------|-----------|---------------|----------------------|-------|
| **(a) Quarantine marker + yaml row** | `quarantinetest.Skip(t, "holomush-i4791")` + `test/quarantine.yaml` row `{id, kind: go/ginkgo, bead: holomush-i4791, issue: 4791, since, reason}`. Post-beads token grammar `holomush-i<issue#>` is documented in the yaml header itself [VERIFIED: quarantine.yaml:8-9] | Yes — Quarantine Health job (`nightly-soak.yml:84-113`: `HOLOMUSH_RUN_QUARANTINED=1 task test:int`) | Bijection meta-test requires the row; `task quarantine:audit` **flags rows whose issue is closed** — when #4791 closes at phase end, the row becomes an audit violation, yet D-05 wants the harness standing for Phase 5. Mitigation: keep a dedicated standing issue open, or accept the semantic mismatch the CONTEXT NB already flags | Semantics: quarantine = "known-flaky with open issue"; this harness is neither |
| **(b) Reuse env var via `Enabled()`, no marker** (recommended) | Gate the suite entry with `if !quarantinetest.Enabled() { t.Skip(...) }` — message must NOT contain `quarantined:` and must not call `quarantinetest.Skip(` (regex at quarantine_registry_test.go:31 then never matches → no row needed) [VERIFIED: regex read] | Yes — same nightly job, zero workflow edits | None — deliberately outside the bijection | A reviewer may see it as sidestepping the registry; document the rationale in the suite doc comment (D-05 NB explicitly anticipates this decision). Coupled to the quarantine env var's meaning |
| **(c) Dedicated `HOLOMUSH_RUN_RESILIENCE=1` flag** | New env const in the resilience package; suite self-skips without it | Only after adding a step/env to `nightly-soak.yml` (workflow edit) | None | Cleanest semantics; costs a workflow change + a new lane to document |
| **(d) `soak` build tag** (context: existing precedent, not in D-05's named pair) | `//go:build integration && soak` like `test/integration/eventbus_e2e/soak_test.go`; excluded from `task test:int` compile entirely | Only via a new task target + nightly step (`task soak:eventbus` is pinned to `./test/integration/eventbus_e2e/...`, Taskfile.yaml:201-208) | None | Strongest isolation (not even compiled on the PR lane) but needs Taskfile + workflow additions; slightly outside D-05's named mechanism |

**Recommendation:** (b). It is literally D-05's named mechanism ("`HOLOMUSH_RUN_QUARANTINED=1` / `quarantinetest.Enabled()`"), runs in the existing nightly lane with no workflow changes, avoids the marker/registry semantics mismatch the CONTEXT NB warns about, and avoids the quarantine-audit conflict when #4791 closes. If the plan-checker or user prefers registry visibility, (a) with a standing "resilience harness" issue is the fallback.

## ADR Convention (verified)

The CONTEXT discretion note says "NNNN-<slug> (197 ADRs; sequential 4-digit prefix)" — **directory reality differs** [VERIFIED: ls docs/adr/]:

- 197 files total = **17** `NNNN-<slug>.md` (0001…0017, early era) + **179** `holomush-<token>-<slug>.md` (dominant, bd-era) + README.md.
- Recent-file template: HTML SPDX comment, `# Title`, `**Date:** / **Status:** Accepted / **Decision:** <token> / **Deciders:**`, `## Context` / `## Decision` / `## Consequences`; plus a row in README.md's index table (Title | Date | Status | bd decision).
- The beads tracker was retired 2026-07-09; no ADR has been created post-retirement, so there is no post-beads ADR precedent. The **post-beads token grammar** is established elsewhere: `test/quarantine.yaml` header — "NEW entries mint `holomush-i<issue-number>` (same grammar, no tracker needed)".
- Recent ADRs carry `<!-- adr-render: source=bd:...; do not edit manually; use \`/adr update ...\` -->` — that renderer is bd-backed; a new ADR should omit that comment (hand-authored is now correct).

**Recommendation:** `docs/adr/holomush-i4784-<slug>.md` (dominant convention + post-beads token grammar, token = the MODEL-01 issue), with a README.md index row whose "bd decision" cell carries `holomush-i4784` (or "—"). Alternative: `0018-<slug>.md` (next free sequential; 0017 is the max). Either satisfies the discretion note; do not invent a third pattern. Check `task lint` for any ADR-shape lint (`lint:adr` exists per plan-review learnings) before finalizing format.

## ADR Decision Inputs (verified, both options costed per D-01)

### Option (A) — build real event sourcing for world state: what it actually entails

1. **Event coverage from ~4 families to total:** today only move/examine/object_create/object_give emit (events.go). Real ES needs an event per mutation: Create/Update/Delete × {location, exit, character, object} + property set/delete + containment moves — ~15-20 new event types with payload schemas, plus the "event is the write" inversion of every `world.Service` write method (write = validate → append → project) [VERIFIED: emission surface].
2. **The emitter substrate is unwired:** production `world.Service` has no EventEmitter (setup/subsystem.go:66-77); `NewEventStoreAdapter` has zero production callers. (A) doesn't extend a working notification pipeline — it builds the pipeline first [VERIFIED].
3. **No rebuild substrate survives to salvage:** the F-series deletions (`EventWriter`, `cursor_lock.go`, `replay.go`, `internal/grpc/replay.go`) were all gRPC Subscribe *client-catch-up*, not state rebuild (jetstream design doc §91/§150/§599/§1451; F1 doc) [CITED: docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md via F1 archaeology].
4. **Retention conflict:** the JetStream EVENTS stream defaults to 30-day MaxAge (`internal/eventbus/config.go:26 defaultStreamMaxAge = 30*24h`); the durable tier is `events_audit`, which is unbounded today **and OPS-02 (Phase 6) exists precisely to bound it with the RetentionWorker**. An event-sourced source of truth requires permanent event retention or snapshot+compaction machinery — in direct tension with the same milestone's OPS-02. The ADR must resolve this if choosing (A) [VERIFIED: config.go; REQUIREMENTS.md OPS-02].
5. **Projection/rebuild machinery:** projection consumers writing the world tables (the existing audit projection writes only `events_audit`, never world tables — F1 doc), idempotent replay (dedup by event ULID exists as a convention: `Nats-Msg-Id`), per-entity ordering (JetStream per-stream seq is the ordering owner; world events would span subjects `location.<id>` / `character.<id>` — events.go:30-40), a genesis snapshot migrating current CRUD state into the log, and a rebuild CLI/verify job.
6. **Concurrency model:** ES replaces the version guard with append-order arbitration — but *command validation* still reads state (ABAC `checkAccess` + existence checks run pre-write in every service method, service.go); (A) needs an optimistic command/aggregate-version protocol anyway or it just moves the lost-update to the validation read. This deserves an explicit line in the ADR: (A) does not automatically dissolve M12; it changes where the guard lives.
7. **ARCH-04 coupling:** the parallel `core.Event`/`eventbus.Event` models must collapse "coordinated with the MODEL-01 outcome" (REQUIREMENTS.md ARCH-04, Phase 7). (A) makes the unified event model load-bearing for state; (B) keeps it a notification/audit concern.
8. **Effort class:** given 1-7, (A) is a multi-phase build (new write protocol + projections + retention/snapshot + migration + rebuild tooling); REQUIREMENTS.md "Out of Scope" already stipulates any large ES build is a follow-on milestone. The ADR can still *choose* (A) — Phase 5 would then implement the outbox/projection foundation slice named by the ADR — but the harness evidence should be weighed against this verified cost, not a hand-waved one.
9. **What (A) uniquely buys** (record fairly per D-01): derivable/replayable state, auditable reconstruction, time-travel debugging, and structural elimination of the M2 class (the event *is* the write, so there is no post-commit notification to lose).

### Option (B) — CRUD-canonical + optimistic concurrency + transactional outbox

1. **Version guard (M12, → Phase 5 MODEL-03):** add `version INTEGER NOT NULL DEFAULT 1` to locations/characters/objects/exits (migration rules: idempotent, paired down — `.claude/rules/database-migrations.md`); repos change to `UPDATE … SET …, version = version + 1 WHERE id = $1 AND version = $2`, `RowsAffected()==0` → conflict; use `RETURNING` (not a follow-up SELECT) to distinguish not-found from conflict deterministically [CITED: .claude/rules/references/plan-review-learnings.md "Repository differentiator-SELECT race"]. In-schema precedent exists: `access_policies.version INTEGER NOT NULL DEFAULT 1` + `access_policy_versions` (baseline.up.sql:265-282) [VERIFIED].
2. **Read-modify-write callers:** `entity_mutator.go` (name/description × location/object/character) and `UpdateCharacterDescription` must carry the read version into the guarded update (the `Location`/`Object`/`Character` structs gain a `Version` field); conflict → retry loop or user-visible "concurrent edit" error — the ADR should name which.
3. **Transactional outbox (M2, → Phase 5 MODEL-04):** no outbox exists (rg: zero production hits). Shape: an `outbox` table written in the SAME transaction as the entity write (the `Transactor.InTransaction` seam already exists and is used by `DeleteLocation`, service.go:256-289 [VERIFIED]); a relay polls/claims rows and publishes to JetStream with `Nats-Msg-Id` = event ULID (dedup via existing `DupeWindow`, default 30min); cleanup via the RetentionWorker sibling (OPS-02 machinery). Note: today most world writes emit *nothing*, so MODEL-04's actual work is "wire emissions through the outbox where notifications are wanted" — the ADR should scope which events the outbox carries.
4. **What (B) forgoes:** state replay/rebuild; the "event sourcing" doc principle is downgraded everywhere (MODEL-02's six sites) to "event-driven with an append-only audit log".

### The intent question (D-02)

The F1 archaeology already establishes drift-by-default with git evidence; the ADR records it as the finding. Intent "cannot be proven from git" (F1 doc) — the ADR's intent section should cite the archaeology and the user's own testimony from discuss-phase rather than promise further excavation.

## Common Pitfalls

### Pitfall 1: Harness suite leaks into the gating PR lane
**What goes wrong:** `task test:int` runs `./...` with `-tags=integration` (Taskfile.yaml:188) — a new `test/integration/resilience` package executes in the required `Integration Test` CI check.
**How to avoid:** the `Enabled()` gate (or soak tag) must be in the suite's `TestXxx` entry, verified by running `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` WITHOUT the env and confirming skip.
**Warning signs:** resilience specs appearing in a PR's Integration Test log.

### Pitfall 2: "Two Start() calls" fake replicas
**What goes wrong:** each `integrationtest.Start` creates its own fresh DB (`FreshDatabase` per call) and its own embedded bus — the "replicas" share nothing; every race probe trivially passes.
**How to avoid:** new options must inject one connStr and per-replica ModeExternal buses; assert in the harness that both replicas resolve the same `GameID` and the same DB.

### Pitfall 3: Replica-2 boot re-seeds / re-migrates
**What goes wrong:** `Start` seeds a guest location, sets KEK env vars, and (under `WithInTreePlugins`) runs plugin storage setup per call; a second replica on the shared DB re-runs these. Guest locations are per-replica IDs (harmless but confusing); plugin migrations must be idempotent (verify); `t.Setenv` twice with identical values is safe within one test.
**How to avoid:** the shared-DB option should make seeding replica-1-only where duplication matters, and the M12 experiment must reference entities by ULID created once and used by both replicas.

### Pitfall 4: Container Stop→Start changes the mapped port
**What goes wrong:** host ports are allocated at container start ("the mapping process occurs during container startup" — testcontainers docs); NATS clients auto-reconnect to the ORIGINAL URL, so a port change strands both replicas and the test misreads it as a resilience failure.
**How to avoid:** primary flap = docker pause/unpause (networking untouched). For the restart experiment, re-resolve `MappedPort` after `Start(ctx)` and `require.Equal` against the old port before asserting reconnection behavior; if it drifts on the CI runner, fall back to pause-only and document.
**Note:** JetStream file store (`-sd /data`) persists across Stop/Start of the same container (fs retained); state is lost only on Terminate.

### Pitfall 5: Pause-based flap and slow client detection
**What goes wrong:** `dialExternal` (natsdial.go:42-64) sets no custom ping/reconnect options → nats.go defaults apply (≈2-minute ping interval [ASSUMED: nats.go library defaults from training — verify in plan against the pinned nats.go version]); during a pause the client may not even notice disconnection — which is fine for M2 (JetStream *publish* acks time out regardless) but means "reconnect observed" assertions must key on publish/subscribe behavior, not connection-state callbacks.
**How to avoid:** assert on outcomes (publish error, event absent/present on stream, `AwaitStreamLastSeq`-style polling) rather than connection state.

### Pitfall 6: Reading the M12 verdict through caches
**What goes wrong:** asserting via session state or subscriber frames measures delivery, not committed state.
**How to avoid:** the corruption read-back goes straight to the shared `pgxpool` (SELECT the row), the same way harness escape hatches do (harness.go:901-936 precedent).

### Pitfall 7: Suite runtime blowing the go-test timeout
**What goes wrong:** container start (≤2min wait deadline in natstest) + N race iterations + flap durations can approach gotestsum's default 10m package timeout under nightly load.
**How to avoid:** budget: one shared NATS container per suite (`BeforeAll`, like cache_invalidation_test.go), short pauses (≤10s), N iterations bounded (≤200), and an explicit `-timeout` via the task invocation if needed. Annotate long steps in the plan with `run_in_background` guidance for executors.

### Pitfall 8: Quarantine idiom misuse
**What goes wrong:** using `quarantinetest.Skip(t, token)` or a `Skip("quarantined: …")` message without a `test/quarantine.yaml` row fails `TestQuarantineRegistryBijection`; conversely adding a row without a marker also fails.
**How to avoid:** pick seam (a) or (b) from the D-05 analysis and follow it exactly; if (a), mint `holomush-i4791` and add the row in the same commit.

## Code Examples

(See Architecture Patterns 1-4 above — all sourced from in-repo files or `go doc` against pinned versions; none invented.)

Additional verified signatures the planner may cite without re-derivation:

```go
// internal/testsupport/natstest/nats.go
func StartNATS(ctx context.Context) (*NATSEnv, error)      // :77
func (e *NATSEnv) Conn(t testing.TB) *nats.Conn            // :60 — independent conn per call
func (e *NATSEnv) Terminate(ctx context.Context) error     // :47

// internal/testsupport/integrationtest (build tag: integration)
func Start(t *testing.T, opts ...StartOption) *Server      // harness.go:291
type StartOption func(*startConfig)                        // harness.go:200
func WithInTreePlugins() StartOption                       // plugins.go:163
func WithPolicyEngine(eng types.AccessPolicyEngine) StartOption // harness.go:223
// Session: SendCommand(ctx, cmd) error; WaitForEvent(ctx, type) *corev1.EventFrame;
// DetachTransport / ReattachTransport (session.go)

// internal/eventbus
func NewSubsystem(cfg Config) *Subsystem                   // subsystem.go:64
func (s *Subsystem) GameID() string                        // subsystem.go:412 — cfg-driven, default "main"

// internal/testsupport/quarantinetest
const EnvVar = "HOLOMUSH_RUN_QUARANTINED"                  // quarantinetest.go:20
func Enabled() bool                                        // :23
func Skip(t *testing.T, bead string)                       // :28 — ONLY with a quarantine.yaml row

// internal/world
func NewEventStoreAdapter(store EventAppender) *EventStoreAdapter // event_store_adapter.go:27
func (s *Service) MoveCharacter(ctx, subjectID string, characterID, toLocationID ulid.ULID) error // service.go:773
func (s *Service) UpdateLocation(ctx, subjectID string, loc *Location) error // service.go:230
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| PG events table + `EventWriter` + client replay | JetStream event bus; `Append` goes straight to the bus (F7 cutover) | 2026-04 (jetstream design) | No world-state rebuild path exists or ever existed; "replay" that was deleted was client catch-up [CITED: F1 doc] |
| beads (`bd`) tracker for ADR decision ids | GitHub Issues; post-beads token grammar `holomush-i<issue#>` (established by quarantine.yaml) | 2026-07-09 | New ADR cannot mint a bd id; use `holomush-i4784` or sequential `0018` |
| Shared embedded `eventbustest` for multi-replica proofs | `natstest` real container + per-replica conns (CLUSTER-01..04) | 2026-06 era | The exact substrate D-03 mandates; precedents in `test/integration/{eventbus_external,cluster,crypto}` |

**Deprecated/outdated:** colon-style subjects (eradicated); `docs/roadmap.md` (retired); bd tracker (retired — historical ids resolve via `.planning/archive/beads/`).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | nats.go client defaults (MaxReconnects≈60, ReconnectWait≈2s, PingInterval≈2m) apply to `dialExternal` since it passes no reconnect options | Pitfall 5 | Flap assertions keyed to reconnect timing could mis-tune; mitigated by asserting on publish/stream outcomes instead. Verify against the pinned nats.go version during planning |
| A2 | Docker may reassign a randomly-published host port across Stop→Start of the same container (behavior varies by daemon/config) | Pitfall 4, Alternatives | If ports are actually stable on the CI runner, Stop/Start restart fidelity is cheaper than assumed; the re-resolve-and-assert mitigation covers both outcomes |
| A3 | `eventbus.Subsystem` stream provisioning tolerates an already-existing EVENTS stream when two replicas both boot with Provision default | Pattern 1 | Replica-2 boot could fail; mitigation already named (replica-2 uses `Provision: false` verify-only path, proven in external_boot_test) — confirm `ensureStream` semantics in plan |
| A4 | Plugin storage setup (per-plugin postgres migrations) is idempotent when a second plugin subsystem starts on the same DB | Pitfall 3 | Replica-2 `WithInTreePlugins` boot fails; fallback: replica-2 without plugins, driving its writes via `world.Service` directly (fidelity tradeoff documented) |

## Open Questions

1. **Does the ADR (A)-vs-(B) verdict need harness data first, or can they proceed in parallel?**
   - What we know: success criteria order the harness verdict before the ADR ("grounded in F1 AND the resilience evidence").
   - What's unclear: whether ADR drafting (context/options/costing) can start in parallel with harness execution.
   - Recommendation: plan the ADR as the final plan/wave with the verdict as an input; draft its Context/Options sections earlier if wave structure allows.
2. **Replica-2 plugin fidelity (A4):** commands on both replicas vs commands on A + direct `world.Service` on B.
   - Recommendation: attempt dual `WithInTreePlugins` first (it exercises the real dispatcher on both sides); fall back if resource contention reproduces holomush-pqzv-style flakes. Either satisfies D-06 because the race lives in entity_mutator/repo SQL.
3. **Where does the harness verdict document live?**
   - What we know: success criterion #2 wants a "documented, reproducible verdict"; #4791 wants the resilience pass recorded.
   - Recommendation: a short findings doc under `docs/reviews/arch-review/2026-07-11/verification/` (sibling of the F1 doc) or embedded as the ADR's evidence section + a #4791 issue comment; planner picks one canonical location and cross-links.
4. **`task quarantine:audit` interaction if seam (a) is chosen** — resolved only by keeping an open standing issue; prefer seam (b).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker daemon | testcontainers (PG + NATS) | ✓ (test:int is the repo's standard tier; Testcontainers Cloud in CI) | — | none — blocks harness execution |
| Go toolchain + task | build/test | ✓ (repo standard) | go.mod toolchain | — |
| Built binary plugins | `WithInTreePlugins` (command dispatch) | ✓ via `task plugin:build:host` (auto inside `task test:int`, Taskfile.yaml:181) | — | replica without plugins (fidelity tradeoff) |
| `nats:2-alpine` image | natstest | ✓ (pulled on demand; already used by 3 suites) | — | — |
| GitHub (`gh`) | ADR cross-link to #4784/#4791, issue comments | ✓ | — | manual linking |

**Missing dependencies with no fallback:** none identified.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go `testing` + testify (unit); Ginkgo v2/Gomega (`//go:build integration`) for the harness suite |
| Config file | Taskfile.yaml (`test`, `test:int` targets); no separate test config |
| Quick run command | `task test -- ./internal/testsupport/integrationtest/` (unit-tier compile check: `go vet`-level only — integration files need the tag) |
| Full suite command | `task test:int` (gating tier; resilience suite self-skips) / `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` (opt-in harness run) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| OPS-05 | M12 lost update reproduced (or proven impossible) under two replicas | integration (opt-in) | `HOLOMUSH_RUN_QUARANTINED=1 task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` | ❌ Wave 0 (new suite) |
| OPS-05 | M2 window characterized on broker flap (DB committed, event lost, `move_succeeded=true`) | integration (opt-in) | same suite, m2 spec | ❌ Wave 0 |
| OPS-05 | Replica restart + client reconnect exercised | integration (opt-in) | same suite, chaos specs | ❌ Wave 0 |
| OPS-05 | Suite self-skips on the gating lane | integration (gating) | `task test:int -- -run TestWorldModelResilience ./test/integration/resilience/` (env UNSET) → skip observed by exit code + gotestsum skip count | ❌ Wave 0 |
| MODEL-01 | ADR exists, names the Phase 5 mechanism, records intent, is indexed | manual-only (doc artifact) + `task lint` (markdown/ADR lint) | `task lint` | ❌ (ADR file) |

Manual-only justification for MODEL-01: an architecture decision's correctness is human judgment; lint verifies form only. `checkpoint:human-verify` appropriate at the ADR task.

### Sampling Rate

- **Per task commit:** `task test -- ./<touched-package>` + `task lint` (repo MUST); harness tasks additionally compile-check with `go build -tags=integration ./test/integration/resilience/ ./internal/testsupport/integrationtest/` via `task test:int -- -run NoSuchTest ./test/integration/resilience/` or a scoped test:int run.
- **Per wave merge:** `task test:int` (gating tier — proves the suite self-skips and nothing else broke; `task test` does NOT compile integration files, per CLAUDE.md refactor rule).
- **Phase gate:** one full opt-in harness run (`HOLOMUSH_RUN_QUARANTINED=1 …`) producing the recorded verdict; `task pr-prep` green before ship.

### Wave 0 Gaps

- [ ] `internal/testsupport/integrationtest/` — external-bus + shared-DB `StartOption`s (covers OPS-05 substrate)
- [ ] `test/integration/resilience/` suite skeleton (entry + gating skip + shared container `BeforeAll`)
- [ ] Harness-side `world.Service` event-emitter wiring (M2 observability)
- [ ] Framework install: none (all pinned)

## Security Domain

This phase ships test-support code and a decision document — no new production auth/input/crypto surfaces.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (harness reuses existing auth paths; `ConnectAuthed` test helpers) | — |
| V3 Session Management | no (exercises, does not modify, session store) | — |
| V4 Access Control | tangential | Harness default allow-all ABAC engine is TEST-ONLY (`allowAllPolicyEngine`, harness.go:373); depguard prevents production import of all testsupport packages — preserve that boundary |
| V5 Input Validation | no | Commands go through the existing dispatcher/validation |
| V6 Cryptography | no | KEK provisioning in `Start` is per-test ephemeral (harness.go:296-319); do not touch `internal/eventbus/crypto/**` (would trip the crypto-reviewer gate) |

### Known Threat Patterns for this phase

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Test-only allow-all engine leaking into production wiring | Elevation of privilege | depguard (existing) + keep new options inside `//go:build integration` files |
| ADR mis-recording the security posture of the chosen mechanism (e.g. outbox relay bypassing ABAC) | Tampering | ADR names where authorization sits in the Phase 5 mechanism (checkAccess remains pre-write in both options) |

Note: no files under `internal/eventbus/crypto/`, `internal/access/`, or plugin `crypto.emits` should change in this phase — if a plan task touches them, the crypto/abac reviewer gates fire (CLAUDE.md Pre-Push Review Gates).

## Sources

### Primary (HIGH confidence — direct reads in this worktree)
- `internal/world/{service.go, events.go, event_store_adapter.go, mutator.go, setup/subsystem.go}`; `internal/world/postgres/{character,location,object,exit}_repo.go`
- `internal/property/entity_mutator.go`; `internal/plugin/hostcap/{property.go, world.go}`; `plugins/core-objects/{plugin.yaml, main.lua}`; `plugins/core-building/plugin.yaml`
- `internal/testsupport/{integrationtest/{harness.go, options.go, plugins.go, session.go}, natstest/nats.go, quarantinetest/quarantinetest.go}`
- `internal/eventbus/{subsystem.go, config.go, natsdial.go, eventbustest/embedded.go}`
- `test/integration/{eventbus_external/external_boot_test.go, crypto/cache_invalidation_test.go, eventbus_e2e/soak_test.go}`; `internal/cluster/clustertest/external.go`
- `test/quarantine.yaml`; `test/meta/quarantine_registry_test.go`; `Taskfile.yaml` (test:int:165-189, soak:201-208); `.github/workflows/nightly-soak.yml`
- `internal/store/migrations/000001_baseline.up.sql`; `docs/adr/` listing + `README.md` + recent ADR template
- `docs/reviews/arch-review/2026-07-11/verification/f1-eventsourcing-why.md`; `.planning/{REQUIREMENTS.md, STATE.md}`; `04-CONTEXT.md`
- `go doc github.com/testcontainers/testcontainers-go` against pinned v0.43.0 (Stop/Start/GetContainerID/NewDockerClientWithOpts)

### Secondary (MEDIUM confidence)
- Context7 `/testcontainers/testcontainers-go` docs — dynamic host-port allocation at container start (networking.md) [CITED]

### Tertiary (LOW confidence)
- nats.go client default reconnect/ping parameters (training knowledge) — tagged A1 [ASSUMED]

## Metadata

**Confidence breakdown:**
- M12/M2 mechanisms & command pair: HIGH — every link in the chain read and cited
- Harness substrate & extension seams: HIGH — constructors/options verified; three in-tree precedents
- D-05 seam analysis: HIGH — regex, yaml grammar, nightly workflow all read
- ADR convention: HIGH — directory enumerated; post-beads grammar cited
- Flap mechanics: MEDIUM-HIGH — API verified via go doc; port-stability across Stop/Start deliberately left as A2 with mitigation
- Option A/B costing inputs: HIGH for facts; the weighing itself is the ADR's job (D-01)

**Research date:** 2026-07-11
**Valid until:** ~2026-08-10 (stable in-repo substrate; re-verify only if Phase 4 slips past other phases touching `internal/world` or the test harness)
