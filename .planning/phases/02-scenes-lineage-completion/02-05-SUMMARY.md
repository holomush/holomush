---
phase: 02-scenes-lineage-completion
plan: 05
subsystem: web-scene-mute-notify-typed-slice
tags: [scenes, mute, notify, proto, facade, bff, web, gateway-boundary, read-back]
requires:
  - "SceneService.MuteScene / SetSceneNotifyPref plugin RPCs + CharacterSceneInfo.muted + ListCharacterScenesResponse.global_notify_enabled read-back (Plan 03)"
  - "SceneAccessServer resolveAndGate/ownedCharacter/beginDispatch facade helpers"
  - "WebCreateScene 4-layer typed-slice precedent (proto → facade → BFF → client)"
provides:
  - "SceneAccessService.MuteScene / SetSceneNotifyPref facade RPCs (CharacterId stamped from verified char)"
  - "web.WebMuteScene / WebSetSceneNotifyPref BFF RPCs (typed structural writes, never the command path)"
  - "top-level global_notify_enabled on ListMyScenesResponse + WebListMyScenesResponse (read-back)"
  - "web notifyFlow.ts (toggleSceneMute / setGlobalNotify) + WorkspaceScene.muted + workspaceStore globalNotifyEnabled"
  - "SceneContextRail mute/unmute toggle initialized from persisted state"
affects:
  - api/proto/holomush/sceneaccess/v1/sceneaccess.proto
  - api/proto/holomush/web/v1/web.proto
  - internal/grpc/sceneaccess_service.go
  - internal/grpc/client.go
  - internal/web/scene_handlers.go
  - cmd/holomush/deps.go
  - web/src/lib/scenes/notifyFlow.ts
  - web/src/lib/scenes/client.ts
  - web/src/lib/scenes/types.ts
  - web/src/lib/scenes/workspaceStore.svelte.ts
  - web/src/lib/components/scenes/SceneContextRail.svelte
tech-stack:
  added: []
  patterns:
    - "structural GUI write → typed BFF RPC on the facade, never sendCommand/HandleCommand (gateway-boundary)"
    - "facade SERVER stamps CharacterId from the verified owned character on the forwarded plugin request so the plugin's mandatory character_id↔actor-metadata guard passes (mirrors ResumeScene)"
    - "monolithic proto regen couples the WebServiceHandler interface — facade + BFF impls land in one buildable commit (Plan 03 precedent)"
    - "notifyFlow mirrors membershipFlow (toggle on an existing resource), not createFlow"
    - "listMyScenes now returns { scenes, globalNotifyEnabled }; refresh() maps csi.muted → WorkspaceScene.muted + captures the global flag; optimistic setMuted/setGlobalNotifyEnabled with authoritative read-back on next refresh"
key-files:
  created:
    - web/src/lib/scenes/notifyFlow.ts
    - web/src/lib/scenes/notifyFlow.test.ts
  modified:
    - api/proto/holomush/sceneaccess/v1/sceneaccess.proto
    - api/proto/holomush/web/v1/web.proto
    - pkg/proto/holomush/sceneaccess/v1/sceneaccess.pb.go
    - pkg/proto/holomush/sceneaccess/v1/sceneaccess_grpc.pb.go
    - pkg/proto/holomush/sceneaccess/v1/sceneaccessv1connect/sceneaccess.connect.go
    - pkg/proto/holomush/web/v1/web.pb.go
    - pkg/proto/holomush/web/v1/web_grpc.pb.go
    - pkg/proto/holomush/web/v1/webv1connect/web.connect.go
    - web/src/lib/connect/holomush/sceneaccess/v1/sceneaccess_pb.ts
    - web/src/lib/connect/holomush/web/v1/web_pb.ts
    - internal/grpc/sceneaccess_service.go
    - internal/grpc/sceneaccess_service_test.go
    - internal/grpc/client.go
    - internal/grpc/client_test.go
    - internal/web/handler.go
    - internal/web/scene_handlers.go
    - internal/web/scene_handlers_test.go
    - cmd/holomush/deps.go
    - cmd/holomush/deps_test.go
    - web/src/lib/scenes/client.ts
    - web/src/lib/scenes/types.ts
    - web/src/lib/scenes/workspaceStore.svelte.ts
    - web/src/lib/scenes/workspaceStore.test.ts
    - web/src/lib/components/scenes/SceneContextRail.svelte
    - web/src/lib/components/scenes/SceneContextRail.svelte.test.ts
    - web/src/lib/components/scenes/SceneListItem.svelte.test.ts
    - web/src/lib/components/scenes/SceneComposer.svelte.test.ts
    - web/e2e/scenes.spec.ts
