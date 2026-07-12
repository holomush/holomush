---
phase: 5
review_round: 2
reviewers: [codex, antigravity]
reviewed_at: 2026-07-12T10:51:41Z
plans_reviewed: [05-01-PLAN.md, 05-02-PLAN.md, 05-03-PLAN.md, 05-04-PLAN.md, 05-05-PLAN.md, 05-06-PLAN.md, 05-07-PLAN.md, 05-08-PLAN.md, 05-09-PLAN.md, 05-10-PLAN.md, 05-11-PLAN.md, 05-12-PLAN.md, 05-13-PLAN.md, 05-14-PLAN.md]
prior_round: "round 1 preserved in git at commit 4238fc876; its findings are incorporated in the current 14-plan set"
---

# Cross-AI Plan Review — Phase 5 (ROUND 2, revised 14-plan set)

Second-round review of the plans **after** the round-1 findings were incorporated (which added
05-14 and restructured the transaction/outbox/relay seams). Both reviewers verified the round-1
structural fixes against the live tree AND hunted for new/residual issues. **Both reach HIGH /
not-execution-ready** — the round-1 fixes HELD, but the revision opened new seams.

Note: unlike round 1, Antigravity this round verified against the real codebase (grounded
`file:line`, no stale-sandbox errors) — its round-2 findings are trustworthy.

---

## Codex Review (round 2)

# Phase 5 Round-2 Plan Review

## 1. Summary

The revision resolves several round-1 findings, but the plan set is not yet execution-ready.

The re-entrant transaction design, numeric invariant IDs, PostgreSQL subsystem count, SQL-location rule, and explicit rollout-order deviation are now grounded in the source. However, several seams remain structurally broken:

- The proposed package graph creates Go import cycles.
- `mutate(..., envelope)` requires the envelope before the repository produces the delta from which that envelope must be built.
- The claimed compile-time write fence remains bypassable through exported repositories, especially `PropertyRepository`.
- The poison “skip” operation creates the exact feed-position gap the invariant prohibits.
- `CreateCharacter` and feed-epoch wiring omit required composition/schema changes.
- The repository signature redesign has a wider compile-time blast radius than plan 05-14 lists.

Overall risk: **HIGH**.

## 2. Round-1 Resolution Verification

