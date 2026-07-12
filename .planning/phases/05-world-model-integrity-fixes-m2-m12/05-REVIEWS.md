---
phase: 5
review_round: 4
reviewers: [codex]
antigravity_status: "FAILED (exit 1, no output produced this round)"
reviewed_at: 2026-07-12T13:15:47Z
plans_reviewed: [05-01-PLAN.md, 05-02-PLAN.md, 05-03-PLAN.md, 05-04-PLAN.md, 05-05-PLAN.md, 05-06-PLAN.md, 05-07-PLAN.md, 05-08-PLAN.md, 05-09-PLAN.md, 05-10-PLAN.md, 05-11-PLAN.md, 05-12-PLAN.md, 05-13-PLAN.md, 05-14-PLAN.md, 05-15-PLAN.md]
prior_rounds: "r1 @4238fc876, r2 @81440289a, r3 @937b03910 (all incorporated); plans authored by Claude Fable 5 this round"
verdict: "NOT execution-ready per Codex (HIGH). Architecture sound; remaining = a few cheap correctness fixes + one SCOPE discovery (unmodeled production world-table writers) that is a decision, not just a plan tweak."
---

# Cross-AI Plan Review — Phase 5 (ROUND 4, revised 15-plan set)

Fourth-round review of the round-3 revision (which added 05-15 CharacterGenesisService + WriteIntent + the
relay Lease). **Antigravity failed to produce output this round (exit 1), so this is a Codex-only review** —
Codex has been the reliable, source-grounded reviewer every round, so this is still a strong signal.

Codex verdict: **NOT execution-ready, HIGH — one more revision.** BUT read the consensus below carefully:
the round-3 architecture HELD (envelope ownership, invariant tests, import guard, character-creation routing
+ compile fence all confirmed sound), and the remaining findings are NOT more architecture problems. They are
(a) a few cheap correctness fixes and (b) ONE genuine SCOPE discovery — production world-table writers the
plans (and every prior round, and the original CONTEXT) never modeled — which is a decision for the human,
not another silent mechanical replan.

---

## Codex Review (round 4)

# Summary

**Verdict: another revision is required. The plans are not execution-ready.**

Round 4 found three unresolved HIGH correctness gaps, plus caller-inventory and package-contract issues. The round-3 redesign substantially improved envelope ownership and character-creation routing, but `INV-WORLD-3` and `INV-WORLD-4` are still not genuinely enforceable as planned.

Risk assessment: **HIGH**.

# Round-3 Resolution Status

| Resolution | Status | Assessment |
|---|---|---|
| Atomic character creation | **STILL BROKEN overall** | The three paths are identified and routed through `CharacterGenesisService`, `Create` is removed from the narrow auth interfaces, and binding modes are preserved. However, guest/bootstrap outer-transaction claims are false with current player/role repositories, and another production character write bypasses the envelope entirely. |
| Envelope epoch/position ownership | **HOLDS** | `WriteIntent(intent, delta)` is consistently the sole allocator/finalizer across 05-05/05-06/05-10/05-15. Commands and executor do not finalize or stamp storage fields. |
| Relay lease + skip service | **STILL BROKEN** | Lease-bound DB operations are structurally sound, but skip-marker retry identity is unsafe, and the generation fence lacks a defined authoritative token. |
| Wave-1 caller inventory | **STILL BROKEN** | Several integration callers and access-policy test implementations are absent from `files_modified`; some are outside the verification scope. |
| INV-WORLD-1 real atomicity test | **HOLDS** | The plan requires rollback-neither, commit-both, and forced-outbox-failure rollback against a real world row. |
| Full eight-edge import guard | **HOLDS in 05-07** | All eight forbidden edges plus the composition allowlist are specified. However, 05-11’s genesis design contradicts the `outbox→postgres` prohibition unless an interface is added. |
| Genesis checkpoint idempotency | **HOLDS mechanically, incomplete operationally** | The checkpoint PK is a sound durable identity, but no production cutover/reset caller is planned. |
| Census bijection + restrictions | **STILL BROKEN** | The bijection is honestly scoped, but the SQL fence encounters known production writers that the plans neither migrate nor exempt. |

