---
phase: 5
round: 8
reviewers: [codex, opencode, antigravity]
reviewed_at: 2026-07-12T17:53:13Z
plans_reviewed: [05-01-PLAN.md,05-02-PLAN.md,05-03-PLAN.md,05-04-PLAN.md,05-05-PLAN.md,05-06-PLAN.md,05-07-PLAN.md,05-08-PLAN.md,05-09-PLAN.md,05-10-PLAN.md,05-11-PLAN.md,05-12-PLAN.md,05-13-PLAN.md,05-14-PLAN.md,05-15-PLAN.md,05-16-PLAN.md]
verdict: NOT READY (R6-1..R6-4 confirmed closed by both; R6-5 consumer-durability residual holes — Codex HIGH / grok MEDIUM; agy false-green)
---

# Cross-AI Plan Review — Phase 5 (Round 8)

> Round-8 verification of the round-7 fixes (commit ed60c0563) to the round-6 blockers. Source-grounded against the live tree. Round-6 REVIEWS preserved in git at 91980fec1.

---

## Codex Review (gpt-5.6-sol) — NOT READY

# Round 8 plan review

Reviewed HEAD `ed60c05638b8d3b23a3b82c5742653d0f80c197f` against the live tree.

## Round-7 fix verification

| Fix | Assessment | Source-grounded result |
|---|---|---|
| R6-1 version-bearing reaper list | **Correct and implementable** | The existing adapter is genuinely version-blind: its query omits `version` and its scan leaves `Character.Version == 0` ([adapters.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/bootstrap/setup/adapters.go:73)). The concrete world repository currently has no `ListByPlayer` method ([character_repo.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/character_repo.go:29)). Plan 05-03 adds the method to that exact concrete type, scans the stored version, updates the adapter, and 05-16 explicitly injects that concrete version-bearing lister into the reaper. The CAS delete will receive a real stored version. |
| R6-2 reaping-lock/genesis-reject | **Correct and implementable** | All three production creation paths currently converge on the seams 05-15/05-16 modify: registered creation persists through `CharacterService` ([character_service.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/character_service.go:63)), guest creation writes directly inside its transaction ([guest_service.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/guest_service.go:134)), and bootstrap calls the same character service ([admin.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/bootstrap/admin.go:85)). 05-15 removes those direct `Create` interfaces and routes all three through `CharacterGenesisService`; 05-16 puts `EnsureNotReaping` before its insert ([05-16](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-16-PLAN.md:135)). The two interleavings are sound: genesis-first holds the player row until character commit, after which marking waits then enumeration sees the character; mark-first causes genesis to observe `reaping_at` and abort. There is no remaining mark→enumerate window in which an admitted creation can occur. |
| R6-3 property cascade parity | **Correct and implementable** | Operator deletion removes properties with parent type `"character"` before deleting the character in the same transaction ([service.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/service.go:616)). `entity_properties` has no character FK ([000001_baseline.up.sql](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/migrations/000001_baseline.up.sql:350)), and `DeleteByParent` matches `(parent_type, parent_id)` exactly ([property_repo.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go:164)). 05-16 now uses the same string, ULID, ordering, and ambient transaction before writing the tombstone. |
| R6-4 location-delete phantom | **Correct and implementable** | Both exit FKs reference `locations` with `ON DELETE CASCADE` ([000001_baseline.up.sql](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/migrations/000001_baseline.up.sql:113)). Locking the parent row `FOR UPDATE` before selecting children prevents a later FK-validating child insert from crossing the preselect/delete window; the planned adversarial blocked-then-failed insert test directly proves the required interleave ([05-02](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-02-PLAN.md:78)). |
| R6-5 receipts/watermark/`ApplyOnce`/bootstrap | **Residual correctness holes — not ready** | Splitting receipts from watermarks is correct, but the transaction and bootstrap protocols still do not establish the claims made by 05-07. Details below. |

## 05-01–05-04 and 05-14 — version guard foundation

### Summary

The version-column, version-scanning, CAS, locked classifier, transaction enrollment, and RMW-threading plans now form an implementable MODEL-03 sequence.

