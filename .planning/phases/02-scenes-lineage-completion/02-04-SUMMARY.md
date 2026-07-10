---
phase: 02-scenes-lineage-completion
plan: 04
subsystem: core-notification-mute-suppression
tags: [scenes, notifications, mute, badge-downgrade, dependency-inversion, ttl-cache, fail-open]
requires:
  - "SceneService.GetSceneNotifyPref + ListMutedScenes RPCs (Plan 03)"
  - "SceneAccessServer.beginDispatch host-vouched BeginServiceDispatch precedent (sceneaccess_service.go)"
  - "SCENE_ACTIVITY badge downgrade at server.go:1296 (INV-SCENE-62 content-free frame)"
provides:
  - "SceneMuteChecker interface + NewSceneMuteChecker exported constructor (internal/grpc)"
  - "WithSceneMuteChecker CoreServer option"
  - "per-character TTL notify-state cache (global pref + muted set) — one loader refresh per window"
  - "badge-downgrade suppression branch honoring global-notify-off OR per-scene mute, fail-open"
  - "production wiring: loader dials plugin SceneService with host-vouched actor+ownerPlayerID"
affects:
  - internal/grpc/server.go
  - cmd/holomush/sub_grpc.go
tech-stack:
  added: []
  patterns:
    - "dependency inversion: CoreServer depends on a narrow SceneMuteChecker interface; concrete checker wired at cmd/holomush (no scene-plugin import in server.go)"
    - "fail-OPEN read path: nil checker or any loader error delivers the content-free badge (preferences, not access control)"
    - "pinned suppression order: global-notify-off → suppress; else scene-muted → suppress; else deliver"
    - "per-character TTL cache; loader never held under the mutex (no per-event RPC, no hot-loop lock across dispatch)"
    - "host-vouched loader dispatch via BeginServiceDispatch(actor+ownerPlayerID), request character_id guarded plugin-side"
key-files:
  created:
    - internal/grpc/scene_mute_cache.go
    - internal/grpc/scene_mute_cache_test.go
  modified:
    - internal/grpc/server.go
    - internal/grpc/subscribe_loop_test.go
    - cmd/holomush/sub_grpc.go
decisions:
  - "Construction-seam coverage: the acceptance's 'production wiring holds a non-nil checker' is proven proportionately by (a) `task build` green (the loader resolves against the real scenev1 client + BeginServiceDispatch at construction) and (b) a unit test asserting the EXPORTED NewSceneMuteChecker constructor + WithSceneMuteChecker option compose to place a live loader-backed checker onto CoreServer. A full sub_grpc production-wiring test would require standing up the plugin subsystem + DB + NATS; the loader is a local closure not independently addressable, so the build + seam test are the practical guarantee."
  - "TTL = 45s (mid-range of the recommended 30-60s window): a mute/unmute or SetSceneNotifyPref takes effect within ~one window while the badge-downgrade branch makes at most one plugin round-trip per character per window."
  - "Loader is NOT held under the cache mutex during the plugin dispatch: a slow round-trip for one character cannot block concurrent deliveries for others. Trade-off is a possible duplicate concurrent refresh for the same character on first miss — harmless (fail-open, best-effort preference)."
  - "sid from extractSceneID is a bare scene ULID; the muted set (store ListMutedScenes) holds bare scene ULIDs — formats match, no normalization needed."
metrics:
  duration: ~35m
  completed: 2026-07-09
status: complete
---

# Phase 2 Plan 04: Scene Mute / Notify Suppression at the Badge Downgrade Summary

Made `scene mute` AND the per-character global notify preference (Plan 03) actually
suppress notifications. The SCENE_ACTIVITY badge downgrade at `server.go:1296` — the
single chokepoint where a non-focused member's scene event becomes a content-free
ping feeding both the web badge and the Plan-01 telnet nudge — now consults a
dependency-inverted `SceneMuteChecker` and drops the frame when the character's
GLOBAL notifications are OFF (review Finding 2) OR the specific scene is muted. The
check is TTL-cached per character (no per-event RPC), reached through a narrow
injected interface (CoreServer never imports the scene plugin), and fails OPEN — a
nil checker or any error delivers the frame, since mute/notify-pref are preferences,
not access control (INV-SCENE-62 privacy is unaffected: the frame is content-free
either way). Without this, `scene mute` / `SetSceneNotifyPref` persisted preferences
nobody read — this is the read/enforcement half of D-04.

## What was built