# Strengths

- The envelope ownership contract is now coherent. [05-06-PLAN.md:91](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-06-PLAN.md:91>) makes the repository write produce the delta and `WriteIntent` allocate epoch/position, finalize, persist, and return the envelope. The executor is explicitly prohibited from calling `Finalize`.
- The character-creation inventory correctly finds all three current paths:
  - Registered: [auth_handlers.go:496](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/grpc/auth_handlers.go:496>)
  - Guest: [guest_service.go:134](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/guest_service.go:134>)
  - Bootstrap admin: [admin.go:85](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/bootstrap/admin.go:85>)
- Removing `Create` from `auth.CharacterRepository`, `GuestCharacterRepository`, and `CharRepoAdapter` is a meaningful compile fence. Those escape hatches exist today at [character_service.go:17](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/character_service.go:17>), [guest_service.go:32](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/guest_service.go:32>), and [adapters.go:37](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/bootstrap/setup/adapters.go:37>).
- Per-path binding behavior is correctly modeled: registered `initial_bind`, guest `initial_bind_guest`, and bootstrap no binding.
- The checkpoint table fixes the earlier pruning/idempotency defect: `(game_id, epoch, aggregate_type, aggregate_id)` is the correct durable genesis key.
- The relay correctly acknowledges at-least-once semantics and no longer claims split-brain publication is impossible.

# Concerns

## HIGH — Production world writes still escape the guard and envelope

The proposed SQL fence says every mutation of `characters` and `scene_participants` outside `internal/world/postgres` must fail CI. [05-09-PLAN.md:16](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-09-PLAN.md:16>) and [05-09-PLAN.md:99](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-09-PLAN.md:99>) then incorrectly assert the current tree will be green.

Known production violations include:

- Character settings directly execute an unversioned, envelope-less `UPDATE characters`: [character_settings_repo.go:68](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/character_settings_repo.go:68>), [character_settings_repo.go:80](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/character_settings_repo.go:80>).
- The production `core-scenes` plugin directly mutates `scene_participants`, including create, join, leave, and ownership transfer: [store.go:205](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/plugins/core-scenes/store.go:205>), [store.go:781](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/plugins/core-scenes/store.go:781>), [store.go:1013](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/plugins/core-scenes/store.go:1013>), [store.go:1246](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/plugins/core-scenes/store.go:1246>).

Neither file appears in the rollout plans. Consequently:

- 05-09’s meta-test cannot pass.
- `MODEL-03` is false for character preference updates.
- `INV-WORLD-4` remains false.
- The scene-participant taxonomy/census covers `world.Service` methods but not the larger production scene store.

## HIGH — Guest/bootstrap “one commit” claims use repositories that ignore the ambient transaction

05-15 claims guest player + character + binding + envelope and bootstrap player + character + role + envelope commit in one transaction at [05-15-PLAN.md:28](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-15-PLAN.md:28>) and [05-15-PLAN.md:133](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-15-PLAN.md:133>).

The existing non-world repositories do not enroll in `world/postgres`’s private `txKey`:

- Player creation always executes on `r.pool`: [player_repo.go:31](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/postgres/player_repo.go:31>), [player_repo.go:52](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/postgres/player_repo.go:52>).
- Role creation always executes on `s.pool`: [role_store.go:57](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/role_store.go:57>), [role_store.go:59](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/role_store.go:59>).
- The ambient transaction is discoverable only through the unexported world-postgres context key: [helpers.go:28](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/helpers.go:28>), [helpers.go:33](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/helpers.go:33>).

Guest player creation therefore commits outside the character-genesis transaction. Bootstrap is worse: role insertion on another connection can wait on the uncommitted character FK owned by the outer world transaction.

The narrower guarantee—character + optional binding + envelope—is sound because `BindingRepository` uses the world transaction context at [binding_repo.go:26](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/binding_repo.go:26>). The larger per-path atomicity claim is not.

