<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Focus-Routed Scene Input — Design

**Bead:** holomush-g1qcw · **Date:** 2026-07-05 · **Status:** design (self-reviewed)
· **Theme:** `theme:social-spaces`

**Blocker (shipped):** the communication-content contract
(`holomush.comm.v1.CommunicationContent`, epic holomush-kk1ot) merged as PR #4571.
With a single content shape both runtimes emit, routing a pose into a scene now
changes only the NATS subject, not the payload shape — so the "flattening"
limitation this work would otherwise have shipped no longer arises.

The keywords **MUST**, **MUST NOT**, **SHOULD**, **SHOULD NOT**, and **MAY** are
to be interpreted as described in RFC 2119.

## 1. Context and Problem

A connection "in a scene" — telnet, web terminal, or web portal — types a plain
ambient conversational verb (`pose bows`, `:bows`, `"hi`, `ooc brb`) and expects
it to reach their **focused scene's** IC/OOC stream, not the grid location, and
without the leading sigil (`:`/`;`/`"`) leaking into content.

Scene *subcommands* already honor focus: `handleEmit`
(`plugins/core-scenes/commands.go:1248`) reads `GetConnectionFocus`, uses
`fk.TargetID` when `Kind == scene`, and falls back to single-membership
inference otherwise. The gap is **top-level** `pose`/`say`/`ooc`/`emit`: these
are handled by core-communication and hardwire to the character's location —
they never consult `Connection.FocusKey`. That asymmetry is the root cause of
the sigil-leak bug holomush-5rh.32 and forces web clients to string-build
`scene pose …` wrappers.

scenes-v2 §3.2
(`docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md:230-234`) already
specifies the intended behavior: *"When a connection sends input, the dispatcher
checks that connection's focus"* and routes to
`events.<game_id>.scene.<scene_id>.ic` (or `.ooc`) when scene-focused, else the
grid location. This design realizes §3.2 for top-level verbs.

## 2. Goals and Non-Goals

**Goals:**

- A scene-focused connection's top-level `pose`/`say`/`ooc`/`emit` (including
  the `:`/`;`/`"` sigil aliases) MUST route to the focused scene's IC/OOC
  stream, identically across telnet, web terminal, and web portal.
- Non-scene-focused connections MUST retain today's location routing
  (back-compat).
- The core dispatcher MUST remain plugin-agnostic: it MUST NOT hardcode the
  `scene` command name or the scene verb set.
- The web `SceneComposer` MUST send raw conversational input rather than a
  string-built `scene pose …` wrapper.

**Non-Goals (out of scope):**

- The rendering seam (holomush-c5zol / holomush-5rh.33) — ships separately.
- Web focus UX beyond the composer (Phase 9 breadth).
- Client-side sigil stripping in the UI — explicitly **rejected** (§9).
- Targeted comms (`page`/`whisper`/`pemit`) — these are single-recipient, not
  ambient, and are never redirected to a scene (kk1ot Slice 2 family).

## 3. Grounding

Every mechanism below is grounded in current code:

| Fact | Location |
| --- | --- |
| `ConnectionID` already threads to dispatch + plugin `CommandRequest` | `internal/command/dispatcher.go`; `TestDispatcher_PassesConnectionIDToPluginCommand`, `TestHandleCommand_ConnectionIDThreadedToExecution` |
| Scene subcommands already honor focus (reference pattern) | `plugins/core-scenes/commands.go:1248` (`handleEmit`) |
| Scene subcommand routing (`scene pose` → `handleEmit`) | `plugins/core-scenes/commands.go:519-526` |
| ABAC `write-scene-as-participant` gate (non-participant → explicit error) | `plugins/core-scenes/commands.go:1285` |
| Alias resolution STRIPS the sigil into `invokedAs` | `internal/command/alias.go:357-361` |
| `comm.Pose(author, invokedAs, raw)` derives no-space from `invokedAs` | `pkg/plugin/comm/builder.go:46`, `pkg/plugin/comm/grammar.go:30` |
| `invokedAs` for `;waves` survives as `";"` | `internal/command/dispatcher_test.go:1396` |
| Dispatcher construction (functional options) | `internal/command/dispatcher.go:97` (`NewDispatcher`) |
| Loader command-registration seam | `internal/plugin/setup/subsystem.go:415` (`RegisterPluginCommands`) |
| Per-connection focus is host state, needs a lookup by connID | `internal/session/session.go:270` (`Connection.FocusKey`, INV-SCENE-15) |
| Web sets routing focus on scene selection | `web/src/lib/scenes/workspaceStore.svelte.ts:165` (`await setSceneFocus(...)`); wrapper `web/src/lib/scenes/client.ts:112-118` |
| Web terminal chip is already a pure preview | `web/src/lib/components/terminal/composerChip.ts:19-47` (design INV-4) |
| Scene Board composer string-builds `scene <verb>` | `web/src/lib/components/scenes/SceneComposer.svelte:62` (`sendSceneCommand`) |
| core-communication registers top-level `say`/`pose`/`ooc`/`emit` | `plugins/core-communication/plugin.yaml` |

