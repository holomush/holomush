# Phase 7: Event-Model & Bootstrap Decomposition - Context

**Gathered:** 2026-07-15
**Status:** Ready for planning

<domain>
## Phase Boundary

Three **behavior-preserving** structural changes, all grounded in the 2026-07-11 L7
architecture review (`docs/reviews/arch-review/2026-07-11/findings/d1-architecture.md`):

- **ARCH-04** — collapse the parallel `core.Event` / `eventbus.Event` models to a single
  representation, coordinated with the MODEL-01 outcome.
- **ARCH-03** — migrate process bootstrap onto `lifecycle.Orchestrator`, unifying
  subsystem start/stop ordering.
- **ARCH-05** — remove the remaining gateway-boundary import violations so
  `internal/web` / `internal/telnet` hold only protocol-translation dependencies.

**Fixed by prior phases (NOT open for planning to re-decide):**

- **MODEL-01 ADR (Phase 4, Option B — accepted, panel-ratified):** world state is
  CRUD-canonical. Real event sourcing / state-rebuild-from-replay is **permanently
  forgone**. The collapsed Event is an **audit/notification wire type**, NOT a
  state-rebuild source.
- **Phase 5's versioned taxonomy registry** (`internal/world/outbox/taxonomy.go`,
  `AppSchemaVersion = 1`) is the **explicitly designated ARCH-04 input** — the collapsed
  model **adopts** these schemas rather than re-inventing them
  (`model-01-consensus-onepager.md` §Schema governance).
- **Phase 5 already fixed the collapse direction:** `internal/world/outbox/wire.go`
  converts `wmodel.Envelope` → `eventbus.Event`. `eventbus.Event` is the survivor.
- Ordering is owned by JetStream's per-stream `uint64` seq; ULID is identity/dedup ONLY.

**Out of scope:**

- ARCH-01 / ARCH-02 god-object decomposition (`CoreServer`, `plugin/manager`) — Phase 8.
- MEDIUM-4's broader plugin ⇄ eventbus ⇄ grpc bidirectional-coupling unwind — Phase 8
  territory. Phase 7 must not *deepen* it (see D-01), but does not resolve it.
- `cmd/holomush`'s `coreOnlyFiles` allowlist (~30 entries) — governs `cmd/holomush`,
  not `internal/web` / `internal/telnet`; outside ARCH-05's text (see Deferred).
- Exposing `Seq` to plugins (see D-08 — forbidden by `internal/eventbus/types.go:143`).

</domain>

<decisions>
## Implementation Decisions

### ARCH-04 — the unified Event representation

- **D-01:** **`eventbus.Event` wins in place** — `core.Event`, `core.NewEvent`, and
  `core.EventAppender` are **deleted**. But do **NOT** achieve this by having
  `internal/command` import `internal/eventbus`: that would deepen the exact
  plugin⇄eventbus⇄grpc coupling arch-review MEDIUM-4 wants unwound. Instead, `command`
  **sheds its event dependency entirely** (D-02).
  - Grounding: `eventbus.Event` keeps an **unexported `auditRow`** field with a
    package-internal accessor (`AuditRowOf`, `audit_row_access.go`) consumed by
    `history.PluginDowngradeFence` for INV-CRYPTO-42/50. Moving the type out of
    `internal/eventbus` breaks that package-private seam — this is why a "neutral leaf
    package for the whole Event struct" was **rejected**.

- **D-02:** **One broadcast port, two callers.** `internal/command`'s ONLY event
  construction is `Services.BroadcastSystemMessage` (`internal/command/types.go:622-641`)
  — an intent-shaped operation (`stream`, `message`), not one needing an Event type.
  - **One concrete builder** constructs the broadcast `eventbus.Event`
    (`EventTypeSystem` + system actor + `{"message": ...}` payload) and publishes. The
    payload shape MUST exist in exactly **one** place.
  - **Consumer-defined interfaces — no shared port package.** `internal/command` declares
    its own one-method interface; `hostcap.SessionAdmin` **already** declares
    `BroadcastSystemMessage(ctx, message)` (`internal/plugin/hostcap/capabilities.go:58-60`).
    Both are satisfied by the one builder; `hostcap.systemBroadcaster` becomes a thin
    adapter pinning `subject = core.SystemBroadcastSubject`.
  - This **removes a live duplicate**: `hostcap/system_broadcaster.go:45-48`'s doc comment
    admits it "Mirrors the `{"message": ...}` payload shape of
    `command.Services.BroadcastSystemMessage`" — a drift contract enforced by nothing.
  - `Services.Events()` (`types.go:561`) has **no production caller outside its own
    package** (only `types_test.go`; `handlers/shutdown.go:43` calls
    `BroadcastSystemMessage`). It goes away with the port.

