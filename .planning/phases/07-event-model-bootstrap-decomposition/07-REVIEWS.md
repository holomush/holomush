---
phase: 7
round: 3
reviewers: [codex, opencode]
reviewed_at: 2026-07-16T00:42:53Z
supersedes: c6184d66a
reviewed_commit: 39023015a
plans_reviewed:
  - 07-01-PLAN.md
  - 07-02-PLAN.md
  - 07-03-PLAN.md
  - 07-04-PLAN.md
  - 07-05-PLAN.md
  - 07-06-PLAN.md
  - 07-07-PLAN.md
  - 07-08-PLAN.md
  - 07-09-PLAN.md
  - 07-10-PLAN.md
  - 07-11-PLAN.md
reviewer_models:
  codex: default (codex-cli 0.144.4)
  opencode: openrouter/x-ai/grok-4.5
reviewer_grounding:
  codex: grounded — opened source, all findings carry path:line
  opencode: grounded — go list -deps, file reads, rg; all findings carry path:line
antigravity: SKIPPED — round 2 proved it ungrounded (never read the repo; scored 10/10 to plans with verified blockers)
verdicts:
  codex: HIGH — NOT APPROVED (3 design blockers + incomplete census)
  opencode: MEDIUM (HIGH if 07-08 ships as-is) — CONDITIONAL APPROVE
---

# Cross-AI Plan Review — Phase 7 (Round 3)

> **Round 3, reviewing rev 4 (`39023015a`).** Round 1 folded in at `55d2078d5`. Round 2
> (`af368f381`) found 4 blockers in round-1-approved plans → folded in at `39c7e8fcb` (rev 3).
> The `gsd-plan-checker` then found 2 warnings → fixed at `39023015a` (rev 4), after which it
> returned **VERIFICATION PASSED (0 blockers)** and every coverage gate went green
> (3/3 requirements, 18/18 decisions, 0 gaps).
>
> **This round found blockers anyway.** Prior-round feedback was withheld so both reviewers
> re-derived independently. Antigravity was excluded on round-2 evidence of ungroundedness.

---

## Codex Review

# Summary

**NOT APPROVED.** Revision 4 is substantially better grounded than a typical cross-package refactor plan, but source review found three design blockers and an incomplete change census. Most importantly, 07-09 and 07-10 create a direct lifecycle dependency cycle that prevents startup. The Prepare/Activate design is coherent for EventBus and audit, but two rows in 07-11 describe seams that do not exist in the current code. Overall execution risk remains **HIGH**.

# Strengths

- The nine-wave DAG is logically ordered, and the declared manifests for `07-03 ∥ 07-05` and `07-04 ∥ 07-06` have no intersections. Convergence through 07-07 before event-model deletion is sensible.

- The EventBus Prepare decision is source-grounded. Embedded NATS uses `DontListen: true` while starting an in-process server and connecting through `nats.InProcessServer`, so treating it as acquisition is coherent despite the monitoring-port exception. [subsystem.go:151](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/subsystem.go:151)

- The audit split follows an existing seam: dependency acquisition and partition boot gates precede `p.start`, which starts the projection loop. This supports audit Prepare against a merely prepared EventBus. [subsystem.go:273](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem.go:273)

- The history pagination correction accurately follows the source contract: zero sequence means stream start/end, not an ID-only cursor, while nonzero sequence owns ordering. [bus.go:88](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/bus.go:88)

- The revised rollback design correctly includes the failing, partially prepared subsystem and uses a fresh bounded context rather than inheriting a cancelled startup context.

- The shutdown closure in 07-10 correctly accounts for Go’s deferred-argument evaluation. Constructing the timeout inside the deferred closure avoids handing `StopAll` a context that expired during normal uptime.

# Concerns

