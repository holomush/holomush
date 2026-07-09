# Phase 2: Scenes Lineage Completion - Research

**Researched:** 2026-07-08
**Domain:** `core-scenes` binary plugin extension — telnet notifications + telnet edge-case hardening
**Confidence:** HIGH (all findings verified against current in-tree code at `path:line`; no external deps)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 (templates descoped):** SCENEFWD-01 removed from Phase 2, returned to backlog (bd `holomush-x4n1r`, P4). Phase 2 is 2 requirements: SCENEFWD-02, SCENEFWD-03.
- **D-02 (telnet nudge, reuse existing delivery):** Non-focused telnet member receives a throttled/coalesced scene-activity nudge via the **existing subscription-router downgrade path** (`ControlFrame{CONTROL_SIGNAL_SCENE_ACTIVITY, scene_id}`, no payload, no decryption) — ADR `holomush-0qnnr`. Extends already-shipped delivery to the telnet gateway's rendering. **NOT** a new notification stream/subject (master spec §3.3 superseded).
- **D-03 (`[>GAME: …]` leader is a reusable primitive):** Telnet system nudges render with a shared `[>GAME: <msg>]` leader — one primitive for ALL game-originated notices. MUST be a shared primitive, not a `core-scenes`-local string. Rendering seam is a research question (resolved below).
- **D-04 (both surfaces):** Telnet `scene mute #X` / `scene unmute #X` (new) + per-character notify on/off pref (persisted). Web mute/prefs UI as a 4-layer typed slice (`SceneAccessService` facade → `WebMuteScene`/prefs BFF RPC on `web.proto` → `internal/web/scene_handlers.go` → `web/src/lib/scenes/*`), built on the shipped create-scene pattern (bd `holomush-5rh.22`). Typed RPC, never the command path.
- **D-05 (realtime now, digest seam):** Realtime delivery only. Prefs/store model MUST leave a seam for a future `realtime | digest` per-character preference — no schema migration or prefs rewrite when digest lands later. Digest itself deferred.
- **D-06 (idle in scope, wire existing infra):** Wire idle-timeout lifecycle using partial existing infra — `idle_timeout_secs` column (`store.go`) + `scene_idle_nudge` event type (`plugin.yaml`/`main.go`); do NOT rebuild. Deliver game-wide default + per-scene override, auto-transition `active → paused` on idle, plus optional idle nudge (OFF by default) through the `[>GAME: …]` leader.
- **D-07 (mixed render branch):** Close `commands.go:890` TODO — add the render branch for when auto-focus-on-join produces both focused and skipped connections.
- **D-08 (reconnection restore):** On telnet reconnect, restore the character's scene memberships AND focus state (spec §11). Builds on live-delivery fixes (`holomush-66228`, `holomush-ymgjs`), session-liveness (AUTHSESS-03), focus store.
- **D-09 (multi-character per connection):** Handle multiple characters bound to a single telnet connection cleanly (focus routing + render targeting). Spec §11 "needs deeper design."

### Claude's Discretion

- Nudge throttling/coalescing policy (per-scene rate, debounce window).
- `[>GAME: …]` rendering seam/placement — gateway `forwardFrame` vs shared `CommLine` primitive (WEBPORT-03) vs verb-registry.
- Exact `[>GAME: <msg>]` wording per notice type; whether the line shows `#id` or scene title.
- Store shape — per-character notify pref + per-scene mute in one table or two (subject to D-05 seam).
- Plan sequencing of reconnection-restore (D-08) vs multi-char (D-09) if they must split.

### Deferred Ideas (OUT OF SCOPE)

- Scene templates (SCENEFWD-01 / bd `holomush-x4n1r`, P4).
- Digest (batched) notification delivery — behind a store/prefs seam (D-05).
- Persisted cross-session read-markers for badges (per ADR `holomush-0qnnr`).
- Generalizing `[>GAME: …]` to channels / other subsystems (primitive is reusable; only scenes wire it this phase).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SCENEFWD-02 | Player receives a notification when a scene they participate in / are invited to has activity — on web (shipped) and telnet (throttled `[>GAME: …]` nudge) | Delivery mechanism confirmed at `internal/grpc/server.go:1287-1317` (badge downgrade) + `internal/web/handler.go:579-618` (web forward). Telnet control-frame seam is `internal/telnet/gateway_handler.go:324-345` (currently no `SCENE_ACTIVITY` case). Mute/prefs typed slice pattern extracted from `WebCreateScene` (`internal/web/scene_handlers.go:153`, `internal/grpc/client.go:421`, `web/src/lib/scenes/createFlow.ts`). Idle infra dormant at `store.go` (`idle_timeout_secs`) + `plugin.yaml:113` (`scene_idle_nudge`); ticker-sweep analog at `publish_scheduler.go`. |
| SCENEFWD-03 | Telnet scene commands handle mixed focused/skipped render, reconnection membership+focus restore, multi-char-per-connection without silent failure | Mixed-render TODO at `commands.go:890` (5-branch switch missing the both-non-empty case). Reconnect-restore primitive `RestoreConnectionFocus` EXISTS + tested (`internal/grpc/focus/restore_connection_focus.go`) but has **no production caller** — the Subscribe/AddConnection path (`server.go:850-899`) never invokes it. Multi-char model characterized below. |
</phase_requirements>

## Summary

This is a **brownfield feature-extension** phase against the already-shipped `core-scenes` binary plugin — no new subsystem, no new plugin, no new external dependencies. Every mechanical decision mirrors the current substrate. The single most important research outcome is a set of **reconciliation corrections** where the CONTEXT hypotheses need adjustment against what is actually in-tree:

1. **Notification delivery already exists end-to-end at the core** — `server.go:1287-1317` already downgrades a non-focused member's scene event to `ControlFrame{CONTROL_SIGNAL_SCENE_ACTIVITY, scene_id}` for **every** connection (telnet included), and the web gateway renders it (`handler.go:579-618`). The **only** telnet gap is that the telnet gateway's control-frame switch (`gateway_handler.go:324-345`) has no `CONTROL_SIGNAL_SCENE_ACTIVITY` case — it silently ignores the badge. So SCENEFWD-02's telnet work is a **rendering + throttle** task, not a delivery task.

2. **`CommLine` (WEBPORT-03) is web-only TypeScript/Svelte** (`web/src/lib/comm/`) — it cannot host the telnet `[>GAME: …]` leader. The reusable telnet primitive must be a **Go** primitive. Recommendation below: a new shared `internal/telnet/gamenotice` (or gateway-render) primitive, wired at the telnet control-frame seam.

3. **D-08's reconnect-focus-restore primitive is already built and proven** (`RestoreConnectionFocus`, `internal/grpc/focus/restore_connection_focus.go`, tested by `test/integration/scenes/reconnect_focus_restoration_test.go` for INV-SCENE-18/25/26) but **has zero production callers** — the Subscribe path (`server.go:850-899`) creates a fresh `Connection` with `FocusKey` unset and never calls restore. D-08 is a **wiring** task, not a build task.

4. **`scene mute`/`unmute` do NOT need `validActions` changes** — they follow the `end`/`pause`/`resume` precedent (engine-`Evaluate` actions + DSL policy), which are NOT in `internal/command/types.go` `validActions` nor in the manifest `actions:` list. Only command-*capability* actions (like `browse`) need registration. This corrects a CONTEXT `code_context` hypothesis.

**Primary recommendation:** Structure the phase as ~4 vertical slices — (A) telnet nudge rendering + throttle at the gateway control-frame seam with a shared `[>GAME: …]` Go primitive; (B) mute/prefs store + telnet commands + web typed slice mirroring `WebCreateScene`; (C) idle-timeout wiring via a `publish_scheduler`-style sweep; (D) telnet edge cases (mixed render one-liner + `RestoreConnectionFocus` wiring + multi-char disambiguation). Bind new INV-SCENE invariants (next free id: **INV-SCENE-70**) for telnet-notify privacy parity and idle transition.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Non-focused scene-event → activity signal | Core (`internal/grpc/server.go`) | — | Already owns the badge downgrade (INV-SCENE-62); privacy boundary lives here, not in a gateway |
| Telnet nudge rendering (`[>GAME: …]`) | Telnet gateway (`internal/telnet`) | — | Protocol-translation/rendering is the gateway's job; MUST NOT compute business state (gateway-boundary) |
| Nudge throttle/coalesce | Telnet gateway | — | The spam is a per-connection render concern (one line per frame); web is idempotent state, so throttle belongs where lines are emitted |
| Mute/notify-pref persistence | `core-scenes` plugin (Postgres) | — | Plugin owns its schema (`plugin_core_scenes`); prefs are plugin-domain data |
| Web mute/prefs write | Web BFF (`internal/web`) → `SceneAccessService` facade (core) → plugin `SceneService` | client `web/src/lib/scenes` | Typed BFF RPC for structural GUI writes (gateway-boundary); never the command path |
| Telnet mute/unmute command | `core-scenes` plugin command handler | Core ABAC engine (Layer-2 `Evaluate`) | Conversational/CLI verbs go through the command path |
| Idle transition (`active→paused`) + idle nudge | `core-scenes` plugin (ticker sweep) | Core EventBus (emit `scene_idle_nudge`) | Lifecycle state is plugin-owned; emit follows the standard plugin emit fence |
| Per-connection focus restore on reconnect | Core focus coordinator (`internal/grpc/focus`) | Core Subscribe path (`server.go`) | Focus coordinator owns the atomic mutation; Subscribe is the reconnect trigger point |

## Standard Stack

**No new external packages.** This phase extends existing in-tree code only. The "stack" is the current substrate:

| Component | Location | Purpose in this phase |
|-----------|----------|-----------------------|
| Badge downgrade path | `internal/grpc/server.go:1278-1317` | Emits `SCENE_ACTIVITY` control frame to non-focused members (extend telnet rendering) |
| Telnet control-frame seam | `internal/telnet/gateway_handler.go:312-345` | Where the `[>GAME: …]` nudge renders (new `SCENE_ACTIVITY` case) |
| Telnet send primitive | `internal/telnet/gateway_handler.go:1084-1099` (`send`, `sendProtoEvent`, `formatEvent`) | Output sink for the nudge line |
| Web BFF scene handlers | `internal/web/scene_handlers.go` (26 `Web*Scene` RPCs) | Add `WebMuteScene`/prefs handler mirroring `WebCreateScene:153` |
| Identity-resolving facade | `internal/grpc/client.go:420-520` (`CreateScene`/`EndScene`/…) + `SceneAccessService` | Add `MuteScene`/`SetSceneNotifyPref` forwarding |
| Web scene client | `web/src/lib/scenes/{createFlow.ts,membershipFlow.ts,client.ts}` | Add mute/prefs flow mirroring `membershipFlow.ts` (invite/kick — closest structural-write analog) |
| Plugin store + migrations | `plugins/core-scenes/store.go`, `plugins/core-scenes/migrations/` (10 up/down pairs) | New mute/prefs table + `idle_timeout_secs` default wiring |
| Ticker sweep pattern | `plugins/core-scenes/publish_scheduler.go` (`Run`+`sweep`, injected `now func() time.Time`) | Idle-transition sweep analog |
| Focus restore primitive | `internal/grpc/focus/restore_connection_focus.go` + `coordinator.go:71` | Wire into Subscribe for D-08 |
| ABAC engine | `p.evaluator.Evaluate(ctx, action, "scene:"+id)` (`commands.go:1287`, `service.go:712`) | Layer-2 gate for mute/unmute |

