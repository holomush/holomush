<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Publish-Vote Web — Interactive Controls Design

| | |
| --- | --- |
| **Bead** | `holomush-5rh.24.41.9` (design) → epic `holomush-5rh.24.41` → `holomush-5rh.24` |
| **Status** | DRAFT — pending design-reviewer |
| **Theme** | `theme:web-portals`, `theme:social-spaces` |
| **Master spec** | [2026-06-28-scenes-web-publish-vote-actions-design.md](2026-06-28-scenes-web-publish-vote-actions-design.md) §7 (UI); [2026-06-29-publish-vote-web-live-event-delivery-design.md](2026-06-29-publish-vote-web-live-event-delivery-design.md) (delivery foundation) |
| **ADRs respected** | `holomush-v4qmu` (structural writes → typed RPC, not command path); `holomush-uqrr7` (route `scene_publish_*` web events as refetch triggers, not payloads) |
| **Reviewers** | `design-reviewer` REQUIRED; `code-reviewer` REQUIRED (web). `abac-reviewer` / `crypto-reviewer` NOT required — no `internal/access/`, facade, proto, or Go change. |

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are to
be interpreted as in RFC 2119 / RFC 8174 (root `CLAUDE.md`).

## 1. Goal

Make the publish-vote web feature **user-drivable and reactive** in the browser:
from the GUI a scene participant can **start** a publication vote, cast/change a
**Yes/No** vote, and the owner can **withdraw** an in-flight attempt — over the
typed BFF RPC wrappers that already exist. This is the integration glue that the
`.41.1`–`.41.6` slices (client wrappers, store, panel, ingest wiring, delivery
proof, all landed in PR #4562) and the cold-start wiring (`.41.10`) built the
pieces for. It unblocks the Tier-3 Playwright E2E (`holomush-5rh.24.41.7`).

## 2. Current state (grounded)

The read path is **complete**; the gap is the **write controls** (gap 2 of the
`.41.9` rescope — gap 1, cold-start wiring, landed as `.41.10`).

| Capability | Where | Status |
| --- | --- | --- |
| Write client wrappers `startScenePublish` / `castPublishSceneVote` / `withdrawScenePublish` | `web/src/lib/scenes/client.ts:299/307/315` | exist (typed BFF RPC) |
| Gated read `getPublishedScene` (signal-abortable) | `web/src/lib/scenes/client.ts:329` | exists |
| `publishStore` cold-start + event-driven aggregate-tally refetch | `web/src/lib/scenes/publishStore.svelte.ts` | exists; **read-only** (no caller-vote state, no write actions) |
| Cold-start wired into scene select | `web/src/lib/scenes/workspaceStore.svelte.ts:162` (`loadColdStart`) | exists (`.41.10`) |
| `ScenePublishPanel` (tally render) | `web/src/lib/components/scenes/ScenePublishPanel.svelte` | exists; renders only when `publishStore.voteInProgress`; **no controls**, takes no props |
| `SceneContextRail` lifecycle action group + panel mount | `web/src/lib/components/scenes/SceneContextRail.svelte:116-142` | exists; actions shown for `active`/`paused` only — **no actions for `ended` scenes** |
| `lifecycleFlow.ts` action pattern | `web/src/lib/scenes/lifecycleFlow.ts` | exists — the template `publishFlow.ts` mirrors |
| Facade + BFF publish writes (engine-excluded) | `internal/grpc/sceneaccess_service.go`, `internal/web/scene_handlers.go` | exist (PR #4562); **unchanged by this slice** |

### 2.1 Two load-bearing backend constraints (grounded)

These bound what the UI can do and **MUST** be respected by the design:

1. **No caller-vote, no per-voter data on the read.** The client reads
   `WebGetPublishedSceneResponse` (`api/proto/holomush/web/v1/web.proto:1281`;
   generated `web/src/lib/connect/holomush/web/v1/web_pb.ts:2854`, consumed as
   `snap.voteSummary` at `publishStore.svelte.ts:108`), whose `vote_summary` reuses
   `PublishedSceneVoteSummary` — only `yes`/`no`/`pending`
   (`api/proto/holomush/scene/v1/scene.proto:721`). It has **no** field for the
   caller's own ballot and **no** per-voter list (the backend
   `GetPublishedSceneResponse`, `scene.proto:742`, is likewise aggregate-only).
2. **Live events carry no payload to the web.** `scene_publish_*` events are
   `category:system`; the BFF (`internal/web/translate.go`) forwards only
   `scene_id` for non-state events (ADR `holomush-uqrr7` — events are *refetch
   triggers*, not payloads). The web therefore **cannot** learn who voted or what
   the caller voted from either the snapshot or the stream.

**Consequences:** "highlight the caller's current vote" is sourced from **local
optimistic state**, not the server; a **per-voter ballot list is infeasible**
without a backend change and is out of scope (§4).

## 3. Scope

**In scope:**

- A new `web/src/lib/scenes/publishFlow.ts` with three thin actions
  (`startPublishAction`, `castVoteAction`, `withdrawAction`) over the existing
  client wrappers, mirroring `lifecycleFlow.ts`.
- `publishStore` additions for the caller's confirmed + in-flight vote
  (`myVote` / `pendingVote` / `castInFlight`) and the dark→bright transition
  (brighten on the cast's own RPC ack; serialized casts; revert-to-confirmed on
  failure).
- A **Start publish vote** button in `SceneContextRail`'s action group, with a new
  `ended`-scene visibility branch.
- **Yes/No** vote controls and an owner-only **Withdraw** (confirm dialog) inside
  `ScenePublishPanel` during `phase == COLLECTING`.

**Out of scope:**

- Any Go / proto / facade / BFF change (the write path already exists, PR #4562).
- Per-voter ballot display (infeasible without a backend field — §2.1).
- Optimistic *tally count* mutation (counts stay event-driven — §6).
- Vote-window extension (`scene publish vote extend`); any telnet command change.
- The Playwright E2E itself (`.41.7`) — this slice only makes it drivable and
  records the scope recommendation (§10).

## 4. Architecture & components

One new module + targeted edits to three existing units. Each unit keeps a single
purpose and a narrow interface.

| Unit | Change | Responsibility |
| --- | --- | --- |
| `web/src/lib/scenes/publishFlow.ts` **(new)** | `startPublishAction` / `castVoteAction` / `withdrawAction` | `ensureSession` → client wrapper → set/clear store optimistic state. No UI, no rendering. Mirrors `lifecycleFlow.ts`. |
| `publishStore.svelte.ts` | add `myVote` (confirmed) / `pendingVote` (in-flight) / `castInFlight` (+ getters and internal `_markVotePending` / `_ackVote` / `_clearVote`); auto-clear on attempt change | Owns the caller's confirmed + in-flight vote; `_ackVote` promotes pending→confirmed (brighten). Serializes casts. Tally counts unchanged. |
| `ScenePublishPanel.svelte` | new props `{ characterId, isOwner }`; Yes/No + Withdraw in the `COLLECTING` participant branch; panel-local error line | Renders controls during a vote; gates Yes/No on `publishStore.isParticipant`, Withdraw on `isOwner`. |
| `SceneContextRail.svelte` | new `ended`-scene branch with **Start publish vote**; pass `characterId` + `isOwner` to the panel; Start runs through the existing `runLifecycle` wrapper | Hosts Start alongside Pause/Resume/End; Start errors land in the existing `lifecycleErr`. |

### 4.1 `publishFlow.ts` (mirrors `lifecycleFlow.ts:14-38`)

```text
startPublishAction({ sceneId, characterId }):
  sessionId = await ensureSession(characterId)
  await startScenePublish(sessionId, { characterId, sceneId })
  // no store mutation: the scene_publish_started event drives reloadPointer → panel appears

castVoteAction({ characterId, vote }):                // vote: boolean — Yes=true, No=false
  attemptId = publishStore.activeAttemptId            // guard: silent no-op if empty (see below)
  if (!attemptId) return
  if (publishStore.castInFlight) return               // serialize: one cast at a time
  publishStore._markVotePending(vote)                 // raise the lock SYNCHRONOUSLY, before any await (dark + castInFlight=true)
  try {
    sessionId = await ensureSession(characterId)
    await castPublishSceneVote(sessionId, { characterId, publishedSceneId: attemptId, vote })
  } catch (e) { publishStore._clearVote(); throw e }  // revert to previous confirmed vote, unlock, surface error
  publishStore._ackVote()                             // promote pendingVote→myVote (brighten) + unlock (§5)

withdrawAction({ characterId }):
  attemptId = publishStore.activeAttemptId            // guard: silent no-op if empty (see below)
  if (!attemptId) return
  sessionId = await ensureSession(characterId)
  await withdrawScenePublish(sessionId, { characterId, publishedSceneId: attemptId })
  // no store mutation: the scene_publish_withdrawn event drives reloadPointer → panel clears
```

`sessionId` is obtained via `ensureSession(characterId)` inside each action, as in
`lifecycleFlow`. Start/withdraw deliberately do **not** mutate the store — the
authoritative `scene_publish_started` / `_withdrawn` lifecycle events already drive
`publishStore.reloadPointer` (`publishStore.svelte.ts:80-88`), which appears/clears
the panel.

Only `startPublishAction` takes `{ sceneId, characterId }` — the uniform shape the
rail's `runLifecycle` wrapper passes (`SceneContextRail.svelte:61-71`).
`castVoteAction` / `withdrawAction` are panel-invoked and take only what they use
(`{ characterId, vote }` / `{ characterId }`), deriving the attempt from
`publishStore.activeAttemptId` — no unused `sceneId` threaded through.

**Empty-attempt guard.** `castVoteAction` / `withdrawAction` early-return a silent
no-op when `activeAttemptId` is empty. This is defensive only: both controls render
exclusively inside the panel's `publishStore.tally != null` branch
(`ScenePublishPanel.svelte:17`), which implies a live attempt, so the guard is
unreachable in normal flow. A unit test asserts the no-op (no RPC issued).

## 5. State model — caller's optimistic vote

`publishStore` gains, alongside its existing state. A cast is modelled as a
**confirmed** vote plus an optional **in-flight** vote — not a single ack/pending
slot — and the dark→bright transition is driven by the **cast's own RPC
outcome**, never by a tally refetch. This is what makes it race-free (both issues
raised in review):

- `myVote: boolean | null` — the caller's **confirmed** vote (last cast that the
  server acked; `true` = Yes, `false` = No, `null` = none). Drives the **bright**
  highlight. Boolean to match the RPC's `bool vote` wire field
  (`api/proto/holomush/web/v1/web.proto:1248`) — no string↔bool mapping anywhere.
- `pendingVote: boolean | null` — an **in-flight** optimistic ballot (drives the
  **dark** highlight); `null` when no cast is in progress.
- `castInFlight: boolean` — a cast RPC is in flight (from click until it acks or
  fails). The panel **disables both Yes/No buttons while this is true** (§7.2), and
  `castVoteAction` **raises this flag synchronously before any `await`** (§4.1), so
  a second click during session setup cannot slip past the guard. One cast always
  settles before another begins.
- `myVoteAttemptId: string` — the attempt the vote state belongs to (scoping guard).

Transitions:

| Trigger | Effect |
| --- | --- |
| `_markVotePending(vote: boolean)` (raised synchronously in `castVoteAction` before any await; the buttons are also disabled while `castInFlight`) | `pendingVote = vote`, `castInFlight = true`, `myVoteAttemptId = activeAttemptId` |
| `_ackVote()` (the caller's own `castPublishSceneVote` RPC resolves OK) | `myVote = pendingVote`, `pendingVote = null`, `castInFlight = false` (**brighten**: promote in-flight → confirmed) |
| `_clearVote()` (RPC reject) | `pendingVote = null`, `castInFlight = false` — **`myVote` (the previous confirmed vote) is left intact** |
| `activeAttemptId` changes to a different id (new attempt) / `reset()` | clear all (`myVote`, `pendingVote`, `castInFlight`, `myVoteAttemptId`) |

The getters gate on `myVoteAttemptId === activeAttemptId`, so stale state never
bleeds into a new attempt. This model guarantees the two properties review called
out:

1. **No overlapping casts.** `castVoteAction` sets `castInFlight` synchronously
   *before* awaiting `ensureSession`, and the panel disables Yes/No while it is
   true, so a second click (even during session setup) bails at the guard. One
   cast RPC always settles before another begins.
2. **Brighten is fenced to the cast that produced it.** Promotion
   (`pendingVote → myVote`) happens **only** in `_ackVote`, i.e. from the caller's
   *own* RPC resolving — never from a tally refetch. An older or another
   participant's refetch can therefore never confirm a ballot, and a failed cast
   clears only `pendingVote`, leaving `myVote` so the highlight reverts to the
   previously confirmed vote (never to `null`).

The button brightens the moment the server **confirms** the vote (RPC ack); the
**tally counts** update independently and slightly later via the event-driven
refetch (§6) — so the vote highlight and the count are decoupled, and neither can
race the other. If the RPC never resolves (transient network failure), the ballot
stays dark and the buttons stay disabled until it settles, then unlocks.

## 6. Live updates — tally stays event-driven

The aggregate tally continues to update **only** via the existing
event→debounced-refetch path (`onEvent` → `scheduleTallyRefetch`, 300 ms debounce,
`publishStore.svelte.ts:43-51,80-88`). This slice adds **no** optimistic count
mutation — doing so would risk a transient double-count against the authoritative
refetch and would mean reading vote payloads the BFF does not deliver (ADR
`holomush-uqrr7`). Only the caller's **own button** does the dark→bright
transition (§5).

## 7. UI — placement & gating

### 7.1 Start (rail action group)

`SceneContextRail.svelte` gains a Start button in the action group
(`:116-139`). Today that group renders only when `showPause || showResume ||
showEnd || canEditSettings` — all false for an `ended` scene — so a new predicate
is added:

```text
showStartPublish = isParticipant && scene.state === 'ended'
                   && !publishStore.loading && !publishStore.voteInProgress
```

- `isParticipant` is the rail's existing role check (`owner`/`member`,
  `SceneContextRail.svelte:23`).
- The **`!publishStore.loading` guard is required.** `voteInProgress` is
  `activeAttemptId !== ''` (`publishStore.svelte.ts:178`), which is not yet
  resolved during the async cold-start window after a scene switch
  (`loadColdStart`, `publishStore.svelte.ts:123-160`). Without the guard, switching
  to an `ended` scene that *does* have an in-flight attempt would briefly flash a
  false Start button before cold-start resolves — the identical race
  `ScenePublishPanel.svelte:10-13` already guards for the panel via
  `publishStore.loading`.
- Single click → `runLifecycle(publishFlow.startPublishAction)` (the existing
  uniform `{ sceneId, characterId }` wrapper, `:61-71`); errors surface in the
  existing `lifecycleErr` line.
- The action group's render guard is widened to include `showStartPublish` so the
  group appears for ended scenes.
- **"Start another"** after a failed attempt needs no separate control: an
  `ATTEMPT_FAILED` attempt leaves the scene `ended` and clears `activeAttemptId`,
  so the same Start button reappears (subject to the backend attempt-budget gate,
  whose `FailedPrecondition` is surfaced — §8). A `PUBLISHED` attempt moves the
  scene to `published`, where Start is hidden.

### 7.2 Vote / Withdraw (panel, COLLECTING)

`ScenePublishPanel.svelte` takes new props `{ characterId, isOwner }` (passed from
the rail at the mount site, `SceneContextRail.svelte:142`; `sceneId` is not needed —
the attempt is read from `publishStore.activeAttemptId`). In the existing
participant branch (`publishStore.isParticipant && publishStore.tally`), when
`publishStore.phase === 'COLLECTING'`:

- **Yes / No** buttons for participants — Yes calls `castVoteAction({ characterId,
  vote: true })`, No `{ vote: false }`. Each button (value `X` ∈ {`true`,`false`})
  renders **dark** when `publishStore.pendingVote === X` (in-flight / awaiting the
  count), **bright** when `pendingVote === null && publishStore.myVote === X`
  (confirmed), and default otherwise. **Both buttons are `disabled` while
  `publishStore.castInFlight`** — this serializes casts (§5). Vote is freely
  changeable (click No after a confirmed Yes) once the prior cast settles and the
  buttons re-enable.
- **Withdraw** for `isOwner` only, behind a confirm dialog ("Cancel this
  publication vote?" → [Withdraw] [Keep]).
- Non-`COLLECTING` phases (`COOLOFF`, resolved) render the tally without active
  controls, as today.

Observers (`publishStore.isParticipant === false`) keep the existing **badge-only**
render — no tally (INV-SCENE-32 read gate), no controls (INV-SCENE-61 observers
have no publish vote). The loading branch is unchanged.

### 7.3 Color tokens (branding INV-7)

The dark "pending" and bright "confirmed" highlights use the existing brand cyan
tokens (`--brand-cyan-bright` / `--brand-cyan-deep` from
`site/src/styles/custom.css`, mirrored in the web theme) via existing `Button`
variants and Tailwind classes — **no new hardcoded brand hex**. Amber is **not**
used (cursor-only per branding rules).

## 8. Error handling

Per `.claude/rules/grpc-errors.md`, the backend already returns safe codes; the web
surfaces them without leaking internals:

| Code | Cause | Surfacing |
| --- | --- | --- |
| `FailedPrecondition` | Start on non-`ended` scene or budget exhausted; vote on a resolved attempt | inline message (rail `lifecycleErr` for Start; panel error line for vote) |
| `PermissionDenied` | non-owner withdraw; non-participant tally read | Withdraw is owner-gated client-side (defensive), so this is an unexpected-path message; the gated read already maps `PermissionDenied` to observer mode (`publishStore.svelte.ts:114-117`) |

`castVoteAction` reverts the optimistic highlight on any reject (§5). Panel control
errors use a panel-local `$state` line mirroring the rail's `lifecycleErr` /
`membershipErr` pattern (`SceneContextRail.svelte:49,136`).

## 9. Authorization & invariants

This slice **adds no authorization code**. The writes flow through the existing
facade/BFF methods, which are **engine-excluded** by design.

- **INV-SCENE-33** — the ABAC engine MUST NOT be on the participant-gated publish
  path; unchanged (no facade/handler edit here).
- **INV-SCENE-32** — the `IsParticipant` gate at `GetPublishedScene` (before any DB
  query, `invariants.yaml:2966`) keeps the tally read participant-only; enforced
  server-side and unchanged. The client's observer badge-only render mirrors it and
  never bypasses it.
- **INV-SCENE-61** — observer-role participants have **no publish vote**
  (`invariants.yaml:3253`); the client shows observers no vote/withdraw controls,
  mirroring the server gate.

Client-side visibility predicates (Start/Yes/No/Withdraw shown to
participants/owner) are **UX only**; the facade self-gates remain authoritative.
No new registry invariants; no `// Verifies:` capstone owed.

## 10. Decomposition (at plan-to-beads)

A single web-only vertical slice, sequenced so each step builds green:

1. **`publishStore` optimistic vote** — `myVote` / `myVotePending` /
   `myVoteAttemptId` + transitions (§5); unit tests.
2. **`publishFlow.ts`** — three actions over the client wrappers (§4.1); unit tests
   mirroring `lifecycleFlow.test`.
3. **`ScenePublishPanel` controls** — props + Yes/No + owner Withdraw + confirm +
   panel error line (§7.2); component tests.
4. **`SceneContextRail` Start** — `showStartPublish` predicate, action-group guard,
   panel prop wiring (§7.1); component tests.

### 10.1 `.41.7` E2E scope (recommendation, not built here)

Drive the participant cast path **UI-driven (telnet-free)** now that controls
exist. A multi-voter tally and the observer assertion need a second roster member;
seed that via a **second browser context** (or a backend/telnet seed in the
harness) — a `.41.7` detail. This slice's acceptance does not depend on `.41.7`.

## 11. Acceptance

From the web GUI with no telnet: a participant opens an `ended` scene and clicks
**Start publish vote**; the panel appears; the participant casts **Yes**, the
button shows dark→bright as the refreshed count lands, then changes to **No**; the
**owner** withdraws via the confirm dialog and the panel clears; an **observer**
sees the badge only (no tally, no controls). Backend, facade, proto, and telnet
surfaces are unchanged. `task pr-prep` passes; `design-reviewer` and `code-reviewer`
return READY.
<!-- adr-capture: sha256=138a9c57d78ef4f3; session=cli; ts=2026-07-01T13:38:12Z; adrs= -->
