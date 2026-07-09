---
phase: 2
reviewers: [codex]
review_round: 2
reviewed_at: 2026-07-09T14:50:31Z
plans_reviewed: [02-01-PLAN.md,02-02-PLAN.md 02-03-PLAN.md,02-04-PLAN.md 02-05-PLAN.md,02-06-PLAN.md 02-07-PLAN.md]
supersedes: round-1 review (incorporated at commit 44076c92a)
---

# Cross-AI Plan Review — Phase 2: Scenes Lineage Completion (ROUND 2)

> Reviewer: **Codex** (codex-cli 0.143.0). Source-grounded against the working tree at commit 44076c92a (post round-1 incorporation). Round 1's 5 findings were incorporated via `/gsd-plan-phase 2 --reviews`; this round verifies their resolution and surfaces issues the incorporation introduced. Single-reviewer run (`claude` skipped, self-CLI).

## Codex Review

**Round-1 Resolution Table**

| Finding | Verdict | Evidence |
|---|---|---|
| 1. Idle store cannot see game default | RESOLVED | Plan 06 now requires `ListScenesIdlePastThreshold(ctx, nowNs, defaultIdleTimeoutSecs int)` and explicit scheduler passing at `.planning/phases/02-scenes-lineage-completion/02-06-PLAN.md:21`, `:84-85`, `:117`. That matches source reality: `SceneStore` only holds `pool *pgxpool.Pool` at `plugins/core-scenes/store.go:83-85`; config is decoded in plugin lifecycle at `plugins/core-scenes/main.go:46-74`; current manifest config lives under `plugins/core-scenes/plugin.yaml:37-49`. |
| 2. Global notify-off persisted but not enforced | PARTIALLY-RESOLVED | The intended enforcement path is now present in the plans: Plan 02 persists `GetSceneNotifyPref`/`SetSceneNotifyPref` at `.planning/.../02-02-PLAN.md:70`, `:113`; Plan 03 adds plugin RPCs including `GetSceneNotifyPref` at `.planning/.../02-03-PLAN.md:73-79`, `:105`; Plan 04 reads both global pref and muted set in `ShouldSuppress` at `.planning/.../02-04-PLAN.md:74-77`, `:113`, and fails open at `:110`, `:140`. The source chokepoint is correct: `SCENE_ACTIVITY` is built and sent only in `internal/grpc/server.go:1278-1314`. However, see New Concern 1: Plan 03’s `SetSceneNotifyPref` ABAC/signature text is internally inconsistent and can block implementation. |
| 3. Loader lacked trusted dispatch identity | RESOLVED | Plan 04 explicitly threads `playerID` through `ShouldSuppress` and loader dispatch at `.planning/.../02-04-PLAN.md:74`, `:89-91`, `:164`. This matches the existing facade shape: `BeginServiceDispatch(ctx, "core-scenes", actor, playerID.String())` in `internal/grpc/sceneaccess_service.go:152-154`, and the host contract says `ownerPlayerID` is the vouched owning player at `internal/plugin/goplugin/host.go:1321-1324`. The downgrade site can access session identity from `currentInfo` read at `internal/grpc/server.go:1256` and existing player/character identity use at `internal/grpc/server.go:993`. |
| 4. Idle nudge could not reach `[>GAME: …]` | RESOLVED | Plan 01 creates `gamenotice.Idle` at `.planning/.../02-01-PLAN.md:66-73`, `:87-89`. Plan 06 depends on Plan 01 at `.planning/.../02-06-PLAN.md:6` and adds a separate `core-scenes:scene_idle_nudge` EventFrame render to `gamenotice.Idle` at `.planning/.../02-06-PLAN.md:23`, `:93-94`, `:190`. This is coherent with source: telnet’s control switch is currently only at `internal/telnet/gateway_handler.go:323-344`, while system EventFrames currently route through `formatSystem` at `internal/telnet/gateway_handler.go:1226-1234`. The event type already exists in manifest/registry at `plugins/core-scenes/plugin.yaml:113-117`, `plugins/core-scenes/main.go:177-187`. |
| 5. Stale `scene.proto` ABAC comment | RESOLVED | Current source is indeed stale: it says “plugin itself runs NO ABAC engine” at `api/proto/holomush/scene/v1/scene.proto:22-29`. Plan 03 explicitly rewords it and gates with proto lint at `.planning/.../02-03-PLAN.md:73-79`, `:93-96`, `:115-116`. Source supports the correction: `SceneServiceImpl` already has `evaluator pluginsdk.HostEvaluator` at `plugins/core-scenes/service.go:176-180`, and lifecycle handlers call it, e.g. `Evaluate(ctx, "end", ...)` at `plugins/core-scenes/service.go:707-712`. |

**New Concerns**

[HIGH] `.planning/phases/02-scenes-lineage-completion/02-03-PLAN.md:109` — `SetSceneNotifyPref` is described as carrying only `enabled`, but the same action says `MuteScene/SetSceneNotifyPref` should call `req.GetSceneId()` and evaluate `scene:<id>`. A global notify preference has no scene id. Existing scene ABAC handlers only use `Evaluate(..., "scene:"+req.GetSceneId())` for requests that actually include `scene_id`, e.g. `EndSceneRequest` at `api/proto/holomush/scene/v1/scene.proto:404-410` and handler code at `plugins/core-scenes/service.go:707-712`. As written, an executor will either hit a compile-time missing `GetSceneId()` or invent an empty/nonsensical scene resource for a global preference.