**Version verification:** N/A — no package installs. Confirmed no external dependency additions are required for any decision.

## Package Legitimacy Audit

**N/A — this phase installs no external packages.** All work extends in-tree Go/TypeScript. No npm/Go-module additions identified in any decision.

## Architecture Patterns

### System Architecture Diagram — scene-activity notification flow (as-shipped + telnet extension)

```
 Participant poses in scene #7
        │
        ▼
 core-scenes emits core-scenes:scene_pose  ──►  EventBus (JetStream)
        │                                             │
        │                          per-member subscription delivery
        ▼                                             ▼
 [FOCUSED member]                          [NON-FOCUSED member]  (server.go:1287)
   full scene event                          extractSceneID(subject) → sid
   forwarded live                            downgrade → ControlFrame{
        │                                        SCENE_ACTIVITY, SceneId: sid }
        │                                             │
   ┌────┴──────────┐                    ┌─────────────┴──────────────┐
   ▼               ▼                    ▼                            ▼
 telnet          web               WEB gateway                 TELNET gateway
 formatEvent   translateEvent    handler.go:610-618          gateway_handler.go:324
   → line        → event frame    forwardFrame →               *** NO SCENE_ACTIVITY
                                  webv1.ControlFrame              CASE TODAY ***  ← THE GAP
                                  {SCENE_ACTIVITY, SceneId}       (D-02: add case →
                                       │                           throttle → [>GAME: …])
                                       ▼
                                  svelte: unread badge (idempotent)
```

Trace: a busy scene produces one `SCENE_ACTIVITY` frame **per event** to a non-focused member. Web coalesces naturally (badge state is idempotent set-true). Telnet, if it renders one line per frame, spams — hence D-02's mandatory throttle/coalesce **at the telnet render seam**.

### Recommended slice structure

```
Slice A — telnet activity nudge (SCENEFWD-02 core)
  internal/telnet/gateway_handler.go   # SCENE_ACTIVITY case at :324 switch
  internal/telnet/gamenotice/*.go      # NEW shared [>GAME: …] Go primitive (D-03)
  + per-(scene_id) debounce state on GatewayHandler
Slice B — mute/prefs (SCENEFWD-02 controls)
  plugins/core-scenes/migrations/000011_scene_notify_prefs.{up,down}.sql
  plugins/core-scenes/store.go         # prefs read/write
  plugins/core-scenes/commands.go      # scene mute/unmute subcommand cases at :479
  plugins/core-scenes/plugin.yaml      # mute/unmute DSL policies
  api/proto/.../scene(access) + web.proto  # MuteScene / SetSceneNotifyPref / WebMuteScene
  internal/grpc/client.go + SceneAccessService facade
  internal/web/scene_handlers.go       # WebMuteScene handler
  web/src/lib/scenes/notifyFlow.ts + UI
Slice C — idle timeout (SCENEFWD-02 polish)
  plugins/core-scenes/idle_scheduler.go  # NEW, mirrors publish_scheduler.go
  plugins/core-scenes/store.go           # ListScenesIdlePastThreshold + game default
  plugins/core-scenes/lifecycle.go       # active→paused already valid (:22)
Slice D — telnet edge cases (SCENEFWD-03)
  plugins/core-scenes/commands.go:890    # mixed-render branch (one switch case)
  internal/grpc/server.go:~899           # wire RestoreConnectionFocus post-AddConnection
  (multi-char: focus/render disambiguation — see Open Questions)
```

### Pattern 1: Extend the control-frame switch (D-02 telnet nudge)

**What:** Add a `CONTROL_SIGNAL_SCENE_ACTIVITY` case to the telnet event loop's control switch. The core already sends the frame; telnet just ignores it today.
**Where:** `internal/telnet/gateway_handler.go:324` (main loop) — note there is a SECOND control switch at `:1057` (`drainUntilClosed`) that only needs `STREAM_CLOSED`; the nudge belongs in the **main** loop only.
**Example (current, no SCENE_ACTIVITY handling):**
```go
// Source: internal/telnet/gateway_handler.go:324-344 (VERIFIED)
case *corev1.SubscribeResponse_Control:
    if frame.Control.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED {
        // ...return to picker / return
    }
    // REPLAY_COMPLETE: no-op for telnet.
    slog.DebugContext(childCtx, "gateway: replay complete", "session_id", h.sessionID)
```
The new case calls the shared `[>GAME: …]` primitive with `frame.Control.GetSceneId()`, gated by the per-scene debounce (Pattern 2). The gateway is a thin translation layer (INV-EVENTBUS-6 discipline at `:1109`): it renders the signal, it does not resolve scene titles from the DB — if a title (not `#id`) is wanted, that is a discretion call requiring the core to carry a label in the frame (the frame has only `scene_id` today, `server.go:1300`).

### Pattern 2: Per-scene debounce for coalescing (D-02 throttle — Claude's discretion)