## HIGH — Skip recovery is not retry-idempotent

The intended ordering is publish marker, receive PubAck, then mark resolved: [05-07-PLAN.md:124](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md:124>). The action also requires each skip marker to use a fresh `core.NewULID()`: [05-07-PLAN.md:130](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md:130>).

If the CLI/service crashes after PubAck but before `MarkSkipResolved`, retry publishes the same position with a different event ID. JetStream dedup and the reference consumer both deduplicate by event ULID, so neither suppresses the second marker. Consumers can observe two distinct events for one `(epoch, feed_position)`, violating the feed-position identity underlying `INV-WORLD-3`.

The marker needs a stable identity persisted before publication, or consumers must enforce idempotency by `(game_id, epoch, feed_position)` as well as event ID.

## MEDIUM — Lease generation fencing is underspecified

The Lease abstraction does bind operations to the lock-holding connection, which is good. But the plan says `AcquireLease` “bumps a generation” and `MarkPublished` verifies it at [05-07-PLAN.md:130](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md:130>), while:

- `MarkPublished` takes no generation argument.
- Migration 000050 defines no durable lease-generation state: [05-05-PLAN.md:83](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-05-PLAN.md:83>).

The implementation could validate advisory-lock ownership on the bound connection, but that is not the generation fence described by the plan. The source of truth and comparison point need to be explicit.

## MEDIUM — Caller inventory is still incomplete

05-14 claims a mechanically complete caller inventory at [05-14-PLAN.md:60](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md:60>), but omitted callers include:

- Seven direct `LocationRepository.Create` calls in `test/integration/auth/multi_tab_test.go`, beginning at [multi_tab_test.go:51](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/test/integration/auth/multi_tab_test.go:51>).
- Access-policy mocks implementing the old full writer interface at [character_test.go:44](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/access/policy/attribute/character_test.go:44>) through [character_test.go:60](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/access/policy/attribute/character_test.go:60>). These will stop satisfying the changed `world.CharacterRepository`.
- 05-15 changes `NewGuestService`/`NewCharacterService` wiring but omits integration callers such as [auth_suite_test.go:135](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/test/integration/auth/auth_suite_test.go:135>) and [harness.go:398](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/testsupport/integrationtest/harness.go:398>) from `files_modified`.

The scoped 05-14 verification also does not compile `internal/access/...`, so that omission can escape the claimed atomic wave.

## MEDIUM — Genesis exists only as library code, not a cutover/reset operation

05-11 promises genesis emission “at cutover” at [05-11-PLAN.md:25](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md:25>), but its files are limited to store/orchestration/tests: [05-11-PLAN.md:7](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md:7>). No lifecycle subsystem, bootstrap registration, migration hook, or admin CLI invokes genesis or epoch reset.

There is also a package-contract ambiguity: [05-11-PLAN.md:37](</Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md:37>) links `outbox/genesis.go` to `postgres/genesis_store.go`, while 05-07 forbids `outbox→postgres`. A consumer-owned `GenesisStore` interface and composition-root wiring are needed.

# Suggestions

1. Add a dedicated plan or extend 05-09/05-11 to migrate `CharacterSettingsRepository` into the guarded executor and reconcile every `core-scenes` `scene_participants` mutation with the taxonomy/outbox contract.
2. Make player and role repositories transaction-aware through a shared exported transaction executor/context contract, then test forced rollback of all guest/bootstrap rows. Alternatively, narrow 05-15’s atomicity claims and explicitly accept orphan-player/admin-role compensation—but bootstrap’s FK blocking still needs resolution.
3. Persist a stable skip-marker event ID before publishing; retry the same marker identity. Add a crash-after-PubAck-before-resolve test.
4. Define the lease generation’s authoritative storage and comparison semantics, or replace the claim with explicit advisory-lock ownership validation.
5. Re-run the caller scan and add `internal/access/**`, `test/integration/auth/multi_tab_test.go`, and every changed-constructor caller to the plans and verification lanes.
6. Give genesis/reset a real operator or lifecycle entry point and inject its postgres implementation behind an outbox-owned interface.

