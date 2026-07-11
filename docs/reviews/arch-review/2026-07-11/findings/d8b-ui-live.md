# D8b — UI Live Verification — Findings

**Agent:** main-loop + agent-browser 0.31.1 (Chromium) · **Date:** 2026-07-11 · **Baseline:** `30d55a162`
**Scope examined:** live web client at `http://localhost:8080` against the full `task dev:obs` stack (core + gateway + postgres healthy). Flows driven: landing → guest login → terminal; verbs `say`/`pose`/`ooc`/`help`/`look`/`who`; exit-click movement (both click and typed direction); command palette (⌘K); character switcher / logout controls. Screenshots in `evidence/ui/01..06`. Root-cause claims cross-checked against `web/src/routes/(authed)/terminal/+page.svelte`, `internal/world/service.go`, `internal/command/`, and all `plugins/*/plugin.yaml` command declarations.

## Summary

The client is **polished and the conversational loop works well live** — guest onboarding is one click to playing, and `say`/`pose`/arrival events render in real time with correct MUSH formatting, live presence, and a room/exits sidebar. But the **core spatial loop is absent**: there is no player-facing command to walk between locations, and the two most fundamental MUSH verbs (`look`, `who`) return "Unknown command." Clicking a rendered exit is a silent no-op. Counts: **High 1 · Medium 2 · Low 1 · Strengths 6.**

## Findings

### HIGH-1 — No player-facing movement: characters cannot walk between locations
- **Severity:** High
- **Claim:** The world engine can move characters (`world.Service.MoveCharacter`) and the UI renders clickable exits, but no registered command or RPC lets a player traverse an exit. Clicking the "market" exit — or typing the direction — leaves the character in place with no feedback.
- **Evidence:**
  - Live: from "The Nexus", clicking the `market` exit button (fresh ref, 3× incl. typed `market`) left the location heading, exits panel, and transcript unchanged; no toast, no console error (`evidence/ui/06-after-exit-click.png`).
  - The exit handler sends the direction as a raw command: `handleExitClick(direction){ sendCommand(direction); }` — `web/src/routes/(authed)/terminal/+page.svelte:664-666`.
  - No command consumes it. Full command inventory across every `plugins/*/plugin.yaml`: say/pose/page/whisper/ooc/pemit/emit/wall, help, describe/examine/create/set, dig/link, scene/scenes — **no go/move/walk/direction/exit command**. No dispatcher exit-resolution fallback (`rg` over `internal/command` for exit/direction/traverse → none).
  - `world.Service.MoveCharacter` (`internal/world/service.go:773`) is real and exposed to the command package as an interface method (`internal/command/types.go:36-37`), but `probe MoveCharacter` finds **no production caller** outside the world package — no command handler invokes it. Dispatcher does a flat `registry.Get(parsed.Name)` with no exit-name fallback (`internal/command/dispatcher.go:269-274`). **Correction (per adversarial verification, `verification/skeptic-d8-movement-pwa.md`):** `MoveCharacter` is unit-tested with mocks only (`service_test.go`, `movement_hook_test.go`); `test/integration/world/movement_test.go` exercises exit-repo CRUD, NOT `MoveCharacter` — so the command→move path has no integration coverage at all, which reinforces the finding.
- **Impact:** Every player. "Move between locations" is the core loop of a MUSH; without it the world is a single room per character. The building commands (`dig`/`link`) let you *construct* a map you cannot *walk*.
- **Recommendation:** Add a movement command (register a handler that resolves the typed/clicked exit direction against `WorldService.ListExits` and calls `MoveCharacter`), OR a typed `Move`/`Traverse` facade RPC the exit button calls (preferred per gateway-boundary — see MEDIUM-1). Emit the existing move event so arrival/departure render. Bind a command-level integration test.
- **Dedup:** none found (no movement/walk/traverse issue in the 186 open issues). **Flag for verification:** confirm no built-in dispatcher handler or alias reaches `MoveCharacter` before filing.