- **HIGH — 07-09 and 07-10 create a direct dependency cycle.**  
  07-09 requires every `cryptoWiring` consumer—including `chain.VerifierSubsystem`—to depend on EventBus. [07-09-PLAN.md:189](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-09-PLAN.md:189)  
  Then 07-10 makes EventBus depend on `SubsystemCryptoChainVerifier`. [07-10-PLAN.md:63](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-10-PLAN.md:63)  
  The resulting graph is:

  `EventBus → CryptoChainVerifier → EventBus`

  The plan’s “No cycle” proof examines the current verifier dependency—Database only—not the mutation imposed by the immediately preceding wave. [verifier_subsystem.go:65](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/chain/verifier_subsystem.go:65) `topoSort` will detect this and refuse startup. [orchestrator.go:108](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/lifecycle/orchestrator.go:108)

- **HIGH — 07-11’s admin-socket split requires a nonexistent seam.**  
  The plan mandates acquiring `admin.lock` in Prepare and binding `admin.sock` in Activate. Current `AdminSocketSubsystem.Start` delegates all lifecycle work to a single `Server.Start`. [subsystem.go:68](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/subsystem.go:68)  
  `Server.Start` atomically acquires the flock, removes the stale socket, calls `net.Listen`, and starts serving. [server.go:58](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/server.go:58) There is no separate lock-acquisition API. Acquiring the lock in Prepare and then invoking the existing `Start` in Activate would attempt to reacquire the same lock. Yet 07-11 does not include `internal/admin/socket/server.go` or its tests in `files_modified`.

- **HIGH — 07-11 assigns PluginSubsystem an Activate loop that does not exist.**  
  The settled table requires “event-delivery subscription and command dispatch to plugins” to start in Activate. Current `PluginSubsystem.Start` constructs hosts, loads plugins, registers commands, and returns; it launches no delivery subscriber or dispatch loop. [subsystem.go:163](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/setup/subsystem.go:163)  
  Runtime delivery is a synchronous manager method invoked by callers. [manager.go:458](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/plugin/manager.go:458) Satisfying the criterion would therefore require inventing or relocating architecture outside the stated task.

- **HIGH — the `files_modified` manifests are incomplete enough that mandated test gates cannot pass.**  
  Removing `Subsystem.Start` affects direct callers beyond 07-11’s manifest, including:

  - [internal/admin/socket/subsystem_test.go:74](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/admin/socket/subsystem_test.go:74)
  - [internal/testsupport/integrationtest/real_abac.go:56](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/testsupport/integrationtest/real_abac.go:56)
  - [internal/testsupport/integrationtest/plugins.go:340](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/testsupport/integrationtest/plugins.go:340)
  - [internal/eventbus/audit/subsystem_boot_gate_integration_test.go:58](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/subsystem_boot_gate_integration_test.go:58)

  Similarly, 07-09 changes verifier configuration from a concrete `Handlers` slice to a provider but omits existing configuration literals such as [verifier_subsystem_test.go:108](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/internal/eventbus/audit/chain/verifier_subsystem_test.go:108). These are compile failures, not optional cleanup.

- **MEDIUM — 07-08 retains superseded cursor prose.**  
  Its action correctly retracts the ID-only fallback, but Task 2’s `<done>` still says “the Seq == 0 ID-only fallback still works.” [07-08-PLAN.md:453](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase7/.planning/phases/07-event-model-bootstrap-decomposition/07-08-PLAN.md:453) That contradicts both the source contract and the rest of the revised plan.

# Suggestions

- Split `cryptoWiring` into at least two providers:

  - verifier boot-gate inputs that do not require EventBus;
  - runtime/admin crypto wiring that may depend on EventBus, Auth, and ABAC.

  Then add a combined 07-09/07-10 test that runs the real production graph through `topoSort` and explicitly proves it is acyclic.

- For the admin socket, choose one settled implementation:

  - Prepare constructs the `Server`; Activate calls its existing atomic `Start`; or
  - explicitly design `Server.Prepare`/`Activate`, including `server.go` and all affected tests in the manifest.

- Correct PluginSubsystem’s disposition to Prepare = current load/wiring body, Activate = documented no-op, unless a real production-owned delivery loop is first identified.

- Rebuild 07-09 and 07-11 manifests from both method definitions and all direct call/config-literal sites. Re-run the parallel-intersection check after expanding them.

- Replace 07-08’s stale `<done>` text with: legacy `Seq == 0` restarts from the stream tail; returned cursors subsequently carry the real sequence.

