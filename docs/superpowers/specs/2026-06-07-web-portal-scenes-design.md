<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Web Portal: Scenes — Player Workspace (E9.5)

**Bead:** `holomush-5rh.8`
**Predecessors:** `holomush-5rh.7` Scene Logging (shipped); `holomush-5rh.14` Phase 5 focus model + multi-connection visibility ([spec](2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md)) — this design **is** the "Web client focus UI" deferred there as Phase 9
**Parent design (v2):** [scenes-and-rp-design-v2.md](2026-04-06-scenes-and-rp-design-v2.md)
**Substrate contract:** [social-spaces-substrate-contract](2026-05-16-social-spaces-substrate-contract.md) (INV-S9 → INV-SCENE-60, reaffirmed by §7 of this design)
**EventBus:** [jetstream-event-log-design](2026-04-18-jetstream-event-log-design.md)

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are to be interpreted per RFC 2119/8174.

## 1. Goal

Give players a web surface for scenes: browse the active scene board, read and contribute to scenes they participate in (live), spectate open scenes, manage participation across their alts, search/filter by tag, read ended/published scene logs, and export them — without ever touching their terminal session.

Three player states MUST all be first-class:

1. A player with a character in play in the terminal.
2. The same player **concurrently** contributing to one or more scenes (via the same or other characters) without disconnecting, resetting, or otherwise impacting the terminal session.
3. A scenes-only player with **no terminal session at all**.

## 2. Non-goals

| Out of scope | Tracked / rationale |
|---|---|
| HTML and plain-text export | Dropped in brainstorming; markdown + jsonl only |
| Anonymous (unauthenticated) access | "Public" = all authenticated non-guest players |
| Persisted per-player read markers (cross-session unread) | Follow-up bead; v1 unread = snapshot + live within a workspace session |
| Telnet `scene watch` command | The `spectate` ABAC action and observer-join path are defined here; the telnet command is a follow-up bead (parity-ready by construction) |
| Forum integration (scene requests, scheduling) | `holomush-5rh.9` |
| Channels in the workspace | `holomush-0sc` channels epic |
| Composer rich-text/WYSIWYG | v1 is multi-line plain text (markdown allowed in content); preview is a MAY |

## 3. Domain decisions