- **D-03:** **`core.Engine` MOVES out of `internal/core`** — it does NOT get a narrow port.
  - **Why the move, not a port:** Engine has **no logic to protect**. It is one field
    (`store EventAppender`) and three methods that marshal a payload → build an `Event` →
    `Append` (`internal/core/engine.go:65-107`, `engine_end_session.go`). A port would
    leave an empty shell delegating to the thing that replaced it.
  - **Why it can't stay:** `internal/eventbus` **already imports** `internal/core`
    (`types.go:13`, `rendering_publisher.go:12` — for `VerbRegistry` + `NewULID`). If
    `core.Engine` named `eventbus.Event`, that is `core → eventbus → core`: **an import
    cycle Go forbids.** "eventbus.Event wins in place" therefore CANNOT mean "core names
    eventbus.Event".
  - Inverting the dep (moving `NewULID`/`VerbRegistry` out of core) was **rejected** —
    `NewULID` is load-bearing for event-ID monotonicity and the blast radius lands
    outside Phase 7's three requirements.

- **D-04:** **New package `internal/presence`; type renamed to `presence.Emitter`, WITH
  method renames** (`HandleConnect`/`HandleDisconnect` → `Arrived`/`Departed` or
  `EmitArrive`/`EmitLeave`; the planner picks the final verb set).
  - `presence` matches the repo's own terminology rule ("presence — active sessions at a
    location"), which is exactly what `arrive`/`leave` describe.
  - "Engine" is **fiction** (its doc says "Engine is the core game engine"; it is an event
    emitter) and **collides** with `internal/access/policy`'s `Engine`/`NewEngine`
    (`policy/engine.go:71`). The move already touches every call site
    (`internal/grpc` ×6, `cmd/holomush` ×2 reaper), so renaming is ~free — this is the one
    moment it costs nothing.
  - `ArrivePayload` / `LeavePayload` travel with it. `presence` imports eventbus; imported
    by `grpc` + `cmd/holomush`; **no cycle** (eventbus imports neither).

- **D-05:** **Event-type vocabulary lives in a dependency-free leaf** that BOTH
  `internal/eventbus` AND the gateway may import.
  - **This resolves a collision neither ARCH-04 nor ARCH-05 mentions:**
    `internal/telnet/gateway_handler.go:1247-1249` renders on `core.EventTypeArrive` /
    `core.EventTypeLeave`. If the vocabulary followed the Event into `internal/eventbus`,
    telnet would have to import `internal/eventbus` — **already on the `forbidden` list**
    (`cmd/holomush/gateway_imports_test.go:107`). **Fixing ARCH-04 naïvely makes ARCH-05
    strictly worse.** The struct collapses into `eventbus.Event`; only the vocabulary
    stays low.

