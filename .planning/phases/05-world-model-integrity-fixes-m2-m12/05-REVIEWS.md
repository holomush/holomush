---
phase: 5
review_round: 5
reviewers: [codex, antigravity]
antigravity_status: "succeeded (GREEN / approved) — but false-green per loop history; not weighted over Codex"
codex_verdict: "HIGH / NOT READY — one more revision (new lifecycle/cascade/consumer-durability edges)"
reviewed_at: 2026-07-12T15:00:37Z
plans_reviewed: [05-01-PLAN.md, 05-02-PLAN.md, 05-03-PLAN.md, 05-04-PLAN.md, 05-05-PLAN.md, 05-06-PLAN.md, 05-07-PLAN.md, 05-08-PLAN.md, 05-09-PLAN.md, 05-10-PLAN.md, 05-11-PLAN.md, 05-12-PLAN.md, 05-13-PLAN.md, 05-14-PLAN.md, 05-15-PLAN.md]
prior_rounds: "r1 @4238fc876, r2 @81440289a, r3 @937b03910, r4 @ce8ac5d5e (all incorporated; round-4 fixes committed @1eab68ad9). Round-5 is a fresh review of the round-4-incorporated set."
verdict: "NOT converged. Round-4 fixes confirmed landed by both reviewers, but Codex round-5 surfaced a new layer of real edges (character-lifecycle deletion, cascade-delta parity, consumer durability, mutate-seam operation identity, wire adapter, a D-05 outbox self-contradiction). Weight Codex; Antigravity GREEN is a false-green."
---

# Cross-AI Plan Review — Phase 5 (ROUND 5, round-4-incorporated 15-plan set)

Fifth-round review, run AFTER the round-4 incorporation (@1eab68ad9). Both reviewers produced output this round (Antigravity recovered from its round-4 failure). Reviewers were given the current plans + CONTEXT (incl. D-05) + RESEARCH + REQUIREMENTS and asked for a fresh source-grounded review — deliberately NOT primed with prior-round findings, so independent convergence would be a real signal. It was not: the two reviewers diverge sharply and the orchestrator independently confirmed four Codex findings against the plan text (see Consensus).

---

## Codex Review (round 5)

# Cross-AI Plan Review

## Summary

The phase has a strong architectural core: versioned CAS writes, transaction enrollment, delta-derived envelopes, a locked feed counter, leased relay, lifecycle wiring, and invariant bindings are all materially better specified than a typical implementation plan. The plans also correctly discovered several brownfield hazards, including repository-owned transactions and character creation outside `world.Service`.

However, I would not approve execution yet. Several cross-plan contradictions still make the claimed invariants unachievable:

- guest cleanup/reaping can delete characters through an FK cascade without a tombstone;
- scene-participant emission contradicts locked decision D-05;
- the proposed generic `mutate(entity, version, intent)` contract cannot identify or execute the actual operation;
- durable consumer idempotency beyond JetStream’s finite deduplication window has no persistence design;
- `MoveCharacter` still contains a post-commit failure path;
- location deletion deltas omit exits deleted by database cascade.

Overall assessment: **HIGH risk / NOT READY** until these blockers are resolved in the plans.

## Strengths

- The transaction-enrollment prerequisite is correctly grounded. `Transactor.InTransaction` currently always begins a new transaction, while only context-aware repository methods reuse it ([transactor.go:27](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/transactor.go:27), [helpers.go:31](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/helpers.go:31)). Plan 05-14’s reentrant transaction work is necessary.

- The plans correctly identify repository-owned transactions as an atomicity blocker. Bidirectional exit creation currently begins and commits its own transaction ([exit_repo.go:59](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/exit_repo.go:59)), making the planned refactor essential.

- Adding versions to all four entities is well aligned with the current code: repository updates are presently unguarded and collapse zero affected rows into not-found ([location_repo.go:66](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/location_repo.go:66), [character_repo.go:62](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/character_repo.go:62)).

- The character-creation census is source-grounded. Registered creation currently persists directly through `auth.CharacterRepository.Create` ([character_service.go:108](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/character_service.go:108)), while guest creation holds a separate create-capable interface ([guest_service.go:32](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/guest_service.go:32)). Centralizing those paths in 05-15 is the right direction.

