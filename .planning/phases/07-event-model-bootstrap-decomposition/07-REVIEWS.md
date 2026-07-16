---
phase: 7
round: 4
reviewers: [codex, opencode]
reviewed_at: 2026-07-16T01:39:56Z
supersedes: ce525400f
reviewed_commit: 9e5d29db8
reviewer_models:
  codex: default (codex-cli 0.144.4)
  opencode: openrouter/x-ai/grok-4.5
reviewer_grounding:
  codex: grounded — all findings carry path:line
  opencode: grounded — hand-computed the topo order from live IDs + tie-break
antigravity: SKIPPED — round 2 proved it ungrounded
verdicts:
  codex: HIGH — NOT APPROVED (3 execution blockers)
  opencode: MEDIUM — not approved as security-self-certified on T-07-51/Wave A
plans_reviewed: [07-01, 07-02, 07-03, 07-04, 07-05, 07-06, 07-07, 07-08, 07-09, 07-10, 07-11]
---

# Cross-AI Plan Review — Phase 7 (Round 4)

> **Round 4, reviewing rev 6 (`9e5d29db8`).** Round 3 (`ce525400f`) found 5 blockers in rev 4 —
> which had passed the `gsd-plan-checker` with **0 blockers**. Rev 5 answered them; a FULL checker
> pass then found 6 broken-regex warnings → rev 6, after which every gate went green again.
>
> Round 4 existed to review rev 5's **self-certified design decisions** — a deleted security-gate
> edge, a threat downgraded by the agent that needed it low, a rejected reviewer finding, two rows
> settled by retreat, and a 2.3× manifest expansion. None had ever been seen by an external reviewer.
>
> **It found 4 more blockers.** Prior-round feedback was withheld; both reviewers re-derived
> independently.

---

## Codex Review

## Summary

**Not approved.** Revision 6 substantially improves the lifecycle design: deleting the reverse EventBus→Verifier edge is correct, the declared post-wave dependency graph is acyclic, the Prepare/Activate barrier is a valid in-scope substitute for the impossible ordinal gate, and the admin-socket/plugin dispositions match the code. However, three implementation blockers remain: Wave A cannot currently preserve crypto-operator initialization, its `gameID` provider migration omits several concrete consumers, and 07-11’s caller manifest is still incomplete. At least one revised verification pattern is also guaranteed to fail on correct code.

## Strengths

- The deleted crypto edge is the correct decision. The verifier’s handler set depends on `eventBusSub.Publisher()` through [core.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:735), while [buildReadStreamWiring](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/readstream_wiring.go:115) returns empty wiring without that publisher. Adding EventBus→Verifier after 07-09 adds Verifier→EventBus would therefore form a cycle that [topoSort](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:108) rejects at lines 144–145.

- The literal ordering intent—“the EventBus process is not running before verification”—is not implementable without redesigning the atomic embedded-server/connect seam at [eventbus/subsystem.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/subsystem.go:164). But the security intent is implementable in bounds: EventBus prepares its process-internal substrate, the verifier walks the chain later in the Prepare sweep, and no domain `Activate` runs until every Prepare succeeds. That is a sound third option, already captured in [07-11](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:157).

- The intended post-wave graph is acyclic once the reverse edge is removed. Its principal chains flow forward—Database→ABAC→World→Plugins→Audit→gRPC and EventBus→Verifier/Cluster/Audit/gRPC—with no declared path returning to EventBus. The proposed live-edge acyclicity test is well designed: it requires the real registration set and live `DependsOn()` methods, and explicitly reconstructs the rev-4 cycle as its RED proof in [07-10](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-10-PLAN.md:491). The production registration seam is indeed centralized at [core.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:1087).

- Both “retreat” dispositions match the code:

  - Admin socket construction allocates nothing, while [Server.Start](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/server.go:58) atomically acquires the lock, removes the stale socket, binds and serves. `NewServer` in Prepare and `srv.Start()` in Activate is the smallest truthful split.
  - [PluginSubsystem.Start](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/setup/subsystem.go:163) builds the stack and registry; it owns no delivery loop. A documented no-op Activate is correct.

