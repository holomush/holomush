<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Full ABAC Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the static role-based `AccessControl` system with a full policy-driven `AccessPolicyEngine` supporting a Cedar-inspired DSL, extensible attribute providers, audit logging, and admin commands.

**Architecture:** Custom Go-native ABAC engine with eager attribute resolution, in-memory policy cache invalidated via PostgreSQL LISTEN/NOTIFY, deny-overrides conflict resolution, and per-request attribute caching. No adapter layer — direct replacement of all **28 production call sites** (24 in `internal/world/service.go`, 3 in `internal/command/`, 1 in `internal/plugin/hostfunc/commands.go`) plus **57 test call sites** in `internal/access/static_test.go` and generated mocks, plus **~20 test call sites** in `internal/plugin/capability/enforcer_test.go` (addressed in Task 29 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md))).

**Tech Stack:** Go 1.24+, [participle](https://github.com/alecthomas/participle) (struct-tag parser generator), pgx/pgxpool, oops (structured errors), prometheus/client_golang, testify + Ginkgo/Gomega, mockery

---

## Task Execution Protocol

Every task in this plan MUST follow these requirements:

### TDD (Test-Driven Development)

| Step | Description                                                         |
| ---- | ------------------------------------------------------------------- |
| 1    | Write failing test(s) first — tests MUST fail before implementation |
| 2    | Verify the test fails (run `task test`)                             |
| 3    | Write minimal implementation to make the test pass                  |
| 4    | Verify the test passes (run `task test`)                            |
| 5    | Refactor if needed (tests still pass)                               |
| 6    | Commit                                                              |

SQL migration tasks (Tasks 1-3) are exempt from red-green-refactor but MUST have integration test coverage before the phase is considered complete.

### Spec & ADR Traceability

Each task MUST denote which spec sections and ADRs it implements. This is tracked via the **Spec References** field on each task. The implementer MUST verify their work aligns with the referenced spec sections before requesting review.

**Design spec:** `docs/specs/2026-02-05-full-abac-design.md` (index file linking to section files in `docs/specs/abac/`)

> **Note:** Spec references use section file names and anchors (e.g., `01-core-types.md#policycompiler`). Legacy line numbers from the monolithic spec are preserved in parentheses for traceability.

Applicable ADRs (from [References > Related ADRs](../specs/abac/08-testing-appendices.md#related-adrs)):

| ADR      | Title                                     | Applies To                                                 |
| -------- | ----------------------------------------- | ---------------------------------------------------------- |
| ADR 0009 | Policy Engine Approach                    | Task 17 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) |
| ADR 0010 | Cedar-Aligned Missing Attribute Semantics | Task 11 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) |
| ADR 0011 | Conflict Resolution                       | Tasks 17, 30                                               |
| ADR 0012 | Attribute Resolution Strategy             | Tasks 14, 17                                               |
| ADR 0013 | Property Model                            | Tasks 3, 4a, 4b, 4c, 16b                                   |
| ADR 0014 | Direct Replacement (No Adapter)           | Tasks 28-29                                                |
| ADR 0015 | Player Access Control Layers              | Tasks 4a, 4b, 4c, 16a-b                                    |
| ADR 0016 | Cache Invalidation                        | Task 18 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) |

**Note:** Full ADR directory contains 95 decisions. See `docs/specs/decisions/epic7/` for comprehensive decision history.

### Acceptance Criteria

Every task includes an **Acceptance Criteria** section with specific, verifiable conditions. A task is NOT complete until ALL acceptance criteria are met.

### Review Gate

Every task MUST pass review before being marked complete:

1. **Code review** — Run `pr-review-toolkit:review-pr` or equivalent specialized reviewer
2. **Spec alignment review** — Verify implementation matches referenced spec sections
3. **ADR compliance** — If task references an ADR, verify the decision is correctly implemented
4. **All findings addressed** — Fix issues or document why not applicable

A task is complete ONLY when: tests pass, acceptance criteria met, AND review passed.

### License Headers

All new `.go` files MUST include SPDX license headers. Run `task license:add` after creating new files, or rely on the lefthook pre-commit hook to add them automatically. See CLAUDE.md License Headers section for format details.

---

## Quick Start

**For experienced implementers:** Start here to jump directly to critical tasks without reading the full plan.

**Critical path:** T0→T0.5→T1→T7→DSL chain→Provider chain→T17.1→T17.2→T17.3→T17.4→T18→T21a→T22→T22b→T23→T28→T28.5→T29

**First 3 tasks:**

1. **Task 0** — AST serialization spike (validates storage feasibility, 1-2 hours)
2. **Task 0.5** — Dependency audit (validates go module compatibility, 30 min)
3. **Task 1** — Database migration (creates `access_policies` + `audit_log` tables)

**Key directories to create:** `internal/access/policy/`, `internal/access/abac/`

**Essential reading:**

- Design spec: [docs/specs/2026-02-05-full-abac-design.md](../specs/2026-02-05-full-abac-design.md)
- Decisions index: [docs/specs/decisions/epic7/README.md](../specs/decisions/epic7/README.md)
- Phase 7.1 plan: [2026-02-06-full-abac-phase-7.1.md](./2026-02-06-full-abac-phase-7.1.md)

