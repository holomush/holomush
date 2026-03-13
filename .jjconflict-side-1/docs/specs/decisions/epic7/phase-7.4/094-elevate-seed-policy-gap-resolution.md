<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 94. Elevate Seed Policy Gap Resolution to Dedicated Task

> [Back to Decision Index](../README.md)

**Question:** Should seed policy coverage gap resolution (Appendix A gaps G1-G6)
remain as a checkbox within T22, or be elevated to a dedicated task?

**Context:** The implementation plan's Appendix A identifies 6 gaps (G1-G6) where
production call sites lack seed policy coverage. G1 (exit read), G2 (builder exit
write), G3 (list\_characters), and G4 (scene operations) are Medium-High severity.
With direct replacement and no adapter/shadow mode (Decisions #36, #37), these gaps
cause immediate functional regressions when T28 migrates call sites. The gap
resolution was originally buried as a T22 acceptance criterion checkbox, which
underrepresents the work involved and risks it being overlooked.

**Decision:** Create a dedicated task T22b "Resolve seed policy coverage gaps" in
Phase 7.4, with an explicit dependency chain: T22 -> T22b -> T28. T28 (call site
migration) MUST NOT start until T22b confirms all Medium-High severity gaps (G1-G4)
are resolved with seed policies, and G5-G6 are documented as intentional or
addressed.

**Rationale:**

- **Risk mitigation:** Direct replacement means no fallback. Unresolved gaps cause
  immediate deny-all for affected call sites (exit navigation, character listing,
  scene operations).
- **Visibility:** A dedicated task surfaces the gap resolution work in dependency
  graphs and sprint planning, preventing it from being lost in a checkbox.
- **Dependency clarity:** The T22 -> T22b -> T28 chain makes it structurally
  impossible to start migration without resolving gaps.
- **Scope separation:** T22 defines seed policy constants; T22b validates coverage
  completeness and adds missing policies. These are distinct concerns.

**Cross-reference:** Review finding C1 (PR #69); Appendix A (Phase 7.4); Decisions
\#36 (direct replacement), #37 (no shadow mode); Task I1 (T28 decomposition).