- The 07-07 rejection is substantively correct. Its YAML frontmatter parses to **34 unique entries**, not 25, and the plan itself correctly explains the comment-aware result at [07-07-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-07-PLAN.md:529).

- Parallel-wave ownership is clean: parsed frontmatter gives empty intersections for **07-03∥07-05** and **07-04∥07-06**.

## Concerns

- **HIGH — Crypto-operator initialization has an unresolved dependency cycle.** Today operators are validated using the live DB pool before constructing ABAC at [core.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:377), then passed as the concrete `CryptoOperators []string` field at lines 392–396. ABAC consumes that slice while building its stack at [access/setup/subsystem.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/access/setup/subsystem.go:79). Rev 6 instead moves `validateCryptoOperators` into `cryptoWiring` at [07-09-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:261), but `cryptoWiring` requires ABAC itself to be started. ABAC is not listed as a wiring consumer, and making it one would create `ABAC → cryptoWiring → ABAC`. As written, Wave A cannot both delete the DB pre-start and construct ABAC with the validated values.

- **HIGH — The `gameID` migration is underdesigned and its manifest cannot support it.** Rev 6 gives TLS an explicit provider but handles all other consumers with one sentence: “Other consumers … take `func() string`” at [07-09-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:255). Current consumers require concrete strings:

  - World: [world/setup/subsystem.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/world/setup/subsystem.go:31)
  - Plugin: [plugin/setup/subsystem.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/setup/subsystem.go:99)
  - Outbox relay: [relay_subsystem.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/world/setup/relay_subsystem.go:38)
  - Cluster: construction rejects an empty `ClusterID` at [cluster/registry.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/cluster/registry.go:78)
  - Audit DLQ: subject is eagerly constructed at [core.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/core.go:571)

  Yet 07-09’s manifest omits the world, plugin, relay and audit implementation files. A function cannot be assigned to these string fields, and resolving `dbSub.GameID()` during straight-line construction violates the plan’s own zero-live-read truth.

- **HIGH — 07-11’s 57-entry caller census remains incomplete.** Confirmed direct subsystem calls omitted from its frontmatter include:

  - [internal/admin/policy/subsystem_test.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/policy/subsystem_test.go:82)
  - [internal/admin/policy/subsystem_integration_test.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/policy/subsystem_integration_test.go:41), with additional calls at lines 72 and 93
  - [internal/eventbus/audit/subsystem_test.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem_test.go:127), with another call at line 145

  Removing `Start` breaks these. Worse, 07-11 explicitly says an unlisted caller is a planning defect and the executor must stop at [07-11-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:719). Its census regex also misses `s.Start(t.Context())`, explaining the audit omission.

- **MEDIUM — T-07-51’s downgrade overstates the evidence.** The plan says exposure is “bounded to nothing observable” at [07-10-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-10-PLAN.md:613). `DontListen: true` prevents the NATS client listener, but the same configuration supplies `HTTPPort` at [eventbus/subsystem.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/subsystem.go:149), so the monitor can bind. External mode also dials a remote broker at [eventbus/subsystem.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/subsystem.go:204), and `EnsureStream` may mutate broker state before verification at line 98. The domain-serving barrier keeps player-facing exposure bounded, but “nothing observable” and the embedded-only rationale do not justify the threat across both supported modes.

- **MEDIUM — A rev-6 verification count is guaranteed to fail on correct code.** Task 2 demands exactly 17 files matching production `Prepare`/`Activate` implementations at [07-11-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-11-PLAN.md:948). But Task 1 has already migrated the test stub in `internal/lifecycle/orchestrator_test.go`, making the count at least 18; Task 3 later adds two more test implementations. The regex is syntactically fixed but semantically scoped incorrectly.

- **LOW — 07-07 still carries superseded count prose.** It correctly records 34 entries at lines 529–535 but still says “30 `files_modified`” at [07-07-PLAN.md](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-07-PLAN.md:494). The prior rejection is valid; this remaining stale number should nevertheless be corrected.

## Suggestions

