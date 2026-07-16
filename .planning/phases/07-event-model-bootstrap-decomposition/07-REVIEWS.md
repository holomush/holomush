---
phase: 7
round: 8
reviewers: [codex, opencode]
reviewed_at: 2026-07-16T21:42:57Z
plans_reviewed: [07-01-PLAN.md, 07-02-PLAN.md, 07-03-PLAN.md, 07-04-PLAN.md, 07-05-PLAN.md, 07-06-PLAN.md, 07-07-PLAN.md, 07-08-PLAN.md, 07-09-PLAN.md, 07-10-PLAN.md, 07-11-PLAN.md]
plans_revision: "rev 10 (f63a3f33c)"
notes: "Round 7 preserved at 61a2071b2. FOCUSED round: reviewers received all 11 plans plus the rev-10 unified diff and a depth directive on 07-07/07-09/07-10/07-11 (the rev-10 change surface) with can-go-green, implementability, cross-plan-composition, and regression checks. No prior-round findings were shared. opencode: openrouter/x-ai/grok-4.5; codex: default model. Orchestrator verified every HIGH against source before the consensus."
---

# Cross-AI Plan Review — Phase 7 (Round 8, rev 10, focused)

## Codex Review

## Summary

Revision 10 materially improves all four changed plans, especially around actor-ID preservation, wrapped publication, lifecycle phase state, and the EventBus dependency graph. However, it is not yet execution-ready. I found one operational compatibility risk in 07-09 that could strand existing event history, one acceptance criterion in 07-11 that correct code cannot satisfy, and an incomplete audit rollback model. Overall verdict: **HIGH risk until those items are resolved**.

## Strengths

- **07-07 correctly preserves the zero-actor contract.** The deleted adapter intentionally maps a zero ULID to `""` in [cmd/holomush/sub_grpc.go:976](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:976), whereas directly calling `ULID.String()` would expose an all-zero 26-character ID. Requiring zero-aware helpers in both `hostfunc` and `hostcap` covers the Lua and binary-plugin paths symmetrically. The proposed implementation also follows the existing helper pattern in [internal/grpc/server.go:702](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/grpc/server.go:702).

- **07-07’s wrapped-publisher constraint closes a real silent-audit failure.** Production currently passes the result of `wrapPublisher` onward in [cmd/holomush/sub_grpc.go:264](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:264). That wrapper stamps `App-Rendering` in [internal/eventbus/rendering_publisher.go:58](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/rendering_publisher.go:58), while the audit projection rejects events lacking it in [internal/eventbus/audit/projection.go:382](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/projection.go:382). An integration assertion against `events_audit` is much stronger than a wiring grep.

- **07-09 fixes an unimplementable crypto-wiring instruction.** The current straight-line block constructs `cryptoPolicySub`, `rekeyCheckpointSweepSub`, and `cryptoChainVerifierSub` before `StartAll` at [cmd/holomush/core.go:738](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:738), [core.go:774](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:774), and [core.go:1053](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:1053). Revision 10 correctly keeps those constructions outside the memoized builder and defers only resource resolution.

- **07-10 now composes correctly with 07-09’s graph change.** Database currently has no lifecycle dependencies ([internal/store/subsystem.go:42](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/store/subsystem.go:42)), so `EventBus → Database` is acyclic. Conversely, the planned verifier’s real EventBus dependency makes `EventBus → CryptoChainVerifier` invalid. Exact-set testing plus named `Database before EventBus` and `EventBus before Verifier` assertions accurately encode both facts.

- **07-11’s audit phase-state correction is necessary and implementable.** The current guard checks `s.worker` before preparation in [internal/eventbus/audit/subsystem.go:267](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:267), but `worker` is assigned only after `p.start` at [subsystem.go:313](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:313). Separate prepared and activated fields correctly model the new lifecycle phases.

- **The guest-reaper timing seam is well grounded.** Only the session reaper currently has an injectable interval; the guest reaper hardcodes one minute in [cmd/holomush/sub_grpc.go:760](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:760). Adding a default-preserving `GuestReaperInterval` makes the two-reaper placement test practical without changing production timing.

## Concerns

