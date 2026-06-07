<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-wf4zj; do not edit manually; use `/adr update holomush-wf4zj` -->

# Use Player-Scoped Workspace Over Terminal Focus-Pivot for Scenes

**Date:** 2026-06-07
**Status:** Accepted
**Decision:** holomush-wf4zj
**Deciders:** Sean Brandt (seanb4t)

## Context

The web scenes surface needed a model for live scene participation. An early design proposed pivoting the terminal's focus to a scene stream, mirroring the MUSH convention of refocusing a connection's event stream. The brainstorm identified AresMUSH's connection-stealing failure mode — focusing a scene steals the connection from the terminal, breaking the three required player states (terminal-active, concurrent scenes, scenes-only).

## Decision

The scenes web portal is a player-scoped workspace at `/scenes`, a sibling of `/terminal` in the left icon-rail. It MUST NOT mutate the terminal connection's focus, its stream, or the session-level PresentingFocus. Workspace connections use their own `comms_hub` ClientType.

## Rationale

- AresMUSH's focus-stealing failure mode is structurally prevented by isolating workspace connections under their own comms_hub ClientType.
- Phase 5 D9 already enforces that comms_hub connections never touch PresentingFocus, so the workspace piggybacks on an existing safety invariant.
- All three player states (terminal-active, concurrent multi-scene, scenes-only) are simultaneously first-class without session-model modification.
- Writes still go through HandleCommand on the per-alt session, preserving command-dispatch ABAC gates by construction.

## Alternatives Considered

**Terminal focus-pivot (shell integration)** — rejected. Reuses SetConnectionFocus machinery directly with no new surface, but steals the terminal connection's focus/PresentingFocus, breaking the concurrent-scenes and terminal-untouched requirements. AresMUSH shipped exactly this and loses connections routinely.

**Player-scoped workspace with its own comms_hub connections (chosen)** — terminal untouched; coordinator and filter-at-delivery invariants unchanged; telnet parity preserved. Costs: per-alt session establishment, comms_hub semantics (arrive-emit skip, presence exclusion).

## Consequences

- Positive: terminal session guaranteed undisturbed; INV-SCENE-38/60 untouched; scenes-only players need no terminal session.
- Negative: workspace lazily establishes one character session per participating alt; SelectCharacter needs client_type=comms_hub to skip the arrive emit; ListActiveByLocation must count only terminal/telnet-connected sessions.
- Neutral: GetConnectionFocus PluginHostService RPC (+ Lua hostfunc) added for focus-aware pose routing, completing a Phase 5 TODO.

Spec: docs/superpowers/specs/2026-06-07-web-portal-scenes-design.md §3 D1. Bead: holomush-5rh.8.
