<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Terminal Hi-Fi Revamp — Design Specification

**Date:** 2026-04-18
**Status:** Approved (brainstorming)
**Scope:** Web client only (`web/`)
**Related follow-up:** `holomush-uhiz` (server-side optional sidebar fields), `holomush-w7t5` (server-side player prefs), future `⌘R` history search bead

---

## Summary

Revamp the terminal page (`/terminal`) to match the "Terminal hi-fi" design from a Claude Design handoff. The design modernizes the terminal experience with a merged 44px topbar, a 48px left icon rail, card-based sidebar, per-line timestamps, a floating composer, a `⌘K` command palette, and a mode chip on command input. All visual tokens map to existing `--color-*` / `--mush-*` runtime theme variables — no theme schema changes.

## Motivation

Current `/terminal` page renders two headers (global 36px `TopBar` + local `StatusBar`), plain text `--- REPLAY ---` / `--- LIVE ---` separators, a monolithic sidebar list, and a plain inline input with no mode indication, palette, or composer. The hi-fi design treats the terminal as a first-class product surface with clear visual hierarchy, keyboard-first navigation, and room to expand (rail placeholders for future DM/Map/Notes views).

## Non-Goals

- Tweaks panel (dev-only postMessage-gated tool in the prototype) — out of scope.
- Latency ms readout in conn pill — out of scope (noise, unreliable at dev-env scale).
- Server-side population of `location.mood`, `presence.lastMode`, `presence.isIdle` — filed as `holomush-uhiz`.
- History search overlay (`⌘R` reverse-i-search) — filed as separate follow-up.
- Proto/gRPC schema changes — timestamps already present on server events.

## Locked Decisions

| # | Decision | Choice |
|---|---|---|
| 1 | Scope | Visuals + palette + composer + density; skip tweaks panel + latency ms |
| 2 | Chrome strategy | Extend global `TopBar` to 44px, terminal-aware extras on `/terminal`; delete `StatusBar.svelte` |
| 3 | Rail items | Room \| DM (disabled) \| Map (disabled) \| Notes (disabled) \| Settings — no People |
| 4 | Timestamps | Server event time (`event.occurredAt`), client-arrival fallback |
| 5 | UI state | New `uiPrefsStore`, localStorage-backed with a server-sync adapter seam |
| 6 | Composer | Draft mirrors into inline input on open/close; submit-and-close; persist pos/size |
| 7 | Palette | UI actions only (no game commands); separate `⌘R` history search as a follow-up |
| 8 | History store | New `commandHistoryStore` — lifts history out of `CommandInput.svelte` |
| 9 | Implementation | Single PR, bottom-up (Approach 1) |

## Architecture

### File changes

| Category | Files |
|---|---|
| **New stores** | `web/src/lib/stores/uiPrefsStore.ts`, `commandHistoryStore.ts`, `connectionStore.ts` |
| **Extended stores** | `terminalStore.ts` (add `timestamp: Date` to `TerminalLine`); `sidebarStore.ts` (add optional `location.mood`, `presence.lastMode`, `presence.isIdle`) |
| **Proto / transport** | No `.proto` changes. Event mapper populates `TerminalLine.timestamp: Date` from `event.timestamp: bigint` (Unix millis — `GameEvent.timestamp` field, already on the wire per `web_pb.ts:133`), with `Date.now()` fallback when absent or zero |
| **New components** | `Rail.svelte`, `CommandPalette.svelte`, `Composer.svelte`, `ModeChip.svelte`, `LiveSeparator.svelte`, `RecentCommandsCard.svelte` |
| **Extended components** | `TopBar.svelte` (breadcrumb, conn pill, palette hint — terminal-aware via `$page.route.id`) |
| **Restructured components** | `Sidebar.svelte` (cards), `RoomInfo.svelte` → `RoomCard.svelte`, `ExitList.svelte` (exit-row shape), `PresenceList.svelte` (pres-row with avatar + status) |
| **Rewritten** | `(authed)/terminal/+page.svelte`, `CommandInput.svelte` (mode chip + suspended), `TerminalView.svelte` (timestamps + LIVE separator + just-arrived flash) |
| **Deleted** | `StatusBar.svelte` |
| **Global** | `app.css` (layout tokens, density tokens, message-class rules, animation keyframes); `+layout.svelte` (global keyboard handler + overlay mount points) |

### State ownership

