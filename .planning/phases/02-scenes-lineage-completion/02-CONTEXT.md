# Phase 2: Scenes Lineage Completion - Context

**Gathered:** 2026-07-08
**Status:** Ready for planning

<domain>

## Phase Boundary

Extend the already-shipped `core-scenes` binary plugin to the remainder of its
designed scope — **scene-activity notifications + controls** (SCENEFWD-02) and
**telnet command edge-case hardening** (SCENEFWD-03). No new subsystem, no new
plugin: every mechanical decision mirrors the *current* `core-scenes` substrate,
exactly as Phase 1 (Channels) mirrored it.

**In scope:**

- **Notifications (SCENEFWD-02):** a telnet notification surface for non-focused
  scene members (web badges already ship — ADR `holomush-0qnnr`); a reusable
  `[>GAME: …]` telnet notification leader; `scene mute`/`unmute` telnet commands
  + per-character notify pref; a web mute/prefs UI (4-layer slice); idle-timeout
  defaults + transitions + optional idle nudge.
- **Telnet edge cases (SCENEFWD-03):** the mixed focused/skipped auto-focus
  render branch (`commands.go:890` TODO); reconnection restoring scene
  membership + focus; multiple characters on one telnet connection.

**Out of scope (this phase):**

- **Scene templates (SCENEFWD-01)** — DESCOPED to backlog (D-01; bd
  `holomush-x4n1r`, P4).
- **Digest (batched) notification delivery** — realtime only this phase; digest
  behind a seam (D-05).
- **Persisted cross-session read-markers** for badges — already a deferred
  follow-up per ADR `holomush-0qnnr`.

</domain>

<decisions>

## Implementation Decisions

### Scope

- **D-01 [deferred] (templates descoped):** SCENEFWD-01 (scene templates) is **removed from
  Phase 2 and returned to backlog**. It is already a standalone P4 backlog item
  (bd `holomush-x4n1r`, lifted out of the scenes epic 2026-07-03), so no new
  bead is needed. `ROADMAP.md` + `REQUIREMENTS.md` were updated in the **same
  session** to reflect a 2-requirement Phase 2 (SCENEFWD-02, SCENEFWD-03).
  User rationale: templates are not actively desired/pursued at this time.
  Phase 2's success criteria drop the template criterion accordingly.

### Notification surface & delivery (SCENEFWD-02)

- **D-02 (telnet nudge, reuse existing delivery):** A non-focused **telnet**
  member receives a **throttled/coalesced** scene-activity nudge, delivered via
  the **existing subscription-router downgrade path** — the same mechanism ADR
  `holomush-0qnnr` already uses for web unread badges
  (`ControlFrame{CONTROL_SIGNAL_SCENE_ACTIVITY, scene_id}`, no payload, no
  decryption). This **extends already-shipped delivery infra to the telnet
  gateway's rendering**; it does **NOT** introduce a new notification stream or
  subject (which the master spec §3.3 originally proposed but ADR `holomush-0qnnr`
  superseded). Makes SC #2 true on telnet, not just web.
- **D-03 (`[>GAME …]` leader is a reusable primitive):** Telnet system nudges
  render with a **shared, reusable** `[>GAME: <msg>]` leader — one telnet
  rendering primitive for **all** game-originated notices (scene activity, idle
  nudge, and future channel/system notices). Scenes wire it this phase; other
  subsystems reuse it later. Echoes the `>holomush_` wordmark. It **MUST** be a
  shared primitive, not a `core-scenes`-local string. The exact rendering
  seam/placement is a research question (see Discretion).

### Notification controls (SCENEFWD-02) — full depth

