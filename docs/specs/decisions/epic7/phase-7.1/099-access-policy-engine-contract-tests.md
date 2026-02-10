<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# ADR 099: AccessPolicyEngine Contract Tests (Task 7b)

**Status:** Accepted

**Date:** 2026-02-10

**Context:**

Bug report holomush-5k1.505 (I9) identified a gap in the Full ABAC test suite: no dedicated contract/interface test for `AccessPolicyEngine` covering edge cases such as malformed subjects, context cancellation, and empty cache scenarios. While these cases are partially exercised through integration tests and migration equivalence tests, they lack focused validation at the engine's public API boundary.

Contract tests validate that an interface implementation correctly handles edge cases and error conditions that are unlikely to be hit by higher-level integration tests. For the `AccessPolicyEngine`, this includes:

- Malformed or invalid `AccessRequest` inputs (empty fields, nil requests, invalid prefixes)
- Runtime errors (context cancellation, provider failures)
- Boundary conditions (empty policy cache, no matching policies)
- Error code preservation through the call stack

Without these tests, edge cases may go undetected until production use, increasing the risk of panics or incorrect authorization decisions.

**Decision:**

Create Task 7b "AccessPolicyEngine contract tests" as a new task in Phase 7.1, with a dependency on Task 7 (PolicyStore interface). This task focuses exclusively on validating the `AccessPolicyEngine.Evaluate()` method's contract at API boundaries.

**Implementation:**

- **Task ID:** T7b
- **Phase:** 7.1 (Policy Schema)
- **Dependencies:** Task 7 (PolicyStore interface and engine scaffold must exist)
- **Scope:** Contract tests only (no implementation changes unless bugs are found)
- **Critical Path:** No (parallel work; not blocking DSL or provider chains)

**Acceptance Criteria:**

1. Malformed subject prefixes return `INVALID_ENTITY_REF` error code
2. Empty subject, action, or resource strings return appropriate error codes
3. Nil `AccessRequest` rejected with `INVALID_REQUEST` error code
4. Context cancellation mid-evaluation returns `context.Canceled` error
5. Empty policy cache (no policies loaded) returns `EffectDefaultDeny`
6. Error wrapping preserves error code through entire call stack
7. All tests pass via `task test`

**Test File:** `internal/access/abac/engine_contract_test.go`

**Rationale:**

1. **Separation of Concerns:** Contract tests validate the public API surface independently of integration tests, which focus on end-to-end workflows.
2. **Edge Case Coverage:** Integration tests typically validate happy paths and common error paths; contract tests focus on unusual or malformed inputs that are hard to trigger naturally.
3. **Error Code Stability:** Contract tests ensure error codes remain stable across refactors, which is critical for upstream error handling.
4. **Parallel Execution:** T7b is not on the critical path. It can run in parallel with Phase 7.2 (DSL) and Phase 7.3 (providers) work.
5. **Low Risk:** Adding tests without changing implementation is low-risk; if the tests reveal bugs, fixes can be scoped to the engine implementation without affecting downstream tasks.

**Alternatives Considered:**

1. **Merge into T17.1-T17.4 (engine implementation tasks):** Rejected because those tasks already have extensive acceptance criteria. Adding contract tests would bloat the task scope.
2. **Merge into T30 (integration tests):** Rejected because integration tests focus on end-to-end workflows, not API boundary validation.
3. **Skip dedicated contract tests:** Rejected because edge case coverage is weak without focused API boundary tests.

**Consequences:**

- **Positive:** Improved edge case coverage at engine API boundaries; better error handling validation.
- **Neutral:** Adds 1 small task (T7b) to Phase 7.1, increasing total task count from 46 to 47.
- **Negative:** None identified.

**Related:**

- Bug report: holomush-5k1.505 (I9)
- Task 7: PolicyStore interface (Phase 7.1)
- Task 17.1-17.4: AccessPolicyEngine implementation (Phase 7.3)
- Task 30: Integration tests (Phase 7.7)