### Strengths

- 05-03 targets the real version-blind adapter query and the real concrete repository.
- 05-02’s parent-lock-first location deletion closes the child-insert phantom rather than merely testing the non-adversarial case.
- The re-entrant transaction work is necessary: current `InTransaction` always starts a new transaction ([transactor.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/transactor.go:25)), while repository enrollment depends on the package-private context key ([helpers.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/helpers.go:28)).
- R6-1 now has an end-to-end proof: stored version read → reaper argument → version-predicated delete.

### Concerns

No new blocking concern in this group.

### Suggestion

Retain the exact interleave test in 05-02. A simple “delete with existing exits” test would not bind the concurrency property.

**Risk: MEDIUM** — broad interface blast radius, but the whole-tree production/unit/integration gates are appropriate.

## 05-05–05-08 — outbox, relay, and reference consumer

### Strengths

- Separate receipt and watermark tables are the right schema.
- `ApplyOnce` puts receipt claim, effect invocation, and watermark update under one transaction owner.
- The relay’s lease, stable skip identity, generation fencing, and explicit at-least-once language are materially stronger than earlier rounds.
- The existing event bus really uses a finite dedup horizon ([publisher.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/publisher.go:64)), so durable receipts are justified.

### Concerns

#### HIGH — `ApplyOnce` still cannot guarantee that an arbitrary effect joins its transaction

05-07 says passing `txCtx` makes the visible effect atomic with the receipt and watermark ([05-07](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md:189)). But the transaction token is a private `world/postgres` context key, and only repositories explicitly calling `execerFromCtx` recognize it ([helpers.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/helpers.go:28)). An arbitrary callback can use its own pool, perform network I/O, or mutate memory and still return success.

The API therefore establishes atomicity only for explicitly transaction-aware world-postgres effects, not for the generic “visible effect” claimed by the plan.

**Required change:** constrain the contract to a concrete transactional effect. Options include:

- pass a `pgx.Tx`/narrow DB executor to the callback;
- place the reference effect SQL inside the postgres implementation;
- or define a transaction-aware effect interface and prohibit external/non-transactional side effects.

The failure-boundary tests must use a real durable effect, not an in-memory counter.

#### HIGH — “strictly greater” watermark advancement can skip an unapplied position

The proposed watermark update only prevents rewind ([05-05](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-05-PLAN.md:85)). It does not establish a contiguous processed prefix.

Example:

1. Position 11 commits first and advances the watermark to 11.
2. Position 10 is still in flight.
3. The process crashes before 10 commits.
4. Restart resumes at 12, permanently skipping 10.

Likewise, an old event below the current watermark still runs its effect before the conditional watermark update declines to move backward.

**Required change:** either prove and enforce one-at-a-time processing per `(consumer, game)` or make `ApplyOnce` require exactly the next contiguous position. A robust store should lock the watermark row, reject/hold gaps, and advance only from `N` to `N+1`; duplicates below the watermark must not execute their effect.

#### HIGH — bootstrap reads the wrong watermark

05-07 proposes reading the consumer watermark and canonical DB snapshot in one repeatable-read transaction, then subscribing from the next position ([05-07](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md:198)).

That does not align the snapshot with the consumer watermark:

- PostgreSQL world state is canonical, while the feed can lag it ([ADR](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/docs/adr/holomush-i4784-world-state-model-decision.md:75)).
- A snapshot may therefore contain mutations beyond the consumer’s processed watermark.
- Tailing from `consumer watermark + 1` reapplies those already-snapshotted mutations.
- Conversely, the existing event-bus resume cursor is JetStream consumer sequence, not application `feed_position` ([server.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/grpc/server.go:1048)); `feed_position` exists only inside the new payload.

**Required change:** capture the world feed high-water position from `world_feed_counter` in the same repeatable-read snapshot, then establish a concrete JetStream handoff. For example, create/pause the durable before the snapshot and discard through the captured high-water, or persist a mapping from feed position to JetStream sequence. “Subscribe from next feed position” is not an existing EventBus operation.