| Store | Responsibility |
|---|---|
| `terminalStore` | Lines buffer with timestamps; replay boundary tracking. Unchanged semantics; extended shape. |
| `sidebarStore` | Location / exits / presence. Remove `sidebarExpanded` (moves to `uiPrefsStore`). Add optional fields. |
| `uiPrefsStore` (new) | `railHidden`, `sidebarHidden`, `sidebarWidthPx` (pixels, clamped 200–520), `density`, `composerOpen`, `composerPos`, `composerSize`, `paletteOpen`, `railView: 'room'`. Plain `writable` + localStorage read-through post-mount (SSR safety — see Risks), write-through on change. No server-sync adapter in this PR — `holomush-w7t5` will dictate the shape when it lands. |
| `commandHistoryStore` (new) | `entries: string[]`, `navIndex: number`. Methods: `push(cmd)` (dedups consecutive duplicates, caps at 100), `navigatePrev/Next`, `reset`, `seed(entries)`. Future home for `⌘R` search state. |
| `connectionStore` (new) | `{ status: 'connected' \| 'syncing' \| 'disconnected' }`. Updated from the stream lifecycle hooks in `+page.svelte`'s existing connect/reconnect logic. Consumed by TopBar's `.conn-pill`. Single responsibility; replaces ad-hoc reading of `terminalStore.replayActive` flags. |
| `themeStore` | Unchanged. |
| `authStore` | Unchanged. |

### Visual tokens

All prototype CSS variables map to existing `--color-*` (chrome) or `--mush-*` (message) tokens from `themeStore`. Zero new theme schema fields.

**Layout tokens (static, in `app.css` outside `@theme`):**

```css
:root {
  --topbar-h: 44px;
  --rail-w: 48px;
  --sidebar-w-default: 280px;
  --sidebar-w-min: 200px;
  --sidebar-w-max: 520px;
  --cmd-max-lines: 8;
  --composer-default-w: 640px;
  --composer-default-h: 340px;
}
```

**Density tokens:**

```css
.app-root[data-density="cozy"]    { --row-py: 6px; --row-gap: 8px; --card-pad: 12px; }
.app-root[data-density="compact"] { --row-py: 3px; --row-gap: 4px; --card-pad: 8px; }
```

Applied on `.app-root` via `uiPrefsStore.density`. **Layering:** layout tokens live on `:root` (app-wide constants, never change at runtime); density tokens + `themeStore` color tokens live on `.app-root` (per-user overrides). Components always read via `var(--token)` — the cascade resolves to whichever scope defines it. Density selectors are scoped to `.app-root` so a future alternate app shell (e.g., embed/iframe) can render without density applied.

**Animations (in `app.css`):** `dot-pulse`, `just-arrived`, `composer-slide-up`. All gated by `@media (prefers-reduced-motion: no-preference)`.

**Message classes:** 11 `.hm-*` classes (`hm-say-speaker`, `hm-say-speech`, `hm-pose-actor`, `hm-pose-action`, `hm-ooc`, `hm-system`, `hm-cmd-out`, `hm-cmd-err`, `hm-move`, `hm-timestamp`, `hm-tag`) each referencing a `--mush-*` token.

## Components

### TopBar (extended, 36px → 44px)

Becomes terminal-aware. Slots left → right:

| Slot | When | Content |
|---|---|---|
| `.brand-chip` | always | 24px H mark + "HoloMUSH" wordmark |
| `.breadcrumb` | `/terminal` only | char name + `@` + location name + `#<id>` |
| spacer | always | `flex: 1` |
| `.kbd-hint` | `/terminal` only, ≥md viewport | `⌘K palette` |
| `.conn-pill` | `/terminal` only | status dot + text (`connected` \| `syncing` \| `disconnected`) read from `connectionStore`. `syncing` dot pulses; no ms |
| `.char-name-chip` | authed | existing |
| icon buttons | always | theme dropdown, sidebar toggle (`/terminal` only; `title="Toggle sidebar"` — preserves E2E selector), switch-character (authed), logout (authed) |

**E2E compat:** `[data-testid="topbar-char-name"]` added; existing `.char-name` class preserved. E2E assertion on `.status-bar .character` migrates to `[data-testid="topbar-char-name"]`. `button[title="Toggle sidebar"]` lives in the TopBar icon row on `/terminal` — same element that fires `uiPrefs.toggleSidebar()`.

### Rail (new, 48px)

Left chrome column with icon buttons (top) and a `⌘B` hint (bottom).

| Button | State | Action |
|---|---|---|
| Room | active (default) | Focus / reveal `.card.room` in sidebar |
| DM | disabled | `aria-disabled="true"`, reduced opacity, tooltip "Coming soon" |
| Map | disabled | Same |
| Notes | disabled | Same |
| Settings | popover | Theme swatches + density toggle + terminal-black checkbox |