- **D-06:** The **three duplicate actor bridges** collapse with `core.Actor`:
  `cmd/holomush/sub_grpc.go:865` (`coreToBusActor`),
  `internal/plugin/event_emitter.go:306` (`coreActorToEventbusActor`), and
  `internal/testsupport/integrationtest/harness.go:1364` (`harnessCoreToBusActor` — a
  hand-copy of production's, per its own comment). Same for the harness's
  `busEventAppenderAdapter` (`harness.go:1328`), which mirrors production's
  `busEventAppender` by hand.

- **D-07:** **Take the host-internal Seq fix** — this is a **live correctness bug**, not
  tidiness.
  - **The bug:** the eventbus history layer has full seq-cursor support
    (`HistoryQuery.AfterSeq`/`BeforeSeq`; tier selection compares cursor seq vs the
    stream's `FirstSeq` at `history/tier.go:506-518`; the hot tier starts on
    `DeliverByStartSequencePolicy`). The **plugin replay path passes neither** —
    `busHistoryReaderAdapter.ReplayTail` (`cmd/holomush/sub_grpc.go:916-968`) sets only
    `Subject`/`Direction`/`PageSize`/`NotBefore`/`BeforeID`. So plugin history pagination
    is **ULID-ordered**, in a system whose own code states
    (`history/hot_jetstream.go:427`): *"Ordering is owned by JetStream per-stream
    sequence, not ULID lex order — concurrent publishers produce events whose ULIDs do
    NOT match stream sequence."* On a busy stream, plugin history pages can **skip or
    repeat** events.
  - **The cause is the ARCH-04 duplication:** `QueryHistory` returns `eventbus.Event`
    **with** `Seq` → `busEventToCoreEvent` **destroys** it (core.Event has no such field)
    → `encodeHostEventCursor` is forced to hardcode `Seq: 0`
    (`internal/plugin/hostcap/servers.go:1279-1296`, with a comment admitting it) → the
    decode side has no seq to pass back.
  - **The fix:** thread the real Seq — `encodeHostEventCursor(e.Seq, e.ID)` — and pass
    `BeforeSeq` into `ReplayTail`. `cursor.HostCursor` **already has** a `Seq` field; it
    is simply always zero.
  - **Cost:** `plugins.HistoryReader.ReplayTail`'s signature grows a seq param across its
    4 impls (`cmd/holomush/sub_grpc.go:916`, `internal/plugin/hostfunc/streamauth.go:49`,
    `internal/core/coretest/store_memory.go:98`,
    `internal/testsupport/integrationtest/harness.go:1522`). Param-vs-cursor-struct shape
    is the planner's call.
  - **This IS a behavior change** (pagination becomes seq-correct) and MUST ship with a
    regression test pinning the **concurrent-publisher skip/repeat** case.

- **D-08:** **Do NOT expose `Seq` to plugins.** `hostv1.Event`
  (`api/proto/holomush/plugin/host/v1/stream.proto:45-67`) has 8 fields and **no seq**;
  `cursor` is documented there as an **opaque** token, encoded by `internal/eventbus/cursor`
  — an `internal/` package an external plugin cannot import. Plugins therefore see **no
  seq today**, hardcoded or otherwise, so D-07 is a **zero-contract-change** fix.
  Adding a plugin-facing `seq` field would:
  - **violate** `internal/eventbus/types.go:143` — *"Seq … **Host-internal — never
    serialized in any public proto envelope.**"*; and
  - hand plugins a number whose meaning silently changes across tiers — `history/tier.go:713-718`:
    a cold event's `js_seq` is *an aged-out JS seq no longer in JS retention*; seq is only
    meaningful within a tier's sequence space.
  If a future plugin genuinely needs independent ordering, that requires an explicit
  invariant amendment + ADR, not a silent override.

### ARCH-03 — bootstrap on the Orchestrator

- **D-09:** **Full two-phase: zero eager starts.** Constructors take **handles/providers**,
  not resolved live values; resolution happens at `Start` time.
  - **The single root cause of all 5 eager starts** (`cmd/holomush/core.go` — db:287,
    eventbus:462, abac:797, auth:800, crypto:977) is a **constructor or accessor demanding
    a live resource**, backed by *"called before Start()"* panic guards:
    - db (`:281`): "must start before TLS cert generation because the gameID (from
      `InitGameID`) is embedded in the CA certificate on first boot."
    - eventbus (`:455`): "`cluster.NewSubsystem` requires a non-nil `*nats.Conn` **at
      construction time**, and `eventBusSub.Conn()` returns nil prior to Start."
    - abac/auth (`:785-797`): so admin-handler wiring "can call `authSub.Hasher()` /
      `abacSub.Resolver()` without hitting the 'called before Start()' panic guards."
  - **This has already caused a production incident.** Per `core.go:790-796`, Phase 5
    sub-epic D's E2E (`holomush-jxo8.6.23`/T25) found "the production admin-handler wiring
    **panics on every boot** when KEK is available" — and the remedy was to add *another*
    pre-start. Each pre-start moves start-ordering authority out of the orchestrator;
    MEDIUM-11 is the downstream symptom.
  - **Fixes, all expressible as existing `DependsOn` edges:** `cluster.NewSubsystem` takes
    the eventBus handle and resolves `Conn()` at Start (`DependsOn(EventBus)`); TLS becomes
    a real subsystem (`DependsOn(Database)`) reading gameID at its own Start; admin-handler
    wiring takes providers (`DependsOn(Auth, ABAC)`).
  - **Consequences:** all 5 pre-starts die; `StartAll` regains sole ownership of ordering;
    the `eventBusOwnedByOrchestrator` flag juggling (`core.go:494-500`) disappears.