- Move crypto-operator validation into ABAC Prepare, using its existing DB dependency, or provide ABAC a dedicated **DB-only** validator. Do not route it through `cryptoWiring`. Preserve the current lax-and-warn behavior from [crypto_operator_validation.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/cmd/holomush/crypto_operator_validation.go:42).

- Add an explicit `gameID` consumer table to 07-09. For each consumer, name the provider signature, resolution phase, dependency edge, config-field change and modified files. Preserve `cfg.GameID` override precedence. Update the manifest for world, plugin, relay, cluster and audit.

- Replace 07-11’s restricted caller regex with a broad `.Start(` census followed by type classification, or a `go/packages`/AST census. Add the three confirmed missing files before execution.

- Scope the Prepare/Activate count to an explicit production file list or count compile-time `lifecycle.Subsystem` assertions instead of searching all tests and production files together.

- Keep the reverse crypto edge deleted, but rewrite T-07-51 as: no external **domain** surface activates before verification; observability endpoints and external-broker acquisition may be observable during Prepare. Address embedded and external modes separately.

- Correct the 07-07 count from 30 to 34.

## Risk Assessment

**HIGH.** The declared lifecycle graph is now acyclic and its barrier design is defensible, but Wave A lacks implementable provider paths for two boot-critical values, and Wave B’s manifest still omits callers that will fail compilation or stop execution at its own preflight. These are execution blockers, not advisory polish.

---

## OpenCode Review (grok-4.5)

# Cross-AI Plan Review — Phase 7 Rev 6

## 1. Summary

Rev 6 is substantially stronger than the round‑3/4 surface: FINDING‑5/6 (gameID + ·`NewEvent`/`NewType`), the Seq multipage RED reframing (`bus.go:87-104`), the page-advance `[0]` anchor, the StopAll **closure-not-arg** bug, the Prepare/Activate **circular barrier** repair, and live caller censuses for 07‑07/07‑11 all look real against this tree at `9e5d29db8`. The headline rev‑5 security story is only **partly** justified: deleting `EventBus → CryptoChainVerifier` is correct (it is a cycle after 07‑09 and would also drop `operator_read` from the boot gate), and the post‑wave graph is acyclic with that non-edge. **T‑07‑51 `low`/`accept` is not fully grounded for Wave A alone** — under the settled deps and ι tie-break, **gRPC and admin.sock can `Start` before the chain verifier**, so “nothing serves” is false until Wave B’s two-sweep barrier (or explicit `DependsOn(CryptoChainVerifier)` on external surfaces). Outside that residual, the eleven plans are execute-ready with clear MUST settlements and mechanically greppable gates.

## 2. Strengths