Width transitions `48px ↔ 0` on `uiPrefsStore.toggleRail()`. Auto-hides below 768px viewport.

### Terminal page layout

```text
<div class="terminal-layout">
  <Rail />
  <Resizable.PaneGroup>
    <Resizable.Pane>
      <TerminalView />
      <CommandInput />
    </Resizable.Pane>
    <Resizable.Handle />
    <Resizable.Pane>
      <Sidebar />
    </Resizable.Pane>
  </Resizable.PaneGroup>
</div>
<!-- Portaled in +layout.svelte -->
<Composer />
<CommandPalette />
```

`.terminal-layout` class preserved (E2E). No `StatusBar`. OTEL spans (`command.roundtrip`, `stream.lifecycle`) intact.

### TerminalView

**DOM restructure:** lines live in their own `.lines` container so `:last-child` targets an actual line (not the scroll sentinel or the LIVE separator, which the current implementation mixes together).

```html
<div class="term-scroll">
  {#if hasReplay && hasLive}
    <div class="sep-live">...</div>    <!-- between replay chunk and live chunk if both exist -->
  {/if}
  <div class="lines" aria-live="polite" aria-relevant="additions">
    {#each $lines as line (line.id)}
      <div class="line" class:replay={line.isReplay} data-event-id={line.id}>
        <span class="tstamp">{formatHHMM(line.timestamp)}</span>
        <EventRenderer event={line.event} />
      </div>
    {/each}
  </div>
  <div class="sentinel" bind:this={sentinel}></div>   <!-- outside .lines -->
</div>
```

**Timestamps:** `Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })`. `tabular-nums`. Color `var(--mush-timestamp)`. Mapped from `event.timestamp` (bigint Unix ms) → `new Date(Number(bigint))` at event-mapper time.

**LIVE separator:** rendered once, between the last replay line and the first live line, outside `.lines` so it doesn't disrupt `:last-child` targeting. Rendered when both replay AND live lines are present; if replay-only or live-only, no separator shown.

```html
<div class="sep-live" role="separator">
  <span class="dot" aria-hidden="true"></span>
  <span class="label">LIVE</span>
  <span class="gradient-line" aria-hidden="true"></span>
</div>
```

Implementation note: since the separator must split the two chunks, render it as two `{#each}` blocks — one for replay lines, separator in between, one for live lines. Keeps each `.lines` block's `:last-child` honest. (Alternative: one `{#each}` with a position check — rejected because it re-introduces the mixed-container problem.)

**Just-arrived flash:** CSS animation on `.lines > .line:last-child:not(.replay)`. Valid now that `.lines` contains only `.line` elements:

```css
@media (prefers-reduced-motion: no-preference) {
  .lines > .line:last-child:not(.replay) { animation: just-arrived 600ms ease-out; }
}
@keyframes just-arrived {
  from { background: color-mix(in srgb, var(--color-primary) 12%, transparent); }
  to   { background: transparent; }
}
```

The animation runs once when the element is newly mounted as the last child. The next append demotes it to `:not(:last-child)`, ending the animation naturally — the animation doesn't re-fire because CSS animations only trigger on style/element changes that start them, not on selector-no-longer-matching. This relies on strict-append semantics: new lines only ever appear at the end of the live chunk. `terminalStore.appendLine()` already guarantees this.

**Scroll behavior:** existing `IntersectionObserver` on sentinel preserved; `.sentinel` stays where it is (outside `.lines`).

### CommandInput

```html
<div class="cmd-wrap" class:is-suspended class:is-multiline>
  <span class="cmd-prompt">›</span>
  {#if modeChip}<ModeChip {modeChip} />{/if}
  <textarea disabled={composerOpen} />
  {#if isMultiline}<button class="cmd-send">Send</button>{/if}
  {#if composerOpen}<div class="suspended-overlay">Composer open — input paused</div>{/if}
</div>
<div class="cmd-hints">...keyboard reference... {linesN}/{maxN} {#if nearMax}<span class="composer-nudge">Use ⌘⇧E</span>{/if}</div>
```

**Mode detection (`$derived`):**

| Input prefix | Mode |
|---|---|
| `:` or `pose` | `pose` |
| `"` or `say` | `say` |
| `ooc` | `ooc` |
| otherwise | none |

**Auto-grow:** reads `var(--cmd-max-lines, 8)` via `getComputedStyle`. At `≥6` lines show `composer-nudge`.

