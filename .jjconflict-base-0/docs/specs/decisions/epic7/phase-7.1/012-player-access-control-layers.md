<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 12. Player Access Control Layers

> [Back to Decision Index](../README.md)

**Question:** Can characters manage policies for things they own?

**Decision:** Three-layer model:

1. **Property metadata** (all characters) — Set visibility, visible_to,
   excluded_from on owned properties. No policy authoring needed.
2. **Object locks** (resource owners) — Simplified lock syntax (`faction:X`,
   `flag:X`, `level:>=N`, `me`, `&`, `|`, `!`) that compiles to scoped policies.
   Ownership verified before creation.
3. **Full policies** (admin only) — Full DSL with unrestricted scope.

Admin `forbid` policies always trump player locks via deny-overrides.
