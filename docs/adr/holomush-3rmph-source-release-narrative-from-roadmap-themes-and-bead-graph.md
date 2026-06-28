<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-3rmph; do not edit manually; use `/adr update holomush-3rmph` -->

# Source release narrative from roadmap themes and bead graph, not raw-commit clustering

**Date:** 2026-06-28
**Status:** Accepted
**Decision:** holomush-3rmph
**Deciders:** HoloMUSH Contributors

## Context

A narrative TLDR needs the "why" behind a release, not just the "what". Raw
commit subjects are a mechanical filtered list. The project already curates
`theme:<slug>` roadmap sections (human-written "why") and a bead epic graph
referenced from ~77% of commit subjects (139/180 in v0.9.0..v0.10.0).

## Decision

Narrative is sourced in priority order: `theme:*` roadmap sections (the why),
closed bead epics mapped via commit `holomush-<id>` refs (per-theme bullets),
and filtered commit subjects as fallback and coverage cross-check. Every commit
is accounted for — unmapped ones land under an "Other changes" catch-all with a
visible coverage warning; nothing is silently dropped.

## Rationale

- Roadmap sections are already human-written narrative — reusing them avoids re-authoring the "why" from scratch.
- ~77% bead-reference density in v0.9.0..v0.10.0 makes the graph a reliable primary source.
- The coverage guarantee ensures no commit is silently dropped; the "Other changes" catch-all handles the remaining ~23%.
- Degrades gracefully: if bd is unavailable, falls back to commit-subject-only with a visible warning.

## Alternatives Considered

- **Themes + bead graph + commit fallback (chosen):** reuses curated "why"; high bead-ref density; graceful degradation; coverage cross-check.
- **LLM clustering of raw commit subjects (rejected):** fully automated but commit subjects carry "what" not "why"; clustering is unreliable for RP-domain vocabulary; low-quality narrative without curated context.

## Consequences

- Positive: narrative quality is bounded by the project's existing curation investment; bead and roadmap discipline pays off at release time.
- Negative: quality depends on ongoing theme/bead curation; sparse tagging produces weaker notes.
- Neutral: the commit-subject fallback mirrors what GoReleaser already produces.
