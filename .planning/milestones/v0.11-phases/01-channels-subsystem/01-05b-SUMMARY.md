---
phase: 01-channels-subsystem
plan: 05b
subsystem: channels-service
tags: [plugin, grpc, abac, channels, self-enforcement, moderation, membership, tdd, chan-02, chan-04, high-4]

# Dependency graph
requires:
  - phase: 01-channels-subsystem
    provides: "channelService (Create/Join/Leave/List) + per-RPC ABAC self-enforcement seam + eval wiring (01-05)"
  - phase: 01-channels-subsystem
    provides: "channelEventEmitter (content + notice emit) + membership-gated audit QueryHistory + MembershipForHistory (01-06)"
  - phase: 01-channels-subsystem
    provides: "core-scenes SceneServiceImpl structural verbs as the per-verb self-enforcement template"
provides:
  - "The COMPLETE holomush.channel.v1.ChannelService surface: the 8 RPCs 01-05 left Unimplemented — PostToChannel, WhoInChannel, QueryChannelHistory, InviteToChannel, MuteMember, BanMember, KickMember, TransferOwnership — on the SAME channelService type (HIGH-4 closed)"
  - "Per-verb self-enforced ABAC: moderation (invite/mute/ban/kick/transfer) is owner+admin only (D-05, no op); PostToChannel is emit(membership)-gated + not-muted; WhoInChannel is store-membership-gated; a denied/hidden channel returns uniform NotFound (T-01-12)"
  - "QueryChannelHistory reuses the SINGLE 01-06 membership fence via ChannelAuditServer.HistoryForMember (extracted authorizeMember) — no second unfenced read path"
  - "channelStore.KickMember / TransferOwnership / ListMembers / IsMuted (moderation + roster substrate); plugin.yaml invite added to the owner + admin-override moderation policies"
affects: [01-07 command layer (delegates who/history/invite/mute/ban/kick/transfer/post to these RPCs), 01-08 moderation→leave live removal, 01-09 census]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-verb ABAC self-enforcement (gateAction) BEFORE store mutation, independent of the command wrapper; denied ⇒ uniform codes.NotFound (T-01-12 — authority-deny and hidden/absent are indistinguishable)"
    - "Single membership fence for history: authorizeMember extracted on ChannelAuditServer; both the streaming QueryHistory (01-06) and the service QueryChannelHistory (HistoryForMember) call it — authorization lives in one place"
    - "Content emit reuses the 01-06 comm.Say/comm.Pose + EventSink path; no payload channel_name (D-08); ooc rejected (no channel_ooc verb, D-04)"
    - "Best-effort notice emit after a committed store mutation (non-fatal, mirrors core-scenes kick notice)"
    - "invite is owner+admin authority (added to moderate-own-channel + admin-override-channel), realised store-side as adding the target member via JoinChannel (the read-gate consumes membership)"

key-files:
  created:
    - "plugins/core-channels/service_rpcs.go"
  modified:
    - "plugins/core-channels/service.go"
    - "plugins/core-channels/store.go"
    - "plugins/core-channels/audit.go"
    - "plugins/core-channels/main.go"
    - "plugins/core-channels/plugin.yaml"
    - "plugins/core-channels/service_test.go"