- **D-10:** **NO countdown latch / bespoke multi-phase gate.** `topoSort` + `DependsOn`
  (`internal/lifecycle/orchestrator.go:44-149`) **already is** the ordered multi-phase
  boot; the gap is purely constructor-side. A latch would be a **second ordering authority
  competing with topoSort** — precisely the MEDIUM-11 failure mode (comments + slice order
  asserting an order the graph didn't enforce, graph silently wins). If a readiness gate is
  ever wanted, **reuse the existing `lifecycle.ReadinessRegistry`**
  (`internal/lifecycle/registry.go` — `AllReady`/`WaitReady`/`HealthReporter`, already used
  at core.go step 9), do not invent one.

- **D-11:** **Split `lifecycle.Subsystem.Start` into two phases (Prepare/Activate)** so the
  **structure enforces** acquire-before-serve rather than per-edge discipline.
  - **Motivating evidence:** `grpcSubsystem.DependsOn()` (`cmd/holomush/sub_grpc.go:171-178`)
    returns `[Bootstrap, Sessions, Auth, EventBus]` — it **excludes `AuditProjection`**, so
    gRPC can begin **serving before the audit projection is up**. Today `Start()` conflates
    *acquire resources* and *begin serving*. This is exactly MEDIUM-11's stated latent risk:
    *"a future change that makes a subsystem's Start begin serving/persisting (not just
    wiring) could rely on a guarantee the graph never made."*
  - Rationale (user, explicit): *"let the code/structure enforce the requirement"* — a
    `DependsOn` edge can be forgotten by the next person adding a subsystem; an interface
    that cannot express "serve before acquire" cannot be.
  - Accepted cost: touches the interface + all ~18 subsystems, on top of D-09.

- **D-12:** **Two waves, each independently verifiable** (the mitigation for D-09+D-11's
  combined size):
  - **Wave A:** constructors take handles → all 5 eager starts die, `StartAll` owns
    ordering, TLS becomes a real subsystem. Provable alone (boot works; no pre-starts).
  - **Wave B:** the Prepare/Activate split on top.
  - Each wave MUST land green and be reviewable alone, so a boot regression bisects to the
    right wave and Wave A remains a complete win if B proves hairy.

- **D-13:** **The planner MUST settle these two — do NOT leave them to the implementer:**
  1. **Two-phase rollback semantics.** `StopAll` today only stops subsystems in
     `startOrder` (`orchestrator.go:81-96`). With Prepare/Activate, "Activate fails after
     N subsystems prepared" is a **new** question — deactivate the activated, then stop the
     prepared? This is a design decision.
  2. **The `Start` MUST-be-idempotent contract** (`internal/lifecycle/subsystem.go:51`)
     **exists to support the pre-start hack** — `StartAll` re-invokes `Start` on
     already-started subsystems. Once eager starts die it may be vestigial. Decide
     deliberately whether to keep it as defense-in-depth or retire it; do not inherit it by
     accident.

- **D-14:** **Ride-along arch-review findings (ALL four selected — in scope):**
  - **MEDIUM-11** — `productionSubsystems`' comment (`core.go:1458`) asserts "cryptoChainVerifierSub
    runs before EventBus", but `StartAll` uses `topoSort` order and `eventbus.Subsystem.DependsOn()`
    returns nil → in-degree 0 → **seeded first**, the opposite of the comment. Fix: encode
    the intent as a **real edge**, or delete the comment + pin the actual topo sequence in a
    test. Also add gRPC's missing `AuditProjection` edge.
  - **LOW-7** — `defer orch.StopAll(context.Background())` (`core.go:1105`) has **no
    deadline**; a hung plugin subprocess or NATS drain means the process never self-exits
    (mitigated only externally by systemd/k8s). Fix: pass the existing 5s `shutdownCtx` and
    have Stops honor `ctx.Done()`.
  - **LOW-8** — `productionSubsystems` (`core.go:1462`) takes 15 same-typed positional
    `lifecycle.Subsystem` params; a mis-order compiles silently (defused today only because
    `topoSort` re-derives real order). Already grew 12→15. Fix: per-subsystem `Register`
    calls or a named struct. Falls out naturally once D-09 rewrites this wiring.
  - **Phantom `SubsystemTLS`** — the ID is declared (`internal/lifecycle/subsystem.go:18`)
    and stringer-generated but **never registered**; it appears only in the const block, the
    stringer, and `cmd/holomush/core_subsystems_test.go:82`. `ensureTLSCerts`
    (`core.go:1325`) runs inline as a plain function. Under D-09, **build** the subsystem
    (`DependsOn(Database)` for gameID) — the ID already anticipates it.

