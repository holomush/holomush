<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

<!-- markdownlint-disable MD013 -->
<!-- adr-render: source=bd:holomush-uqrr7; do not edit manually; use `/adr update holomush-uqrr7` -->

# Route scene_publish_* web events as refetch triggers, not payload carriers

**Date:** 2026-06-30
**Status:** Accepted
**Decision:** holomush-uqrr7
**Deciders:** Sean Brandt

## Context

The publish-vote web slice (epic holomush-5rh.24) needed live tally updates in the web portal. The shared host translation path (`internal/web/translate.go::translateEvent`) drops `scene_publish_*` payload fields — only four IC verbs pass its generic allowlist. Even if forwarded, individual `scene_publish_vote_cast` events carry per-ballot data (`character_id`, `vote`, `is_change`), not an aggregate tally; the aggregate `yes/no/pending` exists only in the participant-gated `GetPublishedScene` snapshot. The original Task 6 plan specified a client-side reducer folding live events into a running tally — drain pre-flight (holomush-vgc0t) found this infeasible and privacy-inconsistent with ADR holomush-o8gx8 (split publish read model).

## Decision

Live `scene_publish_*` events are refetch triggers only. The web client extends `workspaceStore.ingestEvent` to dispatch publish frames to a publish store before the IC-log early-return, keyed on `scene_id` from `ev.metadata`. Participants refetch `GetPublishedScene` for the aggregate tally; all roles refetch `GetScene` on lifecycle events for the existence/phase pointer. No `translate.go`, proto, RPC, or `plugin.yaml` changes. This extends ADR holomush-o8gx8's cold-start strategy (snapshot read + live events) to the client-consumption layer.

## Rationale

- `translate.go`'s generic path drops publish payload fields by design; widening it couples the shared host translation layer to publish-domain wire format for no tally gain — the gated aggregate still requires `GetPublishedScene`.
- Client-side reconstruction of the tally from individual ballot events would expose ballot data (`character_id` + `vote`) in the web client and create a divergent data model that cannot resync on reconnect.
- `ingestEvent` is the correct single per-frame chokepoint in the web client for non-IC-log events; dispatching before the early-return keeps the IC log uncontaminated while still reacting to publish frames.
- The participant tally stays authoritative at `GetPublishedScene`, so the privacy boundary is enforced server-side, not by client-side payload filtering.

## Alternatives Considered

- **A — Extend `translate.go` to forward non-sensitive pointer fields (rejected):** saves one `GetScene` hop, but modifies the shared host translation layer (every game event flows through it) for marginal benefit; still cannot forward the gated tally, so `GetPublishedScene` is still required. Wider blast radius and review burden on the common path.
- **B — Events as refetch triggers, wired at `ingestEvent`, no host change (chosen):** zero host/manifest change; the shared `translate.go` path is untouched; aligns with ADR holomush-o8gx8; the privacy boundary is server-enforced. Costs an extra RPC round-trip per triggering event (debounced) and needs `AbortController` for out-of-order responses.
- **C — Client-side tally reducer from `vote_cast` events, the original Task 6 (rejected):** contradicts ADR holomush-o8gx8's read-model split; exposes individual ballot data in the web client; fragile to missed events (wrong tally on reconnect, no resync). Found infeasible by drain pre-flight.

## Consequences

- Positive: no host or manifest changes — the BFF/facade/proto foundation already shipped every RPC needed; privacy boundary enforced by the server-side participant gate, not client-side payload filtering; reconnect resync is automatic via cold-start reload (`GetScene` + `GetPublishedScene`), the failure mode that made Approach C a false-green.
- Negative: an extra RPC round-trip per triggering event (debounce on `vote_cast` mitigates burst cost); out-of-order refetch responses require `AbortController` cancellation to prevent stale-state clobber.
- Neutral: `translate.go`'s generic path is unchanged and publish notices do not enter the IC log; the `ingestEvent` pre-early-return dispatch becomes the established model for future non-IC-log event types in the web client.