key-decisions:
  - "The 8 new RPC methods live in a NEW file service_rpcs.go (not appended to service.go). This gave a clean TDD RED: with service_rpcs.go absent the embedded UnimplementedChannelServiceServer answers all 8, so the RED commit's tests fail with codes.Unimplemented; the GREEN commit adds the file. It also keeps service.go (create/join/leave/list + limiter + mapStoreError) readable."
  - "Moderation/structural denial ⇒ uniform codes.NotFound, NOT PermissionDenied. Admin overrides moderate channels they are not members of, so a read (visibility) gate cannot precede the moderation gate (admin isn't a member of a private channel). Running only the moderation ABAC gate means a non-owner member and a non-member (hidden) both produce ABAC deny; returning the SAME NotFound for both is the only existence-oracle-safe choice (T-01-12). PostToChannel emit-deny is likewise NotFound; a MUTED member gets PermissionDenied (they are a member — no oracle)."
  - "QueryChannelHistory reuses 01-06's fence: authorizeMember was extracted from the streaming QueryHistory; HistoryForMember calls it + the shared clampHistoryPageSize + queryLog with the joined_at floor. The fence returns PermissionDenied for non-member/absent (uniform); the SERVICE RPC translates that single code to NotFound at its boundary so every new per-channel RPC presents the same uniform not-found (auth is NOT re-implemented — only the presentation code is normalised)."
  - "InviteToChannel = admit the TARGET as a member (store.JoinChannel(channelID, targetID)); there is no separate invitations table. A private channel's read gate denies a non-member, so being invited means being made a member (mirrors core-scenes InviteParticipant). Emits a channel_join notice."
  - "WhoInChannel is membership-gated on the plugin-owned store (MembershipForHistory), not the ABAC read gate — the read gate admits any character to a public channel (visibility), but the roster requires membership. This mirrors 01-06's history model (membership authz in the store, not ABAC) and is existence-oracle-safe."
  - "New store methods KickMember (membership.kick ops event; owner-cannot-be-kicked→FailedPrecondition), TransferOwnership (reads current owner from the channel row so an admin who is not the owner can still transfer; target must be a non-banned member→FailedPrecondition), ListMembers (non-banned roster with role/muted/joined_at), IsMuted. store.go was NOT in the plan's files_modified list — added as Rule 2 (missing critical functionality the RPCs cannot work without)."
  - "PostToChannel ooc kind is rejected with InvalidArgument: the proto validates kind in {\"\",say,pose,ooc} but there is no channel_ooc verb (01-06 D-04 declares only say+pose content verbs); emitting an unqualified/undeclared wire type would hard-fail EMIT_UNKNOWN_VERB."

requirements-completed: [CHAN-02, CHAN-04]

# Metrics
duration: 70min
completed: 2026-07-08
status: complete
---

# Phase 1 Plan 05b: Complete ChannelService RPC Surface Summary

**The eight remaining `holomush.channel.v1.ChannelService` RPCs (PostToChannel, WhoInChannel, QueryChannelHistory, InviteToChannel, MuteMember, BanMember, KickMember, TransferOwnership) are implemented on the same `channelService` type 01-05 started — each self-enforcing ABAC per verb before touching state: moderation is owner+admin only (D-05), PostToChannel is emit(membership)-gated + not-muted, the read verbs are membership-gated, a denied/hidden channel returns a uniform NotFound (T-01-12), PostToChannel emits through the 01-06 content path (no channel_name, D-08), and QueryChannelHistory reuses the SINGLE 01-06 membership fence (HistoryForMember) — closing review finding HIGH-4 so the 01-07 command layer has a complete service to delegate to.**

## Performance

- **Duration:** ~70 min
- **Completed:** 2026-07-08
- **Tasks:** 2 (Task 1 moderation RPCs; Task 2 content/read RPCs) — delivered as one plan-level TDD RED→GREEN cycle (`type: tdd`)

## Accomplishments

- **Moderation RPCs (Task 1, owner+admin only, D-05):** `InviteToChannel`, `MuteMember`, `BanMember`, `KickMember`, `TransferOwnership`. Each: actor-identity cross-check → self-enforced ABAC gate via `gateAction` (invite/mute/ban/kick/transfer on `channel:<id>`) → store mutation (`JoinChannel` for invite / `SetMuted` / `SetBanned` / `KickMember` / `TransferOwnership`, each appending a `channel_ops_events` row) → best-effort notice emit via the 01-06 emitter (`channel_join`/`_mute`/`_ban`/`_kick`/`_rename`). A denied caller (non-owner non-admin, or a channel they cannot see) receives a **uniform `codes.NotFound`** (T-01-12).
- **Content RPC (Task 2):** `PostToChannel` — self-enforces the Layer-2 `emit` (membership) gate (non-member/hidden → uniform NotFound), rejects a **muted** member (`PermissionDenied`), then emits `channel_say`/`channel_pose` through the 01-06 `comm.Say`/`comm.Pose` path, `Sensitive:false`, **no `channel_name`** (D-08). `ooc` is rejected (`InvalidArgument`) — no `channel_ooc` verb is declared.
- **Read RPCs (Task 2):** `WhoInChannel` is membership-gated on the plugin store (`MembershipForHistory`) → uniform NotFound for a non-member → roster from the new `ListMembers` (owner-first, non-banned, with role/muted/joined_at). `QueryChannelHistory` delegates to `ChannelAuditServer.HistoryForMember`, which reuses the **extracted `authorizeMember` fence** + `clampHistoryPageSize` + `queryLog` (joined_at floor, D-07) shared with the streaming `QueryHistory`; the fence's `PermissionDenied` is presented as a uniform NotFound at the service boundary.
- **Store substrate (store.go):** `KickMember` (membership.kick ops event; classifyKickMiss → owner-cannot-kick=FailedPrecondition / absent=NotFound), `TransferOwnership` (reads the current owner from the channel row so an admin non-owner can transfer; promotes a non-banned member, demotes the old owner, updates `owner_id`, records membership.transfer), `ListMembers`, `IsMuted`.
- **Audit refactor (audit.go):** extracted `authorizeMember` (the single membership fence) + `clampHistoryPageSize` from the streaming `QueryHistory`; added `HistoryForMember` so the service history read shares the exact same authorization.
- **Wiring + policy:** `main.go` sets `p.service.history = p.auditSrv`; `plugin.yaml` adds `invite` to `moderate-own-channel` (owner) and `admin-override-channel` (admin).

