---
phase: 5
round: 6
reviewers: [codex, opencode, antigravity]
reviewed_at: 2026-07-12T16:38:18Z
plans_reviewed: [05-01-PLAN.md,05-02-PLAN.md,05-03-PLAN.md,05-04-PLAN.md,05-05-PLAN.md,05-06-PLAN.md,05-07-PLAN.md,05-08-PLAN.md,05-09-PLAN.md,05-10-PLAN.md,05-11-PLAN.md,05-12-PLAN.md,05-13-PLAN.md,05-14-PLAN.md,05-15-PLAN.md,05-16-PLAN.md]
verdict: NOT READY (2/2 source-grounded reviewers; antigravity false-green)
---

# Cross-AI Plan Review — Phase 5 (Round 6)

> Round-6 adversarial check of the round-5 output (16 plans, incl. new 05-16). Reviewers ran source-grounded against the live tree at commit 4b8d873b1 on branch gsd/phase-05-world-model-integrity-fixes-m2-m12. Round-5 REVIEWS preserved in git at 7b4d3fad8.

---

## Codex Review (gpt-5.6-sol)

# Round 6 Cross-AI Plan Review

## 05-01–05-04 + 05-14 — Version guard and repository foundation

### Summary

The two-outcome CAS classifier is sound, and the re-entrant transaction foundation addresses the actual repository topology. The main remaining defect is location-delete delta parity: preselecting exits with `FOR UPDATE` does not prevent a new referencing exit from appearing before the location delete.

### Strengths

- The migration targets the correct four aggregates. The current schema has no versions on `characters`, `locations`, `exits`, or `objects`, confirming the foundation is necessary (`internal/store/migrations/000001_baseline.up.sql:68`, `:93`, `:113`, `:135`).

- Re-entrant transactions are a real prerequisite. `Transactor.InTransaction` currently always begins a new transaction (`internal/world/postgres/transactor.go:27-34`), while repository operations use a context-held transaction through `execerFromCtx` (`internal/world/postgres/helpers.go:42-46`). Plan 05-14 correctly unifies these mechanisms.

- Adding `expectedVersion` to move operations is necessary. Current character movement has no version argument (`internal/world/postgres/character_repo.go:108-112`), so the plan closes a genuine CAS gap.

- The revised two-outcome classifier is correct. With only `(id, expectedVersion)`, an absent row cannot distinguish “never existed” from “another delete committed first.” Reporting both as not-found is honest; an extant row with another version remains `WORLD_CONCURRENT_EDIT`.

- Removing the vestigial world scene-participant write surface appears safe at the production-call level. Current direct world-service calls are confined to tests; production core-scenes calls its own plugin store (`plugins/core-scenes/service.go:1289`, `:1666`). Retaining the read methods and physical table is necessary because `public.scene_participants` remains part of the world schema (`internal/store/migrations/000001_baseline.up.sql:157-165`).

### Concerns

- **HIGH — Location cascade delta parity remains racy.** Plan 05-02 preselects matching exits with `SELECT ... FOR UPDATE` before deleting the location, but that locks only already-existing exit rows. The foreign keys cascade from both `from_location_id` and `to_location_id` (`internal/store/migrations/000001_baseline.up.sql:113-116`). A concurrent transaction can insert a new exit after the preselect and before the location delete; that exit can then be cascade-deleted without appearing in `MutationDelta`. The proposed INV-WORLD-2 binding would be false under this interleave.

- **LOW — The scene-removal grep is too broad as written.** A repository-wide search for `AddParticipant|RemoveParticipant` finds many legitimate production plugin calls, such as `plugins/core-scenes/service.go:1289` and `:1666`. The plan understands the namespace distinction, but its pre-removal gate should search qualified world symbols or restrict paths, rather than treating a noisy broad grep as authoritative.

### Suggestions

- In `LocationRepository.Delete`, first lock and classify the parent location row with `SELECT version ... FOR UPDATE`. Only then preselect the referring exits and issue the delete. The parent `FOR UPDATE` lock conflicts with foreign-key key-share acquisition and closes the child-insert phantom window.

- Add an integration test with this exact interleave:

  1. deletion transaction locks the location;
  2. a second transaction attempts to create an exit referencing it;
  3. deletion gathers children and commits;
  4. creation must fail or wait and then fail;
  5. the manifest must equal every deleted exit.

- Replace the D-07 preflight with searches for `world.Service.AddSceneParticipant`, interface calls through the world repository, and callers outside `_test.go`, while explicitly ignoring `plugins/core-scenes`.

### Risk Assessment

**HIGH** until the parent-lock ordering is added. Without it, the planned delta-parity invariant is bindable only by a non-adversarial test.

---

## 05-05–05-08 — Outbox, relay, consumer, and wire contract

### Summary

The envelope ownership, locked counter, lease abstraction, stable skip identity, and wire adapter are substantially improved. The durable consumer checkpoint addition is directionally correct but still lacks an implementable atomic-effect contract.

### Strengths

- The locked counter is the right ordering primitive. The plans correctly avoid `BIGSERIAL` and hold the per-game row lock until the outer transaction commits.

- Envelope ownership is now coherent: intent comes from the command, delta from the repository, and epoch/position from the PostgreSQL writer. This avoids constructing a finalized envelope before the committed delta exists.

- The wire-adapter design now matches the event bus. `eventbus.Event.Payload` is raw application payload (`internal/eventbus/types.go:141-149`), while the publisher wraps and codec-encodes it (`internal/eventbus/publisher.go:184-190`, `:280-293`). The publisher itself owns `Nats-Msg-Id` and the global schema header (`internal/eventbus/publisher.go:303-306`), so carrying the per-kind schema version inside the world payload is correct.