**Task execution:** Follow [Task Execution Protocol](#task-execution-protocol) (TDD + spec traceability + review gate).

---

## Critical Path Overview

The full dependency diagram below shows all 55 tasks. For quick orientation, here's the critical path showing only the tasks that directly gate project completion:

```mermaid
graph LR
    T0([T0: Spike<br/>M1 Start]) --> T0_5([T0.5: Dep Audit])
    T0_5 --> T1([T1: Migration])
    T1 --> T7([T7: PolicyStore<br/>M1 End])

    T7 --> T8_12([T8-T12:<br/>DSL Chain<br/>M2 End])
    T7 --> T13_15([T13-T15:<br/>Provider Chain])

    T8_12 --> T17_1([T17.1: Bypass<br/>+ Session])
    T13_15 --> T17_1
    T17_1 --> T17_2([T17.2: Target<br/>Matching])
    T17_2 --> T17_3([T17.3: Condition<br/>Evaluation])
    T17_3 --> T17_4([T17.4: Deny-<br/>Overrides<br/>M3 End])

    T17_4 --> T18([T18: Cache])
    T17_4 --> T21a([T21a: Remove<br/>@-prefix])
    T18 --> T23([T23: Bootstrap<br/>M4 End])
    T21a --> T22([T22: Seeds])
    T22 --> T22b([T22b: Gap<br/>Resolution])
    T22b --> T23
    T22b --> T28
    T23 --> T23b([T23b: Validate<br/>Seeds])
    T23b --> T23c([T23c: Smoke<br/>Test Gate])
    T23c --> T28([T28: Migration<br/>M5 End])
    T28 --> T28_5([T28.5: Equiv Tests])
    T28_5 --> T29([T29: Cleanup<br/>M6 End])

    style T7 fill:#ffcccc
    style T8_12 fill:#ffe6cc
    style T17_1 fill:#ffcccc
    style T17_2 fill:#ffcccc
    style T17_3 fill:#ffcccc
    style T17_4 fill:#ffcccc
    style T21a fill:#ffcccc
    style T22 fill:#ffcccc
    style T22b fill:#ffcccc
    style T23 fill:#ffcccc
    style T28 fill:#ffe6cc
    style T29 fill:#ffcccc
```

**Milestones:**

- **M1:** Schema foundation complete (T7)
- **M2:** DSL parser and compiler ready (T12)
- **M3:** Policy engine functional (T17.4)
- **M4:** Bootstrap policies loaded (T23)
- **M5:** Legacy adapter migrated (T28)
- **M6:** All legacy code removed (T29)

**Notes:**

- **T21a** (@-prefix removal) can be submitted as an early independent PR to reduce risk on the T21a→T22→T22b→T23 path
- **T18** (cache) and **T21a** both gate **T23** via different paths (T18 direct, T21a via T22/T22b)

---

## Phase Dependency Diagram

The complete task dependency graph showing all 55 tasks:

```mermaid
graph TD
    subgraph "Phase 7.1: Validation Spike"
        T0[Task 0: AST Serialization Spike]
        T0_5[Task 0.5: Dependency Audit]
        T0 --> T0_5
        T0_5 --> T1
    end

    subgraph "Phase 7.1: Policy Schema"
        T1[Task 1: access_policies migration]
        T2[Task 2: access_audit_log migration]
        T3[Task 3: entity_properties migration]
        T4a[Task 4a: EntityProperty + PropertyRepository]
        T4b[Task 4b: WorldService deletion methods]
        T4c[Task 4c: Property cascade deletion and lifecycle]
        T5[Task 5: Core types]
        T6[Task 6: Subject/resource prefixes]
        T7[Task 7: Policy store]
        T1 --> T7
        T2 --> T7
        T3 --> T4a
        T4a --> T4b
        T4b --> T4c
        T5 --> T7
        T5 --> T6
    end

    subgraph "Phase 7.2: DSL & Compiler"
        T8[Task 8: AST node types]
        T9[Task 9: DSL parser]
        T10[Task 10: DSL fuzz tests]
        T11[Task 11: DSL evaluator]
        T12[Task 12: PolicyCompiler]
        T8 --> T9
        T9 --> T10
        T9 --> T11
        T11 --> T12
    end

    subgraph "Phase 7.3: Policy Engine & Attribute Providers"
        T13[Task 13: Attribute provider interface]
        T14[Task 14: Attribute resolver cache]
        T15[Task 15: Core providers]
        T16a[Task 16a: Simple providers]
        T16b[Task 16b: PropertyProvider]
        T17_1[Task 17.1: System bypass + session]
        T17_2[Task 17.2: Target matching]
        T17_3[Task 17.3: Condition evaluation]
        T17_4[Task 17.4: Deny-overrides + integration]
        T18[Task 18: Policy cache LISTEN/NOTIFY]
        T19[Task 19: Audit logger]
        T19b[Task 19b: Audit retention]
        T20[Task 20: Prometheus metrics]
        T21[Task 21: Performance benchmarks]
        T21b[Task 21b: CI benchmark enforcement]
        T21a[Task 21a: Remove @-prefix from permissions]
        T13 --> T14
        T14 --> T15
        T14 --> T16a
        T15 --> T17_1
        T17_1 --> T17_2
        T17_2 --> T17_3
        T17_3 --> T17_4
        T17_4 --> T18
        T17_4 --> T19
        T19 --> T19b
        T17_4 --> T20
        T17_4 --> T21
        T21 --> T21b
    end

    subgraph "Phase 7.4: Seed Policies & Bootstrap"
        T22[Task 22: Seed policy constants]
        T22b[Task 22b: Resolve seed policy gaps]
        T23[Task 23: Bootstrap sequence]
        T23b[Task 23b: CLI --validate-seeds]
        T23c[Task 23c: Smoke Test Gate]
        T22 --> T22b
        T22b --> T23
        T23 --> T23b
        T23b --> T23c
    end

    subgraph "Phase 7.5: Locks & Admin"
        T24[Task 24: Lock token registry]
        T25[Task 25: Lock parser/compiler]
        T25b[Task 25b: Lock/unlock commands]
        T26a[Task 26a: Admin CRUD commands]
        T26b[Task 26b: Admin state commands]
        T27a[Task 27a: policy test command]
        T27b1[Task 27b-1: Core admin commands]
        T27b2[Task 27b-2: Audit/seed inspection]
        T27b3[Task 27b-3: Recompilation/repair]
        T24 --> T25
        T25 --> T25b
        T26a --> T26b
        T27a --> T27b1
        T27b1 --> T27b2
        T27b2 --> T27b3
    end

    subgraph "Phase 7.6: Call Site Migration"
        T28[Task 28: Migrate to engine]
        T28_5[Task 28.5: Migration equivalence tests]
        T29[Task 29: Remove old AccessControl]
        T28 --> T28_5
        T28_5 --> T29
    end

    subgraph "Phase 7.7: Resilience & Integration"
        T30[Task 30: Integration tests]
        T31[Task 31: Degraded mode]
        T32[Task 32: Schema evolution]
        T33[Task 33: Lock discovery command]
        T34[Task 34: Circuit breaker]
        T35[Task 35: Property orphan cleanup]
    end

    %% Critical cross-phase dependencies
    T0 --> T7
    T0 --> T8
    T5 --> T8
    T7 --> T12
    T7 --> T18
    T12 --> T17_1
    T4a --> T16b
    T5 --> T13
    T12 --> T22
    T12 --> T23
    T21a --> T22
    T6 --> T23
    T16a --> T23
    T16b --> T23
    T18 --> T23
    T14 --> T17_1
    T17_4 --> T24
    T23 --> T26a
    T23 --> T27a
    T17_4 --> T28
    T22b --> T28
    T23c --> T28
    T23b --> T30
    T7 --> T26b
    T17_4 --> T25b
    T17_4 --> T31
    T7 --> T32
    T24 --> T33
    T14 --> T34
    T4c --> T35

    %% Critical path (red); spike (yellow); PropertyProvider parallel chain (orange)
    style T0 fill:#ffffcc
    style T0_5 fill:#ffffcc
    style T7 fill:#ffcccc
    style T12 fill:#ffcccc
    style T13 fill:#ffcccc
    style T14 fill:#ffcccc
    style T15 fill:#ffcccc
    style T17_1 fill:#ffcccc
    style T17_2 fill:#ffcccc
    style T17_3 fill:#ffcccc
    style T17_4 fill:#ffcccc
    style T18 fill:#ffcccc
    style T23 fill:#ffcccc
    style T28 fill:#ffcccc
    style T29 fill:#ffcccc
    style T4a fill:#ffddaa
    style T4b fill:#ffddaa
    style T4c fill:#ffddaa
    style T16b fill:#ffddaa
    style T28_5 fill:#ffcccc
```

**Critical Path (highlighted in red):** Task 0 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) (spike, yellow) → Task 0.5 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) (dependency audit, yellow) → Task 1 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) → Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) → (DSL chain: Task 8 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) → Task 9 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) → Task 11 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) → Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md))) + (Provider chain: Task 13 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) → Task 14 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) → Task 15 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))) → Task 17.1 → Task 17.2 → Task 17.3 → Task 17.4 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) → Task 18 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) → Task 22 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) → Task 22b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) → Task 23 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) → Task 23b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) → Task 23c ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) → Task 28 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)) → Task 28.5 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)) → Task 29 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md))