- **HIGH — 07-09 introduces an unplanned EventBus namespace migration.** Today the EventBus independently defaults its `GameID` to `"main"` in [internal/eventbus/config.go:168](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/config.go:168), while the database initializes a persistent generated ID in [internal/store/subsystem.go:60](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/store/subsystem.go:60). Production compose does not pass an explicit game ID ([compose.yaml:28](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/compose.yaml:28)). Revision 10 makes the DB-derived provider override EventBus configuration, changing existing installations from `events.main.*` to `events.<db-ulid>.*`.

  History queries use the newly resolved ID to build an exact subject filter in [cmd/holomush/sub_grpc.go:920](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/sub_grpc.go:920). Consequently, pre-upgrade `events.main.*` history can become invisible after restart. The plan has no migration, dual-read compatibility, configuration-conflict policy, or upgrade regression test. It also silently overrides an explicit `event_bus.game_id`, contrary to current operator documentation in [configuration.md:367](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/site/src/content/docs/operating/reference/configuration.md:367).

- **HIGH — 07-11 contains an impossible “no Start methods” acceptance gate.** The raw directory-wide grep in [07-11-PLAN.md:1159](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:1159) allows only three non-subsystem exceptions. Correct code must retain additional `Start` methods, including the atomic admin server operation that this same plan explicitly requires `AdminSocketSubsystem.Activate` to invoke: [internal/admin/socket/server.go:58](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/server.go:58). It also catches the non-lifecycle `OrphanDetector.Start` at [internal/world/property_lifecycle.go:106](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/world/property_lifecycle.go:106). Correct implementation therefore cannot satisfy the criterion as written.

- **MEDIUM — 07-11’s audit rollback state remains incomplete.** Preparation runs `lateInit` and conditionally stores `s.cfg.Owners` and `s.pluginMgr` before projection construction in [internal/eventbus/audit/subsystem.go:300](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:300). Revision 10 adds prepared projection and partition-manager state, but only requires `Stop` to clear those new fields plus activated state. A Prepare failure or prepared-only rollback can leave stale owners/plugin consumers in the subsystem. A retry whose provider returns nil or changed data may reuse stale ownership and consumer configuration.

- **LOW — 07-10 retains contradictory revision text.** [07-10-PLAN.md:692](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-10-PLAN.md:692) still says the forbidden edge is pinned by a “nil-DependsOn() unit test,” immediately before the corrected exact `[SubsystemDatabase]` requirement. The executable criteria are otherwise clear, but this stale sentence can misdirect implementation or review.

- **LOW — 07-11’s caller census counts disagree.** Its must-have says approximately 27 callers/21 integration-tagged files, while the later acceptance text still says approximately 24/20 at [07-11-PLAN.md:1160](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:1160). The broad receiver-classified census is the right mechanism, but the conflicting counts weaken its auditability.

## Suggestions

- Resolve the EventBus game-ID compatibility policy before execution. Add tests for:

  - upgrading an existing `events.main.*` installation;
  - an explicit `event_bus.game_id` that differs from `core.game_id`;
  - a fresh installation with neither configured.

  Choose explicitly among preserving the configured EventBus ID, failing closed on conflicts, or providing a migration/dual-read strategy. Update operator documentation in the same plan.

- Replace 07-11’s raw `Start` grep with receiver classification against actual `lifecycle.Subsystem` implementations, matching the plan’s improved Prepare/Activate caller census. Alternatively, enumerate every legitimate non-subsystem `Start` API.

- Treat audit preparation as one aggregate state: projection, partition manager, owner map, and plugin-consumer manager. Clear it on every Prepare error and on prepared-only `Stop`. Add a prepare-fail/rollback/retry test with changed or nil `lateInit` output.

- Remove the stale nil-dependency wording from 07-10 and replace both approximate 07-11 caller counts with the exact Step-0 census result.

- Name the 07-07 `events_audit` regression test and its home explicitly. The full integration harness exposes command dispatch and the database pool through [session.go:89](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/testsupport/integrationtest/session.go:89) and [harness.go:1284](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/testsupport/integrationtest/harness.go:1284), making it suitable for a genuine end-to-end row assertion.

## Risk Assessment

**HIGH.** Revision 10 substantially strengthens the intended implementation, but 07-09 can silently change durable event namespaces on upgrade, and 07-11 has a literally unsatisfiable acceptance gate. The audit retry-state gap adds a further correctness risk. Once those three issues are resolved, the remaining design appears coherent and likely drops to medium implementation risk, driven mainly by the breadth of the atomic lifecycle-interface migration.
---

