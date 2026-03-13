<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 18. Property Package Ownership

> [Back to Decision Index](../README.md)

**Review finding:** The `entity_properties` table was introduced but the spec
didn't clarify whether properties live in `internal/world` (alongside locations
and objects) or in `internal/access/policy/store`.

**Decision:** Properties are world model entities managed by
`internal/world/PropertyRepository`, consistent with `LocationRepository` and
`ObjectRepository`. The `entity_properties` table is part of the world schema.
The `PropertyProvider` in `internal/access/policy/attribute/` wraps
`PropertyRepository` to resolve attributes for policy evaluation.

**Rationale:** Properties have parent entities (characters, locations, objects)
and represent game-world data, not authorization metadata. Placing them in the
world package maintains the separation of concerns: world model stores data,
access control evaluates policies against it.

**Updates [Decision #9](009-property-model.md):** Clarifies the implementation location of the property
model from Decision #9.
