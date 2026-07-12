---
phase: 5
reviewers: [codex, antigravity]
reviewed_at: 2026-07-12T01:33:52Z
plans_reviewed: [05-01-PLAN.md, 05-02-PLAN.md, 05-03-PLAN.md, 05-04-PLAN.md, 05-05-PLAN.md, 05-06-PLAN.md, 05-07-PLAN.md, 05-08-PLAN.md, 05-09-PLAN.md, 05-10-PLAN.md, 05-11-PLAN.md, 05-12-PLAN.md, 05-13-PLAN.md]
---

# Cross-AI Plan Review — Phase 5: World-Model Integrity Fixes (M2 / M12)

Two external AI CLIs independently reviewed the 13 plans against the live working tree.
Codex (codex-cli 0.144.1) produced a deeply source-grounded review with `file:line` citations
and a **HIGH** risk verdict. Antigravity (agy 1.1.1) produced an operational/performance-focused
review with a "structurally sound" verdict — but see the reliability caveat in its section.

---

## Codex Review

## Summary

The plan set has strong requirements coverage and preserves the required slice dependencies, but it is not execution-ready. Several source-grounded blockers would either prevent compilation or silently violate MODEL-04: sibling-package code cannot access `execerFromCtx`; multiple repositories own nested transactions or call `pool.Exec` directly; scene writes are never made transaction-aware; the proposed depguard rule cannot detect SQL; and the named invariants cannot be recognized by the current numeric-only registry parser. Overall, the plans understand the target architecture, but the transaction/writer-boundary implementation needs a concrete redesign before execution.

## Strengths