# Risk Assessment

**HIGH.** The lifecycle cycle alone makes the planned production graph unbootable. The admin-socket and PluginSubsystem rows also force implementations unsupported by current APIs, while incomplete manifests mean the advertised green intermediate commits cannot actually compile and test cleanly. The overall architecture remains promising, but another revision is required before execution.

---

## OpenCode Review (grok-4.5)

## Cross-AI Plan Review — Phase 7 rev 4

**Verdict: CONDITIONAL APPROVE — 1 HIGH fix, several MEDIUM residues.**  
Plans are far stronger than a typical first draft. Most prior-round bugs (FINDING-1/5/6, weekly start fake-greens, Prepare/Activate circular barrier, orphan-gate file move) are settled with live `path:line` grounding. One actionable instruction in **07-08 still encodes the wrong pagination anchor**, which would ship a “fixed” cursor that still repeats pages.

I verified against the tree at this worktree (key hits: `internal/eventbus` deps include `internal/auth`+`internal/core`; `DontListen: true` + `HTTPPort` at `internal/eventbus/subsystem.go:153-164`; five eager starts in `cmd/holomush/core.go`; `Seq: 0` at `hostcap/servers.go:1290` and **two** hostfunc sites `stdlib_focus.go:441,463`; phantom `internal/auth/service` in `gateway_imports_test.go:107` + `invariants.yaml:2345`; `productionSubsystems` **16** params; cluster `Start` in `heartbeat.go:22` vs `DependsOn` in `registry.go:174`).

---

### 1. Summary

Phase 7 is detailed, behavior-preserving, and mostly executor-safe: ARCH-05 leaf extraction order is right, ARCH-04’s cycle/subject/qualification traps are pinned (FINDING-1/5/6), Wave A’s `cryptoWiring` hoist is designed rather than deferred, and Wave B’s barrier is correctly scoped to external **domain** surfaces + work loops with the embedded NATS/`DontListen` argument verified live. Coverage of three requirements is real, not checkbox theater. Residual risk is concentrated in (a) one wrong “last event” next-cursor instruction that undoes D-07, (b) residual prose that still names retracted constructs, and (c) the inherent sheer blast radius of 07-09/07-11.

---

### 2. Strengths

- **Evidence-driven corrections stick** — RESEARCH FINDING-1 (auth cycle), FINDING-5 (`gameID func()` + qualify-once), D-07 tripwire (`bus.go:89-104`), LOW-7 deferred-arg trap, orphan definition at `bootstrap_orphan.go:34` — all reified as settlement tables, not “note for implementer.”
- **Acceptance criteria force the right outcome** — e.g. `go list -deps` for leaf/closure, multipage Lua walk (not single-page encode), `rg -c 'runBootstrapOrphanCheck' cmd/holomush/` → 0 (move ≠ copy), KEK boot via **`task test:int`** only, partial-prepare + cancelled-ctx rollback tests in 07-11.
- **Parallelism safety** — 07-03∥07-05 and 07-04∥07-06 show empty `files_modified` intersections; shared files (`gateway_handler`, `sub_grpc`) are serialised by `depends_on`/wave.
- **07-01 really fixes ARCH-05’s heavy hit** — `cmd/holomush/gateway.go:20,152-153` imports `internal/grpc` **outside** `coreOnlyFiles`; extraction is correctly mandatory, not an allowlist patch.
- **07-11 D-13.0/D-13.3** — disposition table places `eventbus` whole-body in Prepare (matches `connectEmbedded` atomicity) and plugin **load** in Prepare because `audit.DependsOn()` includes Plugins (`audit/subsystem.go:255-261`). External domain binds limited to gRPC + admin.sock — matches code.
- **D-08 non-fabricated** — hostv1 census test with no `// Verifies:` for a nonexistent INV is correct (INV-RB-3 awareness).
- **Threat models** are generally grounded (session_ended ctx, non-ULID actor bridge, cryptoSEAM on `auditRow`, deferred-arg StopAll).

---

### 3. Concerns

#### HIGH