- **Cycle analysis for MEDIUM‑11 is right about the load-bearing fact.** Verifier handler assembly uses `eventBusSub.Publisher()` via `auditPublisher` at `cmd/holomush/core.go:735` and hard-requires `AuditPublisher` in `buildReadStreamWiring` (`cmd/holomush/readstream_wiring.go:118`). `chainHandlers` optionally appends `readStreamW.AuditChainHandler` only when wiring succeeded (`core.go:1046-1052`). After 07‑09’s cryptoWiring rule forces `Verifier → EventBus`, the reverse edge is a freestanding **cycle** Kahn detects at `internal/lifecycle/orchestrator.go:144-146`.
- **Third-option analysis that was written down is useful:** splitting provider packs for boot-gate registration would change INV‑CRYPTO‑102 registration semantics; leaving it is coherent with “behavior-preserving.”
- **D‑07/D‑08 grounding matches the code.** `HistoryQuery` documents `BeforeID` as tripwire and zero seq as tail (`internal/eventbus/bus.go:87-104`); cold uses `hasCursor := cursorSeq > 0` (`cold_postgres.go:134`); `ReplayTail` sets only `BeforeID` (`sub_grpc.go:928-936`); hostcap hardcodes `Seq: 0` and **still lies** about an “ID-only fallback” (`hostcap/servers.go:1279-1290`); Lua has two independent `Seq: 0` sites (`stdlib_focus.go:441,463`) with oldest-anchor comments at `:452-463`. Plan 07‑08’s red_framing + page_advance + legacy_zero_seq are accurate, not rhetorical.
- **Admin socket / Plugin retreats match the seams.** `Server.Start` is lock+remove+listen+Serve one shot (`admin/socket/server.go:58-113`); `NewServer` allocates nothing (`:53-55`). Plugin `Start` builds stack and returns — no work-loop launch (`plugin/setup/subsystem.go:163-165`; no `go ` / subscribe loop in that body).
- **07‑07 `files_modified` rejection checks out.** Frontmatter parses to **34** paths; body-only citations (proto, taxonomy, dispatcher, qualify) are assertion targets, not edits.
- **Parallel waves are file-disjoint.** 03∩05 = 0, 04∩06 = 0.
- **ARCH‑05 stack is sequenced correctly.** grpcclient extraction addresses live `gateway.go:20` import; session/core leaf correction; phantom `internal/auth/service` is real in `gateway_imports_test.go:107`; D‑18 stale INV‑GW rename trap is correctly refused.
- **StopAll deadline wiring** correctly rejects deferred-arg evaluation; in-tree precedent at `core.go:255-261` / `356-362`.
- **07‑11 D‑13.0 barrier scoping** matches `DontListen: true` + `HTTPPort` (`eventbus/subsystem.go:153-156`) and audit’s `AUDIT_DEP_NOT_STARTED` path — the universal “nothing runs” framing would not boot.
- **07‑10 Task 4 acyclicity gate** is the right durable fix for “wave N+1 proved against pre‑wave N state.”

## 3. Concerns

### HIGH

1. **T‑07‑51 is under-defended for Wave A (the claimed “complete win if Wave B is deferred”).**  
   Hand-topo with post‑plan deps (THE RULE on admin/grpc/verifier; gRPC + AuditProjection/TLS/Cluster/Plugins; EventBus `DependsOn=nil`) and SubsystemID tie-break (`orchestrator.go:125,140`; IDs at `subsystem.go:17-39`) yields order of form:  
   `… → eventbus → audit_projection → cluster → **grpc** → **admin_socket** → **crypto_chain_verifier** → …`  
   So under **combined `Start` (Wave A only)**:
   - gRPC binds TCP and `Serve`s in `Start` (`sub_grpc.go:771-781`) **before** the verifier runs.
   - Admin socket binds UDS in atomic `Server.Start` (`server.go:58-113`) **before** the verifier when both are ready (IDs 12 vs 13).  
   Grounds in the threat row:  
   - **(a) `DontListen`** — true for **embedded bus only** (`subsystem.go:153`); does not cover gRPC/admin.  
   - **(b) `StartAll` abort + `StopAll`** — true eventual rollback (`orchestrator.go:58-64`), but **not** “nothing served”: there is a live accept window while the verifier still runs (chain walk over DB can be non-trivial). Public “ready” is later (`core.go:1107-1129`), but the **listener is already open**.  
   - **(c) Wave B barrier** — valid **only if Wave B ships**; contradicts D‑12’s “Wave A remains a complete win if B is hairy.”  
   Scoring T‑07‑51 as `low`/`accept` with those three grounds **fits the deleted-edge decision more than the residual risk**.

2. **In-bounds third option exists and was not used for external surfaces.**  
   `grpc → CryptoChainVerifier` and `admin_socket → CryptoChainVerifier` are **acyclic** (Verifier does not depend on them; only `EventBus → Verifier` cycles). That encodes “fail-closed before bind” without provider surgery or a second ordering authority. Plans pocket residual risk into “accept” instead of that cheap edge (or requiring Wave B for the threat disposition).

### MEDIUM

3. **`hostcap/servers.go:1282` still teaches the false ID‑only fallback in production code today** — 07‑08 rewrites it, but any partial/early execution that functions “with Seq:0 fallback comments” risks reintroducing the revoked framing; criteria that ban the phrase are good — keep them.

4. **Wave A admin break-glass window** is worse than embedded NATS residual: admin handlers include rekey/readstream — crypto‑adjacent, not “in-process substrate only.”

