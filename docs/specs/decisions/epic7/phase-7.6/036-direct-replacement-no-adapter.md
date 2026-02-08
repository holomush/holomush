<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 36. Direct Replacement (No Adapter)

> [Back to Decision Index](../README.md)

**Review finding:** The adapter pattern ([Decision #5](005-migration-strategy.md)) and shadow mode
([Decision #21](021-shadow-mode-cutover-criteria.md)) add significant complexity: normalization helpers, migration
adapters, shadow mode metrics, cutover criteria, exclusion filtering. This
complexity exists solely to support incremental migration from
`StaticAccessControl`.

**Decision:** Replace `StaticAccessControl` directly with `AccessPolicyEngine`.
No backward-compatibility adapter. All call sites switch to `Evaluate()`
directly.

**Rationale:** HoloMUSH has no production releases and no deployed users. The
static access control system has no consumers to preserve compatibility with.
Building adapter and shadow mode infrastructure for a system that has never
been released wastes effort and makes the design harder to understand.

**Impact:**

- Removes `accessControlAdapter`, `migrationAdapter`, `normalizeSubjectPrefix()`,
  `normalizeResource()`, `shadowModeMetrics`
- Removes shadow mode cutover criteria, exclusion filtering, disagreement
  tracking
- All ~30 production call sites update to `AccessPolicyEngine.Evaluate()` in a single
  phase (phase 7.3)
- The `AccessControl` interface and `StaticAccessControl` struct are deleted
  in phase 7.6

**Supersedes:** [Decision #5](005-migration-strategy.md) (adapter pattern)