## OpenCode Review (grok-4.5)

# Cross-AI Plan Review — Phase 7, Rev 10 (focused re-check)

**Scope:** DEPTH on 07-07, 07-09, 07-10, 07-11 (rev 10 diffs). Other plans treated as settled context. Judgments from plans + live source only (no prior reviews).

---

## 1. Summary

Rev 10 is a strong, source-grounded correction pass: every major addition matches a real defect or unimplementable instruction in the current tree. **07-07** closes two real post-collapse behavior traps (zero actor ULID → `""`, and raw vs `wrapPublisher` publisher silent audit loss). **07-09** closes a **pre-existing** subject-qualification split (`events.main.*` vs `events.<db-ulid>.*`) that earlier “two-funnel” prose incorrectly treated as already true, and retracts an impossible “hoist whole `core.go:705–1060`” instruction that would try to construct lifecycle subsystems inside a memoized builder that runs after they must already be registered. **07-10** correctly re-pins EventBus deps to `[SubsystemDatabase]` so it does not fight 07-09. **07-11** correctly re-settles audit prepare state (the old `worker` guard cannot guard Prepare after the split) and makes the guest-reaper preparation test deterministic. All four rev-10 mechanisms look implementable against live source; no HIGH **can-go-green** failure found. Residual risk is execution blast radius (Wave A/B size), not flawed design of rev 10 itself.

---

## 2. Strengths

- **07-07 zero-ULID is a real, load-bearing contract.** `busEventToCoreEvent` maps zero ULID → `""` at `cmd/holomush/sub_grpc.go:976-979`. Deleting that hop without a replacement would turn zeros into the 26-char all-zero ULID string. Host package portal helpers (mirroring the intent of `internal/grpc/server.go:710-721`) are the right shape; do not invent public `eventbus` API.

- **07-07 wrapPublisher risk is real and currently correctly wired in prod.** Live path: `rawPublisher` → `wrapPublisher` → `busEventAppender{publisher}` → `WithEventStore` (`sub_grpc.go:264-282,522`). `RenderingPublisher` stamps `App-Rendering`; projection fails closed without it (`internal/eventbus/audit/projection.go:382-391`, header at `:36`). Ambiguous “underlying publisher” language would allow a compile-green, audit-silent regression. Rev 10’s “wrapped value + land in `events_audit`” gate is proportionate.

- **Harness already documents the same trap** (`integrationtest/harness.go:662-668`) — full-stack precedent for the intended production shape.

- **07-09 BLOCKER 1 (EventBus out of gameID funnel) is pre-existing and well-evidenced:**
  - Independent load: `core.go:136-141`
  - `Defaults()` empty → `"main"`: `internal/eventbus/config.go:28,172-173`
  - Freeze at construct: `subsystem.go:69-70`
  - Resolved process gameID: `core.go:300-302` from cfg/DB
  - Process already fighting the mismatch for DLQ only: `core.go:573-577`
  - EventBus still `DependsOn() nil`: `subsystem.go:76-77`  
  Putting EventBus on dual-path `GameIDProvider` + `DependsOn(Database)` is the minimal structural fix and makes rows 1–4' `bus.GameID()` closures actually share the process id.

- **07-09 BLOCKER 2 (cryptoWiring cannot construct subsystems) is correct.** `cryptoPolicySub` / `rekeyCheckpointSweepSub` / `cryptoChainVerifierSub` are built in the block region and must exist before `orch.StartAll` (~`core.go:1081`). A builder first run from a consumer’s `Start` cannot create them. Split “resolution logic in builder / constructions on straight-line with providers” is the only D-09-legal shape.

- **07-10/07-09 composition is fixed deliberately.** Rev 9’s “EventBus DependsOn stays nil” would go red against 07-09’s Database edge; rev 10 exact-set pin + database-before-eventbus named assertion is the right cascade (and still excludes `EventBus → CryptoChainVerifier`).

- **07-11 audit state diagnosis is exact.** Only guard is `if s.worker != nil` (`audit/subsystem.go:270`); `s.worker` is assigned only **after** `p.start` (`:313-322`). Partition manager is a local (`:293`) that Activate/retention also need. Phase-owned `preparedProjection` + `partitionManager` is necessary, not optional polish.

