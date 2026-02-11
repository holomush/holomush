<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 108. Elevate T16b to Mandatory Phase 7.3 Milestone Gate

> [Back to Decision Index](../README.md)

**Review finding (PR #69, Important Q3):** T16b (PropertyProvider) is
documented as a scheduling risk in 3 plan locations but only as a
recommendation with no enforcement mechanism. T16b is not on the critical
path to the engine, but it blocks T23 (Bootstrap) and therefore all of
Phase 7.4.

**Question:** Should T16b be a mandatory milestone gate or remain a
recommendation?

**Options considered:**

| Option                   | Pros                                    | Cons                                   |
| ------------------------ | --------------------------------------- | -------------------------------------- |
| Mandatory milestone gate | Enforces scheduling; prevents slip      | Adds rigidity to Phase 7.3 completion  |
| Bold callout only        | Flexible scheduling                     | Risk of being overlooked; delays 7.4   |

**Decision:** Elevate T16b to a **mandatory Phase 7.3 milestone gate**.

**Rationale:** T16b's completion is a hard prerequisite for Phase 7.4
bootstrap (T23). Leaving it as a recommendation risks delayed discovery of
the blocking dependency. Making it a gate ensures Phase 7.4 cannot begin
until PropertyProvider is complete, preventing downstream scheduling
failures.