#### MEDIUM — game identity is underspecified at the wire/store boundary

The planned `EnvelopeIntent` and `Envelope` fields omit `game_id` ([05-05](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-05-PLAN.md:121)), while:

- outbox ordering and watermarks are per game;
- `ApplyOnce` needs a game ID to update `(consumer_name, game_id)`;
- `eventbus.Qualify` requires both `gameID` and a relative reference ([qualify.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/qualify.go:23)).

The plan’s pseudo-call `eventbus.Qualify(events.<game_id>...)` does not match the live two-argument API.

**Suggestion:** make game identity explicit in `Envelope` or in mandatory relay/consumer constructor state, and add a round-trip test proving outbox row → envelope → qualified subject → consumer watermark preserves the same game ID.

**Risk: HIGH**

## 05-09–05-11 and 05-15–05-16 — rollout, genesis, and reaping

### Strengths

- R6-2 now covers all live production creation paths and removes the existing gRPC fallback ([auth_handlers.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/grpc/auth_handlers.go:517)).
- The reaping marker is durable, precedes enumeration, and remains set after partial failure.
- Reaping uses one transaction per character, preserving already-committed tombstones when a later character conflicts.
- The player delete remains correctly ordered after all tombstones; its current FK cascade behavior is explicit in both code and migration ([player_repo.go](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/postgres/player_repo.go:326), [000002_player_is_guest.up.sql](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/migrations/000002_player_is_guest.up.sql:9)).
- The deterministic two-interleaving race test is the right acceptance test for the new reaping protocol.

### Concerns

#### MEDIUM — 05-11 consumes 05-16 artifacts without depending on 05-16

05-11 is wave 10 and depends only on 05-10 and 05-15 ([05-11](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md:5)). 05-16 is also wave 10 ([05-16](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-16-PLAN.md:5)).

But 05-11 Task 3 reads `internal/auth/character_reaping.go` and asserts its producer entry ([05-11](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md:157)). Wave-parallel execution can therefore run the census before 05-16 creates the file.

**Required change:** make 05-11 depend on 05-16 and move it to a later wave, or move the out-of-service producer census into 05-12, which already depends on both.

**Risk: MEDIUM**

## 05-12–05-13 — invariants and documentation

### Summary

The numeric invariant IDs, genuine bindings, migration-aware writer fence, and semantic documentation guard remain coherent.

### Strengths

- INV-WORLD-4 now includes the guest-reaping regression rather than binding only structural fences.
- INV-WORLD-1 is bound to an always-run real-row plus envelope atomicity test.
- The documentation plan preserves genuine client catch-up replay language.

### Concerns

No new concern beyond the fact that INV-WORLD-3/consumer claims must not be marked bound until the R6-5 protocol is corrected.

**Risk: LOW after the consumer corrections; HIGH if invariants are bound to the current R6-5 design.**

# Verdict

**NOT READY**

The most important reason is that the round-7 R6-5 repair still does not establish a correct durable-consumer protocol: `ApplyOnce` cannot structurally enroll an arbitrary effect, the watermark can advance across gaps, and bootstrap aligns the canonical snapshot with consumer progress rather than the feed high-water position. Those defects can cause skipped or double-applied feed effects despite all proposed tests passing.

---

## OpenCode Review (openrouter/x-ai/grok-4.5) — READY

I'll verify the round-7 fixes against live source (R6-1…R6-5), starting with the grepping skill and the symbols those fixes claim to touch.
Verifying R6-1…R6-5 against live source now.
Digging into cascade FKs, players schema, character repo methods, and the advisory-lock / consumer patterns.
# Round 8 Cross-AI Plan Review — Phase 5

**HEAD:** `ed60c0563` · **branch:** `gsd/phase-05-world-model-integrity-fixes-m2-m12`  
**Scope:** verify round-7 fixes to round-6 R6-1…R6-5 against live source; do not re-litigate settled R1–R5 design.

---

## Primary focus: R6 fix verification