| Round-1 resolution | Verdict | Verification |
|---|---|---|
| Re-entrant `InTransaction`/`withTx` and self-transacting repos | **PARTIALLY HOLDS** | The proposed join semantics are sound: only the outer transaction commits or rolls back, correcting current unconditional `Begin` at [transactor.go:27](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/transactor.go:27). The identified self-transacting sites are real: [exit_repo.go:60](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/exit_repo.go:60), [exit_repo.go:178](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/exit_repo.go:178), and [object_repo.go:172](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/object_repo.go:172); scene writes also bypass ambient transactions at [scene_repo.go:40](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/scene_repo.go:40) and [scene_repo.go:57](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/scene_repo.go:57). But property Create/Update still use the pool directly at [property_repo.go:60](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go:60) and [property_repo.go:136](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go:136), and no task actually converts them. |
| All world/outbox/counter SQL in `internal/world/postgres` | **PARTIALLY HOLDS** | Plans 05-05 and 05-07 consistently place counter, outbox insert, relay read/mark/prune, and `pg_notify` SQL in postgres. Plan 05-09 has a real package allowlist. But 05-11 puts genesis and epoch/reset work solely in `internal/world/outbox/genesis.go`, with no postgres storage file, leaving its SQL ownership unspecified. |
| Fence opens in 05-06 and completes in 05-11 | **STILL BROKEN** | 05-06 now correctly says it is transitional. The claimed completion in 05-11 only changes `Service` fields from the current write-capable types at [service.go:45](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/service.go:45). Exported postgres repositories remain directly callable, and `PropertyRepository` still exposes Create/Update/Delete at [property.go:32](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/property.go:32). Reader fields on one service are not a repository-wide compile-time fence. |
| Numeric `INV-WORLD-1..4`, parser unchanged | **PARTIALLY HOLDS** | Numeric IDs will bind: the parser accepts only numeric suffixes at [invariant_registry_test.go:163](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/test/meta/invariant_registry_test.go:163). The proposed delta test explicitly requires equality, not presence. However, `INV-WORLD-1` is bound only to the opt-in resilience suite, which skips unless `HOLOMUSH_RUN_QUARANTINED=1` at [resilience_suite_test.go:46](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/test/integration/resilience/resilience_suite_test.go:46), so it is not continuously exercised by the normal integration lane. |
| AST/token SQL fence | **PARTIALLY HOLDS** | The positive/negative fixture and explicit allowlist design are implementable. It is no longer pretending depguard can inspect SQL. But the table list omits `entity_properties`, even though Phase 5 treats property mutation as part of the world writer boundary. |
| Relay lease, wakeup, core composition | **STILL BROKEN overall** | The composition claim is accurate: production currently has 15 stubs at [core_subsystems_test.go:32](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/cmd/holomush/core_subsystems_test.go:32), and `productionSubsystems` currently returns those 15 at [core.go:1445](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/cmd/holomush/core.go:1445). Transaction-side `pg_notify` is the correct commit-coupled wakeup model. The advisory-lock fencing and poison recovery remain unsafe, detailed below. |
| Delete CAS interface and mocks | **HOLDS narrowly** | Plan 05-14 correctly identifies all four current world Delete signatures, such as [repository.go:33](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go:33), and lists the five generated world repository mocks. The broader write-return redesign, however, misses non-world interfaces and adapters. |
| `MutationDelta` threaded repo → manifest | **STILL BROKEN** | The delta contains the needed repository-internal IDs, but the package graph and ordering of envelope construction make the proposed threading impossible as written. |
| Counter late lock + timeouts | **STILL PARTLY BROKEN** | Late allocation is explicit and sound. “Statement/transaction timeouts” appear only in 05-05’s truths/objective; no task, API, acceptance criterion, or test sets or verifies a timeout. |
| Zero-row classifier connection reuse + pool-size-1 test | **HOLDS** | The planned `withTx` plus `execerFromCtx` uses the existing ambient `pgx.Tx` exposed by [helpers.go:33](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/helpers.go:33). The pool-size-1 regression test is explicitly required. |
| CreateCharacter fail-close | **PARTIALLY HOLDS** | The plan correctly targets the current fallback at [auth_handlers.go:521](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/grpc/auth_handlers.go:521), but omits the dependency and composition changes needed to supply an outbox writer. |
| Structural census | **PARTIALLY HOLDS** | It no longer derives coverage from the incomplete `Mutator` interface at [mutator.go:18](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/mutator.go:18). But the plan never defines a sound structural rule that discovers a new mutation before it already contains a `mutate` call. |
| Examine-consumer audit | **HOLDS** | The plan now requires a consumer audit before removing the current examine emission sites, such as [service.go:904](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/service.go:904). |
| Preserve README line 22 | **HOLDS** | The revised plan explicitly distinguishes reconnect/event replay from false world-state reconstruction claims. |
| Slice-3 rollout-before-enforcement deviation | **HOLDS** | The deviation is now explicit in 05-09, 05-10, and 05-12 with a concrete green-intermediate-commit rationale. |

## 3. Strengths

- The re-entrant transaction shape is materially better and matches the existing context-based executor mechanism.
- The revised plans found the real `pool.Begin` and scene `pool.Exec` escape sites rather than treating transaction enrollment as automatic.
- Numeric invariant IDs match the actual registry parser.
- The outbox/counter/relay SQL placement is consistent across 05-05, 05-07, and 05-09.
- The relay composition work is grounded in the real 15-element subsystem list.
- Delete deltas now account for reverse exits and cascaded aggregates generated inside repositories.
- The plans explicitly require a pool-size-1 classifier regression test and version refresh after successful writes.
- `CreateCharacter`’s existing fail-open fallback and the examine notification compatibility question are now directly addressed.
- README replay semantics and the deliberate rollout-order deviation are handled carefully rather than mechanically.

## 4. Concerns

### HIGH — The package graph and delta timing cannot compile as specified

Plan 05-05 makes `internal/world/outbox.Envelope` map from `world.MutationDelta`. Plan 05-06 then places `mutate(..., outbox.Envelope)` in package `world`. Since `mutator.go` is package `world` ([mutator.go:4](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/mutator.go:4)), that creates:

```text
internal/world → internal/world/outbox → internal/world
```