5. **Current `StartAll` rollback uses the startup `ctx`** (`orchestrator.go:60`) — 07‑11 correctly treats cancelled-startup inheritance as a bug. Until Wave B lands, 07‑10’s deadline-aware `StopAll` can **amplify** abandon-on-cancel on that path. 07‑11 should not be optional if 07‑10 ships first in production without the rollback-context fix (ordering is 10 then 11 — OK if both ship; weak if 10 alone for a long time).

6. **07‑11 scope (~57 files)** is justified as atomic but is execution risk (integration-only callers). The plan mitigates with Step 0 census + `task test:int`; still a single large blast radius for human review.

### LOW

7. **Live doc lies that plans fix (good, but nominate as first-week landmines):** `core.go:1476` “runs before EventBus”; hostcap cursor comment; possibly `tier.go:719-720` as 07‑08 notes.

8. **Prod count 16→17** matches `core.go:1462-1471` (16 params) + TLS — fine.

9. **`encodeHostEventCursor` “cold handles Seq=0 as ID-only”** is not only wrong — cold `hasCursor := cursorSeq > 0` proves zero-seq never opens a cursor (`cold_postgres.go:134`).

## 4. Suggestions

1. **Re-open T‑07‑51** to either:  
   - **`medium`/`mitigate`:** add `DependsOn(SubsystemCryptoChainVerifier)` on `grpcSubsystem` and `AdminSocketSubsystem` in 07‑10 (or 07‑09 wiring rule for “external surfaces”), pin in topo test **verifier-before-grpc** and **verifier-before-admin**; or  
   - keep `accept` only with an explicit **“accept only after Wave B lands; Wave A ships with residual external bind-before-chain window”** in SUCCESS + threat model (not the current “structural via Wave B” as Wave‑A comfort).
2. **Topo pin named assertions** in 07‑10 should include **`crypto_chain_verifier` before `grpc` and `admin_socket`** once edges (or Wave B-only disposition) say so; today only bus-before-verifier + audit-before-grpc is required — that is **not** enough for chain-before-serve.
3. Do **not** revive `EventBus → CryptoChainVerifier` under any “encode the comment” rewrite — Task 4 RED proof should stay mandatory.
4. Prefer landing **07‑10 + 07‑11 in one ship unit** if StopAll becomes timeout-aggressive before rollback uses `context.Background()`-parented stop.
5. On 07‑07, keep rejecting adding read-only paths to `files_modified` — that rejection is correct.

## 5. Risk Assessment

| Dimension | Level | Why |
|-----------|--------|-----|
| **Plan correctness (execute → intended design)** | **MEDIUM** | High-quality settlements and green path are real; Wave‑A threat story overclaims. |
| **Security residual if executed as written** | **MEDIUM–HIGH until Wave B** (or external deps-on-verifier edges) | Bind/serve before chain walk under supervised graph; Window closes with two-sweep Activate. |
| **Operational/execute risk** | **MEDIUM** | 07‑11 size + integration-only callers; mitigated by census and test:int gates. |
| **Import-cycle / ARCH‑04–05 coherence** | **LOW** | Consumer interfaces + leaves match eventbus→auth cycle proof (`go list -deps` contains `internal/auth`); 03∥05 / 04∥06 disjoint. |

**Overall: MEDIUM** — **not APPROVED as security-self-certified on T‑07‑51 / Wave A completeness without the fix above**; **APPROVED to execute** if T‑07‑51 is re-scoped or external surfaces gain `DependsOn(CryptoChainVerifier)` (preferred) and 07‑10 Task 4 remains mandatory.

### Focus item verdicts (as requested)

