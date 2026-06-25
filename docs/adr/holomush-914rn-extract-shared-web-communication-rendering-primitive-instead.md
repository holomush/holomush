<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-914rn; do not edit manually; use `/adr update holomush-914rn` -->

# Extract a shared web communication rendering primitive instead of per-bug patching

**Date:** 2026-06-26
**Status:** Accepted
**Decision:** holomush-914rn
**Deciders:** Sean Brandt

## Context

Two web scene bugs (holomush-5rh.32 composer, holomush-5rh.33 PoseCard) shared one root cause: the scene surface reimplemented communication rendering that the terminal already does correctly, and drifted — PoseCard used `--brand-cyan-*` instead of `--mush-*` tokens and had no canonical `{actor} says, "{text}"` phrasing. A minimal fix patches PoseCard alone; the same drift recurs for every future communication surface (forums, channels, discord, the jsonl-export viewer). (The composer half, holomush-5rh.32, was subsequently split into the focus-routed-input effort holomush-g1qcw; this decision governs the rendering seam.)

## Decision

Extract a shared communication-rendering seam: a normalized `CommLine` model, a single `CommunicationLine.svelte` presentation primitive (canonical phrasing + `--mush-*` tokens + `linkUrls`), and per-vocabulary adapters (`commEventToLine`, `logEntryToLine`). The terminal `CommunicationRenderer` and the scene `PoseCard` both thin to adapter + primitive and delegate all phrasing to it.

## Rationale

- Both bugs were the same drift: surfaces re-derived phrasing instead of sharing the canonical version.
- Every future surface hits the same fork without a structural boundary.
- A minimal fix offers no enforcement; the SEAM-1 parity test mechanically prevents re-divergence, and the Go↔TS golden (SEAM-4) pins the TS primitive against the server renderer.
- New surfaces adopt the seam by writing a ~15-line adapter instead of re-deriving phrasing and tokens.

## Alternatives Considered

- **Minimal per-bug fix (rejected):** narrowest changeset, but no structural enforcement — drift recurs for every new surface.
- **Hybrid — share recognition, copy rendering (rejected):** leaves phrasing duplicated across terminal and scene.
- **Full shared seam (chosen):** one primitive + model + adapters; mechanically guarded against drift.

## Consequences

- Positive: terminal and scenes render identically from one primitive; new surfaces plug in cheaply; Go↔TS parity becomes testable.
- Negative: `CommLine` is a shared boundary — breaking it breaks all consumers.
- Neutral: `CommunicationRenderer.svelte` and `PoseCard.svelte` become thin chrome wrappers around the shared body.