No external dependencies are in scope (pure internal Go dispatcher + Svelte
composer); context7/deepwiki grounding is N/A.

## 4. Architecture

Two parts, one PR. Part 1 (server) is the substrate; Part 2 (web) depends on it.

### 4.1 Manifest-declared `focus_redirects` (core-scenes)

core-scenes declares the redirect in its manifest — core owns no scene/verb
knowledge:

```yaml
# plugins/core-scenes/plugin.yaml
focus_redirects:
  - focus_kind: scene
    verbs: [pose, say, ooc, emit]
    target_command: scene
```

Semantics: *"when a connection's `FocusKey.Kind == scene`, a top-level `pose`
(etc.) MUST be redirected to my `scene pose` (etc.) handler."*

- A new `FocusRedirect` type MUST be added to `internal/plugin.Manifest`
  (`internal/plugin/manifest.go`), parsed by `ParseManifest`, and validated
  against `schemas/plugin.schema.json`.
- Validation MUST reject: an unknown `target_command` (no such registered
  command), an empty `verbs` list, and an unrecognized `focus_kind`.
- `focus_redirects` is OPTIONAL; plugins that omit it are unaffected.

This keeps the redirect a plugin-owned concern, consistent with the
event-conventions rule ("plugin-owned … belong in the plugin package — NEVER in
`internal/core/`") and gateway-boundary / plugin-ownership discipline.

### 4.2 Loader → redirect table

At the `RegisterPluginCommands` seam (`internal/plugin/setup/subsystem.go:415`),
the loader MUST collect every plugin's `focus_redirects` into a generic,
plugin-agnostic table. It MUST be keyed **verb-first** so the dispatcher can
gate its focus read behind a cheap verb lookup (§4.3):

```text
map[verb]map[FocusKind]targetCommand
```

The table MUST be handed to the dispatcher via a new
`command.WithFocusRedirects(table)` option. If two plugins declare a redirect
for the same `(focus_kind, verb)` pair, the loader MUST fail closed at startup
(ambiguous redirect), mirroring the existing duplicate-provider discipline.

### 4.3 Dispatcher redirect

In `Dispatcher.Dispatch` (`internal/command/dispatcher.go`), the redirect fires
immediately **after** parse (`:167`) and **before** the first read of
`parsed.Name` for telemetry and rate limiting (`metrics.SetCommandName` `:173`,
the trace span attributes `:176-189`, `rateLimiter.Enforce` `:198-205`) — so
metrics, tracing, and rate-limit labels all observe the **effective** (routed)
command. Placement is a telemetry-labeling concern only, not a security one: the
rate-limit allow/deny decision keys on `exec.SessionID()`
(`internal/command/rate_limit_middleware.go`), not the command name, and the
Layer-1 `execute` authorization check (`:220`) runs after the rewrite so it
correctly gates `command:scene` (see "Full re-authorization" below):

```text
if targets, ok := d.focusRedirects[parsed.Name]; ok {          // only redirect-candidate verbs reach here
    kind := d.focusReader.ConnectionFocusKind(ctx, exec.ConnectionID())  // LAZY read
    if tgt, ok := targets[kind]; ok {                          // e.g. kind == scene
        parsed = Parse(tgt + " " + parsed.Name + " " + parsed.Args)      // -> "scene pose bows"
        // invokedAs is LEFT UNTOUCHED  (see §4.4)
    }
}
```

Key properties:

- **Lazy focus read.** The focus lookup MUST happen only when `parsed.Name` is a
  redirect candidate — the vast majority of commands never touch it. Focus is
  read through an injected narrow `FocusReader` interface
  (`ConnectionFocusKind(ctx, connID) (session.FocusKind, error)`), configured
  via a new `command.WithFocusReader(fr)` option that mirrors the existing
  `WithAliasCache` / `WithPluginDeliverer` / `WithRateLimiter` pattern. The
  adapter wraps the existing host read
  `GetConnectionFocus(ctx, connID) (*session.FocusKey, error)`
  (`internal/grpc/focus/get_connection_focus.go`). Because `session.FocusKind`
  today defines only `FocusKindScene` and grid/no-focus is a **nil**
  `*session.FocusKey` (`internal/session/session.go:270`), the adapter MUST map a
  nil focus (grid) to the zero value `session.FocusKind("")`, which the
  verb-keyed `targets[kind]` lookup correctly no-ops on. A vanished connection
  (`CONNECTION_NOT_FOUND`) also maps to absent focus, but a genuine read error
  MUST propagate — the dispatcher fails closed on it (§4.5, as revised by
  holomush-uprtc). The dispatcher never needs a non-scene `FocusKind` constant.
- **Kind-only decision.** The dispatcher needs only the focus *kind*; the target
  scene is re-derived by `handleEmit` from its own `GetConnectionFocus` read
  (`commands.go:1248`). The dispatcher MUST NOT resolve or pass the scene ID.
- **Full re-authorization.** After the rewrite, the redirected command flows
  through the normal `scene`-command dispatch path — Layer-1 `execute`
  (`command:scene`) check, capability preflight, and the plugin's own ABAC gate.
  This is correct: a redirected command IS a scene command and MUST be
  authorized as one.
- **No redirect loop.** The rewrite target (`scene …`) is not itself a redirect
  verb, and alias resolution is top-level-only, so the rewritten command MUST
  NOT re-enter the redirect branch.

### 4.4 The `invokedAs` / no-space preservation catch

This is the correctness hazard of the design — the direct analog of kk1ot's
`actor_id` preservation catch.

Alias resolution **strips** the sigil into `invokedAs`: `:waves` resolves to
`pose waves` with `aliasUsed = ":"` (`internal/command/alias.go:357-361`). For a
bare-sigil semipose (`;waves`), the no-space bit therefore lives **only** in
`invokedAs`, not in the resolved text. `comm.Pose(author, invokedAs, raw)`
derives `NoSpace` from `invokedAs` when the raw text carries no sigil
(`pkg/plugin/comm/grammar.go:30`).

Therefore the redirect MUST rewrite the command **name and args** but MUST leave
`invokedAs` untouched. Because the dispatcher tracks `invokedAs` separately
(`dispatcher.go:150,159-160`) and it flows to `CommandRequest.InvokedAs`
(`dispatcher_test.go:1396` proves `;` survives), the redirected `scene pose`
reaches `handleEmit` with `req.InvokedAs == ";"`, so
`comm.Pose(author, ";", "waves")` yields `NoSpace == true` — byte-identical to
the top-level path.

A naive string-rewrite that discarded `invokedAs` would silently drop no-space
for `;`-semiposes — precisely the sigil-class regression this work exists to
eliminate. Tests MUST cover `pose`, `:`, `;`, and `"` through the redirect.

### 4.5 Failure semantics

> **Revised 2026-07-06 (holomush-uprtc).** The original contract failed OPEN
> to location on a `FocusReader` infra error. That routed a scene-focused
> player's participant-only encrypted content (INV-SCENE-3, sensitivity:
> always) to the plaintext location stream with no user-facing notice — an
> unrecoverable confidentiality downgrade. The infra-error row now fails
> CLOSED; INV-SCENE-67 pins it. A vanished connection (`CONNECTION_NOT_FOUND`)
> is genuine no-focus, not an infra error, and stays on the no-focus row.

