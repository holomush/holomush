---
phase: 01-channels-subsystem
plan: 06
subsystem: channels-emit-audit
tags: [plugin, eventbus, audit, history, membership, tdd, chan-02, chan-03, d-04, d-07, d-08]

# Dependency graph
requires:
  - phase: 01-channels-subsystem
    provides: "channelStore + channel_log table + channel_memberships.joined_at (01-03)"
  - phase: 01-channels-subsystem
    provides: "channelService + eventSink-aware wiring seam + ChannelResolver membership store (01-05/01-04)"
  - phase: 01-channels-subsystem
    provides: "core-scenes publish_events.go / audit.go as the scene-identical substrate template (CHAN-03)"
provides:
  - "channelEventEmitter: content emit (CommunicationContent via comm.Say/Pose) + notice emit (join/leave/mute/ban/kick/rename) on events.<game>.channel.<id> with qualified wire types core-channels:<verb>, plaintext (Sensitive:false)"
  - "ChannelAuditServer (PluginAuditService): idempotent AuditEvent INSERT ... ON CONFLICT (id) DO NOTHING into plugin_core_channels.channel_log (no DEK columns, D-04); membership-gated QueryHistory (auth step-1, joined_at floor, scrollback cap); parseChannelSubject (wildcard rejection)"
  - "channelStore.MembershipForHistory (membership + most-recent joined_at floor, fail-closed, existence-oracle-safe)"
  - "EventSinkAware wiring on channelPlugin + channelService; ChannelAuditServer construction with the shared membership store (field injection)"
affects: [01-05b PostToChannel/moderation-notice/QueryChannelHistory RPCs (reuse emitter + membership-gated QueryHistory), 01-08 replay-on-join, 01-09 census]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Scene-identical emit substrate: comm builder → pluginsdk.EmitIntent{Subject: dot, Type: qualified, Sensitive:false} via EventSink (CHAN-03)"
    - "Per-plugin audit reachability WITHOUT a `provides` entry: RegisterPluginAuditServiceServer + audit: block + host PluginAuditClient host-walk (NOT the singleton ServiceRegistry)"
    - "Membership-gated history: auth step-1 (caller kind/id) BEFORE any DB work; joined_at floor as a not_before lower bound; scrollback-cap page clamp (D-07)"
    - "Plaintext channel_log mirrors scene_log MINUS dek_ref/dek_version (D-04); DekRef/DekVersion never read on the channel read path"

key-files:
  created:
    - "plugins/core-channels/publish_events.go"
    - "plugins/core-channels/publish_events_test.go"
    - "plugins/core-channels/audit.go"
    - "plugins/core-channels/audit_test.go"
    - "plugins/core-channels/audit_integration_test.go"
  modified:
    - "plugins/core-channels/service.go"
    - "plugins/core-channels/main.go"
    - "plugins/core-channels/store.go"
    - "plugins/core-channels/plugin.yaml"

key-decisions:
  - "holomush.plugin.v1.PluginAuditService is NOT declared in the channels manifest `provides` (the plan's instruction was corrected). The dependency DAG treats a `provides` name as a globally-unique provided service; core-scenes already provides PluginAuditService, so a second declaration hard-fails DUPLICATE_SERVICE_PROVIDER (dependency.go:112) and panics the whole-system census. Audit reachability is entirely per-plugin: RegisterServices calls RegisterPluginAuditServiceServer on the plugin's own gRPC transport, the `audit:` block declares subject ownership (events.*.channel.>), and the host resolves the client via PluginAuditClient(pluginName) host-walk — none of which needs the registry entry."
  - "Content emit is limited to channel_say + channel_pose (the two declared content verbs). comm.OOC is NOT emitted this plan — there is no channel_ooc verb in the manifest, and an unqualified/undeclared wire type would hard-fail EMIT_UNKNOWN_VERB at RenderingPublisher.Lookup. Notice verbs are join/leave/mute/ban/kick/rename (all declared)."
  - "channel_log.timestamp scans into time.Time directly (TIMESTAMPTZ) — channels is a fresh plugin schema with no pgnanos BIGINT history to match; the queryLog cursor/floor filters use time.Time bounds."
  - "MembershipForHistory returns (false, zero, nil) for BOTH a missing channel and a missing/banned membership row — the audit read boundary MUST NOT distinguish 'channel absent' from 'not a member' (T-01-12 existence-oracle mitigation). joined_at is single-valued per PK (channel_id, character_id); leave+rejoin writes a fresh row, so it always reflects the most-recent join."
  - "Membership gating applies to EVERY channel type incl. public (INV-CHANNEL-1): the 01-04 public-read permit is visibility/discoverability, not history content. QueryHistory never consults ABAC — it fences directly on the plugin-owned membership store."

requirements-completed: [CHAN-02, CHAN-03]

# Metrics
duration: 55min
completed: 2026-07-08
status: complete
---

# Phase 1 Plan 06: Channel Emit + Durable Membership-Gated History Summary