- `eventbus.Qualify` accepts a relative reference or passes through an already-qualified subject (`internal/eventbus/qualify.go:12-33`), supporting the proposed subject adapter without another custom prefixing mechanism.

- The stable skip-marker ID and generation-fenced database acknowledgement correctly acknowledge at-least-once delivery instead of claiming a PostgreSQL lease can retract an already-published NATS message.

### Concerns

- **HIGH — “Effect + checkpoint in one transaction” has no concrete transaction contract.** Plan 05-07 says the consumer applies an effect and records `world_consumer_checkpoint` atomically, but the proposed `ConsumerCheckpointStore` only describes checkpoint operations. Current transaction enrollment depends on a private PostgreSQL context value read by `execerFromCtx` (`internal/world/postgres/helpers.go:42-46`), and the current transactor is the component that installs it (`internal/world/postgres/transactor.go:27-35`). The plan does not give the consumer an injected transactor or a callback such as `ApplyOnce(ctx, event, effect func(txCtx) error)`. A generic consumer cannot guarantee that an arbitrary effect uses the same transaction merely because checkpoint SQL does.

- **MEDIUM — The checkpoint table conflates event receipts and the consumer watermark.** A primary key of `(consumer_name, event_id)` creates one row per event, while the “per-consumer watermark” is repeated on every row. The plan does not specify a monotonic conditional update, uniqueness per `(consumer_name, game_id)`, or how concurrent deliveries avoid moving the resume point backward.

- **MEDIUM — The reference-consumer bootstrap contract is underspecified.** “Read a snapshot + watermark, then tail” needs an explicit consistency boundary. Otherwise a mutation can commit between the snapshot and watermark read and be either missed or applied twice. The ordered feed provides the mechanism, but the plan needs the exact transaction/isolation sequence.

### Suggestions

- Define an explicit consumer-owned transaction interface, for example:

  ```go
  ApplyOnce(
      ctx context.Context,
      consumer string,
      envelope wmodel.Envelope,
      effect func(txCtx context.Context) error,
  ) (applied bool, err error)
  ```

  The PostgreSQL implementation should begin one transaction, claim the event ID, run the effect with the same transaction context, advance a monotonic watermark, and commit.

- Split persistence into:

  - `world_consumer_receipts(consumer_name, event_id, ...)`
  - `world_consumer_watermarks(consumer_name, game_id, epoch, feed_position)`

  Advance the watermark only when the new `(epoch, position)` is greater than the stored value.

- Specify bootstrap as one repeatable-read transaction: capture the feed watermark, read the snapshot consistent with that watermark, commit, then subscribe from the next position.

- Add failure tests at all three boundaries: effect succeeds/checkpoint fails, checkpoint claim succeeds/effect fails, and process dies before commit. Every case must leave both or neither visible.

### Risk Assessment

**HIGH** because durable dedup is relied upon to make relay duplicates harmless beyond JetStream’s finite deduplication window, but the atomic visible-effect mechanism is not yet implementable from the described interfaces.

---

## 05-06 — Mutation closure and movement-hook semantics

### Summary

The write-closure redesign resolves the operation-identity problem and correctly keeps writer repositories private. Treating the post-commit movement hook as command degradation is internally consistent with the locked decision, but the plan overstates eventual recovery.

### Strengths

- A closure built by a private executor method identifies the operation without string dispatch and supplies the actual `MutationDelta`.

- The plan correctly removes the existing partial-failure API. Today `MoveCharacter` commits the character location first (`internal/world/service.go:802-805`) and then may return `CHARACTER_MOVE_FAILED` with `move_succeeded=true` when the hook fails (`internal/world/service.go:807-819`). That behavior invites an unsafe retry.

- Moving the hook after state-plus-envelope commit gives the command result a single truth: the move happened.

### Concerns

- **MEDIUM — No source-grounded reconciliation mechanism exists for a failed movement hook.** The production hook writes derived location into the session store (`cmd/holomush/sub_grpc.go:840-849`). The current contract explicitly exists so session consumers observe the new location (`internal/world/movement_hook.go:13-17`). Logging and incrementing a metric do not reconcile that stale row, and Phase 5 explicitly ships no product projection. The plan’s assertion that the session state is eventually re-derived on a later authoritative read is not backed by a cited code path.

### Suggestions

- Keep command success after commit, but add a bounded durable retry/reconciliation record for the session update, or add a read-path repair proven by a test.

- If remediation is deliberately deferred, remove the claim of eventual reconciliation and document the actual consequence: session-derived routing may remain stale until reconnect or another explicit repair mechanism.

- Add a test proving the chosen recovery event, not merely that the command returns success and logs the hook error.

### Risk Assessment

**MEDIUM**. World state and feed correctness are preserved, but session-derived routing can diverge indefinitely.

---

## 05-09–05-12 — Taxonomy, rollout, census, genesis, and invariants

### Summary

The taxonomy/writer-boundary design is much stronger than earlier rounds, especially the import graph and numeric invariant IDs. Two boundary gaps remain: future SQL migrations are outside the fence, and epoch reset does not explicitly reset or serialize the position transition.

### Strengths

- The source-scanning SQL fence is the right mechanism; depguard cannot inspect SQL literals.

- Numeric invariant IDs match the existing parser contract, avoiding symbolic IDs that would never bind.

- Separating taxonomy consistency from writer completeness is honest. The census is paired with reader views, SQL scanning, import restrictions, and dedicated genesis/reaping services.