### ARCH-05 — gateway boundary

- **D-15:** **Leaf-only principle.** The gateway MAY import **dependency-free leaf**
  packages (vocabulary, value types, pure helpers). It MUST NOT import any package that
  transitively reaches DB / domain / bus. Therefore `internal/core`, `internal/session`,
  and `internal/grpc` go on `forbidden` **wholesale**, and the pieces the gateway
  legitimately needs move to real leaves.
  - **Rejected: a narrow per-symbol allow-list** (the arch review's literal
    recommendation). A symbol allow-list inside a forbidden package is the same escape
    hatch that let this drift — `cmd/holomush`'s `coreOnlyFiles` allowlist has already
    grown to ~30 entries, each a judgement call nobody re-checks.
  - Converges with D-01/D-03/D-05: ARCH-04 is already shrinking `internal/core`.

- **D-16:** **The complete violation inventory** (verified live; arch-review LOW-6):
  | Symbol | Site | Disposition |
  |---|---|---|
  | `grpcclient.TranslateSubscribeErr` | `internal/telnet/gateway_handler.go:29,718` | **The consequential one** — `internal/grpc` is the CoreServer monolith that transitively imports `internal/{world,access,command}`, so a gateway file drags the whole domain into its closure while passing the tripwire. **Extract to a neutral package** (destination is Claude's discretion; review suggests `internal/grpcerrors` or `pkg/`). |
  | `session.DefaultLeaseRefreshInterval` | `internal/web/handler.go:22`, `internal/telnet/` (×2) | A duration constant, but `internal/session` **is** the DB-reaching session store — the dangerous import the tripwire would miss. Move the constant to a leaf. |
  | `core.NewULID` | `internal/web/handler.go:21`, `internal/telnet/gateway_handler.go:670` | Generates `connectionID`. Per CLAUDE.md, session IDs legitimately use `core.NewULID()` (NOT `idgen.New()`), so the gateway needs a ULID generator from a leaf — do not silently redirect to `idgen`. |
  | `core.ParseCommand` | `internal/telnet/gateway_handler.go:406` | Command grammar → leaf. |
  | `core.EventTypeArrive` / `EventTypeLeave` | `internal/telnet/gateway_handler.go:1247-1249` | Resolved by **D-05** (leaf vocabulary package). |
  | `naming.Theme` | `internal/telnet/guest_auth.go:21,28` | Guest-name theme; leaf-ish, not domain — currently NOT forbidden. Planner: confirm `internal/naming` is a true leaf; if so it may stay. |

- **D-17:** **Enforcement — extend the existing AST test and BIND the invariant.**
  - The existing test (`cmd/holomush/gateway_imports_test.go:111-137`) **already loads
    `internal/web/...` and `internal/telnet/...`** — the coverage is there; only the
    `forbidden` list is wrong. Add `internal/core`, `internal/session`, `internal/grpc`
    to it (`gateway_imports_test.go:103-111`).
  - **Amend `INV-EVENTBUS-1`'s summary** (`docs/architecture/invariants.yaml:2340-2348`) —
    it enumerates the same package list and omits core/session/grpc, so it drifts the
    moment the list changes.
  - **Flip `INV-EVENTBUS-1` `binding: pending` → `bound`** with `asserted_by` + a
    `// Verifies: INV-EVENTBUS-1` annotation, then `go run ./cmd/inv-render`. It currently
    carries a `refs:` entry, not `asserted_by`. Follow `.claude/rules/invariants.md`.
  - **Rejected: adding depguard as a second gate** — two places to keep in sync, and
    depguard cannot express the file-level `coreOnlyFiles` split `cmd/holomush` needs, so
    the two gates would cover different scopes.

- **D-18:** ⚠️ **The arch review's LOW-6 contains a STALE recommendation the planner MUST
  NOT follow.** It says *"Fix the invariant label to INV-GW-1."* **Do not.** The GW family
  was **retired and migrated** into `INV-EVENTBUS-1..16` by `holomush-hz0v4.14.12` — see
  `internal/gateway_invariants/meta_test.go:17` ("the GW family migrated to
  INV-EVENTBUS-1..16"). `INV-EVENTBUS-1` is the **correct current label**; following the
  review literally would **reverse a completed migration**. Note also that
  `docs/architecture/invariants.yaml:483-486` deliberately retains `INV-GW-*` tokens as
  **regex fixtures** for the word-boundary matcher — do not "clean them up".

### Claude's Discretion

- The final verb set for `presence.Emitter`'s renamed methods (`Arrived`/`Departed` vs
  `EmitArrive`/`EmitLeave`).
- `ReplayTail`'s new signature shape (extra `beforeSeq uint64` param vs a cursor struct).
- The destination package for the extracted `TranslateSubscribeErr` and the gateway leaves.
- Exact package placement/naming of the event-type vocabulary leaf (D-05) and the broadcast
  builder (D-02).
- Internal wave decomposition **within** Wave A / Wave B (the A/B split itself is fixed by
  D-12).
- Whether the D-14 MEDIUM-11 fix lands as a real `DependsOn` edge or as comment-deletion +
  a topo-order pin test (decide against the actual boot-order intent).
- PR/delivery shape. Phase 5's D-04 precedent was **one phase PR**; Phase 7 has no
  structural reason to differ, but the planner may split if Wave A/B argue for it.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The decisions that constrain ARCH-04 (read first)
- `docs/adr/holomush-i4784-world-state-model-decision.md` — the accepted MODEL-01 decision
  (Option B, CRUD-canonical). Establishes that the collapsed Event is an audit/notification
  wire type, NOT a state-rebuild source.
- `docs/reviews/arch-review/2026-07-11/verification/model-01-consensus-onepager.md` —
  **NORMATIVE**. §"Schema governance" explicitly designates the taxonomy registry the
  **ARCH-04 (Phase 7) input** "so the unified event model adopts these schemas".
- `docs/reviews/arch-review/2026-07-11/verification/model-01-panel-round2.md` §104,129 —
  the same designation, with the payload-shape rationale.
- `.planning/phases/05-world-model-integrity-fixes-m2-m12/05-CONTEXT.md` — Phase 5's
  decisions; its `<code_context>` names the taxonomy registry as the ARCH-04 input.

### The findings this phase closes
- `docs/reviews/arch-review/2026-07-11/findings/d1-architecture.md` — **the primary source**:
  - **LOW-6** (line 84) = ARCH-05, with the full violation inventory. ⚠️ Contains the stale
    "rename to INV-GW-1" recommendation — see D-18.
  - **LOW-7** (line 93) = unbounded `StopAll`.
  - **LOW-8** (line 102) = `productionSubsystems` positional params.
  - **MEDIUM-11** (line 129) = documented boot order not enforced by the graph.
  - **MEDIUM-4** (line 58) = plugin⇄eventbus⇄grpc bidirectional coupling — Phase 8's, but
    D-01 exists to avoid deepening it.

### Rules governing this phase
- `.claude/rules/gateway-boundary.md` — the ARCH-05 boundary rule.
- `.claude/rules/event-conventions.md` — subject naming; `core.NewEvent()`/ULID
  identity-vs-ordering; `Nats-Msg-Id` dedup; the qualified-wire / bare-crypto vocabularies.
- `.claude/rules/event-interfaces.md` — `Publisher`/`Subscriber`/`HistoryReader`; ordering
  ownership.
- `.claude/rules/invariants.md` — the register/bind ratchet for D-17's `INV-EVENTBUS-1`
  binding. **Never fabricate a binding.**
- `.claude/rules/plugin-runtime-symmetry.md` — the broadcast port (D-02) touches the
  plugin-facing `SessionAdmin` capability; Lua and binary MUST reach the same outcome.
- `.claude/rules/logging.md` — context-carrying slog variants (bootstrap code is the
  classic bare-`slog` offender; the `main`/init carve-out is narrow).

### Invariant registry
- `docs/architecture/invariants.yaml:2340-2348` — `INV-EVENTBUS-1` (the gateway-import
  invariant): summary to amend, `binding: pending` → `bound` (D-17).
- `docs/architecture/invariants.yaml:483-486` — the deliberate `INV-GW-*` regex fixtures;
  do NOT clean up (D-18).
- `internal/gateway_invariants/meta_test.go:17` — records the GW → EVENTBUS migration.

### Key source seams
- `internal/world/outbox/taxonomy.go` — the designated ARCH-04 input (`AppSchemaVersion`,
  the declared kinds).
- `internal/world/outbox/wire.go:149-175` — the existing `wmodel.Envelope` →
  `eventbus.Event` conversion; the precedent for the collapse target.
- `internal/eventbus/types.go:141-205` — `eventbus.Event` (note `:143` Seq's
  never-serialize rule; the unexported `auditRow`).
- `internal/lifecycle/orchestrator.go` / `subsystem.go` / `registry.go` — the ARCH-03
  target.
- `cmd/holomush/core.go` — `runCoreWithDeps`, 1,531 lines / 12 steps.
- `cmd/holomush/gateway_imports_test.go` — the ARCH-05 gate.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`lifecycle.Orchestrator` already does the hard part** — `topoSort` on `DependsOn`,
  first-failure abort, reverse-order rollback, reverse-order `StopAll`
  (`orchestrator.go:44-149`), with real unit coverage. ARCH-03 is **not** "build an
  orchestrator"; it is "stop doing bootstrap work outside the one that exists".
- **`lifecycle.ReadinessRegistry` already exists** (`registry.go` — `AllReady`/`WaitReady`/
  `HealthReporter`/`HealthTier`), used at core.go step 9. Reuse it if a gate is wanted
  (D-10). ⚠️ Note `AllReady` returns **true when no reporters are registered** (vacuous
  truth) — a fail-open default worth eyeballing if it becomes load-bearing.
- **`cursor.HostCursor` already carries a `Seq` field** — D-07 fills it in rather than
  adding anything.
- **`SubsystemTLS` ID already exists** — D-14's TLS subsystem has its identifier waiting.
- **`hostcap.SessionAdmin` already declares the broadcast shape** (`capabilities.go:58-60`)
  — D-02's port needs no new shared interface.
- **`internal/world/outbox/wire.go`** — the worked example of building an `eventbus.Event`
  from an intent-level envelope; the model for D-02's broadcast builder.

### Established Patterns
- **Intent at the chokepoint, wire type in one place.** Phase 5's `mutate(ctx, entity,
  expectedVersion, envelope)` compile-time seam is the in-repo precedent D-02/D-09 follow:
  name the *intent* at the boundary; construct the wire type at exactly one site. ARCH-04
  **adopts** this pattern rather than inventing one.
- **Structural enforcement over discipline** — the write-requires-envelope seam, census
  meta-tests, fail-closed-at-load manifest gates. D-11 is the same philosophy applied to
  subsystem lifecycle.
- **Consumer-defined interfaces** — the Go idiom that makes D-02 free; also why
  `AccessPolicyEngine` lives in `policy/types` (ARCHITECTURE.md's cycle-avoidance note).
- **Panic guards on pre-Start accessors** are the tell for D-09's bug class: a guard means
  the type has two lifecycle states its constructor can't express, so callers sequence by
  hand — the orchestrator's job.

### Integration Points
- `internal/eventbus` → `internal/core` (`types.go:13`, `rendering_publisher.go:12`, for
  `VerbRegistry` + `NewULID`) — **the cycle constraint that drives D-03**.
- `internal/command` does **NOT** import `internal/eventbus` today — D-02 keeps it that way.
- `internal/grpc` already imports eventbus, so it needs **no port** — it can build
  `eventbus.Event` directly (`emitCommandResponse`, `server.go:633`).
- `core.EventAppender` has **four** consumers, not two: `core.Engine` (`engine.go:34`),
  `command.Services` (`types.go:523`), `hostcap.systemBroadcaster`
  (`system_broadcaster.go:36`), and `grpc.CoreServer` (`server.go:157`, `WithEventStore`
  at `:251`). Plus `plugin/setup/subsystem.go:493` `ConfigureSystemBroadcaster(appender)`.
  **A single broadcast port does NOT absorb Engine or `emitCommandResponse`** — those are
  genuinely different emitters.
- Impls to retire with the type: `busEventAppender` (`sub_grpc.go:821`),
  `coretest.MemoryEventStore`, harness `noopEventAppender` + `busEventAppenderAdapter`
  + `focusHistoryReaderAdapter`.

### Landmines
- **`task test` does NOT compile `//go:build integration` files.** This phase is a
  cross-package type refactor — the exact shape that breaks integration silently.
  `task test:int` is MANDATORY per CLAUDE.md, not optional.