- Plan 05-15 correctly narrows its transaction claim. Player writes use their own pool ([player_repo.go:31](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/postgres/player_repo.go:31)), role writes likewise use a separate pool ([role_store.go:57](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/role_store.go:57)), while bindings explicitly recognize the world transaction context ([binding_repo.go:26](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/binding_repo.go:26)). The revised character + optional binding + envelope atomic unit is honest.

- The relay’s at-least-once framing is correct. The existing JetStream stream has a finite duplicate window ([subsystem.go:274](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/subsystem.go:274)), so avoiding an impossible “duplicates can never occur” claim is important.

- The distinction between the two `scene_participants` tables is real. The world repository writes the unqualified public table ([scene_repo.go:26](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/scene_repo.go:26)), while core-scenes receives a connection whose search path is `plugin_core_scenes` ([store.go:87](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/plugins/core-scenes/store.go:87)). Excluding the plugin tree from the public-world SQL fence is justified.

## Concerns

### HIGH — Guest deletion bypasses the outbox and invalidates INV-WORLD-4

The plans close character creation paths but miss an existing production character deletion path:

- deleting a guest player cascades to its characters by schema design ([000002_player_is_guest.up.sql:9](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/migrations/000002_player_is_guest.up.sql:9));
- the reaper invokes `DeleteGuestPlayer` directly ([guest_reaper.go:68](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/guest_reaper.go:68));
- that method deletes only from `players`, relying on FK cascades for characters ([player_repo.go:326](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/postgres/player_repo.go:326)).

This produces a character deletion without a character tombstone, bypassing the SQL fence because the Go source contains `DELETE FROM players`, not `DELETE FROM characters`.

The same problem exists in `GuestService` cleanup after token or session failure: cleanup deletes the already-committed player after character genesis may have been emitted ([guest_service.go:165](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/guest_service.go:165), [guest_service.go:193](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/guest_service.go:193)).

This defeats the census, writer-boundary invariant, delta parity, and feed completeness.

### HIGH — Plans 05-09 and 05-11 violate D-05

D-05 explicitly excludes `scene_participants` from the guarded/outbox surface. Nevertheless:

- 05-09 declares scene-participant envelope kinds;
- 05-11 routes `AddSceneParticipant` and `RemoveSceneParticipant` through the outbox;
- 05-11 describes these writes as part of the complete rollout.

The source confirms these methods target the vestigial public table ([scene_repo.go:40](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/scene_repo.go:40)), and the production caller census finds only tests outside `world.Service`. The plugin uses its separate schema ([plugin migration:11](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.up.sql:11)).

The plan must choose one position consistently. Under locked D-05, scene participant methods should be excluded from the taxonomy, outbox rollout, and census, with issue #4815 retaining ownership.

### HIGH — The proposed `mutate` signature does not identify an operation

The canonical seam is repeatedly stated as:

```go
mutate(ctx, entity, expectedVersion, intent)
```

That contains no operation or callback. The current repository contracts expose separate create, update, delete, move, and participant methods ([repository.go:21](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go:21), [repository.go:93](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go:93), [repository.go:121](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go:121)). An entity plus version cannot distinguish:

- create from update;
- update from delete;
- object movement from ordinary object update;
- multi-repository cascade deletion;
- character preference updates;
- participant changes.

Dispatching from the envelope’s string `kind` would turn the claimed compile-time seam into runtime string dispatch. The plans never assign this responsibility precisely.

### HIGH — Durable idempotency is missing from the reference consumer

The plans repeatedly claim consumer deduplication beyond the JetStream duplicate window, including crash/retry tests. But 05-07 adds only `consumer.go` and tests; it adds no consumer checkpoint/idempotency table or injected durable store.

The current publisher documents that the deduplication horizon is finite and that expiry changes retry safety ([publisher.go:64](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/publisher.go:64), [publisher.go:109](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/publisher.go:109)). The stream’s duplicate window is explicitly configured rather than permanent ([subsystem.go:281](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/subsystem.go:281)).