**Channel content (CommunicationContent) and notice events now flow through the shared EventBus on `events.<game>.channel.<id>` with plugin-qualified wire types, plaintext (D-04, no crypto.emits); those events audit idempotently to `plugin_core_channels.channel_log`, and `PluginAuditService.QueryHistory` serves durable history fenced on channel membership at auth step-1 (CHAN-02) with a `joined_at` floor and a scrollback cap (D-07) — scene-identical substrate (CHAN-03).**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-08
- **Tasks:** 2 (both TDD RED→GREEN)

## Accomplishments

- **`channelEventEmitter` (CHAN-03 emit half):** `dotStyleChannelSubject(gameID, chID) => events.<game>.channel.<id>`; `emitSay`/`emitPose` build `CommunicationContent` JSON via `comm.Say`/`comm.Pose` and emit through `pluginsdk.EventSink.Emit` with `Type: core-channels:channel_say|channel_pose` (INV-PLUGIN-40 qualified) and `Sensitive:false`; `emitJoin/Leave/Mute/Ban/Kick/Rename` emit small bespoke `channelNotice` payloads on the same subject. **No `channel_name` authz field in any payload** — identity is subject + live lookup (D-08). Reused by 01-05b's PostToChannel / moderation-notice RPCs (HIGH-4).
- **`ChannelAuditServer` (PluginAuditService):** `AuditEvent` validates the delivered `AuditRow` (row/codec/type/subject/id-16-byte/timestamp/schema-range) then INSERTs into `channel_log` with `ON CONFLICT (id) DO NOTHING` — a redelivery of the same bus dedup key is a no-op (T-01-17). Channel rows are plaintext (D-04): no `dek_ref`/`dek_version` columns, and the read path never touches them.
- **Membership-gated `QueryHistory` (CHAN-02 read half):** authorization is **step 1**, before any DB work — reject a nil/non-character/zero-id caller (`PermissionDenied`), `parseChannelSubject` (rejects wildcard/empty tokens → `InvalidArgument`), a nil membership lookup fails closed (`Internal`), then `MembershipForHistory` denies a non-member (`PermissionDenied`) without ever hitting the log store. A permitted read applies the member's `joined_at` as a `not_before` floor (history never crosses the most-recent join) and clamps the page size to the scrollback cap (500, D-07). Errors are generic gRPC status; inner detail is logged via `slog.*Context` (grpc-errors.md).
- **Wiring:** `channelService`/`channelPlugin` gain `EventSinkAware`; `main.go` builds the emitter once `eventSink` + `gameID` are set, and constructs `ChannelAuditServer` with `NewChannelAuditStore(store.Pool())` + the shared `channelStore` as the membership lookup (field injection, fail-closed) + the config scrollback cap. `RegisterServices` now also `RegisterPluginAuditServiceServer`.

## Task Commits

1. **Task 1 (RED):** failing emit tests + no-op emitter stub — `a9763a0b0` (test)
2. **Task 1 (GREEN):** channelEventEmitter content + notice emit; EventSinkAware wiring — `7fb6d6ddf` (feat)
3. **Task 2 (RED):** failing audit tests (auth gate, floor, cap, validation) + permissive stub — `0f234c1e3` (test)
4. **Task 2 (GREEN):** channel_log idempotent AuditEvent + membership-gated QueryHistory + MembershipForHistory + wiring; PluginAuditService-provides deviation fix — `53867a212` (feat)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 + Rule 3 — Blocking] Removed `holomush.plugin.v1.PluginAuditService` from the channels manifest `provides`**
- **Found during:** Task 2 whole-system census verification.
- **Issue:** The plan's artifact list and Task 2 action both instruct "add `PluginAuditService` to `provides`" (mirroring core-scenes). core-scenes **already** declares it. The dependency DAG resolver treats a `provides` name as a globally-unique provided service, so two declarations hard-fail with `DUPLICATE_SERVICE_PROVIDER` at `internal/plugin/dependency.go:112` → `Manager.LoadAll` errors → the `test/integration/wholesystem` census `BeforeAll` panics. core-channels could not load at all.
- **Fix:** Dropped the `PluginAuditService` line from `provides` (kept `ChannelService`) with an explanatory comment. The audit RPC needs no `provides` entry: `RegisterServices` registers `PluginAuditService` on the plugin's own gRPC transport, the `audit:` block declares subject ownership (`events.*.channel.>`), and the host resolves the client per-plugin via `Manager.PluginAuditClient(pluginName)` (host-walk over `pluginHosts`), never via the singleton `ServiceRegistry`. This is the same reason `AttributeResolverService` must not be in `provides`.
- **Files modified:** plugins/core-channels/plugin.yaml
- **Verification:** `task test:int -- ./test/integration/wholesystem/` green (census `LoadAll` succeeds with core-channels + core-scenes both loaded).
- **Committed in:** `53867a212` (Task 2 GREEN commit)

