<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 44. Nested Container Resolution via Recursive CTE

> [Back to Decision Index](../README.md)

**Review finding (I6):** The spec's `parent_location` resolution for objects
used a simple JOIN against `objects.location_id`, but the world model supports
nested containers (objects inside objects). An object in a chest in a room
would have `location_id = NULL` — the simple JOIN would fail to resolve its
location.

**Decision:** Use a recursive CTE to walk the containment chain:

```sql
WITH RECURSIVE chain AS (
    SELECT id, location_id, contained_in_object_id,
           ARRAY[id] AS path, 1 AS depth
    FROM objects WHERE id = $1
    UNION ALL
    SELECT o.id, o.location_id, o.contained_in_object_id,
           c.path || o.id, c.depth + 1
    FROM objects o
    JOIN chain c ON o.id = c.contained_in_object_id
    WHERE NOT o.id = ANY(c.path)  -- cycle detection
      AND c.depth < 20            -- depth limit
)
SELECT location_id FROM chain WHERE location_id IS NOT NULL LIMIT 1;
```

**Note:** The `path` array column tracks visited IDs to detect cycles
(corrupted containment data). The `depth < 20` limit provides defense-in-depth
against pathological chains. Both guards are REQUIRED in the implementation —
PostgreSQL `WITH RECURSIVE` does not automatically prevent cycles.

**Rationale:** The existing `object_repo.go` already uses recursive CTEs for
containment queries. Reusing this pattern in `PropertyRepository` ensures
`parent_location` resolves correctly regardless of nesting depth. The CTE
terminates when it finds the first ancestor with a `location_id`.