| Item | Verdict |
|------|---------|
| 1. Deleted `EventBus → Verifier` fail-closed edge | **Correctly deleted.** Intent “bus must not come up while chain unproven” is **not** implementable as that edge post‑07‑09; bus-up-first is forced by handler construction. Post-graph without that edge is acyclic. **True fail-closed for external serve needs other edges or Wave B**, not the deleted edge. |
| 2. T‑07‑51 high/mitigate → low/accept | **Not fully justified.** (a) only covers embedded bus; (b) leaves a serve-then-rollback window; (c) is Wave B. Fits the edge deletion more than residual risk. |
| 3. 07‑07 files_modified under-count rejection | **Accepted.** 34 parsed entries; body-only paths are non-edit citations. |
| 4. Admin-socket + Plugin retreats | **Match code.** Atomic `Server.Start`; plugin has no Activate loop to move. |
| 5. 07‑11 25→57 census + parallel disjoint `files_modified` | **Manifest growth is justified** if Step 0 re-census stays mandatory; **03∥05 and 04∥06 are empty intersections.** |

---

## Consensus Summary

**Verdict: NOT execution-ready. Do not start Wave 1.** Round 4 found **4 blockers** in rev 6 — plans
that had passed a FULL plan-checker with 0 blockers and cleared every coverage gate. This is the
**fourth consecutive round** to find real defects.

### The signal worth acting on: every round-4 blocker is in ARCH-03

| Round | ARCH-04/05 (07-01…07-08) | ARCH-03 (07-09/07-10/07-11) |
| --- | --- | --- |
| 2 | 07-02, 07-06, 07-07, 07-08 | 07-09, 07-10, 07-11 |
| 3 | 07-08 anchor | **cycle #1**, admin-socket seam, PluginSubsystem loop |
| 4 | **none** | **ABAC cycle #2**, gameID migration, census, T-07-51 |

**ARCH-04 and ARCH-05 have converged — round 4 found zero blockers across 07-01…07-08.** ARCH-03 has
produced blockers in every round, including **two independent dependency cycles**. Both reviewers
separately flag 07-09/07-11 size as the residual execution risk. See "Recommendation" below.

### Blockers

1. **BLOCKER — a SECOND dependency cycle: `ABAC → cryptoWiring → ABAC`.** *(Codex only. Verified.)*
   Today operators are validated against the live DB pool (`cmd/holomush/core.go:377-385`) and passed
   to ABAC as a **concrete slice** at construction — `abacsetup.NewABACSubsystem(… CryptoOperators:
   cryptoOperators)` (`core.go:392-396`). Rev 6 moves `validateCryptoOperators` **into `cryptoWiring`**
   (`07-09-PLAN.md:261`). But THE RULE forces every `cryptoWiring` consumer to
   `DependsOn(…, SubsystemABAC)` — so making ABAC a consumer closes `ABAC → cryptoWiring → ABAC`.
   **Wave A cannot both delete the `:385` DB pre-start and construct ABAC with validated values.**
   *Fix (Codex):* validate inside **ABAC's own Prepare** using its existing DB edge — it already does
   `pool := s.cfg.DB.Pool()` (`internal/access/setup/subsystem.go:79`) — or give ABAC a DB-only
   validator. Do **not** route it through `cryptoWiring`. Preserve the lax-and-warn behavior
   (`cmd/holomush/crypto_operator_validation.go:42`).

2. **BLOCKER — the `gameID` provider migration is underdesigned and unsupported by its manifest.**
   *(Codex only.)* Rev 6 gives TLS an explicit provider and disposes of everything else in one
   sentence — *"Other consumers … take `func() string`"* (`07-09-PLAN.md:255`). But live consumers
   require **concrete strings**: `internal/world/setup/subsystem.go:31`,
   `internal/plugin/setup/subsystem.go:99`, `internal/world/setup/relay_subsystem.go:38`,
   `internal/cluster/registry.go:78` (rejects an empty `ClusterID`), and the audit DLQ subject
   eagerly built at `cmd/holomush/core.go:571`. **A function cannot be assigned to a string field**,
   and resolving `dbSub.GameID()` during straight-line construction violates 07-09's own
   zero-live-read truth. 07-09's manifest omits the world, plugin, relay and audit files.
   *Fix:* an explicit per-consumer table — provider signature, resolution phase, dependency edge,
   config-field change, files — preserving `cfg.GameID` override precedence.