- **D-04 (both surfaces):**
  - **Telnet:** `scene mute #X` / `scene unmute #X` (new — they do not exist
    today) + a per-character notify on/off preference. Persisted.
  - **Web:** a mute/prefs UI as a **4-layer typed slice** (`SceneAccessService`
    facade → `WebMuteScene`/prefs BFF RPC on `web.proto` →
    `internal/web/scene_handlers.go` → `web/src/lib/scenes/*`), built on the
    already-shipped create-scene facade→BFF→client pattern (bd `holomush-5rh.22`,
    closed). Reuse the pattern; do not race/duplicate it. Web structural writes
    go through the typed RPC, never the command path (`gateway-boundary.md`).
- **D-05 (realtime now, digest seam):** Delivery is **realtime only** this phase.
  The prefs/store model **MUST leave a seam** for a future `realtime | digest`
  per-character preference — no schema migration or prefs rewrite when digest
  lands later. Digest itself (batching + interval scheduler + pending queue) is
  deferred. Mirrors Phase 1's ship-now+seam philosophy (D-03/D-06 there).

### Idle timeout & nudge (SCENEFWD-02 polish)

- **D-06 (idle in scope, wire existing infra):** Wire the idle-timeout lifecycle
  using the **partial existing infra** — `idle_timeout_secs` column (`store.go`)
  and `scene_idle_nudge` event type (`plugin.yaml`/`main.go`); **do not
  rebuild**. Deliver: game-wide default + per-scene override, auto-transition
  `active → paused` on idle, plus the **optional idle nudge (spec §4.4, OFF by
  default)** rendered through the `[>GAME: …]` leader (D-03).

### Telnet edge cases (SCENEFWD-03) — all three land

- **D-07 (mixed render branch):** Close the `commands.go:890` TODO — add the
  render branch for when auto-focus-on-join produces **both** focused and skipped
  connections (only single-outcome branches exist today). Smallest, directly
  cited by SCENEFWD-03.
- **D-08 (reconnection restore):** On telnet reconnect, restore the character's
  scene **memberships AND focus state** (spec §11). Builds on the already-landed
  live-delivery fixes (bd `holomush-66228`, `holomush-ymgjs`, both closed) +
  session-liveness/gateway-survival (AUTHSESS-03) + the focus store.
- **D-09 (multi-character per connection):** Handle multiple characters bound to
  a single telnet connection cleanly (focus routing + render targeting). Spec §11
  "needs deeper design."

### Claude's Discretion

- **Nudge throttling/coalescing policy** (per-scene rate, debounce window) — tune
  so a busy scene does not emit one nudge per pose.
- **`[>GAME: …]` rendering seam/placement** — gateway `forwardFrame` vs the shared
  `CommLine` rendering primitive (WEBPORT-03) vs verb-registry. The constraint is
  "shared reusable primitive," not the wiring; research picks the seam.
- **Exact `[>GAME: <msg>]` wording per notice type** (activity / idle / invite)
  and whether the line shows `#id` or the scene title.
- **Store shape** — whether per-character notify pref and per-scene mute share one
  table or two (subject to the D-05 digest seam).
- **Plan sequencing** of reconnection-restore (D-08) vs multi-char (D-09) if they
  must split across plans — both are spec §11 "needs deeper design."

</decisions>

<canonical_refs>

## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Master design (governs both requirements — reconcile against current substrate)

- `docs/superpowers/specs/2026-04-06-scenes-and-rp-design-v2.md` — the scenes
  master spec. **Notifications §3.3**; **telnet edge cases + idle §11**; **idle
  nudge §4.4**. (Templates §1.4/6.5/7.2 are now OUT — D-01.) Predates several
  substrate shifts — see Reconciliation Constraint below; the §3.3
  notification-stream approach in particular is **superseded** by ADR
  `holomush-0qnnr`.

### Notification delivery — ALREADY SHIPPED; extend, do not rebuild

