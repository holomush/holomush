<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 78. Task 27b Split into 3 Sub-Tasks

> [Back to Decision Index](../README.md)

**Question:** Task 27b covers 11 distinct admin command features (`policy
validate`, `policy reload`, `policy attributes`, `policy audit`, `policy seed
verify/status`, `policy clear-degraded-mode`, `policy recompile-all/recompile/
repair`, `policy list --old-grammar`). Should it remain a single task or be
split to respect the plan's atomic commit principle?

**Options Considered:**

1. **Keep as single task** — Implement all 11 commands in one task, accepting
   the large scope.
2. **Split into 3 sub-tasks** — Group related commands into coherent units:
   (a) core admin, (b) audit/seed inspection, (c) recompilation/repair.
3. **Split into 11 individual tasks** — One task per command for maximum
   atomicity.

**Decision:** Split into 3 sub-tasks:

- **27b-1: Core admin commands** — `policy validate`, `policy reload`, `policy
  attributes`, `policy list --old-grammar`
- **27b-2: Audit/seed inspection** — `policy audit`, `policy seed verify`,
  `policy seed status`
- **27b-3: Recompilation/repair** — `policy recompile-all`, `policy recompile`,
  `policy repair`, `policy clear-degraded-mode`

**Rationale:** The 3-way split balances atomicity with practicality. Eleven
individual tasks would create excessive overhead for closely related commands.
Grouping by functional area (admin ops, inspection, repair) ensures each
sub-task has a coherent scope and can be independently tested and reviewed.

**Context:** PR #69 review finding I3.

**Cross-reference:** Phase 7.4 Task 27b, Phase 7.4 plan file.
