---
gsd_state_version: 1.0
milestone: v0.12
milestone_name: Foundation Hardening
current_phase: 07
current_phase_name: event-model-bootstrap-decomposition
status: executing
stopped_at: Completed 07-06-PLAN.md
last_updated: "2026-07-18T00:35:23.278Z"
last_activity: 2026-07-17
last_activity_desc: Phase 07 execution started
progress:
  total_phases: 4
  completed_phases: 3
  total_plans: 36
  completed_plans: 31
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-07)

**Core value:** Players can play HoloMUSH end-to-end (create characters, communicate, roleplay in scenes)
through either telnet or the web client, with every access-control decision default-deny and every plugin
trusted identically.
**Current focus:** Phase 07 — event-model-bootstrap-decomposition

## Current Position

Phase: 07 (event-model-bootstrap-decomposition) — EXECUTING
Plan: 7 of 11
Status: Ready to execute
Last activity: 2026-07-17 — Phase 07 execution started

## Performance Metrics

**Velocity:**

- Total plans completed: 51
- Average duration: N/A (no plans executed yet under this GSD roadmap)
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 01 | 10 | - | - |
| 02 | 7 | - | - |
| 03 | 9 | - | - |
| 04 | 4 | - | - |
| 05 | 16 | - | - |
| 06 | 5 | - | - |

**Recent Trend:**

- Last 5 plans: N/A
- Trend: N/A

*Updated after each plan completion*
| Phase 01 P01 | 11 | 2 tasks | 7 files |
| Phase 01 P02 | 95min | 3 tasks | 24 files |
| Phase 01-channels-subsystem P03 | 40min | 3 tasks | 11 files |
| Phase 01 P04 | 40min | 2 tasks | 5 files |
| Phase 01 P05 | 55min | 2 tasks | 4 files |
| Phase 01 P06 | 55min | 2 tasks | 9 files |
| Phase 01 P05b | 70min | 2 tasks | 7 files |
| Phase 01 P07 | 75min | 2 tasks | 9 files |
| Phase 01 P08 | 55min | 2 tasks | 6 files |
| Phase 01 P09 | 150min | 3 tasks | 11 files |
| Phase 02 P01 | 20m | 3 tasks | 6 files |
| Phase 02 P02 | ~15m | 2 tasks | 4 files |
| Phase 02 P03 | 20m | 4 tasks | 11 files |
| Phase 02 P06 | ~40m | 4 tasks | 11 files |
| Phase 02 P04 | ~35m | 3 tasks | 5 files |
| Phase 02 P05 | 55m | 3 tasks | 28 files |
| Phase 02 P07 | ~35m | 3 tasks | 5 files |
| Phase 03 P01 | ~35m | 2 tasks | 4 files |
| Phase 03 P02 | 20m | 2 tasks | 5 files |
| Phase 03 P03 | 40m | 2 tasks | 4 files |
| Phase 03 P05 | ~70m | 3 tasks | 5 files |
| Phase 03 P07 | 50m | 3 tasks | 9 files |
| Phase 03 P09 | 20min | 2 tasks | 1 files |
| Phase 04 P01 | 40min | 2 tasks | 7 files |
| Phase 04 P02 | 35min | 2 tasks | 3 files |
| Phase 04 P03 | ~55min | 2 tasks | 3 files |
| Phase 04 P04 | ~90min | 3 tasks | 2 files |
| Phase 05 P01 | 20m | 3 tasks | 9 files |
| Phase 05 P14 | 45min | 3 tasks | 48 files |
| Phase 05 P02 | 45m | 2 tasks | 6 files |
| Phase 05 P03 | ~40m | 3 tasks | 5 files |
| Phase 05 P04 | 45m | 2 tasks | 6 files |
| Phase 05 P05 | 55min | 3 tasks | 11 files |
| Phase 05 P06 | 75min | 3 tasks | 18 files |
| Phase 05 P07 | 150min | 4 tasks | 26 files |
| Phase 05 P08 | 110 | 2 tasks | 3 files |
| Phase 05 P09 | 24min | 3 tasks tasks | 13 files files |
| Phase 05 P10 | 120min | 2 tasks | 7 files |
| Phase 05 P15 | 120 | 2 tasks | 21 files |
| Phase 05 P16 | 150 | 3 tasks | 22 files |
| Phase 05 P12 | 14min | 3 tasks | 9 files |
| Phase 05 P13 | 20min | 2 tasks | 5 files |
| Phase 06 P03 | 30 | 6 tasks | 8 files |
| Phase 06 P04 | 8min | 3 tasks | 4 files |
| Phase 06 P05 | 35min | 2 tasks | 4 files |
**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 07 P01 | 20min | 2 tasks | 12 files |
| Phase 07 P02 | 33min | 2 tasks | 40 files |
| Phase 07 P03 | 25min | 3 tasks | 15 files |
| Phase 07 P05 | 50min | 3 tasks | 22 files |
| Phase 07 P04 | ~35min | 3 tasks | 4 files |
| Phase 07 P06 | 50min | 3 tasks | 13 files |

