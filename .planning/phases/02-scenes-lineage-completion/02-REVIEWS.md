---
phase: 2
reviewers: [codex]
reviewers_attempted: [codex, antigravity]
review_round: 4
reviewed_at: 2026-07-09T16:08:06Z
plans_reviewed: [02-01-PLAN.md,02-02-PLAN.md 02-03-PLAN.md,02-04-PLAN.md 02-05-PLAN.md,02-06-PLAN.md 02-07-PLAN.md]
supersedes: round-3 review (incorporated at commit 9a017f7ee)
---

# Cross-AI Plan Review — Phase 2: Scenes Lineage Completion (ROUND 4)

> Reviewers requested: **Codex** + **Antigravity**. Codex ran source-grounded against the working tree at commit 9a017f7ee. **Antigravity FAILED** — its agentic Cascade hung on the large (216KB) inline `-p` prompt and did not converge; `--print-timeout 300s` did not fire, the process was killed after ~15 min, and no transcript/output was produced (known agy failure mode on large file-rich prompts — inline a prompt-file path instead of the full text next time). Skipped per the workflow's fail-and-continue rule. Round-4 focus: fresh adversarial pass + verify the 3 prior rounds held.

## Codex Review

**Prior-Rounds Regression Check**

- **RESOLVED: telnet notify is a rendering gap.** Core already downgrades non-focused scene events to content-free `SCENE_ACTIVITY` with `SceneId` at `internal/grpc/server.go:1287-1301`; web already forwards `SceneId` at `internal/web/handler.go:610-618`; telnet currently handles only `STREAM_CLOSED` and otherwise no-ops at `internal/telnet/gateway_handler.go:324-344`. Plan 01 targets the right seam.
- **RESOLVED: no new notification stream.** Plans keep the existing downgrade path and add only a telnet render/throttle.
- **RESOLVED: reconnect restore is wiring, not rebuild.** `RestoreConnectionFocus` exists and validates membership at `internal/grpc/focus/restore_connection_focus.go:35-62`; `Subscribe` adds connections without calling it at `internal/grpc/server.go:861-899`. Plan 07 wires the missing call.
- **RESOLVED: mute/unmute do not need `validActions`.** Core `validActions` remains generic only at `internal/command/types.go:115-118`; plugin evaluation rejects unowned resource types at `internal/plugin/pluginauthz/evaluate.go:195-201`, and `core-scenes` owns only `scene` via `plugins/core-scenes/plugin.yaml:7`.
- **RESOLVED: character-self notify prefs.** The plan keeps global notify prefs scoped by `character_id` actor-metadata guard, matching the existing `EndScene` guard at `plugins/core-scenes/service.go:696-704`, rather than inventing `character:<id>` plugin ABAC.
- **RESOLVED: idle nudge is declared, emitter-only.** `scene_idle_nudge` is already in rendering metadata at `plugins/core-scenes/plugin.yaml:113-116`, crypto emits at `plugins/core-scenes/plugin.yaml:177-180`, and registry at `plugins/core-scenes/main.go:177-187`. Plan 06 avoids re-declaration.
- **RESOLVED: typed read-back fix held.** Current `CharacterSceneInfo` lacks `muted`, and `ListCharacterScenesResponse` lacks `global_notify_enabled` at `api/proto/holomush/scene/v1/scene.proto:899-920`; sceneaccess/web re-export `CharacterSceneInfo` by reference at `api/proto/holomush/sceneaccess/v1/sceneaccess.proto:260-263` and `api/proto/holomush/web/v1/web.proto:1059-1062`. Plans 03/05 add the missing persisted read path and explicit global flag forwarding.

**Fresh Findings**

- **MEDIUM: Plan 03/05 miss generated artifacts and mock regeneration for new service RPCs.**  
  Plan 03 adds four RPCs to `SceneService` but its `files_modified` list omits the committed Go Connect stub `pkg/proto/holomush/scene/v1/scenev1connect/scene.connect.go` and the mockery-generated `internal/grpc/scenemocks/mock_SceneServiceClient.go` (`02-03-PLAN.md:7-15`, `02-03-PLAN.md:85-95`). The mock is generated from `SceneServiceClient` per `.mockery.yaml:94-99`, and tests pass it as the full `scenev1.SceneServiceClient` interface at `internal/grpc/sceneaccess_service_test.go:163-172`. After proto adds `MuteScene`, `SetSceneNotifyPref`, etc., the existing mock type at `internal/grpc/scenemocks/mock_SceneServiceClient.go:18-21` will no longer implement the interface unless regenerated. Same generated-stub omission applies to Plan 05 for `sceneaccessv1connect/sceneaccess.connect.go` and `webv1connect/web.connect.go`, while the plan lists only `*.pb.go`/`*_grpc.pb.go` at `02-05-PLAN.md:7-15`.  
  **Fix:** add the Go Connect generated files and `internal/grpc/scenemocks/mock_SceneServiceClient.go` to the plans/artifacts, and explicitly run `task proto && task web:generate && task mocks:generate`.