| Condition | Behavior |
| --- | --- |
| No focus / grid focus / connection vanished | No redirect → location handler (unchanged, back-compat) |
| `FocusReader` infra error | **Fail-closed** — abort dispatch with `FOCUS_READ_FAILED`; the player is told the message was **not sent** and to retry. The command MUST NOT be routed to any handler |
| Scene focus, participant | Redirect → scene IC/OOC stream |
| Scene focus, non-participant / stale | Redirect fires; `handleEmit`'s `write-scene-as-participant` ABAC gate (`commands.go:1285`) returns an **explicit** permission error |

The fail-closed-on-infra-error / explicit-error-on-non-participant split is
deliberate: a transient focus-store hiccup MUST surface as a retryable,
player-visible delivery failure rather than silently broadcasting the pose to
the wrong (plaintext) audience, and a genuine authorization failure MUST be
surfaced as a permission error. A dropped pose is a retry; a leaked pose is
unrecoverable. The UX cost of fail-closed (a store blip errors the ambient
verbs) shrinks to near zero once the focus read is served from memory
(holomush-wm0fi).

### 4.6 Web (Part 2)

Two distinct web surfaces reach the command path. Part 1 (§4.3) makes both
symmetric with telnet; the goal here is to remove the last surface that shapes
commands client-side.

