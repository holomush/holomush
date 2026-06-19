<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-x8swp; do not edit manually; use `/adr update holomush-x8swp` -->

# Resolve window-per-scene as single-pane workspace; defer multi-window

**Date:** 2026-06-20
**Status:** Accepted
**Decision:** holomush-x8swp
**Deciders:** Sean Brandt

## Context

E9.5 D4 specified "window-per-scene routing (web)" as a requirement. The shipped E9.5 portal instead delivered a single-pane scene-switcher workspace (`web/src/lib/scenes/workspaceStore.svelte.ts`, `selectedSceneId` + 3-pane layout) with unread badges and quick-switch, approved via the E9.5 visual-companion mockups (v3, D11). The bead holomush-5rh.22 carried this as residual R1 requiring a recorded disposition.

## Decision

The single-pane workspace with quick-switch and unread badges satisfies D4's intent ("the web renders threads"). True multi-window / pop-out per scene is NOT built and is NOT planned. This is recorded as resolved-by-evolution; no code implements R1.

## Rationale

- The D11 visual-companion mockups explicitly approved the single-pane model over literal multi-window during E9.5 design.
- The single-pane workspace already covers the core "threads rendered in the web" intent of D4 via unread badges and quick-switch.
- No current epic or bead targets true pop-out multi-window; recording the disposition prevents future contributors from re-litigating a settled question.

## Alternatives Considered

- **True multi-window / pop-out per scene (rejected):** matches the literal D4 spec text (each scene in its own window/tab), but it was not built in E9.5, is not planned in any bead, and adds significant routing and state-sync complexity; the D11 mockups approved the single-pane approach instead.
- **Single-pane scene-switcher workspace — resolved-by-evolution (chosen):** already shipped and approved; satisfies D4's intent; simpler state model already integrated with `selectedSceneId` and the 3-pane layout.

## Consequences

- Positive: D4 residual R1 is formally closed with a recorded rationale; future contributors have a clear signal that multi-window is a deliberate non-goal, not an oversight.
- Negative: players who want true simultaneous side-by-side scene windows cannot have it under this model.
- Neutral: no code implements R1 in this slice; this ADR is documentation only.