**History:** `commandHistoryStore` replaces component-local state. `push()` on submit, `navigatePrev/Next` on arrows, `reset()` on Esc/submit.

**Draft persistence:** existing component-local `draft` state + `holomush-draft:<sid>` localStorage write-through, **unchanged**. No store needed: the inline textarea is `disabled` when the composer is open (line 188), so only one editor writes at a time. Composer receives `draft` as a `$props()` input and emits changes via a callback (`ondraftChange`); on close, `CommandInput` picks up the latest value from the shared localStorage key (same key, same lifecycle as today).

**Suspended state:** textarea `disabled`; semi-transparent overlay with `aria-live="polite"`. **Autogrow gate:** the autogrow `$effect` computing line count from `textarea.scrollHeight` is gated on `!composerOpen` — disabled textareas can produce unreliable `scrollHeight` values and re-computing during a state the user can't edit is wasted work.

### Sidebar cards

Width from `uiPrefsStore.sidebarWidthPx` (default 280px, clamped 200–520). Stored as pixels because users expect a sidebar to be "the same visual width" across viewport changes, not rescaled proportionally. Collapse via `⌘.` = `width: 0` transition. **E2E migration:** previous selectors `.sidebar.expanded` / `.sidebar:not(.expanded)` migrate to `[data-testid="sidebar"]` with attribute `data-expanded="true|false"` — the test file is updated in the same PR. No alias class (half-migrations age badly).

**RoomCard:** primary-tinted (`color-mix(in srgb, var(--color-primary) 8%, var(--color-card))`), header with room name + id, description, optional mood line.

**ExitsCard:** `.exit-row` with arrow/dir/location; `.locked` variant reduces opacity + lock glyph.

**PresenceCard:** `.pres-row` with colored avatar (color from optional `lastMode`, fallback `sys`), name, status dot. `.is-idle` grayscales and reduces opacity. `.presence-list` class preserved (E2E).

**RecentCommandsCard:** last 8 entries from `commandHistoryStore`, newest first. Click **injects into the active editor** (composer if `$uiPrefs.composerOpen`, else inline input) by writing to `draftStore`. Does not auto-send.

**Resizer:** bits-ui `Resizable.Handle` restyled to `.resizer` 4px. Conversion between pixels and bits-ui's internal proportion representation happens at the component boundary:

- **On mount:** compute `defaultSize = (sidebarWidthPx / containerWidth) * 100` (bits-ui uses percentages in its API) and pass it to `Resizable.Pane`.
- **On resize:** listen to `onResize(pct)` from the pane; convert to px via current `containerWidth`; debounce to avoid per-frame localStorage writes; commit to `uiPrefsStore.setSidebarWidthPx(px)` at drag-end. If bits-ui exposes `onResizeEnd`, prefer that; otherwise debounce `onResize` with a 200ms trailing-edge timer.
- **On viewport resize:** a `ResizeObserver` on the layout container recomputes and reapplies the pane's size so the stored 280px stays 280px visually even when the window changes. Clamp to 200–520 after computing — if the viewport shrinks so 200px would exceed the pane group, cap at whatever proportion gives the max space available, but do NOT write that back to the store (avoid overwriting the user's preference during a transient resize).

### Composer (`Composer.svelte`)

**Floating panel, not a dialog** (640×340 default, viewport-clamped, min 360×200, max viewport - 80px). The user can still interact with the terminal (scroll, click) while the composer is open — only *inline command submission* is suspended. Mounted at the root of `+layout.svelte` (sibling to `<main>`) with `position: fixed`; z-index above sidebar, below palette. **Not a Svelte portal** — Svelte 5 has no built-in portal primitive, and `position: fixed` + layout-root mounting gives the same effect without the extra indirection.

**Structure:** `.chead` (drag handle, title, char/line meta, close button) → `.ctextarea` → `.cfoot` (keyboard hints) → `.resize-handle`.

**Drag/resize:** pointer events on window; commit pos/size to `uiPrefsStore` on `pointerup` (single write). Clamp drag so header stays within viewport (40px slack).

**Draft:** composer receives `draft: string` and `ondraftChange: (text: string) => void` via `$props()`. Its textarea is bound two-way to local `$state` seeded from the prop; on change it calls `ondraftChange(newText)`. `CommandInput` owns the draft state (as today) and passes it down. Inline textarea is `disabled` while composer is open, so the two editors cannot write simultaneously.

**Submit (`⌘⏎`):** calls an `onsubmit: (text: string) => void` prop, which `CommandInput` handles (same `client.sendCommand` path used for inline submit — one send path, one set of OTEL spans). On success: `ondraftChange('')` to clear, close composer. On error: keep composer open, show `.cerr` strip.