**Parallel PropertyProvider chain (highlighted in orange):** Task 4a ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) → Task 16b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)). Tasks 3, 4b, 4c are a side chain (property cascade) off Task 3/4a and do not gate downstream work.

**Note:** T16b (PropertyProvider) is intentionally NOT a dependency of T17.1 (AccessPolicyEngine). The engine works using core providers (T15) and simple providers (T16a); PropertyProvider adds `property.*` namespace support but is not required for engine operation. See [Decision #85](../specs/decisions/epic7/phase-7.3/085-property-provider-not-on-critical-path.md).

**Note:** Task 17.1 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) depends on BOTH Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) (DSL compiler) and Task 15 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (core attribute providers). These chains can run in parallel after Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) completes, but both must finish before Task 17.1 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) can start. Sub-tasks T17.1 through T17.4 are sequential within the engine implementation.

**Parallel Work Opportunities:**

- After Task 0.5 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) (dependency audit) completes, Task 1 gates Task 7, but Task 8 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) (AST types) can proceed in parallel (Task 0 validated AST serialization)
- After Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)) completes, two critical chains can run in parallel:
  - DSL chain: Tasks 8-11 can start independently; only Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)) (PolicyCompiler) requires Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md))
  - Provider chain: Tasks 13-15 (attribute providers) can run in parallel with the DSL chain