**Total deviations:** 1 auto-fixed (blocking). No scope change — the audit RPC still serves AuditEvent + QueryHistory exactly as specified; only the erroneous registry-singleton declaration was removed. All must_have truths still hold.

## Prohibitions Verified

- **A non-member MUST NOT read channel history (QueryHistory PermissionDenied before any DB pagination):** `TestQueryHistoryDeniesNonMemberWithoutHittingLogStore` (denied + `queryLogCalled == false`); integration `denies a non-member with PermissionDenied before any DB read`; nil-caller / non-character-kind / zero-id all denied at auth step-1. **Verified.**
- **Content payloads MUST NOT carry a channel_name authz field (D-08):** `assertNoChannelNameField` asserts absence across `emitSay`/`emitPose`/every notice; subject carries the channel id. **Verified.**
- **Channel events MUST NOT be declared in crypto.emits (plaintext, D-04):** manifest has no `crypto.emits` block; every emitted intent sets `Sensitive:false` (asserted in publish_events_test). `channel_log` has no DEK columns. **Verified.**

## TDD Gate Compliance

Plan `type: tdd`; both tasks `tdd="true"`.
- **Task 1:** RED `a9763a0b0` (`test(...)`) — full-assertion `publish_events_test.go` + no-op emitter stub; 9 assertions fail (`task test -- ./plugins/core-channels/` exit 1). GREEN `7fb6d6ddf` (`feat(...)`) — real emitter; all green.
- **Task 2:** RED `0f234c1e3` (`test(...)`) — full-assertion `audit_test.go` + permissive stub; 31 assertions fail. GREEN `53867a212` (`feat(...)`) — real audit server + store method + wiring; all green.

No `feat` shipped without its asserting tests. Gate sequence (test → feat) present for both features.

## Verification

- `task test -- ./plugins/core-channels/` — 122 unit tests, exit 0.
- `task test` (full unit suite) — 10023 tests, 3 skipped (quarantined), exit 0.
- `task test:int -- ./plugins/core-channels/` — 123 tests (unit + store + audit round-trip integration), exit 0.
- `task test:int -- ./test/integration/wholesystem/` — census `LoadAll` loads core-channels (emit/audit/provides) alongside core-scenes, exit 0 (after the provides deviation fix).
- `task lint:go` — 0 issues. `task lint:proto` clean. `task lint` overall exits non-zero only on 12 pre-existing MD041/MD075 markdown issues in `.planning/` GSD frontmatter artifacts (PLAN.md/STATE.md/deferred-items.md); none in this plan's files. Out of scope per the scope boundary (identical to 01-03/04/05).
- `task plugin:build-all` — exit 0 (all plugins compile linux+darwin).

## Emitted Event Types

| Wire type (qualified, on subject) | Kind | Payload | Sensitive |
|---|---|---|---|
| `core-channels:channel_say` | content | CommunicationContent (comm.Say) | false |
| `core-channels:channel_pose` | content | CommunicationContent (comm.Pose) | false |
| `core-channels:channel_join`/`_leave`/`_mute`/`_ban`/`_kick`/`_rename` | notice | bespoke channelNotice | false |

Channels declare **no** `crypto.emits`, so there is no bare registered-emit set to keep in sync (D-04 plaintext). Wire type + `verbs[].type` are the only qualification surface.

## History Membership Gating (CHAN-02 / D-07)

`QueryHistory` fences at auth step-1 on the plugin-owned membership store — applies to **every** channel type including public (the 01-04 public-read permit is visibility, not history content; INV-CHANNEL-1). Order: caller kind/id → `parseChannelSubject` → nil-lookup fail-closed → `MembershipForHistory`. A member's most-recent `joined_at` becomes the `not_before` floor so scrollback never crosses their join; the page size clamps to the config scrollback cap (500). A non-member is denied before any log-store query (no timing/existence oracle).

## User Setup Required

None.

## Next Phase Readiness

- **01-05b** fills the ChannelService content/moderation RPCs (PostToChannel, moderation notices, QueryChannelHistory) — it reuses `channelService.emitter` (built here) and the membership-gated `QueryHistory` substrate. The emitter + eventSink are wired; the emit-consuming RPCs are the only remaining piece.
- **01-08** replay-on-join reads recent `channel_log` (bounded by `replay_count`) — the audit read path + `joined_at` floor are in place.
- **01-09 census** already asserts core-channels loads with emit + audit; add explicit emit/audit assertions there if desired.

## Self-Check: PASSED

- Created files present: `publish_events.go`, `publish_events_test.go`, `audit.go`, `audit_test.go`, `audit_integration_test.go`.
- All four task commits verified in `git log` (`a9763a0b0`, `7fb6d6ddf`, `0f234c1e3`, `53867a212`).
- `plugin.yaml` no longer declares `PluginAuditService` in `provides` (grep count 4 = comment references only, not a provides entry).
- Channels unit + integration + whole-system census green after the deviation fix.

---
*Phase: 01-channels-subsystem*
*Completed: 2026-07-08*
