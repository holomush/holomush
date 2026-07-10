---
phase: 01-channels-subsystem
plan: 02
subsystem: plugin-host
tags: [plugin-sdk, stream-subscription, session-streams, abac, pluginauthz, live-delivery, eventbus]

# Dependency graph
requires:
  - phase: 01-01
    provides: ChannelService proto (core-channels is the first consumer of this substrate)
provides:
  - Binary SDK SessionStreamsHandler hook (QuerySessionStreams routed to plugin code)
  - RELATIVE-only plugin session-stream contribution with a shared namespace fence
  - Served stream.subscription capability (AddSessionStream/RemoveSessionStream)
  - LIVE_ONLY replay mode end-to-end (no history flood on mid-session join)
  - Instance-level concrete-stream authz guard (pluginauthz.AuthorizeStreamSubscribe)
  - seed:plugin-stream-subscribe instance-level write permit
  - Binary SDK StreamSubscription client + StreamSubscriptionAware
affects: [01-04, 01-05, 01-05b, 01-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Single shared authz gate across both plugin stream-contribution paths (pluginauthz.AuthorizePluginStreamContribution) — establishment merge + mid-session guard cannot diverge"
    - "gameID-free, engine-free namespace fence (leading-domain segment == post-Qualify domain segment)"
    - "In-handler forbidden/owned-namespace fence as the load-bearing write-path control (read-only ABAC forbids do not fence a write permit)"

key-files:
  created:
    - pkg/plugin/session_streams_handler.go
    - pkg/plugin/stream_subscription_client.go
    - internal/plugin/pluginauthz/streamsubscribe.go
  modified:
    - pkg/plugin/sdk.go
    - pkg/plugin/capability_declaration.go
    - internal/plugin/manager.go
    - internal/grpc/stream_registry.go
    - internal/plugin/hostcap/servers.go
    - internal/plugin/hostcap/capabilities.go
    - internal/plugin/goplugin/host.go
    - internal/plugin/lua/hostcap_adapter.go
    - internal/plugin/hostfunc/functions.go
    - internal/plugin/setup/subsystem.go
    - internal/access/policy/seed.go

key-decisions:
  - "Plugin session-stream contributions are RELATIVE-only (reject events.-prefixed + colon) — reverts HIGH-1's blanket full-events. widening (R3-A) so no session_streams plugin can inject a pre-qualified foreign subject"
  - "ONE shared fence (AuthorizePluginStreamContribution) enforced at BOTH establishment (manager) and mid-session (AuthorizeStreamSubscribe) so the two paths cannot diverge (R3-A)"
  - "Forbidden/owned-namespace protection is IN-HANDLER, not the read-only seed forbids, because those are action==read and do not carve back the broad write permit (R2-B)"
  - "Lua adapter OwnedEmitDomains returns nil (fail-closed): no per-plugin manifest emit surface on Functions; permitted availability-shape asymmetry, no in-tree Lua consumer"

patterns-established:
  - "AuthorizeStreamSubscribe mirrors AuthorizeStreamRead: relative-only reject → wildcard reject → forbidden-namespace → owned-namespace → Qualify → capability decision"
  - "HostCapabilities port grows StreamRegistry() + OwnedEmitDomains(pluginName); binary host reads manifest.Emits, Lua returns nil"

requirements-completed: [CHAN-01, CHAN-02]

coverage:
  - id: D1
    description: "Binary plugin SessionStreamsHandler routes QuerySessionStreams to plugin code; event-only plugins get an empty non-error response"
    requirement: "CHAN-01"
    verification:
      - kind: unit
        ref: "pkg/plugin/session_streams_handler_test.go#TestQuerySessionStreamsRoutesToHandlerWhenImplemented"
        status: pass
      - kind: unit
        ref: "pkg/plugin/session_streams_handler_test.go#TestQuerySessionStreamsReturnsEmptyForEventOnlyPlugin"
        status: pass
    human_judgment: false
  - id: D2
    description: "Manager.QuerySessionStreams keeps a relative own-domain channel.<id> (HIGH-1) and drops pre-qualified/colon/forbidden/foreign/wildcard refs via the shared establishment fence (R3-A)"
    requirement: "CHAN-01"
    verification:
      - kind: unit
        ref: "internal/plugin/manager_test.go#TestManagerQuerySessionStreamsFencesForeignAndForbiddenContributions"
        status: pass
      - kind: unit
        ref: "internal/plugin/pluginauthz/streamsubscribe_test.go#TestAuthorizePluginStreamContributionRejects"
        status: pass
    human_judgment: false
  - id: D3
    description: "Served stream.subscription runs the in-handler concrete-stream guard (HIGH-3/R2-B) before mutating the registry; forbidden/foreign/wildcard/pre-qualified rejected even with the broad write permit active"
    requirement: "CHAN-02"
    verification:
      - kind: unit
        ref: "internal/plugin/hostcap/streamsubscribe_test.go#TestAddSessionStreamRejectsInHandlerEvenWithBroadWritePermit"
        status: pass
      - kind: unit
        ref: "internal/plugin/pluginauthz/streamsubscribe_test.go#TestAuthorizeStreamSubscribeRejectsForbiddenAndForeignInHandler"
        status: pass
    human_judgment: false
  - id: D4
    description: "LIVE_ONLY accepted end-to-end (HIGH-2): AddStreamWithMode accepts ReplayModeLiveOnly and forwards it; FROM_CURSOR unchanged, BoundedTail still rejected"
    requirement: "CHAN-02"
    verification:
      - kind: unit
        ref: "internal/grpc/stream_registry_test.go#TestAddStreamWithModeAcceptsLiveOnly"
        status: pass
      - kind: unit
        ref: "internal/grpc/stream_registry_test.go#TestAddStreamWithModeRejectsUnsupportedModes"
        status: pass
    human_judgment: false
  - id: D5
    description: "Binary SDK StreamSubscription client + StreamSubscriptionAware; undeclared use fails closed at load (INV-PLUGIN-54); LIVE_ONLY expressible from the SDK"
    requirement: "CHAN-02"
    verification:
      - kind: unit
        ref: "pkg/plugin/stream_subscription_client_test.go#TestStreamSubscriptionAwareFailsClosedWhenUndeclared"
        status: pass
      - kind: unit
        ref: "pkg/plugin/stream_subscription_client_test.go#TestStreamSubscriptionClientAddStreamMapsLiveOnly"
        status: pass
    human_judgment: false

# Metrics
duration: 95min
completed: 2026-07-08
status: complete
---

# Phase 1 Plan 02: Live-delivery host substrate Summary

**Binary SDK SessionStreamsHandler + served stream.subscription capability with real LIVE_ONLY delivery, fenced by ONE shared relative-only owned-namespace guard (pluginauthz.AuthorizePluginStreamContribution) across both the session-establishment merge and the mid-session AddSessionStream path.**

## Performance

- **Duration:** ~95 min
- **Completed:** 2026-07-08
- **Tasks:** 3 (all TDD)
- **Files modified:** 24 files (incl. tests), 3 created source files

## Accomplishments

- **HIGH-1 + R3-A:** Binary `SessionStreamsHandler` hook closes the binary/Lua session-establishment gap; `Manager.QuerySessionStreams` now KEEPS domain-relative `channel.<id>` contributions (was dropping dot refs by requiring a colon) but is RELATIVE-only — a pre-qualified `events.` subject or colon ref is rejected, and every contributed ref is run through the shared namespace fence, dropping+logging forbidden/foreign refs at establishment.
- **HIGH-2:** `SessionStreamRegistry.AddStreamWithMode` now accepts `ReplayModeLiveOnly` (channels' mid-session join) in addition to FROM_CURSOR; BoundedTail still rejected. No-history-flood is structural (SetFilters preserves the live consumer's start policy on filter rotation).
- **HIGH-3 + R2-A/R2-B:** Served the `stream.subscription` capability (was `codes.Unimplemented`). Both RPCs run `pluginauthz.AuthorizeStreamSubscribe` — the shared fence (relative-only, no-wildcard, forbidden system/audit/crypto, owned-emit-domain) + Qualify + the `write` capability decision — BEFORE the host `StreamRegistry`. Cross-namespace/foreign/wildcard subscription is rejected IN-HANDLER even with the broad `seed:plugin-stream-subscribe` write permit active.
- Binary SDK `StreamSubscription` client + `StreamSubscriptionAware` (fail-closed at load when undeclared); LIVE_ONLY expressible from the SDK.
- Existing scene FROM_CURSOR replay + stream.history read authz preserved (regression-asserted); full integration suite green (10203 tests).

## Task Commits

1. **Task 1: SessionStreamsHandler + relative-only + shared establishment fence** — `620f4177b` (feat)
2. **Task 2: serve stream.subscription — LIVE_ONLY + concrete-stream guard + seed** — `f7fdd1e73` (feat)
3. **Task 3: SDK StreamSubscription client + Aware + capability declaration** — `81100eaeb` (feat)
4. **errcheck lint fix (Task 2 file)** — `a241f7e05` (fix)

## Files Created/Modified

- `pkg/plugin/session_streams_handler.go` (new) — SessionStreamsHandler interface + SessionStreamsRequest.
- `pkg/plugin/stream_subscription_client.go` (new) — StreamSubscription client + Aware + SDK ReplayMode.
- `internal/plugin/pluginauthz/streamsubscribe.go` (new) — shared AuthorizePluginStreamContribution fence + AuthorizeStreamSubscribe.
- `pkg/plugin/sdk.go` — route QuerySessionStreams to the handler; inject the StreamSubscription client at Init.
- `pkg/plugin/capability_declaration.go` — StreamSubscriptionAware → stream.subscription (fail-closed).
- `internal/plugin/manager.go` — isValidStreamName relative-only; QuerySessionStreams merge runs the shared fence per ref.
- `internal/grpc/stream_registry.go` — AddStreamWithMode accepts ReplayModeLiveOnly.
- `internal/plugin/hostcap/servers.go` — real streamSubscriptionServer (guard → registry, no inner-error leak).
- `internal/plugin/hostcap/capabilities.go` — port gains StreamRegistry() + OwnedEmitDomains().
- `internal/plugin/goplugin/host.go` — binary port impls + SetStreamRegistry; reads manifest.Emits.
- `internal/plugin/lua/hostcap_adapter.go` — Lua port impls (StreamRegistry from Functions; OwnedEmitDomains nil).
- `internal/plugin/hostfunc/functions.go` — GetStreamRegistry accessor.
- `internal/plugin/setup/subsystem.go` — thread StreamRegistry into the binary host.
- `internal/access/policy/seed.go` — seed:plugin-stream-subscribe write permit + R2-B comment.

## Fence-security items (orchestrator directive)

- **HIGH-2 (write-permit fencing):** `AuthorizeStreamSubscribe` REJECTS a direct-namespace / pre-qualified `events.` input with `STREAM_NOT_RELATIVE` and requires the ref's leading domain be one of the plugin's owned emit domains. The `seed:plugin-stream-subscribe` permit is deliberately broad; the in-handler fence (independently tested with an AllowAllEngine) is the load-bearing control, not the read-only ABAC forbids (R2-B, verified `seed.go` forbids are all `action in ["read"]`).
- **HIGH-3 (unified fence, both paths):** BOTH plugin stream-contribution paths call the SAME `pluginauthz.AuthorizePluginStreamContribution` — the session-establishment merge (`Manager.QuerySessionStreams`, where per-plugin `p.name` + `Manifest.Emits` are in scope) and the mid-session `AddSessionStream`/`RemoveSessionStream` (via `AuthorizeStreamSubscribe`). There is no second unfenced path: `server.go`'s Subscribe merge only ever sees already-fenced relative refs from the manager loop. Asserted against the shared function in both `manager_test.go` and `streamsubscribe_test.go`.

## Decisions Made

- Plugin session-stream contributions are RELATIVE-only (R3-A), reverting HIGH-1's blanket full-`events.` acceptance; no in-tree plugin declares `session_streams`, so nothing regresses.
- Lua `OwnedEmitDomains` returns nil (fail-closed) — `*hostfunc.Functions` has no per-plugin manifest emit surface. This is a permitted availability-shape asymmetry (identical policy chokepoint, no in-tree Lua consumer of the host.v1 stream.subscription path), not a privilege gradient.

## Deviations from Plan

None affecting scope. One in-scope lint fix (`a241f7e05`): errcheck rejected the blank-discard type assertion `code, _ = oopsErr.Code().(string)`; restructured to an explicit `if c, ok := ...` form. No behavior change.

## Known Limitations / Follow-ups

- The pre-existing ambient Lua hostfunc `holomush.add_session_stream` (`internal/plugin/hostfunc/stdlib_streams.go`) is a SEPARATE, older path that does NOT pass through `AuthorizeStreamSubscribe`. It is out of scope for this plan (which fences the host.v1 served capability). Flagged for a follow-up so the Lua ambient path shares the same fence — recommend a bead against `holomush-l6std`.
- `holomush-l6std` (stream.subscription served) is RESOLVED for the host.v1 path. Mid-session delivery landed fully (Task 2/3) — no degradation to document.

## TDD Gate Compliance

Plan `type: tdd`. Each task's tests were written and observed failing (RED) before implementation, then committed atomically GREEN. Commits are `feat(...)` (not split `test(...)`→`feat(...)`) because the repo's stronger no-broken-intermediate-commit rule (`task lint` MUST pass before commit; plan-review-learnings warn against non-compiling intermediate commits) takes precedence over the RED-commit split. A separate `test(...)` commit of a non-compiling test tree would violate that rule.

## Issues Encountered

- Adding two methods to the shared `HostCapabilities` port required updating the binary host, the Lua adapter, and the `stubHostCaps` test stub together; the `var _ hostcap.HostCapabilities` compile-assertions caught this immediately. Full `task test:int` (10203 tests) confirms the shared-type refactor broke no integration surface.
- `task lint:go` green (0 issues). `task lint:markdown` reports failures ONLY in pre-existing `.planning/` GSD docs (documented as out-of-scope in `deferred-items.md` since 01-01); none in this plan's source.

## abac-reviewer Gate

This plan touches `internal/access/policy/seed.go` and `internal/plugin/pluginauthz/`. The `abac-reviewer` domain gate MUST fire before push (`/holomush-dev:review-abac`), per CLAUDE.md § Pre-Push Review Gates and the plan's abac-reviewer directive.

## Next Phase Readiness

- core-channels (binary plugin) can now (a) contribute channel streams at connect/reconnect via SessionStreamsHandler with relative `channel.<id>` refs, and (b) mutate subscriptions mid-session via the served stream.subscription capability with real LIVE_ONLY and a fail-closed instance-level guard.
- 01-08 (JoinChannel/LeaveChannel) MUST pass the RELATIVE `channel.<id>` form (a `relativeChannelStream(id)` helper), NOT a full `events.<game>.channel.<id>` subject — the guard rejects pre-qualified subjects with STREAM_NOT_RELATIVE.

## Self-Check: PASSED

---

*Phase: 01-channels-subsystem*
*Completed: 2026-07-08*
