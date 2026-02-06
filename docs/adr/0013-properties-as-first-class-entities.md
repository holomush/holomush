# ADR 0013: Properties as First-Class World Model Entities

**Date:** 2026-02-05
**Status:** Accepted
**Deciders:** HoloMUSH Contributors

## Context

HoloMUSH needs per-property access control for gameplay scenarios like:

- A healer can see another character's `wounds` property, but the character themselves cannot
- A character's `backstory` is visible to a select group of trusted players
- A location's `secret_passage` property is only visible to characters with the `explorer` flag

Properties currently exist as simple key-value pairs on entities. To support fine-grained
access control, properties need access control metadata: who owns them, who can see them,
and under what conditions.

The question is whether properties should be modeled as sub-resources of their parent
entity (e.g., `character:01ABC/wounds`) or as independent entities with their own identity
in the world model.

### Options Considered

**Option A: Properties as sub-resources (path-based addressing)**

Properties are addressed by path from their parent: `character:01ABC/wounds`. The parent
entity's repository manages property CRUD. Access control policies reference properties
by path pattern.

| Aspect     | Assessment                                                                                                                                                                                                                     |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Strengths  | Minimal world model change; path-based addressing is familiar                                                                                                                                                                  |
| Weaknesses | Introduces a second resource model (entities vs sub-resources); admins must learn two addressing schemes; property attributes are implicitly derived from parent type; policies on sub-resources require different DSL grammar |

**Option B: Properties as first-class entities**

Properties have their own ULID identity, their own database table (`entity_properties`),
and their own repository (`PropertyRepository`). They are entities in the same sense as
characters, locations, and objects. The policy engine evaluates access to properties using
`resource is property` with the same syntax as any other resource type.

| Aspect     | Assessment                                                                                                                                                                                                         |
| ---------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Strengths  | Conceptual uniformity — everything is an entity; admins learn one model; `PropertyProvider` resolves attributes the same way as `CharacterProvider`; parent relationship is an attribute, not a structural concern |
| Weaknesses | More database rows; properties need their own table and repository; introduces access control metadata directly into the world model                                                                               |

**Option C: Property flags only (no ABAC integration)**

Properties store simple visibility flags (`public`, `private`, `admin`) without ABAC
integration. Access is checked by flag value in application code.

| Aspect     | Assessment                                                                                                                              |
| ---------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Strengths  | Simplest to implement                                                                                                                   |
| Weaknesses | Cannot write rich policies on properties; no `visible_to` lists; no conditional visibility; parallel access control system outside ABAC |

## Decision

**Option B: Properties as first-class entities.**

Properties are world model entities managed by `internal/world/PropertyRepository`,
alongside `LocationRepository` and `ObjectRepository`. The `entity_properties` table is
part of the world schema. The `PropertyProvider` in `internal/access/policy/attribute/`
wraps `PropertyRepository` to resolve property attributes for policy evaluation.

### Property Schema

| Attribute       | Type       | Description                                     |
| --------------- | ---------- | ----------------------------------------------- |
| `id`            | ULID       | Unique property identifier                      |
| `parent_type`   | string     | Parent entity type: character, location, object |
| `parent_id`     | ULID       | Parent entity ID                                |
| `name`          | string     | Property name (unique per parent)               |
| `value`         | string     | Property value (NULL for flag-style properties) |
| `owner`         | string     | Subject who created/set this property           |
| `visibility`    | string     | public, private, restricted, system, admin      |
| `flags`         | \[\]string | Arbitrary flags                                 |
| `visible_to`    | \[\]string | Character IDs (restricted visibility only)      |
| `excluded_from` | \[\]string | Character IDs (restricted visibility only)      |

### Intentional Coupling

Properties embed access control metadata (`owner`, `visibility`, `visible_to`,
`excluded_from`) directly in the world model struct. This is an **intentional architectural
tradeoff**:

- Properties are the **ONLY** world entity with first-class access control fields
- Other entities (locations, objects) rely on external policies alone
- The coupling exists because property visibility is a **core gameplay feature** that
  players configure directly, not just an admin concern

### Dependency Layering

To prevent circular dependencies during attribute resolution:

```text
Engine → PropertyProvider → PropertyRepository → PostgreSQL
```

`PropertyProvider` MUST call `PropertyRepository` directly, bypassing `WorldService`.
The engine resolves property attributes unconditionally (no authorization check during
attribute resolution); authorization happens AFTER attributes are resolved.

`PropertyRepository.GetByID()` uses a SQL JOIN against the parent entity's table to fetch
`parent_location` in the same query. The join target depends on `parent_type`:

- `character` → join `characters` table for current location
- `location` → use `parent_id` directly (the location IS the parent)
- `object` → resolution by placement type: direct `location_id`, character holder's location via `characters` JOIN, or recursive CTE traversal of `contained_in_object_id` chain (see full spec for details)

## Rationale

**Conceptual uniformity:** Admins learn one model: "everything is an entity with attributes,
policies control access to entities." Characters, locations, objects, and properties all
work the same way in the DSL:

```text
// Same pattern for any entity type
permit(principal is character, action in ["read"], resource is property)
when { resource.name == "wounds" && principal.flags.containsAny(["healer"]) };
```

Option A would require admins to learn two addressing schemes and two sets of DSL patterns.

**Gameplay validation:** The healer-wound scenario was tested during design review:

```text
// Healers can see wounds (but not the character themselves)
permit(principal is character, action in ["read"], resource is property)
when { resource.name == "wounds" && principal.flags.containsAny(["healer"]) };

forbid(principal is character, action in ["read"], resource is property)
when { resource.name == "wounds" && resource.parent_id == principal.id };
```

This works cleanly with first-class properties because the property has its own `parent_id`
attribute. With sub-resources, the parent relationship would need special-case handling in
the DSL evaluator.

**Visibility defaults prevent footguns:** When visibility is set to `restricted`, the Go
property store auto-populates `visible_to = [parent_id]` and `excluded_from = []`. This
prevents the "nobody can see it" scenario where a character sets restricted visibility
without adding anyone to the list.

## Consequences

**Positive:**

- Single entity model for admins — characters, locations, objects, and properties all work
  the same way in policies
- `PropertyProvider` follows the same `AttributeProvider` pattern as all other providers
- `visible_to` and `excluded_from` enable fine-grained access lists without separate ACL
  infrastructure
- SQL JOIN for `parent_location` keeps a single code path for all location data

**Negative:**

- Properties have their own database table, adding schema complexity
- Access control metadata is coupled into the world model struct (intentional but unusual)
- `PropertyRepository` must handle parent-type-dependent JOINs

**Neutral:**

- Property count scales with game content (thousands of properties for an active MUSH is
  expected and manageable)
- The `entity_properties` table has a compound unique constraint on
  `(parent_type, parent_id, name)` preventing duplicate property names per entity

## References

- [Full ABAC Architecture Design — Property Model](../specs/2026-02-05-full-abac-design.md)
- [Design Decision #9: Property Model](../specs/2026-02-05-full-abac-design-decisions.md#9-property-model)
- [Design Decision #18: Property Package Ownership](../specs/2026-02-05-full-abac-design-decisions.md#18-property-package-ownership)
- [Design Decision #32: PropertyProvider Uses SQL JOIN](../specs/2026-02-05-full-abac-design-decisions.md#32-propertyprovider-uses-sql-join-for-parent-location)