Plan 05-07 risks a second cycle because postgres persists `outbox.Envelope`, while the relay package calls the concrete postgres store:

```text
internal/world/outbox → internal/world/postgres → internal/world/outbox
```

There is also a temporal contradiction: callers must pass a complete envelope into `mutate`, but its manifest must be built from the delta returned after the repository write.

The design needs a cycle-free contract such as:

```go
mutate(ctx, command, envelopeIntent) (*MutationDelta, error)
```

where `envelopeIntent` excludes the manifest and the executor constructs the final persisted envelope after the write. Storage and relay should communicate through an interface owned by the consumer package, injected by setup.

### HIGH — Plan 05-14’s interface redesign misses real adapters and callers

Changing `CharacterRepository.Create` from `error` to `(*MutationDelta, error)` breaks the auth-side repository contract, which requires `Create(...) error` at [character_service.go:17](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/auth/character_service.go:17). The production adapter also assumes one return value at [adapters.go:38](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/bootstrap/setup/adapters.go:38), and it is wired in production at [sub_grpc.go:326](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/cmd/holomush/sub_grpc.go:326).

Plan 05-14 lists only `internal/world`, postgres implementations, and worldtest mocks. It omits at least the bootstrap adapter and any affected narrow interfaces/tests. This will not compile as an atomic wave-1 change.

Prefer separate read/write interfaces or an adapter method that discards the delta, and enumerate all compile-time assertions before execution.

### HIGH — The completed write fence remains bypassable

Changing `world.Service` fields to reader interfaces prevents direct writes only inside that struct. It does not prevent other production code from constructing and invoking exported postgres repositories.

The largest concrete hole is `PropertyRepository`: it publicly exposes writes ([property.go:32](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/property.go:32)), and Create/Update bypass ambient transactions today. Plan 05-11 merely says to “confirm” those writes remain in postgres; it neither routes them through `mutate` nor removes their public write capability.

The SQL fence also allowlists all of `internal/world/postgres`, so it cannot distinguish an approved mutation-executor call from an arbitrary direct repository call.

### HIGH — Operator “skip” violates the feed-order invariant

Plan 05-07 says a poison row is marked `published_at` without publication and the relay resumes at the next position. That creates a missing `feed_position` on the wire, directly contradicting the same plan’s “gap-free” guarantee and `INV-WORLD-3`.

Recovery must either:

- remain halted until the original envelope can be published, or
- publish an explicit operator-authorized poison/skip marker at the same feed position before advancing.

Simply marking the row published is not compatible with gap-free delivery.

### HIGH — CreateCharacter and epoch/reset are not wired end-to-end

`CoreServer` dependencies live in [server.go:152](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/grpc/server.go:152), while gRPC production options are assembled in [sub_grpc.go:477](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/cmd/holomush/sub_grpc.go:477). Plan 05-11 lists neither file and defines no outbox-writer field/option/provider. Editing only `auth_handlers.go` cannot obtain the store needed for same-transaction emission.

Likewise, migration 000050 defines no epoch column or epoch table, yet 05-11 promises to “advance the feed epoch.” No migration or postgres-store task supplies persistent epoch state.

### MEDIUM — Advisory-lock fencing overstates its guarantee

A session advisory lock on a dedicated PostgreSQL connection prevents two healthy lock holders. It does not by itself fence an old process from publishing to NATS after PostgreSQL has released its session lock but before the process observes the loss. “Re-acquire on observed connection loss” handles graceful detection, not every partition/timeout handoff.

At minimum, require every fetch/claim to occur through the lease connection and document at-least-once duplicate behavior during ambiguous handoff. If “split-brain publish impossible” is truly required, introduce a fencing generation that the publishing/acknowledgment protocol actually checks.

### MEDIUM — SQL fence coverage omits `entity_properties`

The 05-09 fence enumerates seven tables but excludes `entity_properties`, whose raw writes are visible at [property_repo.go:60](/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go:60). That omission weakens both the property rollout and `INV-WORLD-4`.

### MEDIUM — Counter timeout hardening is asserted but unassigned

No 05-05 or 05-06 task specifies `lock_timeout`, `statement_timeout`, a bounded context, expected error code, or timeout test. The operational resolution therefore remains prose-only.

### MEDIUM — Invariant bindings are not all durable regression gates

