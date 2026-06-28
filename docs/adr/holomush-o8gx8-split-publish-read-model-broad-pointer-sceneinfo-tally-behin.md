<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-o8gx8; do not edit manually; use `/adr update holomush-o8gx8` -->

# Split publish read model: broad pointer on SceneInfo, tally behind participant gate

**Date:** 2026-06-28
**Status:** Accepted
**Decision:** holomush-o8gx8
**Deciders:** Sean Brandt

## Context

The publish backend offers two read surfaces: `GetPublishedScene` (participant-gated; returns the full yes/no/pending vote tally) and `GetScene` (broadly readable — any character on an `open` scene, and all FocusMembership holders including observers on a `private` scene). The web portal needs to know whether an in-flight vote is active to gate the Start/Vote/Withdraw affordances on cold-start, and a participant needs the actual tally snapshot before the live IC-event stream catches up. Embedding tally counts in `SceneInfo` would leak participant-confidential vote counts to the wider `GetScene` audience, violating INV-SCENE-60/61.

## Decision

`SceneInfo` gains `active_publish_attempt_id` and `publish_status` (existence/phase only — the in-flight attempt ULID and its FSM phase, no counts). The yes/no/pending tally stays behind the existing participant-gated `GetPublishedScene`, exposed via a pure facade + BFF + client passthrough for a participant's cold-start snapshot. Live deltas reach all scene watchers (incl. observers) via the existing `scene_publish_*` IC stream. Cold-start strategy = snapshot read (participant) + live events (everyone).

## Rationale

- INV-SCENE-60/61 prohibit embedding vote counts in broadly-readable fields; the pointer fields carry no counts, only the attempt ULID and its phase.
- Attempt existence/phase is already low-sensitivity: `scene_publish_*` events stream role-agnostically to all FocusMembership holders on the IC subject (`stream_access.go:101`), so the pointer reveals nothing the event stream does not.
- `GetPublishedScene`'s participant gate is enforced unconditionally in plugin code (`store.go:1448`); the facade passthrough changes no semantics — non-participants receive `PermissionDenied`.
- Decoupling the tally read from the core scene read keeps `GetScene` publish-agnostic and avoids making the broad handler participant-aware for a field unrelated to scene structure.

## Alternatives Considered

- **SceneInfo pointer + participant-gated tally passthrough (chosen):** preserves the privacy split structurally; minimal affordance signal on the broad read; tally stays gated.
- **Embed tally counts in SceneInfo (rejected):** the `GetScene` audience is wider than the participant tally audience — leaks counts across the INV-SCENE-60/61 boundary.
- **Events-only cold-start (rejected):** the IC stream delivers deltas, not a snapshot; a participant arriving after `scene_publish_started` sees a blind gap until the next vote-cast event.
- **Fold GetPublishedScene into WebGetScene (rejected):** couples publish tally into the core scene read and forces conditional participant-awareness inside the broad handler.

## Consequences

- Positive: the existence/phase (broad) vs vote-counts (participant-only) split is enforced at the proto/RPC boundary, not by client-side conditional rendering; `GetScene` stays publish-agnostic; future publish-model changes stay isolated to `GetPublishedScene`.
- Negative: two-RPC cold-start for participants (GetScene then GetPublishedScene); terminal `ATTEMPT_FAILED` reason is live-only and not reconstructed from the pointer on cold-start (PUBLISHED is sourced from the existing archive read).
- Neutral: telnet does not consume the pointer fields (portal affordance-gating only); observers can already reconstruct per-voter ballots from `scene_publish_vote_cast` events — surfaced by the panel but not introduced by this slice.
