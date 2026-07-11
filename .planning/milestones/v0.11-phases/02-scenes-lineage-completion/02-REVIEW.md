---
phase: 02-scenes-lineage-completion
reviewed: 2026-07-09T00:00:00Z
depth: standard
files_reviewed: 25
files_reviewed_list:
  - api/proto/holomush/scene/v1/scene.proto
  - api/proto/holomush/sceneaccess/v1/sceneaccess.proto
  - api/proto/holomush/web/v1/web.proto
  - internal/telnet/gamenotice/gamenotice.go
  - internal/telnet/gateway_handler.go
  - plugins/core-scenes/store.go
  - plugins/core-scenes/service.go
  - plugins/core-scenes/commands.go
  - plugins/core-scenes/idle_scheduler.go
  - plugins/core-scenes/main.go
  - plugins/core-scenes/plugin.yaml
  - plugins/core-scenes/migrations/000011_scene_notify_prefs.up.sql
  - plugins/core-scenes/migrations/000011_scene_notify_prefs.down.sql
  - internal/grpc/scene_mute_cache.go
  - internal/grpc/server.go
  - internal/grpc/sceneaccess_service.go
  - internal/grpc/client.go
  - internal/grpc/focus/restore_connection_focus.go
  - internal/web/scene_handlers.go
  - internal/web/handler.go
  - cmd/holomush/sub_grpc.go
  - web/src/lib/scenes/notifyFlow.ts
  - web/src/lib/scenes/client.ts
  - web/src/lib/scenes/types.ts
  - web/src/lib/scenes/workspaceStore.svelte.ts
  - web/src/lib/components/scenes/SceneContextRail.svelte
findings:
  critical: 0
  warning: 2
  info: 3
  total: 5
status: issues_found
---

# Phase 2: Code Review Report

**Reviewed:** 2026-07-09
**Depth:** standard
**Files Reviewed:** 25
**Status:** issues_found

## Summary

Reviewed the Phase 2 scene-activity notification surface: the plugin-owned
`scene_notify_prefs` store, the participant-gated `MuteScene` RPC, the
character-self-scoped `SetSceneNotifyPref`/`GetSceneNotifyPref`/`ListMutedScenes`
RPCs, the core `SceneMuteChecker` badge-suppression path, the idle-timeout
lifecycle, the telnet `[>GAME:…]` leader, and the web mute slice.

The high-value security surfaces are sound. The facade (`sceneaccess_service.go`)
correctly stamps the verified `CharacterId` on both `MuteScene` (line 473) and
`SetSceneNotifyPref` (line 503), mirroring `ResumeScene`. Mute is participant-gated
via `scene:<id>` ABAC + the actor-metadata cross-check; the notify-pref trio is
character-self-scoped via the actor-binding guard as designed (not a plugin-local
`character:<id>` Layer-2 Evaluate). The badge-downgrade suppression read path is
correctly fail-OPEN (`scene_mute_cache.go`, `server.go:1346-1362`); the mute WRITE
and structural writes are fail-CLOSED. The TTL cache calls the loader off-lock and
never caches errors (no poisoning). The telnet leader carries only a scene id
(INV-SCENE-70); the idle nudge is emitter-only and OFF by default. The migration's
two partial unique indexes are correct and the down-migration reverses cleanly.
Web structural writes route through typed facade RPCs, never the command path.

No BLOCKER-level defects found. Two functional/robustness WARNINGs and three INFO
items follow.

## Warnings

### WR-01: Observer-role scenes show a Mute toggle the backend ABAC always denies

**File:** `web/src/lib/components/scenes/SceneContextRail.svelte:183-193`, `plugins/core-scenes/plugin.yaml:332-335`, `plugins/core-scenes/service.go:963-971`
**Issue:** The mute toggle is rendered **unconditionally** (not gated on
`isParticipant`, which is defined at `SceneContextRail.svelte:26` as
`role === 'owner' || role === 'member'`). The workspace also builds an observer
list (`watching = myScenes.filter(role === 'observer')`,
`workspaceStore.svelte.ts:138`) and tracks `muted` on those rows. But the backend
`mute-scene-as-participant` policy gates on `principal.id in resource.scene.participants`,
and `resource.scene.participants` is resolved from `GetWithMembership`
(`store.go:291-294`) as `role IN ('owner','member')` — **observers are excluded**
(the separate `spectate-open-scene` policy exists precisely because observers are
not "participants"). So an observer who selects a scene they are watching sees the
`🔔 Mute` button, clicks it, and `MuteScene` returns `PermissionDenied`
(`service.go:969-971`); `toggleSceneMute` throws and the mute never persists.
Observers legitimately receive `SCENE_ACTIVITY` badges when non-focused and the
suppression READ path (`ListMutedScenes`) would honor an observer's mute — only
the WRITE path denies them, leaving the feature permanently broken for observed
scenes. Fail-closed, so not a security hole, but a real UI/backend inconsistency.
**Fix:** Pick one contract and apply it on both sides. Either hide the control for
observers:

```svelte
{#if isParticipant}
  <div class="flex flex-wrap gap-1.5 pl-4 pt-2"> … mute button … </div>
{/if}
```

or, if observers are meant to mute, widen the policy resource attribute to include
observer rows (and confirm the resolver surfaces them).

### WR-02: Notify-pref self-scope guard fails OPEN when actor metadata is absent

**File:** `plugins/core-scenes/service.go:932-935` (`mismatchedActingCharacter`), consumed at `service.go:995-999` (`SetSceneNotifyPref`) and `service.go:1022-1026` (`GetSceneNotifyPref`)
**Issue:** `mismatchedActingCharacter` returns `ok && kind == ActorCharacter && id != requestCharacterID`. When no actor metadata is present (`ok == false`) it
returns `false` — i.e. "not mismatched" — so the request is accepted. For
`MuteScene`/`EndScene` this is backstopped by a fail-closed ABAC `Evaluate`
(missing subject → no policy match → deny), but the notify-pref trio
(`SetSceneNotifyPref`, `GetSceneNotifyPref`, `ListMutedScenes`) performs **no** ABAC
evaluation — the actor-binding guard is their _sole_ authorization. If any of these
RPCs is ever reached without host-vouched actor metadata, the guard passes and the
caller can read/write **any** `character_id`'s global notify preference. In the
current architecture the host always stamps actor metadata via
`BeginServiceDispatch` (facade at `sceneaccess_service.go:154`; loader at
`sub_grpc.go:593`), so this is not reachable in production dispatch today — but the
guard that is the _only_ gate for a self-scoped write should fail CLOSED on absent
identity rather than open.
**Fix:** For the notify-pref handlers, require metadata presence explicitly:

```go
kind, id, ok := pluginsdk.ActorMetadataFromIncomingContext(ctx)
if !ok || kind != pluginsdk.ActorCharacter || id != req.GetCharacterId() {
    return nil, status.Error(codes.PermissionDenied, "not permitted to set prefs for this character")
}
```

(i.e. a fail-closed variant of the guard for the RPCs that lack an ABAC backstop).

## Info

### IN-01: Sub-second `idle_timeout_default` truncates to 0s and pauses all active scenes

**File:** `plugins/core-scenes/main.go:292`
**Issue:** `defaultIdleTimeoutSecs: int(p.idleTimeoutDefault.Seconds())` truncates
toward zero. Init validates `idleTimeoutDefault > 0` (`main.go:78-82`) but not
`>= 1s`, so a configured value in `(0, 1s)` passes validation yet yields
`0` seconds. `ListScenesIdlePastThreshold` (`store.go:1855-1872`) would then treat
every active scene as past-threshold on each sweep and auto-pause them. The default
is `30m`, so this is not hit in practice.
**Fix:** Validate `idleTimeoutDefault >= time.Second` at Init, or round up when
converting to seconds.

### IN-02: Idle nudge is written into the scene IC audit log / history

**File:** `plugins/core-scenes/idle_scheduler.go:117-133`
**Issue:** `emitIdleNudge` emits `scene_idle_nudge` on the scene **IC subject**
(`dotStyleSceneSubjectIC`). Because the plugin's audit ownership covers
`events.*.scene.>` (`plugin.yaml:66`), the nudge is persisted into
`plugin_core_scenes.scene_log` and will surface in IC history replay/backfill and
potentially scene export. It is content-free and OFF by default, but mixing a
system idle notice into the IC content log may be unintended.
**Fix:** If the nudge should not appear in IC history, emit it on a non-IC scene
facet (or a control/notice subject) rather than the `.ic` subject; otherwise
confirm `eventFrameToLogEntry` filters it and document the intent.

### IN-03: Per-scene telnet nudge debounce map is never pruned

**File:** `internal/telnet/gateway_handler.go:113,143-152`
**Issue:** `sceneNudgeLast` gains one entry per distinct scene id ever nudged on a
connection and is never pruned, so a long-lived telnet session accumulates
map entries for scenes it will never see again. Bounded by the number of distinct
scenes touched, single-consumer (no lock needed as noted). Memory growth is a
performance concern (out of v1 review scope) but noted for completeness.
**Fix:** Optionally evict entries older than `sceneNudgeWindow` during
`sceneActivityLine`, or cap the map size.

---

_Reviewed: 2026-07-09_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