# Risk Assessment

**HIGH.** The core writer ownership is now sound, but known production writes bypass the proposed boundary, bootstrap transaction composition can block or partially commit, and skip recovery can emit two identities at one feed position. These affect the invariants the phase intends to bind, so execution should wait for round 5.

---

## Antigravity Review (round 4)

**FAILED** — `agy -p` exited non-zero (exit 1) and produced no usable output; the transcript fallback
recovered only a `null` response. No Antigravity findings this round. (Codex-only round.)

---

## Consensus Summary (round 4)

**Verdict: NOT execution-ready per Codex (HIGH). But this is a DECISION POINT, not just another loop
iteration.** The round-3 architecture is confirmed sound — the remaining findings are a few cheap correctness
fixes plus ONE scope discovery that expands Phase 5 beyond its CONTEXT and needs a human decision.

### What HELD (round-3 fixes confirmed sound by Codex)

Envelope epoch/position ownership (`WriteIntent` is the sole allocator/finalizer, executor prohibited from
finalizing); INV-WORLD-1 real world-row+envelope atomicity test; the full 8-edge import guard (05-07); the
genesis checkpoint PK as a durable idempotency key; the three character-creation paths correctly identified
and routed through `CharacterGenesisService` with `Create` removed from the auth interfaces (a real compile
fence); at-least-once correctly acknowledged. **The core writer-ownership architecture is settled.**

### Group A — Cheap correctness fixes (real bugs, small plan edits)

1. **Skip recovery is not retry-idempotent (HIGH).** Crash after PubAck but before `MarkSkipResolved` → retry
   publishes the same `(epoch, feed_position)` with a *fresh* `core.NewULID()` (05-07:130); JetStream + the
   consumer dedup by event ULID, so the second marker is NOT suppressed → two events at one feed position →
   violates INV-WORLD-3. Fix: persist a stable skip-marker event ID before publishing (retry the same
   identity), OR have consumers enforce idempotency by `(game_id, epoch, feed_position)` too. Add a
   crash-after-PubAck-before-resolve test.
2. **Lease generation fence underspecified (MEDIUM).** `MarkPublished` takes no generation argument and
   migration 000050 defines no durable lease-generation state (05-07:130, 05-05:83). Either define the
   generation's authoritative storage + comparison point, or replace the "generation fence" claim with
   explicit advisory-lock-ownership validation on the bound connection.
3. **Genesis package-contract bug + no operator entry point (MEDIUM).** 05-11 links `outbox/genesis.go` →
   `postgres/genesis_store.go`, which violates 05-07's own `outbox→postgres` prohibition — needs a
   consumer-owned `GenesisStore` interface. And genesis/epoch-reset exists only as library code: no lifecycle
   subsystem / bootstrap registration / admin CLI actually invokes it "at cutover" (05-11:25).

### Group B — Atomicity gap (moderate refactor)

4. **Guest/bootstrap "one transaction" claims are false (HIGH).** `player_repo` (player_repo.go:31/52) and
   `role_store` (role_store.go:57/59) execute on their own pool and do NOT enroll in world/postgres's private
   `txKey` (helpers.go:28/33). So guest player creation commits OUTSIDE the genesis transaction, and bootstrap
   role insertion on another connection can block on the outer transaction's uncommitted character FK. The
   NARROW guarantee (character + binding + envelope) IS sound (BindingRepository uses the world tx context,
   binding_repo.go:26); the BROAD per-path atomicity claim is not. Fix: make player/role repos tx-aware via a
   shared exported transaction executor (a real refactor across internal/auth/postgres + internal/store), OR
   narrow 05-15's claims and accept documented orphan-player/admin-role compensation — but bootstrap's FK
   blocking still needs resolving either way.

### Group C — SCOPE DISCOVERY (the important one — a decision, not a replan)

