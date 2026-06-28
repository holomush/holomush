<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Scenes Web — Publish-Vote Actions (slice 4/4) Design

| | |
| --- | --- |
| **Bead** | `holomush-n95d9` (design) → epic `holomush-5rh.24` |
| **Status** | DRAFT — pending design-reviewer |
| **Theme** | `theme:web-portals`, `theme:social-spaces` |
| **Master spec** | [2026-06-24-scenes-web-management-actions-design.md](2026-06-24-scenes-web-management-actions-design.md) §2.2, §7 (Publish), §10.4 |
| **Reviewers** | `abac-reviewer` REQUIRED; `crypto-reviewer` NOT required |

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as in RFC 2119 / RFC 8174 (root `CLAUDE.md`).

## 1. Goal

Deliver the **web-portal** affordances for the scene publish loop — Start a
publish vote, cast/change a Yes/No vote, owner Withdraw, and a **live tally** —
so a participant can run a publication vote end-to-end from the GUI. This is the
final slice of epic `holomush-5rh.24` (after Lifecycle, Settings, Membership).

Per the web-portals superset principle (`holomush-sz0h3`), the publish-attempt
*existence/phase* is modeled as an attribute of the scene read so the signal is
available to every surface; the *tally* remains behind its existing
participant-gated read.

## 2. Current state (grounded)

The backend is **complete**; the gap is the web-exposure stack and the GUI.

| Capability | Where | Status |
| --- | --- | --- |
| `StartScenePublish` handler | `plugins/core-scenes/publish_service.go:163` | exists; participant + ended; no ABAC engine |
| `CastPublishSceneVote(vote bool)` handler | `publish_service.go:565` | exists; frozen-roster participant (store gate); no ABAC engine |
| `WithdrawScenePublish` handler | `publish_service.go:720` | exists; owner check; no ABAC engine |
| `GetPublishedScene` (in-flight + tally read) | `publish_service.go:36` | exists; participant-gated (INV-SCENE-60); tally in response |
| `scene_publish_*` events (6 types) | `plugins/core-scenes/publish_events.go` | stream on `events.<game>.scene.<id>.ic`, `Sensitive:false` |
| Telnet `scene publish` / `vote yes\|no` / `withdraw` / `status` | `commands.go:193/329/232/260` | exists; `status` renders tally (participant-gated) |
| Facade publish methods (`SceneAccessService`) | `internal/grpc/sceneaccess_service.go` | **MISSING** |
| BFF publish methods (`WebService`) | `internal/web/scene_handlers.go` | **MISSING** |
| Web client wrappers + panel UI | `web/src/lib/scenes/` | **MISSING** (no publish/vote code today) |

**Telnet/web-terminal is already complete** for both participation and status:
`scene publish status` (`handleStatus`, `commands.go:260`) already discovers the
attempt (`ListSceneAttempts → latestAttemptID`) and reads the tally via
`GetPublishedScene`, rendering `Publish attempt #N: COLLECTING (votes: 2 yes,
0 no, 3 pending)` to participants. **This slice does not modify any telnet
command.** No dedicated text renderer for `scene_publish_*` events exists, so the
text surfaces are poll-to-refresh; the portal panel is the live upgrade.

## 3. Scope

**In scope:**

- Facade + BFF + web-client passthrough for `StartScenePublish`,
  `CastPublishSceneVote`, `WithdrawScenePublish` (writes) and `GetPublishedScene`
  (participant-gated in-flight tally read).
- Two new `SceneInfo` fields — `active_publish_attempt_id`, `publish_status` —
  populated by the `GetScene` handler, powering the portal's affordance gating.
- A reactive publish-state reducer over the existing `scene_publish_*` IC stream
  in `workspaceStore`.
- The publish panel on `SceneContextRail` (layout C — §7).
- A telnet-free Playwright E2E.

**Out of scope:**

- Any telnet/web-terminal command change (`scene info`, `scene publish *` are
  untouched — already complete; §2).
- Admin vote-window extension (`scene publish vote extend`, `commands.go:1648`).
- Any authorization change (publish is engine-excluded — §4).
- A live text renderer for `scene_publish_*` in the terminal log (separate
  concern; the panel covers the portal need).
- The published-archive read path (`GetPublicSceneArchive`,
  `listPublishedScenes`) — already web-exposed.

## 4. Authorization — the inversion (INV-SCENE-33)

Unlike the prior three slices, **this slice adds no handler self-gate.** Publish
handlers self-protect via internal participant/owner checks, and **INV-SCENE-33**
mandates the ABAC engine **MUST NOT** be called on a participant-gated publish
handler path.