3. **BLOCKER — 07-11's 57-entry census is STILL incomplete, and its own preflight will halt.**
   *(Codex only.)* Confirmed omitted direct callers: `internal/admin/policy/subsystem_test.go:82`;
   `internal/admin/policy/subsystem_integration_test.go:41` (+ `:72`, `:93`);
   `internal/eventbus/audit/subsystem_test.go:127` (+ `:145`). The census regex misses
   `s.Start(t.Context())`, which explains the audit omission. **07-11 itself says an unlisted caller
   is a planning defect and the executor must stop** (`07-11-PLAN.md:719`) — so this halts execution
   by the plan's own rule. *Fix:* replace the restricted regex with a broad `.Start(` census plus type
   classification, or a `go/packages`/AST census.

4. **BLOCKER (OpenCode HIGH) / MEDIUM (Codex) — T-07-51's downgrade does not hold for Wave A, and an
   in-bounds third option exists.** *(Verified independently, including the remedy.)*
   Hand-computing the post-plan graph: `topoSort` tie-breaks ready subsystems by ascending
   `SubsystemID` (`internal/lifecycle/orchestrator.go:139`), and
   `SubsystemGRPC` (`subsystem.go:25`) < `SubsystemAdminSocket` (`:29`) <
   `SubsystemCryptoChainVerifier` (`:33`). Under **Wave A (combined `Start`)**, gRPC binds TCP and
   serves inside its own `Start` (`cmd/holomush/sub_grpc.go:772` `net.Listen` → `:779-781`
   `go s.grpcServer.Serve`) — **before the chain verifier runs**. The three grounds for `low/accept`:
   - **(a) `DontListen`** covers the **embedded bus only** (`internal/eventbus/subsystem.go:153`) —
     it says nothing about gRPC or admin.sock.
   - **(b) `StartAll` abort + `StopAll`** is *eventual* rollback (`orchestrator.go:58-64`); the
     listener is already accepting during the chain walk.
   - **(c) "Wave B's barrier makes it structural"** — **contradicts locked decision D-12**
     (`07-CONTEXT.md:247`: *"Wave A remains a complete win if B proves hairy"*). The threat's
     disposition cannot depend on a wave the phase explicitly allows to be deferred.
   **The in-bounds third option the planner missed:** `grpc → CryptoChainVerifier` and
   `admin_socket → CryptoChainVerifier` are **acyclic** — the verifier depends only on Database
   (`internal/eventbus/audit/chain/verifier_subsystem.go:67-69`), so it depends on neither. Only
   `EventBus → Verifier` cycles. Those two edges encode fail-closed-before-bind with no provider
   surgery and no second ordering authority.
   *Codex adds (MEDIUM):* "nothing observable" also overstates for the NATS monitor (`HTTPPort`,
   `subsystem.go:149`) and for **external mode**, which dials a remote broker (`:204`) and may mutate
   broker state via `EnsureStream` (`:98`) before verification. Address embedded and external modes
   separately.

### Divergence (worth reading — the reviewers split, and both are partly right)

On T-07-51, **OpenCode says HIGH; Codex says MEDIUM.** Codex holds that 07-11's Prepare/Activate
barrier *is* the sound third option and is already captured (`07-11-PLAN.md:157`). OpenCode's counter
is decisive and Codex never engaged it: **the barrier only exists if Wave B ships, and D-12 explicitly
promises Wave A stands alone.** Conversely Codex found the external-mode exposure OpenCode missed.
Take the union: the barrier is right *with* Wave B; the external-surface edges are what make Wave A
honest on its own.

