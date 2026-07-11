# Architecture (D1) — Findings

**Agent:** architect / Opus 4.8 · **Date:** 2026-07-11 · **Scope examined:** `internal/world` write paths + event emission, `cmd/holomush/{core,gateway,sub_grpc}.go`, `internal/lifecycle`, `internal/cluster`, `internal/eventbus` subscriber + audit projection, `internal/plugin` (manager/event_emitter/cryptowiring), `internal/command/alias.go`, `internal/web/handler.go` + `gateway_imports_test.go`, `pkg/plugin` SDK surface + in-tree plugin imports, `docs/adr/` + `docs/superpowers/specs/` coverage, architecture explanation doc + CLAUDE.md claims.

## Summary

The engineering discipline is real and mostly well-directed: the lifecycle orchestrator is textbook-clean, plugin runtime symmetry is enforced at a single common gate, `internal/core` stays genuinely free of plugin vocabulary, the big decisions (JetStream, ABAC, crypto, typed-RPC) each have dedicated specs/ADRs, and the JetStream subscriber path is multi-core-safe by construction. The load-bearing architectural problem is a **claim/reality gap**: docs (and user-facing marketing) describe an event-sourced system where "state derives from replay," but world state is a direct-write CRUD store in PostgreSQL with events as an after-the-fact notification/audit side-channel — and no ADR owns that real decision. Secondary themes: the multi-node substrate is built and runs everywhere but only crypto consumes it (other per-core caches have no coherence story); the plugin/eventbus/grpc layer boundaries are muddy with growing god-objects; and stateful third-party plugins structurally need `internal/`.

**Counts:** Blocker 0 · High 1 · Medium 6 · Low 5 · Strengths 7

> Findings 11–13 were merged in by a second D1 (architect / Opus 4.8) pass. It independently reproduced HIGH-1, MEDIUM-2, and MEDIUM-3 (same citations, listed above) and adds three items the first pass did not reach: a boot-order graph gap (MEDIUM-11), the world-write lost-update path the first pass left in "Not examined" (MEDIUM-12), and the `internal/grpc` omission from the gateway tripwire (folded into LOW-6).

## Findings

### HIGH-1  "State derives from event replay" is false; world state is a CRUD store and no ADR records the real model
- **Severity:** High
- **Claim:** The documented architecture — "actions produce immutable ordered events; state derives from replay" — does not describe the implementation. World state (locations, exits, characters, objects) is written directly to dedicated PostgreSQL tables; events are emitted (when at all) *after* the DB write as a notification/audit side-channel, and nothing ever rebuilds world state from events. The gap is documented nowhere as a deliberate decision.
- **Evidence:**
  - Direct-write CRUD, no event: `internal/world/service.go:222` (`CreateLocation` → `locationRepo.Create`), `:244` (`UpdateLocation` → `locationRepo.Update`), `:332` (`CreateExit`), `:672` (`UpdateCharacterDescription` → `characterRepo.Update`) — none of these emit any event.
  - Where events DO fire, the DB write is the source of truth and the event is downstream: `internal/world/service.go:571` writes via `objectRepo.Move`, *then* `:587` `EmitMoveEvent`.
  - No projector reconstructs world tables from events — the audit projection only writes the audit table: `internal/eventbus/audit/projection.go:416` (`INSERT INTO events_audit`).
  - Production wiring confirms events go to JetStream, not a replayable state store: `cmd/holomush/sub_grpc.go:233-235` ("F7: EventWriter and PG events table are gone. Append goes directly to JetStream").
  - The overclaim, in four places: `CLAUDE.md:274` ("state derives from replay"); `site/src/content/docs/contributing/explanation/architecture.md:79` ("All actions are stored and replayable"); `docs/specs/2026-03-28-site-redesign.md:176` ("Every game action is an immutable event. Replay history"); `docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md:144` ("PostgreSQL becomes a *projection target*").
  - No ADR records "world state is CRUD, not event-sourced" as a chosen trade-off (confirmed by ADR-coverage sweep of `docs/adr/` + `docs/*/specs/`).