1. **07-08 still instructs the wrong page-advance anchor (“last event”).**  
   Live contract (`hostcap/servers.go:900-906`, `stdlib_focus.go:453-463`, `sub_grpc.go:915` / reverse to ascending): **index 0 = oldest = next `BeforeSeq` anchor**.  
   Plan text still says:
   - Task 1: *“encode from the **last** event of each page”* (≈L292)
   - Task 2 call-site prose mix of “last event’s real `e.Seq`” at `:901` (`:901` is **per-event** encode in the loop, not next_cursor; next_cursor is `protoEvents[0]`)
   - Encode site 2: *“Pass the **last** event of the page’s real `Seq`”*  

   Using newest/highest seq as `BeforeSeq` exclusive upper bound **reintroduces near-full page repeats** after “green.” This is the classic “prose around revised block ships the old wrong construct” defect class.  
   **Required fix:** rewrite every next_cursor / feed-back instruction to **oldest / index 0 / first event of ascending page**, and keep *per-event* encode as “each event’s own `e.Seq`.”

#### MEDIUM

2. **07-08 Task 2 `<done>` still claims the retracted ID-only fallback**  
   (`Seq == 0` ID-only fallback still works — L453). Contradicts `<legacy_zero_seq_policy>` and T-07-42. Criteria below are correct (tail read); the done line will mislead SUMMARY/verifiers.

3. **07-09 `cryptoWiring` consumer set is design-settled but still checklist-fragile.**  
   Application depends on the superset-DependsOn test listing *every* holder of the provider. Live sinks of the `:705-1060` block include at least grpc cfg mutation, admin socket handlers, chain.Verifier handlers (incl. readStream handler), crypto policy emit, checkpoint sweep — plan names five. Risk: an omitted config field still wired as a live value at construct time reintroduces an eager read. **Not false**, but the failure mode is boot panic; the Step 0 re-census **must** emancipate any row not in the table (plan already says stop—keep that non-negotiable).

4. **07-11 still defers one structural seam to the executor**  
   (`ConfigureSystemBroadcaster` late-binding: *“Decide explicitly and record”* ~L611). D-13 wanted planner settlement. Leaving Plugin/gRPC late-bind versus `DependsOn`+Prepare wiring is residual “implementer decides” on a real host lifecycle order.

5. **07-07 `files_modified` frontmatter is comment-truncated / under-counted vs body.**  
   Body correctly lists more (event_constructor_test, ulid.go docs, idgen, natstest, idle_scheduler, …). Wave tooling that trusts only YAML may under-detect conflicts / incomplete PRs. Align frontmatter with the full list.

6. **Parallel wave 4 (07-04∥07-06):** safe on files, but 07-06 assumes 07-04’s gates are not required for its own greps (true today). No merge hazard; note only that ARCH-05 permanence is not a 07-06 dependency (already OK).

7. **D-07 success criteria header still emphasises concurrent skip/repeat over the quiet multipage primary bug.** Spec A/B framing is fixed in the task body; top-level success text lags (executor might undervalue Spec A).

#### LOW

8. Residual “~6 packages” language is gone from 07-04 (good); 07-03 gate mixes exact package bans with regexes—fine if kept exact-set.

9. **AdminSocket** today `DependsOn() == nil` (`admin/socket/subsystem.go:64-66`); Wave A/B correctly force deps via wiring rules—ensure the Named-struct registration order tests don’t reassert nil-deps stubs as production truth (07-10 pin already warns against nil-deps stubs).

10. Threat-ID renumbering collisions fixed for several T-07-44s earlier; no new collision spotted.

---

### 4. Suggestions

1. **Before execute:** single editorial pass on **07-08 only** — global replace of next_cursor/`last event of each page` language → **oldest/index 0**; fix Task 2 `<done>`; keep Spec A as the RED gate in success_criteria first bullet.
2. **07-11:** settle `ConfigureSystemBroadcaster` now: recommend **`Prepare` wires nothing late; `Activate(grpc)` or post-Publisher `Prepare` of plugin host after EventBus Generate — preferably inject `sysbroadcast` at grpc Prepare once taxes `DependsOn(Plugins, EventBus)`**, delete late-binding hook when graph guarantees order.
3. **07-09:** add the **explicit list of subsystem ctors constructed inside/after cryptoWiring providers** as a table (not only “consumers of the provider”), so executor cannot leave a live `Pool()` at ctor before StartAll.
4. Sync **07-07 files_modified YAML** with the corrected blast-radius list in `<artifacts>`.
5. Optional: one meta grep in 07-08 acceptance:  
   `rg -n 'last event of each page|ID-only fallback still works' 07-08-PLAN.md` must stay 0 after revise (self-guard on revision drift).