**What:** Suppress repeat nudges for the same `scene_id` within a debounce window, so a busy scene emits at most one `[>GAME: …]` line per window.
**Survey result:** No reusable throttle/debounce helper exists in-tree. The only time-based infra is `time.NewTicker` (refresh loop `gateway_handler.go:171`, publish scheduler `publish_scheduler.go:60`) and `time.After` backoff (`gateway_handler.go:765`). **Recommendation:** a small per-`GatewayHandler` `map[string]time.Time` (scene_id → last-nudged) guarded by the handler's existing single-goroutine event loop (no extra lock needed — the loop is single-consumer), with a configurable window (recommend 30–60s default, matching the "not one per pose" intent). This is per-connection state, correctly scoped: it resets on reconnect (fresh handler), which is acceptable.
**When to use:** every `SCENE_ACTIVITY` frame render. Reject (skip render) if `now - last < window`; always update `last`.

### Pattern 3: Typed BFF slice for web mute/prefs (D-04) — mirror `WebCreateScene`

**What:** 4-layer typed write, NEVER the command path (gateway-boundary rule).
**The exact shipped analog to copy** (structural write = invite/kick, closest to mute):
```
proto:   api/proto/holomush/web/v1/web.proto      rpc WebMuteScene(...)          (:287 WebCreateScene precedent)
         api/proto/holomush/sceneaccess/v1/...     rpc MuteScene / SetSceneNotifyPref
BFF:     internal/web/scene_handlers.go:153        WebCreateScene → resolves session, forwards
facade:  internal/grpc/client.go:421               CreateScene → SceneAccessService (identity-resolving)
client:  web/src/lib/scenes/membershipFlow.ts      invite/kick flow (structural toggle analog)
         web/src/lib/scenes/client.ts              connect-web transport
```
**Why membershipFlow, not createFlow:** mute is a toggle on an existing scene (structural write on a resource you already reference by id), exactly like invite/kick — createFlow builds a new resource. Reuse membershipFlow's request/response shape and error handling.

### Pattern 4: Ticker-sweep for idle transition (D-06) — mirror `publish_scheduler`

**What:** A plugin-owned sweep that finds scenes idle past their threshold and transitions `active → paused`, optionally emitting `scene_idle_nudge`.
**Exact analog:** `plugins/core-scenes/publish_scheduler.go` — `Run(ctx)` ticks at an interval, `sweep(ctx)` queries expired rows via a narrow store interface, applies per-row transitions, WARN-logs per-row failures without aborting the batch, uses an injected `now func() time.Time` for deterministic tests. Idle sweep needs:
- `store.go`: `ListScenesIdlePastThreshold(ctx, nowNs)` filtered on `state='active'` AND `last_activity_ms + idle_timeout_secs*1000 ≤ now` (last-activity is already computed for the board — `ListCharacterScenes`/`ListBoard` use `last_activity_ms`, `store.go:1772`).
- `idle_timeout_secs` is already a nullable column read into `IdleTimeoutSecs *int` (`store.go:68`); wire a **game-wide default** (config knob, following the `config:` block at `plugin.yaml:37`) with per-scene override (the column).
- Transition uses the existing `IsValidTransition(active, paused)` (`lifecycle.go:22`, already valid).
- Idle nudge OFF by default: the emit is behind a game/scene flag; `scene_idle_nudge` is already declared (`plugin.yaml:113,177`) but **has no emitter wiring today** (main.go:186 registers it only for INV-PLUGIN-32 set-equality; `plugin.yaml:180` explicitly says "Trigger implementation deferred to follow-up bead holomush-fux3"). This phase is that trigger implementation.

### Pattern 5: Wire the reconnect focus restore (D-08)

**What:** After a reconnecting telnet connection registers, restore its per-connection `FocusKey` from the surviving `Info.PresentingFocus`.
**The gap, precisely:** `server.go:861-867` creates a fresh `Connection{}` with `FocusKey` unset. `RestoreConnectionFocus(ctx, sessionID, connID)` exists (`internal/grpc/focus/restore_connection_focus.go`, `coordinator.go:71`) and is proven by `test/integration/scenes/reconnect_focus_restoration_test.go` (INV-SCENE-18/25/26) — but **grep confirms zero production callers**. Without the call, a reconnected member whose `PresentingFocus` was a scene is treated as non-focused by the badge downgrade (`server.go:1287-1295`) → they get `[>GAME: …]` nudges instead of live scene content. Wire the call right after `AddConnection` succeeds (`server.go:878`), gated to avoid clobbering web per-tab focus (recommend: only when `PresentingFocus != nil` — restore is a no-op/grid-fallback otherwise per INV-SCENE-18).

### Pattern 6: Mixed-render branch (D-07) — one switch case

**What:** `commands.go:892-908` is a 5-branch switch on `AutoFocusOnJoin` outcome. The both-non-empty case (`FocusedConnectionIDs>0 AND SkippedConnectionIDs>0`, no failures) currently falls to the bare `default` ("Joined scene #%s.") — the least-informative message, i.e. silent under-reporting. Add an explicit case before `default`:
```go
// Source: commands.go:892 (VERIFIED — insert new case)
case len(afResult.FocusedConnectionIDs) > 0 && len(afResult.SkippedConnectionIDs) > 0:
    msg = fmt.Sprintf("Joined scene #%s and focused some connection(s); "+
        "others stay on their current focus (use 'scene focus #%s').", sceneID, sceneID)
```

### Anti-Patterns to Avoid

