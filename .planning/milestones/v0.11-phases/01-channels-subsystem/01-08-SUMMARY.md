---
phase: 01-channels-subsystem
plan: 08
subsystem: core-channels
tags: [core-channels, session-streams, stream-subscription, live-delivery, guest-auto-join, plugin-sdk]

# Dependency graph
requires:
  - phase: 01-02
    provides: SessionStreamsHandler SDK hook, StreamSubscription client + Aware, shared AuthorizePluginStreamContribution fence, real LIVE_ONLY
  - phase: 01-03
    provides: ListDefaultChannels seam (is_default seeded set)
  - phase: 01-05
    provides: JoinChannel/LeaveChannel structural RPCs
  - phase: 01-07
    provides: channel command layer threading req.SessionID into Join/LeaveChannelRequest
provides:
  - core-channels QuerySessionStreams (memberships ∪ default channels, banned excluded) at session establishment
  - guest auto-join via default-channel union (no membership-row write, D-01)
  - mid-session join/leave live delivery via stream.subscription (LIVE_ONLY)
  - graceful-degradation floor (subscription failure logged, not silently dropped)
affects: [01-09]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Session-stream contribution is the deduplicated union of resource-side memberships and the seeded default set — guest auto-join needs no membership-row write"
    - "Relative channel.<id> subject form at BOTH boundaries (establishment + mid-session); host owns Qualify; emit path keeps the full dotStyleChannelSubject"
    - "Live-subscribe failure degrades to next-establishment delivery (logged, non-fatal) rather than failing the structural mutation"

key-files:
  created:
    - plugins/core-channels/session_streams_test.go
  modified:
    - plugins/core-channels/main.go
    - plugins/core-channels/service.go
    - plugins/core-channels/store.go
    - plugins/core-channels/plugin.yaml
    - plugins/core-channels/service_test.go

key-decisions:
  - "Guest auto-join is served by unioning ListDefaultChannels into QuerySessionStreams (resource-side, plaintext), NOT a session-establishment membership-row write (D-01/D-04)"
  - "IsBannedFrom returns false for a missing row so a guest (no membership) receives a default while an explicit banned=true membership excludes it"
  - "A live-subscribe error is LOGGED but NOT propagated — membership is already committed; failing the join would be misleading. Delivery degrades to the next QuerySessionStreams refresh (graceful-degradation floor, holomush-l6std)"
  - "Both AddStream/RemoveStream and QuerySessionStreams pass the domain-RELATIVE channel.<id> (relativeChannelStream); a pre-qualified events. subject is rejected by 01-02 AuthorizeStreamSubscribe (STREAM_NOT_RELATIVE)"

requirements-completed: [CHAN-01, CHAN-02]

coverage:
  - id: D1
    description: "QuerySessionStreams returns memberships ∪ default channels as relative channel.<id> refs, deduped, banned defaults excluded"
    requirement: "CHAN-01"
    verification:
      - kind: unit
        ref: "plugins/core-channels/session_streams_test.go#TestQuerySessionStreamsReturnsMemberChannelsAsRelativeRefs"
        status: pass
      - kind: unit
        ref: "plugins/core-channels/session_streams_test.go#TestQuerySessionStreamsDedupesMemberAndDefaultOverlap"
        status: pass
      - kind: unit
        ref: "plugins/core-channels/session_streams_test.go#TestQuerySessionStreamsExcludesBannedDefault"
        status: pass
    human_judgment: false
  - id: D2
    description: "A guest (no membership rows) receives exactly the seeded default channels via the union (guest auto-join, D-01)"
    requirement: "CHAN-01"
    verification:
      - kind: unit
        ref: "plugins/core-channels/session_streams_test.go#TestQuerySessionStreamsGuestReceivesExactlyDefaultChannels"
        status: pass
    human_judgment: false
  - id: D3
    description: "Relative channel.<id> ref, once host-Qualified, resolves to the same events.<game>.channel.<id> subject the emit path publishes on"
    requirement: "CHAN-01"
    verification:
      - kind: unit
        ref: "plugins/core-channels/session_streams_test.go#TestRelativeChannelStreamQualifiesToEmitSubject"
        status: pass
    human_judgment: false
  - id: D4
    description: "Mid-session join adds a LIVE_ONLY subscription for the RELATIVE channel.<id>; leave removes it"
    requirement: "CHAN-02"
    verification:
      - kind: unit
        ref: "plugins/core-channels/service_test.go#TestJoinChannelAddsLiveOnlySubscriptionForRelativeStream"
        status: pass
      - kind: unit
        ref: "plugins/core-channels/service_test.go#TestLeaveChannelRemovesSubscriptionForRelativeStream"
        status: pass
      - kind: unit
        ref: "plugins/core-channels/service_test.go#TestJoinChannelPassesRelativeNotQualifiedSubject"
        status: pass
    human_judgment: false
  - id: D5
    description: "Graceful-degradation floor: a subscription error does not fail the join/leave; missing session id / unwired client skip cleanly"
    requirement: "CHAN-02"
    verification:
      - kind: unit
        ref: "plugins/core-channels/service_test.go#TestJoinChannelSubscriptionErrorDoesNotFailJoin"
        status: pass
      - kind: unit
        ref: "plugins/core-channels/service_test.go#TestJoinChannelNoSessionIdSkipsSubscription"
        status: pass
      - kind: unit
        ref: "plugins/core-channels/service_test.go#TestJoinChannelNilStreamSubSkipsCleanly"
        status: pass
    human_judgment: false
  - id: D6
    description: "core-channels loads with session_streams: true + stream.subscription requires; whole-system census green"
    requirement: "CHAN-01"
    verification:
      - kind: integration
        ref: "test/integration/wholesystem/ (census)"
        status: pass
    human_judgment: false