- The genesis checkpoint key includes game, epoch, aggregate type, and aggregate ID, which remains durable after outbox pruning and supports legitimate replay after an epoch change.

### Concerns

- **MEDIUM — INV-WORLD-4 claims coverage over migrations/backfills that the fence does not enforce.** The planned AST/token fence scans production Go only. Existing migrations already contain world mutations (`internal/store/migrations/000001_baseline.up.sql:389`, `:393`), demonstrating that SQL migrations are a real writer channel. A future migration can update a world table without emitting an envelope or advancing the epoch and still pass the proposed Go-source fence.

- **MEDIUM — Epoch reset semantics are incomplete.** Plan 05-11 says `AdvanceEpoch` updates `world_feed_counter.epoch`, while the schema also stores `next_position`. It does not explicitly require the same locked update to reset `next_position`, reject active unpublished rows, or coordinate relay state. An epoch increment with an inherited position is not the promised “restart cleanly,” and an epoch change while old rows remain can make relay ordering ambiguous.

### Suggestions

- Extend the SQL fence to migration files. Permit schema DDL, but reject world-table `INSERT`/`UPDATE`/`DELETE` unless the migration is explicitly paired with an epoch-reset marker or registered exception.

- Define epoch reset as one locked operation:

  - acquire the per-game counter row lock;
  - verify no unresolved/unpublished old-epoch row remains, or explicitly quarantine it;
  - increment epoch;
  - reset `next_position` to the defined origin;
  - record reset metadata;
  - notify/restart the relay.

- Add an integration test with a relay active during reset and old-epoch unpublished rows present.

### Risk Assessment

**MEDIUM**. The normal command boundary is strong, but the bound invariant currently claims more than its static enforcement proves.

---

## 05-15–05-16 — Character genesis and guest reaping

### Summary

05-15 correctly closes the three known creation bypasses and narrows cross-pool atomicity claims. However, 05-16 is not ready: its proposed reader does not exist on the named concrete repository, and its snapshot-then-delete ordering still allows a concurrent character creation to be removed by the player FK cascade without a tombstone.

### Strengths

- The current bypasses are real. `auth.CharacterRepository` exposes direct `Create` (`internal/auth/character_service.go:15-20`), and the service writes through it (`internal/auth/character_service.go:108-119`). Removing this method from production-facing narrow interfaces is a strong compile-time fence.

- The current guest cleanup hole is exactly as described: failure cleanup directly deletes the player (`internal/auth/guest_service.go:193-199`), and the guest reaper calls `GuestCleaner.DeleteGuestPlayer` (`internal/auth/guest_reaper.go:90-98`). `DeleteGuestPlayer` performs a direct player delete (`internal/auth/postgres/player_repo.go:326-344`), while the character FK cascades on player deletion (`internal/store/migrations/000002_player_is_guest.up.sql:9-13`).

- 05-15’s narrowed transaction claim is honest. Character, optional binding, and genesis can share the world transaction; player and role operations use other pool-based stores.

### Concerns

- **HIGH — 05-16 names a reader implementation that does not exist.** The plan says concrete `worldpostgres.CharacterRepository` supplies `ListByPlayer`, but its current production API has `Get`, `Create`, `Update`, `Delete`, `GetByLocation`, `UpdateLocation`, and ownership checks—no `ListByPlayer` (`internal/world/postgres/character_repo.go:29-135`). The auth adapter has `ListByPlayer`, but its query does not select or scan `Version` (`internal/bootstrap/setup/adapters.go:73-92`). Therefore it cannot supply the expected version required by guarded delete. Plan 05-16 does not list either repository file for correction.

- **HIGH — Snapshot-then-delete does not eliminate tombstone-less FK cascade under concurrency.** The plan lists characters, deletes each in separate transactions, and only afterward deletes the player on another connection. Character creation accepts an arbitrary player ID and performs no guest/reaping-state check (`internal/auth/character_service.go:64-69`, `:108-119`). A character can therefore be created after the reaper’s list—or after all tombstone transactions but before the player delete. The final `DELETE FROM players` then cascade-deletes that new character through the FK without a tombstone. INV-WORLD-4 remains false.

- **MEDIUM — Per-character transactions create partial reaps.** If character one commits and character two conflicts, the service returns with some characters tombstoned/deleted and the player retained. This is feed-safe for completed characters, but contradicts portions of the plan that describe a single character-reaping operation. The retry and compensation state should be explicit.

- **MEDIUM — A single create-then-reap test will not detect the race.** The planned regression test proves the serial path only. It cannot establish “genesis-without-tombstone is impossible” without an interleaving test against concurrent genesis creation.

### Suggestions

- Add a real version-bearing reader to `internal/world/postgres/character_repo.go`, or explicitly use and update the auth adapter to scan `version`. Include the changed file and tests in 05-16.

- Close the reaper/create race structurally. Suitable options include:

  1. move character enumeration, all guarded deletes, tombstone writes, and guarded guest-player deletion into one shared PostgreSQL transaction; or
  2. mark/lock the player as `reaping` first and make `CharacterGenesisService` reject creation for a reaping player, then enumerate/delete/tombstone and finally delete the player.

- Prefer one transaction for all of a player’s character tombstones, not one transaction per character. If batching is intentionally rejected, specify resumable state and exact partial-success semantics.

- Add a deterministic race test that pauses reaping after enumeration, attempts a genesis creation for the same guest, resumes the player delete, and asserts either creation is rejected or the new character receives a tombstone.

### Risk Assessment

**HIGH**. The new plan does not yet make tombstone-less guest deletion impossible, which is the sole reason 05-16 was added.

---

## 05-13 — Documentation correction