**The planner's error here was over-generalization, not miscalculation.** It correctly proved
`EventBus → Verifier` is impossible (the verifier is built from the bus's publisher), then slid from
*"this edge is impossible"* to *"the intent is unimplementable."* The intent never needed that edge —
it needed edges on the external surfaces.

### Agreed Strengths (both reviewers, verified)

- **Deleting `EventBus → Verifier` was CORRECT** — the verifier's handler set is built from
  `eventBusSub.Publisher()` (`cmd/holomush/core.go:735`) and `buildReadStreamWiring` returns empty
  wiring without it (`cmd/holomush/readstream_wiring.go:115`), so the reverse edge is a cycle
  `topoSort` rejects (`internal/lifecycle/orchestrator.go:144-146`). **Do not revive it.**
- **The post-wave graph is acyclic** with that non-edge.
- **07-10 Task 4's live-edge acyclicity test is the right durable fix** for the "wave N+1 proved
  against pre-wave-N state" class — it requires the real registration set and live `DependsOn()`, and
  reconstructs the rev-4 cycle as its RED proof (`07-10-PLAN.md:491`); the production seam is indeed
  centralized (`cmd/holomush/core.go:1087`).
- **Both "retreat" dispositions match the code** — `Server.Start` is atomic
  (`internal/admin/socket/server.go:58`), `NewServer` allocates nothing (`:53-55`);
  `PluginSubsystem.Start` builds the stack and owns no delivery loop
  (`internal/plugin/setup/subsystem.go:163-165`).
- **The 07-07 rejection is CORRECT** — frontmatter parses to **34** entries; body-only paths are
  non-edit citations. *(Both reviewers upheld it. Do not re-open.)*
- **Parallel waves are file-disjoint** — 07-03∩07-05 = ∅, 07-04∩07-06 = ∅.
- **07-08's D-07/D-08 grounding matches the code**, including the `[0]` page-advance anchor and the
  two independent Lua `Seq: 0` sites; ARCH-05's stack sequences correctly; the D-18 stale INV-GW
  rename trap is correctly refused.

### Lesser findings

- **MEDIUM (Codex) — 07-11 Task 2's "exactly 17 files" count is guaranteed to fail on correct code**
  (`07-11-PLAN.md:948`): Task 1 has already migrated the `internal/lifecycle/orchestrator_test.go`
  stub (≥18), and Task 3 adds two more. The regex is syntactically fixed but semantically mis-scoped.
  *Fix:* scope to an explicit production file list, or count compile-time `lifecycle.Subsystem`
  assertions.
- **MEDIUM (OpenCode) — Wave-A admin break-glass window is worse than the NATS residual**: admin
  handlers include rekey/readstream — crypto-adjacent, not in-process substrate.
- **MEDIUM (OpenCode) — ship 07-10 + 07-11 together**: until Wave B lands, 07-10's deadline-aware
  `StopAll` can amplify abandon-on-cancel on the rollback path that still inherits the startup ctx
  (`orchestrator.go:60`).
- **LOW (Codex) — 07-07 still says "30 `files_modified`"** at `:494` while correctly recording 34 at
  `:529-535`. Stale count; the rejection itself stands.

### Recommendation

**Rev 7 is required. But consider a phase split first — the evidence now supports it.**

Four rounds have converged ARCH-04 and ARCH-05 (07-01…07-08): **round 4 found zero blockers there.**
Every round-4 blocker, and 4 of 5 round-3 blockers, are in **ARCH-03** (07-09/07-10/07-11) — which has
now produced **two independent dependency cycles**, a still-incomplete caller census on its fourth
attempt, an underdesigned provider migration, and a security threat whose disposition contradicts a
locked decision. Both reviewers independently name 07-09/07-11 size as the residual risk.

That is not a plan-quality problem that a fifth revision fixes; it is a **scope** signal. ARCH-03
rewires boot-order for 17 subsystems across two waves while ARCH-04/05 are ordinary (if large)
mechanical refactors. Splitting would let the converged 8 plans ship and give ARCH-03 its own
discuss → plan cycle with the cycles/providers as first-class design inputs rather than review
findings.

If the phase is NOT split, rev 7 must: (1) move crypto-operator validation into ABAC's Prepare;
(2) build the explicit `gameID` consumer table; (3) redo 07-11's census with an AST/`go/packages`
sweep; (4) re-scope T-07-51 — preferably by adding `grpc → CryptoChainVerifier` and
`admin_socket → CryptoChainVerifier` (acyclic, cheap) and pinning verifier-before-grpc /
verifier-before-admin in 07-10's topo test; (5) fix the 17-file count and the stale 30/34.
