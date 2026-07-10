# Phase 2: Scenes Lineage Completion - Pattern Map

**Mapped:** 2026-07-08
**Files analyzed:** 14 (create/modify) across 4 vertical slices
**Analogs found:** 13 / 14 (one net-new primitive has a structural analog, not a role-exact one)

> This phase is ~80% *wiring already-built substrate to a surface that does not yet
> consume it*. RESEARCH.md verified every seam at `path:line`; this map deepens the
> high-value seams into copyable excerpts. Where research already resolved a
> reconciliation landmine (e.g. no `validActions` change, no `forwardFrame` edit,
> `CommLine` is web-only TS), that verdict is carried forward — do not re-litigate.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/telnet/gateway_handler.go` (modify — add `SCENE_ACTIVITY` case at :324) | gateway/handler | event-driven (control frame) | existing `STREAM_CLOSED` case in the same switch `:324-345` | exact |
| `internal/telnet/gamenotice/*.go` (NEW — `[>GAME: …]` primitive) | utility/renderer | transform (string) | no role-exact analog; nearest is telnet `send`/`formatEvent` sink | partial (structural) |
| `plugins/core-scenes/commands.go` (modify — `mute`/`unmute` cases; mixed-render branch :890) | command handler | request-response (CLI verb) | `end`/`pause`/`resume` gated subcommands `:484-489` | exact |
| `plugins/core-scenes/store.go` (modify — notify-prefs read/write; idle query) | store/repository | CRUD | `SceneRow`/`scanSceneRow` + existing store methods | exact |
| `plugins/core-scenes/migrations/000011_scene_notify_prefs.{up,down}.sql` (NEW) | migration | schema | `000010_participants_observer_role.{up,down}.sql` | exact |
| `plugins/core-scenes/plugin.yaml` (modify — mute/unmute DSL policies) | config/policy | ABAC DSL | existing `end`/`resume` participant policies `:273-314` | exact |
| `plugins/core-scenes/idle_scheduler.go` (NEW — idle sweep) | service (ticker sweep) | batch/event-driven | `plugins/core-scenes/publish_scheduler.go` | exact |
| `plugins/core-scenes/main.go` (modify — wire idle emitter + scheduler) | provider/wiring | event-driven | existing scheduler/emitter registration | exact (research: emitter-only; leave declarations) |
| `api/proto/holomush/web/v1/web.proto` (modify — `WebMuteScene`/prefs RPC) | proto/route | request-response | `WebCreateScene` RPC precedent | exact |
| `api/proto/holomush/sceneaccess/v1/*.proto` (modify — `MuteScene`/`SetSceneNotifyPref`) | proto/route | request-response | `CreateScene`/`EndScene` facade RPCs | exact |
| `internal/web/scene_handlers.go` (modify — `WebMuteScene` handler) | BFF handler | request-response | `WebCreateScene` handler `:149-178` | exact |
| `internal/grpc/client.go` (modify — facade forward) | facade/client | request-response | `CreateScene`/`EndScene` facade `:419-436` | exact |
| `web/src/lib/scenes/notifyFlow.ts` (NEW — mute/prefs client flow) | client/store | request-response | `web/src/lib/scenes/membershipFlow.ts` (invite/kick toggle) | exact (per research: membershipFlow, not createFlow) |
| `internal/grpc/server.go` (modify — wire `RestoreConnectionFocus` post-AddConnection :878) | server/wiring | event-driven | the AddConnection block `:854-899` (call site is the gap) | exact (primitive already built + tested) |

## Pattern Assignments

### Slice A — telnet activity nudge

#### `internal/telnet/gateway_handler.go` (gateway, event-driven)

**Analog:** the `STREAM_CLOSED` case in the *same* control switch — mirror its shape for a new `CONTROL_SIGNAL_SCENE_ACTIVITY` case. **Main loop only (`:324`)** — NOT the `drainUntilClosed` switch at `:1057` (Pitfall 1).

**Existing switch to extend** (`gateway_handler.go:324-345`):

```go
case *corev1.SubscribeResponse_Control:
    if frame.Control.GetSignal() == corev1.ControlSignal_CONTROL_SIGNAL_STREAM_CLOSED {
        if msg := frame.Control.GetMessage(); msg != "" {
            h.send(msg)
        }
        // ...server-initiated disconnect → return to picker...
        return
    }
    // REPLAY_COMPLETE: no-op for telnet — replay renders the same as live.
    slog.DebugContext(childCtx, "gateway: replay complete", "session_id", h.sessionID)
```

**New case shape** (insert a `CONTROL_SIGNAL_SCENE_ACTIVITY` branch): consume ONLY
`frame.Control.GetSceneId()`, pass to the `gamenotice` primitive, gate on the
per-scene debounce. MUST NOT re-fetch scene content (Pitfall 2 / INV-SCENE-70).
The frame carries only `scene_id` (`server.go:1300`) → render `#id`, not title (Pitfall 4).

**Debounce state (Pattern 2, Claude discretion):** add a `map[string]time.Time`
(scene_id → last-nudged) field on `GatewayHandler`. No lock — the event loop is
single-consumer. Skip render if `now - last < window` (recommend 30–60s); always
update `last`. Per-connection state; resets on reconnect (acceptable).

#### `internal/telnet/gamenotice/*.go` (NEW utility) — no role-exact analog

Net-new **Go** primitive (research landmine: `CommLine`/WEBPORT-03 is web-only TS,
not reusable here). Keep it a pure string transform: `[>GAME: <msg>]` leader,
reusable across notice types (activity / idle / invite). No I/O, no DB. Table-driven
unit tests per notice type. Echoes the `>holomush_` wordmark (D-03).

---

### Slice B — mute/prefs (store + telnet command + web typed slice)

#### `plugins/core-scenes/commands.go` — `mute`/`unmute` subcommand (command, request-response)

**Analog:** `end`/`pause`/`resume` gated subcommands. Add cases in the dispatch
switch (`commands.go:479-538`) mirroring:

```go
case "end":
    return gated("end", "end", sceneResourceRef, p.handleEnd)
case "pause":
    return gated("pause", "pause", sceneResourceRef, p.handlePause)
```

New: `case "mute": return gated("mute", "mute", sceneResourceRef, p.handleMute)` and
symmetric `unmute`. Update the two usage strings + the `default` known-subcommands list.

**`gated` helper** (`commands.go:463-477`) — reuse verbatim; it fails closed when
`p.evaluator == nil` and routes through `pluginsdk.GatedSubcommand{}.Run(...)`.

**ABAC (research-confirmed):** mute/unmute are engine-`Evaluate` actions like
`end`/`pause` — **do NOT touch `validActions`/`validResourceTypes` in
`internal/command/types.go`** (Reconciliation Landmine). Add a DSL policy only.

Reuse `normalizeSceneID`/`resolveSceneRef` for the `#X` arg (ASVS V5).

#### `plugins/core-scenes/plugin.yaml` — mute/unmute DSL policy (config)

**Analog** — participant-gated policies (`plugin.yaml:273-314`):

```text
permit(principal is character, action in ["end"], resource is scene) when { resource.scene.owner == principal.id };
permit(principal is character, action in ["resume"], resource is scene) when { principal.id in resource.scene.participants ... };
```

New: `permit(principal is character, action in ["mute"], resource is scene) when { principal.id in resource.scene.participants };`
(fail-closed/default-deny; mute is a per-participant control, so gate on membership,
not ownership). Same for `unmute`.

#### `plugins/core-scenes/store.go` + migration (store, CRUD)

**Migration analog** (`000010_participants_observer_role.up.sql`) — SPDX header,
`IF EXISTS`/`IF NOT EXISTS`, paired `.down.sql`:

```sql
-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2026 HoloMUSH Contributors
ALTER TABLE scene_participants
    DROP CONSTRAINT IF EXISTS scene_participants_role_check;
ALTER TABLE scene_participants
    ADD CONSTRAINT scene_participants_role_check
    CHECK (role IN ('owner', 'member', 'invited', 'observer'));
```

**New table (research Open Q2 recommendation — D-05 seam):** one
`scene_notify_prefs(character_id, scene_id NULL, muted bool, mode text DEFAULT 'realtime')`.
A NULL `scene_id` row = per-character global notify pref; a non-NULL row = per-scene
mute. The `mode` column (`realtime`|`digest`) is the D-05 digest seam — ships now,
default `realtime`, no future migration. Confirm cardinality against `store.go`
query ergonomics during planning (Assumption A5).

**Row/scan convention** (`store.go:41-74`): nullable columns → pointer fields
(`*string`, `*int`); timestamps → `pgnanos.Time`. Scanner is a flat
`scanner.Scan(&row.Field, ...)` matching columns 1:1.

#### Web typed slice (D-04) — mirror `WebCreateScene` end-to-end

**Research verdict: mirror `membershipFlow.ts` (invite/kick toggle), NOT `createFlow`**
— mute is a structural write on an existing resource referenced by id.

- **BFF handler** `internal/web/scene_handlers.go` — copy `WebCreateScene` (`:149-178`):
  read token via `req.Header().Get(headerInjectSessionToken)`, `nil`-check
  `h.sceneAccess`, `context.WithTimeout(ctx, rpcTimeout)`, forward, and
  `//nolint:wrapcheck // gRPC status errors pass through as-is` on the error return.
  Log via `errutil.LogErrorContext`.
- **Facade** `internal/grpc/client.go` — copy the `CreateScene`/`EndScene` shape (`:419-436`):

  ```go
  func (c *Client) MuteScene(ctx context.Context, req *sceneaccessv1.MuteSceneRequest) (*sceneaccessv1.MuteSceneResponse, error) {
      resp, err := c.sceneAccessClient.MuteScene(ctx, req)
      if err != nil {
          return nil, oops.Code("RPC_FAILED").With("method", "MuteScene").Wrap(err)
      }
      return resp, nil
  }
  ```

- **Proto** `web.proto` (`WebMuteScene`) + `sceneaccess/v1` (`MuteScene`/`SetSceneNotifyPref`):
  mirror `WebCreateScene`/`CreateScene`. Every proto element needs a Go-grounded doc
  comment (`.claude/rules/proto-doc-comments.md`); run `task proto && task web:generate`
  and commit the generated `*.pb.go` + `*_pb.ts` in the same change.
- **Client** `web/src/lib/scenes/notifyFlow.ts` — mirror `membershipFlow.ts` request/response
  shape + error handling; transport via `client.ts`. NEVER `sendCommand` (gateway-boundary anti-pattern).

---

### Slice C — idle timeout (SCENEFWD-02 polish)

#### `plugins/core-scenes/idle_scheduler.go` (NEW service, batch sweep)

**Analog:** `plugins/core-scenes/publish_scheduler.go` — copy the whole shape:

```go
type idleScheduler struct {
    svc      *SceneServiceImpl
    store    sceneIdleStore
    interval time.Duration
    now      func() time.Time  // injected for deterministic tests
}

func (s *idleScheduler) Run(ctx context.Context) {
    ticker := time.NewTicker(s.interval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            if err := s.sweep(ctx); err != nil {
                errutil.LogErrorContext(ctx, "idle scheduler sweep failed", err)
            }
        }
    }
}
```

`sweep` mirrors `publishScheduler.sweep`: `nowNs := s.now().UnixNano()` (carry the
`// pgnanos-exempt: scheduler clock ...` comment), query expired rows via a narrow
store interface, per-row transition, **WARN-log per-row failures without aborting the
batch**, wrap scan errors with `oops.Code("SCENE_...").Wrap(err)`.

**Store method** (`store.go`): `ListScenesIdlePastThreshold(ctx, nowNs)` filtered on
`state='active' AND last_activity_ms + idle_timeout_secs*1000 ≤ now`. Reuse the
existing `last_activity_ms` (already computed for the board — `store.go:1772`), and
`IdleTimeoutSecs *int` nullable column (`store.go:68`). Wire a game-wide default via
the `config:` block (`plugin.yaml:37`) with the per-scene column as override.

**Transition:** use existing `IsValidTransition(active, paused)` (`lifecycle.go:22`, already valid).

**Idle nudge (OFF by default):** `scene_idle_nudge` is ALREADY declared in both
`phase4EmitTypes()` (`main.go:186`) and `plugin.yaml:113,177` — **do NOT re-add**
(Pitfall 3 / INV-PLUGIN-32 set-equality). Add ONLY the emitter call, behind a
game/scene flag defaulting OFF; render through the `gamenotice` primitive (D-03).

#### `plugins/core-scenes/main.go` (modify — wire scheduler + emitter)

Register `idleScheduler.Run(ctx)` alongside the existing `publishScheduler` startup;
add the emitter call inside the sweep. Leave all `crypto.emits`/`EmitRegistry`
declarations untouched.

---

### Slice D — telnet edge cases (SCENEFWD-03)

#### `plugins/core-scenes/commands.go:890` — mixed-render branch (D-07)

**Analog:** the existing 5-branch switch (`commands.go:892-908`). Insert a case
BEFORE `default` for both-non-empty (the current TODO at `:890`):

```go
case len(afResult.FocusedConnectionIDs) > 0 && len(afResult.SkippedConnectionIDs) > 0:
    msg = fmt.Sprintf("Joined scene #%s and focused some connection(s); "+
        "others stay on their current focus (use 'scene focus #%s').", sceneID, sceneID)
```

Keep the failure-first ordering (`FailedConnectionIDs > 0` stays the first case).
Delete the `// TODO(Phase 6 §7.4)` comment.

#### `internal/grpc/server.go:~878` — wire `RestoreConnectionFocus` (D-08)

**The primitive is already built + tested** (`internal/grpc/focus/restore_connection_focus.go`;
`test/integration/scenes/reconnect_focus_restoration_test.go`, INV-SCENE-18/25/26) —
**zero production callers**. This is a wiring task.

Call site: right after `AddConnection` succeeds (`server.go:878`), before/near the
`streamRegistry.RegisterConnection` block (`:895`). The fresh `Connection{}` (`:861-867`)
sets `FocusKey` unset — restore repopulates it from `Info.PresentingFocus`.

**Gate (Pitfall 5 / Assumption A2):** only when `PresentingFocus != nil` — restore is
a documented no-op otherwise (branch 1 in `RestoreConnectionFocus`), which keeps web
per-tab focus safe. Verify with a web-tab integration test before finalizing.
Run `task test:int` after touching session/focus/Subscribe shapes (Pitfall 6).

#### Multi-character per connection (D-09) — model pending (Open Q3)

Substrate binds one active character per telnet connection (`charName` +
`connectionID`). Likely gap: clearing a stale scene `FocusKey` when a connection
swaps characters (logout→re-pick). Sibling test: `multi_connection_visibility_test.go`.
Planner must pin the exact scenario (character-switch vs simultaneous) against
`2026-05-21-scenes-phase-5-focus-model...` spec before implementing.

## Shared Patterns

### ABAC (Layer-2 Evaluate) — new telnet commands

**Source:** `commands.go:463-477` (`gated` helper) + `plugin.yaml:273-314` (participant policies)
**Apply to:** `scene mute`/`unmute`. Two-layer: Layer-1 command-execution gate
(`execute-scene-commands` policy already covers `scene`, `plugin.yaml:254`); Layer-2
`Evaluate("mute", "scene:"+id)` with a new participant-gated DSL policy. Fail-closed.
**Do NOT** register in `validActions` (research-confirmed).

### Error handling + logging

**Source:** `publish_scheduler.go` (`oops.Code(...).Wrap(err)`, `errutil.LogErrorContext`,
`slog.WarnContext` per-row), `scene_handlers.go:149-178` (`//nolint:wrapcheck` on gRPC
pass-through)
**Apply to:** all new Go files. Context-carrying log variants everywhere `ctx` is in
scope (`.claude/rules/logging.md`). Never leak inner errors past the gRPC boundary
(`.claude/rules/grpc-errors.md`).

### Typed BFF write (never command path)

**Source:** `WebCreateScene` (`scene_handlers.go:149`) → `CreateScene` facade
(`client.go:421`) → `SceneAccessService`
**Apply to:** all web mute/prefs writes (`gateway-boundary.md` structural-writes rule).

### Deterministic ticker sweep

**Source:** `publish_scheduler.go` (`Run`/`sweep`, injected `now func() time.Time`,
per-row failure tolerance)
**Apply to:** `idle_scheduler.go`.

### Invariant registration

New INV-SCENE ids (next free per research: **INV-SCENE-70**). Candidates: INV-SCENE-70
"telnet SCENE_ACTIVITY nudge carries no scene content" (privacy parity), and an idle-
transition guarantee. Register in `docs/architecture/invariants.yaml` (`binding: pending`
until tests land) + regenerate `invariants.md` in the same change (`.claude/rules/invariants.md`).
INV-SCENE-62 (FocusMemberships ⊆ participants) MUST NOT break when extending render to telnet.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `internal/telnet/gamenotice/*.go` | utility/renderer | transform | No existing Go notification-leader primitive. `CommLine` (WEBPORT-03) is web-only TS and NOT reusable. Structural analog = telnet `send`/`formatEvent` sink; build as a pure string primitive. |

## Metadata

**Analog search scope:** `internal/telnet`, `internal/web`, `internal/grpc`,
`internal/grpc/focus`, `plugins/core-scenes` (+ migrations), `web/src/lib/scenes`.
**Files scanned:** ~12 (targeted reads at research-verified `path:line`).
**Pattern extraction date:** 2026-07-08