### R6-1 — version-scanning `ListByPlayer` (+ adapter) — **CORRECT / IMPLEMENTABLE**

| Claim | Live source |
|---|---|
| No `ListByPlayer` on `worldpostgres.CharacterRepository` | **TRUE** — methods are `Get/Create/Update/Delete/GetByLocation/UpdateLocation/IsOwnedByPlayer/GetNamesByIDs/ListAll` only (`character_repo.go:30–249`). |
| `CharRepoAdapter.ListByPlayer` is version-blind | **TRUE** — `internal/bootstrap/setup/adapters.go:77–92` SELECTs `id, player_id, name, description, location_id, created_at` and never sets `Version`. |
| Same blind shape in harness | **TRUE** — `internal/testsupport/integrationtest/harness.go:1401–1405`. |
| `Get` also version-blind today | **TRUE** — `character_repo.go:31–34` SELECT omits `version` (expected pre-05-03). |

Plan targets are real. After 05-03 adds version-scanning `ListByPlayer` and adapter updates, reaper’s CAS `Delete(ctx, id, char.Version)` will see stored `version DEFAULT 1`, not `0`. No residual correctness hole.

**Note (LOW):** 05-16’s `depends_on: [05-15, 05-09]` omits 05-03; wave order still enforces it. Prefer explicit `depends_on: [05-03, …]` for worker self-containment.

---

### R6-2 — reaping-lock + genesis-reject TOCTOU close — **CORRECT / IMPLEMENTABLE**

| Claim | Live source |
|---|---|
| Cascade hole | **TRUE** — `000002_player_is_guest.up.sql:9–13` `characters.player_id ON DELETE CASCADE`; `DeleteGuestPlayer` only `DELETE FROM players … is_guest=true` (`player_repo.go:331–344`). |
| Cleaner/reaper path | **TRUE** — `guest_reaper.go:90` → `GuestCleaner.DeleteGuestPlayer`; composition at `cmd/holomush/sub_grpc.go:715`. |
| Failed-guest cleanup | **TRUE** — `guest_service.go:195–200` → **`s.players.Delete`** (not `DeleteGuestPlayer`) — same cascade hole. Plan correctly routes both. |
| Dual-pool blocks single shared tx | **TRUE** — player_repo uses `r.pool` directly; world enrolls only via private `txKey` (`helpers.go:29–42`, `transactor.go:34`). Option (b) is the only sound choice. |

Cross-connection serialization is sound in PG:

1. Genesis holds `SELECT reaping_at … FOR UPDATE` on the genesis tx conn for the whole creation tx.  
2. `MarkReaping`’s `UPDATE players SET reaping_at=…` on the player pool **blocks** on that row, then sees the committed character on enumerate.  
3. Post-`MarkReaping`, genesis sees non-null `reaping_at` and rejects with `PLAYER_REAPING`.

No residual cascade window under concurrency for production paths that go through `CharacterGenesisService` (enforced by 05-15 Create-removal + 05-16 guard wire-in). Deterministic race tests in the plan cover both interleavings.

**Residuals (not blockers):**

| Severity | Finding |
|---|---|
| **MEDIUM** | `PlayerReapingGuard` in `internal/world/postgres` deliberately SELECTs auth `players` via world `execerFromCtx` for lock enrollment — layering smell, but required for FOR UPDATE on the genesis tx. Document “auth table on world conn” next to the fence exemption. |
| **LOW** | No clear/unmark of `reaping_at` — intentional (player stays closed until deleted). Operator unstick = next reap / recast delete. |
| **LOW** | Adding `reaping_at` does not break existing explicit player SELECTs (no `SELECT *`). |

---

### R6-3 — property cascade in reaping tx — **CORRECT / IMPLEMENTABLE**

| Claim | Live source |
|---|---|
| Operator delete cascade | **TRUE** — `service.go:616–629`: `propertyRepo.DeleteByParent(ctx, "character", id)` **then** `characterRepo.Delete`. |
| No FK on properties | **TRUE** — `entity_properties` is parent_type/parent_id TEXT only (`000001_baseline.up.sql:350–364`). |
| Enrolls in ambient tx | **TRUE** — `property_repo.go:165–168` uses `execerFromCtx`. |
| parent_type | **"character"** — matches `EntityProperty` comment (`property.go:18`). |