- `internal/testsupport/integrationtest/harness.go` hand-mirrors production wiring in at
  least three places (`harnessCoreToBusActor:1364`, `busEventAppenderAdapter:1328`,
  `focusHistoryReaderAdapter:1522`). Each must move in lockstep with production.
- `cmd/holomush/*_test.go` is `package main`, not `package main_test`.
- `internal/plugin/hostcap/servers.go` is `internal/grpc/focus`-coupled (MEDIUM-4);
  touching it may surface Phase 8 territory — stay inside ARCH-04's seam.

</code_context>

<specifics>
## Specific Ideas

- **D-07's regression test must pin the real failure**, not the happy path: concurrent
  publishers producing ULIDs that do NOT match stream sequence, proving pages neither skip
  nor repeat. A test that only walks pages on a quiet stream passes today and proves
  nothing — `hot_jetstream.go:427` names the exact condition to reproduce.
- **Wave A must be verified behaviorally, not just by compilation**: the boot panic D-09
  addresses (`core.go:790-796`) only reproduces "in any environment with a configured KEK
  (which is the production-deploy shape)" — so the eager-start removal MUST be exercised
  with a KEK wired, or the regression it prevents is untested.
- The `productionSubsystems` count assertions cascade — per prior learnings, changing the
  subsystem set touches `TestProductionSubsystemsIncludesCluster` (a count assertion),
  `TestProductionSubsystemsIncludesAdminSocket`, and `TestSubsystemAdminSocketConstantExists`.
  D-14's TLS subsystem + D-9's rewiring will hit all of them.
