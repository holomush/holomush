<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 43. Property Lifecycle: Go-Level CASCADE Cleanup

> [Back to Decision Index](../README.md)

**Review finding (I5):** The spec defined `entity_properties` with
`parent_type` and `parent_id` FK columns but didn't address what happens to
properties when their parent entity is deleted. Orphaned rows would accumulate
silently.

**Decision:** Go-level CASCADE in `WorldService` deletion methods:

- `WorldService.DeleteCharacter()` → `PropertyRepository.DeleteByParent("character", charID)`
- `WorldService.DeleteObject()` → `PropertyRepository.DeleteByParent("object", objID)`
- `WorldService.DeleteLocation()` → `PropertyRepository.DeleteByParent("location", locID)`

Both operations happen within the same database transaction. If either fails,
the entire transaction rolls back.

**Rationale:** Database-level `ON DELETE CASCADE` would require a polymorphic
FK (parent_type + parent_id pointing to different tables), which PostgreSQL
doesn't support natively. Go-level cleanup in `WorldService` is explicit,
testable, and consistent with the project's "no database triggers" constraint.
Transactional guarantees prevent orphans without background jobs.