# Metrics
duration: 55min
completed: 2026-07-08
status: complete
---

# Phase 1 Plan 08: Plugin live-delivery Summary

**core-channels wires the plugin half of live delivery onto the 01-02 substrate: `QuerySessionStreams` contributes the character's memberships ∪ seeded default channels (banned excluded) at session establishment as domain-RELATIVE `channel.<id>` refs (guest auto-join via the default-channel union, no membership-row write), and mid-session `join`/`leave` add/remove the live subscription with LIVE_ONLY through the served `stream.subscription` capability — all passing the single shared 01-02 fence.**

## Performance

- **Duration:** ~55 min
- **Completed:** 2026-07-08
- **Tasks:** 2 (both TDD)
- **Files:** 1 created, 5 modified

## Accomplishments

- **Task 1 — QuerySessionStreams (session establishment).** `channelPlugin` implements `pluginsdk.SessionStreamsHandler`. It returns the deduplicated UNION of (a) the character's non-banned memberships (`ListForCharacter`) and (b) the seeded default channels (`ListDefaultChannels`, 01-03) minus any default the character is banned from, each mapped to the domain-RELATIVE `relativeChannelStream(id)` → `"channel."+id` (R2-A). A guest — a session with zero membership rows — therefore receives EXACTLY the seeded default channels with no membership-row write at establishment (D-01, resource-side, plaintext D-04). `store.IsBannedFrom` is the per-default ban filter (a missing row → `false`, so a guest is not banned; an explicit `banned=true` membership excludes the default). Manifest declares `session_streams: true`.
- **Task 2 — mid-session join/leave.** `JoinChannel` calls `streamSub.AddStream(sessionID, "channel.<id>", LIVE_ONLY)` after the membership commits; `LeaveChannel` calls `RemoveStream(sessionID, "channel.<id>")` after removal — both passing the RELATIVE form the 01-02 `AuthorizeStreamSubscribe` guard permits (a pre-qualified `events.` subject is rejected with `STREAM_NOT_RELATIVE`). The `StreamSubscription` client is injected via `StreamSubscriptionAware` (main.go → `service.SetStreamSubscription`). Manifest declares `requires: [capability: stream.subscription, access: write]` (fail-closed at load if omitted, INV-PLUGIN-54).
- **Single fence confirmed.** Every contributed ref — at establishment and mid-session — is a relative own-domain `channel.<id>`, so all flow through 01-02's SINGLE shared `pluginauthz.AuthorizePluginStreamContribution` (establishment: `Manager.QuerySessionStreams`; mid-session: `AuthorizeStreamSubscribe`). This plan adds no second, unfenced contribution path. `channel` is core-channels' declared `emits` domain, so the fence permits its own join and the host Qualifies each ref to `events.<game>.channel.<id>` — the emit subject.
- **Graceful-degradation floor.** A live-subscribe/unsubscribe error is logged via `errutil.LogErrorContext` but NOT propagated: the membership is already committed, so failing the structural RPC would be misleading. Delivery degrades to the next `QuerySessionStreams` refresh (never silently dropped, holomush-l6std). A missing session id (out-of-session action) or unwired client is skipped cleanly.

## Task Commits

1. **Task 1: QuerySessionStreams session-establishment channel subscription** — `f43c14c09` (feat)
2. **Task 2: mid-session join/leave live delivery via stream.subscription** — `93c918c0d` (feat)

## Files Created/Modified