An in-memory ULID set does not survive consumer restart and cannot establish the claimed exactly-once visible effect after a retry outside the duplicate window.

### HIGH — `MoveCharacter` retains a post-commit failure contradiction

`MoveCharacter` currently:

1. updates the character row;
2. invokes a movement hook;
3. may return an error after the row already committed.

This is explicit in the source ([service.go:802](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/service.go:802), [service.go:807](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/service.go:807)). The hook contract says an error fails the move ([movement_hook.go:13](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/movement_hook.go:13)).

After 05-06, the mutation and envelope will commit together before this hook unless the plan changes the ordering. A hook error would therefore produce:

- a command reported as failed;
- committed world state;
- a committed world envelope.

That conflicts with the plans’ rule that failed commands emit nothing. None of 05-06, 05-08, or 05-10 specifies the movement-hook disposition.

### MEDIUM — Location delete cannot produce its claimed cascade delta

Deleting a location cascades deletion of exits through both exit foreign keys ([000001_baseline.up.sql:113](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/store/migrations/000001_baseline.up.sql:113)). Yet:

- 05-02 specifies the location delete delta as only the primary location tombstone;
- 05-10 later claims the location delete manifest includes cascaded children;
- 05-10 modifies only service/payload files, not the location repository.

The service cannot reconstruct the IDs and versions of database-cascaded exits after deletion. That makes the planned INV-WORLD-2 binding fail for location deletion unless the repository preselects and reports those exits or the invariant explicitly excludes DB cascades.

### MEDIUM — Concurrent-delete classification is overstated

The plans require a locked prior read and claim it distinguishes stale version, concurrent deletion, and never-existent rows.

A prior `SELECT ... FOR UPDATE` serializes the row against a competing delete. If the competing delete committed first, the read sees no row—the same observation as a never-existent ID. If the current transaction locks first, the competing delete waits and is not “concurrent deletion” for this operation.

The existing API accepts only ID for deletes ([repository.go:32](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go:32)), so there is no separate caller-supplied existence token. The plan should either define “concurrent delete” as not-found or add the evidence needed to distinguish it.

### MEDIUM — World envelope wire mapping is under-specified

The proposed `wmodel.Envelope` has epoch, feed position, affected aggregates, and per-kind schema version. The existing `eventbus.Event` wire format has only the standard event metadata plus opaque payload ([types.go:136](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/types.go:136)). Its publisher always stamps the global `App-Schema-Version` constant ([publisher.go:303](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/publisher.go:303)), and callers are forbidden from overriding that reserved header ([types.go:162](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/eventbus/types.go:162)).

The plans need an explicit adapter defining:

- whether `wmodel.Envelope` is serialized entirely into `eventbus.Event.Payload`;
- how per-kind schema version relates to the global header;
- how consumers read `epoch` and `feed_position`;
- how skip markers use exactly the same wire shape.

### MEDIUM — Plan 05-14’s verification violates repository commands policy

Plan 05-14 requires raw `go build ./...`, but project instructions explicitly prohibit direct Go build/test commands ([CLAUDE.md:200](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/CLAUDE.md:200)). The existing `task test` and `task test:int` already compile `./...` by default ([Taskfile.yaml:85](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/Taskfile.yaml:85), [Taskfile.yaml:165](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/Taskfile.yaml:165)).

If a separate all-package non-test build is genuinely required, add a Taskfile target rather than embedding a forbidden command in the execution plan.

### MEDIUM — One phase PR is likely too large for reliable review

This phase combines:

- two migrations;
- repository-interface redesign;
- transaction semantics;
- every world write;
- new relay and operator commands;
- lifecycle changes;
- auth and bootstrap changes;
- new meta-tests and invariants;
- documentation corrections.

The plan’s whole-tree gates reduce integration risk, but one PR makes semantic review and regression isolation difficult. Given the known review history and continuing cross-plan contradictions, the D-04 one-PR constraint materially increases landing risk.

## Plan-by-Plan Assessment