- **Task 1 — `SceneMuteChecker` interface + per-character TTL cache** (`internal/grpc/scene_mute_cache.go`, `scene_mute_cache_test.go`, NEW). A `SceneMuteChecker` interface (`ShouldSuppress(ctx, characterID, playerID, sceneID) (bool, error)`), an exported `SceneMuteLoader` func type, an exported `NewSceneMuteChecker(loader, ttl, now)` constructor (the sole cross-package construction seam — the concrete `sceneMuteCache` stays unexported so `cmd/holomush` reaches it only through the constructor + interface), and a mutex-guarded per-character cache memoizing `{globalNotifyEnabled, mutedSet, fetchedAt}`. `ShouldSuppress = !globalNotifyEnabled || sceneID ∈ mutedSet` (global-off → suppress; else muted → suppress; else deliver). The loader is called off-lock; on error it returns `(false, err)` and never poisons the cache. TDD: 8 unit tests (global-off suppresses any scene; muted scene suppresses when global on; unmuted+global-on delivers; ≤1 loader call per TTL window; refresh after expiry; error fail-open without poisoning; per-character isolation; exported constructor+option seam).
- **Task 2 — suppression branch at the badge downgrade** (`internal/grpc/server.go`). Added an optional `sceneMute SceneMuteChecker` field + `WithSceneMuteChecker` option (mirroring the existing `With*` options). Inside the existing `if !focusedOn { … }` branch at :1296, BEFORE building the badge: when `s.sceneMute != nil`, call `ShouldSuppress(ctx, currentInfo.CharacterID.String(), currentInfo.PlayerID.String(), sid)`; on `(true, nil)` ack-and-drop (no frame); on `(false, _)` or a non-nil error fall through to the existing badge send (fail-open, DebugContext-logged). The check never runs on the focused path. TDD: 4 dispatch tests (suppress → no frame + ack + correct char/player/scene args; allow → badge sent; error → badge sent fail-open; focused path → checker never consulted).
- **Task 3 — wire the live checker into CoreServer construction** (`cmd/holomush/sub_grpc.go`). Before `NewCoreServer`, resolve the plugin `SceneService` via the SAME `serviceRegistry.Resolve("holomush.scene.v1.SceneService")` dial the SceneAccessService facade uses, build a `SceneMuteLoader` that (1) opens a host-vouched dispatch `pluginManager.BeginServiceDispatch(ctx, "core-scenes", core.Actor{Kind: ActorCharacter, ID: characterID}, playerID)` + `defer release()` (mirroring `SceneAccessServer.beginDispatch`, Finding 3), then (2) calls `GetSceneNotifyPref` and `ListMutedScenes` on the vouched ctx — each request carrying `CharacterId: characterID` (guarded plugin-side against the vouched actor metadata) — returning `(enabled, sceneIds, nil)`. Injected via `WithSceneMuteChecker(NewSceneMuteChecker(loader, 45s, time.Now))`. When the plugin is absent the checker stays unset and CoreServer fails OPEN (delivers every badge), WarnContext-logged. Hoisted the shared `sceneServiceName` const above both the checker and the existing facade resolve.

## Verification

- `task test -- ./internal/grpc/...` — PASS (608 tests; cache + suppression: global-off, muted, deliver, error-fail-open, focused-path-skip, per-character isolation, construction seam).
- `task test -- ./cmd/holomush/... ./internal/grpc/...` — PASS (1148 tests).
- `task build` — PASS (production wiring resolves the live checker against the real `scenev1` client + `BeginServiceDispatch` at construction).
- `task test:int` — PASS (10446 tests, 5 skipped — all pre-existing quarantine/documentary). `test/integration/scenes` green (1m40s), including the INV-SCENE-62 scene-activity badge-privacy test — the suppression only DROPS a frame, never adds content.
- `task lint:go` — PASS (0 issues). `task fmt` clean.

## Deviations from Plan

### [Rule 1 - lint] Dropped the now-constant `evType` param from the `makeSceneDelivery` test helper

- **Found during:** Task 3 (`task lint:go`).
- **Issue:** `unparam` flagged `makeSceneDelivery(t, evType, sceneID)` — every caller (the two existing scene tests plus the four new Task-2 tests) passes the constant `"core-scenes:scene_pose"`.
- **Fix:** Removed the `evType` parameter and hardcoded the type inside the helper; updated all 6 callers. Proper fix per CLAUDE.md (no ignore directive).
- **Files modified:** internal/grpc/subscribe_loop_test.go
- **Commit:** 60d113243

No Rule 2 (missing critical) additions, no Rule 3 blocking fixes, no Rule 4 architectural changes, no authentication gates.

## Known Stubs

None. The checker is fully wired: the persisted global notify pref (Plan 03's
`GetSceneNotifyPref`) and muted set (`ListMutedScenes`) finally have a consumer at
the badge downgrade.

## Threat Flags

None new. The change stays within the plan's STRIDE register: suppression only drops
the already-content-free frame (T-02-10, INV-SCENE-62 unchanged); the cache is keyed
by character id with per-character isolation unit-tested (T-02-11); the loader dials
the plugin only via host-vouched `BeginServiceDispatch` actor+ownerPlayerID
(T-02-11b); the per-character TTL bounds hot-loop RPCs (T-02-12); nil checker / loader
error fail OPEN (T-02-13); the global notify pref is now enforced before the per-scene
mute check (T-02-13b).

## Commits

- `11d91ccfd` — test(02-04): add failing SceneMuteChecker TTL cache tests (RED)
- `8d2dd7a18` — feat(02-04): SceneMuteChecker interface + per-character TTL notify cache (GREEN)
- `91738ea95` — feat(02-04): suppress SCENE_ACTIVITY badge for muted/notify-off members
- `60d113243` — feat(02-04): wire live SceneMuteChecker into CoreServer construction

## Self-Check: PASSED

- Files exist: `internal/grpc/scene_mute_cache.go`, `internal/grpc/scene_mute_cache_test.go`, `internal/grpc/server.go`, `internal/grpc/subscribe_loop_test.go`, `cmd/holomush/sub_grpc.go` — all confirmed on disk.
- Commits `11d91ccfd`, `8d2dd7a18`, `91738ea95`, `60d113243` present in git history.
- Acceptance greps pass: `WithSceneMuteChecker`/`BeginServiceDispatch`/`GetSceneNotifyPref`/`ListMutedScenes`/`holoGRPC.NewSceneMuteChecker`/`CharacterId: characterID` all present in `cmd/holomush/sub_grpc.go`; `sceneMute`/`ShouldSuppress` present in `internal/grpc/server.go` inside the `!focusedOn` branch.