### Summary

This plan is appropriately scoped and preserves genuine client replay while correcting false world-state reconstruction claims.

### Strengths

- It distinguishes world-state replay from real client catch-up instead of replacing every occurrence mechanically.

- The guarded meta-test is scoped to false world/current-state claims, reducing false positives around legitimate event-history replay.

### Concerns

- **LOW — The test remains phrase-oriented.** A future semantically equivalent false claim may evade a narrow string check.

### Suggestions

- Guard the relevant document sections and a small set of semantic patterns, not only the exact old sentence.

- Include the ADR link in each architectural correction so future editors see the authoritative model decision.

### Risk Assessment

**LOW**.

---

# Overall Assessment

The plans have converged significantly: transaction ownership, package cycles, envelope finalization, relay leasing, stable skip identity, numeric invariants, and creation-path coverage are now well designed.

Three correctness blockers remain:

1. guest reaping can still race character creation and cascade-delete an un-tombstoned character;
2. location-delete cascade preselection permits a child-insert phantom;
3. durable consumer “effect + checkpoint” atomicity lacks a concrete shared-transaction API.

**NOT READY** — most importantly, 05-16 still cannot guarantee the locked D-06 outcome: a concurrent character can be created after enumeration and then disappear through `ON DELETE CASCADE` without a tombstone.

---

## OpenCode Review (openrouter/x-ai/grok-4.5)

Reviewing round-5 plan edges against live source — focusing on 05-16 reap/tombstone, D-07 removal, consumer checkpoint, cascade parity, and mutate/hook seams.
Verifying the round-5 edges against live call sites and cascade/order facts.
Digging into guest cleanup, character listing, Taskfile build targets, and remaining delete-path holes.
# Phase 5 Plans — Round 6 Cross-AI Review

Source-grounded against current tree (`internal/auth/**`, `internal/world/**`, migrations, `Taskfile.yaml`, eventbus config). Focus: round-5 deltas and residual implementability risks—not rehashing settled r1–r5 design locks.

---

## Cross-cutting (r5-shaped)

| Area | Verdict | Evidence |
|------|---------|----------|
| D-06 guest delete hole is real | Confirmed | `guest_reaper.go:90` → `GuestCleaner.DeleteGuestPlayer`; `player_repo.go:331-334` `DELETE FROM players … is_guest=true` **documents** FK cascade to characters; `000002_player_is_guest.up.sql:9-13` `ON DELETE CASCADE`. Soft path: `guest_service.go:195-199` `cleanupGuestPlayer` → `players.Delete` (`player_repo.go:271-273`)—same cascade, **no** character tombstone. |
| D-07 world scene write removal | Safe | `AddSceneParticipant` / `RemoveSceneParticipant` only at `service.go:703/724` + `scene_repo.go:27/56` + mocks/tests; prod plugins use **plugin** `SceneStore.AddParticipant` (`plugins/core-scenes/store.go:748`), not world. Reads (`ListParticipants`/`GetScenesFor`) still join `public.scene_participants`—table DROP deferral is correct. |
| Location exit cascade preselect | Sound | `000001_baseline.up.sql:115-116` both `from_location_id` and `to_location_id` → `locations` `ON DELETE CASCADE`; 05-02 preselect matches DB. |
| Two-outcome zero-row classifier | Sound | ID-only `Delete(ctx,id,expectedVersion)` carries no existence token; concurrent-delete ≡ absent after commit. |
| Durable consumer checkpoint | Necessary | JetStream `DupeWindow` default **30m** (`eventbus/config.go:27,159-160`); finite dedup is real. Schema PK `(consumer_name,event_id)` + apply+record in one tx is the right shape. |
| `task build:all` | Correct fix | `task build` builds main binary (`Taskfile.yaml` ~290/`MAIN_PKG`); whole-tree needs `go build ./...` under a Task target (CLAUDE.md). |
| Movement hook post-commit degradation | Correct disposition | Hook is session-store I/O (`sub_grpc.go:847-848` → `sessions.UpdateLocationOnMove`); character row already committed `service.go:802-819` with `move_succeeded:true`. Failing hook as command failure was already the dual-write smell; operational degradation matches pool boundary. |

---

## 05-16 `CharacterReapingService` (NEW — highest residual risk)

### Summary
D-06 is correctly diagnosed and mostly well designed (order tombstone-then-player; reuse character delete kind; GuestCleaner seam). Two **implementation holes** remain: (1) the only production `ListByPlayer` path does not live on `worldpostgres.CharacterRepository` and will **not** populate `Version`; (2) reaping bypasses the property cascade that `world.Service.DeleteCharacter` always runs.

### Strengths
- Matches real call graph: reaper → `GuestCleaner` (`guest_reaper.go:20-21,90`); cleanup → `players.Delete` (`guest_service.go:195-199`) must both be rewired.
- Ordering vs FK is right: delete characters first (with envelopes), then player so `characters.player_id ON DELETE CASCADE` is a no-op.
- Kind reuse (same tombstone as `DeleteCharacter`) keeps taxonomy modest.
- Fail-closed deps + concurrent-edit retry posture is appropriate for a background reaper.

