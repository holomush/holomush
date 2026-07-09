---
phase: 2
reviewers: [codex]
reviewers_attempted: [codex, gemini]
review_round: 3
reviewed_at: 2026-07-09T15:25:43Z
plans_reviewed: [02-01-PLAN.md,02-02-PLAN.md 02-03-PLAN.md,02-04-PLAN.md 02-05-PLAN.md,02-06-PLAN.md 02-07-PLAN.md]
supersedes: round-2 review (incorporated at commit 5bb539b22)
---

# Cross-AI Plan Review — Phase 2: Scenes Lineage Completion (ROUND 3)

> Reviewers requested: **Codex** + **Gemini**. Codex ran source-grounded against the working tree at commit 5bb539b22. **Gemini FAILED** — the `gemini` CLI account is not eligible for Gemini Code Assist (`IneligibleTierError: DASHER_USER`, a Workspace account without free-tier access); not a transient error, so it was skipped per the workflow's fail-and-continue rule. Round-3 focus: verify the round-2 ABAC self-scope fix is sound + a fresh adversarial pass.

## Codex Review

## 1. Round-2 Regression Check

- **ABAC split: RESOLVED.** Plan 03 keeps `MuteScene` scene-scoped with `Evaluate("mute", "scene:"+scene_id)` and keeps `SetSceneNotifyPref` / `GetSceneNotifyPref` / `ListMutedScenes` character-self-scoped with no `scene_id` and no `scene:<id>` evaluation. See `.planning/phases/02-scenes-lineage-completion/02-03-PLAN.md:83`, `:86`, `:113-118`, `:128`.

- **`character_id` on all 4 plugin RPCs: RESOLVED.** Plan 03 explicitly requires `character_id` on `MuteSceneRequest`, `SetSceneNotifyPrefRequest`, `GetSceneNotifyPrefRequest`, and `ListMutedScenesRequest`, with actor-metadata mismatch denied before store writes. See `.planning/phases/02-scenes-lineage-completion/02-03-PLAN.md:78-86`, `:118-129`. Existing precedent is real: `EndScene` rejects mismatched actor metadata at `plugins/core-scenes/service.go:696-704`.

- **Idle emit via `pluginsdk.EmitIntent`: RESOLVED.** Plan 06 says `scene_idle_nudge` emits via `sink.Emit(ctx, pluginsdk.EmitIntent{Type: "core-scenes:scene_idle_nudge", ...})`, not `core.NewEvent()`. See `.planning/phases/02-scenes-lineage-completion/02-06-PLAN.md:80-85`, `:150-159`.

- **Facade-server handlers for web writes: RESOLVED.** Plan 05 adds `SceneAccessServer.MuteScene` and `SetSceneNotifyPref`, resolves ownership, calls `beginDispatch`, and forwards `CharacterId: char.ID.String()` so the plugin guard passes. See `.planning/phases/02-scenes-lineage-completion/02-05-PLAN.md:83-88`, `:118-130`. Existing facade pattern verifies ownership at `internal/grpc/sceneaccess_service.go:106-125` and dispatches host-vouched actor+owner at `internal/grpc/sceneaccess_service.go:150-155`.

## 2. ABAC Self-Scope Verdict

**Correct and sufficient, with one wording caveat:** the global notify-pref authorization is sufficient only as **host-vouched actor binding**, not as raw request metadata.

The current ABAC/plugin shape supports this:

- `core-scenes` owns only `resource_types: [scene]` (`plugins/core-scenes/plugin.yaml:4-8`).
- Plugin-side `Evaluate` rejects resources outside owned types (`internal/plugin/pluginauthz/evaluate.go:195-201`), so a plugin-local Layer-2 `Evaluate("write", "character:"+id)` would not fit without broadening plugin authority.
- The host evaluator derives the ABAC subject server-side from the dispatch token, not plugin-supplied identity (`internal/plugin/hostcap/servers.go:503-523`).
- `BeginServiceDispatch` requires a server-vouched actor and owner player, stores them in the token, and attaches advisory actor metadata (`internal/plugin/goplugin/host.go:1301-1324`, `:1346-1359`).
- The facade verifies the character is owned by the authenticated player before dispatch (`internal/grpc/sceneaccess_service.go:106-125`).

So for a per-character global preference with no scene resource, `req.character_id == host-vouched actor.character_id` is the right self-scope. Adding `scene:<id>` would be wrong; adding `character:<id>` from inside `core-scenes` would fight plugin resource ownership.

## 3. New Concerns