- **Rendering the telnet nudge from a `core-scenes`-local string** — violates D-03 (must be a shared primitive). Put it in a gateway-owned Go primitive.
- **Reaching for `sendCommand`/`HandleCommand` for the web mute toggle** — violates gateway-boundary "structural writes use typed RPCs." Use `WebMuteScene`.
- **A new `notifications:<char>` stream/subject** — superseded by ADR `holomush-0qnnr`; use the `ControlFrame` downgrade (Reconciliation Constraint).
- **Adding `mute`/`unmute` to `validActions` in `internal/command/types.go`** — unnecessary; they are engine-`Evaluate` actions like `end`/`pause` (not command-capability actions). Adding them is harmless noise that implies a command capability that does not exist.
- **Throttling at the core downgrade** — the core frame is per-member and web needs every one (idempotent badge); throttle only at the telnet render seam.
- **Rebuilding idle infra** — `idle_timeout_secs` + `scene_idle_nudge` exist; wire, do not re-declare.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Deliver activity signal to non-focused members | A new notification stream / poll | The shipped `ControlFrame` downgrade (`server.go:1287`) | Already privacy-correct (INV-SCENE-62), already reaches telnet connections |
| Web mute write | `sendCommand` string-building | `WebMuteScene` typed BFF RPC mirroring `WebCreateScene` | gateway-boundary rule; the facade→BFF→client slice is a shipped, tested pattern |
| Idle-sweep loop | A bespoke goroutine/timer scheme | `publish_scheduler.go` `Run`/`sweep` shape with injected `now` | Deterministically testable, per-row-failure-tolerant, already reviewed |
| Reconnect focus restore | New CAS logic in the gateway | Call the existing `RestoreConnectionFocus` coordinator method | Atomic mutation + INV-SCENE-18/25/26 already proven; only the call site is missing |
| Scene last-activity for idle | New activity timestamp column | Existing `last_activity_ms` used by `ListBoard`/`ListCharacterScenes` (`store.go:1772`) | Already computed from the IC subject stream |

**Key insight:** ~80% of this phase is *wiring already-built substrate to a surface that does not yet consume it* (telnet render of an existing frame; a call site for an existing restore primitive; an emitter for an already-declared event; a sweep mirroring an existing scheduler). The genuine net-new is the mute/prefs store shape + typed slice, and the throttle policy.

## Common Pitfalls

### Pitfall 1: Two telnet control switches
**What goes wrong:** Adding the `SCENE_ACTIVITY` case only to `drainUntilClosed` (`gateway_handler.go:1057`), or to both.
**Why:** `drainUntilClosed` runs only during quit/logout drain; nudges there are wrong. The live loop is `:324`.
**How to avoid:** Add the case at `:324` only. **Warning sign:** nudges appearing during logout, or never during play.

### Pitfall 2: Badge/telnet privacy parity regression (INV-SCENE-62)
**What goes wrong:** Extending telnet rendering in a way that leaks scene content to a non-participant/non-focused member.
**Why:** The downgrade guard (`server.go:1287-1295`) is the privacy chokepoint; the telnet render must consume ONLY `scene_id` from the control frame, never re-fetch content.
**How to avoid:** The nudge line is content-free (`[>GAME: Scene #7 has new activity]`). Bind a new `INV-SCENE-70` "telnet SCENE_ACTIVITY nudge carries no scene content." **Warning sign:** the render path touches `service`/`store`/decryption.

### Pitfall 3: Idle emitter set-equality (INV-PLUGIN-32 / INV-SCENE-2)
**What goes wrong:** Changing `crypto.emits`/`EmitRegistry` for the idle nudge and breaking the manifest↔registry set-equality that fails plugin load.
**Why:** `scene_idle_nudge` is ALREADY in both `phase4EmitTypes()` (`main.go:186`) and `plugin.yaml:177`. Do NOT re-add it. Just add the emitter call.
**How to avoid:** Emitter only; leave the declarations. **Warning sign:** `EVENT_TYPE_REGISTRY_MISMATCH` at load.

### Pitfall 4: Bare `#id` vs scene title in the nudge
**What goes wrong:** Rendering the scene *title* requires a DB lookup the gateway MUST NOT do (gateway-boundary).
**Why:** The control frame carries only `SceneId` (`server.go:1300`).
**How to avoid:** Either render `#id` (no lookup), or extend the frame to carry a short label at the core (a proto change). Recommend `#id` for this phase; flag title as a follow-up. **Warning sign:** a gateway-side `service.GetScene` call.

### Pitfall 5: Restoring focus for web tabs unintentionally (D-08)
**What goes wrong:** Calling `RestoreConnectionFocus` unconditionally on every Subscribe clobbers a web tab's freshly-set focus.
**Why:** Web sets per-tab focus explicitly; PresentingFocus is "primarily telnet single-pane reconnect UX" (`session.go:239-243`).
**How to avoid:** Gate the restore (it is a no-op when `PresentingFocus==nil`; verify web sets PresentingFocus lazily/never per the comment). Confirm behavior with a web-tab test. **Warning sign:** a web tab losing its chosen scene focus after any resubscribe.

### Pitfall 6: `task test` does not compile integration files
**What goes wrong:** Refactoring shared focus/session types and only running `task test`.
**Why:** `//go:build integration` files (the whole `test/integration/scenes/` suite, incl. `reconnect_focus_restoration_test.go`) compile only under `task test:int`.
**How to avoid:** Run `task test:int` after any change to `session`/`focus`/`SubscribeResponse` shapes.

## Code Examples

