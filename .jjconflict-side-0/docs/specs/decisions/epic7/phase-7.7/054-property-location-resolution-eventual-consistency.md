<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 54. Property Location Resolution Eventual Consistency

> [Back to Decision Index](../README.md)

**Review finding:** At READ COMMITTED isolation, the PropertyProvider's
recursive CTE and subject attribute resolution run in separate transactions.
During character movement, the authorization check could see inconsistent
snapshots — e.g., character in Room B but object still in Room A's containment
hierarchy — violating the design's stated "point-in-time snapshot" invariant.

**Decision:** Document this as a known limitation. MUSH movement is
low-frequency (human-speed, not machine-speed) and the practical impact is
negligible. The 100ms timeout and circuit breaker handle operational concerns.
Do not redesign the visibility model or add pessimistic locking.

**Rationale:** The cost of strict consistency (pessimistic locks or
REPEATABLE READ transactions spanning all providers) is disproportionate to the
risk. In practice, a character moving between rooms takes 100-500ms of human
reaction time, during which authorization checks for their objects are
extremely unlikely. Accepting eventual consistency here follows the principle
of not over-engineering for scenarios that cannot cause meaningful harm.

**Cross-reference:** Main spec, Property Model section; bead `holomush-n0k5`.