---

### 5. Risk Assessment

| Area | Risk | Why |
|------|------|-----|
| ARCH-05 (01–04) | **LOW** | Closure math + phantom auth fix + dual gates are sound; `gateway.go` extraction verified necessary. |
| ARCH-04 presence/broadcast/collapse (05–07) | **LOW–MEDIUM** | Cycles/qualify/NewEvent pinned; large mechanical blast radius, not design holes. |
| D-07 pagination (08) | **HIGH until text fix; LOW after** | Wrong “last event” instruction is a ship-blocking defect in otherwise correct deep analysis (`bus.go` tripwire + dual Seq:0 encode sites verified). |
| ARCH-03 Wave A (09–10) | **MEDIUM** | Correct diagnosis, large `cryptoWiring` surface; depends on mechanical completeness of dep-superset test + orphan **move**. LOW-7 closure defer shape is correct vs Go eval order. |
| ARCH-03 Wave B (11) | **MEDIUM** | Definitions now consistent with `DontListen` + audit JS dep; still a 17-impl interface atomic change — residual late-bind discretion. |
| **Overall** | **MEDIUM** ( **HIGH** if 07-08 paints as-is) | Phase goals (single Event, orchestrated boot, gateway leaf imports) are achievable by these plans once the 07-08 anchor bug and residual done-prose are fixed. |

---

### Goal check

| Success criterion | Plans deliver? |
|-------------------|----------------|
| Single Event; `core.Event` duplication gone | **Yes** (07-07 + predecessors; Seq path unblocked for 07-08) |
| Bootstrap via Orchestrator; start/stop ordering real | **Yes** (09–11 + MEDIUM-11 edges + pin) |
| Gateway only protocol-translation deps | **Yes** (01–04; leaf extracts + forbidden + closure) |

**Do not treat prior VERIFICATION PASSED as proof 07-08 next_cursor prose is safe** — the tripwire/`Seq:0` analysis is excellent; the **action line’s “last event” is still wrong against `hostcap/servers.go:900-906` and `stdlib_focus.go:453-463`**. Fix that, then execute.

---

## Consensus Summary

**Verdict: NOT execution-ready. Phase 7 needs a rev 5. Do not start Wave 1.**

Both reviewers were source-grounded and both found real defects in plans that had **passed the
`gsd-plan-checker` with 0 blockers** and cleared every coverage gate. This is the third
consecutive round to find blockers — round 2 found 4 in round-1-approved plans; round 3 found
5 more in checker-approved plans.

### The headline: a lifecycle cycle that cannot boot

**BLOCKER — 07-09 + 07-10 produce `EventBus → CryptoChainVerifier → EventBus`.** *(Codex only.
Independently verified by the orchestrator.)*