- **07-11 guest reaper testability is real.** Construction hardcodes `Interval: 1 * time.Minute` at `sub_grpc.go:765-769`. Without a config seam, “shortened interval” is false for guests. `GuestReaperInterval` is minimal and behavior-preserving (`NewGuestReaper` already tolerates interval defaults in `internal/auth/guest_reaper.go:39-42`).

- **Acceptance criteria lean on exact-set / reading / `task test:int`**, not magic counts for order proofs — consistent with earlier hard-won lessons.

---

## 3. Concerns

### HIGH

- **None new that would block green execution of rev 10 as written.** The two design-level hard problems (EventBus gameID split; cryptoWiring construction) are correctly settled and source-backed.

### MEDIUM

- **Wave A remaining complexity (07-09)** — rev 10 narrows incomplete design, but Task 2 is still the phase’s largest surface: multi-package provider migrations, THE RULE, dual-path configs, file moves, KEK E2E. Failure mode remains “subtle boot panic / wrong DependsOn,” not a missing sentence. Execution checksum (Step 0 census + `task test:int` KEK gate) must stay non-negotiable.

- **07-07 audit landing test must assert projection path, not only JetStream.** A test that only sees the event on Subscribe would still green with rawPublisher; row in `events_audit` is the right oracle (fail-closed without rendering). Criterion text is good — implementers must not weaken to “published on bus.”

- **07-09 EventBus `GameIDProvider` resolve timing vs consumers of `cfg` at construct.** `NewSubsystemWithStorage` freezes `cfg.Defaults()` at construct (still may store `"main"` until Start). Plan resolves into `s.cfg.GameID` at top of Start — correct for `GameID()` and post-Start emit paths; **any** leftover construct-time reader of unresolved bus config remains a landmine. Only known deliberate fight (DLQ) is already outside bus config (`core.go:573-577`). Worth a one-line SUMMARY note that **no** bus:config field other than the provider is used as the process authority for game id.

- **Dual-path `GameIDProvider` + kept string field** — easy for a future contributor to set only the string and forget the provider (or reverse), reintroducing split under partial wiring. Mitigated by runCore being the sole production assigner; a unit test “with provider set, `GameID()` after Start equals provider, not Defaults” would harden it beyond current greps.

- **07-11 prepared-only Stop path** is new surface. Contracts are right, but partial-prepare fault injection is deferred to SUMMARY verification — acceptable for a refactor phase, residual if `Stop` forgets to release `preparedProjection` / durable consumers constructed in Prepare.

### LOW

- **grpc already has a richer `actorIDString`** (`server.go:710-721`: zero → `""`, else IdentityRegistry name, else ULID string). Plan’s hostfunc/hostcap helpers intentionally stay display-only and simpler — fine, but name reuse can confuse agents; doc one-line “not the gRPC helper” is enough.

- **`GuestReaperInterval` default duplication** — plan defaults zero → 1m on the grpc config **and** `NewGuestReaper` already defaults interval (`guest_reaper.go:39-42`). Harmless; either layer alone is enough.

- **Test rename cascade** (`TestSubsystemDependsOnNothing` → exact `[Database]`) is mechanical; already called out and greppable.

- **Caller-both-phases greps in 07-11** remaining broad-`rg` + receiver classification is the right **method**, but still operator-burden heavy; no better approach available without a type-aware tool.

---

## 4. Suggestions

1. **07-09 GameIDProvider:** add a small unit test  
   `TestEventBusStartResolvesGameIDProviderOverDefaultMain`: Config{GameID:"", GameIDProvider: func() string { return "01FROMDB…" }}, Start (with test storage), assert `GameID() == "01FROMDB…"`. Pins BLOCKER 1 beyond wiring greps.

2. **07-07:** in SUMMARY, explicitly paste both (a) `wrapPublisher` call site wire and (b) one `events_audit` row assertion output — same pattern as 07-08 Spec A intersection evidence.

3. **07-11:** when implementing row-10 Stop prepared-only path, keep drain/timeout behavior parity with the activated path’s existing rollback branch (`subsystem.go:330-339` style: bounded drain, nil fields) so prepared-only doesn’t leak JetStream consumers.

4. **Do not** reintroduce “verifier before EventBus” narrative anywhere after 07-09/10 — rev 10 taxonomy is: EventBus before verifier (data flow); verifier before gRPC/admin socket (domain bind). Keep those two arrows distinct in SUMMARY tables.

