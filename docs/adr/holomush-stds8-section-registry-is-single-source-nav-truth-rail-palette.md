<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-stds8; do not edit manually; use `/adr update holomush-stds8` -->

# Section registry is the single source of nav truth (Rail + palette)

**Date:** 2026-06-20
**Status:** Accepted
**Decision:** holomush-stds8
**Deciders:** Sean Brandt

## Context

The web client's left Rail and ⌘K CommandPalette had no shared source of navigation items. Rail items were hardcoded in `terminal/Rail.svelte` (Room hardcoded active; DM/Map/Notes disabled placeholders) and the palette had no section-switching entries at all. As the workspace grows to several authed sections (scenes, DMs, forum, wiki, profiles), adding one would require independent edits in two unrelated surfaces — guaranteeing drift between what the Rail shows and what the palette can jump to.

## Decision

A typed `WorkspaceSection[]` registry (`web/src/lib/nav/sections.ts`) is the sole authority for both Rail items and palette "go to <section>" entries. The Rail and the palette MUST derive their items and route-driven active state from this registry and MUST NOT hardcode section lists independently. The registry is pure TypeScript (no Svelte/icon imports); the Rail maps section `id` to a lucide icon locally.

## Rationale

- Eliminates the class of drift bugs where the palette and Rail disagree on available sections.
- The registry is pure data, so it unit-tests in the plain vitest project without component mounting.
- The growth path is explicit: a future section (DMs, Forum) is one registry entry plus its route, and it appears in both surfaces automatically.
- Active state is a per-section prefix predicate, so nested routes (`/scenes/[id]`) resolve to the parent section uniformly across Rail and palette.

## Alternatives Considered

- **Declarative registry feeding both surfaces (chosen):** single source of truth; one edit per new section; testable as plain TS.
- **Independent hardcoded lists in Rail and palette (rejected):** no abstraction cost, but the lists drift, every new section needs ≥2 edits in unrelated files, and the rail-as-section-switcher goal is unreachable without coupling anyway.

## Consequences

- Positive: Rail and palette cannot disagree on sections; future section authors touch one file.
- Negative: a small indirection — component authors must read the registry to know what the Rail renders.
- Neutral: the Rail's bottom ⚙ view-prefs control is pinned outside the registry list and stays a Rail-internal concern; the vestigial `RailView` type and its placeholders are retired.