- Task 16a ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (simple providers) can proceed independently of Task 16b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (PropertyProvider)
- Task 19b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (audit retention) can proceed in parallel with Task 20 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (metrics)
- Phase 7.5 (Locks & Admin) can proceed independently after Task 23 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md))
- Phase 7.7 (Resilience) can proceed after Task 23b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) and Task 17.4 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))

**Cross-Phase Gate:** Task 18 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (Policy cache with LISTEN/NOTIFY invalidation) gates Phase 7.4 start via T18→T23 dependency. Task 18 remains in Phase 7.3 because it is engine infrastructure (cache invalidation) that logically belongs with other engine tasks, even though it blocks Phase 7.4 bootstrap.

**Note:** Task 21a ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)) (remove @-prefix from command names) gates Task 22 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)) (seed policy constants) because seed policies reference bare command names (e.g., `say`, `dig`). If @-prefixes remained, policy resources would mismatch command names during bootstrap. See Phase 7.4 prerequisites and cross-phase gate table for details.

### Cross-Phase Gate Summary

The following table lists all dependencies that cross phase boundaries. These gates determine when downstream phases can start:

| Source Task | Source Phase | Target Task | Target Phase | Gate Rationale                                                  |
| ----------- | ------------ | ----------- | ------------ | --------------------------------------------------------------- |
| T0          | 7.1          | T8          | 7.2          | AST serialization spike validates DSL design                    |
| T4a         | 7.1          | T16b        | 7.3          | Property metadata enables PropertyProvider                      |
| T4c         | 7.1          | T35         | 7.7          | Property cascades needed for orphan cleanup                     |
| T5          | 7.1          | T8          | 7.2          | Core types needed by AST node definitions                       |
| T5          | 7.1          | T13         | 7.3          | Core types needed by attribute provider interface               |
| T6          | 7.1          | T23         | 7.4          | Prefix constants needed by bootstrap                            |
| T16b        | 7.3          | T23         | 7.4          | PropertyProvider enables property.* attributes in seed policies |
| T7          | 7.1          | T12         | 7.2          | PolicyStore interface needed by DSL compiler                    |
| T7          | 7.1          | T18         | 7.3          | PolicyStore needed for cache warming                            |
| T7          | 7.1          | T26b        | 7.5          | Store needed for admin state commands                           |
| T7          | 7.1          | T32         | 7.7          | Store interface needed for schema evolution                     |
| T12         | 7.2          | T17.1       | 7.3          | DSL compiler needed by policy engine                            |
| T12         | 7.2          | T22         | 7.4          | Compiler validates seed policy DSL                              |
| T12         | 7.2          | T23         | 7.4          | Compiler needed for bootstrap compilation                       |
| T14         | 7.3          | T34         | 7.7          | Resolver needed for circuit breaker                             |
| T16a        | 7.3          | T23         | 7.4          | Simple providers (Stream, Command) needed for seed policy eval  |
| T17.4       | 7.3          | T24         | 7.5          | Engine needed before lock registry                              |
| T17.4       | 7.3          | T25b        | 7.5          | Engine needed for lock/unlock commands                          |
| T17.4       | 7.3          | T28         | 7.6          | Engine must exist before migration                              |
| T17.4       | 7.3          | T31         | 7.7          | Engine needed for degraded mode                                 |
| T18         | 7.3          | T23         | 7.4          | Cached policies for bootstrap sequence                          |
| T21a        | 7.3          | T22         | 7.4          | @-prefix removal before seed policies use bare names            |
| T23         | 7.4          | T26a        | 7.5          | Seeded policies before admin CRUD                               |
| T23         | 7.4          | T27a        | 7.5          | Seeded policies before policy test                              |
| T22b        | 7.4          | T28         | 7.6          | Seed policy gaps resolved before migration (Decision #94)       |
| T23         | 7.4          | T28         | 7.6          | Bootstrap must complete before migration                        |
| T23b        | 7.4          | T30         | 7.7          | Seed validation before integration tests                        |
| T24         | 7.5          | T33         | 7.7          | Lock registry needed for discovery command                      |

**Key Insights:**

- Phase 7.1 (PolicyStore interface) is the foundation for all downstream phases
- Phase 7.3 (Engine + Cache) is the critical path bottleneck — gates both Phase 7.4 (Bootstrap) and Phase 7.6 (Migration)
- Phase 7.4 (Bootstrap) must complete before Phase 7.5 (Admin) and Phase 7.6 (Migration) can proceed
- Phase 7.7 (Resilience) depends on completed bootstrap and engine infrastructure

### Milestones

The following milestones mark major integration checkpoints for progress tracking and sprint planning:

| Milestone | Gate Task | Completion Criteria                                 | Phase |
| --------- | --------- | --------------------------------------------------- | ----- |
| M1        | T7        | PolicyStore interface + migration operational       | 7.1   |
| M2        | T12       | Policy DSL compiles and evaluates correctly         | 7.2   |
| M3        | T17.4     | AccessPolicyEngine evaluates real policies          | 7.3   |
| M4        | T23       | Bootstrap inserts seed policies successfully        | 7.4   |
| M5        | T28       | Legacy adapter migrated                             | 7.6   |
| M6        | T29       | Old AccessControl removed, engine is sole authority | 7.6   |

**Notes:**

- **M1 (Schema Ready)** enables all downstream work — PolicyStore is the foundation
- **M2 (DSL Functional)** unblocks policy definition and seed creation
- **M3 (Engine Operational)** is the critical path milestone — gates bootstrap, migration, and admin
- **M4 (Policies Seeded)** enables real-world testing and admin tooling
- **M5 (Adapter Migrated)** completes migration to engine for all production call sites
- **M6 (Migration Complete)** marks full ABAC cutover and old system removal

#### M1 Integration Test Gate Requirements

**Phase:** 7.1 (Policy Schema)

**Test Scope:**

- PolicyStore CRUD operations (Create, Read, List, Update, Delete, ListVersions)
- Database schema integrity (`access_policies`, `access_audit_log`, `entity_properties` tables)
- Transaction rollback behavior for invalid policy writes
- Concurrent policy read/write operations under load

**Coverage Threshold:**

- Integration test coverage: ≥80% of PolicyStore interface methods
- Unit test coverage: ≥85% for `internal/access/policy/store/` package

**Cross-Phase Integration:**

- Database migration rollback and re-application (idempotency check)
- Property cascade deletion via WorldService methods (Tasks 4b, 4c)

**Required Test Suite:**

- `test/integration/access/policy_store_test.go` — Full PolicyStore contract validation
- `test/integration/access/policy_migration_test.go` — Schema migration idempotency

**Sign-off Criteria:**

- All integration tests pass in CI with PostgreSQL testcontainer
- No data loss or corruption under concurrent writes
- Migration can be applied twice without errors (idempotency)

#### M2 Integration Test Gate Requirements

**Phase:** 7.2 (DSL & Compiler)

**Test Scope:**

- DSL parsing for all supported expressions (attribute checks, comparisons, logical ops)
- PolicyCompiler end-to-end compilation (DSL text → CompiledPolicy with version)
- Policy evaluation against mock attribute sets (allow/deny/error cases)
- Fuzz test stability (30s runtime without panics)

**Coverage Threshold:**

- Integration test coverage: ≥85% of DSL parser + evaluator + compiler
- Unit test coverage: ≥90% for `internal/access/policy/dsl/` package
- Fuzz test coverage: All parser entry points fuzzed

**Cross-Phase Integration:**

- Compiled policies serialized to AST and stored via PolicyStore (Phase 7.1)
- Evaluator handles missing attributes per ADR 0010 semantics

**Required Test Suite:**

- `test/integration/access/dsl_compiler_test.go` — End-to-end DSL compilation
- `test/integration/access/dsl_evaluation_test.go` — Policy evaluation correctness
- `internal/access/policy/dsl/parser_fuzz_test.go` — Fuzz tests (already in plan as T10)

**Sign-off Criteria:**

- All test cases from spec examples compile and evaluate correctly
- Fuzz tests run 30s without panics or crashes
- Compiler rejects invalid DSL with clear error messages
- Evaluator returns correct Deny for missing required attributes

#### M3 Integration Test Gate Requirements

**Phase:** 7.3 (Policy Engine & Attribute Providers)

**Test Scope:**

- AccessPolicyEngine full evaluation pipeline (attribute resolution → policy matching → conflict resolution)
- Multi-policy evaluation with deny-overrides conflict resolution
- Attribute provider integration (Subject, Resource, Environment, Stream, Command)
- Cache invalidation via PostgreSQL LISTEN/NOTIFY (Task 18)
- Audit logging in all three modes (off, deny-only, all-decisions)
- Performance benchmarks meet spec targets (<2ms p50, <5ms p99)

**Coverage Threshold:**

- Integration test coverage: ≥90% of AccessPolicyEngine and AttributeResolver
- Unit test coverage: ≥85% for all `internal/access/abac/` packages
- Benchmark coverage: All critical paths benchmarked (evaluation, resolution, cache hits)

**Cross-Phase Integration:**

- Engine evaluates compiled policies from PolicyStore (Phase 7.1)
- Engine uses DSL evaluator from PolicyCompiler (Phase 7.2)
- Cache invalidation triggers on policy updates
- Audit logger persists decisions to `access_audit_log` table

**Required Test Suite:**

- `test/integration/access/engine_evaluation_test.go` — End-to-end policy evaluation
- `test/integration/access/engine_cache_test.go` — Cache invalidation via LISTEN/NOTIFY
- `test/integration/access/attribute_resolution_test.go` — Multi-provider resolution
- `test/integration/access/audit_logging_test.go` — Audit log persistence in all modes
- `internal/access/abac/engine_bench_test.go` — Performance benchmarks

**Cross-Phase Integration Test Matrix:**

| Test Case                         | Phase 7.1 (Store) | Phase 7.2 (DSL) | Phase 7.3 (Engine) | Expected Outcome                         |
| --------------------------------- | ----------------- | --------------- | ------------------ | ---------------------------------------- |
| Store policy → Compile → Evaluate | ✓                 | ✓               | ✓                  | Policy evaluates correctly from database |
| Update policy → Cache invalidates | ✓                 | -               | ✓                  | Cache refreshes via LISTEN/NOTIFY        |
| Missing attribute → Deny          | -                 | ✓               | ✓                  | Evaluator denies per ADR 0010            |
| Multi-policy deny-overrides       | ✓                 | ✓               | ✓                  | Conflicting policies resolve to deny     |
| Audit log written (deny-only)     | ✓                 | -               | ✓                  | Denial persisted to `access_audit_log`   |
| Performance <2ms p50              | ✓                 | ✓               | ✓                  | Benchmark meets spec target              |

**Sign-off Criteria:**

- All integration tests pass in CI with PostgreSQL testcontainer
- Cache invalidation triggers within 100ms of policy update
- Benchmarks meet spec targets (p50 <2ms, p99 <5ms)
- Audit log correctly records denials in deny-only mode
- No panics or deadlocks under concurrent evaluation (100 goroutines, 1000 requests)

### Task Complexity Estimates

T-shirt size estimates for sprint planning:

| Task  | Description                   | Size | Rationale                            |
| ----- | ----------------------------- | ---- | ------------------------------------ |
| T0    | AST serialization spike       | S    | Proof-of-concept, limited scope      |
| T0.5  | Dependency audit              | XS   | Validation/documentation only        |
| T1    | DB migration                  | S    | Standard migration, 2 tables         |
| T2    | Audit log migration           | S    | Single table + partitioning          |
| T3    | Property model migration      | S    | Standard migration                   |
| T4a   | Property metadata columns     | S    | ALTER TABLE + repo methods           |
| T4b   | WorldService property methods | M    | Multiple CRUD methods + tests        |
| T4c   | Property cascade/orphan       | M    | Complex cleanup logic                |
| T5    | Core interfaces               | S    | Type definitions                     |
| T6    | Prefix parser                 | S    | Parsing + constants                  |
| T7    | PolicyStore interface         | M    | Interface + PostgreSQL impl + tests  |
| T8    | AST types                     | S    | Type definitions from spike          |
| T9    | Parser/lexer                  | L    | Full DSL parser                      |
| T10   | Fuzz tests                    | M    | Fuzzing infrastructure               |
| T11   | Evaluator                     | L    | Expression evaluation engine         |
| T12   | PolicyCompiler                | M    | Compilation + versioning             |
| T13   | Schema registry               | M    | Provider registration system         |
| T14   | Attribute resolver            | L    | Multi-provider resolution + timeouts |
| T15   | Core attribute providers      | M    | 3 providers (subject, resource, env) |
| T16a  | Simple providers              | S    | Thin provider wrappers               |
| T16b  | PropertyProvider              | M    | Property-to-attribute mapping        |
| T17.1 | System bypass + session       | M    | Engine scaffold, steps 1-2           |
| T17.2 | Target matching               | M    | Policy filtering, step 4             |
| T17.3 | Condition evaluation          | M    | DSL evaluation, step 5               |
| T17.4 | Deny-overrides + integration  | L    | Combination logic, steps 6-7 + glue  |
| T18   | Policy cache                  | M    | LRU cache + invalidation             |
| T19   | Audit logger                  | M    | Structured logging + config          |
| T19b  | Retention/partitions          | S    | PostgreSQL partition management      |
| T20   | Metrics                       | S    | Prometheus counters/histograms       |
| T21   | Benchmarks                    | M    | Performance test suite               |
| T21a  | Remove @-prefix               | S    | Find/replace + migration             |
| T21b  | CI benchmark enforcement      | S    | CI pipeline config                   |
| T22   | Seed policy constants         | M    | Define all system policies           |
| T22b  | Resolve seed policy gaps      | M    | Gap analysis + new policies          |
| T23   | Bootstrap sequence            | L    | Complex startup + idempotency        |
| T23b  | CLI --validate-seeds          | S    | CLI flag + validation                |
| T23c  | Smoke test gate               | M    | Auth pattern validation before T28   |
| T24   | Lock token registry           | M    | Registry + parser                    |
| T25   | Lock parser/compiler          | L    | Lock expression → DSL compilation    |
| T25b  | Lock/unlock commands          | M    | Command implementation               |
| T26a  | Admin CRUD commands           | L    | Full CRUD for policies               |
| T26b  | Admin state commands          | M    | State management commands            |
| T27a  | policy test command           | L    | Test framework with modes            |
| T27b1 | Core admin commands           | M    | validate/reload/attributes           |
| T27b2 | Audit/seed inspection         | M    | Inspection commands                  |
| T27b3 | Recompilation/repair          | M    | Grammar upgrade commands             |
| T28   | Migrate call sites            | XL   | ~28 production call sites            |
| T28.5 | Migration equivalence tests   | L    | Comprehensive equivalence suite      |
| T29   | Remove old AccessControl      | M    | Deletion + verification              |
| T30   | Integration tests             | L    | End-to-end test suite                |
| T31   | Degraded mode                 | M    | Fallback behavior                    |
| T32   | Schema evolution              | M    | Forward-compatible changes           |
| T33   | Lock discovery command        | S    | Single command                       |
| T34   | Circuit breaker               | M    | Resilience pattern                   |
| T35   | Property orphan cleanup       | M    | Background cleanup                   |

**Summary:** 1 XS, 15 S, 28 M, 10 L, 1 XL = 55 tasks

---

## Phase Files

This implementation consists of **55 tasks** split across **7 phases** for manageability:

- [Phase 7.1: Policy Schema (Database Tables + Policy Store)](./2026-02-06-full-abac-phase-7.1.md)
- [Phase 7.2: DSL & Compiler](./2026-02-06-full-abac-phase-7.2.md)
- [Phase 7.3: Policy Engine & Attribute Providers](./2026-02-06-full-abac-phase-7.3.md)
- [Phase 7.4: Seed Policies & Bootstrap](./2026-02-06-full-abac-phase-7.4.md)
- [Phase 7.5: Locks & Admin](./2026-02-06-full-abac-phase-7.5.md)
- [Phase 7.6: Call Site Migration & Cleanup](./2026-02-06-full-abac-phase-7.6.md)
- [Phase 7.7: Resilience, Observability & Integration Tests](./2026-02-06-full-abac-phase-7.7.md)

---

## Post-Implementation Checklist

- [ ] All unit tests pass: `task test`
- [ ] All integration tests pass: `go test -tags=integration ./test/integration/...`
- [ ] All linters pass: `task lint`
- [ ] Fuzz tests run 30s without panics: `go test -fuzz=FuzzParse -fuzztime=30s ./internal/access/policy/dsl/`
- [ ] Benchmarks within spec targets
- [ ] No references to `AccessControl` interface remain
- [ ] No references to `StaticAccessControl` remain
- [ ] No references to `capability.Enforcer` remain
- [ ] No `char:` prefix usage remains (all migrated to `character:`)
- [ ] No `@`-prefixed command names remain
- [ ] All seed policies compile and pass integration tests
- [ ] Audit logging works in all three modes
- [ ] `policy test` command matches actual `Evaluate()` results
- [ ] Metrics exported correctly on `/metrics` endpoint
- [ ] Code coverage >80% per package

## Spec Coverage Matrix

This matrix maps major sections of the design spec to implementing tasks, ensuring complete coverage.

| Spec Section                          | Primary Tasks                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       | Notes                                                                                    |
| ------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| **Core Interfaces**                   | Task 5 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 6 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md))                                                                                                                                                                                                                                                                                                                                                                                | AccessRequest, Decision, Effect, prefixes                                                |
| **Attribute Providers**               | Task 13 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 14 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 15 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 16a ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 16b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))                                                                                                                                                                                        | Provider interface, resolver, core + simple + property providers                         |
| **Session Subject Resolution**        | Task 6 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 17 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))                                                                                                                                                                                                                                                                                                                                                                               | Session-to-character resolution in prefix parser and engine                              |
| **Policy DSL**                        | Task 8 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)), Task 9 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)), Task 10 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)), Task 11 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)), Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md))                                                                                                                                                                                            | AST, parser, fuzz tests, evaluator, compiler                                             |
| **Property Model**                    | Task 3 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 4a ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 4b ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 4c ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 16b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))                                                                                                                                                                                          | entity_properties migration, repository, WorldService methods, provider                  |
| **Attribute Resolution**              | Task 11 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)), Task 13 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 14 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 32 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md)), Task 34 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md))                                                                                                                                                                                          | Evaluator error handling, schema registry, resolver, evolution, breaker                  |
| **Evaluation Algorithm**              | Task 11 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)), Task 17 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 21 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 30 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md))                                                                                                                                                                                                                                                      | DSL evaluator, engine, benchmarks, integration tests                                     |
| **Policy Storage**                    | Task 1 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 18 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 26a ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 26b ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 32 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md))                                                                                                                              | Migration, store interface, cache, CRUD commands, schema evolution                       |
| **Audit Log**                         | Task 2 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 19 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 19b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 27b2 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md))                                                                                                                                                                                                                                                    | Migration, logger, retention/partitions, audit command                                   |
| **Access Control Layers**             | Task 4a ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md)), Task 16b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 24 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 25 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 25b ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md))                                                                                                                                                                                        | Property metadata, PropertyProvider, lock registry/parser/commands                       |
| **Admin Commands**                    | Task 26a ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 26b ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 27a ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 27b1 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 27b2 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 27b3 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md))                                                                                                                     | CRUD, state, test, validate/reload/attributes, audit/seeds, recompile                    |
| **Grammar Versioning**                | Task 12 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)), Task 27b1 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)), Task 27b3 ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md))                                                                                                                                                                                                                                                                                                              | PolicyCompiler version tracking, core admin commands, recompilation/repair               |
| **Replacing Static Roles**            | Task 21a ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 22 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)), Task 22b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)), Task 23 ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)), Task 23b ([Phase 7.4](./2026-02-06-full-abac-phase-7.4.md)), Task 28 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)), Task 28.5 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)), Task 29 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)) | Remove @-prefix, seed policy constants, gap resolution, bootstrap, validation, migration |
| **Testing Strategy**                  | Task 10 ([Phase 7.2](./2026-02-06-full-abac-phase-7.2.md)), Task 21 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 21b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 28.5 ([Phase 7.6](./2026-02-06-full-abac-phase-7.6.md)), Task 30 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md))                                                                                                                                                                                       | Fuzz tests, benchmarks, CI enforcement, migration equivalence tests, integration tests   |
| **Known Limitations / Resilience**    | Task 31 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md)), Task 34 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md))                                                                                                                                                                                                                                                                                                                                                                              | Degraded mode, circuit breaker                                                           |
| **Future Commands (Deferred)**        | _N/A_                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               | Documented in Deferred Features section; not implemented in this plan                    |
| **Performance Targets/Observability** | Task 17 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 20 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 21 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md)), Task 21b ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))                                                                                                                                                                                                                                                     | Engine implementation includes profiling, metrics, benchmarks, CI checks                 |
| **Lock Token Discovery**              | Task 33 ([Phase 7.7](./2026-02-06-full-abac-phase-7.7.md))                                                                                                                                                                                                                                                                                                                                                                                                                                          | `lock discover` command                                                                  |

