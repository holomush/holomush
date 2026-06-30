<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Publish-vote web: live event delivery design

**Date:** 2026-06-29
**Status:** Draft
**Epic:** holomush-5rh.24 (Scenes web: lifecycle & management actions)
**Design bead:** holomush-5rh.24.41
**Supersedes (planning):** the infeasible Task 6 "live tally reducer" in the prior publish-vote slice plan
**Grounded in:** ADR holomush-o8gx8 (split publish read model); INV-SCENE-60/61/62

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119 / RFC 8174.

---

## 1. Context and problem

The publish-vote web slice (epic holomush-5rh.24) shipped its foundation in
PR #4541 (beads `.33`–`.37`): proto `SceneInfo` publish pointer, facade/BFF
publish RPCs, `GetScene` active-attempt pointer, client wrappers, BFF handlers.
The remaining beads were `.39` (live tally reducer + `publishFlow`), `.40`
(publish panel), and `.38` (telnet-free E2E).

Bead `.39`'s plan (Task 6) specified a **client-side reducer that folds live
`scene_publish_*` events into a running tally**. Drain pre-flight (holomush-vgc0t)
found this **infeasible web-TS-only and, worse, a false-green**:

- `internal/web/translate.go::translateEvent` routes `scene_publish_*` events
  (declared `category: system`) through its **generic path**, which builds
  `webv1.GameEvent.metadata` from a fixed allowlist
  (`label`, `no_space`, `style`, `channel`, `target_name`) plus `scene_id`
  extracted from the cleartext subject. The publish payload fields
  (`attempt_id`, `vote`, `outcome`, tally counts) **match no generic field and
  are dropped**. Only `category == "state"` forwards the whole payload.
- Independently, the events **never carry an aggregate tally** at all. The
  emitted payloads carry *individual* data
  (`scene_publish_vote_cast` = `attempt_id`, `character_id`, `vote`,
  `is_change`). The aggregate `yes/no/pending` tally exists only in the
  participant-gated `GetPublishedScene` snapshot.

A reducer built per Task 6 would therefore pass against fabricated metadata but
**never fold a real event** — and reconstructing a tally client-side from
individual ballots would both leak ballots into the web client and contradict
the accepted read-model split.

## 2. Decision

Adopt the cold-start strategy already decided in **ADR holomush-o8gx8**:
**snapshot read (participant) + live events (everyone)**. Live `scene_publish_*`
events are **refetch triggers**, never tally data. The aggregate tally is always
read from the participant-gated `GetPublishedScene`; existence/phase is read from
the broad `GetScene` pointer (`active_publish_attempt_id`, `publish_status`).

Two concrete decisions finalize the design:

1. **Approach B — refetch-on-event, no host change.** The web client reacts to
   `scene_publish_*` events by refetching the appropriate read surface. There is
   **no `translate.go` payload-forwarding change**, **no new proto/RPC**, and
   **no `plugin.yaml` change** — the foundation already shipped every RPC needed.
2. **Client-side wiring at `ingestEvent`.** The events already reach the web
   client over the alt-session stream but are dropped client-side before they
   reach any publish state (see §3.3); the fix is a small wiring extension in
   `workspaceStore.ingestEvent`, not a host or manifest change.

### 2.1 Observer vs participant UX (product decision)

| Audience | Sees |
| --- | --- |
| **Participant** | Full `yes/no/pending` tally (from `GetPublishedScene`) + FSM phase + affordances (start/vote/withdraw per phase and ownership). |
| **Non-participant observer** (FocusMembership, not a voter) | **Only a binary "a publication vote is in progress" indicator**, derived from the *existence* of `SceneInfo.active_publish_attempt_id`. **No phase detail, no outcome, no counts.** |

The observer indicator MUST surface no information beyond existence. Counts are
structurally unavailable to observers (`GetPublishedScene` returns
`PermissionDenied` for non-participants), and the panel MUST NOT infer or display
phase/outcome for observers even though `publish_status` is broadly readable.

## 3. Architecture

The change is entirely web-TS: a reactive store, an ingestion-wiring extension,
four thin client wrappers, and the panel. No Go host or manifest changes.

### 3.1 `.39` — publish store + event→refetch orchestrator (web-TS)