| # | Decision | Rationale |
|---|---|---|
| D1 | **Surface model:** scenes are a player-scoped workspace at `/scenes`, a sibling surface of `/terminal` in the left icon-rail nav. The workspace MUST NOT mutate the terminal connection's focus, its stream, or the session-level `PresentingFocus`. | User requirement (three player states, §1). Phase 5 D9 already isolates web (`comms_hub`) focus changes from `PresentingFocus`; AresMUSH's focus-stealing failure mode is structurally prevented. |
| D2 | **Live delivery rides the Phase 5 focus system.** The workspace holds its own connection(s) (ClientType `comms_hub`); selecting a scene = `SetConnectionFocus(workspaceConn, scene:<id>)`; the coordinator (sole driver, INV-SCENE-38) churns subscriptions and enqueues the IC replay tail (`scenes.focus.replay_tail_default`, clamp 0–10). | The substrate was designed for exactly this consumer (Phase 5 non-goals: "Web client focus UI → Phase 9"). No new streaming RPC; reuses replay, presence, and routing machinery. |
| D3 | **Alts are first-class.** The workspace spans the player's characters: each scene row shows which character participates ("as Mira Vale"); the composer acts as that scene's participating character. The workspace lazily establishes one character session per participating alt via the existing web select-character flow. | Sessions/connections/focus are character-scoped; participation is per-character. Concurrent alt sessions ride the existing player-session → child game-session model (verify V1, §9). |
| D4 | **Writes go through the existing command path.** Pose/say/ooc from the workspace = `HandleCommand("scene pose …")` (et al.) on the acting alt's session. No new write RPCs. | Capability parity with telnet by construction; command-dispatch ABAC gates (`gated()` table, fail-closed) apply identically; zero new trust surface on the emit path. |
| D5 | **Reads go through `Web*` BFF RPCs** on `WebService`, each a thin passthrough to a core-side scene-access facade (`internal/grpc`) that resolves session→subject, **overrides any client-supplied `character_id`/`player_id`**, applies the guest gate, and calls the plugin's `SceneService`. The public-archive RPCs (`GetPublicSceneArchive`, `DownloadPublicSceneArchive`) are wrapped the same way (not raw-proxied). | `SceneService` trusts its caller (`plugins/core-scenes/service.go:194`: "the caller (host) is responsible for ensuring ABAC has authorised"); a raw ConnectRPC proxy would let a browser act as any character. Mirrors the established `WebListFocusPresence → CoreClient` / `WebGetContent → ContentClient` shape; the gateway stays a translator (gateway-boundary rule). Supersedes the brainstorm's earlier "thin proxy for public archive" split: once everything is authed non-guest, one uniform guarded path beats two mechanisms. |
| D6 | **Watching = observer auto-join.** "Watch" on an open scene = `JoinScene` with `role = observer` (then `JoinFocus`/focus as normal), gated in this order: (1) plugin-**code-enforced** `visibility == open` check — non-open scenes fail before ABAC is consulted; (2) ABAC `spectate` action on `scene:<id>`. Observers are real participants: visible in the roster, kickable by the owner, receiving `scene_activity` like any member. Role-aware gates exclude observers from the emit path, pose order, and publish votes. "Join scene" on a watched scene = role upgrade observer→player. | INV-SCENE-60 holds **verbatim** — the participant list remains the sole privacy boundary, and the focus coordinator + filter-at-delivery machinery are untouched (watch = ordinary membership). Watchers are socially visible (RP consent) and moderatable. The `role` column exists but is constrained: adding `observer` REQUIRES a plugin migration widening `CHECK (role IN ('owner','member','invited'))` (`plugins/core-scenes/migrations/000003_scene_participants_and_ops_events.up.sql:14`) plus the `ParticipantRole` constant and `IsValid()` case (`plugins/core-scenes/participants.go:25-29`). Telnet `scene watch` parity = the same plugin join path. |
| D7 | **Unread badges = live notifications + snapshot catch-up.** Transport (V5 resolved): every member connection's Subscribe consumer carries its session's `FocusMemberships` scene subjects in the filter set; at delivery, a scene event for a connection NOT focused on that scene is **downgraded** to `ControlFrame{CONTROL_SIGNAL_SCENE_ACTIVITY, scene_id}` (no payload — privacy-clean, no decryption) instead of an `EventFrame`. `WebListMyScenes` returns last-activity/pose-count snapshots for initial sync and reconnect catch-up. The client de-duplicates (focused scene's own events are not also counted via notifications). Observers receive badges like any member (D6). | User decision: "ConnectRPC is live for a reason — 2 with a 1-based catch-up." `FocusMemberships` is server-authoritative and ⊆ `scene_participants` (Phase 5 D4), so members-only delivery holds by construction (INV-SCENE-62) — non-members' consumers never carry the subject. No plugin manifest/emit changes; nothing persisted (missed pings self-heal via snapshot catch-up). |
| D8 | **The log viewer consumes structured data — via existing machinery.** Live/scrollback reads ride `WebQueryStreamHistory` → `QueryStreamHistory` (already exists: hard FocusMembership gate for scene streams per I-17, host-side decryption, ULID-cursor paging on the `(subject, id)` index). Ended-scene structured reads ride the new `ExportSceneLog(format=jsonl)` (D9) — the client renders jsonl entries with the same pose-card components. No new read RPC; no markdown parsing in the viewer; `marked`/`dompurify` are NOT used. | `scene_log` payloads are **encrypted at rest** (sensitivity:always; DEK columns per migration 000005) and the plugin MUST NOT decrypt outside the host seam (INV-47) — a plugin-side `ReadSceneLog` would return ciphertext. The host history machinery already decrypts for authorized readers; observers pass its membership gate because `JoinFocus` gives them a `FocusMembership` (`internal/grpc/stream_access.go::sessionHasMembership`). |
| D9 | **Export = markdown + jsonl.** Active scenes: participant-gated facade export via the shared renderer. Ended/published scenes: existing `DownloadPublishedScene` / `DownloadPublicSceneArchive` (wrapped per D5). Browser triggers a blob download. | Backend renderer already supports both formats; HTML/plain-text dropped (§2). |
| D10 | **Auth boundary: login required everywhere; guests excluded.** "Public scenes" means visible to all authenticated **non-guest** players. Enforced twice: a frontend route guard on `/scenes/**`, and the facade's guest gate backed by a new `is_guest` attribute on the player ABAC provider (with `has_` witness per ADR holomush-ti1b conventions). | User decision. `Player.IsGuest` already exists (`internal/auth/player.go:67`); the attribute provider is the missing piece. Defense in depth: the route guard is UX, the facade gate is the security boundary. |
| D11 | **UX:** board = rich list rows (toolbar: search + tag chips; row: title, status dot, location, tags, participant count, last activity). Workspace = 3-pane (my-scenes/watching list · pose-card stream + pose-order strip + composer · scene context rail). Archive/ended scene = same chrome, read-only, no composer. Mobile: list collapses to a drawer, context rail to a sheet; the pose stream is an ARIA live region. | Approved via visual-companion mockups (v3) referencing the existing shell idiom (left icon rail, right context rail, bits-ui primitives via shadcn-svelte-scaffolded `$lib/components/ui/`). |
| D12 | **My-scenes data:** new `SceneService.ListCharacterScenes(character_id)` RPC returning `SceneInfo` + activity metadata; the facade fans out across the player's owned characters for `WebListMyScenes`. | `SceneStore.ListScenesForCharacter` exists but is store-internal (IDs only, no proto exposure). The plugin owns its contract; telnet can reuse the RPC later. |
| D13 | **v1 simplifications:** unread state is per-workspace-session (no persistence); composer drafts persist in `localStorage` only. | Scope control; each lifts cleanly into a follow-up bead. |

## 4. Requirements (bead acceptance criteria mapping)

| Criterion | Satisfied by |
|---|---|
| Active scenes list page (public scenes) | `/scenes/browse` board; `WebListScenes` → `ListScenes` (visibility/board semantics, CW filtering with authentic subject) |
| Scene detail page with metadata | Workspace scene view (live) and `/scenes/[id]` read page (ended/published); `WebGetScene` |
| Scene log viewer (formatted, readable) | `WebQueryStreamHistory` (live/scrollback) + `ExportSceneLog` jsonl (ended) rendered as typed entry components (D8) |
| My scenes page | Workspace my-scenes list (per-alt); `WebListMyScenes` (D12) |
| Scene search/filter by tag | `ListScenesRequest.tags` + pagination (already exists); board toolbar |
| Export download | D9 (md + jsonl) |
| Real-time scene updates | Focus-driven delivery (D2) + `scene_activity` badges (D7). The bead says "WebSocket"; the platform's mechanism is ConnectRPC server-streaming — treated as the intended meaning. |
| Mobile-responsive design | D11 mobile behaviors |
| Accessibility compliance | D11: semantic landmarks, ARIA live region for the stream, keyboard navigation, focus management on pane switches; axe checks in E2E |
| Integration with existing auth | D3 (existing select-character/session flow), D10 (route guard + facade gate) |

## 5. Architecture

### 5.1 Routes

| Route | Purpose | Notes |
|---|---|---|
| `/scenes` | Player workspace: my scenes + watching + selected scene view with composer | The primary surface (D11) |
| `/scenes/browse` | Active scene board (rich list, tag search/filter) | "Join" / "Watch" affordances per row |
| `/scenes/archive` | Published archive search/browse | |
| `/scenes/[id]` | Read-only log page for ended/published scenes | Active scenes redirect into the workspace |

All under the `(authed)` layout group plus a non-guest guard.

### 5.2 New/changed surface by layer

| Layer | Change |
|---|---|
| `api/proto/holomush/scene/v1/scene.proto` | **Add** `WatchScene` (observer auto-join, D6), `ExportSceneLog` (participant/observer-gated; markdown + jsonl via the `snapshotDecryptor` host seam; jsonl also powers the ended-scene viewer per D8), `ListCharacterScenes` (D12), and an additive `repeated ParticipantInfo observers` field on `SceneInfo`. Full proto doc comments grounded in handlers (repo rule). |
| `api/proto/holomush/plugin/v1/plugin.proto` | **Add** `GetConnectionFocus` to `PluginHostService` (+ Lua hostfunc — INV-S3 Go/Lua parity), enabling focus-aware emit routing: `scene pose/say/ooc` resolve their target scene from the connection's `FocusKey`, replacing single-membership inference (the Phase 5 TODO at `plugins/core-scenes/commands.go::handleEmit`). |
| `api/proto/holomush/core/v1/core.proto` | **Add** `SelectCharacterRequest.client_type` (when `comms_hub`, a fresh session creation skips the `arrive` emit — V2 resolution) and `CONTROL_SIGNAL_SCENE_ACTIVITY` + `ControlFrame.scene_id` (V5 resolution, see D7). |
| `api/proto/holomush/web/v1/web.proto` | **Add** `WebListScenes`, `WebGetScene`, `WebListMyScenes`, `WebExportScene`, `WebGetPublicSceneArchive`, `WebDownloadPublicSceneArchive`, `WebWatchScene` (observer auto-join, D6), and `WebSetSceneFocus` (focus/clear on a workspace connection — membership-gated as built). Log scrollback uses the **existing** `WebQueryStreamHistory` (D8). |
| `internal/session` + `internal/store` | Presence: `ListActiveByLocation` counts only sessions with ≥1 `terminal`/`telnet` connection, making "grid present = has terminal/telnet conn" (terminology rule) hold for scenes-only sessions (V2 resolution). |
| `internal/grpc` (scene-access facade) | **New** facade implementing the `Web*` delegates: session-token → authentic subject; client `character_id` override; guest gate; calls `SceneService` via the existing in-process service registry path. |
| `internal/web/handler.go` | `Handler` gains a `SceneAccessClient` (same core conn — precedent: `WithContentClient`, defined `internal/web/handler.go:119`, wired in `cmd/holomush/gateway.go::runGatewayWithDeps`); thin passthrough + proto translation only. |
| `internal/grpc/focus/subscription_router.go` | **Extend**: `scene_activity` notification fanout (D7) to non-focused member connections. The coordinator's focus/membership semantics are otherwise **unchanged**. |
| `internal/access` | `is_guest`/`has_is_guest` on the player attribute provider; `spectate` action registered for the scene resource; guest-deny + spectate policy seeds. |
| `plugins/core-scenes` | **New migration** widening the `scene_participants` role CHECK to include `observer` (paired down migration restores the original constraint) + `ParticipantRole` constant/`IsValid()` case. Implement `ListCharacterScenes` (participant gates in plugin code per INV-SCENE-60, unchanged); observer-join path (`role = observer`: code-enforced `visibility == open`, then ABAC `spectate`); role-aware gates excluding observers from emit, pose order, and publish votes; observer join/leave lifecycle treatment in logs; export render entry point for active-scene export. |
| `web/src` | Routes (§5.1), stores (workspaceStore, sceneStreamStore, badgeStore), components (scene row, pose card, pose-order strip, composer, context-rail panels, export menu, tag filter), non-guest guard. |

### 5.3 Flows

**Open workspace.** Authed non-guest player opens `/scenes` → `WebListMyScenes` (snapshot: scenes per alt + activity) → workspace establishes (or reuses) sessions for participating alts (D3) → opens its `comms_hub` connection(s) and `Subscribe` streams → badges seeded from snapshot, updated live via `scene_activity` (D7).

**Select a scene.** Click scene X (as character C) → `WebSetSceneFocus(connection, scene:X)` → coordinator validates membership via `FocusMemberships`, churns the subscription, replays the IC tail → pose cards render structured events; composer is enabled, acting as C.

**Pose.** Composer submit → `SendCommand`/`HandleCommand("scene pose <text>")` on C's session (addressing semantics verified in V4) → command-dispatch ABAC → plugin emit → event fans out to focused connections; non-focused member connections get `scene_activity`.

**Watch.** From the board, "Watch" on an open scene (choosing the acting alt) → `WebWatchScene` → facade (guest gate, identity) → plugin observer-join: code-enforced `visibility == open`, then ABAC `spectate` → participant row (`role = observer`) + `JoinFocus` → workspace focuses the scene normally (replay tail included). Composer area shows a "Join scene" CTA (role upgrade observer→player) instead of the editor. Unwatch = leave.

**Read an ended/published scene.** `/scenes/[id]` → `WebGetScene` + `WebExportScene(format=jsonl)` (participant/observer-gated) or, for published scenes, `WebDownloadPublicSceneArchive(format=jsonl)` → the client renders the jsonl entries with the same pose-card components → static typed log; export buttons (md/jsonl).

**Export.** `WebExportScene(scene, format ∈ {markdown, jsonl})` → facade authz → shared renderer → content + MIME returned → browser blob download.

### 5.4 UX reference (approved mockups)

Workspace (desktop):

```text
┌─ icon rail ─┬─ my scenes ──────────┬─ scene view ──────────────────────┬─ context rail ─┐
│ ⌂ ▣ 🎭 💬   │ MY SCENES (3)        │ The Broken Compass  ● active      │ SCENE          │
│             │ ● Broken Compass  ②  │ ┌────────────────────────────────┐│  @ Dockside…   │
│             │   as Mira Vale       │ │ [avatar] Captain Reyes   19:03 ││  tags          │
│             │ ● Ashfall Council    │ │ "You're late…"                 ││ ROSTER (4)     │
│             │   as Beryl Silver    │ └────────────────────────────────┘│  …             │
│             │ ● Moonlit Duel    ⑦  │ (ooc) Joss: brb · 19:03           │ ACTIVITY       │
│             │   as Vex             │ pose order: Mira (you) → Reyes →… │                │
│             │ WATCHING (1)         │ ┌ posing as [Mira Vale] ─────────┐│                │
│             │ ● Harbor Negotiation │ │ <multi-line composer>          ││                │
│             │ + browse · ⌕ archive │ │ [Pose] [Say] [OOC]             ││                │
└─────────────┴──────────────────────┴───────────────────────────────────┴────────────────┘
```

Board rows: `● title · location · tag chips · N here · last-activity`, toolbar with search + tag chips. Archive/ended page: workspace chrome, read-only stream, no composer, export buttons in the header. Mobile: my-scenes → drawer; context rail → sheet; stream is the page.

## 6. Security & privacy

- **Identity:** every web scene operation derives the acting character server-side from the authenticated session. Client-supplied `character_id`/`player_id` fields on `Web*` requests are ignored and overridden by the facade (INV-SCENE-63). Writes inherit the command path's session-derived identity (D4).
- **Authorization layering:** route guard (UX) → facade guest gate → ABAC `spectate` for observer-joins → command-dispatch gates for writes → plugin-code participant gates for log reads (INV-SCENE-60, **unchanged**) → filter-at-delivery (load-bearing stream privacy gate, **untouched** — watchers are members).
- **Guests:** denied on all `/scenes/**` surfaces and at the facade (D10, INV-SCENE-64).
- **Observer containment:** non-open scenes are never watchable (code-enforced before ABAC); observer-role participants have no emit path, no pose-order slot, and no publish vote (role-gated in plugin code) (INV-SCENE-61).
- **Error opacity:** facade and gateway follow the gRPC error rules (no inner-error leakage; single translation layer).

## 7. Invariant registry changes

Minted in `docs/architecture/invariants.yaml` with this spec as `origin_spec`, `binding: pending`:

| ID | Summary |
|---|---|
| INV-SCENE-61 | Observer-join (watch) is fail-closed: the `visibility == open` check is plugin-code-enforced and evaluated BEFORE the ABAC `spectate` action; non-open scenes fail before ABAC is consulted; observer-role participants have no emit path, no pose-order slot, and no publish vote. |
| INV-SCENE-62 | `scene_activity` notifications fan out only to connections of sessions whose `FocusMemberships` include the scene; non-participant sessions never receive them. |
| INV-SCENE-63 | Every web scene read/write/export path derives the acting character from the authenticated session server-side; client-supplied `character_id`/`player_id` request fields are never trusted. |
| INV-SCENE-64 | Scene web-portal surfaces (board, workspace, archive, export) require an authenticated non-guest player; `is_guest` subjects are denied at the facade. |

**INV-SCENE-60 is unchanged by this design.** Watching is ordinary membership (observer role), so the participant list remains the sole privacy boundary for scene-log reads, plugin-code-enforced, with ABAC never in that path.

Existing invariants this design leans on (unchanged): INV-SCENE-38 (coordinator sole driver), the `comms_hub` never-auto-focused rule, `PresentingFocus` write discipline (Phase 5 D9/D10), `client_type` accept set.

## 8. Testing

| Tier | Coverage |
|---|---|
| Go unit | Facade: identity override (INV-SCENE-63), guest deny (INV-SCENE-64) — table-driven with `policytest` engines. Plugin: observer-join gate ordering (INV-SCENE-61); role gates (observer excluded from emit, pose order, publish votes). Router: notification fanout membership filter (INV-SCENE-62). |
| Integration (Ginkgo, `integrationtest` + `WithInTreePlugins` + `WithFocusDelivery`) | Workspace connection focus → replay tail → live pose delivery; pose via command path from a second alt session while a terminal session stays undisturbed (D1); watch (observer-join) on open scene / denial on private; observer emit/vote attempts rejected; `scene_activity` fanout to member-not-focused connections only; export both formats; archive read path. |
| E2E (Playwright) | Browse board + tag filter; open workspace; live pose append; watch→join pivot; export download; guest denied; mobile viewport; axe a11y. |
| Invariants | `// Verifies:` annotations bind INV-SCENE-61..64 when the asserting tests land; registry meta-tests green. |

## 9. Plan-time verifications

> Status: V1/V2/V4/V5/V6 were resolved during plan grounding (2026-06-07); the resolutions are folded into D2/D7/D8 and §5.2 above. V3 remains an implementation-task checklist.

| # | Verify | Risk if wrong |
|---|---|---|
| V1 | **RESOLVED:** sessions are per-character (`FindByCharacter` reattach invariant; per-surface sessions explicitly rejected in the session-lifecycle-as-events design). A player runs N alts as N character sessions; a workspace connection for a character already in the terminal attaches to the SAME session as an additional `comms_hub` connection with its own `FocusKey`. | — |
| V2 | **RESOLVED (two changes required):** `SelectCharacter` emits `arrive` on fresh session creation (documented contract), so (a) `SelectCharacterRequest.client_type=comms_hub` skips the arrive emit, and (b) `ListActiveByLocation` (presence) counts only sessions with ≥1 `terminal`/`telnet` connection. | Scene-only players would "appear" on the grid (announce + presence). |
| V3 | Enumerate every participant-keyed surface that must become role-aware for `observer`: emit gate, pose order, roster counts, export attribution, and **specifically the publish-vote path**: `CreatePublishAttempt` seeds the `published_scene_votes` roster filtered to `role IN ('owner','member')` (`plugins/core-scenes/publish_store.go:84-87`), so observers are never voters and `CastVote` rejects them (`SCENE_PUBLISH_NOT_A_VOTER`) — verify this seeding filter is the sole roster source. `ReadSceneMetaForSnapshot` likewise filters `role IN ('owner','member')`, so the snapshot path excludes observers by construction. The unimplemented `scene_idle_nudge` (`holomush-fux3`) MUST carry the constraint "observers are not nudgeable" when built. Confirm `FocusMemberships` needs no special-casing. | A missed surface gives observers a capability they must not have (e.g. a publish vote). |
| V4 | **RESOLVED:** `handleEmit` resolves its target via single-membership inference with an explicit "Phase 5 will replace this with focus-aware routing" TODO (`plugins/core-scenes/commands.go`). This design implements that routing: new `PluginHostService.GetConnectionFocus` RPC (+ Lua hostfunc) lets the plugin resolve the emitting connection's focused scene; fallback to single-membership inference when unfocused. Composer writes always land in the focused scene. | — |
| V5 | **RESOLVED:** in-consumer downgrade per D7 — `CONTROL_SIGNAL_SCENE_ACTIVITY` + `ControlFrame.scene_id`, sent when a delivered scene event's scene ≠ the connection's focus. No new stream, no persistence, no decryption. | — |
| V6 | **RESOLVED:** dissolved by D8 — log reads reuse `QueryStreamHistory`'s existing ULID-cursor paging on the `(subject, id)` index; ended-scene reads consume `ExportSceneLog` jsonl (whole-document). | — |

## 10. Follow-up beads (filed at plan time)

Persisted read markers · telnet `scene watch` command · composer preview/rich-text · HTML export (if demand returns).
