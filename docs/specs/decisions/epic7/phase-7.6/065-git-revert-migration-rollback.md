<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 65. Git Revert as Migration Rollback Strategy

> [Back to Decision Index](../README.md)

**Question:** How do we roll back the migration from `StaticAccessControl` to
`AccessPolicyEngine` if serious issues are discovered after Task 28?

**Options Considered:**

1. **Feature flag** — Toggle between old and new authorization at runtime.
2. **Adapter layer** — Maintain backward-compatible wrapper that can fall back.
3. **Git revert** — Revert the migration commit(s) to restore original call sites.

**Decision:** Use `git revert` of the Task 28 migration commit(s) as the
rollback strategy. Since there is no adapter layer ([Decision #36](036-direct-replacement-no-adapter.md))
and no shadow mode ([Decision #37](037-no-shadow-mode.md)), the migration from
static access control to ABAC is a direct replacement. Reverting Task 28's
commit(s) restores all ~28 `AccessControl.Check()` call sites to their
pre-migration state.

**Rationale:** HoloMUSH has no production releases. Building feature flags or
adapter layers for rollback adds complexity for a system with no deployed users.
Git revert is simple, well-understood, and deterministic. Comprehensive test
coverage (>80% per package) serves as the primary safety net — issues should be
caught before merge, not after deployment. Each package migration commit can be
reverted independently if needed.

**Cross-reference:** [Decision #36](036-direct-replacement-no-adapter.md) (no
adapter), [Decision #37](037-no-shadow-mode.md) (no shadow mode), Phase 7.6
Task 28.