### Existing badge downgrade (the delivery this phase renders on telnet)
```go
// Source: internal/grpc/server.go:1287-1316 (VERIFIED)
if connID != nil {
    if sid, ok := extractSceneID(string(event.Subject)); ok {
        var currentFocus *session.FocusKey
        if conn, getErr := s.sessionStore.GetConnection(ctx, *connID); getErr == nil {
            currentFocus = conn.FocusKey
        }
        focusedOn := currentFocus != nil &&
            currentFocus.Kind == session.FocusKindScene &&
            currentFocus.TargetID.String() == sid
        if !focusedOn {
            badge := &corev1.SubscribeResponse{Frame: &corev1.SubscribeResponse_Control{
                Control: &corev1.ControlFrame{
                    Signal:  corev1.ControlSignal_CONTROL_SIGNAL_SCENE_ACTIVITY,
                    SceneId: sid,
                },
            }}
            // ... stream.Send(badge); Ack; return
        }
    }
}
```

### Existing web forward of the same frame (telnet must reach parity)
```go
// Source: internal/web/handler.go:610-618 (VERIFIED)
case *corev1.SubscribeResponse_Control:
    stream.Send(&webv1.StreamEventsResponse{
        Frame: &webv1.StreamEventsResponse_Control{
            Control: &webv1.ControlFrame{
                Signal:  mapCoreSignalToWeb(frame.Control.GetSignal()), // SCENE_ACTIVITY→SCENE_ACTIVITY (:579)
                SceneId: frame.Control.GetSceneId(),
                // ...
            },
        },
    })
```

### Layer-2 ABAC precedent for mute/unmute
```go
// Source: plugins/core-scenes/service.go:712 & commands.go:1287 (VERIFIED)
dec, evalErr := s.evaluator.Evaluate(ctx, "end", "scene:"+req.GetSceneId())
// mute follows this shape: p.evaluator.Evaluate(ctx, "mute", "scene:"+sceneID)
// with a new plugin.yaml policy: permit(... action in ["mute"] ...)
//   when { principal.id in resource.scene.participants }
// No validActions/actions: manifest change needed (mute is not a command capability).
```

## Reconciliation Landmines — resolved

| CONTEXT hypothesis | Verified reality | Planner action |
|--------------------|------------------|----------------|
| "gateway `forwardFrame` must propagate `scene_id`" | Web `forwardFrame` ALREADY propagates it (`handler.go:617`). Telnet has no `forwardFrame`; its seam is the control switch at `gateway_handler.go:324` which drops the frame. | Telnet work = new control-switch case, not a `forwardFrame` edit. |
| "`[>GAME: …]` may live in the shared `CommLine` primitive (WEBPORT-03)" | `CommLine` is web-only TS (`web/src/lib/comm/`). No Go equivalent. | The telnet leader is a NEW Go primitive (recommend `internal/telnet/gamenotice`). CommLine is not reusable here. |
| "add scene `mute`/`unmute` to `validActions`/`validResourceTypes`" | `end`/`pause`/`resume`/etc are engine-`Evaluate` actions and are NOT in `validActions` (`types.go:115`) nor `actions:` (`plugin.yaml:69`). Only command-capability actions (`browse`) are. | Do NOT touch `validActions`. Add a DSL policy only. |
| "reconnection restoring focus … builds on substrate" | The restore primitive `RestoreConnectionFocus` is fully built + tested but has ZERO production callers. | D-08 = wire the existing call at `server.go:~878`, plus a web-safety gate. |
| "idle nudge — wire existing infra" | `scene_idle_nudge` declared but explicitly deferred (`plugin.yaml:180` → bead `holomush-fux3`); NO emitter, NO idle sweep exists. | Net-new emitter + sweep (mirror `publish_scheduler.go`), reusing the column + event type. |

## Runtime State Inventory

Not applicable — this is a greenfield-within-brownfield feature extension (no rename/refactor/migration of existing identifiers). New DB state (mute/prefs table) is additive via a new migration pair; no existing runtime state is renamed or re-keyed.

## Validation Architecture

`workflow.nyquist_validation: true` (`.planning/config.json:24`) — VALIDATION.md derivable from below.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + testify (unit); Ginkgo/Gomega (integration, `//go:build integration`) |
| Config file | none (Taskfile-driven) |
| Quick run command | `task test -- ./plugins/core-scenes/... ./internal/telnet/... ./internal/web/...` |
| Full suite command | `task test:int` (integration, needs Docker) + `task test:e2e` (Playwright, web) |
| Coverage gate | >80% per-package (`task test:cover`) |

