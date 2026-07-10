# Phase 2: Scenes Lineage Completion - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-08
**Phase:** 2-Scenes Lineage Completion
**Areas discussed:** Gray-area selection (scope), Telnet notification gap, Notification controls depth, Notification leader, Delivery timing, Telnet edge-case scope

---

## Gray-area selection (scope shaping)

| Option | Description | Selected |
|--------|-------------|----------|
| Template surface & sharing | Templates on telnet vs telnet+web; character-owned vs game-shared | |
| Telnet notification gap | Whether non-focused telnet members get a notification surface | ✓ |
| Notification controls depth | Per-scene mute only vs + prefs vs + web UI | ✓ |
| Telnet edge-case scope | Mixed-render only vs + reconnection + multi-char | ✓ |

**User's choice:** Selected the three notification/telnet areas; **plus a free-text directive to move templates (SCENEFWD-01) to the backlog** — "not actively desired or pursued at this time."
**Notes:** Templates was already a standalone P4 backlog bead (`holomush-x4n1r`), lifted out of the scenes epic 2026-07-03, so the descope required no new bead. ROADMAP.md + REQUIREMENTS.md updated the same session (user chose "Update both now"). Captured as D-01.

---

## Telnet notification gap

| Option | Description | Selected |
|--------|-------------|----------|
| Telnet nudge line | Throttled/coalesced inline nudge via the existing subscription-router downgrade path (ADR holomush-0qnnr) | ✓ (via note) |
| Mute-controls only (no telnet nudge) | Telnet notification deferred; SC #2 satisfied web-only | |

**User's choice:** No preset option; free-text note: *"we'll need a 'standard' game notification 'leader' to messages/nudges, like '[>GAME: Scene #7 has new activity]'."* Interpreted as: telnet nudge IS in scope, AND a standardized game-notification leader is a new requirement.
**Notes:** Reuse the shipped ControlFrame downgrade delivery path rather than a new stream. Captured as D-02 (nudge) + D-03 (leader).

---

## Notification controls depth

| Option | Description | Selected |
|--------|-------------|----------|
| Per-scene mute/unmute only | Telnet `scene mute`/`unmute`; digest prefs + web UI defer | |
| + notify default preference | Add per-character notify on/off, realtime/digest | |
| + web mute/prefs UI | Everything above PLUS the 4-layer web slice | ✓ |

**User's choice:** "+ web mute/prefs UI" — full depth.
**Notes:** Web slice reuses the shipped create-scene facade→BFF→client pattern (bd `holomush-5rh.22`); don't race it. Captured as D-04.

---

## Notification leader (`[>GAME: …]`)

| Option | Description | Selected |
|--------|-------------|----------|
| Reusable game-notify convention | Shared telnet primitive for ALL game-originated nudges | ✓ |
| Scene-local leader for now | Same format, core-scenes-local; generalize later | |

**User's choice:** "Reusable game-notify convention."
**Notes:** Rendering seam/placement is a research question; the constraint is that it's a shared reusable primitive. Captured as D-03.

---

## Delivery timing (realtime vs digest)

| Option | Description | Selected |
|--------|-------------|----------|
| Realtime only this phase | mute/unmute + notify on/off, realtime delivery; digest defers | ✓ (via note) |
| Include realtime\|digest pref | Add per-character realtime-vs-digest; new scheduler + queue | |

**User's choice:** No preset option; free-text note: *"but leave room for digest."* Interpreted as: realtime this phase, with a store/prefs **seam** so digest lands later with no migration/rewrite.
**Notes:** Mirrors Phase 1's ship-now+seam philosophy. Captured as D-05.

---

## Telnet edge-case scope

| Option | Description | Selected |
|--------|-------------|----------|
| Mixed focused/skipped render | Close the commands.go:890 TODO | ✓ |
| Reconnection restores membership+focus | Restore scene membership + focus on telnet reconnect (spec §11) | ✓ |
| Multi-character on one connection | Correct focus/render per character on a shared connection (spec §11) | ✓ |

**User's choice:** All three.
**Notes:** Reconnection + multi-char are the heavy, spec-§11 "needs deeper design" items. Captured as D-07/D-08/D-09.

---

## Claude's Discretion

- Nudge throttling/coalescing policy (per-scene rate, debounce window).
- `[>GAME: …]` rendering seam/placement (gateway `forwardFrame` vs shared `CommLine` primitive vs verb-registry).
- Exact `[>GAME: <msg>]` wording per notice type; `#id` vs scene title.
- Whether per-character notify pref + per-scene mute share one store table or two.
- Plan sequencing of reconnection-restore vs multi-char if they must split.

## Deferred Ideas

- Scene templates (SCENEFWD-01 / bd `holomush-x4n1r`, P4) — descoped to backlog (D-01).
- Digest (batched) notification delivery — behind a seam (D-05).
- Persisted cross-session read-markers for badges — already deferred per ADR holomush-0qnnr.
- Generalizing `[>GAME: …]` to channels/other subsystems — primitive built reusable, only scenes wire it now.
