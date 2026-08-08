---
gsd_state_version: 1.0
milestone: v0.13
milestone_name: "Web Portal: Identity & Admin Foundations"
current_phase: 02
current_phase_name: ABAC & Schema Vocabulary
status: executing
stopped_at: Phase 02.1 context gathered
last_updated: "2026-08-08T19:19:54.152Z"
last_activity: 2026-08-06
last_activity_desc: Phase 01.1 complete, transitioned to Phase 02
progress:
  total_phases: 9
  completed_phases: 3
  total_plans: 35
  completed_plans: 26
  percent: 33
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-07-07)

**Core value:** Players can play HoloMUSH end-to-end (create characters, communicate, roleplay in scenes)
through either telnet or the web client, with every access-control decision default-deny and every plugin
trusted identically.
**Current focus:** Phase 02 — abac-schema-vocabulary
complete character identity surface (creation, management, public profiles with privacy) and stand up the
`RoleAdmin`-gated admin portal shell, both designed to absorb the deferred portal surfaces without rework.

## Current Position

Milestone: v0.13 Web Portal — Identity & Admin Foundations (Phases 1–6)
Phase: 02 — ABAC & Schema Vocabulary
Plan: Not started
Status: Ready to execute
Progress: [██████████] 100% (1/6 phases)
Last activity: 2026-08-06 — Phase 01.1 complete, transitioned to Phase 02
`binding: pending`, 9 amendments applied, 3 verification gaps closed

**Next action:** review the branch, then `/gsd-code-review` **and** `abac-reviewer`
(`/holomush-dev:review-abac`) — the diff amends the `INV-ACCESS`/`INV-PRIVACY` scope records — then
`task pr-prep`, push, and `/gsd-discuss-phase 2`.

**Phase 1 opened four issues, all still open:** #4899 (per-player vs per-character admin authority —
answered *per player* by SPEC §10.5), #4900 (`docs/superpowers/` retirement sweep), #4901 (published-scene
`participants_snapshot` documented as names, stores ids), #4902 (`oops.AsOops(err).Code()` resolves the
deepest chain code, not the top-level one — PORTAL-10 rule 5 was corrected to a wire-level assertion).

**Milestone shape (phases 1–6):**

> Phase numbers **restart at 1 per milestone as of v0.13**; v0.11 (Phases 1–3) and v0.12 (Phases 4–9)
> used the retired continuous global numbering and are not renumbered. A bare "Phase N" below means v0.13.

| Phase | Name | Reqs | Notes |
|-------|------|------|-------|
| 1 | Portal SPEC | 10 | Opens the milestone — discharges PROJECT.md's Out-of-Scope precondition; 8 of 14 catalogued pitfalls are SPEC-phase decisions |
| 2 | ABAC & Schema Vocabulary | 6 | Lifecycle column + normalized-name unique index land here (load-bearing for two phases each); narrow data audit needed |
| 3 | World Character Commands | 3 | `Rename`/soft `Retire`; `--research-phase` (writeCommands census bijection) |
| 4 | Shared Facade Helpers & `CharacterAccessService` | 7 | Verbatim copy of the shipped `sceneaccess_service.go` path |
| 5 | Character Identity UI & Public Profiles | 12 | UI phase — first user-visible slice; ships the media-schema proof with no uploader |
| 6 | Admin Portal Shell & Character Administration | 12 | UI phase; net-new trust boundary (zero `RoleAdmin` refs in `internal/web/` today); `--research-phase` |

**Binding across every phase (PORTAL-10):** census with set equality; paired positive control on every
denial test; assertions against marshaled response bytes; gates demonstrated RED against the pre-fix state;
wire-level assertion of every opacity and authorization contract (rule 5 was corrected in Phase 1 — `oops.AsOops(err).Code()`
resolves the *deepest* chain code, not the top-level one, so the original prescription asserted the opposite
of the property it guarded; see #4902 and `01-SPEC.md` §12.1); invariant-scope discipline (no ad-hoc `INV-PROFILE-*` /
`INV-ADMIN-*` — allocate in `ACCESS`/`PRIVACY` or declare a boundary, and ship `binding: pending` rather
than fabricating a `// Verifies:`).

**Pre-existing hazards this milestone is the first to load** (all verified in-tree 2026-07-31, none new
defects): `PlayerHasRole` is player-wide not character-wide (`internal/store/role_store.go:83-103`) —
excluded from scope by PORTAL-08/ADMIN-04 and to be filed as a GitHub issue; character-name uniqueness has
no DB constraint and `Rename` doubles the writers into that race (Phase 2); rename/retire cannot reach
denormalized history (`actor_display_name`, `scene_log` via `WebGetPublicSceneArchive`); hard-delete is
already broken (`locations.owner_id`/`objects.owner_id` have no `ON DELETE`); a public profile page is
currently DENIED by `seed:player-character-colocation`; `internal/web/` has zero `RoleAdmin` references.

**Carried in from v0.12:** 3 open Broken Windows block `/gsd-ship` until fixed or waived — #4861
(`cmd/holomush` coverage floor), #4788 (movement pipeline untested), #4864 (yamlfmt block-scalar leak).

## Deferred Items

Items acknowledged and deferred at milestone close on 2026-07-28:

| Category | Item | Status |
|----------|------|--------|
| verification override | Phase 09 `09-VERIFICATION.md` | `gaps_found` (3/4 must-haves) — accepted. Criterion 1 failed: coverage backfilled but the repo-wide gate it promised does not exist. Criteria 2/3/4 passed, 3 verified by execution. |
| requirement | QUAL-02 — coverage backfill | Deferred. `cmd/holomush` 70.09% (9.91 under floor) → #4861; both halves of the D-04 gate → #4875, #4876 |
| requirement | QUAL-03 — weak-test / ACE remediation | Deferred. Zero-assertion sweep clean, but scope is one predicate, not a clean bill of health → #4860 |
| requirement | QUAL-05 — code-health batch | Deferred. 4 of 5 arch-review Mediums landed; DEK read-cache → #4792; de-slop half deliberately not started |
| artifact | `09-21-SUMMARY.md` | Never written; plan executed (opened PR #4874). Must_haves re-derived live into `09-VERIFICATION.md`. Not backfilled — a reconstruction would read as a contemporaneous record it is not. |
| tech debt | `CLAUDE.md` event-construction rule | Wrong about `EnvelopeToEvent`; following it breaks outbox dedup → #4880 |
| tech debt | ruleset ↔ CI context reconciliation | Nothing checks that every required context is producible → #4881 |
| tech debt | two load-dependent flakes | No quarantine row, now tracked → #4882, #4883 |

**Audit-tool note:** `gsd-tools query audit-open` reports `.planning/debug/knowledge-base.md` as an open
debug session. It is not one — it is the debug *knowledge base* of resolved sessions, consumed by
`gsd-debugger`. The query scans `.planning/debug/*.md` and treats every file as a session. False positive;
no action needed.

## Performance Metrics

> Phase numbers in the tables below are **v0.11/v0.12** phases under the retired
> continuous global scheme (v0.11 Phases 1–3, v0.12 Phases 4–9) — not v0.13's Phases 1–6.

**Velocity:**

- Total plans completed: 69
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
| 07 | 11 | - | - |
| 01.1 | 7 | - | - |

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
| Phase 07 P07 | 46min | 3 tasks | 35 files |
| Phase 07 P08 | 35min | 3 tasks | 13 files |
| Phase 07 P09 | ~5h | 3 tasks | 50 files |
| Phase 07 P10 | ~45min | 4 tasks | 8 files |
| Phase 07 P11 | ~4h | 3 tasks | 68 files |
| Phase 08 P01 | 35m | 3 tasks | 6 files |
| Phase 08 P02 | ~40m | 4 tasks | 15 files |
| Phase 08 P03 | 55m | 3 tasks | 8 files |
| Phase 08 P04 | ~55m | 3 tasks | 5 files |
| Phase 08 P05 | ~70m | 3 tasks | 11 files |
| Phase 08 P06 | ~85m | 3 tasks | 6 files |
| Phase 08 P07 | ~75m | 3 tasks | 15 files |
| Phase 08 P08 | ~95m | 3 tasks | 9 files |
| Phase 08 P09 | ~70m | 3 tasks | 4 files |
| Phase 09 P01 | 55min | 2 tasks | 3 files |
| Phase 09 P02 | 35min | 3 tasks | 0 files |
| Phase 09 P03 | 22 | 2 tasks | 6 files |
| Phase 09 P04 | ~15min | 3 tasks | 4 files |
| Phase 09 P05 | ~25min | 1 tasks | 5 files |
| Phase 09 P06 | 25m | 1 tasks | 2 files |
| Phase 09 P07 | ~40m | 3 tasks tasks | 4 files files |
| Phase 09 P08 | 1h | 2 tasks | 2 files |
| Phase 09 P09 | 50min | 2 tasks tasks | 2 files files |
| Phase 09 P10 | 85min | 2 tasks tasks | 1 files files |
| Phase 09 P11 | ~40min | 1 tasks | 4 files |
| Phase 09 P20 | 38m | 3 tasks | 5 files |
| Phase 09 P12 | 26m | 3 tasks | 5 files |
| Phase 09 P13 | 71m | 2 tasks | 2 files |
| Phase 09 P14 | 96m | 3 tasks | 5 files |
| Phase 09 P15 | 71m | 3 tasks | 5 files |
| Phase 09 P16 | 62m | 2 tasks | 2 files |
| Phase 09 P18 | 150min | 3 tasks tasks | 71 files files |
| Phase 09 P17 | 55m | 2 tasks | 1 files |
| Phase 09 P19 | 70m | 3 tasks | 4 files |
| Phase 01.1 P01 | 95m | 3 tasks | 16 files |
| Phase 01.1 P02 | 40 min | 2 tasks | 7 files |
| Phase 01.1 P03 | 35 min | 2 tasks | 2 files |
| Phase 01.1 P05 | ~25 min | 2 tasks | 1 files |
| Phase 01.1 P04 | 75m | 3 tasks | 7 files |
| Phase 01.1 P06 | ~70 min | 2 tasks | 6 files |
| Phase 01.1 P07 | 85m | 3 tasks | 11 files |
| Phase 02 P01 | 75m | 3 tasks tasks | 21 files files |
| Phase 02 P02 | ~95min | 3 tasks | 11 files |
| Phase 02 P03 | 34min | 2 tasks | 4 files |
| Phase 02 P04 | 85m | 3 tasks | 11 files |
| Phase 02 P05 | 110min | 3 tasks | 21 files |
| Phase 02 P13 | 71min | 4 tasks | 15 files |
| Phase 02 P06 | 155min | 3 tasks tasks | 36 files files |
| Phase 02 P07 | 118min | 4 tasks | 9 files |
| Phase 02 P08 | 20min | 3 tasks | 3 files |
| Phase 02 P09 | 95min | 3 tasks | 9 files |
| Phase 02 P12 | ~200min | 3 tasks | 45 files |
| Phase 02 P10 | 63min | 6 tasks | 5 files |
| Phase 02 P11 | 105m | 3 tasks | 5 files |

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
- [Phase ?]: 07-07: WithEventPublisher reuses CoreServer's existing gameID field rather than adding a duplicate
- [Phase ?]: 07-07: Combined Task 1+2 into one commit (interdependent edits verified together); Task 3 committed separately
- [Phase ?]: 07-07: event_emitter.go untouched — CoreServer builds a direct typed system-actor literal instead of exporting the plugin-private actor bridge
- [Phase ?]: 07-08: D-07 fixed — ReplayTail (plugins.HistoryReader + hostfunc.HistoryReader, lockstep) gains beforeSeq uint64; both runtimes' cursor encoders thread each event's real Seq (encodeHostEventCursor + hostfunc's two independent encode sites) instead of a hardcoded 0; beforeSeq==0 means read-the-tail with no ID-only fallback on either tier
- [Phase ?]: 07-08: D-08 preserved — hostv1.Event stays at exactly 8 fields, pinned by a new census meta-test (TestHostV1EventFieldCensusExcludesSequence) asserting field-set equality, not just seq-absence
- [Phase ?]: 07-09: D-12 Wave A — killed all five eager subsystem starts (dbSub/eventBusSub/abacSub/authSub/invalidation.Coordinator); every gameID/live-value consumer resolves through gameIDProvider or the memoized cryptoWiring builder (cryptowiring.go) at its own Start, gated by a real DependsOn edge
- [Phase ?]: 07-09: TLS is a real registered lifecycle.Subsystem (tlscerts.TLSSubsystem, DependsOn Database); productionSubsystems takes a named 17-field productionSubsystemSet instead of a 16-position positional list (LOW-8)
- [Phase ?]: 07-09: ABAC deliberately excluded from the cryptoWiring consumer set (crypto-operator validation moved into ABACSubsystem.Start instead) to avoid a second ABAC->cryptoWiring->ABAC cycle; chain.VerifierSubsystem/socket.AdminSocketSubsystem both gate external binds on SubsystemCryptoChainVerifier, permanently forbidding the EventBus->CryptoChainVerifier reverse edge
- [Phase ?]: 07-09: eventbus.Subsystem.Start now runs VerifyAccountScoping unconditionally in external mode, exposing that a bare/unscoped natstest.StartNATS() container is over-scoped by design; added natstest.StartScopedNATS/ScopedURL (loads deploy/nats/cluster-server.conf) for test harnesses needing a full external-mode boot to succeed
- [Phase ?]: 07-10: MEDIUM-11 closed by comment-deletion + topo-order pin (not the eventbus->CryptoChainVerifier edge rev 4 shipped, which cycles against 07-09's verifier->EventBus edge)
- [Phase ?]: 07-10: StopAll deadline-aware (buffered one-shot result channel per Stop, raced against ctx.Done()); StartAll rollback moved to a fresh bounded context in this plan (not deferred to 07-11)
- [Phase ?]: 07-10: grpcSubsystem.DependsOn() gains SubsystemAuditProjection (T-07-50); core_topo_order_test.go pins the real 17-subsystem topological order + proves the post-07-09 graph acyclic, reading every DependsOn() live
- [Phase ?]: 07-11: D-12 Wave B — lifecycle.Subsystem split into Prepare/Activate/Stop; Orchestrator.StartAll runs two full topological sweeps (all Prepare, then all Activate), the structural barrier that makes acquire-before-serve unrepresentable-to-violate regardless of DependsOn edges; rollback now stops the failing subsystem itself and always runs on a fresh context
- [Phase ?]: 07-11: all 17 production subsystems migrated per the plan's settled D-13.3 disposition table; PluginSubsystem's cleanupOnError extended to close binaryHost+luaHost on every pre-manager Prepare failure path (closed a token-store-sweeper-goroutine leak); audit.Subsystem gained preparedProjection/partitionManager phase-owned fields with lateInit capture/restore-on-failure
- [Phase ?]: 07-11: invalidation.Coordinator's construction+Start() stays bundled inside the memoized cryptoWiring builder (deviation from the plan's literal row-16 text) because CryptoChainVerifier — not necessarily grpcSubsystem — is the actual first resolver of the builder in topological order; confining it to the Prepare sweep (which it always is) preserves D-13.0's guarantee since the Coordinator's pub/sub is process/cluster-internal signaling, not client-facing domain traffic

- [Phase 08]: 08-01: internal/focuscontract created as a neutral types-only leaf holding the 7-declaration focus contract transitive closure (Coordinator + RestorePlan -> StreamWithMode -> ReplayMode + SetConnectionFocusResult + AutoFocusOnJoinResponse -> AutoFocusFailure); import set is exactly context/time/oklog-ulid/internal-session, no internal/grpc edge (D-09 seam 1)
- [Phase 08]: 08-01: all 7 internal/grpc/focus originals converted to Go type ALIASES (= form), not defined types — alias identity keeps ~30 existing focus.* reference sites compiling untouched AND guarantees the Lua and binary plugin hosts see one identical Coordinator type (D-20 plugin-runtime symmetry); a defined type would have created a per-runtime divergence
- [Phase 08]: 08-01: RESEARCH.md § Seam 1's "5 declarations" undercount confirmed wrong — the transitive closure is 7; a 5-symbol move does not compile
- [Phase 08]: 08-01: moving the Coordinator doc comments broke TestProvenanceGuard (six INV-SCENE-14/17/18/24/25/26 refs recorded at internal/grpc/focus/coordinator.go). Resolved by retargeting the registry refs to internal/focuscontract/focuscontract.go + adding internal/focuscontract/** to INV-SCENE owned_paths — the registry follows the canonical home rather than duplicating the spec. Expect the same pattern in 08-02 if further INV-annotated comments leave internal/grpc/focus.
- [Phase 08]: 08-01: D-17 earned its keep on the very first wave — task test was green across all four affected trees while the meta suite was red; only task test:int surfaced it
- [Phase 08]: 08-01: D-15 zero-integration-churn record for this plan: `git diff --stat origin/main...HEAD -- test/integration/` is EMPTY
- [Phase ?]: 08-02: moved the deleted authguard manifestAdapter's nil-guard onto Manager.PluginRequestsDecryption/PluginCanReadBack — a typed-nil *Manager in a ManifestLookup is not interface-nil, so authguard.New's AUTHGUARD_DEPENDENCY_NIL check cannot catch it
- [Phase ?]: 08-02: declared the ManifestLookup mirror interface locally in internal/plugin rather than importing authguard, avoiding a mirror-image import edge
- [Phase ?]: 08-02: took D-08's export_test.go branch — the post-seam-2 TestLoadPlugin caller set is empty outside internal/plugin, so no build-tag plumbing was needed
- [Phase ?]: SubscribeDeps injects buildCharacterIdentity and recomputeSessionLiveness as function values, not a CoreServer backpointer (D-02 held despite cross-cluster method edges)
- [Phase ?]: toSubject extracted to the free function qualifyStreamSubject because emitCommandResponse is a second caller
- [Phase ?]: Zero CoreServer fields deletable: option setters need a field to write into before newSubscribeHandler reads it (D-04 pins CoreServerOption)
- [Phase ?]: 08-04: UnloadPlugin's identity deactivation hoisted out of the m.mu critical section — the one path the lock-split safety argument did not cover; the unload interleaving window is unavoidably widened by D-06.
- [Phase ?]: 08-04: pluginRepo/retentionDays stay on Manager as inert option routing slots because ManagerOption is func(*Manager) (D-07); identity state itself is fully extracted.
- [Phase ?]: 08-05: runDisconnectHooks has three consumers (command, lifecycle, auth/Logout); LifecycleHandler owns it, others take a DisconnectHookRunner function value
- [Phase ?]: 08-05: the Deps-snapshot pattern decouples facade fields from extracted units; CoreServer.buildHandlers() is the ordered constructor fixtures re-call after poking a field
- [Phase ?]: PluginRuntime extracted with its own lock; four m.mu sections spanning both clusters had their runtime call hoisted out (RegisterHost, ConfigureEventEmitter, loadPlugin commit, Close)
- [Phase ?]: Crypto manifest gates relocated byte-identically; nil fail-closed guards retained at BOTH receivers plus lookupManifest
- [Phase ?]: CommitLoaded returns 'existed' so loadedOrder's append stays under m.mu rather than nesting two unit locks
- [Phase ?]: buildCharacterIdentity moved to QueryHandler, making buildHandlers() a two-owner ordered constructor: lifecycleHandler and queryHandler build before commandHandler and subscribeHandler
- [Phase ?]: ARCH-01 closed: CoreServer is a facade over four constructor-injected units; server.go 1891 -> 657 LoC; exported method set fixed at 23
- [Phase ?]: Close and UnloadPlugin assigned to the LOAD unit: both invert load-unit operations and need policyInstaller/hosts/luaHost
- [Phase ?]: RegisterHost passes the IdentityStore to hosts instead of the *Manager — behaviorally identical through the interface, and avoids a D-02 backpointer
- [Phase ?]: Loader and identity/runtime siblings held as concrete pointers, not narrow interfaces (11 and 6 operations, same package)
- [Phase ?]: Manager's fourth field is the managerConfig option holder rather than a bare retentionDaysSet
- [Phase ?]: Ratchet rows target the whole internal/grpc tree, not just .../focus — strictly stronger and RED-proven to catch subpackages
- [Phase ?]: Sixth forbidden edge added: internal/plugin -> internal/eventbus/authguard, the mirror of D-09 seam 2
- [Phase ?]: Manager pinned by field-set equality (structural) as primary regrowth guard; LoC ceiling is the backstop
- [Phase ?]: No method counts hard-coded in the census — the plan's 39/36 were pre-split figures (actual 31/26)
- [Phase ?]: 09-01: E2E coverage was empty because docker compose stop SIGKILLed the -cover binaries at the 10s default grace before Gos exit hook could flush GOCOVERDIR (exit 137, covmeta without covcounters); fixed with stop_grace_period: 60s on core+gateway in compose.e2e.cover.yaml against a measured ~14.4s core shutdown whose tail is the OTLP exporters own 5s export timeout
- [Phase ?]: 09-01: the bind-mount uid hypothesis was ELIMINATED on macOS Docker Desktop (it maps mount ownership; a live touch /coverdata as uid 1000 returned WRITE_OK) so no user: override was added and the production images non-root posture is untouched — note this is disproven locally, not on a Linux CI runner
- [Phase ?]: 09-01: a meta-only .coverdata makes go tool covdata textfmt emit a FULL-SIZE all-zero profile (2,692,028 B / 32,375 body lines) and exit 0, so test -s, a body-line count and exit-status propagation ALL pass the broken shape; the coverage guard must assert a non-zero COVERED-statement count (Rule 2 deviation from the plans specified emptiness guard)
- [Phase ?]: 09-01: codecovs branch API is the authoritative project figure — 78.28% on main @ 497748c6d; the retired ~54.6% was a raw unit-lane go tool cover -func tail that applies neither the ignore list nor the cross-lane session merge
- [Phase ?]: 09-01: cmd/holomush/core.go + sub_grpc.go un-ignored in FULL form (tracer succeeded); from the E2E lane alone they measure 70.1% and 66.0%, so the 656 statements added to the denominator bring 448 covered with them — the un-ignore raises the number rather than lowering it
- [Phase ?]: 09-01: QUAL-02 deliberately left Pending in REQUIREMENTS.md — six phase-9 plans carry it (09-01/08/10/17/19/21) and this plan repaired the measurement chain rather than backfilling tests; the protocol mark-complete flip was reverted so the table does not assert a property no artifact demonstrates
- [Phase ?]: 09-02: the plan's premise that the four eventbus_e2e skip files had no GitHub issue was FALSIFIED — all four exist (#2881/#2880/#2387/#2386), closed NOT_PLANNED 2026-05-17; a tracker-id search misses them because GH issues never carried beads ids, a behaviour-phrase search finds exact title matches
- [Phase ?]: 09-02: #4855/#4856 cover work declined TWICE (closed NOT_PLANNED, then 'Archive only — not migrated'); their bodies state that deleting the test file outright is a legitimate resolution — 09-11 must make the delete-vs-retain call consciously
- [Phase ?]: 09-02: two of the three ec22.9 residue items had drifted — addlicense is RESOLVED (replaced by license-eye pinned v0.8.0; lefthook.yaml deleted) and the write-timeout ask was documentation not a timeout (http2 WriteByteTimeout already covers write liveness; adding http.Server.WriteTimeout would break streaming); issues re-scoped to what is true rather than filed at the stale framing
- [Phase ?]: ABAC providers: unresolved optional attrs are omitted from the bag, never sentinel-valued (ADR holomush-ti1b) — all five providers now conform
- [Phase ?]: 09-03: QUAL-05 left incomplete — carried jointly with 09-04/05/06
- [Phase ?]: 09-04: gateway --secure-cookies default INVERTED to true (#4794 / arch-review D4 MEDIUM-1); flag name + koanf key unchanged, --secure-cookies=false is the documented opt-out; cookie.go/security_headers.go untouched (they already built the secure form and downgraded)
- [Phase ?]: 09-04: the inversion is pinned at the config.Load level (real NewGatewayCmd -> koanf posflag -> fresh struct), not only at the pflag default — a GetBool assertion cannot see the plumbing between the flag and the running server (Rule 2)
- [Phase ?]: 09-04: compose.yaml gateway now passes --secure-cookies=false (E2E overlay does NOT override command; PLAYWRIGHT_BASE_URL=http://gateway:8080 is non-localhost plain HTTP, where browsers drop Secure cookies); proven by task test:e2e:cover exit 0, 104 specs, 482 covered cmd/holomush statements
- [Phase ?]: 09-04: QUAL-05 left Pending — it enumerates 5 Medium-cluster items and this plan delivers 1 (secure-cookie default); 09-05/09-06 carry the rest
- [Phase ?]: 09-05: migration 000053 adds idx_sessions_location_id (plain idempotent CREATE INDEX IF NOT EXISTS + paired DROP IF EXISTS, no concurrent build) closing #4796 — the presence/ListActiveByLocation filter column was unindexed across all 52 prior migrations
- [Phase ?]: 09-05: reversibility is proven by a round-trip spec (step to 52, assert absent, step to 53, assert present by KEYED pg_indexes name lookup + indexdef column check, step back, assert absent, reapply) — task test:int alone only ever migrates UP, so a no-op down passes it; falsifiability demonstrated by emptying the down body and observing the failure, then reverting
- [Phase ?]: 09-05: three pre-existing tests hardcoded latest-migration=52 (census list, mock latest, FullCycle x2); FullCycle literals replaced with a named latestMigrationVersion constant, but the pending-migration census kept as an explicit literal list — deriving it from allMigrationVersions() would be tautological against the helper PendingMigrations() uses
- [Phase ?]: 09-05: no EXPLAIN/query-plan assertion added (#4796's second AC clause) — on an empty test table the planner correctly prefers a seq scan regardless of the index, so the check would be vacuous or a test of fixture row count; QUAL-05 still Pending (3 of 5 Medium items delivered: 09-03 ABAC sentinels, 09-04 secure-cookie, 09-05 index)
- [Phase ?]: 09-06: no metric added on the plugin downgrade fence — a log record is what the finding asks for; an instrument would widen an observability fix into telemetry wiring on a crypto-review surface
- [Phase ?]: 09-06: fence drop log message pinned as a hard-coded test constant, not imported from production, so a silent reword fails rather than passes
- [Phase ?]: 09-07: (*Session).EmitDirectEventAt(ctx, stream, evType string, payload []byte, at time.Time) (string, error) added as a SIBLING of EmitDirectEvent (byte-identical, zero deleted lines, 36 call sites untouched); `at` sets Event.Timestamp ONLY — not the ULID (identity/dedup) and not the JetStream sequence (which owns ordering)
- [Phase ?]: 09-07: the plan's Task-3 demonstration premise was FALSIFIED — task lint runs with NO build tags, so a production import of the integration harness fails at typecheck ('build constraints exclude all Go files') before depguard is consulted; the //go:build integration tag is the first-line control and the new deny entry is an explicit second line, proven load-bearing only under --build-tags=integration
- [Phase ?]: 09-07: the plan's sleep guard rg -c 'time.Sleep' counts PROSE not calls (0 -> 2 from doc comments alone, with zero sleeps added); replaced with rg 'time\\.Sleep\\(' (zero call sites) — same defect class as the phase's other unfalsifiable verifies
- [Phase ?]: 09-07: depguard meta-test needle tightened from the bare package path to '- pkg: <path>' and the pinned set widened 3 -> 5 (natstest was configured but unpinned); both falsifications observed — deleted entry fails, comment-only mention also fails
- [Phase ?]: 09-07: QUAL-04 left Pending — this plan builds only the harness seam; the session-lifecycle matrix it unblocks is written by 09-12/13/14/15
- [Phase ?]: 09-08: plan's coverage gate measured go-tool-cover statement ratio (83.9% at HEAD) while its 76.2% baseline was a codecov line ratio — gate could not fail; replaced with a strict-increase gate plus per-test mutation controls
- [Phase ?]: 09-08: oops merges error context innermost-first, so SaveCertificates' outer 'operation' label is shadowed by saveCert/saveKey's — assert the surviving 'path' key instead
- [Phase ?]: 09-09: 6 of 10 sites cited by the archived weak-test record holomush-ec22.16 no longer exist as cited (3 file-gone, 1 function-gone-file-present); surviving in-scope set is 2 functions in internal/store/alias_test.go
- [Phase ?]: 09-09: alias interface canary moved from a runtime test to 'var _ AliasRepository = (*PostgresAliasRepository)(nil)' in alias.go:52 — strictly stronger (checked by every build, not just task test), proven load-bearing by a rename mutation that fails the build AT the assertion line
- [Phase ?]: 09-09: the 8 TestPostgresAliasRepository_* names are NOT ratchet violations — all carry subtests (documented TestType_Method exception); a 40-line rg window initially reported 6 of 8 as subtest-free, a too-small-window false positive of the same class as a too-loose predicate
- [Phase ?]: 09-09: 09-08's misleading TestEnsureCerts_DirectoryCreationFailure confirmed from source (fileExists returns !IsNotExist so ENOTDIR reads as 'exists' -> load-existing branch, never reaching xdg.EnsureDir); out of the derived remediation set, filed as #4860
- [Phase ?]: 09-10: cmd/holomush 80% floor NOT met and not faked — codecov merged (line, main) 64.82%; unit∪E2E (statement) 70.6%; reaching 80% needs +244 statements while the plan's two authorized files could supply at most +64, an arithmetic certainty established in Task 1 before any test was written; residual recorded per file in #4861
- [Phase ?]: 09-10: codecov's API ?path= filter is a PREFIX match — ?path=cmd/holomush silently includes cmd/holomush-cutover/main.go (30 files, 64.25%); the package-only figure needs select(startswith("cmd/holomush/")) => 29 files, 64.82%
- [Phase ?]: 09-10: deps_test.go (one of the plan's two authorized files) deliberately left untouched — deps.go is already 100% (20/20) under the union, so any test added there would be the assertion-free coverage theatre QUAL-03 exists to remove
- [Phase ?]: 09-10: rekeyAuditPublisherAdapter.PublishAudit was 0% covered — its clock override (ev.Timestamp = a.clock.Now(), which overrides NewEvent's time.Now()) and its three oops failure codes are now pinned; all 8 added tests proven falsifiable by mutation with each failure attributed to the intended test by name
- [Phase ?]: 09-10: config-section tests clear DATABASE_URL as a fall-through negative control — it is the next thing RunE reaches, so a dropped section Load makes the test FAIL rather than pass on 'an error was returned' (proven by NC6)
- [Phase ?]: 09-10: #4647's premise falsified by 09-01 — cmd/holomush/{core,sub_grpc}.go are no longer at '0-0.6%, instrumentation isn't observing it' but 88.7%/78.2% under the union; corrected in-place with a grounded comment rather than filing a duplicate, and left open because sub_grpc.go still has 62 uncovered statements
- [Phase ?]: 09-11: retained all four unimplemented eventbus_e2e specs rather than deleting the twice-declined #4855/#4856 files; the trim dissolves the maintenance-burden case for deletion and both issue bodies reserve that call for a maintainer closing the issue
- [Phase ?]: 09-20: telnet differentiation threaded through attach's SubscribeRequest only — the session_connections row is where production observes it
- [Phase ?]: 09-20: WithInTreePlugins wins over WithBuiltinCommands (register before adoption), so setting both cannot double-register or panic
- [Phase ?]: 09-20: administrator-boot matrix row dispositioned not-implementable-from-harness-defaults; resetpassword --kick exists but bypasses session_ended (issue #4862)
- [Phase 09]: Session-matrix registry adds a fifth disposition, 'planned', so no row claims a spec before that spec is written; 09-16 can also assert zero planned rows remain at phase end
- [Phase 09]: Matrix n/a cells follow the verbatim izk0 TABLE (9/11/12/6 populated per column), not 09-RESEARCH's derived totals (9/10/12/7), which disagree with the table they annotate
- [Phase 09]: multi_tab_test.go:217/242 rejected as a reattach-cell citation: it creates no game session, so 09-RESEARCH's D-16 'Reattach x telnet + multi-session' claim is false
- [Phase ?]: 09-13: post-ttl-relogin.web-guest left planned with blocked_on — no harness route re-logs a guest as the same character; a second-guest stand-in would pass with nothing for the reaper to have done
- [Phase ?]: 09-13: a bare absence assertion on a history read is untrustworthy — the fresh read returns zero rows, so a positive control rides in the same query
- [Phase ?]: 09-14: built Server.GuestPlayer, the guest counterpart of AuthedPlayer, to unblock the two reassigned guest re-authentication matrix cells rather than accept a stand-in that could not fail
- [Phase ?]: 09-14: reattach-cas.multi-session stays planned with owed_by: unassigned — per-connection detach was not built, and naming a closed plan as its ower would be false assurance
- [Phase ?]: 09-15: move-arrival cells take route (b) — MoveTo kept and the cells relabelled as privacy-floor-after-simulated-move; the harness world service has no MovementHook, so driving MoveCharacter would leave location_arrived_at unchanged and prove the opposite. Production movement-lifecycle claim cited to issue #4788.
- [Phase ?]: 09-15: added AuthedPlayer.AdditionalCharacter — two game sessions under ONE player session, the only shape that makes a token-keyed Disconnect teardown detectable (proven by negative control).
- [Phase ?]: 09-15: INV-PRIVACY-6 flipped pending -> bound; the new floor-preservation spec asserts BOTH clauses of the invariant in one read, so it is not a partial binding.
- [Phase ?]: 09-15: the two named privacy specs are Ginkgo containers carrying their identifier verbatim, not func Test symbols — no meta-test binds the names today (plan claim did not check out); recorded as EXEMPT from the 09-18 naming sweep.
- [Phase ?]: 09-16: pinned ALL five session-matrix disposition counts, not just not-applicable — relabelling an unbacked spec row is the cheapest way to satisfy a bijection, and it now fails three guards
- [Phase ?]: 09-16: the matrix guard carries no invariant-registry entry or binding annotation, following the quarantine bijection it models; the absence is grep-checked, so the file names the annotation descriptively rather than quoting it
- [Phase ?]: 09-16: no 'uncovered' disposition added — planned + owed_by:unassigned + blocked_on + issue #4863 already is the honest marking for reattach-cas.multi-session
- [Phase ?]: 09-18: tightened ACE predicate = underscore-form AND no-subtests AND single-token CamelCase tail; reproduced 09-RESEARCH exactly (1572/466/1106), TestGatewayCommand_SecureCookiesFlag correctly NOT a hit (3-token tail)
- [Phase ?]: 09-18: the predicate's 'web' skipDirs entry matched by BASENAME, silently skipping internal/web and hiding 22 real violations — the ratchet (reusing meta_helpers' shared skipDirs, which has no 'web') caught it; total 116 -> 138
- [Phase ?]: 09-18: table-case label detection must be restricted to elements of a SLICE literal — matching any name:/desc: field returns 45 hits, 32 of them domain fixtures like world.Location{Name: "Test"}; restricted it is exactly the 13 sites 09-RESEARCH enumerated
- [Phase ?]: 09-18: TestINV_ carve-out is DEFENSIVE, not load-bearing — the only two single-token-tail INV names (P4/P5_Coverage_Meta) declare subtests at line 170+ and are excluded by the sanctioned exception first (an rg -A25 window falsely reported them subtest-free)
- [Phase ?]: 09-18: ACE ratchet carries no invariant-registry entry and no // Verifies: annotation, following the quarantine and matrix ratchets; asserts a non-vacuous corpus (>500 files/>1000 decls/>500 labels) so an empty walk cannot pass as clean
- [Phase ?]: 09-17: codecov posts coverage results through TWO GitHub endpoints — codecov/patch is a CHECK RUN on PR head commits and a COMMIT STATUS on main pushes; querying only /commits/{sha}/status reports it absent, which is exactly what this plan's own gate did (patch=0, a false negative)
- [Phase ?]: 09-17: codecov/patch GO for the ruleset (14/14 live head commits, exact string codecov/patch, app id 254); codecov/project NO-GO — zero of 64 observations across 32 commits x 2 endpoints spanning 2026-04-26..07-27, issue #4875
- [Phase ?]: 09-17: the 'no base commit to compare against' explanation for the missing project status is FALSIFIED — codecov's own API reports base_totals 78.28 / head_totals 79.11 / ci_passed true for PR #4874; leading cause is the vendor-documented Team-plan patch-only limit, an account condition no .codecov.yml edit can fix
- [Phase ?]: 09-17: ruleset 11923801 already proves check-RUN names are matchable — 7 of its 8 required checks are check runs (integration_id 15368/none) and only CodeRabbit is a commit status, so requiring codecov/patch is mechanically sound
- [Phase ?]: 09-17: PR #4874 head eee76d23e measures 79.11% (codecov LINE ratio, 3 sessions) vs main base 78.28% — coverage ROSE 0.83 points; the 69.12% figure seen earlier was a mid-merge read before all three upload sessions landed
- [Phase ?]: 09-17: QUAL-02 restored to Pending — 09-08's mark-complete was a protocol side-effect that re-created the flip 09-01 had deliberately reverted; 09-19 owns the ruling, gaps are #4861 and #4875
- [Phase 09]: Coverage measurement chain proven repaired: e2e flag 32.27% (was 0.0); whole-repo rose 0.83pt to 79.11%
- [Phase 09]: codecov project ratchet tightened to threshold: 0% (true no-drop); inert until #4875 resolves
- [Phase 09]: D-04 deferred BOTH halves: codecov/project never posts (#4875); codecov/patch would deadlock docs-only PRs (#4876)
- [Phase 09]: QUAL-02 stays Pending — cmd/holomush 70.09% and whole-repo 79.11% are measurably below their named floors
- [Phase ?]: Task 1 checkpoint resolved option-a: two commits (engine, then corpus) as a non-independently-green unit
- [Phase ?]: C1 upheld by execution: the third pg_dump exclusion (goose_db_version_id_seq) is a proven no-op; two -T flags used
- [Phase ?]: PendingMigrations/AppliedMigrations kept version-derived, not Status-derived, to preserve a non-tautological 44-version census
- [Phase ?]: INV-STORE-1 timestamp scan excludes goose_db_version by exact name, guarded by an existence assertion
- [Phase ?]: MigrationSectionForTest errors on a missing goose Up/Down annotation rather than falling back to the whole file — the fallback is the destructive bug
- [Phase ?]: runMigrations' ctx made load-bearing via a pre-flight pool.Ping; store.Migrator's method set stays context-free by design
- [Phase ?]: Plan 01.1-02's 'zero golang-migrate mentions in internal/store' criterion rejected: all 15 remaining sites are deliberate historical rationale; replaced by a no-live-import gate
- [Phase ?]: 01.1-03: corpus census recorded as observation, never asserted — a legitimate new $$ migration must not turn the D-13 guard red
- [Phase ?]: 01.1-03: lint glob pins are static Taskfile-text reads, not shell-outs — a shell-out passes the fail-open glob it guards
- [Phase ?]: Clean branch taken: all five docker/docker [[IgnoredVulns]] retired — go mod why -m reports the main module does not need it, go mod graph has 0 edges (positive control: docker/go-connections matches 8x)
- [Phase ?]: Advisory ids deliberately NOT repeated in osv-scanner.toml's history comment, so a text search for them stays a usable is-it-suppressed check
- [Phase ?]: #4817 left OPEN with full evidence rather than closed — fix is on an unmerged branch; repo precedent (#4878, #4892) closes post-merge. PR should carry Closes #4817
- [Phase ?]: 01.1-04: adopt fires from Migrator.Up() only (option-a); read-only verbs stay read-only
- [Phase ?]: 01.1-04: INV-STORE-10 (ascending adopt seed order) shipped binding: bound; meta-test proven to resolve Ginkgo blocks
- [Phase ?]: INV-STORE-11 shipped binding: pending — registry meta-test accepted bound, declined on human review because the registration clause is vacuous over a zero-Go-migration corpus (holomush/holomush#4906)
- [Phase ?]: internal/store/migrations is now a Go package (D-08); //go:embed migrations/*.sql verified by execution to still resolve against a package directory
- [Phase ?]: task migrate:create deleted, not repointed at goose: goose's create -s numbers 5 digits (%05v), the corpus is 6 (migrate_embed_test.go)
- [Phase ?]: scripts/bootstrap-migrations.sql retired as a comment-only banner, not deleted — an old runbook link lands on the explanation
- [Phase ?]: D-16 rehearsal and D-18 rollback are WRITTEN, not EXECUTED — recorded as such in the SUMMARY and the WINDOWS ledger
- [Phase ?]: 02-01: Task 1 checkpoint auto-selected generate-into-repo (auto_advance=true, gate=blocking not blocking-human); no new module — golang.org/x/text promoted indirect->direct
- [Phase ?]: 02-01: Unicode 17.0.0 security data lives at /Public/17.0.0/security/, NOT /Public/security/17.0.0/ (the plan's URL 404s; /Public/security/ tops out at 16.0.0)
- [Phase ?]: 02-01: migration 000001_baseline.sql:397 seeds a bootstrap character (TestChar), so EVERY stock database is D-30-unverifiable until the backfill — proven by its own integration spec, not argued
- [Phase ?]: 02-01: adding a migration reddens THREE untagged-lane constants the plan did not enumerate (migrate_embed_test expectedMigrationCount, migrate_test census list + latest-version mock) on top of the integration-tagged pair B-5 flagged
- [Phase ?]: 02-01: a machine-readable // Deprecated: on world.NormalizeCharacterName is unlandable before its caller migrates — staticcheck SA1019 fires at internal/auth/character_service.go:105, which this plan must not touch; prose notice used instead
- [Phase ?]: 02-01: this repo has NO umbrella 'task generate' target, only generate:* subtasks; generate:confusables deliberately kept out of pr-prep's drift block because it fetches over the network
- [Phase ?]: Mixed-script CJK families matched by containment, not Latin-required — {Han,Hiragana} and {Han,Hangul} are ordinary non-Latin names
- [Phase ?]: The charname/auth separation guard is directional and file-scoped; a package-wide ban would be RED by design when 02-06 lands Gate.Admit
- [Phase ?]: gen-confusables now gofmts its output — task fmt:check was red at HEAD and the generator fought task fmt
- [Phase ?]: 02-03: viewer.roles resolves PER PLAYER (union across the player's characters), matching 01-SPEC §10.5 and the shipped PlayerHasRole join — so web and operator socket cannot disagree about who is an admin
- [Phase ?]: 02-03: the anonymous viewer rung is exempt from the empty-identifier panic (viewer:anonymous is a complete subject, not a bare prefix) and panics instead on a NON-empty identifier
- [Phase ?]: 02-03: viewer namespace ships FIVE keys, not §8.4.1's three — roles/has_roles added so seed:viewer-property-admin-read is expressible at all (Amendment F for plan 02-11)
- [Phase ?]: INV-WORLD-6's registry summary was corrected to enumerate BOTH sanctioned tombstone-emitting deleters (world.Service.DeleteCharacter and auth.CharacterReapingService) in the same change that bound it — the shipped 'ONLY path' wording was already false and binding it would have written a fabricated guarantee no meta-test can catch.
- [Phase ?]: The empty world.Status zero value is refused and logged at the SelectCharacter call site; world.Selectable's default arm stays deny. Partial projections feeding the selection path were widened instead of softening the predicate.
- [Phase ?]: 02-05: the policy.Cache read barrier is deliberately NOT mirrored for the block list — it would be harmful, not merely dead (no in-process writer; blocking readers would turn poll latency into admission latency while the last valid list is fully valid)
- [Phase ?]: 02-05: the block-list poll indicator is the pair (updated_at, md5(value)) — a bare updated_at poll never observes v0.13's only edit path (direct SQL), which leaves updated_at untouched
- [Phase ?]: 02-05: blocklist.Load does its own strict decode instead of settings.StringSliceN, which collapses absent and unparseable into one (nil,false) and would silently disable the whole list on one malformed direct-SQL edit
- [Phase ?]: 02-05: production hands charname.Gate the live *blocklist.Cache, never a *Snapshot — a snapshot captured at construction freezes the list for the process lifetime with no failing test and no log line
- [Phase ?]: 02-13: derived property peers use the no-widening direction (D-27) — ALL on the permit side (owner_player_id, visible_to_players), ANY on the forbid side (excluded_from_players); the plain player union was proposed for both and declined for the permit side
- [Phase ?]: 02-13: PlayerRoles lands on the concrete *PostgresRoleStore and reaches the ABAC stack via the ABACConfig.PlayerRoleLookup func field — store.RoleStore's interface is unchanged
- [Phase ?]: 02-13: ViewerTierProvider is now registered in BuildABACStack and both new ABACConfig seams are populated at the production composition root (subsystem.go), so principal.viewer.* resolves in production
- [Phase ?]: 02-06: charname.Admitted is an opaque single-constructor token — (*Gate).Admit — so an ungated characters.name write is not expressible in Go rather than merely caught by a census
- [Phase ?]: 02-06: Rename is envelope-atomic through the IN-PACKAGE OutboxStore and takes the intent as a parameter; it MUST NOT be routed through worldMutator.mutate() or two envelopes are emitted per rename
- [Phase ?]: 02-06: guardSkeleton derives its advisory-lock key with FNV-1a in Go, not Postgres hashtext, so every replica computes the same key (D-30 part 2)
- [Phase ?]: 02-06: census rule D counts METHODS, so guardSkeleton (takes a token, writes nothing) is not a name-admitting member; rules A and E still cover free functions
- [Phase ?]: 02-06: mapGateError REPLACES the oops code rather than wrapping it — oops resolves the DEEPEST chain code (#4902), so a wrap would silently change the caller-visible contract
- [Phase ?]: 02-06: NAME_EMPTY_NORMAL_FORM is mapped to CHARACTER_INVALID_NAME alongside NAME_INVALID_SYNTAX; both were CHARACTER_INVALID_NAME before the gate existed
- [Phase ?]: 02-06: a stock database is D-30-unverifiable (000001_baseline seeds a skeleton-less bootstrap character), so the integration harness and three suites repair the corpus at setup — every guest login exhausted its retries until they did
- [Phase ?]: 02-06: block-list window #10 CLOSED — all three composition roots call setup.NewCharacterNameGate, which fails closed on a nil BlockList subsystem
- [Phase ?]: 02-07: Task 2 checkpoint auto-resolved to author-now-gate-on-audit — D-11's posture is already LOCKED in 02-CONTEXT.md; the phase MUST NOT merge before 02-AUDIT-RESULT.md exists and is non-empty
- [Phase ?]: 02-07: the plan's D-29 grep criterion (zero 'resource is character' in seed.go) is unsatisfiable — 3 pre-existing shipped seeds match. Replaced with a compiled-target gate pinning the character-typed seed set
- [Phase ?]: 02-07: createSeedEngine cannot delegate to abactest (import cycle in an in-package test file). The B-6 closure is bought by an external test package in the same directory instead
- [Phase ?]: 02-07: D-03 ships TWO tier-floor policies, not three — the player rung has no seeded §8.6 member and the DSL list grammar forbids an empty 'in []'. Guarded by a conditional re-entry test
- [Phase ?]: 02-08: AttributeVisible does NOT short-circuit on a term-A denial — both terms always evaluate, so 'exactly two evaluations' is unconditional and a term-B infra failure cannot be masked as an ordinary withheld (§8.10)
- [Phase ?]: 02-08: §8.7's not-found-equivalent is signalled as ErrProfileUnreachable, a distinct Go outcome from ErrEvaluationFailed — a caller that cannot tell them apart renders an outage as a missing character; the wire-level indistinguishability binds in Phase 4
- [Phase ?]: 02-09: gate-then-distinguish — the ABAC evaluation runs BEFORE the registry lookup, so DENY_ADMIN_SECTION vs DENY_ADMIN_SECTION_UNREGISTERED is an admin diagnostic, not an enumeration oracle (D-06); pinned by INV-PRIVACY-11, bound to a differential string-equality assertion demonstrated RED against a lookup-first ordering
- [Phase ?]: 02-09: Descriptor.Action is enforced as a declared MAXIMUM operation class through a CLOSED rank ladder (read<write); an action off the ladder is refused rather than ranked zero, and the check sits AFTER the gate so the declare-write distinction stays invisible to a denied caller
- [Phase ?]: 02-09: section.ValidateAtBoot is step 1 of BootstrapSubsystem.Prepare (before the orphan check, no DB needed) — without the call site D-09 would have been satisfied by a unit test only and a zero-valued descriptor would ship
- [Phase ?]: 02-09: error codes are inline oops.Code literals (adminauth precedent) rather than exported constants, forced by the plan's line-ordering criterion; taxonomy documented on AssertSectionAccess for Phase 4/6
- [Phase ?]: 02-09: ADMIN_SECTION_EVALUATION_FAILED added under Rule 2 (§8.10 infra failure must not be flattened into a denial) but is UNTESTED — the suite uses only the real engine; Phase 4 owns the seam
- [Phase ?]: 02-12: migration 000055's Down is a real revert (ClearCharacterIdentity), not an error-returning stub — an erroring Down makes every version below 55 unreachable and wedged 15 in-tree specs
- [Phase ?]: 02-12: Go migrations are invisible to the .sql-only embed glob, so internal/store/go_migration_census.go merges them into the version helpers — without it the adopt gate seeds a goose ledger with a hole at 55
- [Phase ?]: 02-12: the UNIQUE index is isolated with direct INSERTs, not through CharacterService.Create — the ExistsByNormalizedName pre-check and 02-06's advisory lock both sit above it and are present in either schema
- [Phase ?]: 02-12: INV-WORLD-4's 'exactly TWO sanctioned out-of-world writers' is now false (the operator rename CLI is a third); the registry text amendment is owned by plan 02-11
- [Phase ?]: 02-10 Task 2 (checkpoint:decision, gate=blocking) — evidence-recording scheme for the PROFILE-11 exposure audit: SANITIZED-LEDGER. Maintainer-selected, not auto-selected. 02-AUDIT-RESULT.md carries one ledger line per adjudicated row (stable row id, md5 content digest, character length, verdict) plus the five aggregate result sets; the detailed text is read from an operator-only report generated outside the repository and deleted. Explicitly confirmed: NO player-authored text is to be committed to this repository — no entity_properties value, no characters.description text, no character or player name, and no truncated or representative excerpt. Hashes and lengths only. Rejected: private-detailed-record (splits the phase's evidence across two homes, one outside the review the phase is gated on).
- [Phase ?]: 02-10 Task 4 (checkpoint:decision, gate=blocking) — remediation of the PROFILE-11 exposure audit: NO-REMEDIATION-REQUIRED. Maintainer-selected, not auto-selected. Rows in scope: none — the property ledger and the description ledger are both 0 rows and entity_properties is empty in its entirety. Enumerated remediate row ids: none. Prior-value capture and rollback: not applicable, because no row is written. Basis: the ledger actually produced by kopia snapshot 7e48a9b592c2e0d302a5da3cf0171835, not an assumption about it — which is the one condition the option's own text attaches. Task 4b therefore performs no write, creates no 02-REMEDIATION.sql, and touches no database.
- [Phase ?]: abac-reviewer gate is OUTSTANDING and blocking: it is a repo-owned sub-agent and the 02-11 executor had no agent-dispatch tool; no verdict was fabricated
- [Phase ?]: INV-WORLD-4 amended TWO -> THREE out-of-world writers (02-12's rename CLI), an edit to an existing entry rather than a renumber
- [Phase ?]: D-03's tier-floor count verified against seed.go (2 == 2) and 02-CONTEXT.md deliberately NOT edited — a check that self-heals is not a check
- [Phase ?]: 01-SPEC §8.5.1.1 option 2 recorded REJECTED; D-01's five viewer twins are the settled shape
- [Phase ?]: 02-RESEARCH.md's stale schema section deliberately NOT rewritten — a dated research record is annotated, never rewritten

### Pending Todos

None yet.

### Blockers/Concerns

- Forums (Epic 11, `holomush-djj`) has no design yet — blocks any Forums-integration forward work
- Discord integration (Epic 12): Channels prerequisite shipped in v0.11; still blocked on an OAuth substrate that does not yet exist
- 259/334 registered invariants are `binding: pending` (concentrated in INV-CRYPTO and INV-SCENE) — tracked
  epic `holomush-hz0v4`, not a blocker, but phases touching crypto/scenes should bind relevant invariants as
  part of their own definition of done

- Operator action outstanding: ruleset 11923801 unchanged; no coverage context gates merges (#4875, #4876)
- 02-07: PROFILE-11's characters.description half is NOT discharged in Phase 2 — D-29 defers seed:profile-public-read-character to Phase 4, to land with the characterToProto projection narrowing. EXT-07's admin section registry is still 02-09's.
- BLOCKING pre-merge: abac-reviewer (/holomush-dev:review-abac) has NOT reviewed the Phase 2 diff. D-05 makes it mandatory. Brief is written verbatim in 02-11-SUMMARY.md; owner is the orchestrator/human.
- MAINTAINER DECISION: ROADMAP success criterion 4 — the in-world-description half is deferred to Phase 4 by D-29. Three options stated in 02-11-SUMMARY.md, none selected. Criterion 1 is settled by D-30 and MUST NOT be touched.

### Quick Tasks Completed

| # | Description | Date | Commit | Status | Directory |
|---|-------------|------|--------|--------|-----------|
| 260709-sqg | Fix holomush-9hygy — convert core-channels migrations TIMESTAMPTZ→BIGINT epoch-ns (lint:no-timestamptz ship blocker) | 2026-07-10 | 1284ba341 |  | [260709-sqg-…](./quick/260709-sqg-fix-bead-holomush-9hygy-convert-core-cha/) |
| 260711-hg1 | GH-4785 (F2): cap gateway ConnectRPC request-body size (`WithReadMaxBytes` 4 MiB + `ReadTimeout`) to prevent unauthenticated OOM | 2026-07-11 | 0e3806ebf |  | [260711-hg1-…](./quick/260711-hg1-gh-4785-cap-gateway-connectrpc-request-b/) |
| 260728-sec | Add root `SECURITY.md` — vulnerability disclosure policy pointing at the repo's already-enabled GitHub private vulnerability reporting | 2026-07-28 | (this commit) |  | [260728-sec-…](./quick/260728-sec-add-security-md-vulnerability-disclosure/) |
| 260730-wh1 | GH-4892: replace the drifted dependency-only exempt path list with shape-based globs anchored in `vars.DEPENDENCY_ONLY_PATHS`; exclude the E2E compose files, define the lockfile subset, qualify the `scripts/**` claim at all five sites | 2026-07-30 | 9cb8d8f53 |  | [260730-wh1-…](./quick/260730-wh1-fix-gh-issue-4892-dependency-only-exempt/) |
| 260731-ea8 | GH-4890: implement the issue-first gate enforcement workflow — `Issue Gate` applies `gate-violation`, comments, and fails a check when a PR's linked issue lacks a gate label; never closes a PR. Exemption decided by git `:(glob)` pathspec against the Taskfile vars, plus a new `REPO_CONFIG_ONLY_PATHS` | 2026-07-31 | ddb99158f | Needs Review | [260731-ea8-…](./quick/260731-ea8-implement-the-issue-first-gate-enforceme/) |

### Roadmap Evolution

- Phase 01.1 inserted after Phase 1: Migration framework: adopt goose for Go migrations — Phase 2 execution gated on it (URGENT)
- Phase 01.1 edited: goal, requirements, gates, success criteria, UI hint, research flag
- Phase 01.1 edited: edited fields: success_criteria (criterion 1: 49->44 pairs, 'byte-identical' restated as application-schema with bookkeeping objects excluded per D-15; criterion 4: 'real migration' restated as integration-test fixture chain per D-07), research_flag (49->44, baseline question marked resolved per D-02/D-04)
- Phase 01.1 edited: criterion 1 row-count corrected post-research: 'goose_db_version with 44 rows' -> '44 rows at version_id > 0 plus goose's version-0 bootstrap row (45 total)'. goose inserts a version-0 row at table creation (RESEARCH.md:875-877, verified by execution against postgres:18-alpine), so the prior assertion failed against a correct database.
- Phase 01.1 edited: criterion 3 amended post-research (maintainer decision): 'up/down/status/version/force parity' -> 'up/down/status/version parity, force REMOVED with docs+tests'. goose commits body and version row in one transaction (provider_run.go:213-219), so the dirty state force repairs cannot arise; force has no analogue and no purpose (RESEARCH.md:777).
- Phase 2 edited: criterion 4 split: public-properties half stays Phase 2, in-world-description half deferred to Phase 4 as its criterion 6 (D-29)
- Phase 3 edited: scope narrowed: RenameCharacter removed from phase and milestone, moved to backlog 999.20 (linked to 999.6); goal, depends-on, requirements (IDENT-03 dropped), success criteria 1-2, and sketch-findings all amended; rationale in 03-CONTEXT.md D-44
- Phase 3 edited: D-42 resolved: last_active_at lands here via NATS JetStream KV buffer + periodic flush in its OWN general-purpose subsystem (not the retirement reactor); added success criterion 5; phase now adds two subsystems (18->20)
- Phase 02.1 inserted: Background-Job Authorization Model inserted after Phase 2; Phase 3's reactor blocked on it. Three candidate authz models examined and rejected (synthetic system: principal unnarrowable per engine.go:542-548; envelope-actor propagation over-grants; WithSystemSubject bypasses the chokepoint).
- Phase 3 edited: edited fields: depends_on (added Phase 02.1 — retirement reactor cannot authorize MoveCharacter without the job-identity model; D-45 superseded)
- Phase 02.1 edited: edited fields: requirements (AUTHZ-01 minted), shape (JobCaller deferred to 02.2 per D-62)
- Phase 02.2 edited: edited fields: requirements (AUTHZ-02 minted)
- Phase 02.1 blast-radius numbers superseded by measurement: ROADMAP §02.1's 'Verified blast radius (grep-confirmed, do not re-estimate)' block is WRONG in four load-bearing ways; 02.1-RESEARCH.md §'Verified Blast Radius — corrections' (2026-08-08, rg+ast-grep reproducible) supersedes it and is authoritative for planning: 23 public Service methods not 21 (checkAccess appears 23x, 1:1); 5 interfaces + 1 callback type + 3 supporting signatures not 3 (command/types.go has 15 not '9+'; hostfunc.WorldService, property.WorldMutator, property.EntityMutator, property.Definition.Set were unnamed); NO mockery regeneration is needed (no generated mock carries subjectID) — the real work is ~106 hand-rolled test-double method declarations across 13 files; 31 arg-2 production call sites not 47, and ~332 true test sites not 347 (two codemod classes, not one rule). Also: CONTEXT.md D-65 cites internal/world/world_envelope_census_test.go; the census test actually lives at test/meta/world_envelope_census_test.go. NOTE: no gsd-tools verb exists to rewrite an existing ROADMAP phase section (phase verbs are uat-passed/next-decimal/add/add-batch/insert/remove/complete/list-plans; roadmap verbs are analyze/get-phase/update-plan-progress/annotate-dependencies/validate/upgrade), so the stale prose is recorded as superseded here rather than hand-edited.

## Deferred Items

Items acknowledged and carried forward from the ingest, not part of this roadmap:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Social-spaces | Forums integration (Epic 11) | No design yet | Ingest 2026-07-07 |
| Social-spaces | Discord/Slack bridging + OAuth linking (Epic 12) | Blocked on Channels + OAuth substrate | Ingest 2026-07-07 |
| Web portal | Non-scene web surfaces (building/world editing, admin UI) | Directional theme goal, not yet spec'd | Ingest 2026-07-07 |

## Session Continuity

Last session: 2026-08-08T14:40:37.515Z
100% coverage validated (no orphans, no duplicates). Phase numbers **restart at 1 per milestone as of
v0.13** (v0.11 Phases 1–3 and v0.12 Phases 4–9 keep their old continuous global numbers).
Roadmap follows `research/SUMMARY.md`'s proposed 6-phase decomposition. Nothing executed yet.
Stopped at: Phase 02.1 context gathered

Previous session: 2026-07-27T16:45:13.288Z
Phase 09 closed: all 21 plans executed, shipped as PR #4874 on `gsd/v0.12-milestone`.
The measurement-chain repair is proven — the e2e flag reports 32.27% where it reported 0.0, and
project coverage rose 0.83 points to 79.11% against a 78.28% base (`497748c6d`). Two named floors
remain short, `cmd/holomush` at 70.09% against 80% being the consequential one (#4861). QUAL-04 is
Complete; QUAL-02, QUAL-03 and QUAL-05 stay Pending deliberately. Both halves of decision D-04 are
deferred and ruleset 11923801 is unchanged: `codecov/project` has never posted (0 of 64
observations — a codecov Team-plan limit, #4875), and requiring `codecov/patch` would block
docs-only pull requests, which `paths-ignore` routes to a lane that uploads no coverage (#4876).
Archived — Phase 8: all 9 plans executed, CoreServer 1891 → 657 LoC and plugin Manager 1876 → 702;
shipped as PR #4832 with follow-ups #4828, #4829, #4830, #4831.
Stopped at: Completed 09-19-PLAN.md — phase 09 final plan; all 21 plans executed
Resume file: .planning/phases/02.1-world-caller-model/02.1-CONTEXT.md

## Operator Next Steps

- Merge PR #4874 (Phase 9 — test-quality & code-health sweep). All 18 CI checks green.
- **Operator-only, cannot be done from a PR:** decide the ruleset `11923801` required-check
  question. Both halves of decision D-04 are currently deferred (#4876) — `codecov/project` has
  never posted (codecov Team-plan limit, #4875) and requiring `codecov/patch` would deadlock
  docs-only PRs, which `paths-ignore` routes to `ci-docs-skip.yaml` with no coverage upload.

- Two named coverage floors remain unmet and are tracked, not waived: whole-project 79.11% vs
  the 80% target, and `cmd/holomush` 70.09% vs 80% (#4861 — the remainder is `runCoreWithDeps`
  boot branches needing live Postgres/NATS/TLS).

- QUAL-02, QUAL-03 and QUAL-05 stay Pending deliberately; QUAL-04 is Complete.
