<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 112. Fully-Qualified Attribute Paths in Seed DSL

> [Back to Decision Index](../README.md)

**Question:** Should seed policy DSL use shorthand attribute paths (e.g.,
`resource.id`, `principal.role`) as shown in spec `07-migration-seeds.md`
examples, or fully-qualified paths matching the attribute resolver's storage
format (e.g., `resource.character.id`, `principal.character.role`)?

**Context:** The `07-migration-seeds.md` spec (version 1) illustrates seed
policies with shorthand attribute paths like `resource.id` and `principal.role`.
During Phase 7.3 implementation (attribute resolvers and providers), each
provider stores attributes under a namespace reflecting the entity type:

- `principal.character.role` (not `principal.role`)
- `resource.location.id` (not `resource.id`)
- `resource.object.location` (not `resource.location`)
- `resource.property.visibility` (not `resource.visibility`)

The DSL compiler resolves attribute references against the actual attribute
schema, which requires the fully-qualified paths. Shorthand paths would fail
compilation because no provider registers attributes at those paths.

**Decision:** Seed policy DSL uses fully-qualified attribute paths matching the
resolver storage format. The spec `07-migration-seeds.md` examples represent a
v1 draft that predates the provider implementation; the implementation is
authoritative.

**Rationale:**

- **Correctness:** The compiler validates attribute paths against the schema.
  Shorthand paths are not registered and would fail validation.
- **Consistency:** All attribute references in the system use the same
  fully-qualified format — providers, resolvers, policies, and audit logs.
- **Clarity:** Fully-qualified paths are unambiguous. `resource.id` could refer
  to a character ID, location ID, or object ID; `resource.character.id` is
  explicit.
- **No runtime resolution layer:** Adding a shorthand-to-qualified path
  translation would add complexity without benefit, since the entity type is
  already declared in the policy target (`resource is character`).
