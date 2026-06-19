# Web Create-Scene — Design

- **Bead:** `holomush-5rh.22` (Scenes web: create-scene affordance) — Epic 9 (`holomush-5rh`)
- **Theme:** `theme:web-portals` (`holomush-sz0h3`)
- **Status:** Design (brainstorming) — pending `design-reviewer`
- **Date:** 2026-06-19
- **Author:** Sean Brandt (with Claude)

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as in RFC 2119 / RFC 8174.

---

## 1. Problem & scope

The web Scenes Portal (E9.5, `holomush-5rh.8`) shipped **participation-only**:
browse, read, pose/say/ooc, watch, export. It has **no way to create a scene**.
"Web client can **CREATE** … scenes" was an explicit original Phase-9 acceptance
criterion (`holomush-5rh.18`) that the E9.5 redesign silently dropped. The result
is a self-contradiction in E9.5's own design: §1 makes "a scenes-only player with
no terminal session" a first-class supported state, yet that player can join,
watch, and pose but can **never originate** a scene.

This slice restores web create-scene. It satisfies the `theme:web-portals`
principle (the web is a superset of telnet; `holomush-sz0h3`): a player MUST be
able to start a scene from the web without ever touching telnet or a raw command
line.

**In scope:** a web create-scene affordance and the typed RPC path behind it
(proto → facade → BFF → client → UI), plus recording the D4 / window-per-scene
disposition.

**Out of scope:**
- `SceneService.CreateScene` backend — **already exists**
  (`plugins/core-scenes`, called today by telnet `scene create`).
- Scene lifecycle / management verbs (end/pause/resume/invite/kick/transfer/
  set/order/publish-vote) — sibling `holomush-5rh.24`.
- Guest gating of the Scenes nav link and the terminal `scene` command —
  sibling `holomush-5rh.23`.
- Forum integration — `holomush-5rh.9`.
- Multi-window / pop-out scene routing — see §8 (resolved-by-evolution).

---

## 2. Architecture decision: typed RPC, not the command path

**The web create-scene write MUST be a typed RPC, not an assembled
`scene create <title>` command string.**