decisions:
  - "Tasks 1 and 2 landed in ONE commit: the monolithic proto regen also regenerates the webv1connect.WebServiceHandler interface, so *Handler MUST implement WebMuteScene/WebSetSceneNotifyPref in the same commit or internal/web (and every consumer) fails to build. Splitting facade (Task 1) from BFF (Task 2) would leave a non-building intermediate commit — the exact anti-pattern the plan-review learnings warn against. Same precedent as Plan 03's monolithic-regen note."
  - "SceneAccessServer.MuteScene/SetSceneNotifyPref stamp CharacterId from the verified char (mirror ResumeScene) — the load-bearing piece: without it the plugin's character_id↔actor-metadata guard (Plan 03) rejects the web write fail-closed (PermissionDenied)."
  - "listMyScenes return shape changed from CharacterSceneInfo[] to { scenes, globalNotifyEnabled } so the workspace store can read the persisted global flag; all existing workspaceStore.test.ts mocks updated to the object shape."
  - "notifyFlow updates local store state optimistically (setMuted/setGlobalNotifyEnabled) after the RPC; the persisted state is authoritative on the next refresh() — the e2e proves it survives a full reload."
  - "The mute toggle renders unconditionally in the Scene panel (any role) since a per-scene notification mute is meaningful for participants and observers alike; keyed off the persisted WorkspaceScene.muted via aria-pressed."
metrics:
  duration: ~55m
  completed: 2026-07-09
status: complete
---

# Phase 2 Plan 05: Web Mute/Notify Typed Slice Summary

Gave the web client mute/notify controls as a 4-layer typed slice
(proto → facade → BFF → client), mirroring the shipped WebCreateScene pattern
exactly. The web write forwards to the SAME plugin `SceneService.MuteScene` /
`SetSceneNotifyPref` RPCs the telnet command uses, so telnet and web share one
ABAC enforcement point (in the plugin). This is D-04's web surface — a
structural GUI write, so it uses a typed RPC, never the command path
(gateway-boundary). The persisted mute/global-notify state round-trips back to
the web workspace via the typed `ListMyScenes` read (round-3 Concern 1), so the
UI reflects it after reload — proven end-to-end by a Playwright reload test.

## What was built

- **Tasks 1 & 2 — proto RPCs + facade-SERVER handlers + BFF handlers + client
  stubs (one commit, `7e3665bcb`).** Coupled by the monolithic proto regen (see
  Deviations).
  - `sceneaccess.proto`: `MuteScene` / `SetSceneNotifyPref` facade RPCs +
    request/response messages (MuteSceneRequest mirrors ResumeSceneRequest +
    `muted`; SetSceneNotifyPrefRequest mirrors it minus `scene_id` plus
    `enabled`); top-level `global_notify_enabled` bool on `ListMyScenesResponse`
    (the per-scene `muted` rides inside the re-exported `CharacterSceneInfo`).
  - `web.proto`: `WebMuteScene` / `WebSetSceneNotifyPref` BFF RPCs + messages;
    mirroring `global_notify_enabled` on `WebListMyScenesResponse`.
  - `SceneAccessServer.MuteScene` / `SetSceneNotifyPref` (mirror `ResumeScene`
    exactly): `resolveAndGate` (guest gate) → `ownedCharacter` (ownership) →
    `beginDispatch` (host-vouched actor) → forward to the plugin with
    `CharacterId: char.ID.String()` stamped — the load-bearing set that makes the
    plugin's mandatory `character_id`↔actor-metadata guard pass. `ListMyScenes`
    forwards `GlobalNotifyEnabled`.
  - BFF `WebMuteScene` / `WebSetSceneNotifyPref` (mirror `WebCreateScene`: token
    via header, nil-check `h.sceneAccess`, `WithTimeout`, opaque
    `//nolint:wrapcheck` pass-through, `errutil.LogErrorContext`);
    `WebListMyScenes` forwards the read-back global flag.
  - `Client.MuteScene` / `SetSceneNotifyPref` stubs (`oops.Code("RPC_FAILED")`);
    `SceneAccessClient` (internal/web) + `GRPCClient` (cmd/holomush) interfaces
    extended; regenerated bindings for both packages committed (pb.go /
    grpc.pb.go / connect.go / *_pb.ts).