## Task Commits

1. **RED (Tasks 1+2):** failing service tests + store/audit substrate + wiring + policy; service methods fall back to embedded Unimplemented — `7a93c60b8` (test)
2. **GREEN (Tasks 1+2):** `service_rpcs.go` with all 8 RPCs + helpers — `6b1ab49f5` (feat)
3. **Lint:** errcheck / nolintlint / unparam fixes — `aaff154bc` (style)

## Deviations from Plan

### Auto-fixed / scope additions

**1. [Rule 2 - Missing critical functionality] New store methods (store.go was not in `files_modified`)**

- **Found during:** Task 1/2 implementation.
- **Issue:** The moderation and roster RPCs require store operations that did not exist: kicking a non-owner member with a `membership.kick` ops event, transferring ownership (promote member / demote owner / update `owner_id`), the full roster (role+muted+joined_at), and a muted-flag lookup. The plan's `files_modified` listed only service.go/plugin.yaml/main.go/service_test.go.
- **Fix:** Added `KickMember`, `TransferOwnership`, `ListMembers`, `IsMuted` to `store.go`, reusing the existing tx + ops-event + classify-miss patterns. The RPCs cannot function without them.
- **Files modified:** plugins/core-channels/store.go

**2. [Design decision] `service_rpcs.go` as a separate file**

- The 8 RPC methods live in a new `service_rpcs.go` rather than appended to `service.go`. This produced a clean TDD RED (embedded `UnimplementedChannelServiceServer` answers the 8 methods when the file is absent) and keeps `service.go` focused on the 01-05 structural verbs + limiter + `mapStoreError`.

**3. [Presentation normalisation] QueryChannelHistory not-found code**

- The 01-06 fence (`authorizeMember`) returns `PermissionDenied` for a non-member (uniformly, incl. absent channel). The plan's key-link wants "uniform not-found across every new RPC". Rather than re-implement the auth, `QueryChannelHistory` translates the fence's `PermissionDenied` to `NotFound` at the service boundary — the authorization decision is still the single fence; only the wire code is normalised so history is not a hidden-channel oracle either.

**Total deviations:** 1 scope addition (store methods, required), 2 design/presentation choices. No change to the plan's security posture — all must_have truths hold.

## Prohibitions Verified

- **A non-owner (non-admin) MUST NOT invite/mute/ban/kick or transfer (D-05, owner+admin only, NO op):** `TestInviteToChannelNonOwnerDeniedUniformNotFound`, `TestMuteMemberNonOwnerDeniedUniformNotFound`, `TestTransferOwnershipNonOwnerDeniedUniformNotFound` (all denied → uniform NotFound, no store mutation); `TestMuteMemberOwnerMutesAndEmits` / `TestBanMemberOwnerBansAndEmits` / `TestKickMemberOwnerKicksAndEmits` / `TestTransferOwnershipToMemberSucceedsAndEmits` / `TestInviteToChannelOwnerAdmitsTargetAndEmitsJoin` (owner permitted); `TestMuteMemberAdminOverridePermitted` (admin override). **Verified.**
- **A non-member MUST NOT post to / read history from / list members of a channel; a channel the caller cannot see returns uniform not-found:** `TestPostToChannelNonMemberDeniedUniformNotFound`, `TestWhoInChannelNonMemberUniformNotFound`, `TestQueryChannelHistoryNonMemberUniformNotFound` (all NotFound); member paths (`...MemberEmitsSay...`, `...MemberReturnsRoster`, `...MemberReturnsEntries`) succeed; `TestPostToChannelMutedMemberDenied` (muted member → PermissionDenied, no emit). **Verified.**
- **Transfer to a non-member rejected; owner cannot be kicked:** `TestTransferOwnershipToNonMemberRejected` (FailedPrecondition), `TestKickMemberOwnerCannotBeKicked` (FailedPrecondition), `TestKickMemberTargetNotMemberUniformNotFound` (NotFound). **Verified.**
- **PostToChannel carries no channel_name authz field (D-08):** `TestPostToChannelMemberEmitsSayNoChannelName` (`assertNoChannelNameField`). **Verified.**

