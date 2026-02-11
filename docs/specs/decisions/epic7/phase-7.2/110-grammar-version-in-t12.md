<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 110. Add `grammar_version` to CompiledPolicy in T12

> [Back to Decision Index](../README.md)

**Review finding (PR #69, Important P4):** The `grammar_version` field is
not included in Phase 7.2's CompiledPolicy struct. Phase 7.5 (deferred to
Epic 8) depends on this field for versioned grammar evolution.

**Question:** Should `grammar_version` be added now in T12 or deferred
with Phase 7.5?

**Options considered:**

| Option              | Pros                                         | Cons                            |
| ------------------- | -------------------------------------------- | ------------------------------- |
| Add now in T12      | Cheap; avoids schema migration later         | Field unused until Phase 7.5    |
| Defer with Phase 7.5 | No unused fields in struct                   | Schema migration needed later   |

**Decision:** Add `grammar_version` to CompiledPolicy **now in T12**.

**Rationale:** The field is a single integer with negligible cost. Adding
it now avoids a potentially disruptive schema migration when Phase 7.5
lands. Forward-compatible struct design is preferred over strict
minimalism when the cost is trivial.