**Web terminal (`CommandInput.svelte` + `composerChip.ts`) — no code change.**
It already sends the player's raw input through the conversational command path,
and its recognition chip (`resolveComposerChip`,
`web/src/lib/components/terminal/composerChip.ts:19-47`) is **already** a pure
preview: it maps text → `{kind, label}` for display and never mutates the
submitted text (design INV-4). A scene-focused terminal `pose` / `:bows` is
therefore routed by the Part-1 redirect with **no web change** — this surface is
verify-only (an integration/E2E assertion, not new code).

**Scene Board (`SceneComposer.svelte`) — drop the `scene` prefix.** The composer
today string-builds `scene ${verb} ${text}`
(`web/src/lib/components/scenes/SceneComposer.svelte:62`, via `sendSceneCommand`
→ `client.sendCommand`). It MUST instead send the **raw** conversational verb
`${verb} ${text}` (e.g. `pose bows`) and rely on the Part-1 redirect, so it uses
the identical conversational path as telnet and the web terminal. The three
explicit Pose/Say/OOC buttons stay — they select the verb, which is a legitimate
UX affordance, not command parsing — and a leading sigil in the text is still
honored server-side by `ParsePose`. This removes the **last** surface-specific
command-shaping wrapper: the same class of anti-pattern the UI-side
sigil-stripping alternative is rejected for (§9). Routing and verb-shaping belong
server-side, on one path, for every surface.

**Focus-before-send gate.** `workspaceStore.select()` sets `selectedSceneId`
(`web/src/lib/scenes/workspaceStore.svelte.ts:153`) — which renders/enables
`SceneComposer` — **before** it awaits `setSceneFocus`
(`workspaceStore.svelte.ts:165`, wrapper `web/src/lib/scenes/client.ts:112-118`).
Because the composer now relies on the server redirect rather than an explicit
`scene` target, a send in that sub-100ms window would find no focus set and
route to the grid location (the genuine no-focus row of §4.5 — not the
infra-error row). The composer MUST therefore gate sends until the
selected scene's `setSceneFocus` write has resolved — e.g. a per-scene
focus-ready flag on the workspace store that disables the Pose/Say/OOC buttons
and the ⌘↵ shortcut until focus is confirmed. This closes the window that the old
`scene <verb>` wrapper masked via `handleEmit`'s single-membership fallback.

**Symmetric failure is intended.** After the change, every surface degrades
identically: on a focus-read infra error the dispatch aborts with a retryable
delivery error (§4.5, revised) for telnet, terminal, and Scene Board alike. The Scene Board
gives up its bespoke `scene pose` fallback (single-membership inference) — which
in a multi-scene workspace usually could not resolve a target anyway — in
exchange for uniform, predictable behavior across surfaces. That uniformity is
the objective, not a regression.

**Sequencing:** the SceneComposer change depends on Part 1 — raw `pose` without
the redirect would route to the grid location — so Part 1 MUST land at or before
Part 2. Both ship in the same PR, server first. The web-terminal surface needs no
code, only a verifying test.

## 5. Invariant

This design introduces one system-behavior guarantee, registered as
**INV-SCENE-66** (added to `docs/architecture/invariants.yaml` as
`binding: pending` when this spec was finalized; since `bound`). The
holomush-uprtc revision (§4.5) later registered a second, **INV-SCENE-67**
(fail-closed on focus-read error; `binding: bound`, ADR holomush-pbp9j):

