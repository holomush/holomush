<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 59. Fair-Share Provider Timeout Scheduling

> [Back to Decision Index](../README.md)

**Question:** What scheduling approach should be used for distributing the 100ms
evaluation timeout budget across attribute providers during policy evaluation?

**Decision:** Fair-share dynamic allocation is the definitive approach for
provider timeout scheduling. Each provider receives `max(remaining_budget /
remaining_providers, 5ms)` at call time, with unused time automatically
redistributed to subsequent providers. The 100ms total budget serves as a hard
backstop. This is a go-forward commitment—we have evaluated the alternatives and
accept the tradeoffs.

**Rationale:** Fair-share maximizes total budget utilization while maintaining
simplicity. Fast providers (e.g., in-memory lookups returning in <1ms) donate
their unused time to slower providers (e.g., database queries), naturally
balancing the system without operator configuration. The formula is trivial to
implement and reason about, with no tuning parameters beyond the hard 100ms
limit.

**Acknowledged concerns and why they're acceptable:**

1. **Registration order dependency** — A slow early provider can consume budget
   before fast later providers run. This is acceptable because: (a) providers
   register at startup in deterministic order, making behavior predictable; (b)
   the 5ms minimum ensures even late providers get meaningful time; (c) truly
   pathological cases (e.g., a provider consistently timing out) surface quickly
   in monitoring and can be fixed at the provider level.

2. **Dynamic per-provider budgets complicate alerting** — Operators cannot set
   static "provider X should complete in Y ms" alerts because budgets vary based
   on what ran before. This is acceptable because: (a) the total 100ms budget
   provides a clear hard limit for alerting; (b) per-provider P99 latency
   metrics still reveal slow providers; (c) the alternative (fixed equal slices)
   wastes budget when fast providers finish early, making total evaluation
   slower on average.

3. **Alternatives considered but not chosen:**
   - **Fixed equal slices** (e.g., 20ms each for 5 providers) — Simple but
     wastes unused time, making evaluations slower when some providers are fast.
   - **Priority-based allocation** — Adds configuration burden (operators must
     rank provider importance) and complexity (priority queues, starvation
     prevention) with unclear benefit given most providers are ~1-5ms.
   - **Parallel execution** — Would give each provider the full 100ms budget but
     requires thread-safe providers (breaking current single-goroutine contract)
     and adds synchronization complexity. Deferred as future optimization if
     profiling shows evaluation latency is a bottleneck.

Fair-share is the optimal balance of simplicity, utilization, and
predictability. The 100ms hard backstop prevents runaway evaluations, and the
self-balancing property eliminates the need for operator tuning. We move forward
with this approach.

**Cross-reference:** Main spec, Attribute Providers section (lines 1768-1807);
[Decision #58](058-provider-re-entrance-goroutine-prohibition.md) (Provider Re-Entrance Goroutine Prohibition).