**Validation notes:**

- All major spec sections have implementing tasks
- Task 0 (AST serialization spike) validates storage model feasibility
- Task 0.5 (dependency audit) validates Go module compatibility
- ADR compliance tracked per-task in Spec & ADR Traceability section

---

## Conventions

### Legacy Line References

Spec references in this plan link to the modular section files in `docs/specs/abac/`. The primary reference is always the `file.md#anchor` link.

---

## Spec Deviations

Intentional deviations from the design spec, tracked here for discoverability and review.

| Deviation                                                        | Spec Reference                                                                                                       | Task                                                        | Rationale                                                                                                                                               |
| ---------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Metric labels use `{source, effect}` instead of `{name, effect}` | [04-resolution-evaluation.md#performance-targets](../specs/abac/04-resolution-evaluation.md#performance-targets)     | Task 20 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))  | Prevents unbounded cardinality from admin-created policy names                                                                                          |
| Denial audit sync writes elevated from SHOULD to MUST            | [05-storage-audit.md#audit-log-configuration](../specs/abac/05-storage-audit.md#audit-log-configuration)             | Task 19 ([Phase 7.3](./2026-02-06-full-abac-phase-7.3.md))  | Denial audit integrity critical for security forensics; ~1-2ms latency acceptable                                                                       |
| Lock naming uses `lock:<type>:<id>:<action>` format              | [06-layers-commands.md#layer-2-object-locks-owners](../specs/abac/06-layers-commands.md#layer-2-object-locks-owners) | Task 25b ([Phase 7.5](./2026-02-06-full-abac-phase-7.5.md)) | Explicit resource type prefix improves discoverability and query filtering                                                                              |
| Policy compilation moved from PolicyStore to caller              | [01-core-types.md#policycompiler](../specs/abac/01-core-types.md#policycompiler)                                     | Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md))   | Keeps store as pure data access layer; PolicyService wrapper considered but deferred for simplicity; caller validates before persisting                 |
| Policy version records on enable/disable (not just DSL changes)  | [05-storage-audit.md#policy-version-records](../specs/abac/05-storage-audit.md#policy-version-records)               | Task 7 ([Phase 7.1](./2026-02-06-full-abac-phase-7.1.md))   | Enable/disable affects authorization behavior and SHOULD be tracked; spec's narrower scope (DSL changes only) leaves a gap in policy lifecycle auditing |

## Deferred Features

The following features are intentionally deferred from this implementation plan. They are noted here for discoverability.

| Feature                                       | Spec Reference                                                                                                                                                           | Status   | Notes                                                                                                             |
| --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------- | ----------------------------------------------------------------------------------------------------------------- |
| `policy lint` / `policy lint --fix`           | [02-policy-dsl.md](../specs/abac/02-policy-dsl.md), [08-testing-appendices.md#future-commands-deferred](../specs/abac/08-testing-appendices.md#future-commands-deferred) | Deferred | Migration tool for DSL syntax changes; listed under Future Commands                                               |
| `--force-seed-version=N` flag                 | [07-migration-seeds.md#bootstrap-sequence](../specs/abac/07-migration-seeds.md#bootstrap-sequence)                                                                       | Deferred | MAY-level; emergency recovery SQL documented as alternative                                                       |
| Web-based policy editor                       | [00-overview.md#non-goals](../specs/abac/00-overview.md#non-goals)                                                                                                       | Deferred | Future web UI for policy management                                                                               |
| `policy import <file>`                        | [08-testing-appendices.md#future-commands-deferred](../specs/abac/08-testing-appendices.md#future-commands-deferred)                                                     | Deferred | Bulk policy import from file; useful for backup/restore workflows                                                 |
| `policy diff <id1> <id2>`                     | [08-testing-appendices.md#future-commands-deferred](../specs/abac/08-testing-appendices.md#future-commands-deferred)                                                     | Deferred | Compare two policy versions; shows DSL text diff                                                                  |
| `policy export [--format=json]`               | [08-testing-appendices.md#future-commands-deferred](../specs/abac/08-testing-appendices.md#future-commands-deferred)                                                     | Deferred | Export all policies to stdout for backup/migration                                                                |
| `exit:` and `scene:` full attribute providers | [01-core-types.md](../specs/abac/01-core-types.md), [Decision #88](../specs/decisions/epic7/phase-7.3/088-exit-scene-provider-stubs.md)                                  | Partial  | Stub providers (type/id only) added in T16a; full attribute schemas deferred (holomush-5k1.422, holomush-5k1.424) |
| Entity references in policy DSL               | [02-policy-dsl.md](../specs/abac/02-policy-dsl.md)                                                                                                                       | Deferred | References like `subject.owns(resource)` require entity relationship model not yet built                          |
| Cross-resource policy conditions              | [04-resolution-evaluation.md](../specs/abac/04-resolution-evaluation.md)                                                                                                 | Deferred | Conditions spanning multiple resources (e.g., "if owner of parent location"); single-resource evaluation only     |
| RBAC compatibility layer                      | [00-overview.md](../specs/abac/00-overview.md)                                                                                                                           | Deferred | No backwards-compatible RBAC wrapper; direct migration from static roles to ABAC (Decision #36)                   |
| Multi-tenant policy isolation                 | N/A                                                                                                                                                                      | Deferred | Single-server model; no per-game or per-shard policy scoping                                                      |