- **Impact:** Every contributor who reads the docs inherits a false mental model — they may assume they can rebuild or repair state by replaying events (they cannot; there is no projector), or that adding a mutation without an event is a bug (it is the norm). The user-facing site markets replay/auditability the world model does not provide. The real architecture (event-notification + audit log over a CRUD store) is a perfectly good choice for this scale; the cost is entirely in the misrepresentation and the missing decision record.
- **Recommendation:** Write one ADR that states the actual model plainly ("world state is authoritative CRUD in PostgreSQL; the event log is a delivery + audit channel, not a source of truth; state is not reconstructable from events"). Correct the four doc claims to match. Keep the marketing copy honest ("audit log of what happened," not "replay to reconstruct state").
- **Dedup:** none (issue #4674 touches `EventStoreAdapter` mechanics but not the model-claim gap)

### MEDIUM-2  Event-emitting mutations are dual-writes with no outbox — a NATS hiccup silently loses the notification
- **Severity:** Medium
- **Claim:** Mutations that both write the DB and emit an event do so non-atomically and with no transactional outbox: the DB write commits first, then the event is published; if publish fails the row is already mutated and no retry exists, so subscribers never see the change.
- **Evidence:** `internal/world/service.go:571` (`objectRepo.Move` commits) → `:587` (`EmitMoveEvent`) → `:590-593` returns `OBJECT_MOVE_EVENT_FAILED` while stamping `move_succeeded: true` — explicit acknowledgement that the state change persisted but the notification was lost. Same shape in `MoveCharacter` (`service.go:773`). Because the system is not event-sourced (HIGH-1), there is no outbox/relay pattern to make the pair atomic.
- **Impact:** On a transient NATS/JetStream failure during a move, the object/character moves in the DB but connected players never receive the move event — the world silently desyncs from clients until the next full state fetch. Plausible at hobbyist scale during a NATS restart or network blip.
- **Recommendation:** Either (a) document this as an accepted limitation of the notification model, or (b) if a move must be observable, adopt a transactional outbox (write an outbox row in the same tx as the state change; a relay publishes it) so the event survives a broker outage.
- **Dedup:** none

### MEDIUM-3  Multi-core substrate runs in every deployment but only crypto consumes it; other per-core caches have no cross-replica coherence
- **Severity:** Medium
- **Claim:** Running >1 core process is architecturally *intended* (Phase 3 shipped external NATS + a cluster subsystem), but the story is partial: the cluster machinery's only real consumer is crypto DEK-cache invalidation, while other per-process in-memory caches go stale across replicas with no invalidation path, and no doc states whether multi-core core is a supported deployment.
- **Evidence:**
  - Cluster subsystem runs unconditionally and can self-terminate: `cmd/holomush/core.go:500-508` ("cluster.Registry runs in every deployment"; ProductionPill `os.Exit(125)`).
  - Its only non-metrics consumer is the crypto invalidation coordinator: `internal/eventbus/crypto/invalidation/coordinator.go` / `types.go:85` (`Registry cluster.Registry`). No other subsystem consumes cluster membership.
  - Per-process caches with no cross-replica invalidation: `internal/command/alias.go:22` (`AliasCache`, in-memory maps; `SetPlayerAlias` at `:138` mutates local state only), `internal/core/registry.go:39` (`VerbRegistry`, built from plugins at boot). Each core also spawns its own binary-plugin subprocesses and Lua VMs.
  - The delivery path *is* multi-core-safe: subscriber consumers are per-session-durable, not per-core — `internal/eventbus/subscriber.go:177` (consumer name = session id) — so cross-core event fan-out is correct.
  - No doc states multi-core support (grep of `site/` + `docs/roadmap.md` for horizontal/replica/core-count returns nothing).
- **Impact:** An operator who scales core to 2 replicas gets crypto cache coherence but silent staleness elsewhere: a player who edits an alias on the replica holding their web connection won't see it on the replica holding their telnet connection until reload; verb sets diverge if replicas load different plugin versions. There is no warning that the deployment mode is half-supported.
- **Recommendation:** Decide and document the supported deployment topology. If multi-core is a goal, generalize the cluster invalidation bus beyond crypto (or make alias/verb state read-through) and add an ADR; if single-core is the supported mode, say so and gate the cluster subsystem's full cost behind a multi-node flag.
- **Dedup:** none (#4720 is a test-harness alias refresh, not cross-replica coherence)

### MEDIUM-4  plugin ⇄ eventbus ⇄ grpc boundaries are muddy (bidirectional subpackage coupling) and the god-objects are still growing
- **Severity:** Medium
- **Claim:** The three largest subsystems reach into each other's subpackages in both directions, and the two god-objects flagged in #4674 have grown rather than shrunk — the "mid-refactor" feel is compounding, not settling.
- **Evidence:**
  - `internal/plugin` imports `internal/eventbus` (root), yet `internal/eventbus/authguard/adapter_manifest.go:7` imports `internal/plugin` — the eventbus tree both underlies and depends on plugin.
  - `internal/grpc` imports `internal/plugin`, yet 7 plugin files import up into the grpc tree via `internal/grpc/focus` (`internal/plugin/manager.go`, `host.go`, `goplugin/host.go`, `hostcap/capabilities.go`, `lua/{host,focus_ops_adapter,hostcap_adapter}.go`).
  - `internal/plugin/cryptowiring/cryptowiring.go:20-22` reaches into three eventbus subpackages (`audit`, `codec`, `history`).
  - `internal/plugin/manager.go` is now 1869 LoC / 60 functions / 11 `ManagerOption`s; `internal/grpc/server.go` is 1873 LoC — both grew from the #4674 baseline (1222 / 1331). `TestLoadPlugin` still ships in the production binary (`manager.go`, no build tag).
- **Impact:** No Go import cycle (Go forbids it), but the layering is not enforceable by direction — any of the three can pull the others' internals, so the "eventbus is lower than plugin is lower than grpc" mental model is not real. The growing god-objects raise the cost of every future change in the plugin loader and the CoreServer.
- **Recommendation:** Extract the shared seams (`grpc/focus` types, the authguard→plugin manifest adapter) into a neutral lower package so the arrows point one way. Split `manager.go` along the load-time-wiring vs runtime-delivery line already identified in #4674, and move `TestLoadPlugin` behind a test build tag.
- **Dedup:** already-tracked:#4674 (god-objects + error-model); the bidirectional subpackage coupling and the growth-since-filing are new detail

### MEDIUM-5  Stateful third-party plugins structurally require `internal/` — the "no internal/" extensibility claim holds only for stateless plugins
- **Severity:** Medium
- **Claim:** A stateless plugin can be built from `pkg/plugin` + `pkg/proto` alone, but any plugin that persists to its own PostgreSQL tables the way the in-tree marquee plugins do must import Go-`internal/` packages that an external module cannot reach.
- **Evidence:**
  - Clean, stateless plugins prove the SDK works: `plugins/core-communication/`, `plugins/core-building/` import zero `internal/`.
  - The marquee stateful plugin does not: `plugins/core-scenes/audit.go:20`, `types.go:7`, `store.go:21`, `participants.go:6`, `publish_service.go:18`, `publish_types.go:9` import `internal/pgnanos`; `publish_store.go:19` imports `internal/idgen`. `plugins/core-channels/` imports both too.
  - The SDK itself couples to internal: `pkg/plugin/audit.go:29` imports `internal/eventbus/audit/auditheader`.
  - Go's `internal/` visibility rule makes every one of these unimportable by a module outside `github.com/holomush/holomush`.
- **Impact:** The "third parties can extend HoloMUSH without touching internal/" promise is true only for plugins that hold no state. A community author who wants a plugin with its own persistence (the common case for anything interesting) has to copy `pgnanos`/`idgen` by hand or vendor internals — the SDK doesn't actually cover the marquee pattern.
- **Recommendation:** Promote `internal/pgnanos` and `internal/idgen` to `pkg/` (they are tiny: a pgx time codec and a ULID wrapper), and move `auditheader` out of the SDK's internal dependency. Then the in-tree stateful pattern is externally reproducible.
- **Dedup:** none

### LOW-6  Gateway import tripwire has a coverage gap; two literal INV-GW-1 violations pass unflagged
- **Severity:** Low
- **Claim:** The gateway boundary tripwire's forbidden list omits `internal/core` and `internal/session`, so two literal INV-GW-1 ("no domain imports") violations in the gateway compile clean.
- **Evidence:** `cmd/holomush/gateway_imports_test.go:87-95` `forbidden` lists world/access/store/plugin/eventbus/auth/service/command but not core, session, **or `internal/grpc`**. `internal/web/handler.go:21` imports `internal/core` (only for `core.NewULID()`), `:22` imports `internal/session` (only for a duration constant) — both DB-free, so no query path leaks. The more consequential omission is `internal/grpc`: `internal/telnet/gateway_handler.go:29` imports it (aliased `grpcclient`) for one helper used at `:718` (`grpcclient.TranslateSubscribeErr`), and `internal/grpc` is the CoreServer monolith that transitively imports `internal/{world,access,command}` (e.g. `internal/grpc/location_follow.go`). So a gateway-side file pulls the entire domain into its dependency closure while passing the tripwire. Separately, the test's own comment/name labels it `INV-EVENTBUS-1` while it enforces the gateway (INV-GW) boundary.
- **Impact:** The tripwire that operators rely on to keep the gateway thin would not catch a genuinely dangerous `internal/session` query import, and does not catch that `internal/grpc` already drags the whole core domain into gateway-side code — which defeats the "gateway holds no domain" intent the boundary exists to protect for the future separate-process split.
- **Recommendation:** Add `internal/core`, `internal/session`, and `internal/grpc` to `forbidden`. Give core/session a narrow allow-list for the two constant uses (or inline the ULID + duration); for `internal/grpc`, extract `TranslateSubscribeErr` (and any similar wire-error helpers) into a neutral shared package (e.g. `internal/grpcerrors`/`pkg/`) so the gateway stops importing the server monolith. Fix the invariant label to INV-GW-1.
- **Dedup:** none

### LOW-7  Subsystem shutdown has no deadline — a hung Stop blocks process exit indefinitely
- **Severity:** Low
- **Claim:** Graceful shutdown builds a 5s deadline but applies it only to the control server; the orchestrator's subsystem teardown runs on an unbounded `context.Background()`.
- **Evidence:** `cmd/holomush/core.go:1158` creates `shutdownCtx` (5s) and `:1161` uses it for the control gRPC server; but subsystem teardown is the earlier `defer orch.StopAll(context.Background())` at `:1088` — no timeout. `internal/lifecycle/orchestrator.go:81-96` `StopAll` iterates Stops sequentially with no per-subsystem bound.
- **Impact:** If a binary-plugin subprocess or a NATS drain hangs during Stop, the core process never exits on its own. Mitigated externally (systemd `TimeoutStopSec`, k8s `terminationGracePeriodSeconds`, and the ProductionPill exit-125 supervisor model), so it degrades to a hard kill rather than a stuck host — but the process imposes no self-bound.
- **Recommendation:** Pass a bounded shutdown context to `orch.StopAll` (reuse or extend the 5s `shutdownCtx`), and have subsystem Stops honor `ctx.Done()`.
- **Dedup:** none

### LOW-8  `productionSubsystems` takes 15 same-typed positional params — a call-site footgun
- **Severity:** Low
- **Claim:** The wiring entry point accepts 15 `lifecycle.Subsystem` arguments of identical type, so a mis-ordering at the call site compiles silently.
- **Evidence:** `cmd/holomush/core.go:1445-1454` (15 positional `lifecycle.Subsystem` params). The blast radius is limited because `topoSort` re-derives real start order from `DependsOn` (`internal/lifecycle/orchestrator.go:99-149`), so a swap does not corrupt dependency ordering — the risk is readability plus surprising tie-break order among independent subsystems.
- **Impact:** Low today; a maintainability hazard as the subsystem count grows (it grew from 12 to 15 already).
- **Recommendation:** Replace positional params with registration calls (`orch.Register(sub)` per subsystem) or a named struct, so order is explicit and mis-wiring is impossible.
- **Dedup:** none

### LOW-9  Contributor rule doc still says external/clustered NATS is "unimplemented" — Phase 3 shipped it in this baseline
- **Severity:** Low
- **Claim:** A load-bearing rules reference contradicts the shipped reality of the review baseline.
- **Evidence:** `.claude/rules/references/testing-detail.md:105-106` states "external/clustered NATS is unimplemented (tracked in holomush-s5ts)", but the baseline (`30d55a162`, #4782, merged 2026-07-11) ships it: `internal/eventbus/subsystem_external_test.go`, `deploy/nats/cluster-server.conf`, and a full how-to at `site/src/content/docs/operating/how-to/external-nats-deployment.md`. (`holomush-s5ts` in that how-to now scopes only a *future* read-only operator account, not external NATS itself.)
- **Impact:** Contributors reading the testing rules are told a shipped feature doesn't exist.
- **Recommendation:** Update the note to reflect that external NATS shipped in Phase 3; keep the `holomush-s5ts` reference scoped to the read-only-operator follow-up.
- **Dedup:** none

### LOW-10  ADR index is out of sync — 51 of 179 `holomush-*` ADRs are not listed
- **Severity:** Low
- **Claim:** The `docs/adr/README.md` index omits roughly a quarter of the live ADRs, including load-bearing ones.
- **Evidence:** ADR-coverage sweep found 179 `holomush-*` ADR files but 51 absent from the README index table (e.g. `holomush-qd3r5`, `holomush-nki4`, `holomush-f5h0`, `holomush-edqh1`). Supersession tracking itself works; drift is one-directional (files present, index stale).
- **Impact:** The index under-represents recorded decisions; a reader trusting it will miss real ADRs. Low, since files are discoverable directly.
- **Recommendation:** Regenerate the index from the ADR files (or add an index-completeness check to the docs lint gate).
- **Dedup:** none

### MEDIUM-11  Documented boot-order guarantee ("crypto chain verifier runs before EventBus") is not enforced by the dependency graph — the real topo order starts EventBus first
- **Severity:** Medium
- **Claim:** `productionSubsystems` comments assert an ordering the orchestrator does not honor. `StartAll` ignores the slice order and re-derives boot order from `DependsOn()` edges; because `EventBus` declares no dependencies it is seeded in the first batch, while the chain verifier only becomes ready after `Database` and joins the queue tail — so `EventBus.Start` runs *before* the verifier, the opposite of the comment. Nothing except the final sweep declares a dependency on the verifier.
- **Evidence:**
  - `cmd/holomush/core.go:1458` — comment: `cryptoChainVerifierSub, // runs before EventBus (chain integrity check)`.
  - `internal/lifecycle/orchestrator.go:44-78` `StartAll` runs `topoSort()` order (not slice/registration order); `:118-142` seeds the queue with in-degree-0 nodes and appends newly-ready nodes at the **tail**.
  - `internal/eventbus/subsystem.go:77` — `func (s *Subsystem) DependsOn() … { return nil }` → in-degree 0 → seeded first.
  - `internal/eventbus/audit/chain/verifier_subsystem.go:67` — verifier `DependsOn()` = `[SubsystemDatabase]` only; only `RekeyCheckpointSweep` (the last subsystem) `DependsOn` the verifier. `AuditProjection.DependsOn` = `[Database, EventBus, Plugins]` — also does not wait for the verifier.
- **Impact:** The whole-boot fail-closed posture *is* preserved — any `Start` error aborts `StartAll` and rolls back in reverse (`orchestrator.go:59-60,81-96`), so a corrupt chain still stops the process from serving regardless of order. The risk is latent: the "chain integrity confirmed before the event/audit path comes up" ordering exists only in comments + the ignored slice order, so a future change that makes a subsystem's `Start` begin serving/persisting (not just wiring) could rely on a guarantee the graph never made. It also misleads anyone auditing the fail-closed sequence. This refines LOW-8: the very "topoSort re-derives real order" property that makes a param swap harmless is what silently discards this documented ordering intent.
- **Recommendation:** Encode the intent as a real edge — have `EventBus` (or at minimum `AuditProjection`/`CryptoPolicy`) declare `DependsOn(SubsystemCryptoChainVerifier)` — or delete/rewrite the misleading comment and add a boot-order pin test asserting the actual `topoSort` sequence.
- **Dedup:** none (related-to LOW-8, which observed topoSort re-derivation but not this contradiction)

### MEDIUM-12  World writes are last-write-wins with no concurrency control — a lost-update path under the shipped two-replica deployment (fills the first pass's "Not examined")
- **Severity:** Medium
- **Claim:** World structural writes are read-modify-write against plain `UPDATE … WHERE id = $1` statements with no version/optimistic-concurrency guard, so concurrent writers (across the two documented core replicas, or concurrent commands on one replica) can silently clobber each other with no detection.
- **Evidence:**
  - `internal/world/postgres/location_repo.go:72-83` — `UPDATE locations SET … WHERE id = $1`; no `version`/`updated_at` predicate. `RowsAffected()==0` only distinguishes not-found (`:81-83`), never a stale write.
  - Service-layer mutators do read-then-write: `internal/world/service.go:230-251` `UpdateLocation` (and the `MoveCharacter` get-then-update at `:783-805`), giving a lost-update window between the read and the write.
  - No world-entity migration adds a version column for optimistic concurrency (repo-wide check of `internal/store/migrations/`).
  - The two-replica deployment is real and documented: `compose.cluster.yaml:8-14` ("two real core processes sharing one Postgres"), and the cluster substrate coordinates only crypto DEK-cache invalidation (MEDIUM-3), not world writes.
- **Impact:** Calibrated to hobbyist scale this is low-probability (single active replica is the norm; the cluster overlay is primarily a crypto-invalidation/convergence proof). But the multi-replica mode is a documented working example, and the safety the cluster work added (crypto) does not extend to the application write tier — two builders editing one location, or a move racing a description edit, is a silent lost update with no conflict surfaced.
- **Recommendation:** Document the concurrency contract honestly ("world writes are last-write-wins; run one active core for structural editing"). If concurrent structural editing becomes a goal, add an optimistic-concurrency column (`version`/`updated_at`) to the mutable world tables and a `WHERE id = $1 AND version = $expected` guard that surfaces a `CONFLICT`. No change needed for the single-replica default.
- **Dedup:** none (this is the "concurrent world-write races under 2 live core replicas" item the first pass explicitly left in Not-examined)

## Strengths

- **Lifecycle orchestrator is textbook-clean.** Kahn's topological sort with explicit cycle detection and missing-dependency detection, deterministic tie-break by `SubsystemID`, start-failure rollback that only stops successfully-started subsystems, and continue-on-error teardown — `internal/lifecycle/orchestrator.go:44-149`.
- **Plugin runtime symmetry is enforced at one common gate.** Both Lua and binary emits funnel through `internal/plugin/event_emitter.go:91` (`PluginEventEmitter.Emit`), where the manifest gates fire, exactly as the symmetry invariant requires — no per-runtime policy gradient.
- **`internal/core` is genuinely independent of plugin vocabulary.** No import of `internal/plugin` and no plugin event-type strings leak in; the boundary is even documented in-code (`internal/core/builtins.go:32-36`).
- **The big decisions are recorded.** JetStream (`docs/superpowers/specs/2026-04-18-jetstream-event-log-design.md`), ABAC DSL (`docs/specs/2026-02-05-full-abac-design.md` + ADR `holomush-kokk`), event crypto (`docs/superpowers/specs/2026-04-25-event-payload-crypto-design.md`), and typed-RPC-vs-command (ADR `holomush-v4qmu`) each have dedicated specs/ADRs.
- **Gateway is a real protocol-translation layer.** Handlers hold only gRPC interface clients — no `*pgxpool.Pool`, repo, or service struct (`internal/web/handler.go:141-152`); the gateway is a separate binary dialing core over mTLS (`cmd/holomush/gateway.go:227,308`), enforced by an AST-level import tripwire.
- **JetStream subscriber fan-out is multi-core-safe by construction.** Per-session-durable consumers (`internal/eventbus/subscriber.go:177`) mean any core hosting a session gets that session's full subject set, independent of how many cores run.
- **Invariant registry as governance.** 341 registered invariants with a generate-and-diff CI guard is an unusually strong architectural-drift defense for a project this size.

## Not examined

- Crypto correctness (crypto-reviewer's domain) — I assessed only its complexity footprint and its role as the sole cluster consumer.
- ABAC engine internals (ABAC dimension's domain).
- Concurrent world-write races under 2 live core replicas — now examined in MEDIUM-12 (static analysis of the write path; not exercised at runtime).
- Whether per-session durable consumer names can collide across cores on a load-balanced reconnect — flagged as a risk under MEDIUM-3, not verified end-to-end.
- Web/Svelte client architecture (UI dimension).
- Complexity budget (Q6) was folded into MEDIUM-3 rather than raised standalone: the crypto/ABAC investment is deliberate and earns its keep for the project's stated rigor goals; the one place complexity outruns its consumers is the cluster/multi-node substrate, captured there.