### Concerns
1. **[HIGH] Reaper’s character list has no versioned read on the claimed type**  
   - Plan injects “concrete `worldpostgres.CharacterRepository`” as reader with `ListByPlayer`.  
   - `internal/world/postgres/character_repo.go` has **no** `ListByPlayer` (methods: Get/Create/Update/Delete/GetByLocation/UpdateLocation/IsOwnedByPlayer/GetNamesByIDs/ListAll only).  
   - Production `ListByPlayer` is `CharRepoAdapter.ListByPlayer` (`adapters.go:74-118`):  
     `SELECT id, player_id, name, description, location_id, created_at …` — **no `version` column**, never sets `Character.Version`.  
   - After 05-03 CAS `Delete(…, expectedVersion)`, `char.Version == 0` against DB `version DEFAULT 1` → **every reap delete is permanent conflict/not-found**.  
   - 05-03 `files_modified` does **not** include `adapters.go`; 05-16 assumes “05-03 scans populate Version” for a method that does not exist there.

2. **[HIGH] Property cascade parity with `DeleteCharacter` missing**  
   - Service delete: `propertyRepo.DeleteByParent("character", id)` **then** `characterRepo.Delete` in one tx (`service.go:602-657`).  
   - `entity_properties` has **no** FK to `characters` (`000001_baseline.up.sql:350-364`)—delete character leaves orphan property rows.  
   - 05-16 only calls `CharacterWriter.Delete`. Guests *may* have zero props, but feed integrity ≠ DB cleanliness; any reaped character with properties diverges from operator `DeleteCharacter` semantics and can break subsequent parent-scoped property APIs.

3. **[MEDIUM] Per-character vs single-tx ambiguity**  
   - Text says both “same transaction as the delete” and “tx(es)”. Must specify **one InTransaction per character**, then player delete after all succeed—otherwise one concurrent-edit rolls back already-proven tombstones.

4. **[MEDIUM] `cleanupGuestPlayer` is `Delete`, not `DeleteGuestPlayer`**  
   - Covered by rewiring, but player deleter handoff must still use `is_guest=true` guarded delete for the reaper path; cleanup’s current unguarded `Delete` is fine only while input is always a guest—document that.

### Suggestions
- Make `ListByPlayer` (version-scanning) a **canonical** method on `world/postgres.CharacterRepository` (or an injected reader that is *that* repo), update `CharRepoAdapter` to delegate + scan `version`, and put that in **05-03 and/or 05-16 tasks/files_modified** with a RED test: listed version equals stored version → reap CAS succeeds.
- In reaping’s per-character tx: `DeleteByParent("character", id)` via a narrow property deleter + character `@expectedVersion` Delete + `WriteIntent` (mirror service cascade).
- State explicit algorithm: list → for each char { InTx{ props + Delete + envelope } } → then `DeleteGuestPlayer`.

### Risk Assessment
**HIGH** until list+version and property cascade are in the plan.

---

## 05-14 foundation + D-07 + `build:all`

### Summary
Still the load-bearing wave-1 plan. D-07 deletion of vestigial scene writes is safe. Whole-tree gate via `task build:all` is the right CLAUDE.md fix. Residual risk is blast-radius etiquette (gate > hand list)—already framed correctly.

### Strengths
- Re-entrant `InTransaction` / `withTx` targets real bugs: `exit_repo.go:60,178` and `object_repo.go:172` own `pool.Begin`.
- Property `r.pool.Exec` leak (`property_repo.go` create/update) stays in scope.
- D-07 grep gate + keep reads/table is coherent with live callers.
- Caller completeness proof is mechanical whole-tree compile—correct after r2–r4 miss streaks.

### Concerns
1. **[LOW] Vestigial read surface** — `ListSceneParticipants` remains prod code with no non-world external callers grepped; not phase-breaking, but leaves dead API surface until #4815.
2. **[LOW] `CharRepoAdapter` still does raw `characters` SELECT outside `internal/world/postgres` after Create removal** — fence is mutation-only; OK. Note interaction with 05-16.

### Suggestions
- In D-07 task, also note `ListSceneParticipants` may have zero prod demand (optional follow-up).
- Explicitly list `adapters.go` most-likely churn under whole-tree gate when writer signatures change.

### Risk Assessment
**LOW–MEDIUM** (execution risk mostly time/volume of breakage, not design).

---

## 05-01 … 05-04 (version guard)

### Summary
Still solid; rectangle of migration + struct + CAS + RMW + M12 flip matches the ADR.

### Concerns
1. **[MEDIUM] Adapter / auth scanners outside 05-02/05-03**  
   Beyond reaping: `CharRepoAdapter.ListByPlayer` / ListAll, harness adapters, etc. will return `Version=0` until touched. Any path that RMW/deletes using those lists breaks. Census of **character** readers that must learn `version` is incomplete if limited to `internal/world/postgres/*_repo.go`.
2. **[LOW] M12 quarantine still not the MODEL-03 must-ship gate alone** — fine; unit/int CAS tests carry weight.

### Risk Assessment
**MEDIUM** until version-scan inventory includes `CharRepoAdapter` (+ harness auth adapter).

---

## 05-05 outbox schema

### Summary
Schema package (outbox, counter epoch, genesis checkpoint, consumer checkpoint, skip_marker_event_id, lease_generation) is complete for r3–r5 mechanisms.

### Strengths
- WriteIntent owns finalize/position (no envelope-before-delta).
- Always-run state+envelope atomicity tests for INV-WORLD-1.
- Late counter + `WORLD_FEED_LOCK_TIMEOUT` is testable, not prose.

### Concerns
1. **[LOW] Watermark table shape** — ensure watermark updates don’t invent a second PK shape that races multi-game (plan has `game_id` on watermark—good).
2. **[LOW] Four tables in one migration** — fine; down-order reverse explicit—good.

### Risk Assessment
**LOW**

---

## 05-06 mutate + MoveCharacter + emit delete

### Summary
Write-closure mutate + write ownership fix + post-commit hook degradation closes the last temporal/operation-identity holes. Emit-path delete packaged with harness rewrite remains load-bearing.