## Single Membership Fence (HIGH-4 / no second read path)

`QueryChannelHistory` does **not** re-implement history authorization. The membership check that gated the streaming `PluginAuditService.QueryHistory` (01-06) was extracted into `ChannelAuditServer.authorizeMember`; both the streaming path and the new `HistoryForMember` (which the service RPC calls) invoke it, alongside the shared `clampHistoryPageSize`. `main.go` wires `p.service.history = p.auditSrv`. `TestQueryChannelHistoryMemberReturnsEntries` asserts the service builds the qualified subject (`events.<game>.channel.<id>`) and forwards the caller + limit to the fence; `TestQueryChannelHistoryNilFetcherFailsClosed` proves the fail-closed path.

## TDD Gate Compliance

Plan `type: tdd`; the plan-level gate treats the whole plan as one RED→GREEN cycle. **RED** is commit `7a93c60b8` (`test(...)`): full-assertion `service_test.go` for all 8 RPCs; with `service_rpcs.go` absent the embedded `UnimplementedChannelServiceServer` answers the calls, so `task test -- ./plugins/core-channels/` reports **24 failures** (Unimplemented / wrong code). **GREEN** is commit `6b1ab49f5` (`feat(...)`): `service_rpcs.go` lands and all 146 tests pass. No `feat` shipped without its asserting tests; the `test(...)`→`feat(...)` sequence is present in git log. The RED was observed by moving `service_rpcs.go` aside before committing the test commit.

## Verification

- `task test -- ./plugins/core-channels/` — 146 tests, exit 0.
- `task test` (full unit suite) — 10047 tests, 3 skipped (quarantined), exit 0.
- `task test:int -- ./plugins/core-channels/` — 147 tests (unit + store + audit round-trip integration), exit 0.
- `task test:int -- ./test/integration/wholesystem/` — census loads core-channels with the full ChannelService surface, exit 0.
- `task lint:go` — 0 issues. `task lint` overall exits 201 only on pre-existing MD lint in `.planning/` GSD frontmatter artifacts (PLAN.md/STATE.md/deferred-items.md); no core-channels `.go`/`.yaml` issues. Out of scope per the scope boundary (identical to 01-03/04/05/06).
- `task plugin:build -- core-channels` — exit 0 (linux+darwin).

## Per-verb ABAC Gates Applied

| RPC | Gate | Denied code |
|---|---|---|
| InviteToChannel | ABAC `invite` (owner + admin-override) | uniform NotFound |
| MuteMember | ABAC `mute` (owner + admin-override) | uniform NotFound |
| BanMember | ABAC `ban` (owner + admin-override) | uniform NotFound |
| KickMember | ABAC `kick` (owner + admin-override) | uniform NotFound |
| TransferOwnership | ABAC `transfer` (manage-own + admin-override) | uniform NotFound |
| PostToChannel | ABAC `emit` (membership) + IsMuted | NotFound (non-member) / PermissionDenied (muted) |
| WhoInChannel | store `MembershipForHistory` | uniform NotFound |
| QueryChannelHistory | 01-06 `authorizeMember` fence (reused) | uniform NotFound (fence PermissionDenied normalised) |

## User Setup Required

None.

## Next Phase Readiness

- **01-07** command layer now delegates `who`/`history`/`invite`/`mute`/`ban`/`kick`/`transfer`/`post` to a complete service (HIGH-4 closed); it stamps `withTrustedOwningPlayer` before delegating CreateChannel and adds the `channel` command + the Layer-1 `execute-channel-commands` gate.
- **01-08** live moderation→leave removal consumes the ban/kick membership changes (`RemoveStream` on the delivery path).
- **01-09** census asserts core-channels loads with the full service surface.

## Self-Check: PASSED

- Created file present: `plugins/core-channels/service_rpcs.go`.
- Modified files present: `service.go`, `store.go`, `audit.go`, `main.go`, `plugin.yaml`, `service_test.go`.
- All three task commits verified in `git log` (`7a93c60b8`, `6b1ab49f5`, `aaff154bc`).
- Channels unit (146) + integration (147) + whole-system census green.

---

*Phase: 01-channels-subsystem*
*Completed: 2026-07-08*