### Phase Requirements → Test Map
| Req | Behavior | Test Type | Command | Exists? |
|-----|----------|-----------|---------|---------|
| SCENEFWD-02 | Non-focused telnet member renders `[>GAME: …]` on scene activity | integration | `task test:int` (extend `test/integration/scenes/scene_activity_badge_test.go`) | ✅ badge test exists; ❌ telnet-render assertion Wave 0 |
| SCENEFWD-02 | Busy scene coalesces to ≤1 nudge/window | unit | `task test -- ./internal/telnet/...` | ❌ Wave 0 |
| SCENEFWD-02 | Nudge carries no scene content (INV-SCENE-70 privacy parity) | unit+integration | `task test:int` | ❌ Wave 0 |
| SCENEFWD-02 | `scene mute`/`unmute` persist + suppress nudges; ABAC participant-gated | unit+integration | `task test -- ./plugins/core-scenes/...` | ❌ Wave 0 |
| SCENEFWD-02 | Web `WebMuteScene` typed slice writes pref (not command path) | integration+e2e | `task test:int` + `task test:e2e` (`web/e2e/scenes.spec.ts`) | ❌ Wave 0 |
| SCENEFWD-02 | Notify-pref store leaves `realtime\|digest` seam (D-05) — no migration to add digest | unit (schema/model assertion) | `task test -- ./plugins/core-scenes/...` | ❌ Wave 0 |
| SCENEFWD-02 | Idle sweep transitions `active→paused` at threshold; idle nudge OFF by default | unit (injected `now`)+integration | `task test:int` | ❌ Wave 0 |
| SCENEFWD-03 | Mixed focused+skipped join renders the both-non-empty branch | unit | `task test -- ./plugins/core-scenes/...` (extend `commands_focus_test.go`) | ✅ focus test file exists; ❌ mixed case Wave 0 |
| SCENEFWD-03 | Reconnect restores per-connection scene focus (not badge downgrade) | integration | `task test:int` (`reconnect_focus_restoration_test.go` + NEW end-to-end telnet Subscribe wiring test) | ⚠️ coordinator tested; ❌ Subscribe-wiring assertion Wave 0 |
| SCENEFWD-03 | Reconnect restore does NOT clobber web-tab focus | integration | `task test:int` | ❌ Wave 0 |
| SCENEFWD-03 | Multi-character-per-connection focus/render targeting | integration | `task test:int` (`multi_connection_visibility_test.go` is the sibling) | ❌ Wave 0 (pending model clarification) |

### Sampling Rate (Nyquist edge cases the plans MUST cover)
- **Throttle coalescing:** N poses in a window → exactly 1 telnet nudge; window boundary → 2nd nudge fires.
- **Privacy parity on downgrade:** non-participant at same location gets NO nudge and NO content (INV-SCENE-6 + INV-SCENE-62); non-focused participant gets a content-free nudge only.
- **Reconnection race:** concurrent reconnect vs leave (already covered by INV-SCENE-25 test — extend to the wired Subscribe path).
- **Multi-char routing:** a connection cycling characters must not leak the prior character's scene focus (stale `FocusKey`).
- **Idle transition:** boundary at exactly `idle_timeout_secs`; per-scene override beats game default; idle nudge suppressed when flag OFF; a paused scene does not re-transition.
- **Mute suppression:** muted scene emits no telnet nudge and no web badge for that character.

### Wave 0 Gaps
- [ ] `internal/telnet/gamenotice/*_test.go` — `[>GAME: …]` primitive rendering.
- [ ] Telnet-render assertion in `test/integration/scenes/scene_activity_badge_test.go` (or a new `telnet_scene_activity_nudge_test.go`).
- [ ] `plugins/core-scenes/notify_prefs_test.go` + `idle_scheduler_test.go` (injected `now`).
- [ ] `internal/grpc/server_test.go` / integration: Subscribe wiring calls `RestoreConnectionFocus`.
- [ ] `web/e2e/scenes.spec.ts` mute-toggle path.
- [ ] INV-SCENE-70/71 `// Verifies:` bindings once tests land.

## Security Domain

`security_enforcement: true` (`.planning/config.json:46`). ABAC + privacy are load-bearing here.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V4 Access Control | yes | Layer-1 command-execution gate (`execute-scene-commands` policy already covers `scene`); Layer-2 per-resource `Evaluate("mute", "scene:"+id)` with a new participant-gated DSL policy |
| V5 Input Validation | yes | Scene-id normalization (`normalizeSceneID`, `resolveSceneRef`) reused for mute/unmute args |
| V6 Cryptography | no (nudge) | Nudge is content-free; `scene_idle_nudge` is `sensitivity: never` (plaintext by design, `plugin.yaml:178`) — no DEK involved |
| Privacy boundary | yes | INV-SCENE-62 (FocusMemberships ⊆ participants) + INV-SCENE-6 (non-participant location isolation) MUST hold when extending render to telnet |

### Known Threat Patterns
| Pattern | STRIDE | Mitigation |
|---------|--------|-----------|
| Scene content leaks to non-focused/non-participant via telnet nudge | Information disclosure | Content-free `[>GAME: …]` line; render consumes only `scene_id`; bind INV-SCENE-70 |
| Mute/unmute on a scene the caller is not in | Elevation / tampering | Layer-2 `mute-scene-as-participant` DSL policy, fail-closed (default-deny) |
| Reconnect restores focus to a scene the character was kicked from mid-disconnect | Elevation | `RestoreConnectionFocus` already validates membership (INV-SCENE-18: grid-fallback when membership revoked — tested) |
| Web mute via command path bypassing typed-RPC ABAC | Tampering | Typed `WebMuteScene` only; gateway-boundary forbids `sendCommand` for structural writes |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | 30–60s is a reasonable default nudge debounce window | Pattern 2 | Low — tunable config; wrong value is a UX nuisance, not a correctness bug |
| A2 | Web clients set `PresentingFocus` lazily/never, so unconditional restore-gating on `PresentingFocus!=nil` is safe for web | Pattern 5 / Pitfall 5 | Medium — if web DOES set PresentingFocus, restore could fight per-tab focus; MUST verify with a web-tab test before wiring |
| A3 | Multi-char-per-connection (D-09) means one telnet TCP connection cycling characters (logout→re-pick), not two live characters simultaneously | Open Q3 | Medium — the exact model changes the render-targeting design; needs planner/spec confirmation |
| A4 | Rendering `#id` (not scene title) in the nudge is acceptable this phase | Pitfall 4 | Low — discretion item; title is a proto-extension follow-up |
| A5 | A single `scene_notify_prefs` table with a `mode` column (default `realtime`) satisfies the D-05 digest seam without a later migration | Store shape | Medium — if per-scene mute and per-character pref have different cardinality, may want two tables; verify against query patterns |