### Strengths
- Hook pouch at `service.go:807-819` truly cannot enroll in world tx (session pool)—classification is forced by architecture.
- Deleting `move_succeeded=true` failure path is the right honesty fix for envelope semantics (“failed commands emit nothing”).

### Concerns
1. **[MEDIUM] Session location lag is accepted but not regression-tested** — add one non-quarantined test: after move + forced hook failure, character.location is updated, session location may lag, command err is nil. Prevents re-introducing fail-after-commit.
2. **[LOW] Examine consumer audit** — still must run; residual product half-feature if something greps examine subjects.

### Risk Assessment
**LOW–MEDIUM**

---

## 05-07 relay + lease + skip + consumer

### Summary
Lease abstraction, durable generation, stable skip id, at-least-once honesty, full import-graph allowlist: strong.

### Strengths
- Session advisory lock on **dedicated** conn is required (pool-shaped methods cannot prove binding)—plan correctly forces Lease methods onto that conn.
- Stable `skip_marker_event_id` before publish is the only way to keep INV-WORLD-3 under crash-after-PubAck.
- Wire adapter: payload carries epoch/position; leave global `App-Schema-Version` alone (`publisher.go:304`, reserved header set)—matches code.

### Concerns
1. **[MEDIUM] Dedicated-conn lifecycle** under long LISTEN + advisory lock: plan needs `pool.Acquire` / `Conn()` hygiene, cancel-on-loss, and a SEAM test for “conn drops → AcquireLease new generation”. No existing world pattern taught session advisory locks (player sessions use **xact** locks in `player_session_store.go:79`).
2. **[LOW] Reference consumer package path** must stay off crypto/abac greps—already tasked.

### Risk Assessment
**MEDIUM** (ops/implementation complexity, not conceptual defect).

---

## 05-08 resilience matrix

### Summary
Correct firepower for M2 end-to-end and lease handoff; at-least-once assertions are correctly framed.

### Concerns
- **[LOW]** ~quarantine-only coverage → keep 05-07 seam tests mandatory.

### Risk Assessment
**LOW**

---

## 05-09 taxonomy + character preferences fold + SQL fence

### Summary
D-05 fencing + real character_settings fold (`character_settings_repo.go:80` raw `UPDATE characters SET preferences`) is still required and correctly sequenced before green fence assertion.

### Concerns
1. **[MEDIUM] RMW preferences race** — service internal read-version then CAS: two concurrent SetPreferences → one WORLD_CONCURRENT_EDIT; settings callers may not handle it (D-02 ok, but integration test needed).
2. **[LOW] SQL fence comment-text discipline** — already warned; keep executor guardrails.

### Risk Assessment
**LOW–MEDIUM**

---

## 05-10 / 05-11 rollout + genesis + census

### Summary
Data-first for census remains justified. Genesis via consumer-owned `GenesisStore` + CLI resolves r4 A3. Census honesty (bijection ≠ completeness) is right.

### Concerns
1. **[MEDIUM] Character reaping as second out-of-Service producer of the tombstone kind** — census multi-producer rule is stated; implementation must not force 1:1 kind↔command bijection that fails when both `DeleteCharacter` and reaping emit the same kind. Plan text allows multi-producer on the out-of-world set—**tests must match** that wording.
2. **[LOW] Gen era + CharRepo listener Version gap** — genesis snapshot path will scan full world tables; ensure those SELECTs include `version` for any later CAS deletes of the same process (genesis itself is create-only).

### Risk Assessment
**MEDIUM** (census multi-producer + adapter version coupling).

---

## 05-12 invariants

### Summary
Numeric IDs + always-run ATOMIC-FEED binding + same-position skip in FEED-ORDER still correct.

### Concerns
- **[MEDIUM] INV-WORLD-4 asserted_by set** should list a genuine **guest reaping tombstone regression** (`guest_reaper_tombstone_test.go` from 05-16) after 05-16 lands—otherwise D-06 can regrow while the bound tests still pass (writer-boundary false-green for guest deletion).

### Risk Assessment
**MEDIUM** until 05-16 test is among WRITER-BOUNDARY asserted_by.

---

## 05-13 docs

### Summary
Still fine. Preserve reconnect “event replay”; rewrite only world-state reconstruction claims.

### Risk Assessment
**LOW**

---

## 05-15 character genesis

### Summary
Still the right creation fence. B4 narrow atomicity + ordered player/role remains honest vs `player_repo`/`role_store` own pools.

### Concerns
1. **[MEDIUM] After `Create` removal, reaper freeze** — depends on independent List/Delete path still assembling (see 05-16). Not reopening 05-15 scope, but phase readiness is joint.
2. **[LOW] Guest CreateGuest currently claims “atomic player+character+binding” in comments (`guest_service.go:134-135`) while `players.Create` uses `r.pool`**—plan correctly rewrites; keep test that player *intentionally* commits first.

### Risk Assessment
**LOW–MEDIUM** (sound; coordination with 05-16 Version list).

---

# Round-6 net-new blockers (not settled restatements)