- New `SubsystemID` constants go at the **END** of the const block, then `task generate`
  regenerates `subsystemid_string.go` — inserting mid-block breaks the linecomment stringer.

</specifics>

<deferred>
## Deferred Ideas

- **`cmd/holomush`'s `coreOnlyFiles` allowlist (~30 entries)** — the same escape-hatch
  pattern D-15 rejects, but it governs `cmd/holomush`, not `internal/web`/`internal/telnet`,
  so it is outside ARCH-05's text. The `gateway_imports_test.go` design conflates the two
  scopes; worth revisiting (Phase 8, or its own issue).
- **Exposing `Seq` to plugins** (`hostv1.Event`) — forbidden by
  `internal/eventbus/types.go:143` (D-08). If a real plugin need appears, it requires an
  invariant amendment + ADR + a decision on cold-tier seq semantics.
- **MEDIUM-4's full bidirectional-coupling unwind** (`internal/plugin` ⇄ `internal/eventbus`
  ⇄ `internal/grpc`; `grpc/focus` shared seam; `authguard→plugin` manifest adapter) —
  Phase 8 (ARCH-01/ARCH-02).
- **`ReadinessRegistry.AllReady` vacuous-truth fail-open** (returns true with zero
  reporters) — noticed while scouting D-10; not an ARCH-03 requirement. File if it proves
  load-bearing.
- **Doc drift found while scouting (NOT Phase 7 scope, but will mislead agents):**
  - `.planning/PROJECT.md` "Key Decisions" #3 still asserts *"state derives from
    replay/projection, never from mutable authoritative tables alone"* — **reversed by
    MODEL-01**. Root `CLAUDE.md` was corrected by MODEL-02; PROJECT.md was not.
  - `.planning/codebase/ARCHITECTURE.md` (dated 2026-07-08, predates Phases 4/5) repeats
    the same stale event-sourcing framing (§"Pattern Overview", §"State Management").
  - Both should be corrected (a MODEL-02 completeness gap) — worth a `gh issue`.
- **`internal/core` remains a grab-bag after Phase 7** — once `Event` and `Engine` leave,
  it still holds Actor, Character, ParseCommand, VerbRegistry, ULID, actor_context,
  session_ended_payload. A fuller decomposition is Phase 8/999.9 territory.

</deferred>

---

*Phase: 7-Event-Model & Bootstrap Decomposition*
*Context gathered: 2026-07-15*