## Open Questions (RESOLVED)

1. **Nudge label: `#id` vs scene title.** Frame carries only `scene_id` (`server.go:1300`); a title needs either a gateway DB lookup (forbidden) or a core proto extension. **RESOLVED (Plan 01):** ship `#id` this phase — `gamenotice.Activity(sceneID)` renders `[>GAME: Scene #<id> has new activity]` from the bare frame scene_id (no lookup); a title field on the control frame is a filed follow-up, out of scope for Phase 2.
2. **Store shape (D-05):** one `scene_notify_prefs(character_id, scene_id nullable, muted bool, mode text default 'realtime')` table — a NULL `scene_id` row = the per-character global notify pref, a non-NULL row = per-scene mute. This unifies D-04's two controls and leaves the `mode` column as the digest seam. **RESOLVED (Plan 02):** one `scene_notify_prefs` table (migration 000011) — NULL `scene_id` = per-character global notify pref, non-NULL = per-scene mute; the `mode` column ships defaulting `realtime` as the D-05 digest seam, so digest lands later with no migration.
3. **D-09 multi-character model:** the substrate binds one active character per telnet connection (`charName` + `connectionID`, `gateway_handler.go`). Spec §11's "multiple characters per connection" needs the planner to pin the exact scenario (character-switch mid-connection vs. simultaneous). The primitives (per-connection `FocusKey`, connection→character binding, `AutoFocusOnJoin` fan-out) exist; the likely gap is clearing stale scene `FocusKey` when a connection swaps characters. Cite `2026-05-21-scenes-phase-5-focus-model...` spec. **RESOLVED (Plan 07):** pinned to the SEQUENTIAL character-swap-per-connection model (Assumption A3); `RestoreConnectionFocus`'s INV-SCENE-18 membership validation blocks a swapped-in character B from inheriting character A's scene focus (grid-fallback when B is not a member), with a defensive per-connection `FocusKey` clear on character-unbind if any residual is found — Plan 07 Task 3.
4. **D-08 restore trigger scope:** should restore fire for every client_type or telnet only? PresentingFocus is "primarily telnet single-pane" (`session.go:240`). **RESOLVED (Plan 07):** restore fires whenever `PresentingFocus != nil` (naturally telnet-biased), wired at Subscribe after AddConnection (`server.go:878`); a web resubscribe with nil PresentingFocus is not clobbered — Plan 07 Task 2, guarded by A2's verification.

## Sources

### Primary (HIGH confidence — in-tree code, authoritative)
- `internal/grpc/server.go:850-899, 1278-1317` — connection registration + badge downgrade path.
- `internal/web/handler.go:567-634` — web control-frame forward + signal mapping.
- `internal/telnet/gateway_handler.go:312-345, 595-640, 1084-1143` — telnet control loop, reattach, render.
- `internal/grpc/focus/restore_connection_focus.go` + `coordinator.go:65-71` — restore primitive (no prod caller — verified via `rg`).
- `plugins/core-scenes/commands.go:479-536, 820-913, 1276-1290` — subcommand dispatch, auto-focus render, ABAC.
- `plugins/core-scenes/plugin.yaml` — verbs, `scene_idle_nudge`, `crypto.emits`, commands, ABAC policies.
- `plugins/core-scenes/store.go:44-68, 1772-1794` — `idle_timeout_secs`, `last_activity_ms`.
- `plugins/core-scenes/publish_scheduler.go` — ticker-sweep pattern.
- `plugins/core-scenes/main.go:177-204` — emit registries; `lifecycle.go:17-32` — transition validity.
- `internal/command/types.go:108-197` — ABAC action/resource registry (mute/unmute NOT needed).
- `internal/web/scene_handlers.go` + `internal/grpc/client.go:420-520` — typed BFF facade slice.
- `web/src/lib/scenes/` (createFlow/membershipFlow) + `web/src/lib/comm/` (CommLine is web-only).
- `test/integration/scenes/reconnect_focus_restoration_test.go` — INV-SCENE-18/25/26 coverage.
- `docs/architecture/invariants.yaml` — INV-SCENE max id = 69 (next free: 70).
- `.planning/config.json` — nyquist/security/tdd toggles all true.

### Secondary (MEDIUM confidence — design docs, reconciled against code)
- ADR `holomush-0qnnr` (delivery mechanism), master spec `2026-04-06-scenes-and-rp-design-v2.md` §3.3/§4.4/§11, WEBPORT-03 `2026-06-25-shared-web-communication-seam-design.md`, AUTHSESS-03 `2026-05-30-...`, focus-model `2026-05-21-...`.

### Tertiary (LOW confidence)
- None — all claims grounded in code or reconciled design docs.

## Metadata

**Confidence breakdown:**
- Delivery/rendering seams: HIGH — exact `path:line` verified for every frame hop.
- ABAC/store patterns: HIGH — precedent handlers + policies read directly.
- Idle sweep: HIGH — analog scheduler read; declared-but-unwired state confirmed.
- D-08 wiring: HIGH — primitive exists, zero-caller confirmed via `rg`.
- D-09 model + store cardinality: MEDIUM — needs planner/spec confirmation (Open Q2/Q3).

**Research date:** 2026-07-08
**Valid until:** 2026-08-07 (30 days — stable brownfield substrate; re-verify `server.go`/`gateway_handler.go` line numbers if the branch advances significantly).
