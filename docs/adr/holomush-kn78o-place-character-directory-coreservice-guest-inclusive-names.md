<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-kn78o; do not edit manually; use `/adr update holomush-kn78o` -->

# Place character directory on CoreService; guest-inclusive, names-only

**Date:** 2026-06-26
**Status:** Accepted
**Decision:** holomush-kn78o
**Deciders:** Sean Brandt

## Context

The scene-membership Invite picker requires a character directory — a "list all characters by name" surface that did not previously exist in HoloMUSH (no ListAll on auth.CharacterRepository; CoreService.ListCharacters is player-scoped to the caller's own alts). Decisions needed: (1) which service hosts it — CoreService (reachable by guests) vs the SceneAccessService facade (guest-deny, INV-SCENE-64); (2) what it exposes — id+name vs id+name+connection state; and (3) how the read is authorized. The participant-wide invite policy makes guest accessibility a real requirement. The codebase invariant (ADR holomush-lp65) is that every game-state read RPC is gated by a dedicated ABAC action in seed.go so access stays policy-configurable and auditable.

## Decision

CoreService.ListAllCharacters hosts the directory; the SceneAccessService facade is NOT used (it is guest-deny). It returns character id + name only (fetch-all, no pagination); connection/online state is excluded as a separately-permissioned attribute. The read is authorized by a dedicated ABAC action list_character_directory on a new singleton resource character_directory, seeded with a default-permit policy for any authenticated character principal (registered OR guest). The ABAC subject is the acting alt (character_id, ownership-verified server-side); the permit is unconditional so any valid character passes, but the gate keeps enumeration policy-configurable and auditable. The names-enumerable / connection-state-gated boundary is registered as INV-ACCESS-9.

## Rationale

- CoreService is the natural host for game-wide character data; SceneAccessService ownership would couple the directory to scene-specific auth and block guests who are legitimate invite-flow users.
- A dedicated ABAC action (not authentication-only) preserves the lp65 invariant that every read RPC is policy-configurable/auditable; authentication-only would invert the sensitivity ordering (presence-at-a-location is ABAC-gated, but the whole roster would not be) — a design-review finding.
- The default-permit seed keeps the chosen policy (any authenticated incl guest may list names) while leaving operators a Cedar lever to tighten enumeration later.
- id+name is the picker's only need; connection state requires explicit separate grants, so bundling it would silently escalate the read's permission scope.
- Fetch-all (no pagination) is coherent with client-side type-ahead filtering; a paginated server with client-side filter cannot see un-fetched pages. id+name for the whole game is a tiny payload; server-side prefix search is the deferred scale follow-up.

## Alternatives Considered

- **CoreService + dedicated ABAC action (list_character_directory) + default-permit-authenticated seed + fetch-all id+name (chosen):** guest-inclusive, policy-configurable/auditable, coherent with client-side filtering.
- **Authentication-only gate, no ABAC action (rejected — design review):** breaks the lp65 "every read RPC has an ABAC action" invariant and inverts sensitivity ordering vs list_presence; not operator-tunable.
- **SceneAccessService facade host, registered-players-only (rejected):** facade rejects guests (INV-SCENE-64), blocking the invite flow; duplicates guest-rejection policy; couples a general directory to scene auth.
- **Bundle connection/online state into the response (rejected):** conflates low-sensitivity (name) with higher-sensitivity (online status), forcing the stricter gate onto a low-barrier read; the picker has no need for presence.
- **Server-paginated + client-side filter (rejected — incoherent):** type-ahead can only filter the fetched page; mutually exclusive with client-side filtering. Either fetch-all (chosen) or server-side search.

## Consequences

- Positive: any authenticated principal (incl guest) can enumerate character names via a policy-configurable, auditable ABAC action; names vs connection-state permissions stay cleanly separated; CoreService owns the canonical directory with no scene-model coupling; INV-ACCESS-9 pins the boundary so a future is_online field cannot silently bypass the separate gate.
- Negative: all character names are enumerable by any authenticated session (incl guest) — wider exposure than prior player-scoped listing (operators can tighten the seed); fetch-all does not scale to very large character counts without the deferred server-side search.
- Neutral: MVP id+name listing; the scene-character-name-resolution work (holomush-5rh.25) may later supersede or extend it.