| Plan | Assessment | Principal issue |
|---|---|---|
| 05-01 | Sound foundation | Migration/fields/error are appropriately isolated. |
| 05-02 | Needs revision | Delete classifier overclaims concurrent-delete distinction; location delete delta omits cascaded exits. |
| 05-03 | Mostly sound | Same delete-classifier issue; movement expected-version threading must be explicit. |
| 05-04 | Good but incomplete | Correctly covers RMW conflict paths, but not delete-version acquisition. |
| 05-05 | Strong schema core | Add durable consumer idempotency/checkpoint schema or explicitly narrow guarantees. |
| 05-06 | Blocked | `mutate` has no operation; movement-hook post-commit failure is unresolved. |
| 05-07 | Blocked | Durable consumer design and world-envelope wire adapter are missing. |
| 05-08 | Premature | Exactly-once consumer assertions cannot be met without durable idempotency. |
| 05-09 | Contradictory | Taxonomy includes scene participant kinds despite D-05 exclusion. |
| 05-10 | Needs revision | Location cascade manifest cannot be produced by the files this plan changes. |
| 05-11 | Blocked | Violates D-05 and census cannot detect FK-cascade character deletion. |
| 05-12 | Premature | INV-WORLD-2/4 would be falsely bound while cascade and guest-reaper gaps remain. |
| 05-13 | Sound | Narrow doc correction and regression guard are well scoped. |
| 05-14 | Strong prerequisite | Replace raw `go build`; clarify temporary expected-version behavior and delete semantics. |
| 05-15 | Necessary but incomplete | Creation is covered, but cleanup/reaper deletion can still remove characters without tombstones. |

## Suggestions

- Add a new prerequisite plan for character lifecycle deletion:

  - route guest reaping and failed-guest cleanup through a character-aware deletion service;
  - emit one tombstone per deleted character in the same transaction;
  - delete the player only after character tombstones and character deletion commit;
  - add a regression test proving guest expiration cannot create genesis-without-tombstone history.

- Remove `AddSceneParticipant` and `RemoveSceneParticipant` from:

  - the Phase 5 taxonomy;
  - outbox rollout;
  - command census;
  - delta/invariant claims.

  Leave the existing public-world methods unchanged or formally deprecate them under #4815.

- Replace the ambiguous mutation signature with a concrete executable contract, for example:

```go
mutate(
    ctx context.Context,
    intent wmodel.EnvelopeIntent,
    write func(context.Context) (*wmodel.MutationDelta, error),
) (*wmodel.MutationDelta, error)
```

  Keep writer repositories private to the executor so callers cannot supply arbitrary SQL. Alternatively, define typed command objects with one executor method per operation.

- Add a durable reference-consumer store, such as a table keyed by `(consumer_name, event_id)` plus epoch/feed watermark. Apply the consumer effect and record the event ID atomically. Test restart and retry after the JetStream duplicate window expires.

- Specify the relay wire adapter in 05-07:

  - canonical serialization;
  - subject mapping;
  - event type;
  - global versus per-kind schema versions;
  - feed-position/epoch representation;
  - skip-marker representation.

- Resolve `MovementHook` explicitly. Options include:

  - fold its durable session update into the same database transaction;
  - derive the dependent state from the world envelope in a consumer;
  - classify post-commit hook failure as operational degradation while returning command success.

  Do not retain “return failure after state + envelope commit.”

- Extend location deletion delta construction inside `location_repo.go` by selecting affected exits under lock before deletion, or explicitly define database-cascaded rows as outside the affected-aggregate manifest. The latter would require narrowing INV-WORLD-2.

- Simplify delete classification to two observable outcomes unless caller evidence is added:

  - existing row with mismatched version → `WORLD_CONCURRENT_EDIT`;
  - absent row → not-found, including a delete that already won.

- Add a Taskfile `build:all` target if required and use that from 05-14.

- Even if D-04 requires one PR, preserve reviewable commits at the plan boundaries and require a phase-level integration matrix after every wave, not only at the end.

## Risk Assessment

**Overall risk: HIGH**

The core mechanism is promising, and much of the repository analysis is excellent. The remaining issues are not cosmetic: they directly contradict D-05 or invalidate the proposed `INV-WORLD-ATOMIC-FEED`, `INV-WORLD-DELTA-PARITY`, and `INV-WORLD-WRITER-BOUNDARY` bindings. In particular, guest reaper FK cascades, the undefined mutation executor operation, and non-durable consumer deduplication prevent the phase from satisfying its own success criteria.