### MEDIUM-1 — Exit navigation dispatched as a raw string command (gateway-boundary anti-pattern shape)
- **Severity:** Medium
- **Claim:** A GUI button click (machine-initiated navigation) is routed through the human text-command parser via `sendCommand(direction)`, and there is no typed movement RPC at all. Per `.claude/rules/gateway-boundary.md`, machine-initiated structural actions should use a typed facade RPC; movement (a location mutation) sits on the structural side.
- **Evidence:** `web/src/routes/(authed)/terminal/+page.svelte:664-666` (`sendCommand(direction)`); no `Move`/`Traverse` RPC in `api/proto/**` (grep of proto services returns none). Contrast the codebase's own discipline: scene structural writes use typed facade RPCs per ADR `holomush-v4qmu`.
- **Impact:** When movement is implemented (HIGH-1), doing it via string-building through the command parser re-introduces the exact pattern the project deliberately avoids for scenes; also makes the button's behavior depend on command-parser quirks.
- **Recommendation:** Introduce a typed movement RPC on the BFF facade and have the exit button call it; keep a typed conversational movement verb for the terminal if desired.
- **Dedup:** none.

### MEDIUM-2 — `look` and `who` return "Unknown command" with no pointer to the panels that hold that state
- **Severity:** Medium
- **Claim:** The two most-typed MUSH verbs are unhandled. A guest typing `look` gets `Unknown command. Try 'help'.`; `who` likewise. The room description, exits, and presence exist only in the sidebar, which a terminal-first user won't look at.
- **Evidence:** Live transcript: `look` → `Unknown command. Try 'help'.` (`evidence/ui/03`, `04-help-output.png`); `help` lists only quit / communication verbs / help — no `look`/`who`. Room state is in the sidebar panels (`sidebar/*.svelte`, `eventRouter.ts` `location_state`/`exit_update`).
- **Impact:** Poor first-run experience for anyone from the MU\* tradition (the docs explicitly court that audience). Reads as "broken" rather than "GUI-first."
- **Recommendation:** Register `look`/`who` (even as thin commands that echo the current room/presence into the transcript), or special-case them in the dispatcher to surface a "see the Room / Present panels →" hint. Cheap, high UX payoff.
- **Dedup:** none.

### LOW-1 — Command palette (⌘K) is app-navigation only; may mislead a game-command expectation
- **Severity:** Low
- **Claim:** ⌘K opens a palette offering "Go to Room" and theme switches — UI navigation/settings, not game commands. Reasonable, but a new player may expect a game-command launcher.
- **Evidence:** Live snapshot of the palette listbox (`Go to Room`, `Switch theme: …`).
- **Recommendation:** Optional — a one-line palette hint ("app commands; type game commands in the terminal") or include a few common verbs.
- **Dedup:** none.

## Strengths (live-confirmed)

- **Guest onboarding is excellent:** one "Try as Guest" click auto-creates a character ("Garnet Xenon") and spawns it in a fully-described location with exits and presence — zero friction to first play (`evidence/ui/01-landing.png`, `02-terminal-guest.png`).
- **Real-time conversational loop works correctly:** `say` → `Garnet Xenon says, "Hello from the review"`, `pose` → `Garnet Xenon waves at the room`, plus a live `has arrived` notice — proper MUSH rendering, correctly attributed, delivered over the live Subscribe stream.
- **Live sidebar state:** room name + prose description, EXITS (market, threshold), PRESENT (the character) all render and are driven by `location_state`/`exit_update` events (`eventRouter.ts`).
- **Polished terminal UX:** command history ("RECENT"), keyboard palette (⌘K), theme toggle with multiple themes, live connection status ("LIVE"/"connected"), switch-character and log-out controls, keyboard-shortcut legend.
- **`help` works** and is grouped by plugin (Core / Core-communication / Core-help), with `help <command>` detail hinted.
- **Streaming/reconnect wiring is genuinely careful** (generation-gated dedup, presence mirroring across the dedup branch) per `+page.svelte:600-666` — corroborates D8a's correctness verdict.

## Not examined (live)

- Registered-player login/registration flow end-to-end (tested guest only; login/register forms present).
- Scenes and channels UI flows live (D8a covers static; scenes has full routes, channels has no GUI per D8a).
- Admin views, character creation form, multi-tab session behavior, mobile/responsive rendering.
- Whether a *registered* (non-guest) character can move — tested guest only; HIGH-1's root cause (no command calls `MoveCharacter`) is character-kind-independent, but a guest-specific movement lock is not fully excluded and is noted for verification.