| ID | Severity | Issue |
|----|----------|--------|
| R6-1 | **HIGH** | 05-16 / character list: `ListByPlayer` is only on `CharRepoAdapter` (`adapters.go:74-118`), without `version`; world postgres repo has no `ListByPlayer`. Versioned reaper Delete is unimplementable as written → permanent reap failure after 05-03. |
| R6-2 | **HIGH** | 05-16 omits `entity_properties` `DeleteByParent` that `DeleteCharacter` always does (`service.go:617-622`); orphans possible; cascade parity with world delete broken. |
| R6-3 | **MEDIUM** | Version-scan inventory incomplete: any auth/harness adapter listing characters for writes must scan `version` (not only `*_repo.go` Get/List). |
| R6-4 | **MEDIUM** | INV-WORLD-4 binding should include D-06 regression test after 05-16 or guest FK-cascade can return without failing bound tests. |
| R6-5 | **MEDIUM** | 05-07 session-level advisory lease needs explicit `pool.Acquire`/drop/re-acquire protocol (no in-tree session-lock precedent beyond xact locks). |
| R6-6 | **LOW** | 05-06: add regression that hook failure does not flip command fail after envelope commit. |

---

# Overall verdict

**NOT READY**

**Single most important reason:** plan **05-16** routes tombstone-emitting deletes through a CAS that needs a real **stored version**, but the claimed reader (`worldpostgres.CharacterRepository.ListByPlayer`) does not exist, and the actual list path (`CharRepoAdapter.ListByPlayer`) never selects/scans `version`—after MODEL-03, guest reaping would systematically fail or regress to ungarded deletes, so D-06 would not close.

**To flip READY:** patch 05-03/05-16 (and adapters) so a version-populated character list is authoritative; include property cascade in the reaping tx; bind INV-WORLD-4 to the tombstone regression; keep the rest of the r5 design.

---

## Antigravity Review (agy)

> NOTE: 11-line GREEN with no HIGH findings — a shallow false-green (documented pattern, rounds 1/3/6). Contradicted by two source-grounded reviewers. Weight out of the plan-level verdict; incidental notes below are valid.

I have completed a thorough, source-grounded architectural review of the HoloMUSH Phase 5 implementation plans (`05-01-PLAN.md` through `05-16-PLAN.md`).

