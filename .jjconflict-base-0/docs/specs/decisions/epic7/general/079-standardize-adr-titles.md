<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 79. Standardize ADR Titles Across All Locations

> [Back to Decision Index](../README.md)

**Question:** ADR titles differ across three locations: the Decision Index
README uses canonical titles (e.g., "Conflict Resolution"), the Spec ADR
Reference Mapping uses descriptive titles (e.g., "Deny-Overrides Without
Priority"), and the Plan overview uses yet another set with inconsistent
capitalization. Which convention should be authoritative?

**Options Considered:**

1. **Add clarifying note** — Keep different title styles but add a note
   explaining the convention differences.
2. **Standardize to Decision Index canonical form** — Use the Decision Index
   README titles as the single source of truth across all locations.

**Decision:** Standardize all titles to use the canonical form from the Decision
Index README across all three locations (Decision Index, Spec ADR Reference
Mapping, and Plan overview).

**Rationale:** A single canonical title per decision eliminates confusion when
cross-referencing between the spec, plan, and decision files. The Decision Index
README is the natural authority since it lives alongside the decision files.
Descriptive subtitles can remain within individual decision file bodies.

**Context:** PR #69 review finding I4.

**Cross-reference:** Decision Index README, Spec ADR Reference Mapping, Plan
overview.
