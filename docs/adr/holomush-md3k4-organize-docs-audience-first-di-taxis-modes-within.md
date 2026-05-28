<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-md3k4; do not edit manually; use `/adr update holomush-md3k4` -->

# Organize docs audience-first with Diátaxis modes within

**Date:** 2026-05-28
**Status:** Accepted
**Decision:** holomush-md3k4
**Deciders:** Sean Brandt

## Context

After the Astro Starlight migration (SP1), the docs site needed a new information architecture. Diátaxis recognizes both **mode-first** (tutorials/how-to/reference/explanation at the top level) and **audience-first** (role sections at the top level, modes nested within) as valid, and says user-perception of the product should drive the primary axis. The site was audience-only with no mode differentiation; ~21 docs were orphaned from the hand-maintained sidebar.

## Decision

Organize docs **audience-first** (`guide` / `operating` / `extending` / `contributing` / `reference`) with **Diátaxis modes** (`tutorials` / `how-to` / `reference` / `explanation`) as subfolders within each audience section. The top-level cross-cutting `reference/` section (generated gRPC/events/access-control) stays flat.

## Rationale

- Diátaxis's complex-hierarchies guidance says the primary axis should match how users perceive their role; HoloMUSH readers identify as player / operator / plugin developer / contributor before they identify by task type.
- Audience-first scopes each mode list to a single audience, keeping nav groups ≤7 per Diátaxis's nav-length guidance.
- Audience assignment is unambiguous for virtually every doc; mode-purity within an audience can be improved incrementally (follow-up content-surgery beads) without blocking the IA.
- Mode-first would mix operators and plugin developers inside the same `how-to` group, degrading findability for the site's primary personas.

## Alternatives Considered

- **Mode-first (Diátaxis at root)** — pure Diátaxis, single primary axis; but conflates distinct audiences into shared buckets and contradicts the user-perception guidance for this product. Rejected.
- **Audience-only (status quo)** — no structural change, but no learning/procedure/reference differentiation and no answer to nav drift or the ~21 orphans. Rejected.

## Consequences

**Positive:** every doc lives in exactly one audience/mode bucket (unambiguous placement for new docs); Diátaxis adoption is iterative (structure now, mode-purity later); nav lists stay ≤7 at both levels.

**Negative:** "reference" appears at two levels (top-level cross-cutting section + in-audience mode) — contributors must understand the distinction; path depth +1 for non-index docs (longer slugs).

**Neutral:** the root splash `index.mdx` sits outside all audience dirs (unaffected); mode subfolders are created only where content exists.