- **LOW/MEDIUM: Plan 04’s concrete mute cache needs an exported constructor/type for `cmd/holomush`.**  
  Plan 04 defines an unexported `sceneMuteCache` in package `internal/grpc` at `02-04-PLAN.md:73-75` and `02-04-PLAN.md:116-121`, but Task 3 constructs the concrete checker from `cmd/holomush/sub_grpc.go` at `02-04-PLAN.md:156-168`. `cmd/holomush` imports `internal/grpc` as `holoGRPC` (`cmd/holomush/sub_grpc.go:1-20`) and assembles options there (`cmd/holomush/sub_grpc.go:463-497`, `cmd/holomush/sub_grpc.go:571`). It cannot instantiate an unexported `sceneMuteCache` directly.  
  **Fix:** Plan 04 should require an exported constructor such as `holoGRPC.NewSceneMuteChecker(loader, ttl, now)` or an exported cache type; otherwise Task 3 has a package-boundary compile trap.

**Summary + Overall Risk**

Overall risk: **MEDIUM until the two execution traps are patched; LOW afterward.**

The architecture/security decisions are sound and the prior-round fixes held. I do not see a new ABAC/privacy correctness flaw. Recommendation: make the generated-artifact/mock and exported-constructor plan edits before execution, then proceed. No tests were run; this was a markdown-only source review.

---

## Consensus Summary

Round-4, single effective reviewer (Codex; Antigravity hung/failed). **All 3 prior rounds' fixes verified HELD** (clean regression check) and **no new correctness/security/ABAC/privacy flaw** was found. Codex surfaced 2 NEW **executor compile-traps** (build plumbing, not design). Overall reviewer risk: **MEDIUM until the two traps are patched, LOW afterward.**

### Verified sound (do not re-open)

Telnet rendering-gap seam; no new stream; reconnect-restore wiring; no `validActions` change; ABAC self-scope split (global pref character-self, mute scene-scoped); `character_id`-guarded RPCs; `EmitIntent` emit; facade-server `CharacterId` stamp; and the round-3 typed read-back (`CharacterSceneInfo.muted` + `global_notify_enabled`, self-scoped, fail-open) — all confirmed against source.

### New Concerns (executor compile-traps — mechanical, worth pinning)

1. **[MEDIUM] Generated Connect stubs + mockery mock not in `files_modified`.** Plan 03 adds 4 RPCs to `SceneService` but omits the committed Go Connect stub `pkg/proto/holomush/scene/v1/scenev1connect/scene.connect.go` and the mockery mock `internal/grpc/scenemocks/mock_SceneServiceClient.go` (generated per `.mockery.yaml:94-99`; tests pass it as the full `scenev1.SceneServiceClient` at `sceneaccess_service_test.go:163-172`). After the proto change the existing mock no longer implements the interface until regenerated → test compile failure + CI stale-diff failure on the uncommitted generated files. Same omission for Plan 05 (`sceneaccessv1connect`/`webv1connect`). **Fix:** add the Connect stubs + the mock to `files_modified`/artifacts and run `task proto && task web:generate && task mocks:generate` (commit the regen).
2. **[LOW/MED] Unexported `sceneMuteCache` can't be constructed cross-package.** Plan 04 defines an unexported `sceneMuteCache` in `internal/grpc` (`02-04-PLAN.md:73-75,116-121`), but Task 3 constructs the checker from `cmd/holomush/sub_grpc.go` (imports `internal/grpc` as `holoGRPC`). An unexported type can't be instantiated across the package boundary. **Fix:** require an exported constructor `holoGRPC.NewSceneMuteChecker(loader, ttl, now)` (or an exported cache type).

### Divergent Views

None — single effective reviewer (Antigravity unavailable).

### Recommended next step

Both findings are mechanical but real (the generated-artifacts omission in particular causes a CI stale-diff failure if the executor forgets to regenerate + commit). Cheap to pin. Fold in with:

```text
/gsd-plan-phase 2 --reviews
```

then execute. No design/security work remains — this is the last plumbing pass.