Once those blockers are resolved in the plan—not deferred to implementation judgment—the phase should be reviewable at **MEDIUM** implementation risk.

---

## Antigravity Review (round 5)

# Plan Review: HoloMUSH Phase 5 (World-Model Integrity Fixes)

This document provides structured feedback on the plan quality, completeness, and risks for **Phase 5: World-Model Integrity Fixes (M2 / M12)** of the HoloMUSH project. This review covers the revised 15-plan set (`05-01-PLAN.md` through `05-15-PLAN.md`), integrating prior review feedback from Round 4.

---

## 1. Executive Summary

- **Overall Verdict**: **APPROVED FOR EXECUTION (GREEN)**
- **Risk Level**: **LOW** (down from **HIGH** in Round 4)
- **Primary Finding**: The revised plans successfully incorporate all feedback from prior rounds. The core architecture is highly cohesive, Go import cycles are resolved by the introduction of the leaf package `wmodel`, and the boundary conditions for database transactions (including character settings, scene plugins, and guest/bootstrap-admin creation paths) have been correctly and honestly scoped.

---

## 2. Codex Round-4 Concerns vs. Plan Resolutions

The table below traces how the major concerns highlighted in the Round 4 review have been addressed in the latest plans:

| Concern (Round 4) | Plan Resolution | Status |
| :--- | :--- | :--- |
| **Unmodeled Writers Escaping SQL Fence** (settings & scene plugin writes) | Scoped the AST SQL fence in [05-09-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-09-PLAN.md) to exclude `scene_participants` and the `plugins/` directory (schema-blind false positive). Folded the genuine `UPDATE characters` settings write into the versioned/envelope world path in `internal/world/postgres` and rewired `character_settings_repo.go` to route through the world service. | **Resolved** |
| **False Guest/Bootstrap Transaction Atomicity** (non-world repos ignoring txKey) | Corrected in [05-15-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-15-PLAN.md) by narrowing the transaction boundary to the sound unit (character + binding + envelope). Handled the foreign key (FK) hazard by ordering player/role creation (player committed first; roles assigned after character commit) and explicitly documenting compensation gaps. | **Resolved** |
| **Non-Idempotent Skip Recovery** (fresh ULID on skips violating `INV-WORLD-3`) | Updated [05-07-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md) and [05-05-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-05-PLAN.md) to persist a stable `skip_marker_event_id` in the outbox table prior to publication, ensuring duplicate publishes are absorbed by JetStream and consumer deduplication. | **Resolved** |
| **Underspecified Lease Generation** (missing generation argument/column) | Stamped `lease_generation` in the `world_feed_counter` table (migration `000050`) and verified the fencing token in `MarkPublished(..., generation)` via a database re-read comparison in [05-07-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md). | **Resolved** |
| **Genesis Package-Contract Cycles** (outbox genesis importing postgres) | Added a consumer-owned `GenesisStore` interface declared in `internal/world/outbox` and implemented in `internal/world/postgres/genesis_store.go`, preserving the 05-07 eight-edge import guard. | **Resolved** |
| **Genesis/Reset Lacked Operator Entry Point** (library-only code) | Added real operator entry points via Cobra CLI subcommands `holomush world genesis` and `holomush world epoch-reset` in [05-11-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md). | **Resolved** |
| **Incomplete Caller/Mock Inventories** (missed test files in wave 1) | Replaced the fragile hand-maintained `files_modified` check with an unscoped whole-tree compile gate (`go build ./...`, `task test`, `task test:int`) in the wave-1 verification of [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md). | **Resolved** |
| **Invariants Using Symbolic IDs** (parser rejects symbolic names) | Mapped the invariants to canonical numeric IDs `INV-WORLD-1` through `INV-WORLD-4` in [05-12-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-12-PLAN.md), preserving symbolic names as legacy aliases. | **Resolved** |

---

## 3. Plan Strengths

