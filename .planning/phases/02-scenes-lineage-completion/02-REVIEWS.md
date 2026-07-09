---
phase: 2
reviewers: [codex]
reviewed_at: 2026-07-09T13:53:19Z
plans_reviewed: [02-01-PLAN.md,02-02-PLAN.md 02-03-PLAN.md,02-04-PLAN.md 02-05-PLAN.md,02-06-PLAN.md 02-07-PLAN.md]
---

# Cross-AI Plan Review — Phase 2: Scenes Lineage Completion

> Reviewer: **Codex** (codex-cli 0.143.0, default model). Source-grounded against the working tree on `gsd/v1.0-milestone` (commit bf0b997af). Single-reviewer run (`/gsd-review --phase 2 --codex`); `claude` skipped for independence (self-CLI).

## Codex Review

## Summary
The plan set is mostly well-grounded and correctly treats telnet scene activity as a rendering gap, not a new delivery subsystem. The main risks are in cross-plan contracts: idle timeout needs a default value that the proposed store API cannot currently see, global notify-off is persisted but not clearly enforced, and idle-nudge rendering is conflated with generic scene-activity badge rendering.

## Strengths
- The telnet notification premise is correct. `internal/grpc/server.go:1278-1314` already downgrades non-focused scene events to `CONTROL_SIGNAL_SCENE_ACTIVITY` with only `SceneId`, while `internal/telnet/gateway_handler.go:324-344` currently ignores every non-`STREAM_CLOSED` control frame.
- Non-participant privacy is already protected by the subscription/filter layer. `test/integration/scenes/scene_activity_badge_test.go:133-138` asserts a non-member receives no badge, and `docs/architecture/invariants.yaml:3300-3309` binds INV-SCENE-62 to that behavior.
- The “do not edit `validActions`” guidance is right. `internal/command/types.go:115-118` contains only core capability actions, while scene lifecycle handlers use engine actions like `end` through `Evaluate` in `plugins/core-scenes/service.go:707-719` and command gating in `plugins/core-scenes/commands.go:463-489`.
- `scene_idle_nudge` is already declared. It appears in manifest verbs at `plugins/core-scenes/plugin.yaml:113-116`, crypto emits at `plugins/core-scenes/plugin.yaml:177-180`, and the emit registry at `plugins/core-scenes/main.go:177-187`; Plan 06 is correct that re-registering it would be wrong.
- INV-SCENE-70/71 are free. The current registry ends at `INV-SCENE-69` in `docs/architecture/invariants.yaml:3381`.

## Concerns
- **HIGH: Plan 06’s idle store API cannot implement the game-default timeout as specified.** The plan asks for `ListScenesIdlePastThreshold(ctx, nowNs)` to use `COALESCE(idle_timeout_secs, <game_default_secs>)`, but `SceneStore` only holds a pool (`plugins/core-scenes/store.go:83-85`) and config is decoded into `scenePlugin` / `SceneServiceConfig` (`plugins/core-scenes/main.go:46-73`, `plugins/core-scenes/publish_helpers.go:108-116`). With only `nowNs`, the store has no source for `<game_default_secs>`. Failure mode: implementers hard-code a default, ignore NULL timeouts, or cannot compile cleanly.

- **MEDIUM: Global notify on/off is persisted but not wired into suppression.** The only current notification chokepoint is the badge downgrade at `internal/grpc/server.go:1287-1314`. Plan 04 only describes suppressing muted `(character, scene)` pairs via `ListMutedScenes`; it does not say the same branch checks the per-character global notify preference introduced by Plan 02/03/05. Failure mode: `WebSetSceneNotifyPref(false)` or telnet notify-off succeeds but scene activity badges/nudges still fire unless each scene is individually muted.

- **MEDIUM: Plan 04’s concrete checker loader underspecifies trusted dispatch identity.** The existing facade creates a host-vouched plugin dispatch with both actor and owner player id (`internal/grpc/sceneaccess_service.go:150-155`), and the plugin host documents `ownerPlayerID` as part of that trusted dispatch (`internal/plugin/goplugin/host.go:1321-1324`). Plan 04’s cache loader is only `func(ctx, characterID) ([]string, error)`. Failure mode: the core either calls plugin `ListMutedScenes` without the same dispatch context as web facade calls, or has to reinvent identity plumbing during implementation.

- **MEDIUM: Plan 06 promises idle notices through `[>GAME: …]`, but current rendering does not support that path.** Focused telnet clients receive normal EventFrames; system events render raw payload text via `formatSystem` (`internal/telnet/gateway_handler.go:1226-1234`). Non-focused clients would be downgraded to generic `SCENE_ACTIVITY`, which Plan 01 formats as “has new activity,” not “is now idle.” Failure mode: idle nudge either appears as generic activity, as raw system text, or not through the shared `gamenotice.Idle` primitive.

- **LOW: The scene proto service comment is stale relative to Plan 03.** `api/proto/holomush/scene/v1/scene.proto:22-29` says the plugin runs no ABAC engine except participant-gate reads, but current code already has an evaluator field for lifecycle gates (`plugins/core-scenes/service.go:176-180`, `231-237`). Adding mute ABAC should update that contract comment, or future readers will get contradictory guidance.