## Accumulated Context

### Decisions

Full decision log lives in PROJECT.md "Key Decisions" (v0.11 phase-level decisions were folded in at
milestone close; per-plan detail is archived in `milestones/v0.11-phases/`). No decisions accumulated for
the next milestone yet.

- [Phase 04]: M12 last-write-wins world corruption REPRODUCED deterministically (D-06): a stale full-row UPDATE silently reverts a committed rename, both writers returning nil (04-02)
- [Phase 04]: Success-criterion #1 four chaos dimensions all green; replica restart recovers canonical state from the DB, not event replay (04-02)
- [Phase 04]: M2 dual-write window characterized (D-07): MoveCharacter commits then emits post-commit; on broker flap the caller sees move_succeeded=true while notification delivery is decoupled from the result
- [Phase 04]: Production world.Service wires NO EventEmitter — the move-notification leg is dead code today (pinned by a spec)
- [Phase 04]: MODEL-01 decided: Option B — CRUD-canonical + optimistic concurrency + transactional outbox in the panel-ratified strengthened shape (consensus one-pager NORMATIVE); Phase 5 implements MODEL-03 version guard + MODEL-04 ordered atomic feed — Human decider (Sean Brandt) chose under future-state-first framing after a two-round three-model panel unanimously ratified the strengthened B shape; the ordered complete world-change feed is the platform's extensibility contract; evolvability inverts under event sourcing pre-1.0; coverage rot countered structurally (compile-time seam + census meta-test + delta-parity)
- [Phase 04]: INV-WORLD-ATOMIC-FEED/-DELTA-PARITY/-FEED-ORDER/-WRITER-BOUNDARY named in the ADR; registration/binding deferred to Phase 5's spec per .claude/rules/invariants.md
- [Phase ?]: Phase 5 slice-1 foundation: version INTEGER NOT NULL DEFAULT 1 on locations/exits/characters/objects (migration 000049); Version int on the four world structs; WORLD_CONCURRENT_EDIT/ErrConcurrentEdit as the single typed conflict signal (D-02/MODEL-03).
- [Phase ?]: [Phase 05]: MODEL-03 CAS mechanism for locations+exits (05-02): version-predicated Update/Delete + a locked follow-up read (same-connection via re-entrant withTx) classifying a zero-row result into TWO outcomes — existing-row-version-moved -> WORLD_CONCURRENT_EDIT, absent -> NOT_FOUND (a committed concurrent delete is correctly observed as not-found).
- [Phase ?]: [Phase 05]: expectedVersion/Version==0 stays an unversioned (id-only) write so existing world.Service delete/update callers (which pass 0 today) remain green; the guard fires only when a caller threads a read version >0 (version-threading is plan 05-04).
- [Phase ?]: [Phase 05]: location DELETE locks the parent row FOR UPDATE BEFORE preselecting FK-cascaded exits (round-6 R6-4) — the parent lock conflicts with the FK key-share lock a child-exit INSERT needs, fencing the child-insert phantom; an interleave integration test binds INV-WORLD-2 delta-parity adversarially.
- [Phase ?]: 05-04: RMW version threading was already end-to-end after 05-02/05-03 via struct.Version transport plus deepest-oops-code; Task 1 added pinning tests with no production change
- [Phase ?]: 05-04: M12 command-race specs serialize through HandleCommand, so the surfaced conflict is proven deterministically at the service level (spec 1 location + new spec 4 object)
- [Phase ?]: [Phase 05]: 05-05 MODEL-04 outbox foundation (slice 2): migration 000050 lands outbox (event_id PK dedup + (game_id,epoch,feed_position) UNIQUE gap-free) + world_feed_counter (locked per-game next_position/epoch + durable lease_generation) + world_genesis_checkpoint + SPLIT world_consumer_receipts/world_consumer_watermarks.
- [Phase ?]: [Phase 05]: 05-05 WriteIntent (internal/world/postgres writer boundary) is sole owner of storage-stamped envelope fields (round-3 blocker #1): allocates epoch/feed_position from the locked FOR UPDATE counter, finalizes via pure wmodel.Finalize, persists one outbox row via execerFromCtx (same tx), returns the finalized Envelope; types in wmodel leaf; WORLD_FEED_LOCK_TIMEOUT bounds a stuck lock.
- [Phase ?]: [Phase 05]: 05-05 always-run INV-WORLD-1 integration test proves a REAL world row + its envelope commit-or-roll-back together (rollback/commit/forced-duplicate-event_id); binding annotation added in 05-12.
- [Phase ?]: [Phase 05]: 05-06 mutate(ctx, intent, write-closure) compile-time write-requires-envelope seam — closure identifies+executes the operation (round-5 finding 1), writer repos private to executor, package world imports neither outbox nor postgres (round-2 cycle fix); injected world.OutboxWriter owns epoch/position+finalization (round-3 blocker #1).
- [Phase ?]: [Phase 05]: 05-06 MoveCharacter is first through the same-tx outbox; post-commit emit path (events.go/EmitMoveEvent/go-retry) DELETED folding WR-01 (D-03); post-commit movement-hook failure = operational degradation (log+metric, command success), move_succeeded=true fail-after-commit path deleted (round-5 finding 3); M2 dual-write window CLOSED (proven by rewritten resilience spec).
- [Phase ?]: [Phase 05]: 05-07 MODEL-04 relay slice — single leased relay (Lease abstraction, dedicated advisory-lock conn + durable generation fence), reference idempotent consumer (tx-bound ApplyOnce + contiguity-safe watermark UPSERT), SkipService (stable skip-marker id) wired as OutboxRelaySubsystem; production world.Service finally gets a real OutboxWriter; 8-edge import-graph guard + composition allowlist
- [Phase ?]: 05-08: D-05 resilience specs construct the REAL relay/lease/reference-consumer over the shared stack via the production setup.NewOutboxStore adapter; relays release their lease via DeferCleanup so a pinned conn never blocks harness teardown.
- [Phase ?]: 05-10: all 10 location/exit/object write commands route through the mutate() seam — one taxonomy-declared envelope per successful command in the same tx (INV-WORLD-4); manifest finalized from the repo's returned MutationDelta (cascaded exits, reverse exit), never command inputs. First half of the D-01 rollout; character/scene/property + census land in 05-11.
- [Phase ?]: 05-10: the per-game feed-counter FOR UPDATE lock globally serializes the world-write phase; bisect-confirmed this widened the conflict window so the slow describe command path deterministically loses its full-row CAS to a concurrent direct UpdateLocation (correct per INV-WORLD-ATOMIC-FEED), surfacing an errA-swallowing assumption in the M12 cross-field-race spec — now read-back-driven.
- [Phase ?]: 05-15: ONE atomic CharacterGenesisService; all 3 creation paths route through it; Create removed from auth repo interfaces (compile fence); player/role ordered not atomic (round-4 B4).
- [Phase ?]: 05-15: genesis service must NOT import internal/world/outbox (eventbus-relay import cycle) — uses local kind/schema constants mirroring the taxonomy, like internal/world/service.go.
- [Phase ?]: 05-16: guest character reaping routes through one atomic CharacterReapingService (per-character tombstone tx then ordered player delete) — deletion-side counterpart to 05-15 genesis, closing D-06
- [Phase ?]: 05-16: anti-TOCTOU closed at creation side (R6-2 option b) — players.reaping_at + genesis SELECT reaping_at FOR UPDATE serializing with the reaper MarkReaping; single shared tx precluded by the two-pool boundary
- [Phase ?]: 05-16: added BindingRepository.DeleteByCharacter (guest-teardown-only, in-tx) so the character-first tombstone delete avoids the RESTRICT binding FK; operator forensic soft-end path untouched
- [Phase ?]: 05-12: INV-WORLD scope registered as status:pending because internal/world carries pre-existing FOREIGN bare INV-N tokens (holomush-72ou per-property-ABAC) the provenance residual-walk would misattribute; the four INV-WORLD-1..4 entries are nonetheless binding:bound (born canonical).
- [Phase ?]: 05-12: INV-WORLD ids are canonical NUMERIC (INV-WORLD-1..4); ADR symbolic names (ATOMIC-FEED/DELTA-PARITY/FEED-ORDER/WRITER-BOUNDARY) live in summary+legacy — the //Verifies parser (invariant_registry_test.go:163) requires a trailing number (Codex finding 3).
- [Phase ?]: 05-12: INV-WORLD-2 delta-parity binds to a REAL-ROW integration test in internal/world/outbox (location-delete cascade + bidirectional exit) proving manifest==MutationDelta==actual row version transition, not presence.
- [Phase ?]: 05-13: MODEL-02 doc downgrade — false 'event sourcing / state derives from replay' corrected at 4 sites (CLAUDE.md/AGENTS.md-symlink, README.md, coding-standards.md, architecture.md) to the decided model (event-driven + append-only audit log, ADR holomush-i4784); real client-catch-up/Subscribe replay language preserved; index.mdx:41 legitimate audit-log language (Open Q4 resolved); regression-guarded by test/meta/world_model_doc_claim_test.go.
- [Phase ?]: 06-01: events_audit partitioned on a deterministic ULID-derived event_ms key; timestamp column unchanged (cold-tier boundary preserved); no DEFAULT partition; crypto gate READY
- [Phase ?]: 06-03: nats CVE GHSA-q59r-vq66-pxc2 is a git-range-only OSV record no manifest/reachability scanner can flag; remediation = bump to v2.14.3 + deterministic cmd/nats-floor-guard compensating control.
- [Phase ?]: 06-03: task lint:vuln = 3 fail-closed legs (nats floor guard + govulncheck + osv-scanner v2); OSV allowlist scoped to osv-scanner only; 5 test-only docker/docker findings allowlisted (issue #4817).
- [Phase ?]: 06-04: codecov project ratchet (target: auto, threshold: 1%); patch+project POST but are not required protect-main checks (gh api ruleset 11923801) — codecov ruleset add accepted-deferred, only OPS-03 Vuln mandatory (owned by 06-03)
- [Phase 07]: rev3 D-13.0 — the Prepare/Activate barrier is scoped to EXTERNALLY-REACHABLE surfaces + domain work loops, not "anything running". Grounding: embedded NATS sets `DontListen: true` (eventbus/subsystem.go:153) so it binds no socket, and audit's own acquisition requires that server live (audit/subsystem.go:273 AUDIT_DEP_NOT_STARTED) — a universal "nothing serves until everything is acquired" barrier is circular for that chain. eventbus's whole Start body → Prepare; plugin loading/subprocess launch → Prepare (audit's DependsOn(Plugins) forces it); only grpc's listener and admin.sock bind in Activate.
- [Phase 07]: rev3 — 07-09's ~20 pre-orchestrator live-value reads are settled by HOISTING core.go:705-1060 whole into a memoized (sync.Once) `cryptoWiring` builder in package main; the block's body moves verbatim so the dbSub.Pool()/authSub.Hasher()/abacSub.Resolver() reads inside it simply execute post-Start. No 18th subsystem; no repo signature churn. THE RULE: every cryptoWiring consumer must declare DependsOn ⊇ {Database, Auth, ABAC, EventBus} — the first consumer to resolve the provider builds it.
- [Phase 07]: rev3 — `deps.TLSCertEnsurer` (deps.go:53,71) is a live test seam that breaks at compile time when ensureTLSCerts is deleted; the body becomes exported `tls.EnsureCerts` with the SAME signature so the Deps field type is unchanged.
- [Phase 07]: rev3 — the promised `Seq == 0` → BeforeID pagination fallback DOES NOT EXIST (bus.go:87,94; hot_jetstream.go:334; cold_postgres.go:125 — BeforeID is a tripwire for a NONZERO seq). Policy settled: zero seq means "no cursor, read the tail" (status quo); reject-as-stale and ID→seq resolution both rejected.
- [Phase 07]: rev3 — Go evaluates deferred ARGUMENTS at registration, so `defer orch.StopAll(shutdownCtx)` would expire ~5s into uptime and cancel every graceful stop. The closure form (core.go:255-261 telemetry / :356-362 observability) is the in-repo precedent and the mandated shape.
- [Phase ?]: 06-05: OPS-04 audit-DLQ replay resolves game_id MIRRORING the server (--game-id override -> core.game_id via config.Load(...,core) -> persisted DB), closing the F3 external-NATS subject-prefix mismatch; tautological embedded-NATS test replaced with a divergent-game natstest test driving the real resolver seam
- [Phase ?]: 07-01: internal/grpc/client.go extracted verbatim into new leaf package internal/grpcclient; telnet closure dropped 47->10 holomush/internal/ packages, closing the gateway.go client-import gap RESEARCH.md Pitfall-4 missed
- [Phase ?]: 07-02: internal/eventvocab created as dependency-free event-type vocabulary leaf (D-05); internal/core repointed with zero forwarding alias; 39 consumers (9 prod + 30 test) repointed; event_payload_size_test.go deleted as exact duplicate (coverage folded into eventvocab_test.go)
- [Phase ?]: [Phase 07]: 07-03: internal/ulidgen/cmdparse/sessionlease leaves extracted — internal/telnet and internal/web now import neither internal/core nor internal/session (production or test code); D-16's three remaining gateway leaks closed; 07-04 has no code left to change, only enforcement to add
- [Phase ?]: 07-05: core.Engine moved to internal/presence (presence.Emitter), publishing arrive/leave/session_ended through eventbus.Publisher; internal/auth breaks the resulting import cycle with its own 2-method consumer-defined PresenceEmitter interface rather than importing presence
- [Phase ?]: 07-05: cmd/holomush's presence emitter wraps the wrapPublisher-wrapped publisher (never rawPublisher) so events_audit still receives the App-Rendering header; harness resolves gameID from its own bus.GameID, not a hardcoded main
- [Phase ?]: [Phase 07]: 07-04 gateway boundary closure gate + INV-EVENTBUS-1 binding — added a transitive-closure import gate (packages.NeedDeps walk) alongside the existing AST direct-import gate; forbade internal/core/session/grpc wholesale (D-15/D-17); fixed the dead internal/auth/service phantom entry (replaced with internal/auth), surfacing two genuinely core-only files that needed coreOnlyFiles classification (crypto_operator_validation.go, cmd_admin_totp_run_test.go); INV-EVENTBUS-1 flipped pending->bound with asserted_by naming both gates
- [Phase ?]: sysbroadcast.Broadcaster copies presence.Emitter's {pub eventbus.Publisher; gameID func() string} shape verbatim (FINDING-5), including the empty-gameID->main fallback
- [Phase ?]: cmd/holomush introduces a shared bus := s.cfg.EventBus local in grpcSubsystem.Start reused by both the SessionAdmin broadcast closure and the command-services broadcaster closure — one game-id source for the whole host
- [Phase ?]: internal/grpc's dispatcher_test.go/test_helpers_test.go were undeclared consumers of the deleted Services.Events() accessor; registerTestCommands now takes the shared store directly as a parameter

### Pending Todos

None yet.

### Blockers/Concerns

- Forums (Epic 11, `holomush-djj`) has no design yet — blocks any Forums-integration forward work
- Discord integration (Epic 12): Channels prerequisite shipped in v0.11; still blocked on an OAuth substrate that does not yet exist
- 259/334 registered invariants are `binding: pending` (concentrated in INV-CRYPTO and INV-SCENE) — tracked
  epic `holomush-hz0v4`, not a blocker, but phases touching crypto/scenes should bind relevant invariants as
  part of their own definition of done

### Quick Tasks Completed

| # | Description | Date | Commit | Directory |
|---|-------------|------|--------|-----------|
| 260709-sqg | Fix holomush-9hygy — convert core-channels migrations TIMESTAMPTZ→BIGINT epoch-ns (lint:no-timestamptz ship blocker) | 2026-07-10 | 1284ba341 | [260709-sqg-…](./quick/260709-sqg-fix-bead-holomush-9hygy-convert-core-cha/) |
| 260711-hg1 | GH-4785 (F2): cap gateway ConnectRPC request-body size (`WithReadMaxBytes` 4 MiB + `ReadTimeout`) to prevent unauthenticated OOM | 2026-07-11 | 0e3806ebf | [260711-hg1-…](./quick/260711-hg1-gh-4785-cap-gateway-connectrpc-request-b/) |

## Deferred Items

Items acknowledged and carried forward from the ingest, not part of this roadmap:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Social-spaces | Forums integration (Epic 11) | No design yet | Ingest 2026-07-07 |
| Social-spaces | Discord/Slack bridging + OAuth linking (Epic 12) | Blocked on Channels + OAuth substrate | Ingest 2026-07-07 |
| Web portal | Non-scene web surfaces (building/world editing, admin UI) | Directional theme goal, not yet spec'd | Ingest 2026-07-07 |

## Session Continuity

Last session: 2026-07-18T00:35:23.271Z
PROJECT.md / REQUIREMENTS.md / ROADMAP.md / STATE.md written and committed (PR #4811).
Stopped at: Completed 07-06-PLAN.md
Resume file: None

## Operator Next Steps

- Merge PR #4811 (milestone-init planning docs).
- Ship the F2 gateway DoS cap (#4785) as a `/gsd-quick` fix — the immediate opener.
- Then `/gsd-plan-phase 4` (or `/gsd-discuss-phase 4`) — the F1 resilience pass + event-sourcing-vs-CRUD ADR.