5. **Production world-table writers the plans NEVER modeled (HIGH).** The AST SQL fence (05-09) forbids
   `UPDATE characters` / `scene_participants` mutations outside `internal/world/postgres` and asserts the
   current tree is green. It is NOT:
   - `character_settings_repo.go:68/80` (internal/store) does an unversioned, envelope-less `UPDATE characters`.
   - The **`core-scenes` plugin** directly mutates `scene_participants` — create/join/leave/ownership-transfer
     — at `plugins/core-scenes/store.go:205/781/1013/1246`.
   Neither is in any rollout plan. Consequences: 05-09's meta-test cannot pass; MODEL-03 is false for character
   preference updates; INV-WORLD-4 remains false; the census covers `world.Service` methods but not the plugin.

   **Why this is a decision, not a fix:** the plans' fence + INV-WORLD-4 ("exactly one envelope per
   externally-visible command") implicitly commit Phase 5 to migrating EVERY world-table writer in the entire
   codebase — including character *preferences* and a *plugin's* scene-participant management — which is far
   larger than the CONTEXT's stated scope (4 world tables + ~15-20 `world.Service` commands; "zero product
   projections in Phase 5"; the scene plugin is a product feature). Resolving this means a scope call:
   (a) expand Phase 5 to migrate character_settings + reconcile the core-scenes plugin's scene_participants
   writes into the outbox/taxonomy (large), OR (b) narrow the SQL fence + INV-WORLD-4 to the in-scope surface
   and file an explicit follow-up issue for character_settings + core-scenes with documented rationale (small,
   but changes what the phase promises). This should be a deliberate decision, not an implicit replan outcome.

### Recurring meta-issue: caller-inventory completeness (MEDIUM)

The interface-change blast radius has been under-estimated THREE rounds running (r2: auth callers; r3:
test/harness callers; r4: `internal/access/**` mocks at character_test.go:44-60, `multi_tab_test.go:51`'s 7
LocationRepository.Create calls, auth_suite_test.go:135, harness.go:398). The hand-maintained `files_modified`
list keeps missing callers, and the scoped verification doesn't even compile `internal/access/...`. The durable
fix is a MECHANICAL gate — a full `go build ./...` + integration-tag compile across the WHOLE tree as the
wave-1 acceptance — not another hand-added file list that will just recur.

### Honest assessment of the loop

Four rounds in, Codex still returns HIGH — but this is NOT "the reviewer will never be satisfied." The
severity has genuinely fallen (r1 mechanism → r2 import cycles → r3 write-path coverage → r4 crash-recovery
edges + a scope discovery), and Codex explicitly confirms the core architecture is now sound. What remains:
- Group A (cheap fixes) + the caller-inventory mechanical gate: a targeted `--reviews` pass handles these and
  should converge.
- Group B (player/role tx-awareness): a real but bounded refactor.
- Group C (scope): a genuine human decision about how much of the codebase's world-write surface Phase 5 owns.

### Recommended next step

This is a fork, not an obvious "run --reviews again":
1. **If you want to keep going:** a round-4 `/gsd-plan-phase 5 --reviews` that (a) fixes Group A + B, (b)
   replaces the manual caller list with a whole-tree compile gate, and (c) makes an EXPLICIT scope decision on
   Group C — most likely narrowing the fence/INV-WORLD-4 to the in-scope world surface + filing a follow-up
   issue for character_settings + core-scenes, rather than absorbing a plugin migration into Phase 5. With
   Group C decided, this should be the converging pass.
2. **If the scope question deserves deliberate treatment:** step back to `/gsd-discuss-phase 5` (or a scoped
   decision) on the fence-coverage question specifically, then replan.
3. **If you want to cap the investment:** the architecture is sound enough to execute the in-scope core with
   Group A/B fixed and Group C explicitly deferred as documented known-gaps — but that requires the scope
   decision either way (the fence can't assert greenness while those writers exist).

Antigravity produced nothing this round, so a re-run (or adding `--gemini`/`--claude` on a non-authoring
model) would restore a second voice if you want cross-model confirmation of Codex's round-4 findings.