- `docs/adr/holomush-0qnnr-scene-activity-uses-consumer-control-frame-downgrade-not-new.md`
  — THE delivery mechanism this phase extends to telnet. Non-focused scene events
  downgrade to `ControlFrame{CONTROL_SIGNAL_SCENE_ACTIVITY, scene_id}`; snapshot
  catch-up via `WebListMyScenes`; **INV-SCENE-62** (`FocusMemberships ⊆
  scene_participants`) holds by construction — MUST NOT break it extending to
  telnet. Consequence: `ControlFrame` carries a `scene_id` the gateway
  `forwardFrame` must propagate (the telnet rendering hook).
- `docs/superpowers/specs/2026-06-07-web-portal-scenes-design.md` §3 D7 / §9 V5 —
  badge/notification design context (bd `holomush-5rh.8`).

### Web mute/prefs UI — reuse the shipped typed slice

- `docs/superpowers/specs/2026-06-19-web-create-scene-design.md` — the
  facade→BFF→client typed-RPC pattern to mirror (bd `holomush-5rh.22`, closed).
- `.claude/rules/gateway-boundary.md` § "Structural writes use typed RPCs" —
  mute/prefs toggles are structural GUI writes → typed BFF RPC, never the command
  path.
- Integration targets: `internal/web/scene_handlers.go`, `web/src/lib/scenes/*`,
  `web.proto` (`WebMuteScene`/prefs RPC).

### `core-scenes` plugin — the code being extended

- `plugins/core-scenes/commands.go` — telnet handlers; `:890` mixed-render TODO
  (D-07); `scene mute`/`unmute` added here (D-04).
- `plugins/core-scenes/store.go` — `idle_timeout_secs` column (D-06);
  mute/prefs persistence (D-04/D-05).
- `plugins/core-scenes/plugin.yaml` + `plugins/core-scenes/main.go` —
  `scene_idle_nudge` event type (D-06); manifest verbs/commands additions.
- `plugins/core-scenes/migrations/` — plugin-owned schema; new mute/prefs
  table(s), idle defaults.

### Reconnection / focus / session (D-08, D-09)

- `docs/superpowers/specs/2026-05-30-session-liveness-and-gateway-survival-design.md`
  (AUTHSESS-03) — the reconnect substrate (D-08).
- `docs/superpowers/specs/2026-05-21-scenes-phase-5-focus-model-and-multi-connection-visibility-design.md`
  — focus-state restore + multi-connection visibility (D-08/D-09).
- bd `holomush-66228` (Focus-Delta Coordinator Unification) + `holomush-ymgjs`
  (binary-plugin scene-join live delivery) — both CLOSED; the live-delivery
  prerequisites reconnection-restore builds on.

### Wire / event conventions (same as Phase 1)

- `.claude/rules/event-interfaces.md` — Publisher/Subscriber/HistoryReader;
  JetStream-owned ordering.
- `.claude/rules/event-conventions.md` — dot-subjects, `<plugin>:<verb>` wire
  types, `core.NewEvent()`, colon eradication.

### Shared rendering seam (relevant to D-03)

- `docs/superpowers/specs/2026-06-25-shared-web-communication-seam-design.md`
  (WEBPORT-03) — the shared `CommLine` rendering primitive; the `[>GAME: …]`
  leader should live in/near a shared primitive, not a bespoke renderer.

### Invariants

- `.claude/rules/invariants.md` + `docs/architecture/invariants.yaml` —
  **INV-SCENE** scope. If notifications/idle mint a new named guarantee (e.g.
  telnet-notify privacy parity, idle-transition), register `INV-SCENE-N`
  (`binding: pending`) and regenerate `invariants.md` in the **same change**.
  INV-SCENE-62 is the privacy invariant the downgrade path preserves.

### Issue tracking

- bd `holomush-5rh.19` (Scenes Phase 10 — notifications/telnet/idle, P2) — the
  epic this phase mostly delivers; deps `5rh.18`/`5rh.22`/`66228`/`ymgjs` all
  CLOSED. Do NOT mirror the bd graph into `.planning/`.