`INV-WORLD-1` is assigned to the opt-in resilience suite rather than the always-run outbox transaction test. A syntactically bound invariant can therefore remain unexecuted in normal CI.

The delta-parity task also says a normal `task test` file will “commit a mutation” and compare actual rows. If it genuinely uses PostgreSQL, it belongs in the integration lane; if it is a pure mapper test, it does not alone prove committed-row parity.

### MEDIUM — The structural census discovery rule is undefined

Parsing method declarations with `go/ast` does not inherently reveal whether a method mutates state. Detecting existing calls to `mutate` misses the exact failure case: a newly added direct write without `mutate`. Name-prefix classification is brittle and not stated.

The census needs an explicit structural marker or closed command interface whose membership is mechanically discoverable.

## 5. Suggestions

1. Revise the package boundaries before implementation:
   - Put `MutationDelta`, envelope intent, and affected-aggregate value types in a cycle-neutral package.
   - Let the executor finalize the envelope after receiving the delta.
   - Inject a narrow outbox-store interface into the relay; do not import postgres from outbox.

2. Split broad repository interfaces into reader and writer interfaces without changing unrelated auth-facing method signatures. Add adapters where a caller legitimately does not need the delta.

3. Make the writer executor the only production owner of writer repositories. Expose reader-only constructors/providers elsewhere, and either envelop `PropertyRepository` writes or explicitly remove them from production mutation surfaces.

4. Replace poison “skip” with same-position recovery-marker publication, or delete skip support and retain strict halt-until-repaired semantics.

5. Add persistent epoch schema/store work to migration 000050 or a new paired migration, and add the missing `CoreServer`/`sub_grpc.go` outbox dependency wiring.

6. Add `entity_properties` to the SQL fence and define timeout mechanics and tests in 05-05.

7. Bind `INV-WORLD-1` to the always-run transaction rollback test; retain the chaos test as supplemental evidence.

8. Define the census around an explicit command declaration/registration construct rather than inference from method names or already-correct calls.

## 6. Risk Assessment

**HIGH.**

The remaining defects are architectural and compile-time, not polish issues. As written, implementation is likely to encounter import cycles, incomplete interface migrations, missing production dependency wiring, and a relay recovery path that violates its own ordering invariant. The transaction, parser, subsystem-count, documentation, and test-harness revisions are strong, but the package/executor/relay seams need one more planning revision before execution.

---

## Antigravity Review (round 2)

> Recovered from agy's brain disk (stdout carried only a summary). This round agy operated on the
> real working tree and confirmed all 10 checked round-1 resolutions hold, then surfaced 5 residual
> issues — all grounded in `file:line`. Reliable this round.

# HoloMUSH Phase 5 Plan Review (Round 2) — Verification & Audit Report

This report provides the round-2 verification of the 14 revised implementation plans for **HoloMUSH Phase 5: World-Model Integrity Fixes (M2 / M12)**. The review has been conducted against the active codebase located at `/Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5`.

---

## 1. Executive Summary

The round-2 review confirms that the revised plans successfully incorporate the structural feedback from the round-1 review. The transaction boundaries are properly aligned, package boundary violations have been corrected, event-sourcing claims have been downgraded while preserving client catch-up functionality, and the invariant bindings are structurally sound.

However, a detailed analysis of the interfaces and seams has surfaced **five residual issues** that must be addressed in the plans before proceeding to execution. The most critical is the transaction leak in `PropertyRepository` and the missing `expectedVersion` parameter in `Move` / `UpdateLocation` repository signatures.

---

## 2. Verification of Round-1 Resolutions

Each of the round-1 resolutions has been checked against both the revised plans and the actual codebase:

