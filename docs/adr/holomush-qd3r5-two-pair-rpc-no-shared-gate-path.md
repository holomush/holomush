<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# Participant-Gated and Public Scene Publication RPCs Use No Shared Code Path

**Date:** 2026-05-23
**Status:** Accepted
**Decision:** holomush-qd3r5
**Deciders:** HoloMUSH Contributors
**Related:** [`holomush-c8a9`](holomush-c8a9-scene-privacy-plugin-code-enforcement.md), [`holomush-nt2d`](holomush-nt2d-participant-gate-pattern-generalized.md), [`holomush-c4jee`](holomush-c4jee-inv-p6-6-structural-enforcement.md)

## Context

Phase 6 introduces two read surfaces for the scene publication artifact:

1. **Participant-gated reads** — `GetPublishedScene`, `DownloadPublishedScene`, `ListScenePublishAttempts` — return full state (metadata, vote progress, content if PUBLISHED) only to scene participants. INV-S9 (substrate-contract §4.1) requires plugin-code enforcement of the participant check; ABAC engine MUST NOT be in the read path. Pattern established by ADR `holomush-c8a9` and generalized by `holomush-nt2d`.

2. **Public reads** — `GetPublicSceneArchive`, `DownloadPublicSceneArchive` — return the publication artifact (content + frozen participants snapshot + title) to anyone, but ONLY when `published_scenes.status == PUBLISHED`. Non-PUBLISHED rows return opaque `NOT_FOUND` to prevent leaking attempt-in-progress state to non-participants (INV-P6-8).

The two surfaces share the same backing table (`published_scenes`) and serve content from the same JSONB `content_entries` column. The implementation question: should they share helper code?

Two approaches were considered:

- **Shared core helper** — `getPublishedSceneCore(ctx, id, gatePolicy)` with `gatePolicy ∈ {participant, public}`, called by both handler pairs. Single read path, minimal duplication.
- **Structurally separate handlers** — `GetPublishedScene` and `GetPublicSceneArchive` are independent functions with no shared helpers beyond read-only store methods. Distinct response assemblers, distinct proto request/response messages, distinct error code surfaces.

## Decision

The participant-gated and public RPC pairs are implemented as **structurally separate handlers with no shared gate helper, no shared response assembler, and no flag-driven routing**. Each handler reads the store directly via `GetPublishedSceneHeader` and (conditionally) `GetPublishedSceneContent`, but the call graph from RPC entry point to response assembly is distinct between the two pairs.

The proto contract reinforces the separation:

- Distinct RPC names (`GetPublishedScene` vs `GetPublicSceneArchive`).
- Distinct request types (the participant request carries `caller_character_id`; the public request does not).
- Distinct response types (the participant response includes vote progress + per-attempt metadata; the public response includes only the publication artifact's frozen fields).

The implementation is constrained:

- There is no `getPublishedSceneCore(ctx, id, gatePolicy)` helper.
- There is no shared `assembleResponse(...)` that branches on policy.
- The two read paths may evolve independently (e.g., the public path may add caching; the participant path may add per-vote-state filtering) without coupling.

Acceptable duplication: both handlers call `GetPublishedSceneHeader` and `GetPublishedSceneContent` (read-only store methods), and both encode their respective response proto messages. The duplication is at the assembly layer, not the read layer.

## Rationale

**The gate must be structural, not behavioral.** INV-S9 enforces that no ABAC override exists for scene reads. A shared helper with a `gatePolicy` flag turns the participant/public boundary into a runtime choice — a flag flip during refactor (or a subtle bug in flag derivation) silently leaks private content. Separate handlers make the access tier visible at the call site: anyone reading `GetPublicSceneArchive` can see immediately that no participant check fires; anyone reading `GetPublishedScene` can see the `IsParticipant` call inline.

**The cost of duplication is paid where it surfaces correctly.** Adding a new access tier (guild-gated reads, time-windowed reads) is design-time work that should require new handler code — not a runtime parameter passed through an existing helper. Code review can verify the gate inline without tracing through a shared helper's branches. The two handlers grow independently: one may add per-vote-state filtering for participants; the other may add anonymous read caching. Coupling would block each from evolving.

**Combines with structural INV-P6-6 enforcement.** Per ADR `holomush-c4jee`, the no-ABAC-engine invariant is verified by AST + reflect structural checks. Together with this ADR's no-shared-path rule, INV-S9 regression resistance becomes a multi-layer property: even if one layer is compromised by a future refactor, the other catches it.

## Alternatives Considered

**Option A: Structurally separate handlers with no shared gate helper (chosen).**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Gate is a structural property of each handler; flag-flip refactor cannot leak content; access tier is visible at the call site; each handler may evolve independently |
| Weaknesses | Visible code duplication at the response-assembly layer; new access tier requires a new handler (paid at design time, not runtime) |

**Option B: Shared `getPublishedSceneCore(ctx, id, gatePolicy)` helper called with policy flag.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Minimal duplication; single read path; new access tier is a new flag value |
| Weaknesses | A flag flip during refactor silently turns a participant-only RPC public; the gate becomes a runtime parameter rather than a structural property; INV-S9 enforcement depends on every caller passing the correct flag |

**Option C: Middleware-style decorator wrapping a single core read.**

| Aspect | Assessment |
| ------ | ---------- |
| Strengths | Common in HTTP frameworks; familiar to web developers; gate logic is one composable layer |
| Weaknesses | The core read remains a single function — same flag-driven-gate risk as Option B once any decorator is bypassed; the proto contract still needs distinct request/response messages; complexity added for negligible reuse |

## Consequences

- **Regression resistance.** A future contributor cannot accidentally publish private content by refactoring a shared helper. The gate is a structural property of the handler, not a runtime parameter.
- **Independent evolution.** Adding a third access tier (e.g., guild-gated reads) requires a new handler pair, not a new flag value. The cost is felt at design time, where it surfaces correctly; not at runtime, where flag-routing bugs would surface.
- **Explicit code duplication.** The two assembler functions and response message types are visibly distinct, accepted as the cost of regression resistance. Code review can verify the gate is present in each participant handler without tracing through a shared helper's branches.
- **Structural INV-P6-6 enforcement.** Combined with ADR `holomush-c4jee` (AST import scan + reflect field check), this decision makes "no ABAC engine in the participant read path" enforceable at CI time without runtime injection seams.

## References

- [Scenes Phase 6 design spec](../superpowers/specs/2026-05-23-scenes-phase-6-logs-vote-privacy-design.md) §5.1, §9.1, §9.2
- [Scenes Phase 6 implementation plan](../superpowers/plans/2026-05-23-scenes-phase-6-logs-vote-privacy.md) Task B5 (GetPublishedScene), Task C4 (GetPublicSceneArchive)
- [Substrate-contract spec](../superpowers/specs/2026-05-16-social-spaces-substrate-contract.md) INV-S9
- Design bead `holomush-5rh.20` (brainstorm Q8 — read access by status)