[MEDIUM] `.planning/phases/02-scenes-lineage-completion/02-03-PLAN.md:109`, `.planning/phases/02-scenes-lineage-completion/02-04-PLAN.md:164` — the new plugin RPCs are “character-scoped via context,” but current plugin RPC convention carries `character_id` in the request and checks actor metadata against it, e.g. `scene.proto` request fields at `api/proto/holomush/scene/v1/scene.proto:406-410`, service mismatch guards at `plugins/core-scenes/service.go:696-704`, and facade forwarding of verified `CharacterId` at `internal/grpc/sceneaccess_service.go:433-436`. The host dispatch contract also says the vouched actor and request payload identity must match at `internal/plugin/goplugin/host.go:1312-1317`. The plans should require `character_id` on `MuteScene`, `SetSceneNotifyPref`, `GetSceneNotifyPref`, and `ListMutedScenes` plugin requests, populated from the verified actor/loader input.

[LOW] `.planning/phases/02-scenes-lineage-completion/02-06-PLAN.md:138`, `:141` — Plan 06 says to emit through the plugin’s standard path but names `core.NewEvent()`. Existing binary plugin emission uses `pluginsdk.EmitIntent` through `EventSink.Emit`, not host-core `core.Event`: see `pkg/plugin/event.go:117-131`, `pkg/plugin/event_sink.go:25-29`, and existing core-scenes emitters at `plugins/core-scenes/service.go:1153-1159` / `plugins/core-scenes/publish_events.go:57-62`. This is likely executor confusion rather than architectural failure, but the plan should say `pluginsdk.EmitIntent{Type: "core-scenes:scene_idle_nudge", ...}`.

**Summary + Overall Risk**

Overall risk: MEDIUM. The round-1 fixes are mostly incorporated and source-grounded, especially the explicit idle default, host-vouched dispatch identity, idle telnet render, and proto comment correction. The main remaining risk is Plan 03’s new RPC contract: global notify prefs and character-scoped reads need a clean identity/request shape before execution, or Plan 04/05 will be built on mismatched generated protobuf methods.

---

## Consensus Summary

Round-2 single-reviewer (Codex). **4 of 5 round-1 findings fully RESOLVED, 1 PARTIALLY-RESOLVED.** The 3 new concerns all stem from a single root — the new notify-preference RPCs (`SetSceneNotifyPref`/`GetSceneNotifyPref`) that round-1's fix added were given an identity/ABAC shape inconsistent with the plugin's existing RPC convention. Overall reviewer risk: **MEDIUM — one HIGH RPC-contract bug to fix before execution; the loop is converging (5→3 findings, all localized to new surface).**

### Round-1 Resolution
- **RESOLVED (4):** #1 idle store explicit default; #3 dispatch identity (`playerID` via `BeginServiceDispatch`, matches `sceneaccess_service.go:152-154`); #4 idle render via `gamenotice.Idle`; #5 stale proto comment.
- **PARTIALLY-RESOLVED (1):** #2 global notify-off — the enforcement *path* is correctly wired (persist in 02/03/05 → read in Plan 04 `ShouldSuppress` at the `server.go:1278-1314` chokepoint, fail-open), but the new `SetSceneNotifyPref` RPC's ABAC/identity shape is broken (New Concern 1/2).

### New Concerns (introduced by round-1 incorporation)
1. **[HIGH] `SetSceneNotifyPref` ABAC resource mismatch** (`02-03-PLAN.md:109`). It is grouped with `MuteScene` under an action that evaluates `scene:<id>` via `req.GetSceneId()` — but a **global** notify pref has no scene id (contrast `EndSceneRequest` at `scene.proto:404-410` which does). Failure mode: executor hits a compile-time missing `GetSceneId()` or invents a nonsensical empty scene resource for a global pref. Fix: give the global pref its own ABAC shape (a character/self-scoped action, not `scene:<id>`), separate from the scene-scoped `MuteScene`.
2. **[MEDIUM] Plugin RPC identity convention** (`02-03-PLAN.md:109`, `02-04-PLAN.md:164`). The new RPCs are specified "character-scoped via context", but the plugin convention carries `character_id` in the request and guards it against the vouched actor metadata (`service.go:696-704`, `sceneaccess_service.go:433-436`, `host.go:1312-1317`). Fix: require `character_id` on `MuteScene`/`SetSceneNotifyPref`/`GetSceneNotifyPref`/`ListMutedScenes`, populated from the verified actor/loader input.
3. **[LOW] Emit API naming** (`02-06-PLAN.md:138,141`). Plan 06 says `core.NewEvent()`, but binary plugins emit via `pluginsdk.EmitIntent` through `EventSink.Emit` (`pkg/plugin/event.go:117-131`, `plugins/core-scenes/publish_events.go:57-62`). Fix: say `pluginsdk.EmitIntent{Type: "core-scenes:scene_idle_nudge", ...}`.

### Divergent Views
None — single reviewer.

### Recommended next step
Findings 1 (HIGH) and 2 (MEDIUM) are a coherent RPC-contract cleanup on the notify-pref surface; 3 is a naming correction. All three are cheap, localized plan edits. Fold in with:

    /gsd-plan-phase 2 --reviews

Then execute. Fixing the global-vs-scene ABAC shape (Concern 1) in the plan — rather than leaving the executor to improvise it on a security surface — is the reason to run one more `--reviews` pass before `/gsd-execute-phase 2`.