- `07-09-PLAN.md:187-191` — **THE RULE**: "Every subsystem whose config holds a `cryptoWiring`
  provider MUST declare the wiring's full dependency set — `DependsOn(SubsystemDatabase,
  SubsystemAuth, SubsystemABAC, **SubsystemEventBus**)`".
- `07-09-PLAN.md:663` names the five consumers, **including `chain.VerifierSubsystem`**.
  ⇒ wave 8 gives `VerifierSubsystem → EventBus`.
- `07-10-PLAN.md:63-65` — "**Add the real edge:** `eventbus.Subsystem.DependsOn()` returns
  `[lifecycle.SubsystemCryptoChainVerifier]`. **No cycle** (Verifier→Database, EventBus→Verifier,
  Database→∅)." ⇒ wave 9 gives `EventBus → CryptoChainVerifier`.
- **The "No cycle" proof reads the LIVE state, not the post-wave-8 state.**
  `internal/eventbus/audit/chain/verifier_subsystem.go:67-69` returns `[]lifecycle.SubsystemID{
  lifecycle.SubsystemDatabase}` — true **today**, false after 07-09 (its own predecessor wave) runs.
- `topoSort` (`internal/lifecycle/orchestrator.go:108`) detects the cycle and refuses startup.

**The bitter irony: THE RULE is the safety mechanism.** It exists to prevent a boot panic from a
missing `DependsOn` edge — and it is what closes the cycle. Worse, 07-09's mandated superset test
would **pass** (each consumer does declare ⊇ the required set), pinning the cycle while the process
refuses to boot. A green test suite and an unbootable binary.

*Codex's fix:* split `cryptoWiring` into (a) verifier boot-gate inputs that do NOT need EventBus and
(b) runtime/admin wiring that may; then add a combined 07-09/07-10 test running the **real production
graph** through `topoSort` to prove acyclicity. That test is the durable fix — it would have caught
this at plan time.

### Other blockers

1. **BLOCKER — 07-11's admin-socket split requires a seam that does not exist.** *(Codex.
   Verified.)* The plan mandates acquiring `admin.lock` in Prepare, binding `admin.sock` in Activate.
   But `internal/admin/socket/server.go:58-60` — `Server.Start()` **atomically** acquires the flock,
   removes the stale socket, binds the UDS, and serves; there is no separate lock-acquisition API.
   `AdminSocketSubsystem.Start` (`internal/admin/socket/subsystem.go:73`) delegates wholesale.
   Acquiring in Prepare then calling the existing `Start` in Activate re-acquires the same lock.
   `server.go` and its tests are not in 07-11's `files_modified`.

2. **BLOCKER — 07-11 assigns PluginSubsystem an Activate loop that does not exist.** *(Codex.
   Verified.)* The table requires "event-delivery subscription and command dispatch to plugins" to
   start in Activate. `internal/plugin/setup/subsystem.go:163-165` — `Start` "builds the full plugin
   stack and command registry" and returns; it launches no delivery subscriber or dispatch loop.
   Delivery is a synchronous manager method invoked by callers (`internal/plugin/manager.go:458`).
   Satisfying the criterion means inventing or relocating architecture outside the task's scope.

3. **BLOCKER — 07-08 instructs the WRONG page-advance anchor.** *(OpenCode only. Verified.)*
   The live contract is unambiguous — `internal/plugin/hostcap/servers.go:900-906`: *"Populate
   next_cursor from the **oldest (first)** event in the page … ReplayTail returns events in ascending
   order (oldest→newest), so **index 0** is the boundary"* → `nextCursor = protoEvents[0].GetCursor()`;
   `internal/plugin/hostfunc/stdlib_focus.go:452-462` → `ID: events[0].ID`;
   `cmd/holomush/sub_grpc.go:913-915` confirms ascending order. But the plan says:
   - `07-08-PLAN.md:292` — "the way `hostcap/servers.go` does (encode from the **last** event of each
     page)" — **misdescribes the very file it cites as precedent**;
   - `07-08-PLAN.md:379` — "Call site (`:901`): pass the **last** event's real `e.Seq`" (`:901` is a
     comment line; the anchor is `protoEvents[0]`).

   Using the newest event's seq as an exclusive upper bound **reintroduces near-full page repeats** —
   the very D-07 bug the plan exists to fix, by a different mechanism — and it would pass every
   acceptance criterion, because they test that a **real** `Seq` arrives, not that the **right**
   event's Seq arrives. Note the per-event encode is separate and correct: each event carries its own
   `e.Seq`, and `next_cursor` then simply reuses `protoEvents[0]`'s cursor.

### Agreed Concerns (both reviewers)

4. **07-08's Task 2 `<done>` asserts a retracted fiction.** *(Codex MEDIUM + OpenCode MEDIUM.
   Verified.)* `07-08-PLAN.md:453` — "the `Seq == 0` ID-only fallback still works" — contradicts the
   plan's own `:118` ("**DOES NOT EXIST**. Policy settled: **restart from the tail**") and `:445`
   ("Do NOT write a test named for an 'ID-only fallback' … such a test would pin fiction"). A
   half-applied retraction: rev 3 killed the fiction in two places and left it asserted in a third.

5. **`files_modified` manifests are incomplete enough to break the mandated gates.** *(Codex HIGH +
   OpenCode MEDIUM.)* Removing `Subsystem.Start` hits callers outside 07-11's manifest —
   `internal/admin/socket/subsystem_test.go:74`, `internal/testsupport/integrationtest/real_abac.go:56`,
   `internal/testsupport/integrationtest/plugins.go:340`,
   `internal/eventbus/audit/subsystem_boot_gate_integration_test.go:58`. 07-09 moves verifier config
   from a concrete `Handlers` slice to a provider but omits literals like
   `internal/eventbus/audit/chain/verifier_subsystem_test.go:108`. These are compile failures, not
   cleanup. OpenCode separately flags 07-07's frontmatter as under-counted vs its own body.

### Agreed Strengths

- **The nine-wave DAG is logically ordered**, parallel manifests for 07-03∥07-05 and 07-04∥07-06 do
  not intersect, and convergence through 07-07 before event-model deletion is sensible.
- **The EventBus Prepare decision is source-grounded and correct** — `DontListen: true` +
  `nats.InProcessServer` make embedded NATS startup acquisition, not serving.
- **The audit split follows a real existing seam** — dependency acquisition and partition boot gates
  precede `p.start` (`internal/eventbus/audit/subsystem.go:273`).
- **The D-07 pagination *diagnosis* is accurate** (`internal/eventbus/bus.go:88`): zero seq means
  stream start/end, not an ID cursor. (The *action line* is wrong — blocker 3.)
- **07-10's shutdown closure correctly accounts for Go's deferred-argument evaluation.**
- **The rollback design correctly includes the failing subsystem and uses a fresh bounded context.**
- **07-01 fixes ARCH-05's heavy hit for real** — `cmd/holomush/gateway.go:20` imports `internal/grpc`
  outside `coreOnlyFiles`; extraction is mandatory, not an allowlist patch.

### Divergent Views

The two grounded reviewers had **near-zero overlap on blockers** — the same pattern as round 2.
OpenCode alone caught the 07-08 anchor; Codex alone caught the cycle and both 07-11 seam defects.
Neither is redundant; a single grounded reviewer would have shipped the other's blockers.

OpenCode rated overall **MEDIUM** (CONDITIONAL APPROVE, HIGH if 07-08 ships as-is); Codex rated
**HIGH** (NOT APPROVED). **Codex's verdict is correct** — OpenCode never found the cycle, so its
"conditional approve" was formed without knowledge of the phase's most severe defect. Weight the
verdict by what each reviewer actually saw, not by averaging.

Other single-reviewer items worth folding in: OpenCode flags 07-11 still deferring the
`ConfigureSystemBroadcaster` late-binding seam to the executor ("Decide explicitly and record", ~L611)
— residual "implementer decides" on real host lifecycle order; and notes `AdminSocketSubsystem`
currently has `DependsOn() == nil` (`internal/admin/socket/subsystem.go:64-66`).

### Recommended next step

`/gsd-plan-phase 7 --reviews` (rev 5). Blocker 3 and finding 4 are cheap editorial fixes to 07-08.
Blockers 1 and 2 need real design decisions on 07-11's admin-socket and PluginSubsystem rows —
both are cases of a disposition table asserting a seam the code does not have. The **cycle** is the
one that matters most: it is a cross-plan interaction defect that neither plan can see alone, and
the durable fix is Codex's suggestion — a test that runs the real production graph through `topoSort`
and proves acyclicity, so the next revision cannot reintroduce it.

**Process note for the next round:** the `gsd-plan-checker` passed these plans. Its iteration-2 pass
was deliberately scoped to a targeted re-check of 07-09/07-11, which is why 07-08's anchor prose was
never in scope. Scoping a re-check buys speed and costs coverage — after a rev 5, re-check the full
set, and specifically add a cross-plan check (does wave N+1's premise survive wave N's mutations?),
which is the class both the cycle and the "No cycle" proof fall into.
