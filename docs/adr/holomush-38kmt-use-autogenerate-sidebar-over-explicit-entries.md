<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-38kmt; do not edit manually; use `/adr update holomush-38kmt` -->

# Use autogenerate sidebar over explicit entries

**Date:** 2026-05-28
**Status:** Accepted
**Decision:** holomush-38kmt
**Deciders:** Sean Brandt

## Context

Starlight supports two sidebar modes: an explicit list of entries (the SP1-inherited 43-entry list in `astro.config.mjs`) and `autogenerate`, which derives the nav from the directory tree. SP1 kept the explicit sidebar to preserve parity during the platform swap. Nav drift — hand-maintained sidebar entries falling out of sync with content — was the root cause of ~21 docs being orphaned from the nav.

## Decision

Replace the explicit sidebar with one `autogenerate: { directory }` group per audience section. Use per-page `sidebar.order` / `sidebar.label` frontmatter for ordering and label overrides where alphabetical/title-cased defaults are wrong.

## Rationale

- `autogenerate` makes nav drift **structurally impossible** rather than lint-guarded: any file placed in a configured directory is in the nav by construction.
- The 43-entry explicit list was a contributing cause of the ~21 orphaned docs; keeping it would require a new lint invariant to prevent recurrence.
- Label/ordering gaps are addressable via Starlight frontmatter — a maintenance cost, not an architectural limitation.
- SP1's reason for the explicit sidebar (parity during migration) no longer applies in SP2, the IA-restructuring pass.
- Flipping to `autogenerate` first (before retire/move) also keeps `astro build` green throughout the re-org: an explicit `{slug}` entry throws `AstroUserError` on a missing target, whereas `autogenerate` re-derives from whatever files exist.

## Alternatives Considered

- **Explicit sidebar (status quo)** — full label/order/grouping control without frontmatter, already in place; but every new doc must be manually added or it is silently orphaned (the exact root cause of the hidden docs), and drift is a recurring bug class needing a CI guard. Rejected.

## Consequences

**Positive:** INV-1 (every non-retired doc reachable) holds by construction, not by test; adding a doc requires only placing it in the right directory; the ~21 orphans surface automatically once placed.

**Negative:** per-page `sidebar.order` frontmatter must be maintained where alphabetical is wrong; multi-word folder labels (e.g. `how-to`) need `sidebar.label`/group-label config to render well.

**Neutral:** the `astro.config.mjs` sidebar field shrinks from 43 entries to 5 autogenerate directives; behavior verified via context7 `/withastro/starlight`.