The findings and micro-architectural observations have been compiled into a markdown artifact:
* [phase_5_plan_review.md](file:///Users/sean/.gemini/antigravity-cli/brain/07c174c3-705b-4435-9125-fb785ff48f6c/phase_5_plan_review.md)

### Key Review Outcomes & Recommendations

1. **Overall Verdict**: The plans are highly cohesive and successfully resolve the world-model integrity issues, outbox atomicity gaps, and import cycle constraints without introducing regression risks. The dependency ordering across waves is structurally sound.
2. **Orphan Guest Players**: In [GuestService](file:///Users/sean/Code/github.com/holomush/holomush/internal/auth/guest_service.go), player creation is committed before the genesis transaction. While the plans accept the resulting orphan-player compensation gap, we recommend adding a best-effort call to `cleanupGuestPlayer` upon transaction failure to minimize immediate DB bloat.
3. **Settings Store CAS Integration**: To support version-guarding for the folded-in preferences write, we note that the settings repository must return the character's `version` on load so it can be passed as `expectedVersion` to [Service.UpdateCharacterPreferences](file:///Users/sean/Code/github.com/holomush/holomush/internal/world/service.go) during write-back.
4. **Deadlock Safety**: The zero-row CAS write classifier correctly reuses the caller's transaction connection from `execerFromCtx` rather than borrowing from the pool, preventing deadlocks on pool-size-1 configurations.

---

## Consensus Summary

**Round 6 — NOT CONVERGED.** Both source-grounded reviewers returned **NOT READY** with `file:line`-cited HIGH blockers; the third (Antigravity) returned an 11-line GREEN with no HIGH findings.

### Reviewer reliability note (weighting)

- **Codex (gpt-5.6-sol)** and **OpenCode / openrouter x-ai/grok-4.5** are source-grounded — they traced the concrete adapters, migrations, and call graph and cite `path:line`. Both independently reach **NOT READY** and **agree on the load-bearing blocker** (05-16 version read). Weight these.
- **Antigravity (`agy`)** returned ~11 lines: *"highly cohesive … without introducing regression risks"* — an implicit READY with **no** HIGH findings, directly contradicted by two independent source-grounded reviewers who found real HIGH blockers. This is the documented Antigravity false-green pattern (rounds 1/3/6). Treat its overall verdict as non-authoritative. (Its incidental notes are valid and echo the others: settings store must return `version` for the folded preferences CAS; best-effort `cleanupGuestPlayer` on genesis-tx failure to bound orphan-player bloat.)

### Agreed Strengths (2+ reviewers)

Round-5 architecture has largely converged and should be KEPT: two-outcome CAS zero-row classifier is sound; the re-entrant transaction foundation (05-14) targets real bugs (`exit_repo.go:60,178`, `object_repo.go:172`); envelope ownership is coherent (intent from command, delta from repo, epoch/position from PG writer); the wire adapter matches the eventbus (`publisher.go:184-190,280-306`); **D-07** world scene-participant write removal is safe (prod core-scenes uses the *plugin* `SceneStore.AddParticipant`, not the world service); `task build:all` is the correct CLAUDE.md fix; the durable consumer checkpoint is necessary (JetStream `DupeWindow` ~30m is finite); movement-hook post-commit degradation is the correct disposition.

### Agreed Concerns — BLOCKERS (2+ reviewers)

- **[HIGH] 05-16 versioned reaper `Delete` is unimplementable as written (Codex + grok agree).** The reaper needs each character's stored `version` to issue the MODEL-03 CAS `Delete(ctx,id,expectedVersion)`, but (a) `worldpostgres.CharacterRepository` has **no** `ListByPlayer` (`character_repo.go:29-135`), and (b) the actual production list path `CharRepoAdapter.ListByPlayer` (`internal/bootstrap/setup/adapters.go:74-118`) never selects/scans `version`. After 05-03's CAS delete, `char.Version==0` vs DB `version DEFAULT 1` → **every reap delete is a permanent conflict/not-found → guest reaping systematically fails and D-06 does NOT close.** Neither 05-03 nor 05-16 lists `adapters.go` in `files_modified`. **Fix:** add a version-scanning `ListByPlayer` on the world repo (or update the adapter to scan `version`), add `adapters.go` to 05-03/05-16, and add a RED test: listed version == stored version → reap CAS succeeds.
- **[HIGH] INV-WORLD-4 must bind the D-06 guest-reaping tombstone regression (grok R6-4; Codex "serial test won't detect the race").** Otherwise the guest FK-cascade hole can regrow while the bound writer-boundary tests still pass (a writer-boundary false-green — the exact failure mode 05-16 exists to prevent).

### Divergent / single-reviewer HIGH concerns (still fix)

- **[HIGH — Codex only] 05-16 snapshot-then-delete TOCTOU.** Character create accepts an arbitrary player id with no reaping-state check (`character_service.go:64-69,108-119`); a character created after enumeration (or after the per-character tombstone txns but before `DELETE FROM players`) is cascade-deleted with **no tombstone** → D-06 reopens under concurrency, INV-WORLD-4 false. **Fix:** one shared PG tx for enumerate + all tombstones + guarded player delete, OR mark/lock the player `reaping` first and make `CharacterGenesisService` reject creation for a reaping player. grok did not raise the concurrency race (it focused on the version read + property cascade), so this is a real, additional gap.
- **[HIGH — grok only] 05-16 omits the `entity_properties` property cascade.** `DeleteCharacter` runs `propertyRepo.DeleteByParent("character", id)` before the char delete (`service.go:617-622`); `entity_properties` has no FK to characters → reaping leaves orphan property rows and diverges from operator-delete semantics. **Fix:** mirror the cascade in the reaping per-character tx.
- **[HIGH — Codex only] 05-02 location-cascade child-insert phantom.** The `SELECT … FOR UPDATE` preselect locks only *existing* exit rows; a concurrent exit insert after preselect and before the location delete is cascade-deleted without appearing in `MutationDelta` → INV-WORLD-2 bindable only by a non-adversarial test. **Fix:** lock the parent location row `FOR UPDATE` FIRST (conflicts with FK key-share acquisition, closing the child-insert phantom), then preselect + delete; add the exact interleave integration test.
- **[HIGH — Codex only] 05-07 "effect + checkpoint in one transaction" has no concrete shared-transaction API.** `ConsumerCheckpointStore` describes only checkpoint ops; a generic consumer can't guarantee an arbitrary effect enrolls in the same tx as the checkpoint SQL (`execerFromCtx` reads a private ctx value the transactor installs). **Fix:** an injected transactor / `ApplyOnce(ctx, consumer, envelope, effect func(txCtx) error) (applied bool, err error)`. Related: the `(consumer_name,event_id)` table conflates per-event receipts with the per-consumer watermark (Codex MEDIUM — split into receipts + watermark, advance watermark only on monotonic `(epoch,position)`).

### MEDIUMs to fold

- **Version-scan inventory incomplete** beyond `internal/world/postgres/*_repo.go` — any auth/harness adapter that lists characters for a subsequent write/delete must scan `version` (both reviewers; grok R6-3).
- **INV-WORLD-4 SQL-migration blind spot:** the fence scans Go only; existing migrations already mutate world tables (`000001_baseline.up.sql:389,393`), so a future migration can write a world table without an envelope and still pass the Go fence (Codex). Extend the fence to migration files or register explicit exceptions.
- **Epoch reset incomplete:** `AdvanceEpoch` updates `epoch` but not `next_position`, and doesn't reject/quarantine unpublished old-epoch rows or coordinate the relay (Codex). Define epoch reset as one locked operation.
- **Movement-hook (05-06):** no cited reconciliation path for the stale session-location row — either add a bounded durable retry / read-path repair proven by a test, or drop the "eventually re-derived" claim and document the actual consequence (Codex); add a non-quarantined regression that a hook failure does NOT flip the command to failure after envelope commit (grok R6-6).
- **05-09 preferences RMW race:** two concurrent `SetPreferences` → one `WORLD_CONCURRENT_EDIT`; settings callers may not handle it — needs an integration test (grok).
- **05-07 session advisory-lease lifecycle (grok R6-5):** no in-tree session-lock precedent beyond xact locks (`player_session_store.go:79`); needs explicit `pool.Acquire`/drop/re-acquire + cancel-on-loss + a seam test.

### Net verdict

**NOT READY / NOT CONVERGED.** 2/2 source-grounded reviewers independently NOT READY; the lone GREEN is a shallow false-green. The round-5 architecture otherwise held — the failures are concentrated in the **new deletion path (05-16, the D-06 addition)** plus two pre-existing parity/atomicity gaps that deeper source-grounding surfaced (05-02 cascade phantom, 05-07 consumer atomicity). The load-bearing blocker: 05-16's versioned reaper delete is unimplementable against the real character-list adapter (which never scans `version`), and — even once implementable — the snapshot-then-delete ordering + missing property cascade leave the D-06 tombstone guarantee incomplete under concurrency.

**Loop status:** severity is concentrating, not thrashing — round 5 closed the architecture; round 6 narrowed the open surface to the 05-16 deletion path + 2 adjacent gaps, all with concrete, bounded fixes. Recommended next step: incorporate via `/gsd-plan-phase 5 --reviews` (round-7 incorporation) — do NOT execute yet. The 05-16 version-read blocker (R6-1) and the TOCTOU race are the must-fixes before execution.
