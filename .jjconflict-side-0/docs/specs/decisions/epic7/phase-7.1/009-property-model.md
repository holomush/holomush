<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 9. Property Model

> [Back to Decision Index](../README.md)

**Question:** How should per-property access control work?

**Options considered:**

| Option | Description                        | Pros                                              | Cons                                                               |
| ------ | ---------------------------------- | ------------------------------------------------- | ------------------------------------------------------------------ |
| A      | Properties as sub-resources        | Minimal world model change, path-based addressing | Two concepts (entities vs sub-resources), implicit type derivation |
| B      | Properties as first-class entities | Conceptual uniformity, one mental model           | More DB rows, properties need own table                            |
| C      | Property flags only (no ABAC)      | Simplest                                          | Loses ability to write rich policies on properties                 |

**Discussion:** Sean initially leaned toward B but questioned whether it was
over-engineering. Analysis of the admin experience showed that option A
introduces a second-class citizen (sub-resources) requiring admins to understand
two different resource models. Option B keeps everything uniform: characters,
locations, objects, and properties are all entities evaluated by the same engine.

**Decision:** **Option B — Properties as first-class entities.**

**Rationale:** Properties already need storage (name, value, parent entity).
Adding `owner`, `visibility`, and `flags` columns is minimal overhead. Admins
learn one model: "everything is an entity with attributes, policies control
access to entities." The healer-wound example and faction-backstory scenarios
both validated this approach.

**Key scenario tested:** A property of a character visible only to a group of
other characters (healers) but NOT to the character it belongs to. This works
cleanly with first-class properties:

```text
permit(principal is character, action in ["read"], resource is property)
when { resource.name == "wounds" && principal.flags.containsAny(["healer"]) };

forbid(principal is character, action in ["read"], resource is property)
when { resource.name == "wounds" && resource.parent_id == principal.id };
```
