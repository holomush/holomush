<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-etnfd; do not edit manually; use `/adr update holomush-etnfd` -->

# Do not slim plugin-owned skill loads from the repo

**Date:** 2026-07-03
**Status:** Accepted
**Decision:** holomush-etnfd
**Deciders:** Sean Brandt

## Context

A cold-session token audit for holomush-wagqb found two forced full-skill loads at SessionStart — jj (~5K tokens) and grepping (~2.5K). The jj load is the jj plugin's own `session-start-jj-detect` hook (outside `.claude/**`), which already emits a compact safety cheat-sheet (git→jj map, the pre-push rebase `-s`-not-`-r` truncation hazard, the guard-jj-* gates) plus a "REQUIRED: Load jj skill" instruction that drives the full load. The grepping load is driven by a repo-owned script (`require-grepping-skill.sh`).

## Decision

The design MUST NOT slim or override the jj plugin's full-skill SessionStart load from repo-owned config. Only the repo-owned grepping force-load may be replaced with an on-demand cheat-sheet. An upstream request against the jj plugin is the sanctioned future lever for the jj load.

## Rationale

- The jj hook lives in the plugin's own package, not `.claude/**` — the repo cannot alter it without fighting the plugin's control flow, and a repo-only "load on demand" edit would directly contradict the plugin's REQUIRED-load instruction.
- The jj cheat-sheet already duplicates the safety-critical content this design cares about, so there is nothing for the repo to add.
- This establishes a reusable ownership test for any future forced-skill-load or model-tier token-cost work: act only on what `.claude/**` owns.

## Alternatives Considered

- **Repo-side override to suppress/gate jj's full-skill load (rejected):** would save ~5K tokens but contradicts the plugin's REQUIRED-load and reaches outside repo ownership.
- **File an upstream request against the jj plugin (sanctioned future lever):** respects the boundary and addresses the root cause, but is not actionable within this design's `.claude/**` scope.
- **Leave jj untouched; slim only the repo-owned grepping load (chosen):** stays within actual repo ownership; no risk of fighting the plugin.

## Consequences

- Positive: prevents a future repo-side jj slimdown attempt that would contradict the plugin and risk dropping safety content.
- Negative: the jj skill-load cost (~5K tokens/session) remains unaddressed until an upstream fix lands.
- Neutral: grepping's cheat-sheet approach becomes the template for future repo-owned forced-load slimming.