5. **Unchanged plans:** no composition defect found with rev 10. 07-05/06/07 already funnel through `bus.GameID()`; 07-09 row 13 is what makes that funnel honest. No action on 01–06/08 beyond executing as written.

---

## 5. Risk Assessment

| Dimension | Level | Why |
|-----------|--------|-----|
| **Rev-10 design correctness** | **LOW** | Source-verified; unimplementable instructions retracted; criteria satisfiable |
| **Cross-plan composition** | **LOW–MEDIUM** | 07-09→07-10 DependsOn/topo pin cascade fixed; 07-05/06/07 emit funnels now depend on successful EventBus GameID resolution |
| **Execution risk (Wave A/B size)** | **MEDIUM–HIGH** | Unchanged by rev 10’s quality — still multi-subsystem topological surgery; KEK E2E + THE RULE + acyclicity tests are the real safety net |
| **Silent behavior drift (ARCH-04/03)** | **MEDIUM if gates skipped; LOW if followed** | Zero-ULID + raw publisher + audit Prepare guard are exactly the “compiles and lies” class; rev 10’s greps/tests target them well |

**Overall: MEDIUM** — not because rev 10 is weak (it is strong), but because Phase 7’s remaining plans (especially 07-09 + 07-11) still concentrate large, partial-order, multi-package refactors where a single missed provider edge is a production boot failure. Rev 10 **reduces** that risk relative to rev 9; it does not remove the inherent size risk.

---

## Phase-goal check (rev 10 vs ARCH-03/04/05)

| Goal | Impact of rev 10 |
|------|------------------|
| **ARCH-04 single Event** | 07-07 keeps collapse direction; adding zero-aware helpers + wrapPublisher discipline preserves wire/audit behavior that pliers could otherwise “fix” wrong |
| **ARCH-03 Orchestrator bootstrap** | 07-09/10/11 still deliver handles-then-Prepare/Activate; EventBus→Database edge is necessary for honest game_id and is cyclically safe |
| **ARCH-05 gateway boundary** | Unchanged by rev 10 (settled in 01–04) |

**Verdict:** Ship rev 10 plans as executable. No REQUIRED rework of the four changed plans; suggestions above are hardening, not blockers.

---

## Consensus Summary

Orchestrator verification: every HIGH was checked against live source before this summary; verdicts below are grounded, not vote-averaged. The two reviewers diverged on headline verdict (Codex: HIGH risk until resolved; OpenCode: "ship rev 10 as executable", overall MEDIUM) — the divergence is the familiar depth/breadth split, and source verification sides with Codex on both of its HIGHs, with corrections to one framing.

### Verified findings (orchestrator-confirmed against source)

1. **VERIFIED BLOCKER (can-go-green) — 07-11's "no `Start` compatibility shim" sweep is impossible on correct code** (Codex HIGH; OpenCode missed). The criterion sweeps `internal/admin/` and `internal/world/` and allows only three named non-subsystem exemptions, but correct post-migration code must retain `Server.Start()` (`internal/admin/socket/server.go:60`) — which this same plan mandates stays unmodified and is invoked from `AdminSocketSubsystem.Activate` — and `OrphanDetector.Start` (`internal/world/property_lifecycle.go:108`), a non-subsystem. Same defect class as round 6's impossible repo-wide grep. **Fix (rev 11):** extend the exemption list (Server.Start, OrphanDetector.Start — and re-run the sweep post-fix to catch any other legitimate survivor) or scope the sweep by receiver classification exactly like the plan's own caller census does.

2. **VERIFIED — 07-09's gameID closure needs an operational disposition + a factual correction** (Codex HIGH, framing corrected; OpenCode accepts the flip as correct divergence-closure). The flip of unset-`game_id` installs from `events.main.*` to `events.<db-ulid>.*` is deliberate and documented in-plan as closing a pre-existing divergence (07-09:105, :471) — "unplanned migration" is too strong. But three gaps are real:
   - **Pre-upgrade bus-side history**: `ReplayTail` builds exact-subject queries from the resolved id (`cmd/holomush/sub_grpc.go:920`); after the flip, pre-upgrade `events.main.*` rows (JetStream + `events_audit`) are unreachable from history queries. The plan has no upgrade note, accept-and-document disposition, or sandbox migration pointer.
   - **Silent override of a live koanf key**: `event_bus.game_id` is loaded on the boot path (`internal/eventbus/config.go:42`, `cmd/holomush/core.go:136-137`); "provider wins when non-nil" silently discards an explicitly-set value. The documented global `game_id` key keeps its semantics (explicit value wins inside the provider), so Codex's configuration.md:367 contradiction claim is overstated — but the undocumented-but-live `event_bus.game_id` deserves an explicit disposition (warn-on-conflict log line, or documented-dead note).
   - **False in-plan claim**: 07-09's EventBus row says the koanf `GameID` field serves "standalone tools/tests… not boot-path live reads" — factually wrong; `core.go:136-137` loads it on the boot path today. The claim must be corrected so the executor doesn't reason from it.
   **Fix (rev 11):** one disposition paragraph + a warn-on-conflict (or documented-dead) criterion + correct the claim. Not a design reversal; the provider mechanism itself is verified implementable.

