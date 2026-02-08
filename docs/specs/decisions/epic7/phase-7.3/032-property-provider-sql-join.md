<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 32. PropertyProvider Uses SQL JOIN for Parent Location

> [Back to Decision Index](../README.md)

**Review finding:** The `PropertyProvider` needed the parent entity's location
ULID when resolving `parent_location`. The original design used a
`LocationLookup` dependency, creating a `PropertyProvider → LocationLookup →
WorldService` chain that re-introduced the circular dependency the provider
pattern was designed to avoid.

**Decision:** `PropertyRepository` resolves `parent_location` via a recursive
CTE that walks the containment chain (see [Decision #44](044-nested-container-resolution-via-recursive-cte.md)
for the full query). For top-level objects and locations, this is equivalent to
a simple JOIN against `objects.location_id`. For nested containers (objects
inside objects), the CTE walks upward until finding an ancestor with a non-NULL
`location_id`. No extra Go-level dependency is required.

**Rationale:** The data is already in PostgreSQL. A single query with a
recursive CTE handles both top-level and nested container cases correctly. This
keeps the provider dependency chain flat: `PropertyProvider →
PropertyRepository → PostgreSQL`.