**Esc:** closes composer. Implemented via a **composer-scoped `window` keydown listener** registered in an `$effect` that runs when `composerOpen === true` and torn down when it flips false. Using a window listener (not an element listener) is important because the composer is non-modal: focus can be on the drag handle, close button, sidebar scrollbar, or elsewhere; an element-scoped listener would miss Esc in those cases. The listener calls `preventDefault()` + `stopPropagation()` so the global layout handler never sees the event.

**A11y:** `role="region"` with `aria-label="Command composer"`. **No `role="dialog"`** — this is a non-modal tool panel, not a dialog. Focus moves to the composer textarea on open (convenience focus). Tab order: textarea → close button → (exits into the rest of the page). The close button also has `aria-label="Close composer"`. Inline input shows the suspended-overlay with `aria-live="polite"` announcing "Composer open, input paused" once on state change.

### CommandPalette (`CommandPalette.svelte`)

Portal-mounted overlay. Input + filtered list. Backdrop click closes.

**Item registry (static):**

| Item | Action |
|---|---|
| Switch theme: Default Dark / Light / Classic Dark / Light (4 items) | `themeStore.setTheme(...)` |
| Toggle rail | `uiPrefs.toggleRail()` (hint `⌘B`) |
| Toggle sidebar | `uiPrefs.toggleSidebar()` (hint `⌘.`) |
| Toggle composer | `uiPrefs.toggleComposer()` (hint `⌘⇧E`) |
| Toggle density | `uiPrefs.toggleDensity()` |
| Toggle terminal black background | `themeStore.setTerminalBlackBackground(!current)` |
| Clear terminal | `terminalStore.clear()` (hint `⌘L`) |
| Sign out | `authStore.logout()` |

**Mount:** built on **`cmdk-sv`** — the Svelte port of Vercel's `cmdk`, the canonical command-palette primitive (this is what shadcn-svelte's `Command` component wraps). It provides the correct combobox + listbox wiring, built-in filtering, arrow-key nav, Enter handling, scroll-into-view for the active item, and proper ARIA out of the box. Wrapping it in bits-ui `Dialog` would conflict (dialog + combobox roles competing) — `cmdk-sv`'s `Command.Dialog` primitive handles the modal shell natively.

**Adding the dependency:** `cmdk-sv` is a small, zero-runtime peer (uses bits-ui primitives under the hood). Added via `pnpm add cmdk-sv` in step 10. shadcn-svelte's `Command` component is the idiomatic layer on top — prefer generating it via `pnpm dlx shadcn-svelte@latest add command` so we get the project's existing shadcn styling conventions and the full accessibility surface for free.

```svelte
<Command.Dialog bind:open={$uiPrefs.paletteOpen} label="Command palette">
  <Command.Input placeholder="Type a command…" />
  <Command.List>
    <Command.Empty>No matches</Command.Empty>
    {#each items as item}
      <Command.Item value={item.label} onSelect={() => { item.run(); uiPrefs.closePalette(); }}>
        <span class="palette-icon">{item.icon}</span>
        <span class="palette-label">{item.label}</span>
        {#if item.hint}<kbd class="palette-hint">{item.hint}</kbd>{/if}
      </Command.Item>
    {/each}
  </Command.List>
</Command.Dialog>
```

**Filtering:** `cmdk-sv` provides case-insensitive substring filtering by default on `Command.Item` `value`. Ranking (exact-prefix first) is its default heuristic. Arrow nav, Enter, Escape, focus management, scroll-on-keyboard-nav — all free. We do not hand-roll any of this.

**Focus return:** handled by `cmdk-sv` — returns focus to the opener (the element that was focused when `paletteOpen` flipped to true).

## Keyboard

Single global listener in `+layout.svelte`, bound on `window` in the **capture** phase so it sees events before bubbling listeners on children:

```ts
window.addEventListener('keydown', handleGlobalKey, { capture: true });
```

**Rules the handler MUST follow:**

1. **IME composition guard** — skip immediately if `event.isComposing || event.keyCode === 229`. Without this, CJK/Japanese/Korean input users lose keystrokes mid-composition. Non-negotiable.
2. **Explicit `preventDefault()` on every matched combo** — `⌘L` otherwise clears the address bar; `⌘K` in some browsers focuses the search bar; `⌘.` is macOS "Stop" in some apps. Always `preventDefault()` when the combo matches.
3. **`stopPropagation()` on match** — prevents the shortcut from reaching focused inputs (the `textarea` must not also see `⌘L` as a keystroke).
4. **Platform normalization** — use `event.metaKey || event.ctrlKey` (cover both macOS Cmd and Linux/Windows Ctrl) for the modifier.

