<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 96. Defer Phase 7.5 (Locks & Admin) to Epic 8

> [Back to Decision Index](../README.md)

**Question:** Should Phase 7.5 (Locks & Admin) remain in Epic 7 scope, or be
deferred to Epic 8?

**Context:** Phase 7.5 contains 9 tasks (T24, T25, T25b, T26a, T26b, T27a,
T27b-1, T27b-2, T27b-3) covering lock token registry, lock expression
parser/compiler, lock/unlock commands, admin CRUD commands, admin state
management, policy test command, and various admin inspection/repair tools.

These tasks are not on the critical path for ABAC core functionality. The
critical path runs through Phases 7.1 (schema) -> 7.2 (DSL) -> 7.3 (engine) ->
7.4 (bootstrap) -> 7.6 (migration) -> 7.7 (resilience). Phase 7.5 branches off
after Phase 7.4 and does not gate any downstream work in Phases 7.6 or 7.7,
except for Task 33 (lock discovery command in Phase 7.7) which depends on Task
24 (lock token registry).

**Options considered:**

1. **Keep Phase 7.5 in Epic 7** — Complete all 9 tasks before considering Epic 7
   done. Adds 1S + 6M + 2L of work (estimated 2-3 sprints) to an already large
   epic (46 remaining tasks).

2. **Defer Phase 7.5 to Epic 8** — Remove from Epic 7 scope, reducing active
   task count from 55 to 46. Lock/admin features become Epic 8 deliverables.
   One Phase 7.7 task (T33) becomes blocked until Epic 8.

**Decision:** Option 2 — Defer Phase 7.5 to Epic 8.

**Rationale:**

- Phase 7.5 is not architecturally required for the ABAC policy engine to
  function. The engine evaluates policies, resolves attributes, and enforces
  access control without lock commands or admin tooling.
- The core ABAC replacement (Phases 7.1-7.4 + 7.6) delivers the primary
  architectural goal: replacing `StaticAccessControl` with `AccessPolicyEngine`.
- Lock/admin commands enhance operability but are additive features, not
  foundational infrastructure.
- Deferring reduces Epic 7 scope by 9 tasks (16% reduction), making the epic
  more tractable for sprint planning.
- Only one downstream task (T33, lock discovery) is affected. All other Phase
  7.7 tasks proceed independently.
- The `policy clear-degraded-mode` command (T27b-3) is deferred, but degraded
  mode can be cleared via direct database flag reset or server restart as a
  temporary workaround until Epic 8.

**Impact:**

| Area                       | Impact                                                          |
| -------------------------- | --------------------------------------------------------------- |
| Phase 7.7 Task 33          | Blocked until Phase 7.5 completes in Epic 8                     |
| Phase 7.7 Task 31          | Degraded mode clearing needs temporary workaround               |
| Cross-phase gate table     | 6 entries marked as deferred                                    |
| Mermaid dependency diagram | Phase 7.5 subgraph marked deferred with dashed dependency edges |
| Active task count          | Reduced from 55 to 46                                           |

**Status:** Accepted

**References:**

- `docs/plans/2026-02-06-full-abac-implementation.md` (master plan)
- `docs/plans/2026-02-06-full-abac-phase-7.5.md` (deferred phase)
- Bug: holomush-5k1.513