| Requirement | Rule |
| --- | --- |
| **MUST NOT** add `evaluator.Evaluate` | On any facade publish method or publish handler. |
| **MUST** mirror the `EndScene` facade shape *minus* the engine | `resolveAndGate` (session/guest, INV-SCENE-64) + `ownedCharacter` (INV-SCENE-63) + `beginDispatch` (actor stamp), then call the RPC. No gate call. |
| **MUST** keep the tally read participant-gated | `GetPublishedScene` already enforces the owner+member gate (INV-SCENE-60) and excludes observers (INV-SCENE-61); the facade passes it through unchanged. |

Facade precedent: `EndScene` at `sceneaccess_service.go:359`; helpers
`resolveAndGate:130`, `ownedCharacter:109`, `beginDispatch:152`.

`abac-reviewer` **MUST** run to confirm (a) the engine stays out of the publish
path, (b) the `SceneInfo.active_publish_attempt_id` *pointer* is acceptable to
surface to non-participants on open scenes (see §5.1), and (c) — though **not
introduced by this slice** — that the existing observer visibility of
*per-voter ballot choices* is acceptable: `ScenePublishVoteCastEvent` carries
`character_id` + `vote bool` (`scene.proto:997-1008`) and streams role-agnostically
to any FocusMembership holder (`stream_access.go:101`), so an observer can
reconstruct individual yes/no choices, not merely the aggregate tally. The slice
surfaces this in the panel; the reviewer should vet whether that exposure is
intended before the panel renders per-voter state to observers.

## 5. Read model — publish status as a scene attribute

### 5.1 `SceneInfo` gains a non-sensitive pointer

`SceneInfo` (`api/proto/holomush/scene/v1/scene.proto:225`) gains:

- `string active_publish_attempt_id` — the live attempt id, empty when none.
- `string publish_status` — `"COLLECTING"` / `"COOLOFF"` / empty.

The `GetScene` handler (`plugins/core-scenes/service.go:436`) **MUST** populate
these via the existing `activeAttemptID` helper (`commands.go:389`) over
`ListSceneAttempts`. The pointer is **active-only** — it reflects an in-flight
attempt (`COLLECTING` / `COOLOFF`) and is empty once an attempt resolves. A
cold-start of a **terminal** attempt is therefore sourced from the existing
signals, not this pointer: `PUBLISHED` from the already-web-exposed published
archive (`GetPublicSceneArchive` / `listPublishedScenes`); a transient
`ATTEMPT_FAILED` reason is live-only (the `scene_publish_resolved` event) and is
not reconstructed on cold-start. These fields carry **no tally** — `SceneInfo` is broadly
readable (any character on `open` scenes; participants/invitees/observers on
`private` scenes, gate at `service.go:458-491`), so embedding vote counts would
leak them across the INV-SCENE-60/61 boundary.

**Privacy rationale.** The *existence and phase* of an attempt is low-sensitivity
— `scene_publish_*` events already stream to any scene watcher (role-agnostic
FocusMembership, `internal/grpc/stream_access.go:101-131`). The *counts* are
participant-confidential at the snapshot boundary (`GetPublishedScene` denies
observers, `store.go:1448`). This split is the design's load-bearing invariant
respect: pointer on the broad read, counts behind the narrow gate.

These fields exist **solely** for the portal's affordance gating (decide Start
vs in-progress, and whether to issue the participant-gated tally read) as one bit
on the `getScene` the portal already calls. Telnet does not consume them.

### 5.2 Tally snapshot via `GetPublishedScene` passthrough

`GetPublishedScene` (`scene.proto:159`; response tally at `:745`,
`PublishedSceneVoteSummary` at `:709`) is exposed via facade + BFF + client as a
**pure passthrough** (the handler already enforces the participant gate). A
participant cold-starting an in-flight vote reads the snapshot once; observers
and non-participants receive `PermissionDenied` (and rely on live events).

## 6. Write model & live updates

### 6.1 Write passthroughs

Facade + BFF + web-client wrappers for the three writes, structurally identical
to the lifecycle slice (`WebEndScene` BFF precedent at `scene_handlers.go:183`;
`SceneAccessClient` iface at `handler.go:108`). Each new facade-client method
**MUST** also be added to `cmd/holomush`'s `GRPCClient` narrowing interface
(`cmd/holomush/deps.go`) and `mockGRPCClient` (`deps_test.go`), verified with
`task build` (not just package tests).

