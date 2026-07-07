<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-gcr2k; do not edit manually; use `/adr update holomush-gcr2k` -->

# Scene info read access: participants, invitees, and open scenes via sibling plugin policies

**Date:** 2026-07-06
**Status:** Accepted
**Decision:** holomush-gcr2k
**Deciders:** Sean Brandt

## Context

internal/access/policy/seed.go seeded 'seed:player-scene-read' as an UNCONDITIONAL permit(character, read, scene). In the OR-of-permits ABAC engine this subsumed the core-scenes plugin's participant-conditioned read-scene-as-participant policy, so ANY character could 'scene info' any scene and read its metadata (owner, state, visibility, participant roster, invitees) regardless of participation — the read twin of the write-side bypass fixed by holomush-8m01u / migration 000047. Removing the seed makes 'scene info' plugin-policy-gated, which raises a UX question the write fix did not have: invitees are not in resource.scene.participants, and open scenes are publicly discoverable (board 'browse' action) and spectatable (spectate-open-scene) but 'scene info' uses the distinct per-scene 'read' action.

## Decision

Remove seed:player-scene-read from SeedPolicies() (fresh installs) and disable it via migration 000048 in existing deployments (exact-DSL + source='seed' compare-and-swap guard, mirroring 000047). Scene info remains accessible to participants + private-scene invitees + anyone for visibility=open scenes, implemented as SIBLING plugin policies mirroring the join-policy shape: read-scene-as-invitee (principal.id in resource.scene.invitees) and read-open-scene (resource.scene.visibility == "open") alongside the existing read-scene-as-participant. Net: 'scene info' denies only for private scenes the caller is neither in nor invited to. Pinned as INV-SCENE-68.

## Rationale

Open scenes' live content is already spectatable by anyone (spectate-open-scene), so hiding their metadata would be incoherent; an invitee may reasonably inspect a scene before accepting an invitation the owner chose to extend. Sibling single-purpose policies (rather than ||-widening read-scene-as-participant) keep each permit auditable in the OR-of-permits engine and mirror the established join-open-scene / join-private-scene-as-invitee shape.

## Alternatives Considered

(1) Participants only (pure 8m01u mirror) — rejected: UX regression; invitees must join blind and open-scene browsers get info denied for scenes whose content they can already watch. (2) Participants + invitees but not open scenes — rejected: incoherent with open visibility (board-browsable, joinable, spectatable, yet info denied). (3) Widening read-scene-as-participant with || clauses — rejected: single-purpose sibling permits are more auditable and match the join-policy convention.

## Consequences

'scene info' on a private scene now denies non-participant non-invitees (metadata privacy restored; the resolver's hard-privacy boundary already excluded log content and vote tallies). Invitee and open-scene info access is preserved. The browse/board path ('browse' action) is untouched. Existing deployments converge via migration 000048; operator-customized policies are left untouched by the exact-DSL guard. INV-SCENE-68 (bound to test/integration/scenes/scene_info_read_access_test.go) prevents regression re-widening.