> A scene-focused connection's ambient conversational verbs
> (`pose`/`say`/`ooc`/`emit`, including the `:`/`;`/`"` sigil aliases) route to
> the focused scene's IC/OOC stream and never leak to the grid location; the
> leading sigil never reaches content, and no-space/OOC-style semantics are
> preserved across the redirect.

The implementation plan MUST bind it (`binding: bound`) to the dispatcher
redirect tests that assert scene-focused rewrite + sigil/no-space preservation +
grid/no-focus fall-through.

## 6. Testing

- **Dispatcher unit** (`internal/command/dispatcher_test.go`): scene-focused →
  rewrites to `scene <verb>`; grid-focused / no-focus → no rewrite (location);
  `FocusReader` error → dispatch aborts with `FOCUS_READ_FAILED`, no handler
  reached (§4.5 as revised by holomush-uprtc); `pose`/`:`/`;`/`"` preserve
  no-space and OOC style through the redirect (asserted via the delivered
  `CommandRequest`).
- **Manifest** (`internal/plugin/manifest_test.go`): `focus_redirects` parses;
  validation rejects unknown `target_command`, empty `verbs`, bad `focus_kind`;
  duplicate `(focus_kind, verb)` across plugins fails closed at load.
- **Integration** (`test/integration/…`, `//go:build integration`): a
  scene-focused telnet `pose` lands on `events.<game>.scene.<id>.ic`; a
  non-participant scene focus yields an explicit permission error; a grid-focused
  `pose` lands on the location stream.
- **Web — Scene Board**: `SceneComposer` sends raw `<verb> <text>` (no `scene`
  prefix) via `sendSceneCommand`; and the composer cannot send before the
  selected scene's `setSceneFocus` has resolved (focus-before-send gate — no send
  races ahead of focus).
- **Web — terminal (verify-only)**: an integration/E2E assertion that a
  scene-focused terminal `pose` / `:bows` lands on the scene IC/OOC stream
  (proving Part 1 covers this surface with no web change), plus a regression
  guard that `resolveComposerChip` stays a pure preview (never mutates submitted
  text).

Per-package unit runs plus `task test:int` (the redirect crosses the
plugin-command boundary, so integration coverage is required).

## 7. Sequencing and Scope

Single PR, two parts, server first (§4.6). No migration is required —
`focus_redirects` is additive and optional; existing plugins and non-focused
connections are unaffected.

## 8. Open Questions (resolved during brainstorming)

1. **Multi-scene "which wins"** — resolved structurally. A command arrives on a
   specific `ConnectionID`, which has exactly one `FocusKey` (INV-SCENE-15).
   The originating connection is the answer; no tiebreak is needed.
   Session-scoped `PresentingFocus` is used only for restore-on-reconnect, not
   routing.
2. **Actor identity per alt** — resolved. Each connection maps to one session →
   one `CharacterID`; actor is `exec.CharacterID()`, unambiguous per connection.
   Alts are separate sessions.
3. **Failure split** — resolved as §4.5 (infra error → fail-closed abort per
   the holomush-uprtc revision; non-participant → explicit ABAC error).

## 9. Rejected Alternatives

- **Dispatcher-owned (scene-specific) redirect** — the core dispatcher directly
  knowing "scene focus → reroute ambient verbs to the `scene` command." Rejected:
  it bakes plugin-specific knowledge (the `scene` command name + verb set) into
  `internal/command`, the exact coupling gateway-boundary and event-conventions
  forbid elsewhere. The manifest-declared table (§4.1) keeps core plugin-agnostic
  and extends to future focus kinds (e.g. channel focus) with no core change.
- **Stamp focus on `exec` (mirror `ConnectionID`)** — rejected in favor of the
  lazy injected `FocusReader` (§4.3): stamping forces an eager per-connection
  focus read for every command, when only the four redirect verbs ever need it.
- **Client-side sigil stripping** (`parseComposerInput` in the UI) — rejected:
  pushes command parsing into the gateway boundary, stays asymmetric with telnet,
  and papers over the missing server routing.
<!-- adr-capture: sha256=1268666ccd4f49d2; session=cli; ts=2026-07-05T14:52:02Z; adrs=holomush-4u3qe,holomush-11488 -->
