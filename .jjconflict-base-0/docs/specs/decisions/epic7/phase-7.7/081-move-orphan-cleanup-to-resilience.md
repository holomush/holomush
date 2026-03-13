<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 81. Move Task 4c Orphan Cleanup Goroutine to Phase 7.7

> [Back to Decision Index](../README.md)

**Question:** Task 4c in Phase 7.1 includes both cascade deletion (synchronous
cleanup when a parent entity is removed) and an orphan cleanup goroutine
(periodic background scan for orphaned properties). Should the background
goroutine remain in Phase 7.1 or move to Phase 7.7 (Resilience)?

**Options Considered:**

1. **Keep in Phase 7.1** — Implement both cascade deletion and orphan cleanup
   goroutine together as part of schema design.
2. **Move goroutine to Phase 7.7** — Keep cascade deletion in Phase 7.1 for
   correctness; defer the background orphan scanner to Phase 7.7 where other
   resilience concerns (circuit breakers, eventual consistency) are addressed.

**Decision:** Move the orphan cleanup goroutine to Phase 7.7. Keep cascade
deletion in Phase 7.1.

**Rationale:** Cascade deletion is a correctness concern — it prevents orphaned
data when parent entities are removed. The orphan cleanup goroutine is a
resilience concern — it catches edge cases where cascade deletion might miss
entries (crashes, race conditions). Separating them aligns each with its
appropriate phase and reduces Phase 7.1 scope.

**Context:** PR #69 review suggestion 10.

**Cross-reference:** Phase 7.1 Task 4c, Phase 7.7 resilience tasks.