- **Task 3 — web notifyFlow + read-back snapshot wiring + rail toggle + tests
  (`c2098a3a5`).**
  - `notifyFlow.ts` (new): `toggleSceneMute` / `setGlobalNotify` call
    `WebMuteScene` / `WebSetSceneNotifyPref` via the connect-web client, then
    reflect state locally. Mirrors `membershipFlow.ts` (toggle on an existing
    resource), never builds a command string.
  - `client.ts`: `muteScene` / `setSceneNotifyPref` wrappers; `listMyScenes` now
    returns `{ scenes, globalNotifyEnabled }`.
  - `types.ts`: `WorkspaceScene.muted`; `workspaceStore.svelte.ts` `refresh()`
    maps `csi.muted` onto it and captures `globalNotifyEnabled` into runes state,
    plus `setMuted` / `setGlobalNotifyEnabled` mutators + `globalNotifyEnabled`
    getter.
  - `SceneContextRail`: mute/unmute toggle (`data-testid="scene-mute-toggle"`,
    `aria-pressed` reflecting persisted `WorkspaceScene.muted`).

## Verification

- `task lint:proto` — PASS (buf lint + format + name-echo gate) with the new RPCs,
  messages, and the two top-level `global_notify_enabled` fields.
- `task test -- ./internal/grpc/... ./internal/web/...` — PASS (982 tests). New:
  facade-server `MuteScene`/`SetSceneNotifyPref` tests asserting the forwarded
  plugin request carries `CharacterId == verifiedChar.ID.String()` and that a
  guest / unowned-character_id is rejected before any forward; a `ListMyScenes`
  read-back test (global_notify_enabled + muted row pass through); client-stub
  forward-success + `RPC_FAILED` error-wrap tests; BFF handler forward /
  denial-pass-through / nil-client-Unimplemented tests + a `WebListMyScenes`
  read-back forward test.
- `task test -- ./cmd/holomush/...` — PASS (540 tests; `GRPCClient` interface +
  mock extended).
- `task lint:go` — PASS (0 issues). `task fmt` clean.
- Generated stale-diff check clean:
  `git diff --exit-code pkg/proto/holomush/{sceneaccess,web} web/src/lib/connect/holomush/{sceneaccess,web}`
  returns clean post-regen.
- `pnpm run test:unit` (web) — PASS (456 tests). `notifyFlow.test.ts` (fast,
  sub-second, no browser): typed-request assertions + toggle round-trip + denial
  surfacing. `workspaceStore.test.ts` snapshot assertions: `refresh()` seeds
  `WorkspaceScene.muted=true` + `globalNotifyEnabled=false` from a muted read-back
  row (and defaults both when absent). `SceneContextRail.svelte.test.ts`: toggle
  renders persisted state and calls `toggleSceneMute` with negated `muted`.
- `pnpm run check` (svelte-check) — 0 errors (6 pre-existing unrelated warnings).
- `task test:e2e -- scenes.spec.ts -g "mute toggle persists"` — **PASS (1/1, 7.2s)**
  in the full Docker stack (core + gateway + web with the new RPCs compiled in):
  mutes a scene from the workspace rail via the typed RPC, RELOADS, and the toggle
  still reads `aria-pressed=true` — proving the persisted read-back, not just an
  in-session flag. Run locally (Docker available); CI re-runs the full suite.

