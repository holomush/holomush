<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 97. P99 Performance Target Adjustment (5ms → 25ms)

> [Back to Decision Index](../README.md)

**Review finding (holomush-5k1.502, Issue I6):** The original p99 <5ms target
for policy evaluation is aggressive given PropertyProvider uses SQL JOINs for
parent location resolution (Decision #32). Reviewer recommended validation.

**Decision:** Adjust the `Evaluate()` p99 latency target from <5ms to <25ms.

**Rationale:**

1. **SQL JOIN overhead:** PropertyProvider performs recursive CTE queries with
   JOINs to resolve parent location attributes (Decision #32). This adds
   database roundtrip latency that makes sub-5ms p99 unrealistic.

2. **Still imperceptible:** 25ms is well under the 100ms human perception
   threshold. Authorization remains invisible to players.

3. **Headroom for growth:** As property usage increases and containment
   hierarchies deepen, the additional headroom prevents future CI failures.

4. **Cold cache remains 10ms:** The cold cache target (with DB roundtrip) stays
   at <10ms, which now aligns better with warm cache expectations given that
   both involve database queries.

**Impact:**

- Decision #23 updated with new target
- All plan files updated (implementation plan, Phase 7.3 plan)
- All spec files updated (core types, resolution/evaluation, testing appendices)
- CI benchmark thresholds updated (110% of 25ms = 27.5ms)

**Related:** Decision #23 (Performance Targets), Decision #32 (PropertyProvider
SQL JOIN), Decision #85 (PropertyProvider Not on Critical Path)