Parity with `DeleteCharacter` is source-faithful. One-tx (props + CAS delete + envelope) is achievable via re-entrant `InTransaction`.

---

### R6-4 — parent-location `FOR UPDATE` before exit preselect — **CORRECT / IMPLEMENTABLE**

| Claim | Live source |
|---|---|
| Dual-sided CASCADE | **TRUE** — `exits.from_location_id` and `to_location_id` both `REFERENCES locations(id) ON DELETE CASCADE` (`000001_baseline.up.sql:115–116`). |
| Parent lock kills child-insert phantom | **TRUE under PG lock rules** — child INSERT takes KEY SHARE on parent; location `SELECT … FOR UPDATE` conflicts with KEY SHARE, so concurrent exit insert either precedes the lock (preselected) or blocks then fails after parent delete. |

Interleave integration test in 05-02 is the right binding. Residual **LOW:** implementer must share the classifier’s parent lock with the cascade preselect (one `withTx` connection) — plan already requires this.

---

### R6-5 — receipts/watermarks + `ApplyOnce` — **CORRECT / IMPLEMENTABLE**

| Claim | Live source |
|---|---|
| Finite JetStream dedup | **TRUE** — `publisher.go:64/109`, `subsystem.go:282` `Duplicates: DupeWindow`. |
| tx enrollment is private | **TRUE** — only world/postgres `transactor` installs `txKey` (`transactor.go:34`), `execerFromCtx` reads it (`helpers.go:42–46`). Checkpoint-only API cannot intentionally enroll external effects. |
| Split receipts vs watermark | **REQUIRED** — single conflated table cannot be ticker-monotonic per (consumer, game). |

`ApplyOnce` order (claim receipt → `effect(txCtx)` → monotonic watermark → commit) is sound; three failure-boundary tests bind atomicity; claim-before-effect closes double-apply on redelivery.

**Residuals:**

| Severity | Finding |
|---|---|
| **MEDIUM** | Monotonic compare must be specified as lexicographic SQL: `WHERE epoch < $e OR (epoch = $e AND feed_position < $p)` (or isomorphic). Plan says “strictly greater (epoch, feed_position)” but never shows the predicate — easy to botch with separate comparisons. |
| **LOW** | Session-advisory-lease via dedicated `pool.Acquire` + `pg_advisory_lock` is greenfield (only in-tree precedent is `pg_advisory_xact_lock` at `player_session_store.go:79`). Plan’s seam-test for drop→re-acquire is load-bearing; keep it non-optional. |

---

## Strengths (with evidence)

1. **R6-1 targets correct production writers** — `CharRepoAdapter.ListByPlayer` at `adapters.go:74` and absence of world `ListByPlayer` are the real holes; plan names them.
2. **R6-2 prefers creation-side reject over false single-tx claims** — respects real dual-pool reality (`player_repo` `r.pool` vs world `txKey`).
3. **R6-3 mirrors operator delete** — same `"character"` parent_type and order as `service.go:616–622`.
4. **R6-4 closes a real FK phantom** — dual ON DELETE CASCADE on exits is real (`000001:115–116`).
5. **R6-5 `ApplyOnce` matches how enrollment actually works** — private `txKey` end-to-end, not “call any effect and hope.”
6. **Honest sequence** — intermediate wave states (D-06 open until wave 10) are acceptable under D-04 one-phase PR.

---

## Concerns

### HIGH
*None remaining versus live source.*

### MEDIUM

1. **Watermark monotonic SQL underspecified (05-05/05-07)**  
   Document the exact conditional UPDATE predicate (lexicographic epoch, then position). Incomplete ranking after epoch advance is a quiet feed-resume bug.

2. **Auth table accessed from `world/postgres` (`reaping_guard.go`)**  
   Required for FOR UPDATE enrollment; still a durable layering exception. Capture in fence docs / allowlist commentary so it is not “fixed away” later.

