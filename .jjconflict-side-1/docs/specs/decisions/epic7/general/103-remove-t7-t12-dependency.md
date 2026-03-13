<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 103. Remove T7→T12 Dependency (PolicyStore → PolicyCompiler)

> [Back to Decision Index](../README.md)

**Review finding (PR #69, Important #4):** The mermaid dependency diagram
includes a T7→T12 edge (PolicyStore → PolicyCompiler), but Task 12's listed
dependencies are T9 (DSL parser), T11 (condition evaluator), and T6
(AttributeSchema extensions). PolicyCompiler compiles DSL text against a schema;
it does not require the storage layer.

**Decision:** Remove the T7→T12 dependency edge from the mermaid diagram and
gate table. PolicyCompiler depends only on T9, T11, and T6.

**Rationale:**

1. **No code dependency:** `PolicyCompiler.Compile()` takes DSL text and an
   `AttributeSchema`, validates conditions, and returns a `CompiledPolicy`. It
   never reads from or writes to `PolicyStore`.

2. **Critical path improvement:** Removing T7→T12 allows T12 to start as soon
   as T9/T11/T6 complete, without waiting for T7 (PolicyStore). This shortens
   the critical path through the DSL chain.

3. **Task 12 spec confirms:** The Phase 7.2 plan for Task 12 lists dependencies
   on T9, T11, and T6 only — T7 is not mentioned.

**Impact:**

- Mermaid diagram in implementation plan: remove `T7 --> T12` edge
- Cross-phase gate table: remove T7→T12 row if present
- Critical path text: verify no change needed (T12 is not on the main critical
  path text)

**Related:** Decision #30 (PolicyCompiler Component)
