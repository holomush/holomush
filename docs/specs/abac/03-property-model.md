<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

## Property Model

Properties are first-class entities with their own identity, ownership, and
access control attributes. This provides conceptual uniformity — characters,
locations, objects, and properties are all entities that the policy engine
evaluates against using the same interface.

**Package ownership:** Properties are world model entities managed by
`internal/world/PropertyRepository`, consistent with `LocationRepository` and
`ObjectRepository`. The `entity_properties` table is part of the world schema.
The `PropertyProvider` (in `internal/access/policy/attribute/`) wraps
`PropertyRepository` to resolve property attributes for policy evaluation.

**Intentional coupling:** Properties embed access control metadata (`owner`,
`visibility`, `visible_to`, `excluded_from`) directly in the world model struct.
This is an intentional architectural tradeoff — properties are the ONLY world
entity with first-class access control fields. Other entities (locations,
objects) rely on external policies. The coupling exists because property
visibility is a core gameplay feature (players configure it directly), not just
an admin concern.

**Dependency layering:** `PropertyRepository` owns data access AND data
invariants. Specifically, `PropertyRepository.Create()` and
`PropertyRepository.Update()` MUST enforce visibility defaults in Go code: when
visibility is `restricted`, auto-populate `visible_to` with `[parent_id]` and
`excluded_from` with `[]` if they are nil. `WorldService` (or the command
handler) owns business rules (e.g., "only the owner can set restricted
visibility") and calls the repository after validation.

During attribute resolution, `PropertyProvider` MUST call
`PropertyRepository` methods directly, bypassing `WorldService`. The engine
resolves property attributes unconditionally (no authorization check during
attribute resolution); authorization happens AFTER attributes are resolved.
This prevents a circular dependency:
`Engine → PropertyProvider → PropertyRepository` (no callback to Engine).
`PropertyProvider` MUST NOT depend on `WorldService` or `AccessPolicyEngine`.

### Property Attributes

| Attribute         | Type     | Description                                                                   |
| ----------------- | -------- | ----------------------------------------------------------------------------- |
| `id`              | ULID     | Unique property identifier                                                    |
| `parent_type`     | string   | Parent entity type: character, location, object                               |
| `parent_id`       | ULID     | Parent entity ID                                                              |
| `name`            | string   | Property name (unique per parent)                                             |
| `value`           | string   | Property value                                                                |
| `owner`           | string   | Subject who created/set this property                                         |
| `visibility`      | string   | Access level: public, private, restricted, system, admin                      |
| `flags`           | []string | Arbitrary flags (JSON array)                                                  |
| `visible_to`      | []string | Character IDs allowed to read (restricted, max 100)                           |
| `excluded_from`   | []string | Character IDs denied from reading (max 100)                                   |
| `parent_location` | ULID     | Resolved dynamically from parent entity's current location at evaluation time |

### Visibility Levels

| Visibility   | Who can see?        | visible_to/excluded_from |
| ------------ | ------------------- | ------------------------ |
| `public`     | Anyone in same room | Not applicable (NULL)    |
| `private`    | Owner only          | Not applicable (NULL)    |
| `restricted` | Explicit list       | Defaults: [self], []     |
| `system`     | System only         | Not applicable (NULL)    |
| `admin`      | Admins only         | Not applicable (NULL)    |

**Public visibility and movement:** Public visibility on character properties
means the property is visible to characters in the same location as the owning
character. As the owning character moves, the set of characters who can see
their public properties changes accordingly. The `parent_location` attribute
is **resolved dynamically at policy evaluation time** from the parent entity's
current location state in the database, NOT frozen at property creation time.
The `PropertyProvider` queries the parent entity's current `location_id` via
SQL JOIN (see [decision #32](../decisions/epic7/phase-7.3/032-property-provider-sql-join.md))
to populate the `parent_location` attribute in the resource bag. If the
parent entity has no valid location (e.g., a character in the lobby before
entering a room), `parent_location` is nil and location-based visibility
policies fail-safe (deny).

When visibility is set to `restricted`, the Go property store MUST auto-populate
`visible_to` with `[parent_id]` and `excluded_from` with `[]` if they are nil.
This prevents the "nobody can see it" footgun.

**List size limits:** `visible_to` and `excluded_from` are capped at 100 entries
each. The property store MUST reject updates that would exceed this limit. For
access control involving larger groups, admins SHOULD use flag-based policies
(e.g., `principal.flags.containsAny(["guild-members"])`) rather than listing
individual character IDs. This prevents linear-scan performance degradation
during `principal.id in resource.visible_to` evaluation.

**List overlap prohibition:** A character ID MUST NOT appear in both
`visible_to` and `excluded_from` for the same property. The property store
MUST reject updates that would create this overlap. Without this constraint,
adding a character to `visible_to` has no observable effect if they are already
in `excluded_from` (deny-overrides means the `forbid` policy on
`excluded_from` always wins), creating confusing UX for property owners.

### Visibility Seed Policies

Each visibility level is enforced by system-level seed policies (see
[Seed Policies](07-migration-seeds.md#seed-policies) for the complete set). These property
visibility policies are created during bootstrap alongside the role-based
seed policies:

```text
// Public properties: readable by characters in the same location as the parent
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "public"
    && principal.location == resource.parent_location };

// Private properties: readable only by owner
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "private"
    && resource.owner == principal.id };

// Restricted properties: visible_to/excluded_from policies are defined as seed policies below

// System properties (visibility == "system") are protected by default-deny.
// No seed policy grants access to them, so they remain inaccessible to all
// characters (including admins). This is intentional — system properties are
// reserved for internal use by the platform itself, not player access.
// We rely on default-deny instead of an explicit forbid policy because:
// 1. Under deny-overrides conflict resolution, a forbid would block even
//    seed:admin-full-access (permit), locking admins out permanently.
// 2. Default-deny still provides full audit attribution (effect=default_deny
//    is logged), so an explicit forbid provides no additional value.

// Admin properties: readable only by admins
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "admin" && principal.role == "admin" };

// seed:property-restricted-visible-to
// Restricted properties: readable by characters in the visible_to list
permit(principal is character, action in ["read"], resource is property)
when { resource.visibility == "restricted"
    && resource has visible_to
    && principal.id in resource.visible_to };

// seed:property-restricted-excluded
// Restricted properties: denied to characters in the excluded_from list
forbid(principal is character, action in ["read"], resource is property)
when { resource.visibility == "restricted"
    && resource has excluded_from
    && principal.id in resource.excluded_from };
```

**Note:** The `PropertyProvider` MUST also expose a `parent_location` attribute
— the ULID of the parent entity's location. For character properties, this
is the character's current location. For location properties, this is the
location itself. For object properties, this is the object's containing
location.

**Dependency chain:** Resolving `parent_location` requires knowing the parent
entity's ultimate containing location. To avoid a second code path for
world-model queries, `PropertyRepository.GetByID()` MUST resolve
`parent_location` in the same query. The `PropertyProvider` reads
`parent_location` from the repository result — no separate `LocationLookup`
function or direct DB query is needed. This keeps a single code path for all
location data: `PropertyProvider → PropertyRepository → DB`.

The repository determines the resolution strategy based on `parent_type`:

- `character` → JOIN `characters` table for current `location_id`
- `location` → use `parent_id` directly (the location IS the parent)
- `object` → resolution depends on the object's placement column. The
  `objects` table has `location_id`, `held_by_character_id`, and
  `contained_in_object_id` columns — exactly one is non-NULL (a world model
  invariant enforced by `WorldService`). Resolution strategy:
  - **Direct location:** If `location_id` is non-NULL, use it directly.
  - **Held by character:** If `held_by_character_id` is non-NULL, JOIN
    through the `characters` table to get the character's current
    `location_id`. The object's location is its holder's location.
  - **Contained in object:** If `contained_in_object_id` is non-NULL,
    recursive CTE traversal of the containment hierarchy. Objects support
    nested containment (chest inside a room, gem inside the chest). The
    repository MUST walk up the `contained_in_object_id` chain until reaching
    an object with a non-NULL `location_id` or `held_by_character_id`. The
    existing `checkCircularContainmentTx` already uses recursive CTEs for
    this pattern.
  - **Orphaned objects:** If no placement column is non-NULL (data
    corruption), `parent_location` is nil and location-based visibility
    policies fail-safe (deny).

  The recursive CTE **MUST** include both a depth limit of **20 iterations**
  and cycle detection (tracking visited IDs via an array path column,
  rejecting IDs already in the path) as defense-in-depth against data
  corruption. PostgreSQL `WITH RECURSIVE` does not automatically prevent
  cycles. If cycles are detected or the depth limit is exceeded,
  `parent_location` is nil and location-based visibility policies fail-safe
  (deny). A 20-level nesting limit is sufficient for typical object
  containment hierarchies (e.g., gem → lockbox → drawer → chest → room)
  while preventing excessive query complexity.

  The `PropertyProvider` **MUST** set a per-query timeout of **100ms** using
  `SET LOCAL statement_timeout = '100ms'` at the start of each
  `PropertyRepository.GetByID()` transaction. This ensures that recursive CTE
  queries for deeply nested or corrupted data do not block the evaluation
  pipeline. If the query timeout is exceeded, PostgreSQL returns a timeout
  error, which the PropertyProvider **MUST** propagate as a core provider
  error (return `(nil, err)`), triggering EffectDefaultDeny with error
  propagation.

  If the PropertyProvider encounters **5 or more** query timeout errors within
  a **60-second window**, it **MUST** trip a circuit breaker and return
  default-deny for all subsequent resource attribute requests for that
  60-second window without executing additional queries. This prevents
  systematic timeout errors from overwhelming the database connection pool.
  The circuit breaker trip is logged once at ERROR level with message
  `"PropertyProvider circuit breaker tripped after 5 timeout errors in 60s —
  skipping queries"`. (See [Decision #83](../decisions/epic7/phase-7.3/083-circuit-breaker-threshold-increase.md) for rationale on the 5-timeout threshold.) A Prometheus counter metric
  `abac_property_provider_circuit_breaker_trips_total` **MUST** be
  incremented when the circuit breaker trips. Operators **SHOULD** configure
  alerting on this metric and investigate data model integrity issues (deep
  nesting, circular containment) when circuit breaker trips occur.

  **N+1 Query Pattern:** Resolving multiple properties with `parent_location`
  triggers separate recursive CTE executions per property (e.g., a `look`
  command checking 10 properties executes 10 CTEs). The per-request attribute
  cache mitigates within-request duplication. Implementations **SHOULD**
  prefer batch resolution: collect all needed properties upfront and resolve
  `parent_location` once per unique object.