**Matched combos:**

| Key | Handler |
|---|---|
| `Cmd/Ctrl + K` | `uiPrefs.togglePalette()` |
| `Cmd/Ctrl + B` | `uiPrefs.toggleRail()` |
| `Cmd/Ctrl + .` | `uiPrefs.toggleSidebar()` |
| `Cmd/Ctrl + Shift + E` | `uiPrefs.toggleComposer()` |
| `Cmd/Ctrl + L` | `terminalStore.clear()` |
| `Esc` | see cascade below |

**Esc cascade (priority order, first match wins):**

1. Palette open → `uiPrefs.closePalette()`. **Handled by bits-ui `Dialog`**, not the window listener — bits-ui captures Esc at the dialog root. The global window listener never sees it.
2. Composer open → composer's own `onkeydown` on `.ctextarea` (and header) calls `uiPrefs.closeComposer()` + `stopPropagation()`. The global listener sees Esc only if both palette and composer are closed.
3. Otherwise → `CommandInput` handles Esc = clear draft (local listener on the textarea).

This cascade is implemented by **ordering**, not by explicit priority checks — bits-ui Dialog traps Esc first because it's closest to the focused element; composer traps next because its listener is on the composer root; the window listener gets whatever's left. The only guarantee the global handler provides is that "Esc when nothing is open does nothing" (it doesn't call any close handler).

## Accessibility

- Rail icon buttons have `aria-label`; disabled rail items have `aria-disabled="true"` (not just `disabled` — screen readers announce "coming soon" label).
- Palette uses bits-ui `Dialog` (`role="dialog"`, `aria-modal="true"`, focus trap, Esc, scroll lock — all handled by bits-ui, not hand-rolled).
- Composer is **non-modal** — `role="dialog"` with `aria-labelledby` but **no `aria-modal`**. Focus moves to textarea on open (convenience, not a trap); tab order continues naturally out when user Shift-Tabs from the textarea.
- Suspended-overlay uses `aria-live="polite"` to announce "Composer open, input paused" once on state change.
- All animations gated on `@media (prefers-reduced-motion: no-preference)` — reduced-motion users get instant state changes (including the `just-arrived` flash).
- Color contrast: all `--mush-*` tokens in existing themes already meet WCAG AA; new layout doesn't introduce text-on-color combinations beyond what theme redesign (#178) verified.

## Testing

### Unit (Vitest)

| Target | Behavior |
|---|---|
| `uiPrefsStore` | Toggles; `setSidebarWidthPx` clamps 200–520; hydrate from localStorage via post-mount `$effect` (SSR safety); write-through on change; `railView` defaults to `'room'` |
| `commandHistoryStore` | `push` dedups consecutive duplicates, caps at 100; `navigatePrev/Next` bounds; `reset`; `seed` |
| `connectionStore` | Transitions `disconnected → syncing → connected` on lifecycle events; consumers see latest state |
| `terminalStore` | Timestamp set from `event.timestamp` (bigint → Date); fallback to `Date.now()` when 0/absent |
| `CommandInput` mode derivation | Prefixes `:`, `pose`, `"`, `say`, `ooc` map correctly; otherwise no chip |
| `CommandInput` autogrow gate | No `scrollHeight` reads while `composerOpen === true` |
| `Composer` | Receives `draft` prop, emits `ondraftChange`; `⌘⏎` calls `onsubmit` then closes; drag/resize commits on `pointerup` |
| Global keyboard handler | IME guard (`isComposing=true` → skip all); `preventDefault` + `stopPropagation` on every match; platform normalization (`metaKey || ctrlKey`) |

### E2E (Playwright)

**Existing `terminal.spec.ts` selector migrations (all updated in the same PR — no aliases):**

- `.status-bar .character` → `[data-testid="topbar-char-name"]`
- `.sidebar.expanded` / `.sidebar:not(.expanded)` → `[data-testid="sidebar"][data-expanded="true"]` / `[data-testid="sidebar"][data-expanded="false"]`
- `button[title="Toggle sidebar"]` → kept (title attr preserved on the TopBar toggle button)
- `.terminal-layout`, `.presence-list`, `[data-testid="event"]` → kept

**New scenarios:**

- `⌘K` opens palette; Esc closes; typing narrows list; Enter on active item runs it
- `⌘B` toggles rail visibility (persisted across reload)
- `⌘.` toggles sidebar visibility
- `⌘⇧E` opens composer; text typed in inline input is visible in composer; editing composer then closing keeps text visible in inline; `⌘⏎` from composer submits and closes
- Mode chip appears on `": hello"`, `"say foo"`, `"ooc bar"`; no chip on `"look"` or `"go north"`
- Every line has a timestamp in `HH:MM` format; replay lines carry their historical time (not current time)
- LIVE separator exists exactly once in the DOM (when any live line present); absent when all lines are replay
- IME guard: with IME composition active, global shortcuts do not fire (mock `isComposing=true` via dispatched event)

**A11y checks (Playwright + axe-core):** palette dialog focus trap (bits-ui); composer textarea gets focus on open but can be Tab-exited; disabled rail items announced as "coming soon, button disabled"; reduced-motion honored (inspect computed `animation-name` is `none` on `.line:last-child`).

## Implementation Order

Single PR, bottom-up.

| Step | Work | Gate |
|---|---|---|
| 0 | Load `jj:jujutsu` skill | - |
| 1 | `jj workspace add <parent>/.worktrees/terminal-hifi --name terminal-hifi -r main`; `task gowork` | - |
| 2 | Create `uiPrefsStore` (post-mount hydration via `$effect`; `railView`, `sidebarWidthPx`), `commandHistoryStore`, `connectionStore`. Unit tests. Add `timestamp: Date` to `TerminalLine` + event mapper from `event.timestamp: bigint` | `task test` |
| 3 | Layout tokens (`:root`), density tokens (`.app-root`), animations, `.hm-*` classes in `app.css`. Apply message classes in EventRenderer sub-components | `task lint` + `task fmt` |
| 4 | Extend `TopBar.svelte` (44px, breadcrumb, conn pill consuming `connectionStore`, palette hint, sidebar-toggle button with `title="Toggle sidebar"`); delete `StatusBar.svelte`; migrate `.status-bar .character` E2E selector to `[data-testid="topbar-char-name"]` | E2E locally |
| 5 | Build `Rail.svelte`; wire `⌘B`; integrate in `+page.svelte`; disabled buttons have `aria-disabled="true"` and "Coming soon" tooltip | - |
| 6 | Rewrite `TerminalView.svelte`: **restructure DOM so `.lines` contains only `.line` elements**, with sentinel and LIVE separator outside it; render timestamps from `line.timestamp`; `.lines > .line:last-child:not(.replay)` just-arrived animation; `.replay` dimming; **preserve existing OTEL span instrumentation** for `command.roundtrip` and `stream.lifecycle`; wire `connectionStore` updates from stream lifecycle hooks | - |
| 7 | Extend `CommandInput.svelte`: mode chip, suspended state (on `$uiPrefs.composerOpen`), autogrow gate while suspended, line-count, composer-nudge; swap local history state for `commandHistoryStore`; keep existing draft-state + localStorage write-through unchanged | - |
| 8 | Restructure `Sidebar.svelte` into cards; migrate `.sidebar.expanded` → `[data-testid="sidebar"][data-expanded]`; wire bits-ui `Resizable` with px↔pct conversion at component boundary + `ResizeObserver` for viewport-resize handling | - |
| 9 | Build `Composer.svelte` (`role="region"`, not dialog; mounted at `+layout.svelte` root with `position: fixed`); `draft` prop + `ondraftChange` callback from `CommandInput`; composer-scoped window-level Esc listener gated on `composerOpen`; drag + resize via pointer events; persist pos/size on `pointerup` | - |
| 10 | Install `cmdk-sv`; `pnpm dlx shadcn-svelte@latest add command`; build `CommandPalette.svelte` wrapping `<Command.Dialog>` with static item registry; no hand-rolled focus trap or arrow nav | - |
| 11 | Global keyboard handler in `+layout.svelte` with `capture: true`, IME guard, explicit `preventDefault()` + `stopPropagation()` on every match; wire reduced-motion CSS guards | - |
| 12 | Migrate E2E selectors (single step — no aliases); add palette/rail/composer/mode-chip/timestamp/LIVE/IME scenarios | `task test:int` |
| 13 | `task pr-prep` (mirrors all CI jobs) | MUST be green |
| 14 | Open PR; invoke `/pr-review-toolkit:review-pr`; address findings | - |

### PR-description smoke checklist (not a build gate)

Capture and attach to the PR body:

- Screenshots for each theme × density combination (4 × 2 = 8)
- Toggle each chrome element (rail, sidebar, composer, palette) and confirm persistence across reload
- Confirm every keyboard shortcut (`⌘K/B/./L`, `⌘⇧E`) works and does NOT propagate to the focused textarea
- Confirm `command.roundtrip` and `stream.lifecycle` OTEL spans still emit (browser devtools → console or network)
- Confirm `undefined` fields on `location.mood`, `presence.lastMode`, `presence.isIdle` render gracefully (server not yet populating them per `holomush-uhiz`)

## Risks

| Risk | Mitigation |
|---|---|
| E2E selector breakage mid-implementation | `.terminal-layout`, `.presence-list`, `[data-testid="event"]`, `button[title="Toggle sidebar"]` preserved. `.status-bar .character` + `.sidebar.expanded` migrated in same PR (no half-migrations); E2E updates land in step 12. |
| Tailwind v4 `@theme` build-time constraint | Layout tokens on `:root`, density tokens on `.app-root` — both outside `@theme`. Documented in Visual Tokens section. (CLAUDE.md feedback memory) |
| Density toggle regressing existing paddings | New tokens (`--row-py`, `--row-gap`, `--card-pad`) are only consumed by components that opt in. Existing hard-coded paddings are untouched. |
| bits-ui `Resizable` uses proportions in its API | Store pixels in `uiPrefsStore`; convert px↔pct at the component boundary. On mount: `defaultSize = pxToPct(width, container)`. On user resize: `onResize(pct)` → debounced convert → `setSidebarWidthPx`. Use `onResizeEnd` if bits-ui exposes it; otherwise 200ms trailing-edge debounce. `ResizeObserver` on container reapplies pane size on viewport change (never writes to store — avoids overwriting user preference during transient resizes). |
| `just-arrived` animation relying on `:last-child` | Only correct if `.lines` contains only `.line` elements. DOM restructure in step 6 puts the sentinel and LIVE separator *outside* `.lines`. Also requires strict-append semantics — `terminalStore.appendLine()` guarantees this. Note in spec says revisit if out-of-order insertion is ever added. |
| IME composition losing keystrokes to global handler | Hard rule: `handleGlobalKey` first line is `if (event.isComposing \|\| event.keyCode === 229) return;`. Tested in unit tests and E2E via dispatched event with `isComposing: true`. |
| Global keyboard shortcuts conflicting with browser (⌘L, ⌘K) | Every matched combo calls both `preventDefault()` and `stopPropagation()`. Tested by verifying `event.defaultPrevented === true` after dispatch. |
| Composer Esc missed when focus is outside composer | Composer is non-modal — focus can be on drag handle, close button, or elsewhere. Element-scoped keydown listeners miss Esc in those cases. Fix: composer registers a `window`-level keydown listener in an `$effect` gated on `composerOpen`, with `stopPropagation()` so the global layout handler doesn't double-handle. |
| CommandInput autogrow firing on disabled textarea | `scrollHeight` on a `disabled` textarea can be unreliable across browsers and is wasted work while the user cannot type. Autogrow `$effect` guards with `if ($uiPrefs.composerOpen) return;`. |
| Composer + palette z-index / focus interaction | Palette (`cmdk-sv` `Command.Dialog`) takes focus on open and restores it on close. Composer is non-modal; opening palette over composer does not close composer; palette closes returns focus to whatever was focused before open (composer textarea if that was active). |
| OTEL span preservation in page rewrite | `+page.svelte` rewrite preserves the `command.roundtrip` and `stream.lifecycle` span call sites unchanged; verified by the PR-description smoke checklist (browser devtools inspection). |
| SSR / hydration mismatch from localStorage-backed stores | `uiPrefsStore` MUST NOT read localStorage during module initialization. Initial store state = hard-coded defaults (render same on server and client). A post-mount `$effect` in `+layout.svelte` reads localStorage and pushes values into the store after first paint. Accept a tiny first-paint flash of defaults in exchange for zero hydration mismatches. Same pattern for any other persisted UI store. |

## Open Questions

None. All decisions locked in brainstorming session on 2026-04-18.

## References

- Prototype: `/tmp/terminal-handoff/revamp-the-terminal-page-modernize-fancify/project/Terminal hi-fi.html`
- Prototype tokens: `/tmp/terminal-handoff/revamp-the-terminal-page-modernize-fancify/project/assets/colors_and_type.css`
- Current theme redesign: `docs/superpowers/specs/2026-04-02-web-client-theme-redesign.md` (PR #178)
- Web client conventions: `web/CLAUDE.md`
- Follow-ups: `holomush-uhiz` (server optional fields), `holomush-w7t5` (server-side prefs)