**MEDIUM — Web prefs UI has writes but no typed read/snapshot path for persisted mute/global-pref state.**  
Plan 05 adds `WebMuteScene` and `WebSetSceneNotifyPref` only (`.planning/phases/02-scenes-lineage-completion/02-05-PLAN.md:79-97`, `:122-130`). Existing `WebListMyScenes` returns only `CharacterSceneInfo` (`api/proto/holomush/web/v1/web.proto:1059-1062`), and `CharacterSceneInfo` contains scene, role, last activity, and entry count only (`api/proto/holomush/scene/v1/scene.proto:899-913`). The workspace store seeds UI state from `listMyScenes` (`web/src/lib/scenes/workspaceStore.svelte.ts:66-90`, `:101-117`) and `WorkspaceScene` has no muted/global-notify fields (`web/src/lib/scenes/types.ts:11-40`). Failure mode: the UI can toggle locally, but after reload/reconnect it has no typed source of truth to show which scenes are muted or whether global notifications are off. Add a typed read path or extend `ListMyScenes`/workspace snapshot with mute/global-pref fields.

**LOW — `scene_idle_nudge` manifest description may drift from the implemented semantics.**  
Plan 06 changes the rendered nudge to “Scene #id is now idle” and emits payload with `scene_id` (`.planning/phases/02-scenes-lineage-completion/02-06-PLAN.md:22-23`, `:150`), but the existing manifest describes `scene_idle_nudge` as “next-up character has been idle ... name + duration” (`plugins/core-scenes/plugin.yaml:177-180`). If the implementation follows the plan, update that existing description without re-declaring the event.

## 4. Summary + Overall Risk

**Overall risk: MEDIUM.** The security-critical ABAC split is sound, and the round-2 fixes stayed incorporated. I recommend **one more small fix pass** before execution: add a web read/snapshot path for persisted mute/global notify state, and align the idle-nudge manifest description with the planned event semantics.

---

## Consensus Summary

Round-3, single effective reviewer (Codex; Gemini ineligible). **All 4 round-2 fixes verified RESOLVED and the security-critical ABAC self-scope shape is CONFIRMED correct & sufficient** via a full authorization-chain trace. Two NEW concerns surfaced (neither a regression). Overall reviewer risk: **MEDIUM — one completeness gap (web read-back) + one doc-string drift; the loop has converged (no correctness/security issues remain).**

### Verified sound (do not re-open)
- **ABAC self-scope (round-2 HIGH):** `req.character_id == host-vouched actor.character_id` is the correct self-scope for the global notify pref. Adding `scene:<id>` would be wrong; adding `character:<id>` inside `core-scenes` would fight its `resource_types:[scene]` ownership (`plugin.yaml:4-8`, `pluginauthz/evaluate.go:195-201`, `hostcap/servers.go:503-523`, `host.go:1301-1324`). The binding guard is sufficient — no extra Layer-2 policy needed for a character mutating its own preference.
- character_id on all 4 plugin RPCs (guarded, `service.go:696-704`); idle emit via `pluginsdk.EmitIntent`; Plan 05 facade-server handlers stamp `CharacterId` (`sceneaccess_service.go:106-125`, `:150-155`) — all RESOLVED.

### New Concerns
1. **[MEDIUM] Web mute/prefs UI can write but has no typed READ/snapshot path.** Plan 05 adds only `WebMuteScene`/`WebSetSceneNotifyPref` (writes). `WebListMyScenes`→`CharacterSceneInfo` carries scene/role/last-activity/entry-count only (`web.proto:1059-1062`, `scene.proto:899-913`); the workspace store seeds from it (`web/src/lib/scenes/workspaceStore.svelte.ts:66-90`) and `WorkspaceScene` has no muted/global-notify fields (`web/src/lib/scenes/types.ts:11-40`). Failure mode: the UI can toggle locally but after reload/reconnect has no source of truth for which scenes are muted / whether global notifications are off. Fix: extend `ListMyScenes`/`CharacterSceneInfo` (or add a typed read RPC) with `muted` + `global_notify` fields and wire the workspace snapshot.
2. **[LOW] `scene_idle_nudge` manifest description drift.** Plan 06 renders "Scene #id is now idle" with a `scene_id` payload, but the manifest still describes the event as "next-up character has been idle … name + duration" (`plugins/core-scenes/plugin.yaml:177-180`). Fix: update the description (do NOT re-declare the event).

### Divergent Views
None — single effective reviewer (Gemini unavailable).

### Recommended next step
Concern 1 is a scope call: a mute/prefs UI that can't reflect persisted state on reload is functionally incomplete (D-04), but adding the read path grows the phase (proto fields on `CharacterSceneInfo` + workspace-store wiring). Either fold both in with `/gsd-plan-phase 2 --reviews`, or accept Concern 1 as a fast-follow bead and incorporate only the LOW. No correctness/security blocker remains — execution is viable once the scope of Concern 1 is decided.