3. **VERIFIED MEDIUM — audit Prepare rollback state is incomplete** (Codex MEDIUM; OpenCode suggestion 3 is the complementary half). `lateInit` writes `s.cfg.Owners` / `s.pluginMgr` (`internal/eventbus/audit/subsystem.go:304-312`) before the fallible projection construction; rev 10 requires Stop to clear only `preparedProjection`/`partitionManager` + activated state. A failed/rolled-back Prepare leaves stale owners/plugin-consumer state; prepared-only Stop should also release durable consumers with the bounded-drain parity OpenCode cites (`subsystem.go:330-339` style). **Fix (rev 11):** add the lateInit-written fields to the cleared set + drain parity for the prepared-only path.

4. **VERIFIED LOW — 07-10:692 stale sentence** (Codex LOW). "Pinned by Task 2's nil-`DependsOn()` unit test" survives two lines above the corrected exact-`[SubsystemDatabase]` criterion. Delete/reword the stale clause.

### Agreed Strengths (both reviewers, source-cited)

- 07-07's zero-actor helpers preserve a real contract (`sub_grpc.go:976-979` maps zero → `""`); helper shape mirrors `internal/grpc/server.go` precedent; covers Lua + binary symmetrically.
- 07-07's wrapPublisher constraint closes a real silent-audit regression (`rendering_publisher.go:58` stamps `App-Rendering`; `audit/projection.go:382` fails closed without it); the `events_audit`-row oracle is the right assertion.
- 07-09's BLOCKER-2 retraction is correct: the three crypto subsystem constructions (`core.go:738/774/1053`) must stay on the straight-line pre-StartAll path; resolution-only inside the memoized builder is the only D-09-legal shape.
- 07-09→07-10 composition is now deliberate: `EventBus → Database` is acyclic (Database has no deps, `internal/store/subsystem.go:42`), exact-set pin + database-before-eventbus assertion encodes it, `EventBus → CryptoChainVerifier` prohibition preserved.
- 07-11's audit phase-state diagnosis is exact (`subsystem.go:267` guard vs `:313` assignment) and the guest-reaper seam is minimal and behavior-preserving (`sub_grpc.go:765-769` hardcode today).

### Divergent Views

- **Headline verdict**: Codex HIGH-until-resolved vs OpenCode ship-as-executable. Resolution: rev-10's design content is verified sound (OpenCode is right about that), but two of Codex's findings are verified defects in criteria/disposition text that would misdirect execution — so one more scoped rev is warranted before execute. Both reviewers independently rate residual execution risk MEDIUM(+) driven by Wave A/B size, not design.
- **OpenCode hardening items not in Codex** (fold in as criteria polish where cheap): `TestEventBusStartResolvesGameIDProviderOverDefaultMain` unit pin; SUMMARY note that no construct-time bus-config reader is a gameID authority; dual-path partial-wiring pin; wrapPublisher SUMMARY evidence paste; distinct "EventBus before verifier (data flow)" vs "verifier before gRPC/admin (domain bind)" arrows kept separate in SUMMARY tables.

### Recommendation

**Rev 11, scoped to 07-09 (disposition + claim fix), 07-10 (stale sentence), 07-11 (sweep exemptions + rollback fields)** — plus the cheap OpenCode hardening pins — then `/gsd-execute-phase 07`. No design reversals; all four fixes are text/criterion-level. 07-07 needs no change (both reviewers found its rev-10 content correct; OpenCode's two 07-07 items are SUMMARY-evidence guidance for the executor, not plan changes).
