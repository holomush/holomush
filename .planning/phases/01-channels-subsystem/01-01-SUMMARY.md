---
phase: 01-channels-subsystem
plan: 01
subsystem: api
tags: [protobuf, grpc, connectrpc, buf, channels, abac]

# Dependency graph
requires:
  - phase: none
    provides: substrate-parity reference (holomush.scene.v1.SceneService)
provides:
  - "holomush.channel.v1.ChannelService proto contract"
  - "Generated Go bindings (channel.pb.go, channel_grpc.pb.go, channelv1connect/)"
  - "Generated ConnectRPC TS web bindings (channel_pb.ts)"
affects: [01-05, 01-05b, 01-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Interface-first: typed proto contract lands before the service implementation"
    - "Channel identity by ID + live name lookup (no channel_name payload field, D-08)"
    - "Plaintext channel events (no crypto.emits, no sensitive fields, D-04)"

key-files:
  created:
    - "api/proto/holomush/channel/v1/channel.proto"
    - "pkg/proto/holomush/channel/v1/channel.pb.go"
    - "pkg/proto/holomush/channel/v1/channel_grpc.pb.go"
    - "pkg/proto/holomush/channel/v1/channelv1connect/channel.connect.go"
    - "web/src/lib/connect/holomush/channel/v1/channel_pb.ts"
  modified:
    - "pkg/proto/holomush/comm/v1/comm.pb.go"
    - "site/src/content/docs/reference/grpc-api.md"

key-decisions:
  - "Modeled request/response shapes mirroring SceneService: character_id-scoped calls, empty responses for mutations that carry no body"
  - "PostToChannel/Join/Leave carry session_id where live-stream (un)subscribe is needed; no channel_name on any content message (D-08)"
  - "Committed pre-existing stale-generated drift (comm.pb.go, grpc-api.md) surfaced by full regeneration, per the stale-diff CI gate"

patterns-established:
  - "ChannelService is the second substrate consumer contract; its shape parallels SceneService for the eventkit/groupkit N=2 extraction (CHAN-05)"

requirements-completed: [CHAN-01, CHAN-02]

coverage:
  - id: D1
    description: "holomush.channel.v1.ChannelService proto defines create/join/leave/list/post/who/history + invite/mute/ban/kick/transfer RPCs with Go-grounded doc comments"
    requirement: "CHAN-01"
    verification:
      - kind: unit
        ref: "test/meta/proto_doc_comments_test.go#TestProtoCommentsNoNameEcho (via task lint:proto)"
        status: pass
      - kind: other
        ref: "task lint:proto (buf lint + buf format --exit-code + name-echo gate)"
        status: pass
    human_judgment: false
  - id: D2
    description: "Generated Go + ConnectRPC + web bindings committed in the same change as the .proto; grpc-api.md documents the service"
    requirement: "CHAN-02"
    verification:
      - kind: unit
        ref: "test/meta/grpc_api_coverage_test.go#TestGRPCReferenceCoversAllServices"
        status: pass
      - kind: other
        ref: "task build (Go + web compile against generated bindings)"
        status: pass
    human_judgment: false

# Metrics
duration: 11min
completed: 2026-07-08
status: complete
---

# Phase 01 Plan 01: ChannelService Proto Contract Summary

**Typed holomush.channel.v1.ChannelService gRPC contract (create/join/leave/list, post/who/history, invite/mute/ban/kick/transfer) mirroring SceneService, with committed Go/ConnectRPC/web bindings and a green proto lint gate.**

## Performance

- **Duration:** ~11 min
- **Started:** 2026-07-08T21:11Z
- **Completed:** 2026-07-08T21:22Z
- **Tasks:** 2
- **Files created:** 5 (proto + 4 generated); **modified:** 2 (regenerated drift)

## Accomplishments

- Defined `ChannelService` with 12 RPCs and their request/response messages plus `ChannelInfo`, `MemberInfo`, `ChannelHistoryEntry`, structurally mirroring `holomush.scene.v1.SceneService`.
- Every message, field, RPC, and service element carries a substantive Go-grounded doc comment describing the intended handler behavior; the name-echo gate passes.
- Channel identity flows by ID + live name lookup — no `channel_name` field on any content/post message (D-08); no crypto/sensitive fields (D-04, plaintext).
- Regenerated and committed Go (`channel.pb.go`, `channel_grpc.pb.go`, `channelv1connect/`) and web TS (`channel_pb.ts`) bindings in the same change; `task lint:proto` green, `task build` and full `task test` (9858 tests) pass.

## Task Commits

1. **Task 1: Define ChannelService proto** — `4c4e3ab23` (feat)
2. **Task 2: Regenerate + commit bindings** — `99a651c98` (feat)
   - **Auto-fix (Rule 3):** `29bff8ea9` (chore) — stale `comm.pb.go` header sync
   - **Auto-fix (Rule 3):** `69560c9b2` (docs) — regenerated `grpc-api.md` for `TestGRPCReferenceCoversAllServices`

## Files Created/Modified

- `api/proto/holomush/channel/v1/channel.proto` — new ChannelService contract + messages
- `pkg/proto/holomush/channel/v1/channel.pb.go`, `channel_grpc.pb.go`, `channelv1connect/channel.connect.go` — generated Go/gRPC/ConnectRPC bindings
- `web/src/lib/connect/holomush/channel/v1/channel_pb.ts` — generated web TS binding
- `pkg/proto/holomush/comm/v1/comm.pb.go` — pre-existing stale-diff synced by full regen (SPDX header)
- `site/src/content/docs/reference/grpc-api.md` — regenerated to include ChannelService (+ pre-existing drift sync)

## Decisions Made

- Web bindings land under `web/src/lib/connect/` (the actual `web/buf.gen.yaml` output dir), not the `web/src/lib/proto/` path named in the plan frontmatter (see Deviations).
- PostToChannel takes `kind` (say/pose/ooc) + `text`; Join/Leave carry `session_id` for host live-stream (un)subscription. Mutation RPCs return empty bodies, matching the SceneService convention.
- Committed regenerated `comm.pb.go` and `grpc-api.md` rather than reverting them: both are legitimate generated outputs and required for a clean tree + passing stale-diff/API-coverage gates.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Stale comm.pb.go surfaced by full regeneration**

- **Found during:** Task 2 (regenerate bindings)
- **Issue:** `buf generate` regenerates all files; the committed `comm.pb.go` predated the SPDX header protoc-gen-go now copies from `comm.proto`'s leading comment, leaving an uncommitted diff (blocks clean tree + stale-diff CI gate).
- **Fix:** Committed the regenerated `comm.pb.go`.
- **Verification:** `git status` clean for generated output; `task lint:proto` green.
- **Committed in:** `29bff8ea9`

**2. [Rule 3 - Blocking] grpc-api.md missing ChannelService section**

- **Found during:** Task 2 (`task test`)
- **Issue:** `TestGRPCReferenceCoversAllServices` failed — the new service was absent from `grpc-api.md`.
- **Fix:** Ran `task docs:proto` (canonical regenerator the test mandates); it added the ChannelService section and synced pre-existing reference drift.
- **Verification:** `TestGRPCReferenceCoversAllServices` passes; full `task test` green.
- **Committed in:** `69560c9b2`

### Non-blocking observations

- **Plan frontmatter path drift:** plan listed `web/src/lib/proto/` for web bindings; the repo's `web/buf.gen.yaml` emits to `web/src/lib/connect/`. Followed the real output path.

---

**Total deviations:** 2 auto-fixed (both Rule 3 blocking), 1 path-drift note.
**Impact on plan:** Both auto-fixes are canonical generated-output syncs required by CI gates. No scope creep — no hand-written Go/TS.

## Issues Encountered

- `task lint` (full) fails only on `task lint:markdown`: 164 pre-existing rumdl issues across 12 `.planning/phases/01-channels-subsystem/*.md` GSD planning artifacts (01-02..01-09 PLAN, CONTEXT, RESEARCH). Not caused by this plan; logged to `deferred-items.md`. `lint:go`, `lint:proto`, `build`, and `task test` are all green.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- 01-05 (service.go create/join/leave/list) and 01-05b (post/who/history + moderation) now have a concrete generated `ChannelService` type to implement against, before the 01-07 command layer delegates to any method (HIGH-4 satisfied).
- No blockers.

---

*Phase: 01-channels-subsystem*
*Completed: 2026-07-08*

## Self-Check: PASSED

All created files present; all task commits (4c4e3ab23, 99a651c98, 29bff8ea9, 69560c9b2) exist in history.