`CastPublishSceneVote` carries `vote bool` (Yes=true/No=false) +
`published_scene_id`; the panel supplies the attempt id from the cold-start
read or the live `scene_publish_started` event.

### 6.2 Live tally reducer

`workspaceStore` gains a reactive `publishStateBySceneId` map updated from the
existing scene IC stream (`altSessions.svelte.ts → workspaceStore.ingestEvent`,
`workspaceStore.svelte.ts:218`). The reducer folds `scene_publish_started →
vote_cast → cooloff_started → resolved → withdrawn` into per-scene phase, tally,
and per-voter state. **No new subscription** — the portal already consumes this
stream for IC log entries. This is what makes the tally tick live for
participants **and** observers without a refetch (master spec §8).

## 7. UI — publish panel on `SceneContextRail` (layout C)

A compact rail card (`web/src/lib/components/scenes/SceneContextRail.svelte`), one component
rendering every state off `publishStateBySceneId` + `SceneInfo`:

| State | Render |
| --- | --- |
| ended, no attempt, caller is participant | `Start publish vote` button |
| `COLLECTING` | Yes/No/Pending stat tiles, Yes/No vote buttons (caller's current vote highlighted), per-voter list behind a "show voters" expander, owner `Withdraw` |
| `COOLOFF` | countdown to publish; tally frozen |
| resolved `PUBLISHED` | "Published ✓ · view archive" (links existing archive read) |
| resolved `ATTEMPT_FAILED` | failure reason (`ANY_NO` / `TIMEOUT` / `WITHDRAWN`); "Start another" when attempt budget remains |

**Visibility predicates** (client-side UX only; facade/handler gates remain
authoritative): Start/vote/Withdraw shown only to roster participants
(`owner_id` / participant membership); observers see status + live tally but no
controls. Component tests follow the raw-`svelte`-mount + `flushSync` idiom
(no `@testing-library`); portaled menu items defer to E2E.

## 8. Error handling

Per `.claude/rules/grpc-errors.md`: facade returns generic
`status.Errorf(codes.Internal, "internal error")` for internal failures (log via
`errutil.LogErrorContext`, no inner-error leak); passes safe codes through —
`PermissionDenied` (non-participant tally read; non-owner withdraw),
`FailedPrecondition` (FSM — e.g. vote on a resolved attempt, start on a
non-ended scene). The web maps codes to toasts; FSM messages surfaced where safe.

## 9. Invariants

This slice introduces **no** new registry invariants. It **respects**:

- **INV-SCENE-33** — no ABAC engine in publish handler/facade paths (§4).
- **INV-SCENE-60** — participant-gated tally read kept plugin-code-enforced (§5.2).
- **INV-SCENE-61** — observers excluded from votes; no tally on the broad read (§5.1).

No `// Verifies:` capstone is owed (these are already bound elsewhere).

## 10. Decomposition (at plan-to-beads)

One spec → a single vertical slice, sequenced:

1. **Proto** — `SceneInfo` += `active_publish_attempt_id`, `publish_status`;
   facade + BFF publish RPCs (3 writes + `GetPublishedScene`). Red-build window
   until the BFF handler task lands (Connect `WebServiceHandler` has no
   `Unimplemented` embed — same pattern as prior slices).
2. **`GetScene` populate** — fields filled via `activeAttemptID` lookup.
3. **Facade** — 3 write passthroughs + `GetPublishedScene` passthrough; **no
   engine** (INV-SCENE-33).
4. **Client + narrowing** — `SceneAccessClient` methods + `cmd/holomush`
   `GRPCClient` iface + `mockGRPCClient` (compile gate; `task build`).
5. **BFF** — `WebStartScenePublish` / `WebCastPublishSceneVote` /
   `WebWithdrawScenePublish` / `WebGetPublishedScene` (build green).
6. **Web client + reducer** — client wrappers + `publishStateBySceneId` event
   reducer.
7. **Publish panel UI** — layout C on `SceneContextRail`.
8. **E2E** — telnet-free GUI publish/vote/withdraw + live tally.

## 11. Acceptance

From the web GUI with no telnet: a participant can start a publish vote on an
ended scene, cast and change a Yes/No vote, watch the tally update live, the
owner can withdraw, and a resolved attempt shows published/failed state. Telnet
surfaces are unchanged and still complete. `task pr-prep` passes; `abac-reviewer`
returns READY.
<!-- adr-capture: sha256=0936010459aaead2; session=cli; ts=2026-06-28T19:22:51Z; adrs=holomush-o8gx8 -->