3. **05-16 should `depends_on` 05-03**  
   Wave order covers it; plan-level dep does not. Fail-closed for out-of-order execute.

### LOW

1. Intermediate commits between 05-15 and 05-16 leave D-06 open (OK only because D-04 is one PR).  
2. No unmark path for `reaping_at` (acceptable; next reap resumes).  
3. Harness `authCharRepoAdapter.ListByPlayer` stays version-blind — OK for test-only seeding; don’t inject it into production reaping.

### Round-7 regression check
No new severity-HIGH defect introduced by the R6 fold-ins. Round-7 correctly:

- split consumer store  
- added parent-lock-first delete  
- added reaping_at + FOR UPDATE guard  
- forced versioned list for reaping CAS  
- mirrored DeleteByParent cascade  

---

## Suggestions (not re-openers)

1. In 05-05/05-07 task action, paste the watermark `UPDATE … WHERE …` SQL.  
2. Add 05-03 to 05-16 `depends_on`.  
3. On `reaping_guard.go`, one-line SPDX-adjacent comment: “reads auth.players on world tx conn for FOR UPDATE serialization with MarkReaping.”  
4. Keep session-lease drop→re-acquire and R6-2 race tests non-quarantined seam / required integration, respectively.

---

## Risk

**Risk: LOW–MEDIUM** for execute — remaining issues are implementer-spec gaps and layering documentation, not broken mechanisms.

---

## Verdict

# READY

**Most important reason:** All five round-6 HIGH blockers map to real source separations (version-blind list, dual-pool TOCTOU, property non-FK, dual exit CASCADE, private `txKey` enrollment), and the round-7 mechanisms close them with PG- and codebase-faithful designs; no residual HIGH remains under live verification.

---

## Antigravity Review (agy) — READY

> NOTE: 16-line review — still shallow relative to the two source-grounded reviewers; its overall READY verdict is weighted out, but its two minor gaps (test-adapter version scan; watermark UPSERT) are valid and folded into the consensus.

I have completed the source-grounded architectural review of the Round-7 fixes against the live HoloMUSH codebase. The full review has been compiled into the following artifact:

