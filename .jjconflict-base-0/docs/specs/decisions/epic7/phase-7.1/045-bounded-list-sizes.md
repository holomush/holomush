<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 45. Bounded List Sizes for `visible_to` / `excluded_from`

> [Back to Decision Index](../README.md)

**Review finding (S3):** The `visible_to` and `excluded_from` TEXT arrays
had no size limit. A character could theoretically add thousands of entries,
degrading `containsAny`/`in` evaluation performance.

**Decision:** Enforce a maximum of 100 entries per list. The property store
rejects updates that would exceed this limit with a clear error message.

**Rationale:** 100 entries balances flexibility (covers 99% of realistic MUSH
use cases — a property visible to 100 specific characters is already unusually
granular) with performance (avoids multi-millisecond scans). At 100 entries,
`in` evaluation requires O(100) string comparisons per check. Adversarial lists
(e.g., 10,000+ entries) would require hash set implementation instead of linear
scan to maintain p99 latency targets.