## Deviations from Plan

### [Rule 3 — blocking build coupling] Tasks 1 and 2 merged into one commit

- **Plan:** Task 1 (proto + facade + client) and Task 2 (BFF handlers) as separate
  commits.
- **What was done:** Both landed in commit `7e3665bcb`.
- **Why:** `task proto` is monolithic and regenerates the
  `webv1connect.WebServiceHandler` interface, which now requires `WebMuteScene` /
  `WebSetSceneNotifyPref` on `*Handler`. Committing Task 1 (proto regen) without
  Task 2's BFF handler impls would leave `internal/web`, `cmd/holomush`, and their
  test builds broken at the intermediate commit — the exact "intermediate-commit
  broken build from out-of-order wiring" anti-pattern the plan-review learnings
  flag, and the same monolithic-regen reality Plan 03 documented. The two tasks'
  production + tests are otherwise implemented exactly as specified.

### [Rule 3 — blocking] Extended two extra interfaces the plan did not name

- The `WebServiceHandler` regen forced additions to the `SceneAccessClient`
  interface (`internal/web/handler.go`) AND the `GRPCClient` interface
  (`cmd/holomush/deps.go`), plus their test mocks — otherwise `*holoGRPC.Client`
  no longer satisfies them and the gateway wiring (`gateway.go`) fails to compile.
  Mechanical mirror of the existing per-RPC entries.

### [Rule 3 — blocking] Updated existing WorkspaceScene test builders + listMyScenes mocks

- Adding the required `WorkspaceScene.muted` field and changing `listMyScenes`'s
  return shape from `CharacterSceneInfo[]` to `{ scenes, globalNotifyEnabled }`
  broke existing test builders (`SceneContextRail` / `SceneListItem` /
  `SceneComposer` `makeScene` helpers) and `workspaceStore.test.ts` `listMyScenes`
  mocks. Updated all to the new shape (added `muted: false` to builders; wrapped
  mock returns in the object). In scope — direct consequence of this plan's typed
  changes.

No Rule 1 (bug) fixes, no Rule 4 architectural changes, no authentication gates.

## Known Stubs

None. The mute toggle renders and round-trips persisted state end-to-end; the
`SetSceneNotifyPref`/`setGlobalNotify` global-notify surface is fully wired at all
four layers (proto → facade → BFF → client + store `globalNotifyEnabled`), though
this plan wires no dedicated global-notify UI control beyond the store state the
rail can read — the per-scene mute toggle is the shipped UI control.

## Threat Flags

None. New surface is a pure BFF proxy → identity-resolving facade → the SAME
plugin ABAC gate the telnet command hits (Plan 03). The threat register
(T-02-14 command-path bypass, T-02-15 error leakage, T-02-16 forged
session/character) is mitigated exactly as planned: typed RPC only (no
sendCommand), opaque pass-through errors, and server-verified CharacterId stamped
from the owned character.

## Commits

- `7e3665bcb` — feat(02-05): web mute/notify typed RPCs — sceneaccess+web proto, facade + BFF handlers (Tasks 1 & 2)
- `c2098a3a5` — feat(02-05): web notifyFlow mute/notify client + rail toggle + read-back (Task 3)

## Self-Check: PASSED

- Files exist: `notifyFlow.ts`, `notifyFlow.test.ts`, `sceneaccess_service.go`,
  `scene_handlers.go`, `SceneContextRail.svelte`, both protos — all confirmed on disk.
- Commits `7e3665bcb`, `c2098a3a5` present in git history.
- Acceptance greps: `rpc MuteScene`/`rpc SetSceneNotifyPref` (2), `rpc WebMuteScene`/
  `rpc WebSetSceneNotifyPref` (2), facade-server handlers (2), BFF handlers (2),
  `Client.MuteScene` (1), `global_notify_enabled` on both responses (2/2),
  `GlobalNotifyEnabled` forward in facade + BFF (1/1), `sendCommand` in notifyFlow.ts (0).
- Generated-artifact stale-diff check clean.
