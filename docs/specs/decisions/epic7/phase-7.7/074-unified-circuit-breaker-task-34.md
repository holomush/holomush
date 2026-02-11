<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 74. Unified Circuit Breaker via Task 34

> [Back to Decision Index](../README.md)

**Question:** Should PropertyProvider (Task 16b, Phase 7.3) implement its own
circuit breaker, or should circuit breaker behavior be consolidated elsewhere?

**Options Considered:**

1. **Per-provider circuit breakers** — Each provider implements its own circuit
   breaker with provider-specific thresholds.
2. **Unified circuit breaker in Task 34** — Defer all circuit breaker behavior
   to a single implementation in the resilience phase (Phase 7.7).

**Decision:** Defer all circuit breaker behavior to Task 34 (Phase 7.7), which
implements a unified budget-utilization circuit breaker covering all providers
including PropertyProvider. PropertyProvider in Task 16b returns errors on
timeout but does not implement circuit breaker logic itself.

**Rationale:** Implementing a separate circuit breaker for PropertyProvider in
Phase 7.3 would create duplicate circuit breaker implementations that must later
be reconciled when Task 34 introduces the general-purpose version. Consolidating
all resilience logic in Phase 7.7 keeps the provider implementations simple and
avoids the risk of inconsistent circuit breaker behavior across providers.

**Cross-reference:** Phase 7.3 Task 16b (PropertyProvider), Phase 7.7 Task 34
(provider circuit breaker).