- bd `holomush-x4n1r` (Scenes Phase 7: Templates, P4) — **DESCOPED** here, stays
  backlog (D-01).

## Reconciliation Constraint (standing — applies to every task)

The 2026-04-06 master spec is product-intent and predates substrate shifts.
Mirror the **current** `core-scenes` substrate in every mechanical decision:
**no new notification stream/subject** — extend the ADR `holomush-0qnnr`
in-consumer `ControlFrame` downgrade path (the spec §3.3 `notifications:<char>`
stream is superseded); `<plugin>:<verb>` qualified wire types; `core.NewEvent()`
for event construction; **typed BFF RPC** for web mute/prefs (never the command
path); dot-subjects (never colon streams).

</canonical_refs>

<code_context>

## Existing Code Insights

### Reusable Assets

- **ControlFrame downgrade path + subscription router** (ADR `holomush-0qnnr`) —
  the shipped non-focused-member delivery mechanism; extend its rendering to the
  telnet gateway, don't rebuild.
- **Web create-scene facade→BFF→client slice** (bd `holomush-5rh.22`) — the exact
  pattern for the web mute/prefs UI (D-04).
- **Idle infra** — `idle_timeout_secs` (`store.go`) + `scene_idle_nudge` event
  type (`plugin.yaml`/`main.go`); dormant, wire it (D-06).
- **`WebListMyScenes` snapshot** — reconnect/initial-sync catch-up; also relevant
  to telnet reconnect-restore (D-08).
- **Shared `CommLine` rendering seam** (WEBPORT-03) — where the reusable
  `[>GAME: …]` leader (D-03) should live.

### Established Patterns

- Plugin-owned Postgres schema + migrations (new mute/prefs table + idle
  defaults).
- Two-layer ABAC for new telnet commands: Layer-1 command-execution gate on
  `command:scene*` + Layer-2 per-resource check for `mute`/`unmute`.
- Typed BFF RPC for web structural writes (`gateway-boundary.md`).
- Ship-now + seam (digest, D-05) — mirrors Phase 1's D-03/D-06.

### Integration Points

- `plugins/core-scenes/{commands.go,store.go,plugin.yaml,main.go,migrations/}`.
- `internal/web/scene_handlers.go` + `web/src/lib/scenes/*` + `web.proto`
  (`WebMuteScene`/prefs RPC).
- Gateway `forwardFrame` — propagate `ControlFrame.scene_id` into telnet nudge
  rendering (D-02).
- `internal/command/types.go` — add scene `mute`/`unmute` actions/commands to
  `validActions`/`validResourceTypes` if not already present.

</code_context>

<specifics>

## Specific Ideas

- Notification leader format example: `[>GAME: Scene #7 has new activity]`,
  `[>GAME: Scene #7 is now idle]`, `[>GAME: You were invited to Scene #12]` — one
  reusable leader across notice types (D-03).
- Telnet nudge MUST be throttled/coalesced — not one line per pose (D-02).
- Web mute/prefs UI reuses create-scene's facade→BFF→client pattern; do not race
  or duplicate it (D-04).
- Idle nudge is OFF by default (D-06).

</specifics>

<deferred>

## Deferred Ideas

- **Scene templates (SCENEFWD-01 / bd `holomush-x4n1r`, P4)** — descoped from
  Phase 2, stays backlog (D-01). Not actively pursued at this time.
- **Digest (batched) notification delivery** — realtime this phase; digest
  deferred behind a store/prefs seam (D-05). Needs a scheduler + pending queue.
- **Persisted cross-session read-markers for badges** — already a deferred
  follow-up per ADR `holomush-0qnnr` (badges are per-workspace-session in v1).
- **Generalizing `[>GAME: …]` to channels / other subsystems** — the primitive is
  built reusable (D-03), but only scenes wire it this phase.

</deferred>

---

*Phase: 2-Scenes Lineage Completion*
*Context gathered: 2026-07-08*
