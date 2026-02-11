<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# 100. Standardize Dependency Format Across Phase Files

> [Back to Decision Index](../README.md)

**Date:** 2026-02-10
**Phase:** General (all phases)
**Status:** Accepted

## Context

The 7 ABAC implementation plan phase files (7.1-7.7) evolved over multiple
editing sessions, resulting in inconsistent dependency declaration formats:

- **Phase 7.1:** Mix of inline prose (`**Dependencies:** Requires Task 0.5
  completion`) and missing fields
- **Phase 7.2:** No explicit dependency fields on any task
- **Phase 7.3:** Mix of no field, blockquote (`> **Depends on:`), and note
  format
- **Phase 7.4:** Structured bullet lists with markdown links
- **Phase 7.5:** Structured bullets with separate `**Cross-Phase
  Dependencies:**` field
- **Phase 7.6:** Structured bullets with markdown links
- **Phase 7.7:** Structured bullets with markdown links

This inconsistency makes it difficult to audit the dependency graph, grep for
dependency relationships, or mechanically validate that all dependencies are
declared.

## Decision

Standardize all dependency declarations to a single format across all 7 phase
files:

### Format

```markdown
**Dependencies:**

- Task N (Phase 7.X) — rationale for why this dependency exists
```

### Rules

1. **Field name:** Always `**Dependencies:**` (bold, followed by colon)
2. **Bullet format:** `- Task N (Phase 7.X) — description` using plain text
   phase reference (no markdown links)
3. **No dependencies:** `**Dependencies:** None` on a single line, or
   `**Dependencies:** None (can start immediately)` for first tasks in a phase
4. **Same-phase deps:** `- Task N (Phase 7.X) — description` (include phase
   even for same-phase references, for grep-ability)
5. **Deferred phase:** `**Dependencies (deferred — applies when Epic 8 work
   begins):**` for Phase 7.5 tasks
6. **Cross-phase deps:** Merged into the main `**Dependencies:**` bullet list
   (no separate `**Cross-Phase Dependencies:**` field)
7. **Inline prose refs:** Markdown links to phase files in non-dependency
   prose text (design notes, acceptance criteria) are left as-is

### Task Numbering Convention (documented, not changed)

The plan uses three numbering schemes:

| Scheme       | Pattern        | Example                  | Usage                            |
| ------------ | -------------- | ------------------------ | -------------------------------- |
| Integer      | `T<N>`         | T0, T1, T7               | Primary tasks                    |
| Decimal      | `T<N>.<M>`     | T0.5, T17.1-T17.4, T28.5 | Sub-tasks within a logical group |
| Alphanumeric | `T<N><letter>` | T4a-c, T16a-b, T22b      | Variant/parallel tasks           |

Hybrid forms exist (e.g., `T27b-1`, `T27b-2`) for deeply nested sub-tasks.
No renaming was performed; this documents existing convention.

## Alternatives Considered

1. **Markdown link format** (`Task N ([Phase 7.X](./file.md))`): More
   navigable in rendered markdown but inconsistent when some files use links
   and others do not. Plain text is more grep-friendly and simpler to maintain.

2. **Separate cross-phase field:** Phase 7.5 used a `**Cross-Phase
   Dependencies:**` field. Merging into the main list reduces field
   proliferation and keeps all dependency information in one place.

3. **Omit phase for same-phase deps:** Considered but rejected. Including the
   phase reference on every bullet enables consistent `grep "Phase 7.3"`
   queries regardless of which file you are searching.

## Consequences

- All ~55 tasks across 7 phase files now use identical dependency format
- Dependency graph can be extracted by grepping for `^- Task \d`
- Phase cross-references are plain text, reducing link rot if files are renamed
- Inline prose references retain markdown links for navigation convenience