A Svelte 5 reactive store (`$state`) scoped to the focused scene:

```text
{ activeAttemptId, voteInProgress, phase, tally?, affordances, stale }
```

- **Cold-start load.** `GetScene` → pointer (`active_publish_attempt_id`,
  `publish_status`). If the caller is a participant **and** an attempt is active,
  `GetPublishedScene` → tally. A `PermissionDenied` from `GetPublishedScene` MUST
  be treated as **observer mode** (existence-only), not an error.
- **Event handler.** Receives `scene_publish_*` frames dispatched by the
  `ingestEvent` wiring (§3.3), gated on `scene_id === focusedScene`.
  - lifecycle (`started` / `resolved` / `withdrawn` / `cooloff_started` /
    `vote_attempts_extended`) → refetch `GetScene` (pointer/affordances) and, for
    participants with an active attempt, `GetPublishedScene` (tally).
  - `vote_cast` → **participant**: debounced `GetPublishedScene(activeAttemptId)`;
    **observer**: ignored (no fetch).
- **Debounce.** Rapid `vote_cast` bursts MUST coalesce into a single trailing
  `GetPublishedScene` refetch (~300 ms trailing window; tunable in the plan).
- **Stale-fetch cancellation.** Refetches MUST be cancellable so out-of-order
  responses cannot clobber newer state — `$effect` cleanup aborts the prior
  in-flight request (`AbortController`).
- **Client wrappers + `publishFlow`.** The four thin TypeScript client wrappers
  the store calls — `startScenePublish`, `castPublishSceneVote`,
  `withdrawScenePublish`, `getPublishedScene` — are **not yet present** in
  `web/src/lib/scenes/client.ts` and are a `.39` deliverable (the generated proto
  types already exist; only the wrappers are missing). `publishFlow` start / cast
  / withdraw call these wrappers (the BFF facade RPCs shipped in `.36`/`.37`),
  then refetch — no optimistic tally mutation; the tally always originates from
  the gated snapshot.

The store unit (what it does / how it is used / what it depends on) MUST be
testable in isolation using **Vitest module mocks** of the `client.ts` wrappers
(the `vi.doMock('./client', …)` pattern in
`web/src/lib/scenes/altSessions.test.ts`). The Go `mockGRPCClient` from `.36` is
for the Go BFF handler tests only, not the TypeScript store.

### 3.2 `.40` — publish panel (layout C, context rail)

A reactive component rendering off the `.39` store:

- **Participant view:** tally (`yes/no/pending`), FSM phase, outcome on
  resolution, and affordances (start/vote/withdraw) enabled per phase + ownership.
- **Observer view:** a single "publication vote in progress" badge, shown iff
  `voteInProgress`. The observer view MUST NOT render any numeric count or phase
  string.

### 3.3 Client event wiring (the delivery extension point)

`scene_publish_*` events already reach the web client over the alt-session
stream (`altSessions.openStream` → `workspaceStore.ingestEvent`,
`web/src/lib/scenes/altSessions.svelte.ts:141`) — but `ingestEvent`
(`web/src/lib/scenes/workspaceStore.svelte.ts:218`) **drops them**: it routes
only through `eventFrameToLogEntry` (`web/src/lib/scenes/types.ts`), which maps
the four IC verbs (`scene_pose` / `scene_say` / `scene_ooc` / `scene_emit`) and
returns `null` for everything else. `display_target` is irrelevant on this path —
it is consumed only by the **terminal page** router
(`web/src/lib/stores/eventRouter.ts`), a separate code path the scene workspace
does not use, so **no `plugin.yaml` flip is needed**.

`.39` MUST extend `ingestEvent` to **dispatch `scene_publish_*` frames to the
publish store** (the single per-frame chokepoint) **before its
`if (!entry) return` early-return**, keyed on the `scene_id` stamped in
`ev.metadata` (set by `translate.go`'s `sceneIDFromSubject` for all scene IC
events, so it is available even though the current log path returns before
reading it). The existing `eventFrameToLogEntry`→`null` path is left intact, so
publish notices do **not** enter the IC log (panel-only, per the product
decision). This is the chosen service-boundary mechanism.

### 3.4 `.38` — telnet-free E2E (Playwright)

See §6 (Tier 3).

## 4. Data flow