* **[05-REVIEW-ROUND8.md](file:///Users/sean/.gemini/antigravity-cli/brain/a4a74f32-f998-479d-8a1c-340e56ae7ed6/05-REVIEW-ROUND8.md)**

### Summary of Key Findings

1. **Overall Verdict**: **READY**. The Round-7 plan updates successfully resolve the 5 high blockers (reaper versions, TOCTOU concurrency race, property cascades, exit cascades, consumer transaction boundaries) and align perfectly with the live code's database pools and connection schema.
2. **Minor Gap #1 (Test Adapters Version Scan)**: The integration-test harness adapter `authCharRepoAdapter.ListByPlayer` in [harness.go:L1401-1444](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/testsupport/integrationtest/harness.go#L1401-1444) and the test adapter in [auth_suite_test.go:L303-320](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/test/integration/auth/auth_suite_test.go#L303-320) also define a version-blind `ListByPlayer` query. We recommend updating these test adapters to select/scan `version` during Wave 1 implementation so that subsequent writes/deletes do not fail on CAS expectedVersion checks.
3. **Minor Gap #2 (Consumer Watermark Upsert)**: The monotonic conditional update query for `world_consumer_watermarks` will match `0` rows if no watermark row exists yet. We recommend that `ApplyOnce` uses an **UPSERT** (`INSERT ... ON CONFLICT DO UPDATE ... WHERE ...`) rather than a simple `UPDATE` to handle the first event processed by a consumer.

### Recommended Next Steps

Since the plans are verified and structurally sound, you can begin executing the waves. You can use the following command to begin:
```bash
gsd-plan-phase 5
```

---

## Consensus Summary

**Round 8 — NOT READY, but narrowly: 4 of 5 round-6 fixes are now CONFIRMED closed by both source-grounded reviewers; the split is on R6-5 (consumer durability).**

### Reviewer weighting

- **Codex (gpt-5.6-sol)** — NOT READY. Source-grounded; confirmed R6-1..R6-4 implementable, found 3 residual HIGH holes in R6-5.
- **OpenCode / grok-4.5** — READY. Source-grounded; confirmed R6-1..R6-4 AND rated R6-5 OK, but flagged the R6-5 watermark predicate as MEDIUM and the session-lease as LOW.
- **Antigravity** — READY. 16 lines (still shallow relative to the two above), but this round its two minor gaps are VALID and folded below (test-adapter version scan; watermark UPSERT-for-first-event).

Both primary reviewers are reliable and **agree completely on R6-1..R6-4**. They **diverge on R6-5**: Codex did deeper tracing (out-of-order/crash/bootstrap failure scenarios) and found real mechanism-level holes; grok validated the happy-path ordering and did not stress the gap/bootstrap cases. When two reliable reviewers split, the one with concrete, traced failure scenarios wins — and grok's own MEDIUM on the same watermark area corroborates that R6-5 needs another pass. **Verdict weighted to Codex on R6-5.**

### CONFIRMED closed (both reviewers, source-grounded) — the round-6 HIGH blockers R6-1..R6-4

- **R6-1 version-scanning `ListByPlayer`** — CORRECT. 05-03 adds the method to the real concrete `worldpostgres.CharacterRepository` (which genuinely lacked it) and updates the version-blind `CharRepoAdapter.ListByPlayer` (adapters.go:77-92) to scan `version`; 05-16 injects that version-bearing lister → the reaper CAS `Delete(ctx,id,char.Version)` gets a real stored version. The round-6 fabricated-reader defect is closed at the source level.
- **R6-2 reaping-lock / genesis-reject TOCTOU** — CORRECT (both reviewers, both interleavings). Single shared tx correctly precluded (player/role two-pool boundary is real: `player_repo` uses `r.pool`, world enrolls only via private `txKey`). Cross-connection serialization is sound PG: genesis holds `SELECT reaping_at … FOR UPDATE`; `MarkReaping`'s UPDATE blocks on that row then sees the committed character on enumerate; post-mark genesis sees non-null `reaping_at` and rejects `PLAYER_REAPING`. **No remaining mark→enumerate window.** All 3 production creation paths route through `CharacterGenesisService` (05-15 Create-removal + 05-16 guard). This was the scariest new mechanism — it holds.
- **R6-3 property cascade parity** — CORRECT. 05-16's per-character tx mirrors `DeleteCharacter` (service.go:616-629): `propertyRepo.DeleteByParent(ctx,"character",id)` before the guarded Delete; signature `property_repo.go:165` matches exactly.
- **R6-4 location-delete phantom** — CORRECT. Parent `SELECT version … FOR UPDATE` before preselect+delete conflicts with the KEY SHARE lock a child-exit INSERT must take → closes the phantom; the adversarial interleave test binds it.

### NOT converged — R6-5 consumer durability (Codex HIGH; grok MEDIUM) — the remaining blocker cluster

The receipts/watermarks table split is correct, and `ApplyOnce`'s claim→effect→watermark→commit ORDER is sound. But three residual holes (Codex, source-grounded) mean the durable-consumer protocol is not yet correct:

- **[HIGH] `ApplyOnce` cannot structurally force an arbitrary effect into its transaction.** `func(txCtx) error` relies on the effect calling `execerFromCtx` (private world/postgres `txKey`); a callback can ignore `txCtx`, use its own pool / do network I/O, and still return success — atomicity holds only for explicitly tx-aware world-postgres effects, not the generic "visible effect" the plan claims. This is an authority-boundary issue for a reusable primitive. **Fix:** constrain the contract — pass a `pgx.Tx`/narrow DB executor to the callback, OR place the reference effect SQL inside the postgres impl, OR a tx-aware effect interface that prohibits external side effects; failure-boundary tests must use a real durable effect, not an in-memory counter.
- **[HIGH] "strictly greater" watermark advancement can skip an unapplied position (no contiguous prefix).** If position 11 commits/advances the watermark before 10, then a crash before 10 commits → restart resumes at 12, **permanently skipping 10**. And an old event below the watermark still runs its effect before the conditional update declines to rewind. **Fix:** enforce one-at-a-time processing per `(consumer, game)` OR require exactly the next contiguous position; lock the watermark row, hold/reject gaps, advance `N`→`N+1`, and make sub-watermark duplicates skip their effect. (grok independently flagged the same area MEDIUM: the monotonic compare must be a single lexicographic predicate `WHERE epoch < $e OR (epoch = $e AND feed_position < $p)`, not separate comparisons — corroborating the watermark needs precise spec.)
- **[HIGH] bootstrap aligns the snapshot with consumer progress, not the feed high-water.** PG world state is canonical and can contain mutations beyond the consumer watermark; tailing from `watermark+1` reapplies already-snapshotted mutations. Also `feed_position` is NOT the JetStream resume cursor (JS consumer sequence, server.go:1048) — "subscribe from next feed position" is not an existing EventBus op. **Fix:** capture the `world_feed_counter` high-water in the same repeatable-read snapshot + a concrete JetStream handoff (create/pause the durable before the snapshot and discard through the captured high-water, or persist a feed-position→JS-sequence mapping).

### MEDIUMs / cross-cutting to fold

- **game_id missing from `EnvelopeIntent`/`Envelope` (Codex MEDIUM).** Outbox ordering + watermarks are per-game, `ApplyOnce` updates `(consumer_name, game_id)`, and `eventbus.Qualify` needs `(gameID, ref)` (two-arg — the plan's `Qualify(events.<game_id>…)` pseudo-call doesn't match the live API). Make game identity explicit in `Envelope` or relay/consumer constructor state + a round-trip test.
- **watermark UPSERT for the first event (Antigravity, valid).** A bare conditional `UPDATE` matches 0 rows when no watermark exists yet → use `INSERT … ON CONFLICT DO UPDATE … WHERE …`.
- **05-11 (wave 10) reads `internal/auth/character_reaping.go` that 05-16 (also wave 10) creates (Codex MEDIUM).** Wave-parallel execution can run the census before the file exists. Make 05-11 `depends_on` 05-16 and move it later, OR move the out-of-service producer census into 05-12 (which already depends on both).
- **05-16 `depends_on` should include 05-03 (grok LOW).** Wave order covers it, but plan-level dep is fail-closed for out-of-order execute.
- **`reaping_guard.go` reads auth `players` on the world tx conn (both, MEDIUM/LOW).** Required for `FOR UPDATE` enrollment with `MarkReaping`; a durable layering exception — document it beside the SQL-fence allowlist so it isn't "fixed away."
- Don't mark INV-WORLD-3 / consumer invariants `bound` until the R6-5 protocol is corrected (Codex).

### Net verdict

**NOT READY — but the loop has nearly closed.** Four of the five round-6 HIGH blockers (R6-1..R6-4), including the two scariest new mechanisms' harder half (R6-2 reaping-lock), are now CONFIRMED closed by both source-grounded reviewers. The remaining work is a single well-scoped cluster: the **durable-consumer protocol (05-05/05-07)** — constrain `ApplyOnce`'s effect contract, make the watermark contiguity-safe (no gap-skip), and specify the bootstrap snapshot↔feed-position↔JetStream handoff — plus a handful of MEDIUMs (game_id in Envelope, 05-11/05-16 wave dep, watermark UPSERT). No new defect was introduced by the round-7 edits.

**Loop status:** converging hard, not thrashing — round 6 opened 5 HIGH blockers, round 7 closed 4 outright and reshaped the 5th; round 8 confirms the 4 and narrows the 5th to 3 concrete, bounded consumer-protocol fixes. Recommended next: `/gsd-plan-phase 5 --reviews` (round-9 incorporation) scoped to R6-5 + the MEDIUMs — do NOT execute yet. The R6-5 consumer holes (gap-skip especially) are real at-least-once/exactly-once correctness bugs that would pass the currently-planned tests.