This reframes — and for *structural* writes, supersedes — E9.5 D4 ("web writes
ride the command path; no new write RPCs"). The governing principle
(maintainer directive, 2026-06-19):

> The command path (host `HandleCommand`; surfaces = telnet request/response and
> the web `/terminal`) is for **human / CLI interaction only** — conversational
> gameplay verbs a person performs in the moment (`scene join`, `pose`, `say`,
> `ooc`, `emit`). **Everything else** — structural / CRUD / management operations
> on objects (create, set, end, invite, …) — MUST be exposed as **typed RPCs**
> through the BFF.

This is not a new pattern on this surface: typed scene-write RPCs already exist —
`WebWatchScene` and `WebExportScene` (`internal/web/scene_handlers.go:120,152`)
proxy to `SceneAccessServer` facade mutations
(`internal/grpc/sceneaccess_service.go:232` `WatchScene`).

Rationale:
- **Create returns a result the caller consumes** — the new scene. The command
  path surfaces that only as human-readable prose (`"Scene created: <id>"`,
  `plugins/core-scenes/commands.go` `handleCreate`) that the web would have to
  parse. A typed RPC returns a structured `SceneInfo`.
- **Atomic, no escaping hazard.** Free-text title/description cross the wire as
  typed fields, not tokens in a single command line.
- **Consistency** with the existing read + Watch/Export/SetFocus RPCs.

**Rule of thumb (for this and future surfaces):** typed RPC when the operation
returns something the caller consumes (create→`SceneInfo`, watch→participant,
export→bytes); command path when it is a fire-and-forget human verb
(pose/say/join). The existing web pose/say/ooc composer
(`web/src/lib/components/scenes/SceneComposer.svelte`) and `scene join` staying on
`sendCommand` is therefore **correct** and is not changed by this slice.

**Cross-cutting consequence (recorded, not actioned here):** sibling
`holomush-5rh.24`'s current "writes ride the command path per D4" framing is
superseded by the same principle; its management verbs SHOULD also become typed
RPCs. Flagged on that bead; not designed here.

---

## 3. Components & data flow

### 3.1 Proto

Two new request/response pairs and two new RPCs MUST be added, each fully
documented per `.claude/rules/proto-doc-comments.md` (comments grounded in the
implementing handler, no name-echo).

- `SceneAccessService.CreateScene` (`api/proto/holomush/sceneaccess/v1`):
  - Request: `player_session_token`, `character_id`, `title`, `description`.
  - Response: `SceneInfo` (the created scene).
- `WebService.WebCreateScene` (`api/proto/holomush/web/v1/web.proto`):
  - Request: `session_id`, `character_id`, `title`, `description`.
  - Response: `SceneInfo`.

`SceneService.CreateScene` (`scenev1.CreateSceneRequest{CharacterId, Title,
Description, …}`) is unchanged. `Visibility`, `LocationId`, `PoseOrderMode`,
`ContentWarnings` default (location-optional = off-grid scene), matching telnet
`scene create`'s title-only behavior.

### 3.2 Facade (`internal/grpc/sceneaccess_service.go`)

`SceneAccessServer.CreateScene` MUST mirror the established write template used by
`WatchScene` (`:232`):

1. `ps, err := s.resolveAndGate(ctx, req.GetPlayerSessionToken())` — resolve the
   player-session token to the player identity **and gate the request**. This is
   where **guest denial** is enforced for web-portal surfaces
   (INV-SCENE-64); a guest token MUST be rejected here.
2. `char, err := s.ownedCharacter(ctx, ps.PlayerID, req.GetCharacterId())` —
   verify the player owns the requested character (anti-forgery). A character not
   owned by the resolved player MUST be rejected.
3. `dctx, release, err := s.beginDispatch(ctx, char, ps.PlayerID)` — establish the
   host-vouched dispatch context for the downstream service call; `defer release()`.
4. `resp, err := s.sceneClient.CreateScene(dctx, &scenev1.CreateSceneRequest{
   CharacterId: char.ID.String(), Title: req.GetTitle(), Description:
   req.GetDescription()})`.
5. Return `&sceneaccessv1.CreateSceneResponse{Scene: resp.GetScene()}`.

`CreateScene` MUST NOT require an existing game session (unlike `WatchScene`,
which does `FindByCharacter` because it piggybacks on focus). Creation does not
touch focus or sessions.

Errors MUST be opaque at the boundary per `.claude/rules/grpc-errors.md`: log
inner errors with `errutil.LogErrorContext`; return `status.Errorf(codes.Internal,
"internal error")`; pass plugin status errors through as-is.

### 3.3 BFF (`internal/web/scene_handlers.go`)

`Handler.WebCreateScene` MUST proxy to `SceneAccessClient.CreateScene`, forwarding
the session token + op fields exactly as `WebWatchScene` (`:120`) does, returning
the `SceneInfo`. The gateway remains protocol-translation-only
(`.claude/rules/gateway-boundary.md`): no DB access, no business logic.

### 3.4 Web client (`web/src/lib/scenes/client.ts`)

Add `createScene(sessionId, { characterId, title, description }) → SceneInfo`,
wrapping `client.webCreateScene(...)`. It does **not** use `sendCommand`; the
existing `sendSceneCommand` (`:101`) is untouched and remains for pose/say/ooc/join.

### 3.5 UI

**Acting character.** Create runs as a chosen character. With one character, it is
used implicitly. With more than one, the Sheet MUST present an acting-character
selector (default: first). The character's per-alt session is obtained via the
existing `ensureSession(characterId)` (`web/src/lib/scenes/altSessions.svelte.ts`).
`connectionId` is **not** required (create is request/response, not command
routing).

**`CreateSceneSheet.svelte`** (new) — a slide-over `Sheet` (existing
`web/src/lib/components/ui/sheet` primitive) containing:
- **Title** (`input`, required; Create disabled until non-empty).
- **Description** (raw HTML `<textarea>`, styled as in `SceneComposer.svelte` —
  there is no `ui/textarea` shadcn primitive; optional).
- Acting-character selector when `characters.length > 1`.
- **Create scene** button + Cancel.

On submit it calls `createScene`, then `workspaceStore.refresh(characters)`
(`workspaceStore.svelte.ts:69`) followed by `workspaceStore.select(newScene.id, '', actingCharacterId)`
(`:135`; the 2nd arg is the unused legacy `_playerSessionId`, the 3rd is the
acting character's id) so the new scene appears in *My Scenes* and is focused. The Sheet closes
on success; failures surface inline (no close), mirroring `SceneComposer`'s
error handling.

**Left-rail toolbar** (`web/src/routes/(authed)/scenes/+page.svelte`) — a new thin
action bar pinned above the *My Scenes* header (`~:300`): **+ New scene**
(primary), **Browse**, **Archive**. The current footer link `+ browse · ⌕ archive`
(`:347-352`) is removed (consolidated into the toolbar). **+ New scene** opens the
Sheet.

**Empty-state CTA** — the center pane's "No scene selected" empty state
(`:406-415`) MUST gain a **+ New scene** button (the new-player fix), opening the
same Sheet.

Brand: the primary action uses brand cyan (`--brand-cyan-bright` /
tile gradient); amber is **not** used as an accent here
(`.claude/rules/branding.md`).

---

## 4. Authorization

Create authorization is enforced **at the facade**, reusing the existing template:

- **Guest denial** is inherited from `resolveAndGate` (INV-SCENE-64 — guests
  denied at the facade for web-portal surfaces). No new guest logic is added here;
  the terminal-command guest gate and the nav-link gate remain `holomush-5rh.23`.
- **Character ownership** is enforced by `ownedCharacter` (the player MUST own the
  acting character).

**Open question for design/ABAC review:** v2 ABAC (§5.4) gates scene creation
*only* by `command:scene execute` (v1's resource-level `scene:<id> create` action
was removed). For a registered player owning the character, `resolveAndGate`
(non-guest) + `ownedCharacter` is the natural facade equivalent and is the default
"any registered character may create" policy. If the game must support a
*further configurable* restriction on who may create scenes, the facade MUST
evaluate an explicit policy (reusing the `command:scene execute` predicate rather
than minting a new action). This spec assumes the default (guest-denied,
owner-scoped) and defers any configurable-restriction policy to review. This is
**not** a new invariant family; it reuses INV-SCENE-64 and the existing
ownership/dispatch pattern.

---

## 5. Error handling

- Facade & BFF: opaque `Internal` to the client; inner errors logged with
  `errutil.LogErrorContext(ctx, …)` (`.claude/rules/grpc-errors.md`).
- Translate gRPC↔oops at one layer only (the BFF boundary); plugin status errors
  pass through.
- UI: a failed `createScene` keeps the Sheet open and shows the error message;
  the draft is preserved. A successful create that then fails to refresh/select
  MUST still leave the scene created (the create is authoritative); refresh/select
  is best-effort and retriable by reopening the workspace.

---

## 6. Testing

- **Facade (Go unit, `internal/grpc`):** `CreateScene` — allows a registered owner
  (calls `sceneClient.CreateScene`, returns `SceneInfo`); denies a guest
  (resolveAndGate); denies a character the player does not own; opaque error on
  downstream failure. Mirror the `mockSceneAccessClient` /
  `scene_handlers_test.go` patterns.
- **BFF (Go unit, `internal/web`):** `WebCreateScene` forwards token + op fields to
  the facade and passes status errors through as-is (mirror
  `TestWebWatchSceneForwardsTokenAndOpFieldsToFacade` /
  `…PassesStatusErrorThroughAsIs`).
- **Web (vitest):** `createScene` client wrapper; `CreateSceneSheet` component —
  required-title validation, calls `createScene`, closes on success, error on
  failure, acting-character selector appears only when >1 character.
- **E2E (Playwright):** a registered player with **no telnet session** lands on an
  empty `/scenes`, clicks **+ New scene**, enters a title (+ optional
  description), submits, and the new scene appears in *My Scenes* and is focused.
  This is the telnet-free path that MAY later bind a per-subsystem
  "web surface is self-sufficient" invariant for scenes (per `holomush-sz0h3`).
- Run `task test:int -- ./test/integration/scenes/...` and `task pr-prep` before
  push. (Adding a method to the already-provided `SceneAccessService` needs no
  manifest change; verify no `plugins/core-scenes/plugin.yaml` capability/`provides`
  change is required.)

---

## 7. Invariants

- **Reuses** INV-SCENE-64 (guest denied at the facade for web-portal surfaces).
- Introduces **no** new invariant family. The create-authorization behavior is an
  instance of existing guest-deny + ownership patterns. A future per-subsystem
  "scenes web surface requires no telnet" invariant (from `holomush-sz0h3`) MAY be
  minted and bound by the §6 E2E once the surface is genuinely telnet-free; that
  is out of scope here.

---

## 8. D4 / window-per-scene disposition (bead R1)

The bead carries a second residual: E9.5 D4 specified "window-per-scene routing
(web)". The shipped portal instead delivers a **single-pane scene-switcher
workspace** (`web/src/lib/scenes/workspaceStore.svelte.ts` `selectedSceneId` +
3-pane layout) with unread badges and quick-switch, which was approved via the
E9.5 visual-companion mockups (v3, D11).

**Disposition: resolved-by-evolution.** The single-pane workspace satisfies D4's
intent ("the web renders threads"). True multi-window / pop-out is **not** built
in this slice and is not planned. This spec records the disposition; no code
implements R1.

---

## 9. Open questions for the reviewer

1. Facade create-authorization: is guest-deny (INV-SCENE-64) + `ownedCharacter`
   sufficient, or must the facade additionally evaluate a configurable
   `command:scene execute`–equivalent policy (§4)?
2. Acting-character default when `characters.length > 1`: first character is
   proposed — acceptable, or should it follow a "primary"/most-recent signal?
3. Toolbar consolidation: removing the footer `+ browse · ⌕ archive` link in favor
   of the toolbar — confirmed (mockup-approved), flagged here for completeness.

<!-- adr-capture: sha256=42a5599e370649cc; session=535177b8; ts=2026-06-20T00:18:51Z; adrs=holomush-v4qmu,holomush-x8swp -->
