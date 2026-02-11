<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 83. Circuit Breaker Threshold Increase (3 to 5)

> [Back to Decision Index](../README.md)

**Question:** Is the PropertyProvider circuit breaker threshold of 3 consecutive
timeouts too aggressive, risking false trips during transient database load?

**Context:** The PropertyProvider circuit breaker (unified via ADR
[#74](../phase-7.7/074-unified-circuit-breaker-task-34.md)) trips after
consecutive timeout failures, preventing further queries until reset. The
original threshold of 3 was chosen for aggressive protection but may cause
unnecessary trips during normal transient DB load (connection pool exhaustion,
brief GC pauses, periodic vacuum operations).

**Options Considered:**

1. **Keep at 3** --- Aggressive protection prioritizes safety. Risk: false
   positives during transient load cause unnecessary service degradation.
2. **Increase to 5** --- More tolerant of transient load while still providing
   protection against sustained failures.
3. **Make configurable** --- Runtime-tunable threshold. Adds complexity for
   marginal benefit at this stage.

**Decision:** Increase to 5 (option 2).

**Rationale:**

- 3 consecutive timeouts can occur during normal PostgreSQL maintenance
  (autovacuum, checkpoint) without indicating a real failure
- 5 consecutive 100ms timeouts (500ms total) still provides sub-second
  detection of genuine provider failures
- The circuit breaker's half-open state will still probe recovery quickly
- Avoids unnecessary degraded-mode activations that require admin intervention

**Implementation:** Update the circuit breaker threshold constant from 3 to 5
in spec and plan references.

**Review Finding:** H4 (PR #69 review)
**Bead:** holomush-5k1.371