```text
vote cast (any client)
  → core-scenes emits scene_publish_vote_cast on events.<game>.scene.<id>.ic
    (Sensitive:false)
  → EventBus
  → web BFF translateEvent → GameEvent{type, scene_id}
  → alt-session stream → workspaceStore.ingestEvent
       dispatches scene_publish_* to the publish store (NOT into the IC log)
  → .39 store handler
      participant: debounced GetPublishedScene(activeAttemptId) → tally
      observer:    (vote_cast ignored)
  → .40 panel re-renders
```

Observer lifecycle path: lifecycle event → `GetScene` refetch →
`voteInProgress` flips → badge appears/disappears.

## 5. Privacy invariants

The design MUST preserve the existing boundary; it introduces no new broad
surface.

- **INV-SCENE-60** (scene-log read privacy, plugin-code-enforced) and the
  publish gate it covers: the aggregate tally MUST be obtained only via the
  participant-gated `GetPublishedScene`. The web BFF facade is a pure passthrough
  (no gating semantics added or removed).
- **INV-SCENE-61** (observer fail-closed; "no publish vote"): unchanged — the web
  client gives observers no vote affordance and no counts.
- **INV-SCENE-62** (activity fan-out only to FocusMembership sessions): the live
  refetch triggers ride the existing IC fan-out; non-participant sessions outside
  FocusMembership receive nothing.
- The client MUST NOT reconstruct a tally from individual `vote_cast` ballots.
- **Ballot visibility is pre-existing (ADR holomush-o8gx8, Consequences):**
  individual ballot data (`character_id` + `vote` + `is_change`) already streams
  to all FocusMembership holders on the IC subject. The "no counts to observers"
  guarantee is therefore **aggregate-only client discipline** — the web panel
  never aggregates or displays a tally except from the participant-gated
  `GetPublishedScene` — not a server-enforced gate on individual ballot events.

**New-invariant evaluation (decided at plan time, per `.claude/rules/invariants.md`):**
whether to register a new `INV-SCENE-N` capturing *"no web delivery path ever
surfaces publish vote counts to a non-participant session"*. This MUST consult the
existing INV-SCENE family first and MUST NOT mint an ad-hoc family. If registered,
it ships `binding: pending` and is bound by the Tier-2/Tier-3 tests in §6.

## 6. Test strategy (boundary-first, three tiers)

The privacy boundary (observer never receives counts) MUST be asserted at every
tier, not only in mocked web-TS unit tests.

### Tier 1 — web-TS unit / component (`.39`, `.40`; Vitest module mocks)

The orchestrator and panel MUST have boundary tests covering:

- **`ingestEvent` wiring:** a `scene_publish_*` frame is dispatched to the publish
  store **and** still excluded from the IC log (`eventFrameToLogEntry` returns
  `null`); a `scene_pose` frame still lands in the log and not the publish store.
- `vote_cast` → participant refetches `GetPublishedScene`; **observer ignores it**
  (no fetch issued; no counts enter the store).
- lifecycle events → `GetScene` refetch; observer badge toggles on
  `activeAttemptId` existence ↔ absence.
- **cross-scene isolation:** an event whose `scene_id` differs from the focused
  scene triggers no refetch.
- **debounce:** N rapid `vote_cast` → exactly one trailing `GetPublishedScene`.
- **stale/abort:** out-of-order refetch responses → latest wins.
- **`PermissionDenied`** on `GetPublishedScene` → observer mode (badge), not error.
- **phase-affordance boundary:** vote/withdraw enabled only in `collecting`.
- **participant→observer transition:** a character that loses participation
  mid-vote MUST stop seeing the tally (store drops counts on the next refetch
  returning `PermissionDenied`).
- `.40` rendering: participant DOM shows counts; **observer DOM asserts zero
  numeric counts** across the whole lifecycle.

### Tier 2 — Go integration (`test/integration/scenes/`, Ginkgo + `integrationtest`)

The backend tally gate is already covered (`publish_e2e_test.go`: non-participant
→ `PermissionDenied`; `scene_activity_badge_test.go`: INV-SCENE-62 fan-out). New,
narrow additions MUST cover:

- **live-event → web subscriber:** a `scene_publish_vote_cast` reaches a
  web-style subscriber as `GameEvent{type, scene_id}` (mirror
  `real_scene_join_subscription_test.go`), confirming the events that the `.39`
  `ingestEvent` wiring consumes actually arrive on the web stream.
- **facade-layer tally gate:** re-assert participant-gets-tally /
  non-participant-`PermissionDenied` through the BFF facade path, carrying
  `// Verifies: INV-SCENE-60` (and `61` where genuinely asserted).

### Tier 3 — Playwright E2E (`web/e2e/scene-publish-vote.spec.ts`, telnet-free)

Using the multi-context `{ browser }` pattern (`multi-tab-session.spec.ts`):

- **Participant + observer on one scene:** participant starts → casts → the
  **tally updates live** in the participant context; the **observer context shows
  only "vote in progress" and asserts zero counts** in its DOM through the whole
  lifecycle; on resolution each role sees the role-appropriate state.
- **Reconnect resync:** the participant reloads mid-vote and the panel
  re-renders the correct current tally from cold-start — proving the missed-event
  recovery that the rejected reducer approach lacked.

## 7. Error handling

- `GetPublishedScene` `PermissionDenied` ⇒ observer mode (badge), never surfaced
  as an error.
- Refetch failure (transport/Internal) ⇒ retain last-known tally, set `stale`,
  retry on the next triggering event.
- Out-of-order refetch responses ⇒ aborted via `$effect` cleanup; latest wins.
- Reconnect / resubscribe ⇒ full cold-start reload (`GetScene` +
  `GetPublishedScene`) to resync, recovering any events missed while disconnected.

## 8. Alternatives considered

- **A — forward `attempt_id`+`status` via `translate.go` (rejected).** Extend the
  generic path to forward the non-sensitive pointer fields for `scene_publish_*`
  events. It still cannot forward the gated tally, so participants STILL call
  `GetPublishedScene`; it saves only the occasional `GetScene` hop the client
  already has from cold-start/`started`. It modifies the shared host translation
  path (every event flows through it → wider blast radius and review burden) for
  marginal benefit. Not worth the surface.
- **C — client-side tally reducer from `vote_cast` (rejected; the original Task 6).**
  Contradicts ADR holomush-o8gx8, relies on/exposes individual ballots in the web
  client, and is fragile to missed events (wrong tally on reconnect, no resync).
  This is the false-green path drain pre-flight caught.

## 9. Work breakdown (re-plan; `plan-to-beads` materializes)

- `.39` — publish store + event→refetch orchestrator + the four TypeScript
  `client.ts` wrappers + the `ingestEvent` wiring extension (§3.3) + Tier-1
  unit/boundary tests.
- `.40` — publish panel (layout C) + Tier-1 component/boundary tests.
- **New task** — Tier-2 Go integration additions (live-event → web subscriber;
  facade-layer tally gate) in `test/integration/scenes/`. No host or manifest
  change.
- `.38` — Tier-3 Playwright E2E (participant + observer, live tally, reconnect
  resync, observer-no-counts).

Dependency shape (proto/facade/BFF/client foundation already merged): `.40`
depends on `.39`; `.38` depends on `.39`, `.40`, and the new integration task;
the integration task is independent of the web-TS work and MAY land in parallel.

## 10. Out of scope (YAGNI)

- No `translate.go` payload forwarding (Approach B, not A).
- No `plugin.yaml`/`display_target` change — the events already reach the web
  client; the fix is client-side `ingestEvent` wiring (§3.3). Surfacing publish
  notices in the web *terminal* page (via `display_target: both`) or as entries in
  the scene IC log is a possible separate follow-up, deliberately out of scope.
- No client-side tally reconstruction.
- No new proto/RPC — the foundation shipped them.
- Terminal `ATTEMPT_FAILED` live-only reason is not reconstructed on cold-start
  (accepted consequence of ADR holomush-o8gx8).

## 11. Open items deferred to planning

- **New invariant decision** (§5) — register an INV-SCENE-N for the web-delivery
  no-counts guarantee, or rely on the existing family.
- **Debounce window** (~300 ms trailing) — confirm against UX feel during impl.
<!-- adr-capture: sha256=2488ced23c3012e8; session=cli; ts=2026-06-30T01:03:53Z; adrs=holomush-uqrr7 -->