- **Dependency Graph Integrity**: The architecture cleanly avoids circular dependencies through the leaf package `wmodel` (`internal/world/wmodel`). Production packages follow a strict dependency order: `world -> wmodel`, `outbox -> wmodel`, and `postgres -> world, outbox, wmodel`. The composition roots inject concrete implementations behind consumer-owned interfaces (`OutboxStore`, `Lease`, `GenesisStore`).
- **Storage-Owned Finalization**: The `WriteIntent` method owned by `postgres` allocates epoch/position, finalizes the envelope, and persists it in a single transaction. This prevents temporal anomalies where executors try to construct "finalized" envelopes before database storage has assigned positions.
- **Durable Genesis Checkpoints**: Keying genesis snapshot idempotency to a persistent `world_genesis_checkpoint` table is highly robust. Checking and inserting the checkpoint row under the serializing per-game counter lock prevents double-emission races on parallel executions.
- **Fail-Closed Execution**: The write executor requires a non-optional envelope intent at compile-time, and the delete methods require an explicit version parameter. Direct repository writes are blocked by reader-only interfaces on `world.Service`, making un-guarded writes compilation errors.

---

## 4. Key Risks and Mitigations

### Risk 1: Performance Impact of Serialized Write Locks
- **Description**: Locking the `world_feed_counter` row using `SELECT ... FOR UPDATE` serializes all write operations for a given game.
- **Impact**: Under high write load, this creates a throughput bottleneck.
- **Mitigation**: The plans correctly acquire the counter lock as late as possible (just before committing the mutation transaction). Ensure that no slow/blocking operations (e.g., network calls, disk writes outside the database) occur within the transaction after the counter lock is acquired.

### Risk 2: AST SQL Fence Bypass via Dynamic Queries
- **Description**: The AST SQL fence (`world_sql_fence_test.go`) scans production source code for string literals containing mutation SQL (e.g., `"UPDATE characters"`). It can be bypassed if queries are constructed dynamically (e.g., string concatenation or formatting).
- **Impact**: An un-guarded write could bypass the SQL fence and leak to production undetected.
- **Mitigation**: Enforce a strict coding guideline that all database queries targeting world tables must be static string literals. When implementing `world_sql_fence_test.go`, make the string scanning robust enough to check for substring matches or dynamic query patterns.

### Risk 3: Connection Starvation in Zero-Row Classifiers
- **Description**: When a write returns zero rows affected, a follow-up query checks the row's version. If this follow-up query attempts to acquire a new connection from the pool, it will deadlock if the connection pool is exhausted (e.g., constrained to a size of 1).
- **Impact**: Deadlock/hang during write conflicts.
- **Mitigation**: The plans mitigate this by executing the follow-up check using `withTx` and `execerFromCtx`, which reuse the caller's connection/transaction. During implementation, developers must be extremely careful not to use `r.pool` directly within any zero-row classification logic.