### 1. Re-entrancy & Self-transacting Repositories
*   **Status**: **Verified Sound**
*   **Reference Plans**: [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md)
*   **Findings**: The plan to make `Transactor.InTransaction` and its internal `withTx` helper re-entrant is structurally correct. It checks `txFromContext(ctx) != nil` to join the ambient transaction rather than opening nested transactions. The self-transacting repository methods in [exit_repo.go](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/exit_repo.go#L60) and [object_repo.go](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/object_repo.go#L172) are correctly targeted to use the ambient transaction via `execerFromCtx(ctx, r.pool)`.

### 2. Package Placement
*   **Status**: **Verified Sound**
*   **Reference Plans**: [05-05-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-05-PLAN.md), [05-07-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md)
*   **Findings**: The package boundaries have been cleaned up. All SQL logic (including outbox writes, feed counter updates, and relay reads/writes) is placed inside `internal/world/postgres`. The `internal/world/outbox` package is strictly limited to domain types (like `Envelope`) and the logical publisher loop, resolving the package visibility conflict for `execerFromCtx`.

### 3. Write Fence Rollout
*   **Status**: **Verified Sound**
*   **Reference Plans**: [05-06-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-06-PLAN.md), [05-11-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md)
*   **Findings**: The transition plan is safe. [05-06-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-06-PLAN.md) opens the `mutate` seam and migrates only `MoveCharacter`. [05-11-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md) migrates the remaining commands and completes the compile-time fence by swapping `world.Service` repository fields to read-only views (`LocationReader`, etc.). This avoids intermediate build failures during the rollout.

### 4. Invariant ID Syntax
*   **Status**: **Verified Sound**
*   **Reference Plans**: [05-12-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-12-PLAN.md)
*   **Findings**: The plans successfully adopt the numeric invariant IDs `INV-WORLD-1` through `INV-WORLD-4` to match the parser regex `INV-[A-Z]+-\d+` in [invariant_registry_test.go:163](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/test/meta/invariant_registry_test.go#L163). The symbolic names (such as `ATOMIC-FEED`) are correctly downgraded to the summary/legacy metadata.

### 5. AST-based SQL Fence
*   **Status**: **Verified Sound**
*   **Reference Plans**: [05-09-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-09-PLAN.md)
*   **Findings**: The AST/token-based parser meta-test in `test/meta/world_sql_fence_test.go` is implementable and replaces the package-level `depguard` package matching. It walks the AST, inspects string literals for mutation keywords (`INSERT`, `UPDATE`, `DELETE`) targeting world tables, and allowlists `internal/world/postgres`.

### 6. Relay Lease & Wakeup Mechanics
*   **Status**: **Verified Sound**
*   **Reference Plans**: [05-07-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md)
*   **Findings**: The lease is now explicitly bound to a session-level PostgreSQL advisory lock on a dedicated connection. Wakeup is driven transactionally by `pg_notify` within the database transaction, backed by a periodic sweep fallback. The composition wiring in `cmd/holomush/core.go` and the `productionSubsystems` stub count cascade (from 15 to 16) are correctly updated.

### 7. Delete-CAS Mechanics
*   **Status**: **Verified Sound**
*   **Reference Plans**: [05-02-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-02-PLAN.md), [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md)
*   **Findings**: Delete signatures now accept `expectedVersion`. The repositories use a prior in-transaction read (`SELECT FOR UPDATE`) to distinguish between a row that never existed (returning `LOCATION_NOT_FOUND`, etc.) and a row that was concurrently deleted by another transaction (returning `WORLD_CONCURRENT_EDIT`).

### 8. MutationDelta Propagation
*   **Status**: **Verified Sound**
*   **Reference Plans**: [05-10-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-10-PLAN.md), [05-11-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md), [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md)
*   **Findings**: The `MutationDelta` returned from repo writes carries cascade IDs (like the reverse exit ID) and aggregate version transitions. This delta is correctly threaded to the outbox manifest builder inside `mutate()`, ensuring the outbox manifest is constructed from database facts rather than command inputs.

---

## 3. Newly Discovered Residual Issues & Seams

During this second-round verification, **five residual issues** have been identified. They must be resolved in the plans before execution:

### ⚠️ Issue 1: PropertyRepository Transaction Leak
*   **Location**: [property_repo.go:60](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go#L60) & [property_repo.go:136](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go#L136)
*   **Impact**: High
*   **Description**: While `PropertyRepository.Delete` and `DeleteByParent` use `execerFromCtx`, the `Create` and `Update` methods write directly using `r.pool.Exec` on the connection pool. Because these write methods escape the transaction context, property modifications during parental operations (such as character deletions or entity updates funneled through `world.Service`) will execute on separate connection sessions. This breaks atomicity and introduces database lock contention / deadlocks.
*   **Required Fix**: Refactor `PropertyRepository.Create` and `PropertyRepository.Update` in [property_repo.go](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/property_repo.go) to use `execerFromCtx(ctx, r.pool)`. Update [05-11-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md) and [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md) to explicitly include these methods in the `execerFromCtx` refactor task.

### ⚠️ Issue 2: Missing `expectedVersion` in `Move` / `UpdateLocation` Repo Signatures
*   **Location**: [repository.go:118](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go#L118) & [repository.go:161](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/repository.go#L161)
*   **Impact**: High
*   **Description**: In [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md), the repository interfaces are updated to return `(*MutationDelta, error)`. However, the signatures for Object `Move` and Character `UpdateLocation` do not accept an `expectedVersion` parameter. Since these methods do not receive a struct containing a version field (unlike `Create`/`Update`), they cannot enforce optimistic concurrency checks (LWW / CAS) against the version read at the beginning of the command.
*   **Required Fix**: Update [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md) and the corresponding repository files to include `expectedVersion int` in the `ObjectRepository.Move` and `CharacterRepository.UpdateLocation` signatures. The calling commands in `service.go` must pass the version read during the initial `Get` query.

### ⚠️ Issue 3: SQL Fence Gap for `entity_properties`
*   **Location**: [world_sql_fence_test.go](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/test/meta/world_sql_fence_test.go) (planned in 05-09)
*   **Impact**: Medium
*   **Description**: The AST/token-based SQL fence test introduced in [05-09-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-09-PLAN.md) checks for mutation SQL (`INSERT`, `UPDATE`, `DELETE`) targeting only `locations`, `exits`, `characters`, `objects`, `scene_participants`, `outbox`, and `world_feed_counter`. It does not include the `entity_properties` table. Consequently, a developer could bypass the repositories and write raw SQL mutations directly targeting entity properties outside of the `internal/world/postgres` package.
*   **Required Fix**: Include `entity_properties` in the list of fenced tables inside `test/meta/world_sql_fence_test.go` and update the requirements in [05-09-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-09-PLAN.md).

### ⚠️ Issue 4: Lack of Caller/Trigger for Relay Skip Affordance
*   **Location**: [relay.go](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/outbox/relay.go) (planned in 05-07)
*   **Impact**: Low/Medium
*   **Description**: Although [05-07-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md) implements the database logic for skipping poison outbox positions, there is no command-line tool, gRPC administrative endpoint, or in-game command wired to invoke this method. As a result, when the relay halts due to a poison envelope, operators will still have to execute raw SQL updates against the `outbox` table to clear the halt, defeating the "clear the blockage without raw SQL" objective.
*   **Required Fix**: Define an administrative command (e.g., in the `holomush` or `agy` CLI tools) that exposes this skip method, allowing operators to skip a specific position safely.

### ⚠️ Issue 5: Relay Query Connection Fencing
*   **Location**: [outbox_store.go](file:///Volumes/Code/github.com/holomush/.worktrees/v0.12-phase5/internal/world/postgres/outbox_store.go) (planned in 05-07)
*   **Impact**: High (Split-Brain Risk)
*   **Description**: The relay uses the outbox store to fetch unpublished rows and mark them published. If the outbox store executes these queries on the general connection pool (`r.pool`) rather than the dedicated connection holding the advisory lock lease, a network partition that drops the dedicated lease connection might go unnoticed by the query runner. The partitioned instance could continue to fetch and publish events via the pool before realizing it has lost its lease, leading to a split-brain double-publish.
*   **Required Fix**: Ensure the outbox store methods called by the relay are executed strictly on the same dedicated connection that holds the advisory lock, or tightly check lease validity inside the transaction block executing the queries. Update [05-07-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md) to explicitly require this connection routing.

---

## 4. Conclusion & Action Items

All 10 structural resolutions checked from Round 1 hold successfully. To achieve absolute integrity and compiler/concurrency safety, the implementation plan waves should be adjusted to incorporate the five residual fixes:
1. Include `PropertyRepository.Create`/`Update` in the `execerFromCtx` refactor of [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md) and [05-11-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-11-PLAN.md).
2. Add `expectedVersion int` parameters to `ObjectRepository.Move` and `CharacterRepository.UpdateLocation` signatures in [05-14-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-14-PLAN.md).
3. Include `entity_properties` in the AST SQL fence of [05-09-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-09-PLAN.md).
4. Wire the outbox skip method to an admin CLI or endpoint in [05-07-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md).
5. Require the relay's DB methods to run on the dedicated lease connection in [05-07-PLAN.md](file:///.planning/phases/05-world-model-integrity-fixes-m2-m12/05-07-PLAN.md).

---

## Consensus Summary (round 2)

**Net verdict: the round-1 fixes HELD, but the revised set is still NOT execution-ready — both
reviewers rate it HIGH.** The transaction re-entrancy, numeric invariant IDs, subsystem-count
cascade, SQL-location rule, doc handling, and rollout-order deviation are all confirmed sound
against source. The new blockers are seams the round-1 restructure introduced. Recommend one more
`/gsd-plan-phase 5 --reviews` pass. The loop is converging (round-2 findings are strictly
downstream of round-1's fixes), not thrashing.

### Round-1 resolutions that HELD (both reviewers confirmed against source)

Re-entrant `InTransaction`/`withTx` join semantics (only outer commits); numeric `INV-WORLD-1..4`
bind under the real parser regex `INV-[A-Z]+-\d+` (`invariant_registry_test.go:163`); the AST/token
SQL fence is implementable (no longer pretends depguard can read SQL); relay composition is grounded
in the real 15-stub `productionSubsystems` (→16); Delete-CAS `expectedVersion` + prior-in-tx-read
classification; `MutationDelta` carries cascade/reverse-exit IDs; zero-row classifier reuses the
ambient `pgx.Tx` + pool-size-1 test; examine-consumer audit; README:22 preserved; rollout-order
deviation explicit. The self-transacting escape sites (`exit_repo.go:60/178`, `object_repo.go:172`,
`scene_repo.go:40/57`) are correctly targeted.

### Agreed NEW concerns (both reviewers, HIGH priority)

1. **`PropertyRepository.Create`/`Update` transaction leak** — `property_repo.go:60/136` still write via
   `r.pool.Exec` directly; the round-1 `execerFromCtx` sweep missed them, and no task converts them.
   Property mutations funneled through a parent operation (character delete, entity update) escape the
   mutation transaction → breaks atomicity + risks deadlocks. Add Property Create/Update to the
   `execerFromCtx` refactor (05-14/05-11).
2. **SQL fence omits `entity_properties`** — the 05-09 fence lists 7 tables but not `entity_properties`
   (`property_repo.go:60`), so raw property writes outside the repo package are not caught; weakens
   INV-WORLD-4. Add it to `world_sql_fence_test.go`.
3. **Relay must run its DB queries on the dedicated lease connection, not the general pool** — if the
   outbox store fetch/mark runs on `r.pool` rather than the advisory-lock connection, a partition that
   drops the lease connection can still double-publish before the process notices (split-brain).
   Route the relay's store methods through the lease connection (Codex frames this as: advisory lock
   alone doesn't fence a NATS publish after PG releases the session lock — needs a fencing generation
   checked in the publish/ack path, or documented at-least-once during ambiguous handoff).

### Codex-only NEW findings (single reviewer, source-grounded — HIGH)

- **Go import cycles + envelope-before-delta contradiction.** `mutate(..., outbox.Envelope)` in package
  `world` (`mutator.go:4`) importing `outbox` which maps from `world.MutationDelta` creates
  `world → outbox → world`; and `outbox → postgres → outbox` (postgres persists `outbox.Envelope`, relay
  calls the concrete postgres store). Plus a temporal impossibility: the caller must pass a complete
  envelope into `mutate`, but the manifest can only be built from the delta returned *after* the write.
  Fix: `mutate(ctx, command, envelopeIntent) (*MutationDelta, error)` — intent excludes the manifest,
  executor finalizes the envelope post-write; put `MutationDelta`/intent/aggregate types in a
  cycle-neutral package; inject a narrow outbox-store interface into the relay (don't import postgres
  from outbox). **This is the biggest new blocker — it's a package/executor seam redesign.**
- **05-14's interface redesign has a wider compile-time blast radius than listed.** Changing
  `CharacterRepository.Create` to `(*MutationDelta, error)` breaks the auth-side contract
  (`internal/auth/character_service.go:17` needs `Create(...) error`), the production adapter
  (`internal/bootstrap/setup/adapters.go:38`), wired at `cmd/holomush/sub_grpc.go:326`. 05-14 lists only
  `internal/world` + postgres + worldtest mocks → won't compile as an atomic wave-1 change. Fix: split
  reader/writer interfaces or an adapter that discards the delta; enumerate ALL compile-time assertions.
- **The completed write fence is still bypassable.** Reader fields on `world.Service` only stop direct
  writes *inside that struct*; exported postgres repos (esp. `PropertyRepository`, `property.go:32`)
  remain constructible and callable elsewhere. The SQL fence allowlists *all* of
  `internal/world/postgres`, so it can't distinguish an approved executor call from an arbitrary repo
  call. Make the writer executor the only production owner of writer repos; envelop or remove
  `PropertyRepository` public writes.
- **Operator "skip" violates INV-WORLD-3 (feed-order).** Marking a poison row `published_at` without
  publishing leaves a missing `feed_position` on the wire — the exact gap the invariant forbids. Fix:
  stay halted until publishable, OR publish an explicit operator-authorized poison/skip marker *at the
  same position* before advancing. (This directly contradicts the round-1 skip affordance as drawn.)
- **CreateCharacter + epoch/reset not wired end-to-end.** `CoreServer` deps (`server.go:152`) + gRPC
  option assembly (`sub_grpc.go:477`) are unlisted in 05-11 and no outbox-writer field/option/provider
  is defined — editing only `auth_handlers.go` can't obtain the store for same-tx emission. And
  migration 000050 has no epoch column/table, yet 05-11 promises to "advance the feed epoch" — no
  migration supplies persistent epoch state.

### Codex-only MEDIUM

- Counter timeout hardening asserted but unassigned (no task sets `lock_timeout`/`statement_timeout` or
  a timeout test). — `INV-WORLD-1` bound only to the opt-in resilience suite (skips unless
  `HOLOMUSH_RUN_QUARANTINED=1`, `resilience_suite_test.go:46`), so a "bound" invariant isn't
  continuously exercised; bind it to the always-run outbox transaction-rollback test instead. — Census
  discovery rule undefined: `go/ast` over method declarations doesn't reveal *mutation*, and detecting
  existing `mutate` calls misses a *new* direct write; needs an explicit command marker / closed
  registration construct.

### Antigravity-only NEW finding (HIGH)

- **Missing `expectedVersion` on `ObjectRepository.Move` (`repository.go:118`) and
  `CharacterRepository.UpdateLocation` (`repository.go:161`).** These take no version-bearing struct, so
  the 05-14 redesign gives them no way to enforce CAS — containment changes and character moves can
  still lose updates (the exact LWW hole MODEL-03 must close, for the move path). Add `expectedVersion
  int` to both signatures and thread the read version from `service.go`.

### Divergent Views

Minimal this round — the reviewers converge on HIGH and overlap on the property-leak, entity_properties
fence, and relay-connection findings. Antigravity did not surface the import-cycle / interface-blast-
radius / skip-violates-ordering blockers (Codex's deepest catches); Codex did not separately call out
the Move/UpdateLocation `expectedVersion` gap (Antigravity's). The two are complementary — treat the
union as the round-2 fix list.

### Recommended next step

`/gsd-plan-phase 5 --reviews` (round-2 incorporation). Priority order:
1. **Package/executor seam redesign** — break the `world ↔ outbox ↔ postgres` import cycles and the
   envelope-before-delta contradiction (`mutate` returns the delta; envelope finalized post-write;
   cycle-neutral types package; injected outbox-store interface). This is the load-bearing fix.
2. **Interface blast radius** — enumerate every caller/adapter of the redesigned repo signatures
   (auth `character_service.go`, `adapters.go`, `sub_grpc.go`); reader/writer split or delta-discarding
   adapter so wave-1 compiles atomically.
3. **Property writes** — Create/Update onto `execerFromCtx`; `entity_properties` into the SQL fence;
   `expectedVersion` on Move/UpdateLocation.
4. **Relay** — replace "mark published without publishing" with a same-position recovery marker (or
   strict halt-until-repaired); route relay DB queries through the lease connection (or a checked
   fencing generation); wire the skip method to an admin command.
5. **End-to-end wiring** — CreateCharacter outbox-writer dependency through `server.go`/`sub_grpc.go`;
   epoch schema in a paired migration.
6. **MEDIUM** — counter timeouts as real tasks/tests; bind INV-WORLD-1 to an always-run test; define the
   census on an explicit command construct.
