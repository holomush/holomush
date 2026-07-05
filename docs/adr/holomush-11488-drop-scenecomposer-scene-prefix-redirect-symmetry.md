<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-11488; do not edit manually; use `/adr update holomush-11488` -->

# Drop SceneComposer scene-prefix for redirect symmetry

**Date:** 2026-07-05
**Status:** Accepted
**Decision:** holomush-11488
**Deciders:** Sean Brandt

## Context

The web Scene Board composer (`SceneComposer.svelte`) string-built a `scene <verb> <text>` wrapper, diverging from telnet and the web terminal, which already send raw conversational verbs. For holomush-g1qcw (focus-routed scene input), the design made Scene Board symmetric by dropping the wrapper and relying on the server-side dispatcher redirect, explicitly trading away the wrapper's single-membership fallback. Spec: docs/superpowers/specs/2026-07-05-focus-routed-scene-input-design.md §4.6.

## Decision

`SceneComposer` sends raw verbs (e.g. `pose bows`) instead of `scene pose bows`, relying on the Part-1 dispatcher redirect; the composer adds a focus-before-send gate to close the resulting sub-100ms race window (selection sets `selectedSceneId` before `setSceneFocus` resolves).

## Rationale

- Uniform, predictable routing and failure behavior across all three surfaces (telnet, web terminal, Scene Board) is the explicit objective, not an incidental effect — every surface now fails open to the grid location identically.
- The old fallback's resilience was already narrow: `handleEmit`'s single-membership inference rarely resolves a target in a multi-scene workspace.

## Alternatives Considered

- **Symmetric redirect path — drop the scene-prefix (chosen):** an identical conversational path across all three surfaces; removes the last client-side command-shaping wrapper; the server redirect becomes the single routing source of truth. Cost: loses `handleEmit`'s single-membership fallback; requires a new focus-before-send gate.
- **Keep the explicit `scene <verb>` wrapper (rejected):** retains a fallback that can resolve a target even before focus is set in a single-scene workspace, but is surface-specific command-shaping, asymmetric with telnet/terminal, papers over the missing server routing, and the fallback usually cannot resolve a target in a multi-scene workspace anyway. It is the same class of anti-pattern as the rejected client-side sigil-stripping.

## Consequences

- Positive: removes the last surface-specific command-shaping wrapper; future web composer surfaces need no bespoke command-building logic.
- Negative: the composer must add a per-scene focus-ready flag gating sends; loses the old wrapper's brief single-membership fallback window.
- Neutral: the Pose/Say/OOC buttons remain — they select the verb, not the routing target.
