<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 23. Performance Targets

> [Back to Decision Index](../README.md)

**Review finding:** The spec had no stated performance requirements, making it
impossible to detect regressions or know when optimization is needed.

**Decision:** Define performance targets for the policy engine:

| Metric                   | Target |
| ------------------------ | ------ |
| `Evaluate()` p99 latency | <5ms   |
| Attribute resolution     | <2ms   |
| DSL condition evaluation | <1ms   |
| Cache reload             | <50ms  |

All targets assume 200 concurrent users. Implementation SHOULD add
`slog.Debug()` timers for profiling.

**Rationale:** Concrete targets enable CI-based performance regression detection
and give implementers a clear "good enough" threshold. The 5ms target leaves
headroom for the full request path while keeping authorization invisible to
players.