### Risk 4: Operational Management of Orphaned Guest Players
- **Description**: Because guest player creation commits prior to the character genesis transaction (to satisfy the character's `player_id` FK), a subsequent failure during character/binding creation will leave an orphaned player row in the database.
- **Impact**: Database cruft over time.
- **Mitigation**: This is an accepted and documented compensation gap. Ensure that a background cleanup worker or operational script exists to periodically prune guest players that have no associated character records.

---

## 5. Structured Recommendations

1. **Verify `withTx` Re-Entrancy in Code Review**: During the PR review of `05-14-PLAN.md`, verify that the re-entrant logic in `Transactor.InTransaction` correctly prevents nested commits/rollbacks and propagates errors cleanly to the outermost transaction.
2. **Review AST Fence Exclusions Periodically**: The AST SQL fence excludes the `plugins/` directory and `scene_participants` to avoid false positives. Ensure this exclusion does not lead to drift where core world tables are mutated from plugins. A future architecture phase should audit plugin table boundaries (tracked under issue `#4815`).
3. **Assert Late Counter Locking in Tests**: Add a dedicated concurrent integration test in the outbox store tests to verify that the locked counter is held for a minimal duration, proving that concurrent writes serialize only during the final commit phase.

---

_Executive summary (agy stdout):_

I have completed the review of the revised 15-plan set (`05-01-PLAN.md` through `05-15-PLAN.md`) for **HoloMUSH Phase 5 (World-Model Integrity Fixes)**. 

The detailed structured feedback has been saved to the artifact file:
👉 **[analysis_results.md](file:///Users/sean/.gemini/antigravity-cli/brain/bba20b08-fb16-4407-a33d-a723b5d69ef4/analysis_results.md)**

### Key Highlights of the Review:
1. **Overall Verdict**: **APPROVED FOR EXECUTION (GREEN)**. The plan set is robust, complete, and fully implements the normative mechanisms while addressing all 8 concerns raised during the Round 4 review.
2. **Prior Gaps Resolved**:
   - **Skip recovery retry-idempotency**: Persisting a stable `skip_marker_event_id` in the outbox table preserves `INV-WORLD-3` and prevents duplicate feed events on retries.
   - **Lease generation fencing**: The use of a durable `lease_generation` column in the database provides a concrete fencing token checked inside `MarkPublished`.
   - **Transaction boundaries**: The plans correctly narrow the guest/bootstrap atomic unit to character + binding + envelope, eliminating false claims about un-enrolled repositories (`player_repo`, `role_store`) while resolving the FK-blocking deadlock risk through ordering.
   - **Scope Boundaries (D-05)**: The AST SQL fence and outbox scope correctly exclude the plugin's `scene_participants` table while folding the settings `UPDATE characters` write into the versioned world boundary.
3. **Primary Risks Identified**:
   - **Throughput Serialization**: Acquiring the locked counter as late as possible is critical to minimize transaction holding time.
   - **AST SQL Fence limitations**: String scanners can be bypassed if queries are constructed dynamically. Developers should strictly use static string queries.
   - **Guest cruft**: A background worker should eventually clean up orphaned guest players if character creation fails after a player is committed.

---

## Consensus Summary (round 5)

**Verdict: NOT converged — divergent reviews.** Codex (source-grounded): **HIGH / NOT READY**, one more revision. Antigravity: **GREEN / approved for execution**. Per this project's established reviewer-reliability pattern (Codex has been source-grounded and reliable every round; Antigravity has a documented false-green history in this loop — false-green r1/r3, failed r4), and after **independently grounding the top findings against the actual plan text**, the Codex verdict is the one to weight. Round-4's targeted fixes landed correctly, but a fresh review reached a **new layer of real, previously-untested edges**.

### What round-4 fixed (BOTH reviewers confirm)

A1 stable skip-marker id, A2 durable `lease_generation`, B4 narrowed guest/bootstrap atomicity + FK ordering, and the C5 D-05 disposition (fence schema-scoping + `character_settings` fold-in + the two-`scene_participants`-tables distinction / plugin rejection) are all confirmed landed and correct — by Codex (Strengths §, e.g. it agrees the two-table distinction is real and the plugin-tree fence exclusion is justified) and by Antigravity (GREEN). **The round-4 incorporation itself was sound; the write-path/envelope/atomicity core is settled.**

### New round-5 findings (Codex). ★ = independently confirmed against plan text this session

- **HIGH — Guest-reaper character deletion escapes census + fence + outbox.** `guest_reaper.go:68` → `DeleteGuestPlayer` → `DELETE FROM players` → FK-cascades to `characters` (`000002_player_is_guest.up.sql:9`) with **no character tombstone**. Genesis-without-tombstone breaks INV-WORLD-4 / delta-parity / feed-completeness. Creation is fully covered (05-15 routes all 3 paths); **deletion is not** — no plan routes guest reaping (or failed-guest cleanup, `guest_service.go:165/193`) through a tombstone-emitting deletion. The `DELETE FROM players` also evades the SQL fence, which greps `DELETE FROM characters`.
- **HIGH ★ — 05-11 contradicts locked D-05 on `scene_participants`.** 05-09 correctly excludes `scene_participants` from the SQL fence, but **05-11 (lines 24/92/99/105/159) still routes `world.Service.AddSceneParticipant`/`RemoveSceneParticipant` through `mutate()` + the outbox + the taxonomy + the census bijection.** D-05 defers the vestigial `public.scene_participants` entirely to #4815 → those methods must be excluded from the outbox rollout / taxonomy / census too, not just the fence. The plans hold two positions at once.
- **HIGH ★ — `mutate(ctx, entity, expectedVersion, intent)` does not identify the operation.** 05-06:91's seam has no operation selector or write callback, so it cannot distinguish create/update/delete/move/preference/participant. It would collapse to runtime string-dispatch on `intent.kind` (undermining the compile-time-seam claim) unless it takes a write closure. Codex's fix: `mutate(ctx, intent, write func(ctx)(*MutationDelta, error))`, keeping writer repos private to the executor.
- **HIGH — Durable consumer idempotency claimed but not designed.** The A1 stable-id fix relies on JetStream's **finite** dedup window (`publisher.go:64/109`, `subsystem.go:281`). The reference consumer (05-07) claims crash/retry exactly-once but adds no durable idempotency table — an in-memory ULID set does not survive restart. Needs a durable `(consumer_name, event_id)` + epoch/feed watermark store applied atomically with the consumer effect.
- **HIGH/MED ★ — `MoveCharacter` movement-hook post-commit failure unresolved.** 05-06 deletes the post-commit *emit* path, but the movement *hook* (`movement_hook.go:13`, `service.go:802/807`) can still error **after** state+envelope commit → a "failed" command that already emitted an envelope, contradicting "failed commands emit nothing." Grep confirms 05-06/08/10 never address the hook disposition.
- **MEDIUM — Location-delete cascade delta.** Deleting a location FK-cascades its exits (`000001_baseline.up.sql:113`), but 05-02's delta is only the location tombstone while 05-10 claims cascaded children yet modifies only service/payload (not `location_repo.go`). The repo must preselect cascaded exits under lock, or INV-WORLD-2 (delta-parity) must explicitly exclude DB cascades.
- **MEDIUM ★ — 05-14's raw `go build ./...` violates CLAUDE.md's MUST-use-`task` rule.** 05-14:188 embeds `go build ./... && task test && task test:int`; CLAUDE.md (~line 200) forbids direct `go build`/`go test`. `task test`/`task test:int` already compile `./...`; a pure production all-package build should be a new `task build:all` target, not a raw command. **(This one originated in my round-4 directive — my error to own.)**
- **MEDIUM — Envelope wire adapter under-specified** — `wmodel.Envelope` → `eventbus.Event.Payload` mapping: per-kind vs the global `App-Schema-Version` header (`publisher.go:303`, reserved per `types.go:162`), how consumers read `epoch`/`feed_position`, and the skip-marker wire shape.
- **MEDIUM — Concurrent-delete classification overstated** — a locked `SELECT … FOR UPDATE` can't distinguish concurrent-delete from never-existed; simplify to two observable outcomes (mismatched-version → `WORLD_CONCURRENT_EDIT`; absent → not-found) unless a caller existence token is added.
- **MEDIUM — One-PR (D-04) is large for reliable review** (advisory) — suggests reviewable per-plan commits + a per-wave integration matrix, not only an end-of-phase gate.

### Divergent views

Antigravity rated the identical plan set GREEN and surfaced **none** of the above — including the two findings ( D-05 outbox contradiction, `go build` policy violation ) that are plainly present in the plan text. Given its false-green history in this loop and that four Codex findings are directly confirmable, Antigravity's optimism does not override Codex.

### Bottom line

Both reviewers agree the **core architecture is settled** and round-4's fixes were correct. But this is **not** convergence: Codex's fresh pass reached a real new layer — character-lifecycle **deletion** (guest reaper / cleanup), cascade-delta parity, consumer durability, the `mutate` executor contract, the wire adapter, and a D-05 self-contradiction in the outbox rollout. Codex frames all of them as resolvable to **MEDIUM** risk in-plan. The honest state is **"one more targeted `--reviews` incorporation pass,"** not "ready to execute." Several fixes are cheap (drop `scene_participants` from 05-11's outbox/census; `go build` → `task build:all`; simplify delete classification); a few need real design (guest-reaper tombstone deletion path; `mutate` write-closure; durable consumer store; movement-hook ordering; location cascade delta; wire adapter).
