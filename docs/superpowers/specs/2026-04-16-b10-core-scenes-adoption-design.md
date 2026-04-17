<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2026 HoloMUSH Contributors -->

# B10 — core-scenes Plugin Adoption Design

**Status:** Draft (awaiting user review)
**Date:** 2026-04-16
**Bead:** `holomush-oy6e.10`
**Epic:** `holomush-oy6e` — Server-Owned Focus Substrate
**Blocks:** `holomush-5rh.13` — Scenes Phase 4: Event streams + pose order
**Depends on:** `holomush-oy6e.8` (plugin host focus RPCs — merged PR #216), `holomush-oy6e.9` (client QueryStreamHistory — merged PR #223), PR #225 (session identity — merged 2026-04-16)

**Parent spec:** [`2026-04-11-focus-substrate-design.md`](./2026-04-11-focus-substrate-design.md). This document is B10-specific and assumes familiarity with the parent spec's invariants (I-1 … I-16), focus transition flows (§4.3, §4.4, §4.5), and plugin host proto (§3.4).

## RFC2119 Keywords

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** in
this document are to be interpreted as described in RFC 2119.

---

## Section 1: Scope

### 1.1 What B10 delivers

1. A binary-plugin SDK facade (`FocusClient`) in `pkg/plugin` that exposes
   the four host focus RPCs (`JoinFocus`, `LeaveFocus`, `PresentFocus`,
   `QueryStreamHistory`) to plugin Go code, parallel in shape to the
   existing `EventSink` facade.
2. `core-scenes` command-path wiring — DB first, focus second, symmetric
   across all state-changing commands:
   - `scene join <id>` → `service.JoinScene` (DB), then `focusClient.JoinFocus`.
   - `scene leave <id>` → `service.LeaveScene` (DB), then `focusClient.LeaveFocus`.
   - `scene end <id>` → `service.EndScene` (DB), then `focusClient.LeaveFocus`
     for the calling (owner's) session.
   - `scene switch <id>` — new subcommand — `focusClient.PresentFocus` (no DB
     write).
3. Plugin-local test doubles for `FocusClient` (a `fakeFocusClient` parallel
   to the existing `fakeEventSink` pattern).
4. The Phase 4 §7.2 acceptance tests that are reachable at B10 scope.
5. Documentation updates in `site/docs/extending/binary-plugins.md` for the
   new `FocusClient` facade and `FocusClientAware` interface.

### 1.2 What B10 explicitly does NOT deliver

- **No `session_id` field added to any scene proto request.** The scene
  plugin service (`SceneService`) remains character-id-only. Justification
  in §3.1.
- **No multi-session fan-out on `EndScene`.** When a non-owner participant
  remains in a scene that ends, their session's `FocusMembership` for that
  scene persists as a cosmetic leak. The scene stops emitting IC events, so
  no stale deliveries occur, but the cursor table retains the row. Fix is
  filed as a follow-up bead (§6.1).
- **No auto-join on `scene switch`.** If target is not already in the
  session's `FocusMemberships`, the command errors with the coordinator's
  `FOCUS_NOT_MEMBER`. No implicit `JoinFocus` call.
- **No Lua plugin adoption.** `core-scenes` is a binary plugin. Channel
  adoption (Lua or binary) is B11, channel-specific.
- **No changes to scene lifecycle event emission.** `service.go`'s existing
  `eventSink.Emit` calls for scene creation/pause/resume/end remain
  unchanged — they publish on the `scene:<id>` stream independent of
  subscription state.

### 1.3 Success criteria

1. `task pr-prep` green.
2. Every new code path has a unit test asserting behavior with the
   `fakeFocusClient` stub.
3. The five Phase 4 §7.2 tests listed in §5.2 pass.
4. No regressions in existing `core-scenes` tests.
5. No changes to `session.FocusMemberships` in any plugin code path —
   all mutations go through `focusClient` (invariant I-6 enforced by
   the facade not exposing a direct-mutation method).

---

## Section 2: SDK Facade — `FocusClient`

### 2.1 Shape

A single-purpose facade in `pkg/plugin`, parallel to `EventSink`. New file
`pkg/plugin/focus_client.go`:

```go
// FocusClient is the SDK-facing facade binary-plugin code uses to drive the
// server-owned focus substrate on behalf of a session. All calls cross the
// plugin broker (mTLS) to the host's PluginHostService.
type FocusClient interface {
    // JoinFocus adds a focus membership and applies the kind-specific
    // server-owned replay policy. Caller provides the target (kind + id);
    // the server determines streams, replay mode, cursor baselines, and
    // subscription updates. Callers MUST NOT declare replay mode or stream
    // names — this is invariant I-7 at the proto boundary.
    JoinFocus(ctx context.Context, sessionID string, target FocusKey) error

    // LeaveFocus removes a focus membership. Idempotent on non-member.
    LeaveFocus(ctx context.Context, sessionID string, target FocusKey) error

    // PresentFocus updates the session's presenting pointer. Target MUST
    // already be in FocusMemberships.
    PresentFocus(ctx context.Context, sessionID string, target FocusKey) error

    // QueryStreamHistory reads the tail of a stream. Read-only (I-13):
    // does not mutate cursors, subscriptions, or session state.
    QueryStreamHistory(ctx context.Context, req QueryStreamHistoryRequest) ([]Event, error)
}

// FocusKey identifies a focus membership within a session.
type FocusKey struct {
    Kind     FocusKind
    TargetID string
}

// FocusKind mirrors session.FocusKind, scoped to the SDK.
type FocusKind string

const (
    FocusKindScene FocusKind = "scene"
)

// QueryStreamHistoryRequest describes a bounded tail read.
type QueryStreamHistoryRequest struct {
    Stream    string
    Count     int       // server clamps to 500
    NotBefore time.Time // zero == no time floor
}

// FocusClientAware is the optional interface service providers implement to
// receive a FocusClient during Init, parallel to EventSinkAware.
type FocusClientAware interface {
    SetFocusClient(FocusClient)
}
```

### 2.2 Injection

The plugin SDK adapter (`pkg/plugin/sdk.go`'s `pluginServerAdapter.Init`)
gains a parallel injection path:

```go
if fcAware, ok := a.serviceProvider.(FocusClientAware); ok {
    client, err := newFocusClientFromBroker(a.brokerDialer, requiredServices)
    if err != nil {
        return nil, oops.With("phase", "init").With("service", PluginHostServiceName).Wrap(err)
    }
    fcAware.SetFocusClient(client)
}
```

`newFocusClientFromBroker` dials the same broker service as `EventSink`
(`PluginHostServiceName = "holomush.plugin.v1.PluginHostService"`),
reusing broker ID resolution and authority string. Only ONE grpc.ClientConn
is dialed per plugin for the host service — `EventSink` and `FocusClient`
MUST share the underlying connection, not open two. Implementation:
construct both from a single cached conn inside Init.

### 2.3 Error mapping

The host returns typed gRPC codes from B8's landed RPCs:

| Host code | SDK behavior |
|---|---|
| `codes.NotFound` + `SESSION_NOT_FOUND` | wrap with `oops.Code("SESSION_NOT_FOUND")` |
| `codes.FailedPrecondition` + `SESSION_EXPIRED` | wrap with `oops.Code("SESSION_EXPIRED")` |
| `codes.AlreadyExists` + `FOCUS_ALREADY_MEMBER` | wrap with `oops.Code("FOCUS_ALREADY_MEMBER")` |
| `codes.InvalidArgument` + `FOCUS_KIND_UNREGISTERED` | wrap with `oops.Code("FOCUS_KIND_UNREGISTERED")` |
| `codes.Internal` + `FOCUS_POLICY_FAILED` | wrap with `oops.Code("FOCUS_POLICY_FAILED")` |
| `codes.NotFound` + `FOCUS_NOT_MEMBER` | wrap with `oops.Code("FOCUS_NOT_MEMBER")` |

Other gRPC errors pass through with `oops.Wrap`. Callers distinguish via
`errutil.IsErrorCode(err, "FOCUS_ALREADY_MEMBER")` etc.

### 2.4 No new host RPCs

`PluginHostService` is unchanged. All four RPCs B10 calls were landed in B8.
B10 adds only client-side Go code and plugin-side adoption.

---

## Section 3: Command-path wiring

### 3.1 Why command-path-only

The SceneService gRPC surface is plugin-internal: the only callers are
(a) the `core-scenes` plugin's own command handlers via local Go method
calls, and (b) unit/integration tests. No external-facing gateway (web or
telnet) invokes `SceneServiceClient.JoinScene` directly — scene mutations
arrive via `CoreService.HandleCommand` → plugin dispatcher → plugin's
internal service method.

PR #225 (session identity, 2026-04-16) codified this pattern at the layer
below: `HandleCommand` now requires `player_session_token` and validates
ownership via `auth.ValidateSessionOwnership`. `CommandRequest.SessionID`
arriving at the plugin is therefore rigorously trusted as owned by the
authenticated player. Scene plugin adoption rides on this.

Adding `session_id` to scene proto requests now would:

- Solve a non-problem (no current caller needs it).
- Diverge from the established "external → HandleCommand" pattern.
- Couple scene proto evolution to session-identity decisions.

**Decision:** focus wiring lives in `plugins/core-scenes/commands.go`
handlers, NOT in `service.go`. `service.go` stays session-unaware.

When a future caller does need session-aware scene RPCs (e.g., a rich
web client's scene management pane), the correct remedy is to add
`session_id` + `player_session_token` fields to the specific RPCs at
that time — not to speculatively add them now. File a tracking bead at
that time; do not pre-wire.

### 3.2 Wired commands

#### 3.2.1 `scene join <scene-id>`

```text
handleJoin(ctx, req, args):
    service.JoinScene(ctx, {character_id: req.CharacterID, scene_id: id})
      ├── OK            → continue
      ├── OpNoChange    → user is already a member; still call JoinFocus
      │                   (may FOCUS_ALREADY_MEMBER; treat as success)
      └── error         → return pluginsdk.Errorf("Failed to join scene: %v", err)

    focusClient.JoinFocus(ctx, req.SessionID, FocusKey{Kind: Scene, TargetID: id})
      ├── OK                    → return OK("Joined scene %s.", id)
      ├── FOCUS_ALREADY_MEMBER  → return OK (idempotent)
      └── error                 → return pluginsdk.Errorf(
                                      "Joined scene in database, but your session could not subscribe (%v). "+
                                      "Please retry `scene join %s`.", err, id)
```

**Failure-window analysis.** Between `service.JoinScene` success and
`focusClient.JoinFocus` failure, the DB says the character is a scene
member but the session has no `FocusMembership`. Consequences:

- Pose events emitted on `scene:<id>:ic` are NOT delivered to this
  session (it's not subscribed).
- Reconnect-restore does NOT heal the gap, because `RestoreFocus`
  iterates `session.FocusMemberships`, which never gained the row.
- User retry (`scene join X` again) is safe: `service.JoinScene` is
  idempotent (returns `OpNoChange`), and `focusClient.JoinFocus`
  re-attempts. If it now succeeds, state is consistent. If it also
  returns `FOCUS_ALREADY_MEMBER` because the first call did partially
  succeed at the coordinator, treat it as success.

This failure mode is visible and user-correctable. No compensation
write is required.

#### 3.2.2 `scene leave <scene-id>`

```text
handleLeave(ctx, req, args):
    service.LeaveScene(ctx, {character_id: req.CharacterID, scene_id: id})
      ├── OK          → continue
      └── error       → return pluginsdk.Errorf("Failed to leave scene: %v", err)

    focusClient.LeaveFocus(ctx, req.SessionID, FocusKey{Kind: Scene, TargetID: id})
      ├── OK          → return OK("Left scene %s.", id)
      └── error       → log warn, return OK (DB write succeeded; session
                        unsubscribe is eventually-consistent — stale focus
                        is cosmetic because the user is no longer a scene
                        member so ABAC blocks writes on the streams)
```

**Order rationale — DB first, focus second.** Symmetric with `scene join`.

- Owner-leave rejection (P3.D7) fires inside `service.LeaveScene` before
  any state is mutated. If `LeaveFocus` ran first, the owner would lose
  their focus subscription before being told they cannot leave — wrong.
- `LeaveFocus` is idempotent on the coordinator (§4.4 of parent spec).
  A failure after a successful DB write leaves the session subscribed
  to a scene it is no longer a DB member of. The scene's ABAC write
  policy (`write-scene-as-participant`) rejects further pose/ooc writes,
  so no incorrect effect occurs. Stream delivery on ambient scene events
  is cosmetic noise until reconnect-resume (which will not re-add the
  membership because the DB write removed it).
- The coordinator-side fix for this stale-focus window is `LeaveFocusByTarget`
  (§6.1). B10 accepts the cosmetic leak.

#### 3.2.3 `scene end <scene-id>`

```text
handleEnd(ctx, req, args):
    service.EndScene(ctx, {character_id, scene_id})
      └── error → return pluginsdk.Errorf(...)

    focusClient.LeaveFocus(ctx, req.SessionID, FocusKey{Kind: Scene, TargetID: id})
      ├── OK    → return OK("Scene %s ended.", id)
      └── error → log warn, return OK (same rationale as Leave)
```

Only the caller's (owner's) session has its focus removed. Other
participants are NOT fanned-out in B10; see §1.2 and §6.1.

#### 3.2.4 `scene switch <scene-id>` — new subcommand

```text
handleSwitch(ctx, req, args):
    focusClient.PresentFocus(ctx, req.SessionID, FocusKey{Kind: Scene, TargetID: id})
      ├── OK               → return OK("Switched to scene %s.", id)
      ├── FOCUS_NOT_MEMBER → return pluginsdk.Errorf(
                                 "You are not a member of scene %s. Use `scene join %s` first.", id, id)
      └── error            → return pluginsdk.Errorf("Failed to switch scene: %v", err)
```

Must be added to `dispatchCommand`'s switch in `commands.go` and to the
help text.

### 3.3 Plugin wiring

`plugins/core-scenes/main.go`:

```go
type scenePlugin struct {
    store       *SceneStore
    service     *SceneServiceImpl
    resolver    *SceneResolver
    focusClient pluginsdk.FocusClient   // new
}

func (p *scenePlugin) SetFocusClient(client pluginsdk.FocusClient) {
    p.focusClient = client
}
```

No change to `Init`, `RegisterServices`, `RegisterAttributeResolver`. The
`FocusClientAware` interface is detected by the SDK adapter during Init.

Command handlers access the client via `p.focusClient`. Unit tests inject
a `fakeFocusClient` (see §5.3).

### 3.4 ABAC gating

`scene join`, `scene leave`, `scene end` already have ABAC policies in
`plugin.yaml` (`join-open-scene`, `leave-scene`, `end-own-scene`, etc.).
These continue to gate the underlying service calls; no changes.

`scene switch` is a pure session-state operation that does not touch
scene resources. It is a subcommand of the existing `scene` command, so
the Layer-1 `execute-scene-commands` policy already permits dispatch. No
new ABAC policy is required. The coordinator's `FOCUS_NOT_MEMBER` check
is the correctness gate — a player cannot switch to a scene they are
not a member of.

---

## Section 4: Error handling summary

| Scenario | Behavior |
|---|---|
| `JoinScene` DB error | Command errors; no `JoinFocus` call; user sees DB error. |
| `JoinScene` OK, `JoinFocus` error | Command returns Error status with explicit retry instruction. DB retains participant row; user retry is idempotent. |
| `JoinFocus` `FOCUS_ALREADY_MEMBER` | Treated as success; return OK to user. |
| `LeaveScene` DB error (incl. owner-leave) | Command errors; no `LeaveFocus` called; no state changed. |
| `LeaveScene` OK, `LeaveFocus` error | Log warn, return OK to user. Cosmetic stale membership. |
| `EndScene` DB error | Command errors; no `LeaveFocus` called. |
| `EndScene` OK, `LeaveFocus` error | Log warn, return OK. |
| `PresentFocus` `FOCUS_NOT_MEMBER` | User-facing error with hint. |
| `PresentFocus` other error | Command errors with generic message. |

No compensating writes in any path. No panics. All error logs are `slog.WarnContext` with `session_id`, `scene_id`, and wrapped `err`.

---

## Section 5: Testing

### 5.1 Unit tests — plugin command handlers

Location: `plugins/core-scenes/commands_test.go` (extended).

New tests, with a `fakeFocusClient` injected:

| Test | Asserts |
|---|---|
| `TestSceneJoinCallsFocusClientJoinFocus` | `service.JoinScene` + `focusClient.JoinFocus` both called; OK returned. |
| `TestSceneJoinPropagatesJoinSceneError` | DB error returns Error; `JoinFocus` NOT called. |
| `TestSceneJoinHandlesJoinFocusError` | DB OK but `JoinFocus` err returns Error with retry hint. |
| `TestSceneJoinTreatsFocusAlreadyMemberAsSuccess` | `FOCUS_ALREADY_MEMBER` → OK. |
| `TestSceneLeaveCallsLeaveScene` | DB `LeaveScene` called first; if OK, `LeaveFocus` called. |
| `TestSceneLeaveRejectsOwnerBeforeFocusChange` | Owner-leave DB error → `LeaveFocus` NOT called. |
| `TestSceneLeaveToleratesLeaveFocusError` | DB OK + focus err → OK status + warn log. |
| `TestSceneEndCallsLeaveFocusForOwner` | DB `EndScene` OK + `LeaveFocus` called with owner session. |
| `TestSceneSwitchCallsPresentFocus` | Routes to `PresentFocus` with correct FocusKey. |
| `TestSceneSwitchReturnsNotMemberError` | `FOCUS_NOT_MEMBER` → Error with usage hint. |
| `TestSceneSwitchStrictArity` | Rejects empty and trailing-args inputs. |

### 5.2 Integration / Phase 4 §7.2 acceptance tests

Reachable at B10 scope (have a running substrate + scene plugin + telnet):

| Test from spec §7.2 | In B10? | Location |
|---|---|---|
| `TestTelnetReconnectResumesSceneWithUnseenEvents` | ✓ | `test/integration/scenes/` |
| `TestFocusSwitchCatchUpUsesBoundedIC` | ✓ | `test/integration/scenes/` |
| `TestFocusSwitchSkipsHistoricalOOC` | ✓ | `test/integration/scenes/` |
| `TestFocusSwitchHonorsPlayerPreference` | ✓ | `test/integration/scenes/` |
| `TestLeaveFocusClearsPresentingWhenReferenced` | ✓ | `test/integration/scenes/` |
| `TestFocusSwitchFallsBackToGameSetting` | covered by B6 coordinator tests | — |
| `TestFocusSwitchClampsOutOfRange` | covered by B6 coordinator tests | — |
| `TestMultiSceneMembershipReconnect` | ✓ stretch | `test/integration/scenes/` |
| `TestPoseOrderConsistentAcrossAllParticipants` | ✗ — requires Phase 4 pose-order wiring, deferred to `holomush-5rh.13` |  |
| `TestChannelJoinLiveOnlyAndHistoryDisplay` | ✗ — channels, deferred to B11 |  |

Each integration test uses testcontainers Postgres, creates players +
characters + a scene via the plugin host, sends commands through the
telnet adapter, asserts event delivery (or non-delivery) on the expected
streams.

### 5.3 Test doubles

New in `plugins/core-scenes/commands_test.go`:

```go
type fakeFocusClient struct {
    joinCalls    []focusCall
    leaveCalls   []focusCall
    presentCalls []focusCall
    joinErr      error
    leaveErr     error
    presentErr   error
}

type focusCall struct {
    sessionID string
    target    pluginsdk.FocusKey
}

func (f *fakeFocusClient) JoinFocus(_ context.Context, sid string, t pluginsdk.FocusKey) error {
    f.joinCalls = append(f.joinCalls, focusCall{sid, t})
    return f.joinErr
}
// ... LeaveFocus, PresentFocus, QueryStreamHistory analogous.
```

No SDK-level `FakeFocusClient` is published yet — plugin-local is
sufficient. Promote to `pkg/plugin` only if B11 (channels) duplicates
the fake (write-it-once-then-copy, hoist on third use).

### 5.4 Non-regression

- All existing `plugins/core-scenes/*_test.go` tests MUST continue to pass.
- Existing `task pr-prep` gates (lint, fmt, license, unit, integration,
  E2E) MUST pass.
- Finding 1 cursor-lock tests and session-identity tests MUST NOT regress.

---

## Section 6: Known limitations / follow-ups

### 6.1 EndScene multi-session fan-out

**Issue.** When `scene end` executes, only the owner's session gets
`LeaveFocus`. Other participant sessions retain a `FocusMembership` for
the now-ended scene. The scene stops emitting IC events, so no stale
delivery occurs, but the cursor table has a dangling row per stale
membership.

**Remedy.** A new host-side operation `LeaveFocusByTarget(FocusKey)` that
sweeps all sessions whose `FocusMemberships` include the given key. This
is a pure host operation (coordinator + session store); no plugin broker
plumbing needed except exposing the RPC.

**Work.** File a follow-up bead, P2, depends on `holomush-oy6e.10`:

- Add `FocusCoordinator.LeaveFocusByTarget(ctx, target) (count int, err error)`.
- Add `PluginHostService.LeaveFocusByTarget` RPC.
- Extend SDK `FocusClient` with the new method.
- Call from `core-scenes` `EndScene` after DB transition commits.

### 6.2 Non-command scene API surface

**Issue.** `SceneService.JoinScene` etc. take only `character_id`. Any
future non-command caller (rich web client, admin tool) that needs to
trigger focus mutations cannot do so through `SceneService` today.

**Remedy when needed.** Add `session_id` + `player_session_token` to the
specific RPC request, apply `auth.ValidateSessionOwnership` at the
handler, then mirror the command-path focus wiring inside the service
method. Matches PR #225's pattern.

**Work.** No bead filed now — premature. File on demand.

### 6.3 `scene switch` UX for non-member target

**Issue.** Player types `scene switch 1064` having never joined scene
1064. Gets `FOCUS_NOT_MEMBER` error with hint to `scene join` first.

**Potential future UX.** Auto-join followed by switch. Not in scope;
auto-join changes the operational semantics of `switch`. If desired,
file a separate bead with explicit UX rationale.

---

## Section 7: Invariant coverage

| Invariant | How B10 preserves |
|---|---|
| I-1 Focus Membership Uniqueness | `FOCUS_ALREADY_MEMBER` passthrough; no plugin-side duplicate attempt. |
| I-4 Atomic Focus Transition | Coordinator owns atomicity; plugin makes a single RPC per operation. |
| I-6 Server-Authoritative Mutation | Plugin never touches `session.FocusMemberships`; facade exposes no direct-mutation method. |
| I-7 Plugin Declaration-Only API | `FocusClient` surface takes only `sessionID` + `FocusKey`; no cursors, no replay modes, no stream names. |
| I-9 Kind-Policy Isolation | No scene-specific logic in the SDK facade; `FocusKind` enum is the only kind-parameter. |
| I-13 Read-Only Plugin History Access | `QueryStreamHistory` returns events; plugin code does not advance cursors. |

Invariants I-3, I-5, I-8, I-10, I-11, I-14, I-15 are host-side concerns
covered by prior beads (B2, B4, B6, B7, B8). B10 does not affect them.

---

## Section 8: File-by-file change list

### Added

- `pkg/plugin/focus_client.go` — `FocusClient` interface, types, `FocusClientAware`, private impl.
- `pkg/plugin/focus_client_test.go` — unit tests for impl (broker dial, error mapping).
- `docs/superpowers/specs/2026-04-16-b10-core-scenes-adoption-design.md` — this document.
- `docs/superpowers/plans/2026-04-16-b10-core-scenes-adoption.md` — implementation plan (next step via writing-plans).

### Deferred (NOT added in this PR)

- `test/integration/scenes/focus_*_test.go` — Phase 4 end-to-end acceptance
  tests. Scoped out of B10 during execution because every invariant listed
  in §5.2 that's reachable today is already covered by unit tests in
  `internal/grpc/focus/` (B4/B6) and `pkg/plugin/` (B8). The remaining tests
  (`TestTelnetReconnectResumesSceneWithUnseenEvents`, the full switch
  catch-up flow, multi-scene merge-sort) exercise pose-order + stream
  plumbing that is Phase 4's responsibility — they belong in
  `holomush-5rh.13` alongside the pose-order work, not B10. The PR
  description lists the specific unit-test locations that cover each
  §5.2 invariant at unit level.

### Modified

- `pkg/plugin/sdk.go` — `pluginServerAdapter.Init` detects `FocusClientAware`, injects client sharing the `EventSink` broker conn.
- `pkg/plugin/event_sink.go` OR new shared helper — refactor broker-dial so `EventSink` and `FocusClient` share a single `*grpc.ClientConn`.
- `plugins/core-scenes/main.go` — add `focusClient` field and `SetFocusClient` method.
- `plugins/core-scenes/commands.go` — extend `dispatchCommand` with `switch` subcommand; update `handleJoin`, `handleLeave`, `handleEnd`, add `handleSwitch`.
- `plugins/core-scenes/commands_test.go` — add `fakeFocusClient`, the 11 unit tests from §5.1.
- `plugins/core-scenes/plugin.yaml` — (no changes expected; existing `execute-scene-commands` policy already permits all subcommands).
- `site/docs/extending/binary-plugins.md` — document `FocusClient` facade and `FocusClientAware`.

### Reserved / deliberate no-ops

- `api/proto/holomush/scene/v1/scene.proto` — no change.
- `api/proto/holomush/plugin/v1/hostfunc.proto` — no change.
- `plugins/core-scenes/service.go` — no change (session-unaware).
- `internal/core/` — no change (plugin boundary preserved).

---

## Section 9: Acceptance checklist

- [ ] `pkg/plugin/focus_client.go` implements `FocusClient` over broker-dialed `PluginHostServiceClient` with typed error wrapping.
- [ ] `pkg/plugin/sdk.go` injects `FocusClient` via `FocusClientAware.SetFocusClient` during `Init`.
- [ ] `EventSink` and `FocusClient` share a single `grpc.ClientConn` in-process.
- [ ] `plugins/core-scenes/main.go` implements `FocusClientAware`.
- [ ] `plugins/core-scenes/commands.go`:
  - [ ] `handleJoin` calls `service.JoinScene` then `focusClient.JoinFocus`.
  - [ ] `handleLeave` calls `service.LeaveScene` then `focusClient.LeaveFocus`.
  - [ ] `handleEnd` calls `service.EndScene` then `focusClient.LeaveFocus` for owner session.
  - [ ] `handleSwitch` added, calls `focusClient.PresentFocus`.
  - [ ] `dispatchCommand` switch and usage-help text updated for `switch`.
- [ ] All 11 unit tests in §5.1 present and passing.
- [ ] 5 Phase 4 §7.2 integration tests from §5.2 present and passing.
- [ ] `task lint` clean.
- [ ] `task pr-prep` clean.
- [ ] `task lint:docs` clean.
- [ ] `site/docs/extending/binary-plugins.md` updated.
- [ ] Follow-up bead filed for `LeaveFocusByTarget` (§6.1).
- [ ] PR reviewed via `pr-review-toolkit:review-pr`; all findings resolved.
- [ ] Post-squash-merge reconciliation per epic §8.0 phase 5 discipline (`jj rebase -r <change-id> -d main --skip-emptied`).