- `plugins/core-channels/session_streams_test.go` (new) — QuerySessionStreams union/dedup/ban/guest/qualify unit tests.
- `plugins/core-channels/main.go` — `QuerySessionStreams` impl + `sessionStreamStore` interface + `streamStore` field (wired in Init) + `SetStreamSubscription` (StreamSubscriptionAware) forwarding to the service.
- `plugins/core-channels/service.go` — `relativeChannelStream(id)` helper; `streamSub` field + `SetStreamSubscription`; `subscribeLive`/`unsubscribeLive` (LIVE_ONLY add / remove, degrade-on-error); wired into `JoinChannel`/`LeaveChannel` after the store mutation.
- `plugins/core-channels/store.go` — `IsBannedFrom` (per-default ban filter; missing row → false).
- `plugins/core-channels/plugin.yaml` — `session_streams: true`; `requires: [capability: stream.subscription, access: write]`.
- `plugins/core-channels/service_test.go` — mid-session join/leave subscription unit tests (relative form, LIVE_ONLY, no-session-id skip, nil-client skip, degrade-on-error).

## QuerySessionStreams union behavior

`QuerySessionStreams(req)` returns, as relative `channel.<id>` refs, the dedup-by-id union of:

1. the character's non-banned memberships (`ListForCharacter` — already excludes `banned=true` and archived), and
2. the seeded default channels (`ListDefaultChannels`) the character is not banned from (`IsBannedFrom` false).

A default the character is also an explicit member of appears once. A guest (no memberships) gets exactly the default set. Empty (no error) only when there are neither memberships nor seeded defaults. An unwired store fails closed (no silent empty contribution).

## Mid-session delta + degradation floor

`join` → `AddStream(sess, "channel.<id>", LIVE_ONLY)` after commit; `leave` → `RemoveStream(sess, "channel.<id>")`. LIVE_ONLY advances the cursor to tail (no history flood, T-01-09). On a subscription error the join/leave still succeeds (membership consistent), the failure is logged, and live delivery falls back to the next session-establishment `QuerySessionStreams` — never silently dropped.

## Deviations from Plan

None affecting scope. Two supporting additions the plan implied but did not name explicitly:

1. **`store.IsBannedFrom`** — the plan's "ban filter applies to defaults too" behavior needs a way to distinguish a *banned member* (excluded) from a *non-member guest* (included). `ListForCharacter`/`MembershipForHistory` both collapse those to false, so a dedicated `IsBannedFrom` (missing row → false) was added. [Rule 2 - missing critical functionality]
2. **Fail-closed on unwired session-stream store** — `QuerySessionStreams` returns an error (not an empty set) when `streamStore` is nil, so a wiring bug surfaces rather than silently contributing nothing. [Rule 2]

## Degradation status (Landmine 2)

**No degradation.** 01-02 landed mid-session serving FULLY (its SUMMARY: "Mid-session delivery landed fully — no degradation to document"; `AddStreamWithMode` accepts real `LIVE_ONLY`, and `AddSessionStream`/`RemoveSessionStream` are served + fenced). The `join`/`leave` LIVE_ONLY subscription calls therefore take effect immediately mid-session — the "join takes effect on next session-stream refresh" fallback path was NOT needed. The in-code degradation floor remains as a runtime safety net (a transient host-registry failure logs and falls back), not a plan-shape degradation.

## TDD Gate Compliance

Plan `type: tdd`. For each task the tests were written and observed failing (RED — compile failure on the undefined symbols) before the implementation, then verified GREEN. Commits are `feat(...)` (not split `test(...)`→`feat(...)`) following the repo's stronger no-broken-intermediate-commit rule (`task lint`/build MUST pass before commit; a `test(...)` commit of a non-compiling test tree would violate it) and the precedent set by 01-02.

## Verification

- `task test -- ./plugins/core-channels/` — green (188 tests; +8 from this plan).
- `task lint:go` — 0 issues. `task lint` markdown failures are ONLY pre-existing `.planning/` GSD docs (out of scope, tracked in `deferred-items.md` since 01-01); no source failures.
- `task test:int` — full suite green (10392 tests, 5 skipped/quarantined); whole-system census (`./test/integration/wholesystem/`) green — core-channels loads with `session_streams: true` + `stream.subscription`.
- Member-vs-non-member LIVE delivery + mid-session join is the 01-09 integration assertion (prohibition T-01-01, status pending there).

## Next Phase Readiness

- 01-09 integration can now assert end-to-end: a member connected at establishment receives `channel_say` live, a non-member does not; a mid-session `join` starts live delivery via LIVE_ONLY without reconnect; a guest receives exactly the seeded default channels.

## Self-Check: PASSED

---

*Phase: 01-channels-subsystem*
*Completed: 2026-07-08*
