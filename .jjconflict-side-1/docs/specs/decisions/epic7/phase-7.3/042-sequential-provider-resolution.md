<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 42. Sequential Provider Resolution

> [Back to Decision Index](../README.md)

**Review finding (I1):** The spec didn't justify why attribute providers are
resolved sequentially rather than in parallel. With 4+ providers, parallel
resolution could reduce latency.

**Decision:** Keep sequential resolution. Document the rationale explicitly.

**Rationale:** At ~200 concurrent users with providers backed by indexed
PostgreSQL queries, parallel resolution saves <1ms total. Sequential
resolution provides deterministic merge order (later providers can't
silently overwrite earlier attributes), simpler error attribution (which
provider failed is unambiguous), and straightforward debugging
(`slog.Debug` after each provider). Parallel resolution adds goroutine
management, merge synchronization, and non-deterministic log ordering for
negligible latency benefit. If profiling shows provider resolution exceeding
the 2ms target, parallelization can be introduced without API changes.

**Note:** This decision assumes each provider completes in <500us with indexed
PostgreSQL queries. If profiling during implementation shows attribute
resolution exceeding the 2ms budget (see [Decision #23](../general/023-performance-targets.md)),
parallel resolution can be added without API changes — the `AttributeResolver`
interface supports both sequential and parallel strategies.
