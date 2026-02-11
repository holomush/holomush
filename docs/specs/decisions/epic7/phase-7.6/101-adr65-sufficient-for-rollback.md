<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 101. ADR #65 Is Sufficient for Rollback Coverage

> [Back to Decision Index](../README.md)

**Date:** 2026-02-10
**Phase:** 7.6 (Migration & Cutover)
**Status:** Accepted

## Context

During code review of the Phase 7.6 migration task specification (Task 28 —
migration of ~28 call sites from `StaticAccessControl` to `AccessPolicyEngine`),
a reviewer raised concern that the lack of an adapter layer could make rollback
difficult or impossible if issues surface post-migration.

The specific concern: without an adapter layer maintaining backward compatibility,
rolling back the migration might leave the codebase in an inconsistent state
with no clear recovery path.

## Decision

**Decision #65** ("Git Revert as Migration Rollback Strategy") already provides
comprehensive rollback coverage for Task 28. No additional rollback playbook or
adapter layer is needed.

Decision #65 documents:

1. **Code rollback strategy:** Use `git revert` to restore all ~28 `AccessControl.Check()`
   call sites to pre-migration state
2. **Database rollback strategy:** Down migrations in reverse order (migration
   000017 → 000016 → 000015) with detailed procedures
3. **Backup requirements:** Pre-rollback backup procedures and data recovery paths
4. **Testing strategy:** Rollback testing procedures before production deployment

## Rationale

1. **Comprehensive coverage:** Decision #65 documents both code and database
   rollback with specific migration numbers, down migration scripts, and step-by-step
   procedures.

2. **Direct replacement model:** Per [Decision #36](036-direct-replacement-no-adapter.md),
   the migration is a direct replacement with no adapter layer. Task 28 commits
   can be reverted cleanly without leaving inconsistent state.

3. **Deterministic rollback:** `git revert` is deterministic and well-understood.
   It requires no runtime feature flags or adapter maintenance.

4. **Test coverage as safety net:** Task 28 migration must maintain >80% test
   coverage per package ([Performance Targets](../general/023-performance-targets.md)).
   Comprehensive testing before merge is the primary safety mechanism — issues
   should be caught in CI, not after deployment.

5. **No production releases:** HoloMUSH has no deployed production systems. The
   rollback strategy is documented for completeness, but the primary defense is
   test coverage and code review.

## Consequences

- Task 28 rollback procedures documented in [Decision #65](065-git-revert-migration-rollback.md)
  — no separate playbook needed
- No adapter layer required (per [Decision #36](036-direct-replacement-no-adapter.md))
- Operators performing rollback reference Decision #65 for complete procedures
- Test coverage requirements ([>80% per package](../general/023-performance-targets.md))
  serve as the primary mitigation for post-migration issues

## Cross-references

- [Decision #36](036-direct-replacement-no-adapter.md) — No adapter layer
- [Decision #65](065-git-revert-migration-rollback.md) — Rollback strategy and procedures
- Phase 7.6 Task 28 — Migration of call sites to `AccessPolicyEngine`