- The plans correctly identify the central transaction-enrollment defect. `execerFromCtx` selects an ambient `pgx.Tx` when present ([internal/world/postgres/helpers.go:41](internal/world/postgres/helpers.go#L41)), while current location, object, character, and exit updates bypass it with direct pool calls, for example [location_repo.go:72](internal/world/postgres/location_repo.go#L72), [object_repo.go:78](internal/world/postgres/object_repo.go#L78), [character_repo.go:63](internal/world/postgres/character_repo.go#L63), and [exit_repo.go:141](internal/world/postgres/exit_repo.go#L141).

- The version-guard scope matches the source model: the four repositories expose unguarded update/delete surfaces, and the read queries currently omit versions, such as [location_repo.go:29](internal/world/postgres/location_repo.go#L29) and [character_repo.go:29](internal/world/postgres/character_repo.go#L29). Splitting these across parallel location/exit and character/object plans is sensible.

- The plans correctly target the empirical M2 path. `MoveCharacter` updates PostgreSQL first at [service.go:802](internal/world/service.go#L802) and emits afterward at [service.go:841](internal/world/service.go#L841), explicitly returning `move_succeeded=true` on emission failure. Deleting this path in the same wave as rewriting the integration harness is appropriate.

- Relay requirements accurately preserve the normative distinction from the audit projection. The existing audit consumer captures and terminates poison messages at [projection.go:254](internal/eventbus/audit/projection.go#L254); the plans explicitly require the world feed to halt rather than copy that continue-after-DLQ behavior.

- The phase dependency chain is mechanically sound: every plan depends on the preceding slice/wave, and the two wave-2 repository plans converge before RMW threading.

- The documentation inventory is well grounded. The false claims are present at [CLAUDE.md:274](CLAUDE.md#L274), [coding-standards.md:344](site/src/content/docs/contributing/reference/coding-standards.md#L344), and [architecture.md:305](site/src/content/docs/contributing/explanation/architecture.md#L305). The plan also correctly distinguishes those from genuine client catch-up at [architecture.md:74](site/src/content/docs/contributing/explanation/architecture.md#L74).

## Concerns

- **HIGH — The proposed `mutate()` helper is not a compile-time write fence.** `world.Service` will still hold repositories with public write methods ([service.go:45](internal/world/service.go#L45)), and its methods can continue calling them directly, as they do at [service.go:222](internal/world/service.go#L222), [service.go:490](internal/world/service.go#L490), and [service.go:714](internal/world/service.go#L714). Adding a helper whose envelope argument is mandatory only makes calls to that helper type-safe; it does not make direct repository calls fail compilation. Plan 05-06 also prohibits all direct service writes while only migrating `MoveCharacter`, contradicting the later rollout plans.

- **HIGH — The outbox package cannot use the transaction mechanism described by the plans.** Both `execer` and `execerFromCtx` are unexported members of package `internal/world/postgres` ([helpers.go:23](internal/world/postgres/helpers.go#L23), [helpers.go:42](internal/world/postgres/helpers.go#L42)). A sibling package at `internal/world/outbox` cannot call them. Plans 05-05 and 05-06 therefore cannot implement their stated same-transaction contract without first defining an exported transaction-scoped abstraction or moving persistence into `internal/world/postgres`.

- **HIGH — Repository-owned transactions will escape or nest inside the proposed mutation transaction.** `ExitRepository.Create` opens and commits its own transaction ([exit_repo.go:59](internal/world/postgres/exit_repo.go#L59), [exit_repo.go:89](internal/world/postgres/exit_repo.go#L89)); exit deletion does the same at [exit_repo.go:176](internal/world/postgres/exit_repo.go#L176). `ObjectRepository.Move` also opens its own transaction at [object_repo.go:165](internal/world/postgres/object_repo.go#L165). `Transactor.InTransaction` is not re-entrant; it always calls `pool.Begin` ([transactor.go:27](internal/world/postgres/transactor.go#L27)). Merely replacing some `Exec` calls with `execerFromCtx` does not solve the internal `Begin`/`Commit` paths.

- **HIGH — Scene-participant emission cannot be atomic under Plan 05-11.** Both scene writes use `r.pool.Exec` directly ([scene_repo.go:40](internal/world/postgres/scene_repo.go#L40), [scene_repo.go:57](internal/world/postgres/scene_repo.go#L57)), but `scene_repo.go` is absent from Plan 05-11’s files and actions. Wrapping `AddSceneParticipant` in `mutate()` would therefore commit the scene row outside the outbox transaction.

- **HIGH — Delete CAS is underspecified and incompatible with existing interfaces.** Repository deletion currently accepts only an ID ([repository.go:32](internal/world/repository.go#L32), [repository.go:75](internal/world/repository.go#L75), [repository.go:104](internal/world/repository.go#L104), [repository.go:153](internal/world/repository.go#L153)). Supplying `expectedVersion` requires changing these interfaces, every caller, and generated mocks, but Plans 05-02/03 do not list `repository.go` or `internal/world/worldtest/mock_*Repository.go`. More importantly, a post-delete zero-row read finding no row cannot distinguish “never existed” from “concurrently deleted” unless the transaction carries prior existence/version evidence. The plans request all three classifications without specifying that evidence.

- **HIGH — Delta-parity for cascades cannot be derived through current repository APIs.** Bidirectional exit creation generates the reverse exit ID inside the repository ([exit_repo.go:73](internal/world/postgres/exit_repo.go#L73)); deletion and object containment logic are likewise repository-internal. Service methods receive only `error`, so they cannot construct a manifest proven to equal every affected row. Plans 05-10/11 need a mutation-result/delta contract, not payload construction based only on command inputs.

- **HIGH — The proposed SQL fence cannot be implemented with depguard.** The configured depguard rules match imported Go packages ([.golangci.yaml:136](.golangci.yaml#L136)); the existing meta-test merely asserts package strings remain in configuration ([depguard_config_test.go:18](test/meta/depguard_config_test.go#L18)). Depguard cannot inspect SQL literals or table names. There is also an internal contradiction: Plans 05-05/07 place outbox `INSERT`/`SELECT`/`UPDATE` SQL in `internal/world/outbox`, while Plan 05-09 says all outbox SQL must live under `internal/world/postgres`.

- **HIGH — The four locked invariant names are incompatible with current tooling.** Registry binding annotations are parsed only if they match `INV-<SCOPE>-<number>` ([invariant_registry_test.go:162](test/meta/invariant_registry_test.go#L162)). Names such as `INV-WORLD-ATOMIC-FEED` will not be discovered, so the proposed `// Verifies:` annotations cannot bind them. Plan 05-12 does not include the registry parser/tests among its modified files.

- **HIGH — Relay lifecycle wiring is incomplete.** `WorldSubsystem` currently depends only on database and ABAC ([subsystem.go:53](internal/world/setup/subsystem.go#L53)), and its config has no event-bus provider ([subsystem.go:31](internal/world/setup/subsystem.go#L31)). Production subsystem construction happens in [cmd/holomush/core.go:403](cmd/holomush/core.go#L403) and the registered set in [core.go:1455](cmd/holomush/core.go#L1455), but Plan 05-07 modifies neither. A separately identified relay subsystem cannot become live merely by changing `internal/world/setup/subsystem.go`.

- **HIGH — The lease and wake-up mechanisms are assertions, not planned mechanisms.** Migration 000050 includes no lease state, and Plan 05-07 does not choose a concrete alternative such as a session-level PostgreSQL advisory lock held on a dedicated connection. Likewise, `LISTEN` requires a long-lived connection and the writer must issue `NOTIFY`; neither connection ownership nor notification emission is assigned to a concrete task. Without fencing/lifetime semantics, a dual-relay test may pass while split-brain publication remains possible during reconnects.

- **MEDIUM — CreateCharacter retains a non-transactional escape hatch.** The actual handler delegates to `createCharacterAtomic`, which deliberately falls back to plain creation when its transaction dependencies are missing ([auth_handlers.go:517](internal/grpc/auth_handlers.go#L517), [auth_handlers.go:521](internal/grpc/auth_handlers.go#L521)). Plan 05-11 does not explicitly remove or fail-close this fallback, so character creation could succeed without its genesis envelope.

- **MEDIUM — The census source is incomplete.** `world.Mutator` exposes only a subset of write operations ([mutator.go:34](internal/world/mutator.go#L34)); deletes, moves, scene membership, character updates, and `CreateCharacter` are absent. A hard-coded census derived from that interface will not automatically detect all new service methods. The test must inspect actual declarations with `go/types`/AST or first make one interface the authoritative complete command surface.

- **MEDIUM — Slice-3 ordering diverges from the normative sequence.** The one-pager requires taxonomy, census, and invariant registration before the mechanical rollout ([model-01-consensus-onepager.md:41](docs/reviews/arch-review/2026-07-11/verification/model-01-consensus-onepager.md#L41)). The plans introduce rollout in 05-10, finish it in 05-11, and register invariants only in 05-12. The “data-first, enforcement-last” rationale is understandable, but it changes a locked sequencing requirement.

- **MEDIUM — In-memory version state is not maintained after creates or updates.** Current creates use plain `INSERT` without `RETURNING` ([location_repo.go:53](internal/world/postgres/location_repo.go#L53), [character_repo.go:48](internal/world/postgres/character_repo.go#L48)); updates use `Exec`, not `RETURNING`. Adding a `Version` field but leaving newly created or successfully updated structs at version 0/old-version makes subsequent reuse conflict incorrectly. Plans should return and assign the committed version.

- **LOW — README line 22 is likely legitimate replay language.** It says “session persistence and event replay” ([README.md:22](README.md#L22)), which appears closer to the real reconnect/catch-up behavior than the false world-state reconstruction claim. The doc plan should not mechanically downgrade this line.

## Suggestions

- Add a transaction-foundation plan before repository CAS/outbox work:

  - Define an exported transaction-scoped executor/unit-of-work abstraction.
  - Make transaction entry re-entrant or prohibit nested transaction ownership.
  - Refactor exit create/delete and object move to operate on the ambient executor.
  - Place outbox persistence in `internal/world/postgres`, leaving `internal/world/outbox` for domain envelope and relay logic.

- Redesign repository write APIs before implementing guards:

  - `Delete(ctx, id, expectedVersion)`.
  - Move methods receive expected version.
  - Create/update/delete/move return a `MutationDelta` containing primary and affected aggregates with committed before/after versions.
  - Update `repository.go`, adapters, all callers, and regenerate the mockery outputs.

- Make the compile-time fence real. Give `Service` only read repositories plus a mutation executor whose sole write method requires an envelope and typed state-change closure. Do not retain directly callable write repositories on `Service`.

- Replace the depguard proposal with a real static analyzer or AST/token-based meta-test that scans production Go files for world-table mutation SQL and enforces an explicit repository-directory allowlist.

- Specify the relay operational model:

  - Dedicated PostgreSQL connection holding a session advisory lock keyed by game.
  - Fencing/reacquisition behavior after connection loss.
  - Dedicated `LISTEN` connection and transaction-side `pg_notify`.
  - Periodic sweep interval/backoff.
  - Graceful cancellation and PubAck/mark-published failure handling.
  - Composition-root changes in `cmd/holomush/core.go` and subsystem ordering tests.

- Extend invariant tooling to accept the locked symbolic IDs, including parser regression tests and genuine-binding checks. Alternatively, obtain an explicit decision permitting canonical numeric registry IDs with the named invariants recorded as aliases; do not silently rename them.

- Make the census structural: derive commands from one authoritative interface or inspect `world.Service`/other declared command entry points with `go/types`, then compare that derived set to taxonomy registrations.

- Remove the `CreateCharacter` non-transactional fallback for production configuration and include the auth service/adapter path in the plan, not only the gRPC handler.

- Rework slice 3 so taxonomy, enforcement, and rollout land without a knowingly incomplete intermediate boundary—potentially one combined wave/commit—while preserving the normative ordering.

## Risk Assessment

**HIGH.** The principal risks are correctness failures, not schedule or style concerns. As written, some paths cannot compile (`execerFromCtx` visibility and omitted interface/mock changes), while others can compile but commit state outside the outbox transaction (scene writes and repository-owned nested transactions). The current invariant and lint proposals also cannot enforce the guarantees they claim. Addressing these issues requires revising the plan architecture and file ownership before implementation begins.

---

## Antigravity Review

> **Reliability caveat (orchestrator note):** agy `-p` emitted only a closing summary to stdout;
> the full report below was recovered from its transcript/brain disk. More importantly, agy
> operated on a **sandboxed scratch copy** of the repo and falsely reported `05-13-PLAN.md` as
> "missing" and `05-12-PLAN.md` as "truncated" — **both exist and are complete in the repo**. Its
> drafted replacement 05-12/05-13 specs (§3 below) are therefore redundant and should be IGNORED.
> Its genuine value is the four operational/performance risks in §2, which are grounded in the
> locked mechanism (counter lock, connection reuse, relay halt-recovery, examine-event consumers).
> Because agy never had the real plan set, weight its optimistic "structurally sound" verdict
> below Codex's source-grounded HIGH verdict.

# HoloMUSH Phase 5 Plan Review: World-Model Integrity Fixes (M2 / M12)

This document provides a structured, professional review of the 13 implementation plans for **HoloMUSH Phase 5: World-Model Integrity Fixes (M2 / M12)**. 

---

## Executive Summary

The proposed 13-plan roadmap is **highly cohesive, structurally sound, and technically rigorous**. It strictly follows the normative design decisions of **Option B** (CRUD-canonical world state with optimistic concurrency version guards and a transactional outbox) as ratified in the consensus one-pager. 

The sequence of slices cleanly separates concern zones, minimizing the blast radius of breaking changes:
1. **Slice 1 (05-01 to 05-04)** establishes the database schema versioning and threads it through all RMW callers, closing the Last-Write-Wins (LWW) vulnerability (#4798).
2. **Slice 2 (05-05 to 05-08)** constructs the transactional outbox, deletes the problematic post-commit emit path, and implements the relay and its resilience harness, closing the M2 dual-write window (#4784).
3. **Slice 3 (05-09 to 05-11)** implements schema taxonomy governance and performs the mechanical write command rollout.
4. **Slice 4 (05-12 to 05-13)** ratifies system invariants and corrects outdated architecture documentation.

While the overall plan quality is excellent, this review identifies several critical operational risks, connection pool starvation edge cases, and missing documentation steps, along with concrete mitigations and detailed drafts to complete the truncated and missing plans.

---

## 1. Slice-by-Slice Quality & Completeness Analysis

### Slice 1: Version Guard (MODEL-03)
*   **05-01-PLAN.md (Version Guard Foundation):** Appropriately scopes migration `000049` to the four world tables (locations, exits, characters, objects), leaving child tables like `entity_properties` out, which aligns with the one-pager. Struct-level transport fields and error code definitions are correctly placed.
*   **05-02-PLAN.md & 05-03-PLAN.md (Repo CAS rewrites):** The decision to rewrite all repo writes from `pool.Exec` to `execerFromCtx` is a vital prerequisite for Slice 2's outbox transaction enrollment. 
*   **05-04-PLAN.md (RMW Version Threading):** This is a key completeness gate. Threading the version through `entity_mutator.go` (describe/rename actions) and `UpdateCharacterDescription` ensures the optimistic concurrency guard actually intercepts the LWW window.

### Slice 2: Outbox + Relay + MoveCharacter (MODEL-04, WR-01)
*   **05-05-PLAN.md (Outbox schema & Writer):** The outbox table design uses `event_id` (ULID) for dedup and a UNIQUE `(game_id, feed_position)` constraint for gap-free ordering. Sourcing `feed_position` from a locked per-game counter inside the transaction prevents out-of-order commits.
*   **05-06-PLAN.md (mutate seam & Emit Path Deletion):** resizes the `Mutator` interface to introduce compile-time enforcement of envelopes. Deleting `events.go` and `event_store_adapter.go` successfully eliminates the post-commit dual-write window.
*   **05-07-PLAN.md (Single Leased Relay):** Wires the relay as a `lifecycle.Subsystem`. Alerts are rightfully introduced in this wave; since an ordered feed halts on poison, silent failure is a major risk.
*   **05-08-PLAN.md (Fault-Injection & Resilience):** Leverages testcontainers to execute broker pause/resume and relay crashes around `PubAck`, verifying that the consumer handles duplicate deliveries gracefully via `Nats-Msg-Id` deduplication.

### Slice 3: Taxonomy + Census + Rollout (MODEL-04)
*   **05-09-PLAN.md (Taxonomy Registry & SQL Lint Fence):** Defines the envelope kind registry. The depguard lint fence prevents raw database write leakage.
*   **05-10-PLAN.md & 05-11-PLAN.md (Rollout, CreateCharacter, Genesis):** Wires the remaining ~14 mutate calls. The census meta-test ensures no new mutating command escapes without declaring an envelope kind.

### Slice 4: Invariants & Doc Correction
*   **05-12-PLAN.md (Invariants binding):** Binds the 4 new invariants (`INV-WORLD-ATOMIC-FEED`, `INV-WORLD-DELTA-PARITY`, `INV-WORLD-FEED-ORDER`, `INV-WORLD-WRITER-BOUNDARY`).
*   **05-13-PLAN.md (Doc correction):** Replaces false event-sourcing claims with "event-driven with an append-only audit log."

---

## 2. Key Technical Risks & Mitigations

### ⚠️ Risk 1: Database Lock Contention on `world_feed_counter`
Because the transactional outbox uses a locked per-game counter (`SELECT next_position FROM world_feed_counter WHERE game_id=$1 FOR UPDATE`) inside the mutation transaction, **all world mutations within the same game are serialized**. While this is acceptable for typical MUSH write rates, a long-running transaction (e.g., deleting a location that triggers cascading deletes of multiple child exits, characters, and properties) will lock the counter row, blocking all other concurrent player actions.

*   **Mitigation Strategy:** 
    1. **Decommit Counter Lock Early:** The lock on `world_feed_counter` must be acquired as late as possible in the Go transaction block. Envelopes should be buffered in a transaction-context slice and flushed to the database (which triggers the counter lock and outbox insert) right before the transaction commits.
    2. **Transaction Timeouts:** Enforce strict statement and transaction timeouts (e.g., 250ms–500ms) on all mutation operations to prevent stuck locks from causing connection pool exhaustion.

### ⚠️ Risk 2: Broken Client Side-Effects from Deleting Examine Events
The plans delete `internal/world/events.go`, which contains `EmitExamineEvent`. The justification is that examine commands are reads, not state modifications, and should be excluded from the outbox transactional feed. However, if other systems (such as logging, security auditing, room description updates, or plugin triggers) rely on `EmitExamineEvent` to know when a player is actively looking at something, this functionality will break.

*   **Mitigation Strategy:** 
    *   Perform a dependency analysis to check if any plugins or PWA event handlers listen to `events.*.examine.*` subjects. 
    *   If they do, reintroduce `EmitExamineEvent` as a lightweight, non-durable, post-commit broadcast via NATS that bypasses the outbox and is completely decoupled from the transaction.

### ⚠️ Risk 3: Lack of Relay Recovery / Skip Mechanism (Infinite Halt)
The relay is designed with a strict **halt-and-alert** posture on poison (unpublishable) envelopes to satisfy `INV-WORLD-FEED-ORDER` (no gaps in feed position). If an envelope is corrupted (e.g., invalid JSON, unsupported schema version, database field truncation), the relay will halt permanently. Without a recovery procedure, the real-time sync for the entire game remains dead until manual SQL intervention occurs.

*   **Mitigation Strategy:** 
    *   Provide an admin command or a secure CLI tool (e.g., `agy admin outbox skip <position>`) that updates the outbox table to set `published_at = NOW()` and logs a warning with the poison payload. This allows operators to clear the blockage safely.
    *   The relay should export structured health metrics showing the exact position of the halt and the error details.

### ⚠️ Risk 4: Database Connection Pool Exhaustion on Zero-Row Classifier
In `05-02-PLAN.md`, if `Update` or `Delete` returns 0 rows, a follow-up `SELECT version FROM locations WHERE id=$1 FOR UPDATE` is executed to classify the failure. If the follow-up read borrows a new connection from the pool, it can cause immediate deadlocks when concurrent writers exhaust the connection pool.

*   **Mitigation Strategy:** 
    *   The repository methods must execute the classifier using the same connection context returned by `execerFromCtx(ctx, r.pool)`.
    *   Write a unit test with pool sizing constrained to 1 to guarantee that zero-row classification does not deadlock on connection acquisition.

---

## 3. Completing Truncated & Missing Plans

### 05-12-PLAN.md (Completion)
*Plan 12 was truncated at the `prohibitions` block. The completed portion is provided below.*

```markdown
  prohibitions:
    - "No INV-WORLD-* may be left binding: pending at phase end (ROADMAP success criterion 4)."
    - "No // Verifies: annotation may bind a Skip-only or presence-only test — delta-parity MUST prove the manifest matches the delta, not merely that a manifest is present."
    - "Do not manually edit the generated regions of invariants.md — always run go run ./cmd/inv-render."

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: Register the 4 Invariants in invariants.yaml</name>
  <files>docs/architecture/invariants.yaml</files>
  <read_first>
    - docs/architecture/invariants.yaml (inspect existing boundaries and formatting style)
    - .claude/rules/invariants.md (ratchet rule: mint new boundary WORLD, add 4 entries with binding: bound)
  </read_first>
  <action>
    Add the `WORLD` boundary scope to `docs/architecture/invariants.yaml` and register the 4 invariants:
    1. `INV-WORLD-ATOMIC-FEED`: State mutations and their corresponding outbox envelope must commit in the same transaction.
    2. `INV-WORLD-DELTA-PARITY`: The outbox manifest before/after version metadata must reflect the actual row version transitions.
    3. `INV-WORLD-FEED-ORDER`: The relay must process outbox events in strict, gap-free feed_position order; poison events must halt the feed.
    4. `INV-WORLD-WRITER-BOUNDARY`: All writes to world tables must occur through the mutation wrapper.
  </action>
  <verify>
    <automated>grep -q 'WORLD' docs/architecture/invariants.yaml</automated>
  </verify>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Bind Invariants to Genuinely-Asserting Tests</name>
  <files>
    - internal/world/outbox/delta_parity_test.go
    - internal/world/outbox/writer_boundary_test.go
    - internal/world/outbox/relay_test.go
    - test/integration/resilience/m2_dualwrite_test.go
  </files>
  <action>
    Add the `// Verifies: INV-WORLD-*` annotations to the respective tests:
    - `INV-WORLD-ATOMIC-FEED` verified by `m2_dualwrite_test.go` (injecting mid-transaction faults).
    - `INV-WORLD-DELTA-PARITY` verified by `delta_parity_test.go` (verifying manifest versions).
    - `INV-WORLD-FEED-ORDER` verified by `relay_test.go` (asserting strict ordering and poison halts).
    - `INV-WORLD-WRITER-BOUNDARY` verified by `writer_boundary_test.go` (verifying SQL access constraints).
  </action>
  <verify>
    <automated>go run ./cmd/inv-render && git diff --exit-code docs/architecture/invariants.md</automated>
  </verify>
</task>

</tasks>
```

---

### 05-13-PLAN.md (Full Specification)
*Slice 4 — Doc correction (MODEL-02). This plan was completely missing from the user request.*

```markdown
---
phase: 05-world-model-integrity-fixes-m2-m12
plan: 13
type: execute
wave: 12
depends_on: [05-12]
files_modified:
  - CLAUDE.md
  - README.md
  - site/src/content/docs/contributing/reference/coding-standards.md
  - site/src/content/docs/contributing/explanation/architecture.md
  - test/meta/documentation_meta_test.go
autonomous: true
requirements: [MODEL-02]
must_haves:
  truths:
    - "No documentation files claim that the world state is derived from event replay."
    - "A meta-test asserts that no documentation files contain the forbidden phrasing 'state derives from replay' or 'derived from event replay'."
  artifacts:
    - path: test/meta/documentation_meta_test.go
      provides: "meta-test checking doc sites for stale event-sourcing claims"
      contains: "replay"
---

<objective>
Downgrade the false event-sourcing claims in the documentation (MODEL-02, slice 4). Multiple files state that the world state is derived from replay. Since the architecture is actually CRUD-canonical with an transactional outbox feed, these doc sites must be updated to state "event-driven with an append-only audit log" while retaining correct descriptions of reconnecting clients catching up via subscription streams.

Purpose: Correct stale documentation to prevent developer confusion and align codebase guidelines with the Option B architecture.
Output: Corrected docs across 4 key files and a regression-prevention meta-test.
</objective>

<tasks>

<task type="auto">
  <name>Task 1: Correct stale documentation files</name>
  <files>
    - CLAUDE.md
    - README.md
    - site/src/content/docs/contributing/reference/coding-standards.md
    - site/src/content/docs/contributing/explanation/architecture.md
  </files>
  <action>
    Search for all occurrences of 'event-sourced', 'state derives from replay', and 'derived from event replay' inside the repository documentation. Update these files to reflect the option B architecture:
    - Replace 'state derives from replay' statements with 'the world state is CRUD-canonical, backed by version columns and a transactional outbox feed for event delivery.'
    - Ensure client catch-up text ('Reconnecting clients catch up from their last seen event') remains intact as that is functionally correct.
  </action>
  <verify>
    <automated>! grep -rn "state derives from replay" CLAUDE.md README.md site/src/content/docs/</automated>
  </verify>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Implement documentation regression meta-test</name>
  <files>test/meta/documentation_meta_test.go</files>
  <action>
    Write a Go meta-test `documentation_meta_test.go` that reads the contents of the documentation directory and explicitly checks for the absence of strings like 'state derives from replay' and 'derived from event replay' to prevent future PRs from re-introducing stale architectural statements.
  </action>
  <verify>
    <automated>task test -- ./test/meta/</automated>
  </verify>
</task>

</tasks>
```

---

## 4. Key Invariant Bindings Map

To ensure the invariants are not left in a `pending` state, the following tests must be verified to contain their respective verification annotations:

| Invariant ID | Target Test File | Assertion Logic |
| :--- | :--- | :--- |
| **INV-WORLD-ATOMIC-FEED** | [m2_dualwrite_test.go](file:///Users/sean/.gemini/antigravity-cli/scratch/test/integration/resilience/m2_dualwrite_test.go) | Asserts that database rollbacks of entity mutations also roll back outbox rows, and vice-versa, leaving no orphans. |
| **INV-WORLD-DELTA-PARITY** | [delta_parity_test.go](file:///Users/sean/.gemini/antigravity-cli/scratch/internal/world/outbox/delta_parity_test.go) | Asserts that `before_version` and `after_version` fields in the outbox JSONB manifest exactly match the state changes in the database. |
| **INV-WORLD-FEED-ORDER** | [relay_test.go](file:///Users/sean/.gemini/antigravity-cli/scratch/internal/world/outbox/relay_test.go) | Asserts that the relay halts on a poison event and raises the halt metric without skipping or duplicate publishes. |
| **INV-WORLD-WRITER-BOUNDARY** | [writer_boundary_test.go](file:///Users/sean/.gemini/antigravity-cli/scratch/internal/world/outbox/writer_boundary_test.go) | Combines with the depguard linter config to assert that no raw SQL writes occur outside `internal/world/postgres`. |

---

## Consensus Summary

**Net verdict: the plans are well-organized and cover the right scope, but they are NOT
execution-ready as written.** Codex's source-grounded review found a cluster of real
blockers — several are compile-breakers or silent atomicity violations that the in-context
plan-checker could not see because they live in package visibility, interface signatures, and
tooling regexes rather than in plan text. Antigravity independently corroborated the
transaction/connection concern from a performance angle and added operational risks. Recommend a
`/gsd-plan-phase 5 --reviews` replan before executing.

Weighting note: Codex's verdict is source-grounded (`file:line` citations verified against the
tree, matching what the phase researcher/pattern-mapper independently found — e.g. only `Delete`
uses `execerFromCtx` at `helpers.go:42`). Antigravity's structural-completeness assessment is
unreliable (it worked on a stale sandbox and misreported which plans exist), so its "sound"
verdict is discounted; its operational risks are kept.

### Agreed Strengths (2+ reviewers)

- **Slice dependency ordering is correct.** Both confirm the 1→2→3→4 chain is enforced as hard
  `depends_on`, with the two wave-2 repo plans converging before RMW threading.
- **Version-guard scope is right.** Migration `000049` covers the 4 world tables and correctly
  excludes child tables like `entity_properties`; the four repos' unguarded update/delete
  surfaces are the correct targets.
- **`execerFromCtx`-as-prerequisite correctly identified.** Both flag switching repo writes off
  `pool.Exec` onto the tx-aware execer as the load-bearing enrollment step.
- **Relay halt-not-DLQ distinction preserved.** Both confirm the plans correctly require the world
  feed to HALT (unlike `audit/projection.go`'s continue-after-DLQ at `projection.go:254`).
- **M2 path correctly targeted.** Deleting the post-commit emit path (`MoveCharacter` emit at
  `service.go:841`) in the same wave as rewriting the integration harness is sound.

### Agreed Concerns (2+ reviewers — HIGHEST PRIORITY)

1. **The transaction / connection model is underspecified and currently unimplementable as
   drawn.** Codex: the outbox package can't reach the unexported `execerFromCtx`
   (`helpers.go:42`), and `ExitRepository.Create`/`ObjectRepository.Move` open their own
   non-reentrant transactions (`exit_repo.go:59`, `object_repo.go:165`; `Transactor.InTransaction`
   always `pool.Begin` at `transactor.go:27`) that would escape or nest inside the mutation tx.
   Antigravity: the zero-row classifier follow-up `SELECT ... FOR UPDATE` must reuse the SAME
   connection or it deadlocks under a constrained pool; and the locked per-game counter serializes
   all same-game writes (long cascading deletes block every other player action). **Together these
   demand a concrete, exported unit-of-work / transaction-scoped executor abstraction, decided
   before repo CAS and outbox work — likely a new "transaction foundation" plan ahead of the
   current slice 2.**

2. **The relay operational model is asserted, not planned.** Codex: lease + LISTEN/NOTIFY are named
   but no concrete mechanism (advisory lock, dedicated connection ownership, `pg_notify` emitter)
   is assigned to a task, and `cmd/holomush/core.go` composition (`core.go:403/1455`) +
   `WorldSubsystem`'s missing event-bus provider (`subsystem.go:31/53`) are not modified by 05-07.
   Antigravity: there is no halt-recovery/skip path, so a single poison envelope kills the feed
   until manual SQL. **Specify the concrete lease/wakeup/composition mechanism AND an operator
   skip/recovery affordance + halt health metrics.**

### Codex-only findings (single reviewer, but source-grounded — treat as actionable)

- **HIGH — Locked invariant names are incompatible with the registry parser.** The tooling only
  discovers `INV-<SCOPE>-<number>` (`invariant_registry_test.go:162`); `INV-WORLD-ATOMIC-FEED`
  etc. will never bind, so 05-12's `// Verifies:` annotations silently fail — directly defeating
  **success criterion 4**. 05-12 does not modify the parser/tests. *Requires either extending the
  parser to accept symbolic IDs (+ regression test) or an explicit decision to use numeric IDs
  with the named ones as aliases — do NOT silently rename the locked names.*
- **HIGH — `mutate()` is not a real compile-time fence.** `Service` retains public write repos
  (`service.go:45`) and can still call them directly (`service.go:222/490/714`); a mandatory
  envelope arg only makes calls to the helper type-safe. 05-06 also prohibits all direct service
  writes while migrating only `MoveCharacter`, contradicting the later rollout plans.
- **HIGH — `scene_repo.go` is absent from 05-11.** Scene writes use `pool.Exec` directly
  (`scene_repo.go:40/57`); wrapping `AddSceneParticipant` in `mutate()` would commit the scene row
  outside the outbox tx.
- **HIGH — Delete CAS interface change is unlisted.** `repository.go` `Delete` takes only an ID
  (`repository.go:32/75/104/153`); adding `expectedVersion` touches the interface, every caller,
  and generated mocks (`worldtest/mock_*Repository.go`) — none listed in 05-02/03. Zero-row post-
  delete also can't distinguish never-existed vs concurrently-deleted without prior version
  evidence carried in the tx.
- **HIGH — depguard cannot enforce the SQL fence.** depguard matches imported packages
  (`.golangci.yaml:136`), not SQL literals. Plus an internal contradiction: 05-05/07 place outbox
  SQL in `internal/world/outbox` while 05-09 requires all outbox SQL under `internal/world/postgres`.
  *Needs a real AST/token analyzer, and the package-location contradiction resolved.*
- **HIGH — Delta-parity manifests can't be derived from current repo APIs** (cascade IDs are
  repository-internal; service methods receive only `error`). 05-10/11 need a mutation-result/delta
  contract, not payload-from-command-inputs.
- **MEDIUM — `CreateCharacter` keeps a non-transactional fallback** (`createCharacterAtomic`
  falls back to plain creation, `auth_handlers.go:517/521`) — genesis envelope could be skipped.
- **MEDIUM — census source (`world.Mutator`, `mutator.go:34`) is an incomplete subset** — a
  hard-coded census won't auto-detect all write methods; use `go/types`/AST over the real command
  surface.
- **MEDIUM — slice-3 ordering diverges from the normative sequence.** The one-pager requires
  taxonomy/census/invariants BEFORE the mechanical rollout (`model-01-consensus-onepager.md:41`);
  the plans do rollout (05-10/11) then invariants (05-12). The "data-first, enforcement-last"
  rationale changes a *locked* sequencing requirement — needs explicit acknowledgement or reorder.
- **MEDIUM — version not returned after create/update** (no `RETURNING`; `location_repo.go:53`,
  `character_repo.go:48`) leaves reused structs at stale version → spurious later conflicts.
- **LOW — Do not over-downgrade `README.md:22`** ("session persistence and event replay") — this
  is legitimate reconnect/catch-up language, not the false world-state-reconstruction claim.

### Antigravity-only findings (operational/performance)

- **Counter-lock contention** — long cascading-delete transactions hold `world_feed_counter FOR
  UPDATE` and serialize all same-game writes; acquire the counter lock as late as possible (buffer
  envelopes, flush just before commit) + enforce statement/transaction timeouts.
- **Examine-event consumer dependency** — before deleting `EmitExamineEvent`, verify no plugin /
  PWA handler subscribes to `events.*.examine.*`; if any does, reintroduce as a non-durable
  post-commit broadcast that bypasses the outbox. (The plans already decided examine = read/dropped;
  this adds the consumer-audit safety step.)
- **Zero-row classifier connection reuse** — the follow-up classifier SELECT must run on the
  execer-provided connection; add a pool-size-1 deadlock test.
- **Relay skip/recovery affordance** — an admin skip command + halt-position health metrics so a
  poison envelope doesn't require raw SQL to clear.

### Divergent Views

- **Overall risk: Codex HIGH vs Antigravity ~LOW/MEDIUM.** Codex judged the plans not execution-
  ready (structural blockers); Antigravity judged them "structurally sound, technically rigorous"
  with operational caveats. **Resolution:** Codex is the more reliable signal here — its findings
  are source-grounded and match the researcher/pattern-mapper's independent code reading, whereas
  Antigravity demonstrably never had the full plan set (misreported 05-12/05-13). Treat the
  overall risk as **HIGH — replan before execute.**
- **05-12/05-13 "incomplete":** Antigravity claimed these were truncated/missing and drafted
  replacements. This is FALSE — both plans exist and are complete. Discard agy's drafted specs.

### Recommended next step

`/gsd-plan-phase 5 --reviews` — replan incorporating this feedback. The replan should prioritize
the two Agreed Concerns (transaction-foundation abstraction; concrete relay operational model) and
the Codex-only HIGH findings (invariant-name↔parser incompatibility, the real compile-time write
fence, scene_repo coverage, Delete-CAS interface + mocks, the SQL-fence mechanism + package-location
contradiction, delta-parity contract). The MEDIUM/LOW items and Antigravity's operational risks can
be folded into the same replan.