## Suggestions
- Change Plan 06’s store contract to `ListScenesIdlePastThreshold(ctx, nowNs int64, defaultIdleTimeoutSecs int)` or have the scheduler query candidate rows and apply the default in Go. Do not leave the default implicit in `SceneStore`.
- Extend Plan 04 to check both `GetSceneNotifyPref(characterID)` and per-scene mute at the badge downgrade. Suggested order: if global notifications disabled, suppress; else if scene muted, suppress; otherwise deliver.
- Make the Plan 04 checker loader accept the full session identity: `characterID` plus `playerID`, or build the checker at a layer that can call `BeginServiceDispatch` with `currentInfo.PlayerID`.
- Split idle nudge rendering into an explicit path: either render `core-scenes:scene_idle_nudge` EventFrames through the `gamenotice.Idle(sceneID)` primitive, or extend `ControlFrame` with a notice kind. As written, generic `SCENE_ACTIVITY` is not enough.
- Add generated proto outputs to `files_modified` for Plans 03 and 05 so reviewers can see expected churn and wave overlap clearly.

## Risk Assessment
**MEDIUM.** The delivery/privacy architecture is sound and the telnet edge-case plan is well-aligned with existing code. The remaining risk is not conceptual scope creep; it is contract drift between plans, especially Plan 06’s missing default input and Plan 04’s incomplete use of the newly persisted notify preference.

---

## Consensus Summary

Single external reviewer (Codex). It independently re-derived the plan set against
current source and **confirmed every load-bearing claim** the internal plan-checker
relied on, then surfaced **5 cross-plan contract gaps** the internal checker did not
catch. Overall reviewer risk: **MEDIUM — contract drift between plans, not scope creep.**

### Agreed Strengths (verified at `path:line`)
- Telnet scene-activity notify is correctly a **rendering gap**: `internal/grpc/server.go:1278-1314` already downgrades non-focused events to `CONTROL_SIGNAL_SCENE_ACTIVITY` (scene_id only); `internal/telnet/gateway_handler.go:324-344` ignores every non-`STREAM_CLOSED` control frame.
- **Non-participant privacy already holds** at the subscription/filter layer (`test/integration/scenes/scene_activity_badge_test.go:133-138`; INV-SCENE-62 at `docs/architecture/invariants.yaml:3300-3309`).
- `scene mute/unmute` correctly need **no `validActions` change** (engine-`Evaluate` precedent: `plugins/core-scenes/service.go:707-719`, `commands.go:463-489`; `internal/command/types.go:115-118` carries only core actions).
- `scene_idle_nudge` **already declared** (`plugin.yaml:113-116,177-180`, `main.go:177-187`) — Plan 06 correctly emitter-only.
- **INV-SCENE-70/71 are free** (registry ends at INV-SCENE-69, `invariants.yaml:3381`).

### Agreed Concerns (actionable — fold in before executing)
Ranked by severity:

1. **[HIGH] Plan 06 idle store API cannot see the game-default timeout.** `ListScenesIdlePastThreshold(ctx, nowNs)` with `COALESCE(idle_timeout_secs, <game_default_secs>)` — but `SceneStore` holds only a pool (`store.go:83-85`); config lives on `scenePlugin`/`SceneServiceConfig` (`main.go:46-73`, `publish_helpers.go:108-116`). With only `nowNs`, the store has no source for the default. Fix: pass `defaultIdleTimeoutSecs int` into the store method, or query candidate rows and apply the default in Go (scheduler layer).
2. **[MEDIUM] Global notify on/off is persisted but not enforced.** Plan 02/03/05 persist a per-character global notify preference; Plan 04's suppression at the `server.go:1287-1314` downgrade only checks per-scene mute via `ListMutedScenes`. Fix: suppression order at the downgrade = if global notify disabled → suppress; else if scene muted → suppress; else deliver. (Also add `GetSceneNotifyPref(characterID)` to the checker path.)
3. **[MEDIUM] Plan 04 checker loader underspecifies trusted dispatch identity.** The web facade builds a host-vouched dispatch with actor **and** `ownerPlayerID` (`internal/grpc/sceneaccess_service.go:150-155`, `internal/plugin/goplugin/host.go:1321-1324`); Plan 04's loader is only `func(ctx, characterID) ([]string, error)`. Fix: thread `playerID` (or build the checker where `BeginServiceDispatch` with `currentInfo.PlayerID` is reachable) so the core dials `ListMutedScenes` with the same trusted context.
4. **[MEDIUM] Plan 06 idle nudge cannot reach `[>GAME: …]` as written.** Focused clients render system events via `formatSystem` (`gateway_handler.go:1226-1234`); non-focused clients get generic `SCENE_ACTIVITY` which Plan 01 formats as "has new activity", not "is now idle". Fix: render `core-scenes:scene_idle_nudge` EventFrames through a `gamenotice.Idle(sceneID)` primitive, or extend `ControlFrame` with a notice-kind discriminator.
5. **[LOW] Stale proto contract comment.** `api/proto/holomush/scene/v1/scene.proto:22-29` says the plugin runs no ABAC except participant-gate reads, but an evaluator field already exists (`service.go:176-180,231-237`); adding mute ABAC should update the comment.

### Divergent Views
None — single reviewer.

### Recommended next step
Findings 1–4 are cross-plan contract gaps worth resolving before execution; 5 is a doc-comment cleanup. Fold them in with:

    /gsd-plan-phase 2 --reviews

The planner will incorporate each finding into the relevant PLAN.md (task/action/acceptance/must_haves) or record an explicit deferral, and the plan-checker will verify the incorporation.
