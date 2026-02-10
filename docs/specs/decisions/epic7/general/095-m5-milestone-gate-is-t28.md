<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 95. M5 Milestone Gate Is T28 (Call-Site Migration)

> [Back to Decision Index](../README.md)

**Question:** Should milestone M5 gate on T27b3 (admin command recompilation,
Phase 7.5) or T28 (call-site migration, Phase 7.6)?

**Context:** The implementation plan had an inconsistency — the milestones
summary listed M5 as T28 ("Legacy adapter migrated") while the milestones table
listed M5 as T27b3 ("All admin commands functional"). T28 is XL-sized (28
production call sites) and sits on the critical path. T27b3 is a Phase 7.5
admin task that does not gate downstream migration work.

**Decision:** M5 = T28. The milestone table MUST be updated to match the
summary. T28 represents the key migration gate — once all 28 call sites are
migrated, the legacy adapter can be removed (M6 = T29). T27b3 does not need
its own milestone since Phase 7.5 completion is implicitly tracked by the
existing task dependency chain.

**Rationale:**

- T28 is on the critical path; T27b3 is not
- The milestones summary description ("Legacy adapter migrated") clearly
  describes T28's purpose, not T27b3's
- Adding a separate milestone for T27b3 would dilute milestone meaning — the
  plan uses milestones for critical path gates, not phase completion markers
- Sprint planning uses the cross-phase gate table for phase transitions, not
  milestones

**Status:** Accepted

**References:**

- `docs/plans/2026-02-06-full-abac-implementation.md` lines 146–153 (summary)
  and 405–413 (table)
- PR #69 review finding: Critical Issue #1
