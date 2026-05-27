<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Terminal Hi-Fi Revamp Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Recreate the "Terminal hi-fi" design (handoff from Claude Design) in the HoloMUSH SvelteKit web client — merged 44px topbar, 48px icon rail, card-based sidebar, per-line timestamps, LIVE separator, mode chip, floating composer, `⌘K` command palette — delivered as a single PR.

**Architecture:** Svelte 5 runes + Tailwind v4 + bits-ui + shadcn-svelte + cmdk-sv. All visual values map to existing `--color-*` / `--mush-*` runtime theme variables; layout/density tokens land in `app.css` at `:root` and `.app-root`. Three new stores (`uiPrefsStore`, `commandHistoryStore`, `connectionStore`) with post-mount localStorage hydration for SSR safety. `TerminalView` is restructured so `.lines` contains only `.line` elements, letting a pure-CSS `:last-child` selector drive the just-arrived flash. `CommandPalette` is a thin wrapper over `cmdk-sv` (no hand-rolled focus trap). `Composer` is a non-modal floating `role="region"` mounted at the layout root. Global keyboard listener uses capture-phase, IME-composition guard, and explicit `preventDefault`+`stopPropagation`.

**Tech Stack:** SvelteKit 2, Svelte 5 (runes), Tailwind CSS v4, bits-ui, shadcn-svelte, cmdk-sv, lucide-svelte, ConnectRPC (gRPC-Web), Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-04-18-terminal-hifi-revamp-design.md`

---

## Prerequisites

Before starting any task, verify:

- You are in a jj workspace named `terminal-hifi` (see Task 0).
- `cd web` (all pnpm / pnpm-test commands run in `web/`).
- `task test` passes on `main` before Task 1 begins.

## Task 0: Workspace setup

**Files:** none (VCS-only operation)

- [ ] **Step 1: Detect VCS**

Run: `jj root`
Expected: prints a path ending in `/holomush` (jj repo detected).

- [ ] **Step 2: Fetch latest main**

Run: `jj --no-pager git fetch`
Expected: success (or no-op).

- [ ] **Step 3: Create workspace from main**

Run: `jj --no-pager workspace add /Users/sean/Code/github.com/holomush/.worktrees/terminal-hifi --name terminal-hifi -r main`
Expected: prints `Created workspace ...` and new workspace path.

- [ ] **Step 4: Enter workspace**

Run: `cd /Users/sean/Code/github.com/holomush/.worktrees/terminal-hifi`
Expected: pwd ends in `/terminal-hifi`.

- [ ] **Step 5: Regenerate Go workspace**

Run: `task gowork`
Expected: `go.work` regenerated; no errors.

- [ ] **Step 6: Verify workspace ready**

Run: `jj --no-pager st`
Expected: clean working copy; `@` has no description set.
Run: `cd web && pnpm install`
Expected: `Done in <time>` — dependencies installed.

---

## Task 1: TerminalLine gains `timestamp` field

**Files:**

- Modify: `web/src/lib/stores/terminalStore.ts`
- Modify: `web/src/lib/stores/eventRouter.ts`
- Test: `web/src/lib/stores/terminalStore.test.ts` (new)

**Goal:** `TerminalLine` carries `timestamp: Date` sourced from `GameEvent.timestamp` (Unix ms `bigint`) with `Date.now()` fallback. Must not break existing callers.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/stores/terminalStore.test.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { appendLine, clearLines, lines } from './terminalStore';

describe('terminalStore.appendLine', () => {
  beforeEach(() => clearLines());

  it('sets timestamp from numeric millis when provided', () => {
    const ms = 1713456789000;
    appendLine({ type: 'say', characterName: 'A', text: 'hi' }, false, ms);
    const [line] = get(lines);
    expect(line.timestamp).toBeInstanceOf(Date);
    expect(line.timestamp.getTime()).toBe(ms);
  });

  it('falls back to Date.now() when timestamp is 0', () => {
    const before = Date.now();
    appendLine({ type: 'say', characterName: 'A', text: 'hi' }, false, 0);
    const after = Date.now();
    const [line] = get(lines);
    expect(line.timestamp.getTime()).toBeGreaterThanOrEqual(before);
    expect(line.timestamp.getTime()).toBeLessThanOrEqual(after);
  });

  it('falls back to Date.now() when timestamp is omitted', () => {
    const before = Date.now();
    appendLine({ type: 'say', characterName: 'A', text: 'hi' }, false);
    const after = Date.now();
    const [line] = get(lines);
    expect(line.timestamp.getTime()).toBeGreaterThanOrEqual(before);
    expect(line.timestamp.getTime()).toBeLessThanOrEqual(after);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm vitest run src/lib/stores/terminalStore.test.ts`
Expected: FAIL — `appendLine` doesn't accept a timestamp parameter.

- [ ] **Step 3: Modify `terminalStore.ts`**

Replace lines 6–10 of `web/src/lib/stores/terminalStore.ts`:

```ts
export interface TerminalLine {
  id: string;
  event: { type: string; characterName: string; text: string; channel?: number; metadata?: unknown };
  replayed: boolean;
  timestamp: Date;
}
```

Replace the `appendLine` function (lines 27–37):

```ts
export function appendLine(
  event: TerminalLine['event'],
  replayed: boolean,
  timestampMs?: number,
) {
  const id = `line-${++lineCounter}`;
  const bufferSize = getBufferSize();
  const ms = timestampMs && timestampMs > 0 ? timestampMs : Date.now();
  const timestamp = new Date(ms);
  lines.update((current) => {
    const next = [...current, { id, event, replayed, timestamp }];
    return next.length > bufferSize ? next.slice(next.length - bufferSize) : next;
  });
  if (!get(isAtBottom)) {
    newMessageCount.update((n) => n + 1);
  }
}
```

- [ ] **Step 4: Update `eventRouter.ts` to pass the proto timestamp**

In `web/src/lib/stores/eventRouter.ts`, replace line 21 (inside `routeEvent`):

```ts
  if (target === DISPLAY_TERMINAL || target === DISPLAY_BOTH || target === DISPLAY_UNSPECIFIED) {
    // GameEvent.timestamp is bigint Unix millis; convert to number for Date construction.
    const ms = Number(event.timestamp ?? 0n);
    appendLine(event, replayed, ms);
  }
```

- [ ] **Step 5: Run tests to verify pass**

Run: `cd web && pnpm vitest run src/lib/stores/terminalStore.test.ts`
Expected: PASS — all 3 tests green.

- [ ] **Step 6: Run full test suite to ensure no regressions**

Run: `cd web && pnpm vitest run`
Expected: PASS; no breakage in existing tests.

- [ ] **Step 7: Commit**

Run:

```bash
jj --no-pager commit -m "feat(terminal): add timestamp field to TerminalLine from GameEvent.timestamp"
```

---

## Task 2: Create `uiPrefsStore`

**Files:**

- Create: `web/src/lib/stores/uiPrefsStore.ts`
- Create: `web/src/lib/stores/uiPrefsStore.test.ts`

**Goal:** New store for all UI layout preferences with post-mount localStorage hydration for SSR safety. Includes `railHidden`, `sidebarHidden`, `sidebarWidthPx`, `density`, `composerOpen`, `composerPos`, `composerSize`, `paletteOpen`, `railView`.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/stores/uiPrefsStore.test.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import {
  uiPrefs,
  toggleRail,
  toggleSidebar,
  toggleComposer,
  togglePalette,
  toggleDensity,
  setSidebarWidthPx,
  setComposerPos,
  setComposerSize,
  hydrateUiPrefs,
  resetUiPrefs,
} from './uiPrefsStore';

describe('uiPrefsStore', () => {
  beforeEach(() => {
    localStorage.clear();
    resetUiPrefs();
  });

  it('has sane defaults', () => {
    const p = get(uiPrefs);
    expect(p.railHidden).toBe(false);
    expect(p.sidebarHidden).toBe(false);
    expect(p.sidebarWidthPx).toBe(280);
    expect(p.density).toBe('cozy');
    expect(p.composerOpen).toBe(false);
    expect(p.paletteOpen).toBe(false);
    expect(p.railView).toBe('room');
  });

  it('toggles boolean flags', () => {
    toggleRail();
    expect(get(uiPrefs).railHidden).toBe(true);
    toggleSidebar();
    expect(get(uiPrefs).sidebarHidden).toBe(true);
    toggleComposer();
    expect(get(uiPrefs).composerOpen).toBe(true);
    togglePalette();
    expect(get(uiPrefs).paletteOpen).toBe(true);
  });

  it('toggles density between cozy and compact', () => {
    expect(get(uiPrefs).density).toBe('cozy');
    toggleDensity();
    expect(get(uiPrefs).density).toBe('compact');
    toggleDensity();
    expect(get(uiPrefs).density).toBe('cozy');
  });

  it('clamps sidebarWidthPx to 200-520', () => {
    setSidebarWidthPx(150);
    expect(get(uiPrefs).sidebarWidthPx).toBe(200);
    setSidebarWidthPx(600);
    expect(get(uiPrefs).sidebarWidthPx).toBe(520);
    setSidebarWidthPx(350);
    expect(get(uiPrefs).sidebarWidthPx).toBe(350);
  });

  it('persists composer position and size', () => {
    setComposerPos({ x: 100, y: 200 });
    setComposerSize({ w: 700, h: 400 });
    expect(get(uiPrefs).composerPos).toEqual({ x: 100, y: 200 });
    expect(get(uiPrefs).composerSize).toEqual({ w: 700, h: 400 });
  });

  it('does NOT read localStorage during module init (SSR safety)', () => {
    const spy = vi.spyOn(Storage.prototype, 'getItem');
    resetUiPrefs();
    expect(spy).not.toHaveBeenCalledWith('holomush-ui-prefs');
    spy.mockRestore();
  });

  it('hydrateUiPrefs loads from localStorage and merges with defaults', () => {
    localStorage.setItem(
      'holomush-ui-prefs',
      JSON.stringify({ railHidden: true, sidebarWidthPx: 420, density: 'compact' }),
    );
    hydrateUiPrefs();
    const p = get(uiPrefs);
    expect(p.railHidden).toBe(true);
    expect(p.sidebarWidthPx).toBe(420);
    expect(p.density).toBe('compact');
    // Unspecified fields keep defaults
    expect(p.composerOpen).toBe(false);
    expect(p.railView).toBe('room');
  });

  it('hydrateUiPrefs ignores corrupt localStorage data', () => {
    localStorage.setItem('holomush-ui-prefs', 'not-json');
    hydrateUiPrefs();
    // Defaults stand
    expect(get(uiPrefs).sidebarWidthPx).toBe(280);
  });

  it('write-through: toggles persist to localStorage after hydration', () => {
    hydrateUiPrefs();
    toggleRail();
    const saved = JSON.parse(localStorage.getItem('holomush-ui-prefs') ?? '{}');
    expect(saved.railHidden).toBe(true);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm vitest run src/lib/stores/uiPrefsStore.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Create the store**

Create `web/src/lib/stores/uiPrefsStore.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, get } from 'svelte/store';

export type Density = 'cozy' | 'compact';
export type RailView = 'room';  // future: 'dm' | 'map' | 'notes'

export interface UiPrefs {
  railHidden: boolean;
  sidebarHidden: boolean;
  sidebarWidthPx: number;
  density: Density;
  composerOpen: boolean;
  composerPos: { x: number; y: number };
  composerSize: { w: number; h: number };
  paletteOpen: boolean;
  railView: RailView;
}

const STORAGE_KEY = 'holomush-ui-prefs';

const MIN_WIDTH = 200;
const MAX_WIDTH = 520;
const DEFAULT_WIDTH = 280;

const DEFAULTS: UiPrefs = {
  railHidden: false,
  sidebarHidden: false,
  sidebarWidthPx: DEFAULT_WIDTH,
  density: 'cozy',
  composerOpen: false,
  composerPos: { x: -1, y: -1 },  // -1 = not placed; consumer centers on first open
  composerSize: { w: 640, h: 340 },
  paletteOpen: false,
  railView: 'room',
};

// SSR safety: initial state is the plain defaults. Do not read localStorage
// during module evaluation — it would produce a server/client hydration mismatch.
// Call `hydrateUiPrefs()` from a post-mount `$effect` in +layout.svelte.
export const uiPrefs = writable<UiPrefs>({ ...DEFAULTS });

let hydrated = false;

function clampWidth(px: number): number {
  if (px < MIN_WIDTH) return MIN_WIDTH;
  if (px > MAX_WIDTH) return MAX_WIDTH;
  return px;
}

function persist(prefs: UiPrefs) {
  if (!hydrated || typeof window === 'undefined') return;
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs));
  } catch { /* quota or privacy mode — best effort */ }
}

export function hydrateUiPrefs() {
  if (typeof window === 'undefined') return;
  hydrated = true;
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return;
    const parsed = JSON.parse(raw) as Partial<UiPrefs>;
    uiPrefs.update((current) => ({
      ...current,
      ...parsed,
      sidebarWidthPx: clampWidth(parsed.sidebarWidthPx ?? current.sidebarWidthPx),
    }));
  } catch { /* corrupt or invalid — keep defaults */ }
}

export function resetUiPrefs() {
  hydrated = false;
  uiPrefs.set({ ...DEFAULTS });
}

function mutate(fn: (prefs: UiPrefs) => UiPrefs) {
  uiPrefs.update((current) => {
    const next = fn(current);
    persist(next);
    return next;
  });
}

export const toggleRail = () => mutate((p) => ({ ...p, railHidden: !p.railHidden }));
export const toggleSidebar = () => mutate((p) => ({ ...p, sidebarHidden: !p.sidebarHidden }));
export const toggleComposer = () => mutate((p) => ({ ...p, composerOpen: !p.composerOpen }));
export const togglePalette = () => mutate((p) => ({ ...p, paletteOpen: !p.paletteOpen }));
export const openPalette = () => mutate((p) => ({ ...p, paletteOpen: true }));
export const closePalette = () => mutate((p) => ({ ...p, paletteOpen: false }));
export const openComposer = () => mutate((p) => ({ ...p, composerOpen: true }));
export const closeComposer = () => mutate((p) => ({ ...p, composerOpen: false }));

export const toggleDensity = () =>
  mutate((p) => ({ ...p, density: p.density === 'cozy' ? 'compact' : 'cozy' }));

export const setSidebarWidthPx = (px: number) =>
  mutate((p) => ({ ...p, sidebarWidthPx: clampWidth(px) }));

export const setComposerPos = (pos: { x: number; y: number }) =>
  mutate((p) => ({ ...p, composerPos: pos }));

export const setComposerSize = (size: { w: number; h: number }) =>
  mutate((p) => ({ ...p, composerSize: size }));

// Helper for tests that need to inspect current value synchronously
export const currentUiPrefs = () => get(uiPrefs);
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd web && pnpm vitest run src/lib/stores/uiPrefsStore.test.ts`
Expected: PASS — all tests green.

- [ ] **Step 5: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): add uiPrefsStore for rail/sidebar/density/composer/palette state"
```

---

## Task 3: Create `commandHistoryStore`

**Files:**

- Create: `web/src/lib/stores/commandHistoryStore.ts`
- Create: `web/src/lib/stores/commandHistoryStore.test.ts`

**Goal:** Lift command history out of `CommandInput.svelte` component-local state into a shared store so the sidebar `RecentCommandsCard` can read it. Future home for `⌘R` search state.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/stores/commandHistoryStore.test.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import {
  commandHistory,
  pushCommand,
  navigatePrev,
  navigateNext,
  resetNav,
  seedCommands,
  MAX_HISTORY,
} from './commandHistoryStore';

describe('commandHistoryStore', () => {
  beforeEach(() => {
    commandHistory.set({ entries: [], navIndex: -1 });
  });

  it('starts empty with navIndex=-1', () => {
    const h = get(commandHistory);
    expect(h.entries).toEqual([]);
    expect(h.navIndex).toBe(-1);
  });

  it('pushCommand appends and resets nav', () => {
    pushCommand('look');
    pushCommand('say hi');
    const h = get(commandHistory);
    expect(h.entries).toEqual(['look', 'say hi']);
    expect(h.navIndex).toBe(-1);
  });

  it('dedupes consecutive duplicates', () => {
    pushCommand('look');
    pushCommand('look');
    pushCommand('say hi');
    pushCommand('say hi');
    expect(get(commandHistory).entries).toEqual(['look', 'say hi']);
  });

  it('keeps non-consecutive duplicates', () => {
    pushCommand('look');
    pushCommand('say hi');
    pushCommand('look');
    expect(get(commandHistory).entries).toEqual(['look', 'say hi', 'look']);
  });

  it('ignores empty / whitespace commands', () => {
    pushCommand('');
    pushCommand('   ');
    expect(get(commandHistory).entries).toEqual([]);
  });

  it(`caps entries at MAX_HISTORY (${MAX_HISTORY})`, () => {
    for (let i = 0; i < MAX_HISTORY + 10; i++) pushCommand(`cmd-${i}`);
    const h = get(commandHistory);
    expect(h.entries).toHaveLength(MAX_HISTORY);
    expect(h.entries[0]).toBe('cmd-10');
    expect(h.entries[h.entries.length - 1]).toBe(`cmd-${MAX_HISTORY + 9}`);
  });

  it('navigatePrev walks back through history', () => {
    seedCommands(['a', 'b', 'c']);
    expect(navigatePrev()).toBe('c');
    expect(navigatePrev()).toBe('b');
    expect(navigatePrev()).toBe('a');
    // Beyond oldest: returns null, navIndex stays at last
    expect(navigatePrev()).toBeNull();
    expect(get(commandHistory).navIndex).toBe(2);
  });

  it('navigateNext walks forward and returns empty at end', () => {
    seedCommands(['a', 'b', 'c']);
    navigatePrev(); navigatePrev();  // at 'b'
    expect(navigateNext()).toBe('c');
    // One past newest: returns empty string, resets nav
    expect(navigateNext()).toBe('');
    expect(get(commandHistory).navIndex).toBe(-1);
    // Further next is null
    expect(navigateNext()).toBeNull();
  });

  it('resetNav clears navIndex', () => {
    seedCommands(['a', 'b']);
    navigatePrev();
    resetNav();
    expect(get(commandHistory).navIndex).toBe(-1);
  });

  it('seedCommands replaces entries and resets nav', () => {
    pushCommand('a');
    seedCommands(['x', 'y', 'z']);
    expect(get(commandHistory).entries).toEqual(['x', 'y', 'z']);
    expect(get(commandHistory).navIndex).toBe(-1);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm vitest run src/lib/stores/commandHistoryStore.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Create the store**

Create `web/src/lib/stores/commandHistoryStore.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, get } from 'svelte/store';

export const MAX_HISTORY = 100;

export interface CommandHistoryState {
  entries: string[];
  navIndex: number;  // -1 = not navigating; 0..entries.length-1 = position from newest
}

export const commandHistory = writable<CommandHistoryState>({
  entries: [],
  navIndex: -1,
});

export function pushCommand(cmd: string) {
  const trimmed = cmd.trim();
  if (!trimmed) return;
  commandHistory.update((s) => {
    const entries = [...s.entries];
    if (entries[entries.length - 1] === trimmed) {
      // dedupe consecutive duplicate
      return { entries, navIndex: -1 };
    }
    entries.push(trimmed);
    if (entries.length > MAX_HISTORY) entries.splice(0, entries.length - MAX_HISTORY);
    return { entries, navIndex: -1 };
  });
}

export function seedCommands(entries: string[]) {
  commandHistory.set({
    entries: entries.slice(-MAX_HISTORY),
    navIndex: -1,
  });
}

/**
 * Move one step back in history (toward older entries).
 * Returns the command at the new position, or null if already at oldest.
 */
export function navigatePrev(): string | null {
  const s = get(commandHistory);
  if (s.entries.length === 0) return null;
  const nextIdx = s.navIndex + 1;
  if (nextIdx >= s.entries.length) return null;
  commandHistory.update((prev) => ({ ...prev, navIndex: nextIdx }));
  return s.entries[s.entries.length - 1 - nextIdx];
}

/**
 * Move one step forward (toward newer entries). Returns the command at the
 * new position, empty string at the "past-newest" position (consumer clears
 * the input), or null if not currently navigating.
 */
export function navigateNext(): string | null {
  const s = get(commandHistory);
  if (s.navIndex < 0) return null;
  const nextIdx = s.navIndex - 1;
  if (nextIdx < 0) {
    commandHistory.update((prev) => ({ ...prev, navIndex: -1 }));
    return '';
  }
  commandHistory.update((prev) => ({ ...prev, navIndex: nextIdx }));
  return s.entries[s.entries.length - 1 - nextIdx];
}

export function resetNav() {
  commandHistory.update((s) => ({ ...s, navIndex: -1 }));
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd web && pnpm vitest run src/lib/stores/commandHistoryStore.test.ts`
Expected: PASS — all tests green.

- [ ] **Step 5: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): add commandHistoryStore with dedup, bounded size, and nav helpers"
```

---

## Task 4: Create `connectionStore`

**Files:**

- Create: `web/src/lib/stores/connectionStore.ts`
- Create: `web/src/lib/stores/connectionStore.test.ts`

**Goal:** Named single-source-of-truth for the conn pill status, consumed by the extended `TopBar`.

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/stores/connectionStore.test.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { get } from 'svelte/store';
import { connectionStatus, setConnectionStatus } from './connectionStore';

describe('connectionStore', () => {
  it('defaults to disconnected', () => {
    expect(get(connectionStatus)).toBe('disconnected');
  });

  it('transitions through all states', () => {
    setConnectionStatus('syncing');
    expect(get(connectionStatus)).toBe('syncing');
    setConnectionStatus('connected');
    expect(get(connectionStatus)).toBe('connected');
    setConnectionStatus('disconnected');
    expect(get(connectionStatus)).toBe('disconnected');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd web && pnpm vitest run src/lib/stores/connectionStore.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Create the store**

Create `web/src/lib/stores/connectionStore.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable } from 'svelte/store';

export type ConnectionStatus = 'connected' | 'syncing' | 'disconnected';

export const connectionStatus = writable<ConnectionStatus>('disconnected');

export function setConnectionStatus(status: ConnectionStatus) {
  connectionStatus.set(status);
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `cd web && pnpm vitest run src/lib/stores/connectionStore.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): add connectionStore for TopBar conn pill"
```

---

## Task 5: Layout + density tokens, animations, message classes in `app.css`

**Files:**

- Modify: `web/src/app.css`
- Modify: `web/src/routes/+layout.svelte` (hydrateUiPrefs on mount + density attribute)

**Goal:** Layout/density tokens live outside `@theme` (Tailwind v4 constraint). Global animation keyframes gated on `prefers-reduced-motion`. `.app-root` gets a `data-density` attribute so density tokens cascade.

- [ ] **Step 1: Modify `app.css` — add layout + density + animations at the end**

Append to `web/src/app.css`:

```css
/* Layout tokens — static constants, not themed. Live on :root outside @theme
   because Tailwind v4 @theme compiles var() at build time, and these values
   never change at runtime. */
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

/* Density tokens — runtime-switchable per user via uiPrefsStore.density.
   Scoped to .app-root so future alternate shells (embed/iframe) can render
   without density applied. */
.app-root[data-density="cozy"] {
  --row-py: 6px;
  --row-gap: 8px;
  --card-pad: 12px;
}
.app-root[data-density="compact"] {
  --row-py: 3px;
  --row-gap: 4px;
  --card-pad: 8px;
}

/* Animations — all gated on prefers-reduced-motion so reduced-motion users
   see instant state changes. */
@media (prefers-reduced-motion: no-preference) {
  @keyframes dot-pulse {
    0%, 100% { opacity: 0.5; }
    50% { opacity: 1; }
  }
  @keyframes just-arrived {
    from { background: color-mix(in srgb, var(--color-primary) 12%, transparent); }
    to { background: transparent; }
  }
  @keyframes composer-slide-up {
    from { transform: translateY(16px); opacity: 0; }
    to { transform: none; opacity: 1; }
  }
}
```

- [ ] **Step 2: Modify `+layout.svelte` to hydrate prefs + set density attribute**

Replace lines 5–20 of `web/src/routes/+layout.svelte`:

```ts
  import '../app.css';
  import TopBar from '$lib/components/TopBar.svelte';
  import { initTelemetry, startNavigationSpan, endNavigationSpan } from '$lib/telemetry';
  import { restoreSession } from '$lib/stores/authStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { uiPrefs, hydrateUiPrefs } from '$lib/stores/uiPrefsStore';
  import { beforeNavigate, afterNavigate } from '$app/navigation';
  import { onMount } from 'svelte';

  let { children } = $props();

  onMount(() => {
    initTelemetry();
    restoreSession();
    hydrateUiPrefs();
  });
```

Replace the `.app-root` wrapper (line 31):

```svelte
<div
  class="app-root"
  data-density={$uiPrefs.density}
  style={themeToCssVars($activeTheme.colors)}
>
  <TopBar />
  <main>{@render children()}</main>
</div>
```

- [ ] **Step 3: Verify lint and format**

Run: `cd web && pnpm check`
Expected: no type errors.

Run: `task lint`
Expected: clean.

Run: `task fmt`
Expected: no diffs (or auto-format applied).

- [ ] **Step 4: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): add layout + density + animation tokens to app.css; hydrate uiPrefs on mount"
```

---

## Task 6: Extend `TopBar.svelte` — 44px, terminal-aware extras, delete `StatusBar`

**Files:**

- Modify: `web/src/lib/components/TopBar.svelte`
- Modify: `web/src/routes/(authed)/terminal/+page.svelte` (remove StatusBar import + usage; wire connectionStore)
- Modify: `web/e2e/terminal.spec.ts` (migrate `.status-bar .character` selector)
- Delete: `web/src/lib/components/terminal/StatusBar.svelte`

**Goal:** TopBar grows to 44px. On `/terminal` route it renders breadcrumb, palette hint, conn pill, and a sidebar-toggle button. `StatusBar.svelte` is deleted. E2E assertion migrated.

- [ ] **Step 1: Rewrite `TopBar.svelte`**

Replace `web/src/lib/components/TopBar.svelte` entirely:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { LogOut, ArrowLeftRight, Palette, PanelRightOpen, Command as CommandIcon } from 'lucide-svelte';
  import { page } from '$app/stores';
  import { authState, clearAuth } from '$lib/stores/authStore';
  import {
    activeTheme,
    themePreferences,
    setTheme,
    setTerminalBlackBackground,
    getAvailableThemes,
  } from '$lib/stores/themeStore';
  import { location } from '$lib/stores/sidebarStore';
  import { connectionStatus } from '$lib/stores/connectionStore';
  import { toggleSidebar } from '$lib/stores/uiPrefsStore';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
  import defaultDark from '$lib/theme/default-dark.json';
  import defaultLight from '$lib/theme/default-light.json';
  import classicDark from '$lib/theme/classic-dark.json';
  import classicLight from '$lib/theme/classic-light.json';
  import type { Theme } from '$lib/theme/types';

  const themeData: Record<string, Theme> = {
    'default-dark': defaultDark as Theme,
    'default-light': defaultLight as Theme,
    'classic-dark': classicDark as Theme,
    'classic-light': classicLight as Theme,
  };

  const client = createClient(WebService, transport);
  const availableThemes = getAvailableThemes();

  let themeId = $derived($themePreferences.themeId);
  let onTerminal = $derived($page.route.id?.includes('/terminal') ?? false);

  async function handleLogout() {
    try { await client.webLogout({}); } catch { /* best effort */ }
    clearAuth();
    goto('/');
  }

  function handleSwitchCharacter() { goto('/characters'); }
  function displayName(id: string): string {
    return id.split('-').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
  }
</script>

<header>
  <div class="left">
    <a href="/" class="logo brand-chip">
      <span class="logo-icon">H</span>
      <span class="logo-text">HoloMUSH</span>
    </a>
    {#if onTerminal && $authState.characterName}
      <span class="vdiv" aria-hidden="true"></span>
      <div class="breadcrumb" data-testid="topbar-breadcrumb">
        <span class="char">{$authState.characterName}</span>
        {#if $location?.name}
          <span class="sep">@</span>
          <span class="loc">{$location.name}</span>
          <span class="loc-id">#{$location.id.slice(0, 8)}</span>
        {/if}
      </div>
    {/if}
  </div>
  <nav class="right">
    {#if onTerminal}
      <span class="kbd-hint" aria-hidden="true">
        <CommandIcon size={12} /><kbd>K</kbd> palette
      </span>
      <span
        class="conn-pill"
        data-testid="conn-pill"
        data-status={$connectionStatus}
      >
        <span class="conn-dot" aria-hidden="true"></span>
        {#if $connectionStatus === 'connected'}connected{:else if $connectionStatus === 'syncing'}syncing{:else}disconnected{/if}
      </span>
      <span class="vdiv" aria-hidden="true"></span>
    {/if}

    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}
          <button {...props} class="icon-btn" title="Theme" aria-label="Change theme">
            <Palette size={16} />
          </button>
        {/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end" class="w-56">
        <DropdownMenu.Label>Theme</DropdownMenu.Label>
        <DropdownMenu.Separator />
        <DropdownMenu.RadioGroup value={themeId} onValueChange={(v) => v && setTheme(v)}>
          {#each availableThemes as theme (theme.id)}
            <DropdownMenu.RadioItem value={theme.id}>
              <span class="theme-option">
                <span class="theme-swatches">
                  <span class="swatch" style="background: {themeData[theme.id]?.colors.background ?? '#000'}"></span>
                  <span class="swatch" style="background: {themeData[theme.id]?.colors.primary ?? '#888'}"></span>
                  <span class="swatch" style="background: {themeData[theme.id]?.colors.accent ?? '#888'}"></span>
                </span>
                {displayName(theme.id)}
              </span>
            </DropdownMenu.RadioItem>
          {/each}
        </DropdownMenu.RadioGroup>
        <DropdownMenu.Separator />
        <DropdownMenu.CheckboxItem
          bind:checked={$themePreferences.terminalBlackBackground}
          onCheckedChange={(v) => setTerminalBlackBackground(v === true)}
        >
          Black terminal background
        </DropdownMenu.CheckboxItem>
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    {#if onTerminal}
      <button
        class="icon-btn"
        onclick={toggleSidebar}
        title="Toggle sidebar"
        aria-label="Toggle sidebar"
      >
        <PanelRightOpen size={16} />
      </button>
    {/if}

    {#if !$authState.isPlayerAuthenticated && !$authState.sessionId}
      <a href="/login" class="nav-link">Login</a>
      <a href="/register" class="nav-link accent">Register</a>
    {:else if $authState.sessionId && $authState.characterName}
      <span class="char-name" data-testid="topbar-char-name">{$authState.characterName}</span>
      <button class="icon-btn" onclick={handleSwitchCharacter} title="Switch character" aria-label="Switch character">
        <ArrowLeftRight size={16} />
      </button>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {:else if $authState.isPlayerAuthenticated}
      <span class="player-name">{$authState.playerName}</span>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {/if}
  </nav>
</header>

<style>
  header {
    height: var(--topbar-h);
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-border);
    flex-shrink: 0;
    font-size: 13px;
  }
  .left { display: flex; align-items: center; gap: 10px; }
  .logo { display: flex; align-items: center; gap: 6px; text-decoration: none; color: var(--color-input-text); }
  .logo-icon {
    width: 22px; height: 22px;
    display: flex; align-items: center; justify-content: center;
    background: var(--color-primary); color: var(--color-primary-foreground);
    border-radius: 4px; font-weight: bold; font-size: 12px; flex-shrink: 0;
  }
  .logo-text { color: var(--color-primary); font-weight: 600; letter-spacing: 0.05em; }
  .vdiv {
    width: 1px; height: 20px;
    background: var(--color-border);
  }
  .breadcrumb { display: flex; align-items: center; gap: 6px; font-size: 13px; }
  .breadcrumb .char { color: var(--mush-pose-actor); }
  .breadcrumb .sep { color: var(--color-status-text); }
  .breadcrumb .loc { color: var(--color-input-text); }
  .breadcrumb .loc-id { color: var(--color-status-text); font-size: 11px; font-family: 'JetBrains Mono', monospace; }
  .right { display: flex; align-items: center; gap: 8px; }
  .kbd-hint {
    display: none;
    align-items: center;
    gap: 4px;
    color: var(--color-status-text);
    font-size: 11px;
  }
  @media (min-width: 768px) {
    .kbd-hint { display: inline-flex; }
  }
  .kbd-hint kbd {
    font-family: inherit; font-size: 11px;
    padding: 1px 4px;
    border: 1px solid var(--color-border);
    border-radius: 3px;
  }
  .conn-pill {
    display: inline-flex; align-items: center; gap: 5px;
    padding: 2px 8px;
    border-radius: 999px;
    font-size: 11px;
    background: var(--color-muted);
    color: var(--color-muted-foreground);
  }
  .conn-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--color-status-text); }
  .conn-pill[data-status="connected"] .conn-dot { background: var(--mush-arrive, #66bb6a); }
  .conn-pill[data-status="syncing"] .conn-dot { background: var(--color-accent); animation: dot-pulse 1200ms ease-in-out infinite; }
  .conn-pill[data-status="disconnected"] .conn-dot { background: var(--mush-system, #e57373); }
  .nav-link {
    color: var(--color-input-text); text-decoration: none;
    padding: 2px 8px; border-radius: 4px; border: 1px solid var(--color-border);
    transition: border-color 0.15s;
  }
  .nav-link:hover { border-color: var(--color-primary); }
  .nav-link.accent { background: var(--color-primary); color: var(--color-primary-foreground); border-color: var(--color-primary); }
  .char-name, .player-name { color: var(--color-primary); font-size: 13px; }
  .icon-btn {
    background: none; border: none; cursor: pointer;
    color: var(--color-status-text);
    display: flex; align-items: center; padding: 2px; border-radius: 4px;
    transition: color 0.15s;
  }
  .icon-btn:hover { color: var(--color-input-text); }
  .theme-option { display: flex; align-items: center; gap: 8px; }
  .theme-swatches { display: flex; gap: 2px; }
  .swatch { display: inline-block; width: 12px; height: 12px; border-radius: 2px; border: 1px solid var(--color-border); }
</style>
```

- [ ] **Step 2: Remove `StatusBar` from `+page.svelte`**

In `web/src/routes/(authed)/terminal/+page.svelte`:

- Delete line 19: `import StatusBar from '$lib/components/terminal/StatusBar.svelte';`
- Delete lines 12–13 imports: `toggleSidebar` from sidebarStore (we'll use uiPrefsStore instead; fix in Task 11).
- Delete the `<StatusBar .../>` block (lines 352–358).
- Replace `{#if !isMobile}` block and `{:else}` block as follows (keep the Resizable pattern; remove mobile branch for now — the topbar sidebar-toggle button works everywhere):

Replace lines 351–379:

```svelte
  <div class="terminal-layout" style={$themePreferences.terminalBlackBackground ? terminalBlackOverrideVars() : ''}>
    <Resizable.PaneGroup direction="horizontal" class="main-area">
      <Resizable.Pane defaultSize={75} class="terminal-column">
        <TerminalView />
        <CommandInput {sessionId} onSend={sendCommand} />
      </Resizable.Pane>
      <Resizable.Handle withHandle />
      <Resizable.Pane defaultSize={25}>
        <Sidebar onExitClick={handleExitClick} resizable />
      </Resizable.Pane>
    </Resizable.PaneGroup>
  </div>
```

Remove the `isMobile`-specific mobile overlay branch; mobile handling is now "sidebar hides below 768px" — addressed in Task 10 with a CSS media query in `Sidebar.svelte`. Remove `isMobile` state and `checkMobile()` function entirely — they are no longer needed.

Specifically:

- Delete line 46: `let isMobile = $state(false);`
- In `onMount` (lines 59–76) remove lines 60–61 (`checkMobile();` and `window.addEventListener('resize', checkMobile);`).
- In `onDestroy` (lines 78–90) remove line 79 (`window.removeEventListener('resize', checkMobile);`).
- Delete the `checkMobile()` function (lines 92–94).

- [ ] **Step 3: Wire `connectionStore` in `+page.svelte`**

Add to imports (top of `<script>`):

```ts
  import { setConnectionStatus } from '$lib/stores/connectionStore';
```

Inside `hydrateAndStream()`, set status transitions:

- Right after `clearLines();` (line 137): `setConnectionStatus('syncing');`
- In the Subscribe loop, after handling `REPLAY_COMPLETE` (line 177): add `setConnectionStatus('connected');`
- In the `STREAM_CLOSED` handler (before `goto('/characters')`): add `setConnectionStatus('disconnected');`
- In the catch block where `error = 'Connection lost...'` is set: add `setConnectionStatus('disconnected');`
- In `disconnect()` (line 321), before `goto('/characters')`: add `setConnectionStatus('disconnected');`

- [ ] **Step 4: Delete `StatusBar.svelte`**

Run: `rm web/src/lib/components/terminal/StatusBar.svelte`

- [ ] **Step 5: Migrate the E2E selector**

In `web/e2e/terminal.spec.ts` line 23, change:

```ts
    await expect(page.locator('.status-bar .character')).toContainText(/\w+ \w+/);
```

to:

```ts
    await expect(page.locator('[data-testid="topbar-char-name"]')).toContainText(/\w+ \w+/);
```

In line 117 (`responsive layout hides sidebar on mobile` test), the assertion already uses `button[title="Toggle sidebar"]` — this selector is preserved in the new TopBar (the button with title exists on `/terminal`). Leave the test as-is.

- [ ] **Step 6: Typecheck**

Run: `cd web && pnpm check`
Expected: no type errors.

- [ ] **Step 7: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): extend TopBar to 44px with terminal-aware breadcrumb/conn-pill/sidebar-toggle; delete StatusBar"
```

---

## Task 7: Build `Rail.svelte`, wire into terminal layout

**Files:**

- Create: `web/src/lib/components/terminal/Rail.svelte`
- Modify: `web/src/routes/(authed)/terminal/+page.svelte` (mount `<Rail />` + restructure layout)

**Goal:** 48px left chrome column with Room (active), DM (disabled), Map (disabled), Notes (disabled), Settings (popover). `⌘B` hint at bottom. Collapses to `width: 0` when `uiPrefs.railHidden`.

- [ ] **Step 1: Create `Rail.svelte`**

Create `web/src/lib/components/terminal/Rail.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Home, MessageSquare, Map, NotebookPen, Settings } from 'lucide-svelte';
  import { uiPrefs, toggleDensity } from '$lib/stores/uiPrefsStore';
  import { themePreferences, setTheme, setTerminalBlackBackground, getAvailableThemes } from '$lib/stores/themeStore';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';

  const availableThemes = getAvailableThemes();
  let themeId = $derived($themePreferences.themeId);

  function displayName(id: string): string {
    return id.split('-').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
  }
</script>

<aside class="rail" class:is-hidden={$uiPrefs.railHidden} data-testid="rail" aria-label="Navigation rail">
  <div class="rail-inner">
    <button class="rail-btn is-active" title="Room" aria-label="Room" aria-current="true">
      <Home size={18} />
      <span class="rail-bar" aria-hidden="true"></span>
    </button>
    <button class="rail-btn is-disabled" title="DM (coming soon)" aria-label="DM — coming soon" aria-disabled="true">
      <MessageSquare size={18} />
    </button>
    <button class="rail-btn is-disabled" title="Map (coming soon)" aria-label="Map — coming soon" aria-disabled="true">
      <Map size={18} />
    </button>
    <button class="rail-btn is-disabled" title="Notes (coming soon)" aria-label="Notes — coming soon" aria-disabled="true">
      <NotebookPen size={18} />
    </button>

    <div class="rail-spacer"></div>

    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}
          <button {...props} class="rail-btn" title="Settings" aria-label="Settings">
            <Settings size={18} />
          </button>
        {/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end" side="right" class="w-56">
        <DropdownMenu.Label>Theme</DropdownMenu.Label>
        <DropdownMenu.Separator />
        <DropdownMenu.RadioGroup value={themeId} onValueChange={(v) => v && setTheme(v)}>
          {#each availableThemes as theme (theme.id)}
            <DropdownMenu.RadioItem value={theme.id}>{displayName(theme.id)}</DropdownMenu.RadioItem>
          {/each}
        </DropdownMenu.RadioGroup>
        <DropdownMenu.Separator />
        <DropdownMenu.Label>Density</DropdownMenu.Label>
        <DropdownMenu.CheckboxItem
          checked={$uiPrefs.density === 'compact'}
          onCheckedChange={() => toggleDensity()}
        >
          Compact
        </DropdownMenu.CheckboxItem>
        <DropdownMenu.Separator />
        <DropdownMenu.CheckboxItem
          bind:checked={$themePreferences.terminalBlackBackground}
          onCheckedChange={(v) => setTerminalBlackBackground(v === true)}
        >
          Black terminal background
        </DropdownMenu.CheckboxItem>
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    <div class="rail-hint" aria-hidden="true">
      <kbd>⌘</kbd><kbd>B</kbd>
    </div>
  </div>
</aside>

<style>
  .rail {
    width: var(--rail-w);
    flex-shrink: 0;
    overflow: hidden;
    background: var(--color-sidebar-background);
    border-right: 1px solid var(--color-border);
    transition: width 180ms ease;
  }
  .rail.is-hidden { width: 0; border-right-width: 0; }
  @media (max-width: 767px) {
    .rail:not(.is-hidden) { width: 0; border-right-width: 0; }
  }
  .rail-inner {
    display: flex;
    flex-direction: column;
    align-items: center;
    padding: 6px 0 4px;
    height: 100%;
    gap: 4px;
  }
  .rail-btn {
    width: 36px; height: 36px;
    display: flex; align-items: center; justify-content: center;
    background: none; border: none; border-radius: 6px; cursor: pointer;
    color: var(--color-status-text);
    position: relative;
    transition: background 120ms, color 120ms;
  }
  .rail-btn:hover:not(.is-disabled) {
    background: color-mix(in srgb, var(--color-primary) 10%, transparent);
    color: var(--color-input-text);
  }
  .rail-btn.is-active { color: var(--color-primary); }
  .rail-btn.is-active .rail-bar {
    position: absolute; left: -6px; top: 6px; bottom: 6px;
    width: 2px; background: var(--color-primary); border-radius: 1px;
  }
  .rail-btn.is-disabled { opacity: 0.35; cursor: not-allowed; }
  .rail-spacer { flex: 1; }
  .rail-hint { margin-top: 4px; margin-bottom: 4px; }
  .rail-hint kbd {
    display: inline-block; padding: 1px 3px; font-size: 9px;
    border: 1px solid var(--color-border); border-radius: 3px;
    color: var(--color-status-text);
  }
  .rail-hint kbd + kbd { margin-left: 1px; }
</style>
```

- [ ] **Step 2: Mount `Rail` in `+page.svelte`**

In `web/src/routes/(authed)/terminal/+page.svelte`:

Add to the import block:

```ts
  import Rail from '$lib/components/terminal/Rail.svelte';
```

Replace the `.terminal-layout` block (the one established in Task 6 Step 2):

```svelte
  <div class="terminal-layout" style={$themePreferences.terminalBlackBackground ? terminalBlackOverrideVars() : ''}>
    <Rail />
    <Resizable.PaneGroup direction="horizontal" class="main-area">
      <Resizable.Pane defaultSize={75} class="terminal-column">
        <TerminalView />
        <CommandInput {sessionId} onSend={sendCommand} />
      </Resizable.Pane>
      <Resizable.Handle withHandle />
      <Resizable.Pane defaultSize={25}>
        <Sidebar onExitClick={handleExitClick} resizable />
      </Resizable.Pane>
    </Resizable.PaneGroup>
  </div>
```

Update `.terminal-layout` style rule at the bottom of the `<style>` block:

```css
  .terminal-layout {
    display: flex;
    flex-direction: row;   /* was: column */
    height: calc(100vh - var(--topbar-h));
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 15px;
    background: var(--color-background);
    color: var(--color-input-text);
  }
```

The existing `.main-area` rule can be removed (the `Resizable.PaneGroup` now IS the main area).

- [ ] **Step 3: Typecheck**

Run: `cd web && pnpm check`
Expected: no type errors.

- [ ] **Step 4: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): add Rail.svelte (48px nav rail) with Room/Settings + coming-soon placeholders"
```

---

## Task 8: Rewrite `TerminalView.svelte` — restructure DOM, timestamps, LIVE separator, just-arrived

**Files:**

- Modify: `web/src/lib/components/terminal/TerminalView.svelte`
- Modify: `web/src/lib/components/terminal/EventRenderer.svelte` (no change to wrapper — just make sure `.dimmed` still works via the `replayed` flag)

**Goal:** `.lines` container holds ONLY `.line` elements (sentinel + LIVE separator go outside it). Each line shows its `timestamp` (HH:MM) in a 44px gutter. `just-arrived` flash is `.lines > .line:last-child:not(.replay)`.

- [ ] **Step 1: Rewrite `TerminalView.svelte`**

Replace `web/src/lib/components/terminal/TerminalView.svelte` entirely:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { onMount } from 'svelte';
  import { lines, newMessageCount, isAtBottom, scrolledToBottom, scrolledAway } from '$lib/stores/terminalStore';
  import EventRenderer from './EventRenderer.svelte';

  let scrollContainer: HTMLDivElement;
  let sentinel: HTMLDivElement;
  let observer: IntersectionObserver;

  const fmt = new Intl.DateTimeFormat(undefined, { hour: '2-digit', minute: '2-digit', hour12: false });
  function formatHHMM(d: Date): string { return fmt.format(d); }

  // Index of the first live (non-replayed) line in $lines; -1 if none.
  let liveStartIdx = $derived($lines.findIndex((l) => !l.replayed));
  let hasReplay = $derived($lines.some((l) => l.replayed));
  let hasLive = $derived(liveStartIdx !== -1);

  let replayLines = $derived(hasLive ? $lines.slice(0, liveStartIdx) : $lines.filter((l) => l.replayed));
  let liveLines = $derived(hasLive ? $lines.slice(liveStartIdx) : []);

  onMount(() => {
    observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) scrolledToBottom(); else scrolledAway();
      },
      { root: scrollContainer, threshold: 0, rootMargin: '50px' },
    );
    observer.observe(sentinel);
    return () => observer.disconnect();
  });

  $effect(() => {
    if ($isAtBottom && $lines.length > 0) {
      requestAnimationFrame(() => { sentinel.scrollIntoView({ behavior: 'instant' }); });
    }
  });

  function scrollToBottom() {
    sentinel.scrollIntoView({ behavior: 'smooth' });
    scrolledToBottom();
  }
</script>

<div class="terminal-view" bind:this={scrollContainer}>
  <div class="scrollback">
    {#if hasReplay}
      <div class="lines replay-chunk">
        {#each replayLines as line (line.id)}
          <div class="line replay" data-event-id={line.id}>
            <span class="tstamp">{formatHHMM(line.timestamp)}</span>
            <EventRenderer event={line.event} dimmed={true} />
          </div>
        {/each}
      </div>
    {/if}

    {#if hasReplay && hasLive}
      <div class="sep-live" role="separator" aria-label="Live events begin">
        <span class="dot" aria-hidden="true"></span>
        <span class="label">LIVE</span>
        <span class="gradient-line" aria-hidden="true"></span>
      </div>
    {/if}

    {#if hasLive}
      <div class="lines live-chunk">
        {#each liveLines as line (line.id)}
          <div class="line" data-event-id={line.id}>
            <span class="tstamp">{formatHHMM(line.timestamp)}</span>
            <EventRenderer event={line.event} dimmed={false} />
          </div>
        {/each}
      </div>
    {/if}

    <div class="sentinel" bind:this={sentinel}></div>
  </div>

  {#if $newMessageCount > 0}
    <button class="scroll-indicator" onclick={scrollToBottom}>
      {$newMessageCount} new -- click to scroll down
    </button>
  {/if}
</div>

<style>
  .terminal-view {
    flex: 1;
    overflow-y: auto;
    background: var(--color-background);
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 15px;
    position: relative;
  }
  .scrollback { padding: 8px 12px; }
  .sentinel { height: 1px; }

  .line {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    padding: var(--row-py, 3px) 0;
    line-height: 1.7;
  }
  .line.replay { opacity: 0.45; }
  .tstamp {
    flex-shrink: 0;
    width: 44px;
    color: var(--mush-timestamp, var(--color-status-text));
    font-variant-numeric: tabular-nums;
    font-size: 12px;
    padding-top: 2px;
  }

  @media (prefers-reduced-motion: no-preference) {
    .lines.live-chunk > .line:last-child:not(.replay) {
      animation: just-arrived 600ms ease-out;
    }
  }

  .sep-live {
    display: flex; align-items: center; gap: 8px;
    margin: 10px 0 6px;
    color: var(--color-primary);
    font-size: 10px; letter-spacing: 2px; font-weight: bold;
  }
  .sep-live .dot {
    width: 8px; height: 8px; border-radius: 50%;
    background: var(--color-primary);
    animation: dot-pulse 1200ms ease-in-out infinite;
  }
  .sep-live .label { flex-shrink: 0; }
  .sep-live .gradient-line {
    flex: 1; height: 1px;
    background: linear-gradient(to right, var(--color-primary), transparent);
  }
  @media (prefers-reduced-motion: reduce) {
    .sep-live .dot { animation: none; opacity: 0.8; }
  }

  .scroll-indicator {
    position: sticky; bottom: 0; width: 100%;
    background: var(--color-border);
    color: var(--color-scrollback-indicator);
    border: none; padding: 4px;
    font-size: 12px; cursor: pointer; text-align: center;
  }
</style>
```

- [ ] **Step 2: Typecheck**

Run: `cd web && pnpm check`
Expected: no type errors. Note: the removed `replayActive` import is now unused in this file; that's fine — it's still exported from `terminalStore` and used elsewhere.

- [ ] **Step 3: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): restructure TerminalView — lines container, timestamps, LIVE separator, just-arrived animation"
```

---

## Task 9: Extend `CommandInput.svelte` — mode chip, suspended state, commandHistoryStore

**Files:**

- Modify: `web/src/lib/components/terminal/CommandInput.svelte`
- Create: `web/src/lib/components/terminal/ModeChip.svelte`

**Goal:** Add a colored mode chip based on input prefix, a suspended-state overlay when `uiPrefs.composerOpen`, and switch history management from local state to `commandHistoryStore`.

- [ ] **Step 1: Create `ModeChip.svelte`**

Create `web/src/lib/components/terminal/ModeChip.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  type Mode = 'say' | 'pose' | 'ooc';
  interface Props { mode: Mode; }
  let { mode }: Props = $props();
</script>

<span class="mode-chip" class:mode-say={mode === 'say'} class:mode-pose={mode === 'pose'} class:mode-ooc={mode === 'ooc'}>
  {mode}
</span>

<style>
  .mode-chip {
    display: inline-flex; align-items: center;
    padding: 0 6px;
    border-radius: 999px;
    font-size: 10px; font-weight: bold; letter-spacing: 0.5px;
    text-transform: uppercase;
    flex-shrink: 0;
    line-height: 16px;
    height: 16px;
  }
  .mode-say {
    background: color-mix(in srgb, var(--mush-say-speaker) 20%, transparent);
    color: var(--mush-say-speaker);
  }
  .mode-pose {
    background: color-mix(in srgb, var(--mush-pose-actor) 20%, transparent);
    color: var(--mush-pose-actor);
  }
  .mode-ooc {
    background: color-mix(in srgb, var(--mush-ooc) 20%, transparent);
    color: var(--mush-ooc);
  }
</style>
```

- [ ] **Step 2: Rewrite `CommandInput.svelte`**

Replace `web/src/lib/components/terminal/CommandInput.svelte` entirely:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { createClient } from '@connectrpc/connect';
  import { onDestroy } from 'svelte';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { uiPrefs } from '$lib/stores/uiPrefsStore';
  import {
    pushCommand,
    navigatePrev,
    navigateNext,
    resetNav,
    seedCommands,
  } from '$lib/stores/commandHistoryStore';
  import ModeChip from './ModeChip.svelte';

  interface Props {
    sessionId: string;
    onSend: (command: string) => void;
  }

  let { sessionId, onSend }: Props = $props();

  const DRAFT_KEY_PREFIX = 'holomush-draft:';
  const DRAFT_DEBOUNCE_MS = 500;
  const LINE_HEIGHT_PX = 20;

  let text = $state('');
  let textarea: HTMLTextAreaElement;
  let draftTimer: ReturnType<typeof setTimeout> | undefined;

  const client = createClient(WebService, transport);

  // Derived: mode chip from leading characters
  let modeChip = $derived.by<'say' | 'pose' | 'ooc' | null>(() => {
    const v = text.trimStart();
    if (v.startsWith(':') || v.startsWith('pose ')) return 'pose';
    if (v.startsWith('"') || v.startsWith('say ')) return 'say';
    if (v.startsWith('ooc ')) return 'ooc';
    return null;
  });

  // Derived: line count and near-max flag for composer nudge
  let lineCount = $derived(text === '' ? 1 : text.split('\n').length);
  let nearMax = $derived(lineCount >= 6);

  // Restore draft and load command history when session changes
  $effect(() => {
    clearTimeout(draftTimer);

    if (!sessionId) {
      seedCommands([]);
      text = '';
      return;
    }

    const saved = localStorage.getItem(DRAFT_KEY_PREFIX + sessionId);
    text = saved ?? '';
    requestAnimationFrame(() => {
      autoGrow();
      if (textarea && !$uiPrefs.composerOpen) textarea.focus();
    });

    const captured = sessionId;
    client.getCommandHistory({ sessionId }).then((resp) => {
      if (captured !== sessionId) return;
      seedCommands(resp.commands ?? []);
    }).catch(() => { /* best-effort */ });
  });

  // Debounced save of draft text to localStorage
  $effect(() => {
    const current = text;
    const sid = sessionId;
    if (!sid) {
      clearTimeout(draftTimer);
      return;
    }
    clearTimeout(draftTimer);
    if (current) {
      draftTimer = setTimeout(() => {
        localStorage.setItem(DRAFT_KEY_PREFIX + sid, current);
      }, DRAFT_DEBOUNCE_MS);
    } else {
      localStorage.removeItem(DRAFT_KEY_PREFIX + sid);
    }
  });

  onDestroy(() => clearTimeout(draftTimer));

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      submit();
    } else if (e.key === 'Escape') {
      text = '';
      resetNav();
    } else if (e.key === 'ArrowUp' && !e.shiftKey) {
      const prev = navigatePrev();
      if (prev !== null) {
        text = prev;
        requestAnimationFrame(autoGrow);
      }
      e.preventDefault();
    } else if (e.key === 'ArrowDown' && !e.shiftKey) {
      const next = navigateNext();
      if (next !== null) {
        text = next;
        requestAnimationFrame(autoGrow);
      }
      e.preventDefault();
    }
  }

  function submit() {
    const cmd = text.trim();
    if (!cmd) return;
    pushCommand(cmd);
    text = '';
    clearTimeout(draftTimer);
    if (sessionId) localStorage.removeItem(DRAFT_KEY_PREFIX + sessionId);
    onSend(cmd);
    requestAnimationFrame(() => {
      if (textarea) textarea.style.height = 'auto';
    });
  }

  function autoGrow() {
    // Gate: don't read scrollHeight on a disabled textarea (composer open)
    if ($uiPrefs.composerOpen) return;
    if (!textarea) return;
    textarea.style.height = 'auto';
    const maxLines = parseInt(
      getComputedStyle(textarea).getPropertyValue('--cmd-max-lines') || '8',
      10,
    );
    const maxHeight = (Number.isFinite(maxLines) ? maxLines : 8) * LINE_HEIGHT_PX;
    textarea.style.height = Math.min(textarea.scrollHeight, maxHeight) + 'px';
  }
</script>

<div class="cmd-wrap" class:is-suspended={$uiPrefs.composerOpen} class:is-multiline={lineCount > 1}>
  <span class="cmd-prompt">&gt;</span>
  {#if modeChip}<ModeChip mode={modeChip} />{/if}
  <textarea
    bind:this={textarea}
    bind:value={text}
    onkeydown={handleKeydown}
    oninput={autoGrow}
    rows="1"
    placeholder="Enter command..."
    spellcheck="false"
    autocomplete="off"
    disabled={$uiPrefs.composerOpen}
    aria-disabled={$uiPrefs.composerOpen}
  ></textarea>
  {#if $uiPrefs.composerOpen}
    <div class="suspended-overlay" aria-live="polite">Composer open — input paused</div>
  {/if}
</div>

<div class="cmd-hints">
  <span><kbd>↑↓</kbd> history</span>
  <span><kbd>⇧⏎</kbd> newline</span>
  <span><kbd>Esc</kbd> clear</span>
  <span><kbd>⌘K</kbd> palette</span>
  <span><kbd>⌘B</kbd> rail</span>
  <span><kbd>⌘.</kbd> sidebar</span>
  <span><kbd>⌘⇧E</kbd> composer</span>
  <span class="line-count">{lineCount} line{lineCount === 1 ? '' : 's'}</span>
  {#if nearMax && !$uiPrefs.composerOpen}
    <span class="composer-nudge">Press ⌘⇧E for a bigger editor</span>
  {/if}
</div>

<style>
  .cmd-wrap {
    position: relative;
    display: flex;
    align-items: flex-start;
    gap: 6px;
    padding: 8px 12px;
    background: var(--color-input-background);
    border-top: 1px solid var(--color-border);
  }
  .cmd-prompt { color: var(--color-input-prompt); line-height: 20px; flex-shrink: 0; }
  textarea {
    flex: 1;
    background: transparent;
    border: none; outline: none;
    color: var(--color-input-text);
    font-family: inherit; font-size: inherit;
    resize: none; line-height: 20px;
    overflow-y: auto;
  }
  textarea:disabled { opacity: 0.5; }
  .suspended-overlay {
    position: absolute;
    inset: 0;
    display: flex; align-items: center; justify-content: center;
    color: var(--color-status-text);
    background: color-mix(in srgb, var(--color-background) 50%, transparent);
    font-size: 12px;
    pointer-events: none;
  }
  .cmd-hints {
    padding: 3px 12px;
    font-size: 10px;
    color: var(--color-status-text);
    background: var(--color-background);
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    align-items: center;
  }
  .cmd-hints kbd {
    font-family: inherit; padding: 0 3px;
    border: 1px solid var(--color-border); border-radius: 3px;
    font-size: 9px;
  }
  .line-count { color: var(--color-muted-foreground); }
  .composer-nudge { color: var(--color-primary); }
</style>
```

- [ ] **Step 3: Typecheck and run unit tests**

Run: `cd web && pnpm check`
Expected: no type errors.
Run: `cd web && pnpm vitest run`
Expected: all previously-passing tests still pass.

- [ ] **Step 4: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): CommandInput — mode chip, suspended state, commandHistoryStore wiring"
```

---

## Task 10: Restructure sidebar into cards

**Files:**

- Modify: `web/src/lib/stores/sidebarStore.ts` (add optional fields + remove `sidebarExpanded`)
- Modify: `web/src/lib/components/sidebar/Sidebar.svelte` (cards layout, uiPrefsStore-driven state)
- Modify: `web/src/lib/components/sidebar/RoomInfo.svelte` → rewrite as RoomCard
- Modify: `web/src/lib/components/sidebar/ExitList.svelte` (card wrapper)
- Modify: `web/src/lib/components/sidebar/PresenceList.svelte` (card wrapper with avatar + status)
- Create: `web/src/lib/components/sidebar/RecentCommandsCard.svelte`
- Modify: `web/e2e/terminal.spec.ts` (migrate `.sidebar.expanded` selectors)

**Goal:** Sidebar becomes a scrollable column of cards: Room (primary-tinted) | Exits | Presence | Recent. Width driven by `uiPrefs.sidebarWidthPx`. Collapse via `uiPrefs.sidebarHidden`. `sidebarStore.sidebarExpanded` is removed (replaced by `uiPrefs.sidebarHidden`).

- [ ] **Step 1: Modify `sidebarStore.ts` — remove `sidebarExpanded`, add optional fields**

Replace `web/src/lib/stores/sidebarStore.ts` entirely:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable } from 'svelte/store';

export interface RoomLocation {
  id: string;
  name: string;
  description: string;
  mood?: string;  // optional; populated by server when holomush-uhiz lands
}

export interface RoomExit {
  direction: string;
  name: string;
  locked: boolean;
}

export interface RoomCharacter {
  name: string;
  idle: boolean;
  lastMode?: 'say' | 'pose' | 'ooc' | 'sys';  // optional; holomush-uhiz
  isIdle?: boolean;  // optional; holomush-uhiz — distinct from presence.idle which is per-char timeout
}

export const location = writable<RoomLocation | null>(null);
export const exits = writable<RoomExit[]>([]);
export const presence = writable<RoomCharacter[]>([]);

export function applyLocationState(metadata: Record<string, unknown>) {
  const loc = metadata.location as RoomLocation | undefined;
  if (loc) location.set(loc);
  const ex = metadata.exits as RoomExit[] | undefined;
  if (ex) exits.set(ex);
  const pr = metadata.present as RoomCharacter[] | undefined;
  if (pr) presence.set(pr);
}

export function addPresence(name: string) {
  presence.update((list) => {
    if (!list.some((c) => c.name === name)) {
      return [...list, { name, idle: false }];
    }
    return list;
  });
}

export function removePresence(name: string) {
  presence.update((list) => list.filter((c) => c.name !== name));
}
```

- [ ] **Step 2: Rewrite `RoomInfo.svelte` as card**

Replace `web/src/lib/components/sidebar/RoomInfo.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { location } from '$lib/stores/sidebarStore';
</script>

{#if $location}
  <section class="card room" data-testid="room-card">
    <header class="card-head">
      <h3 class="room-name">{$location.name}</h3>
      <span class="room-id">#{$location.id.slice(0, 8)}</span>
    </header>
    <p class="room-desc">{$location.description}</p>
    {#if $location.mood}<p class="room-mood"><em>{$location.mood}</em></p>{/if}
  </section>
{/if}

<style>
  .card {
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: var(--card-pad, 12px);
    margin-bottom: var(--row-gap, 8px);
  }
  .card.room {
    background: color-mix(in srgb, var(--color-primary) 8%, var(--color-card));
    border-color: color-mix(in srgb, var(--color-primary) 30%, var(--color-border));
  }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 8px; margin-bottom: 6px; }
  .room-name { font-size: 14px; font-weight: 600; color: var(--color-input-text); margin: 0; }
  .room-id { font-size: 10px; color: var(--color-status-text); font-family: 'JetBrains Mono', monospace; }
  .room-desc { font-size: 11px; color: var(--color-status-text); line-height: 1.5; margin: 0; }
  .room-mood { font-size: 11px; color: var(--color-muted-foreground); margin: 6px 0 0; }
</style>
```

- [ ] **Step 3: Rewrite `ExitList.svelte` as card**

Replace `web/src/lib/components/sidebar/ExitList.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { exits } from '$lib/stores/sidebarStore';

  interface Props { onExitClick: (direction: string) => void; }
  let { onExitClick }: Props = $props();
</script>

{#if $exits.length > 0}
  <section class="card" data-testid="exits-card">
    <header class="card-head">
      <h3>Exits</h3>
      <span class="count">{$exits.length}</span>
    </header>
    <ul class="exit-list">
      {#each $exits as exit}
        <li class="exit-row" class:locked={exit.locked}>
          <button
            class="exit-btn"
            onclick={() => !exit.locked && onExitClick(exit.direction)}
            disabled={exit.locked}
            aria-disabled={exit.locked}
          >
            <span class="arrow" aria-hidden="true">→</span>
            <span class="dir">{exit.direction}</span>
            <span class="loc">{exit.name}</span>
            {#if exit.locked}<span class="k" title="Locked" aria-label="Locked">🔒</span>{/if}
          </button>
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .card {
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: var(--card-pad, 12px);
    margin-bottom: var(--row-gap, 8px);
  }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 6px; }
  .card-head h3 { font-size: 11px; font-weight: 600; color: var(--color-input-prompt); letter-spacing: 1px; text-transform: uppercase; margin: 0; }
  .count { font-size: 10px; color: var(--color-status-text); }
  .exit-list { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 2px; }
  .exit-btn {
    display: flex; align-items: center; gap: 6px;
    width: 100%;
    padding: var(--row-py, 3px) 4px;
    background: none; border: none; border-radius: 4px;
    color: var(--mush-pose-actor);
    font-family: inherit; font-size: 12px;
    cursor: pointer; text-align: left;
  }
  .exit-btn:not(:disabled):hover {
    background: color-mix(in srgb, var(--color-primary) 8%, transparent);
  }
  .exit-row.locked .exit-btn { opacity: 0.45; cursor: not-allowed; }
  .arrow { color: var(--color-status-text); }
  .dir { color: var(--color-primary); font-weight: 600; }
  .loc { color: var(--color-input-text); flex: 1; }
  .k { font-size: 10px; }
</style>
```

- [ ] **Step 4: Rewrite `PresenceList.svelte` as card with avatars**

Replace `web/src/lib/components/sidebar/PresenceList.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { presence } from '$lib/stores/sidebarStore';

  function initials(name: string): string {
    const parts = name.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase();
    return name.slice(0, 2).toUpperCase();
  }
</script>

{#if $presence.length > 0}
  <section class="card presence-list" data-testid="presence-card">
    <header class="card-head">
      <h3>Present</h3>
      <span class="count">{$presence.length}</span>
    </header>
    <ul class="rows">
      {#each $presence as char}
        <li class="pres-row" class:is-idle={char.isIdle ?? char.idle}>
          <span class="avatar {char.lastMode ?? 'sys'}" aria-hidden="true">{initials(char.name)}</span>
          <span class="name">{char.name}</span>
          <span class="status-dot" aria-hidden="true"></span>
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .card {
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: var(--card-pad, 12px);
    margin-bottom: var(--row-gap, 8px);
  }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; margin-bottom: 6px; }
  .card-head h3 { font-size: 11px; font-weight: 600; color: var(--color-input-prompt); letter-spacing: 1px; text-transform: uppercase; margin: 0; }
  .count { font-size: 10px; color: var(--color-status-text); }
  .rows { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 2px; }
  .pres-row {
    display: flex; align-items: center; gap: 8px;
    padding: var(--row-py, 3px) 4px;
    font-size: 12px;
  }
  .pres-row.is-idle { opacity: 0.5; filter: grayscale(0.5); }
  .avatar {
    width: 22px; height: 22px; border-radius: 50%;
    display: flex; align-items: center; justify-content: center;
    font-size: 9px; font-weight: bold;
    background: var(--color-muted);
    color: var(--color-muted-foreground);
    flex-shrink: 0;
  }
  .avatar.say { background: color-mix(in srgb, var(--mush-say-speaker) 25%, transparent); color: var(--mush-say-speaker); }
  .avatar.pose { background: color-mix(in srgb, var(--mush-pose-actor) 25%, transparent); color: var(--mush-pose-actor); }
  .avatar.ooc { background: color-mix(in srgb, var(--mush-ooc) 25%, transparent); color: var(--mush-ooc); }
  .avatar.sys { background: var(--color-muted); color: var(--color-muted-foreground); }
  .name { flex: 1; color: var(--color-input-text); }
  .status-dot {
    width: 6px; height: 6px; border-radius: 50%;
    background: var(--mush-arrive, #66bb6a);
  }
  .pres-row.is-idle .status-dot { background: var(--color-status-text); }
</style>
```

- [ ] **Step 5: Create `RecentCommandsCard.svelte`**

Create `web/src/lib/components/sidebar/RecentCommandsCard.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { commandHistory } from '$lib/stores/commandHistoryStore';

  interface Props {
    onInject: (cmd: string) => void;
  }
  let { onInject }: Props = $props();

  const MAX_SHOWN = 8;
  let recent = $derived($commandHistory.entries.slice(-MAX_SHOWN).reverse());
</script>

{#if recent.length > 0}
  <section class="card" data-testid="recent-card">
    <header class="card-head">
      <h3>Recent</h3>
    </header>
    <ul class="rows">
      {#each recent as cmd}
        <li>
          <button class="recent-btn" onclick={() => onInject(cmd)} title="Inject into input">
            <code>{cmd}</code>
          </button>
        </li>
      {/each}
    </ul>
  </section>
{/if}

<style>
  .card {
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 6px;
    padding: var(--card-pad, 12px);
    margin-bottom: var(--row-gap, 8px);
  }
  .card-head h3 { font-size: 11px; font-weight: 600; color: var(--color-input-prompt); letter-spacing: 1px; text-transform: uppercase; margin: 0 0 6px; }
  .rows { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 1px; }
  .recent-btn {
    display: block; width: 100%;
    background: none; border: none; border-radius: 3px;
    padding: var(--row-py, 3px) 4px;
    text-align: left; cursor: pointer;
  }
  .recent-btn:hover { background: color-mix(in srgb, var(--color-primary) 8%, transparent); }
  .recent-btn code {
    font-family: 'JetBrains Mono', monospace;
    font-size: 11px;
    color: var(--color-input-text);
  }
</style>
```

- [ ] **Step 6: Rewrite `Sidebar.svelte`**

Replace `web/src/lib/components/sidebar/Sidebar.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { uiPrefs } from '$lib/stores/uiPrefsStore';
  import RoomInfo from './RoomInfo.svelte';
  import ExitList from './ExitList.svelte';
  import PresenceList from './PresenceList.svelte';
  import RecentCommandsCard from './RecentCommandsCard.svelte';

  interface Props {
    onExitClick: (direction: string) => void;
    onInject: (cmd: string) => void;
    /** kept for compat with the page's existing `resizable` prop; not used */
    resizable?: boolean;
  }

  let { onExitClick, onInject, resizable = false }: Props = $props();

  // Expose a stable testid with a boolean attribute for E2E assertions
  let expandedAttr = $derived(!$uiPrefs.sidebarHidden);
</script>

<aside
  class="sidebar"
  class:is-hidden={$uiPrefs.sidebarHidden}
  data-testid="sidebar"
  data-expanded={expandedAttr}
  aria-label="Room sidebar"
>
  <div class="sidebar-content">
    <RoomInfo />
    <ExitList {onExitClick} />
    <PresenceList />
    <RecentCommandsCard {onInject} />
  </div>
</aside>

<style>
  .sidebar {
    height: 100%;
    background: var(--color-sidebar-background);
    border-left: 1px solid var(--color-border);
    display: flex;
    flex-direction: column;
    transition: width 180ms ease;
    overflow: hidden;
  }
  .sidebar.is-hidden { width: 0; border-left-width: 0; }
  @media (max-width: 767px) {
    .sidebar { width: 0; border-left-width: 0; }
  }
  .sidebar-content {
    flex: 1;
    padding: 8px;
    overflow-y: auto;
    font-size: 12px;
  }
</style>
```

- [ ] **Step 7: Update `+page.svelte` to pass `onInject`**

In `web/src/routes/(authed)/terminal/+page.svelte`:

Add a helper that injects a command into the CommandInput's draft (via the existing shared localStorage key, so the inline input picks it up when it next re-reads). The simplest approach is to expose a small ref — but since `CommandInput` owns its own state, the cleanest way is to route injection through a store event. For this PR, use a `writable` for "inject request" that `CommandInput` subscribes to:

**Correction (avoid new store):** pass injection via the parent — `+page.svelte` owns a `$state` for injection, passes it to `CommandInput` via a prop. But `CommandInput` takes its draft from its own `$state`; we need the injection to land there.

Simplest: add an `injectText: string | null` prop to `CommandInput`, and a `$effect` inside that writes to `text` when it changes, then calls back `onInjectConsumed()` to clear the parent state.

Modify `CommandInput.svelte` to accept an inject prop:

```ts
  interface Props {
    sessionId: string;
    onSend: (command: string) => void;
    injectText?: string;
    onInjectConsumed?: () => void;
  }
  let { sessionId, onSend, injectText, onInjectConsumed }: Props = $props();

  $effect(() => {
    if (injectText !== undefined && injectText !== '') {
      text = injectText;
      onInjectConsumed?.();
      requestAnimationFrame(() => {
        autoGrow();
        if (textarea && !$uiPrefs.composerOpen) textarea.focus();
      });
    }
  });
```

In `+page.svelte`, add:

```ts
  let injectText = $state<string | undefined>(undefined);
  function handleInject(cmd: string) { injectText = cmd; }
  function handleInjectConsumed() { injectText = undefined; }
```

And update the component instances:

```svelte
        <CommandInput
          {sessionId}
          onSend={sendCommand}
          {injectText}
          onInjectConsumed={handleInjectConsumed}
        />
        ...
        <Sidebar onExitClick={handleExitClick} onInject={handleInject} resizable />
```

- [ ] **Step 8: Migrate E2E selectors**

In `web/e2e/terminal.spec.ts`, replace lines 105–112 of the `sidebar toggles with Ctrl+B` test:

```ts
  test('sidebar toggles with Ctrl+B', async ({ page }) => {
    await connectAsGuest(page);
    await expect(page.locator('[data-testid="sidebar"][data-expanded="true"]')).toBeVisible();
    await page.keyboard.press('Control+b');
    await expect(page.locator('[data-testid="sidebar"][data-expanded="false"]')).toBeAttached();
    await page.keyboard.press('Control+b');
    await expect(page.locator('[data-testid="sidebar"][data-expanded="true"]')).toBeVisible();
  });
```

(The `.presence-list` selector used by the `presence list shows self and other connections` test is preserved — kept as a class on the PresenceCard `<section>`. No change needed there.)

- [ ] **Step 9: Typecheck + tests**

Run: `cd web && pnpm check`
Expected: no type errors.
Run: `cd web && pnpm vitest run`
Expected: all unit tests pass.

- [ ] **Step 10: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): restructure sidebar into cards (Room/Exits/Presence/Recent); wire uiPrefs"
```

---

## Task 11: Wire sidebar width to `uiPrefs.sidebarWidthPx` via bits-ui Resizable

**Files:**

- Modify: `web/src/routes/(authed)/terminal/+page.svelte`

**Goal:** Sidebar pane uses `sidebarWidthPx` from store, with px↔pct conversion at component boundary + `ResizeObserver` on the pane-group container for viewport-resize handling.

- [ ] **Step 1: Modify `+page.svelte` — convert px↔pct and wire `onResize`**

In `web/src/routes/(authed)/terminal/+page.svelte`, add imports:

```ts
  import { uiPrefs, setSidebarWidthPx } from '$lib/stores/uiPrefsStore';
```

Add state for the pane-group container width and an initial proportion derived from the stored px:

```ts
  let paneGroupEl: HTMLElement | undefined = $state(undefined);
  let containerWidth = $state(0);

  function pctFromPx(px: number, cw: number): number {
    if (cw <= 0) return 25;  // sane fallback
    return Math.min(Math.max((px / cw) * 100, 5), 60);
  }

  // Debounce sidebar-width writes to store
  let widthCommitTimer: ReturnType<typeof setTimeout> | undefined;
  function handleSidebarResize(pct: number) {
    clearTimeout(widthCommitTimer);
    widthCommitTimer = setTimeout(() => {
      if (containerWidth > 0) setSidebarWidthPx(Math.round((pct / 100) * containerWidth));
    }, 200);
  }

  $effect(() => {
    if (!paneGroupEl) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) containerWidth = e.contentRect.width;
    });
    ro.observe(paneGroupEl);
    return () => ro.disconnect();
  });

  let sidebarDefaultPct = $derived(pctFromPx($uiPrefs.sidebarWidthPx, containerWidth || 1120));
```

Update the `.terminal-layout` block to bind the pane-group element and use the derived default size:

```svelte
  <div class="terminal-layout" style={$themePreferences.terminalBlackBackground ? terminalBlackOverrideVars() : ''}>
    <Rail />
    <Resizable.PaneGroup
      direction="horizontal"
      class="main-area"
      bind:el={paneGroupEl}
    >
      <Resizable.Pane defaultSize={100 - sidebarDefaultPct} class="terminal-column">
        <TerminalView />
        <CommandInput
          {sessionId}
          onSend={sendCommand}
          {injectText}
          onInjectConsumed={handleInjectConsumed}
        />
      </Resizable.Pane>
      <Resizable.Handle withHandle />
      <Resizable.Pane
        defaultSize={sidebarDefaultPct}
        onResize={handleSidebarResize}
      >
        <Sidebar onExitClick={handleExitClick} onInject={handleInject} resizable />
      </Resizable.Pane>
    </Resizable.PaneGroup>
  </div>
```

Note: the exact `Resizable.PaneGroup` API for `bind:el` or the callback name for resize events depends on the shadcn-svelte version. If `bind:el` is unsupported, bind a local ref: add `class="main-area"` + `this:bind={paneGroupEl}` on a wrapping div and measure that. Verify API with `pnpm check` in the next step.

- [ ] **Step 2: Verify API with typecheck**

Run: `cd web && pnpm check`
Expected: If any type errors around `Resizable` props, consult `node_modules/@shadcn-svelte/ui/resizable` or `bits-ui` types to pick the correct prop names (likely `onResize` or `onSizeChange`). Adjust the component props to match.

- [ ] **Step 3: Sanity test — does the dev server render?**

Run: `cd web && pnpm dev` (background)
Wait 3s, then open http://localhost:5173 manually or leave it — this step is a sanity check, not automated.

Stop the dev server (Ctrl-C).

- [ ] **Step 4: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): wire sidebar width to uiPrefs.sidebarWidthPx with ResizeObserver-based px↔pct"
```

---

## Task 12: Build `Composer.svelte` (non-modal floating panel)

**Files:**

- Create: `web/src/lib/components/terminal/Composer.svelte`
- Modify: `web/src/routes/+layout.svelte` (mount Composer portal at layout root)

**Goal:** Floating panel at `+layout.svelte` root with `position: fixed`. Non-modal (`role="region"`, no `aria-modal`). Receives `draft` + `ondraftChange` + `onsubmit` props. Composer-scoped window-level Esc listener gated on `composerOpen`. Drag header / resize bottom-right. Persist pos+size to `uiPrefs`.

- [ ] **Step 1: Create `Composer.svelte`**

Create `web/src/lib/components/terminal/Composer.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { X } from 'lucide-svelte';
  import {
    uiPrefs,
    closeComposer,
    setComposerPos,
    setComposerSize,
  } from '$lib/stores/uiPrefsStore';

  interface Props {
    draft: string;
    ondraftChange: (text: string) => void;
    onsubmit: (text: string) => void;
  }
  let { draft, ondraftChange, onsubmit }: Props = $props();

  let textareaEl: HTMLTextAreaElement | undefined = $state(undefined);
  let panelEl: HTMLElement | undefined = $state(undefined);

  // Local mirror of draft — bound to textarea. Re-synced from prop on prop change.
  let localText = $state('');
  $effect(() => { localText = draft; });

  let pos = $state({ x: 0, y: 0 });
  let size = $state({ w: 640, h: 340 });

  // Initialize position/size on open
  $effect(() => {
    if (!$uiPrefs.composerOpen) return;
    const stored = $uiPrefs.composerPos;
    const storedSize = $uiPrefs.composerSize;
    if (stored.x < 0 || stored.y < 0) {
      // Center on first open
      pos = {
        x: Math.max(0, (window.innerWidth - storedSize.w) / 2),
        y: Math.max(0, (window.innerHeight - storedSize.h) / 2),
      };
    } else {
      pos = { ...stored };
    }
    size = { ...storedSize };
    requestAnimationFrame(() => textareaEl?.focus());
  });

  // Composer-scoped window Esc listener — gated on composerOpen so
  // the global layout handler never sees Esc while composer is open,
  // even if focus has escaped the panel.
  $effect(() => {
    if (!$uiPrefs.composerOpen) return;
    function onEsc(e: KeyboardEvent) {
      if (e.key !== 'Escape') return;
      if (e.isComposing) return;
      e.preventDefault();
      e.stopPropagation();
      closeComposer();
    }
    window.addEventListener('keydown', onEsc, true);
    return () => window.removeEventListener('keydown', onEsc, true);
  });

  function onTextInput() {
    ondraftChange(localText);
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.isComposing) return;
    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      const cmd = localText.trim();
      if (!cmd) return;
      onsubmit(cmd);
      ondraftChange('');
      closeComposer();
    }
  }

  // Drag
  let dragStart: { x: number; y: number; origX: number; origY: number } | null = null;
  function onHeaderPointerDown(e: PointerEvent) {
    if ((e.target as HTMLElement).closest('.cclose')) return;
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    dragStart = { x: e.clientX, y: e.clientY, origX: pos.x, origY: pos.y };
  }
  function onHeaderPointerMove(e: PointerEvent) {
    if (!dragStart) return;
    const nx = dragStart.origX + (e.clientX - dragStart.x);
    const ny = dragStart.origY + (e.clientY - dragStart.y);
    pos = {
      x: Math.max(-size.w + 80, Math.min(window.innerWidth - 80, nx)),
      y: Math.max(0, Math.min(window.innerHeight - 40, ny)),
    };
  }
  function onHeaderPointerUp() {
    if (!dragStart) return;
    dragStart = null;
    setComposerPos({ ...pos });
  }

  // Resize
  let resizeStart: { x: number; y: number; origW: number; origH: number } | null = null;
  function onHandlePointerDown(e: PointerEvent) {
    e.stopPropagation();
    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
    resizeStart = { x: e.clientX, y: e.clientY, origW: size.w, origH: size.h };
  }
  function onHandlePointerMove(e: PointerEvent) {
    if (!resizeStart) return;
    const nw = resizeStart.origW + (e.clientX - resizeStart.x);
    const nh = resizeStart.origH + (e.clientY - resizeStart.y);
    size = {
      w: Math.max(360, Math.min(window.innerWidth - 40, nw)),
      h: Math.max(200, Math.min(window.innerHeight - 40, nh)),
    };
  }
  function onHandlePointerUp() {
    if (!resizeStart) return;
    resizeStart = null;
    setComposerSize({ ...size });
  }

  let charCount = $derived(localText.length);
  let lineCount = $derived(localText === '' ? 1 : localText.split('\n').length);
</script>

{#if $uiPrefs.composerOpen}
  <section
    bind:this={panelEl}
    class="composer"
    role="region"
    aria-label="Command composer"
    style="left: {pos.x}px; top: {pos.y}px; width: {size.w}px; height: {size.h}px"
  >
    <header
      class="chead"
      onpointerdown={onHeaderPointerDown}
      onpointermove={onHeaderPointerMove}
      onpointerup={onHeaderPointerUp}
      oncontextmenu={(e) => e.preventDefault()}
    >
      <span class="ctitle">Composer</span>
      <span class="cmeta">{charCount} char{charCount === 1 ? '' : 's'} · {lineCount} line{lineCount === 1 ? '' : 's'}</span>
      <button class="cclose" onclick={closeComposer} aria-label="Close composer"><X size={14} /></button>
    </header>
    <textarea
      bind:this={textareaEl}
      bind:value={localText}
      oninput={onTextInput}
      onkeydown={onKeydown}
      class="ctextarea"
      spellcheck="false"
      autocomplete="off"
    ></textarea>
    <footer class="cfoot">
      <kbd>⌘⏎</kbd> send · <kbd>Esc</kbd> close · <kbd>⇧⏎</kbd> newline
    </footer>
    <button
      class="resize-handle"
      aria-label="Resize composer"
      onpointerdown={onHandlePointerDown}
      onpointermove={onHandlePointerMove}
      onpointerup={onHandlePointerUp}
    ></button>
  </section>
{/if}

<style>
  .composer {
    position: fixed;
    z-index: 100;
    display: flex; flex-direction: column;
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    box-shadow: 0 8px 32px rgba(0,0,0,0.4);
    overflow: hidden;
  }
  @media (prefers-reduced-motion: no-preference) {
    .composer { animation: composer-slide-up 180ms ease-out; }
  }
  .chead {
    display: flex; align-items: center; gap: 8px;
    padding: 6px 10px;
    background: var(--color-muted);
    border-bottom: 1px solid var(--color-border);
    cursor: move;
    user-select: none;
    font-size: 12px;
  }
  .ctitle { font-weight: 600; color: var(--color-input-text); }
  .cmeta { flex: 1; color: var(--color-status-text); font-size: 11px; }
  .cclose {
    background: none; border: none; cursor: pointer;
    color: var(--color-status-text); padding: 2px; border-radius: 4px;
  }
  .cclose:hover { color: var(--color-input-text); background: color-mix(in srgb, var(--color-primary) 10%, transparent); }
  .ctextarea {
    flex: 1;
    padding: 10px 12px;
    background: var(--color-input-background);
    border: none; outline: none;
    color: var(--color-input-text);
    font-family: 'JetBrains Mono', 'Fira Code', 'SF Mono', monospace;
    font-size: 14px;
    line-height: 1.5;
    resize: none;
  }
  .cfoot {
    padding: 4px 10px;
    background: var(--color-muted);
    border-top: 1px solid var(--color-border);
    font-size: 10px;
    color: var(--color-status-text);
  }
  .cfoot kbd {
    font-family: inherit;
    padding: 0 3px;
    border: 1px solid var(--color-border); border-radius: 3px;
    font-size: 9px;
  }
  .resize-handle {
    position: absolute;
    right: 0; bottom: 0;
    width: 14px; height: 14px;
    background: none; border: none;
    cursor: nwse-resize;
  }
  .resize-handle::before {
    content: '';
    position: absolute; right: 3px; bottom: 3px;
    width: 8px; height: 8px;
    border-right: 2px solid var(--color-border);
    border-bottom: 2px solid var(--color-border);
  }
</style>
```

- [ ] **Step 2: Mount `Composer` in `+layout.svelte`**

Modify `web/src/routes/+layout.svelte`. Add imports:

```ts
  import Composer from '$lib/components/terminal/Composer.svelte';
  import { writable, get } from 'svelte/store';
```

The composer needs access to the current draft + send callback, but those live on the terminal page. Use a simple bridge store so page-level code can register a send callback + draft bridge. Add this bridge:

```ts
  // Composer bridge — the terminal page registers its draft + submit handlers
  // here. When no page registers, composer has no effect (noop callbacks).
  const composerDraft = writable<string>('');
  const composerSubmit = writable<((cmd: string) => void) | null>(null);
  function handleDraftChange(t: string) { composerDraft.set(t); }
  function handleComposerSubmit(t: string) { get(composerSubmit)?.(t); }
```

Export these so the terminal page can use them — add to a new file instead (keep the bridge discoverable). Create `web/src/lib/stores/composerBridge.ts`:

```ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, get } from 'svelte/store';

export const composerDraft = writable<string>('');
export const composerSubmit = writable<((cmd: string) => void) | null>(null);

export function setComposerDraft(text: string) { composerDraft.set(text); }
export function registerComposerSubmit(fn: ((cmd: string) => void) | null) {
  composerSubmit.set(fn);
}
export function invokeComposerSubmit(cmd: string) {
  const fn = get(composerSubmit);
  if (fn) fn(cmd);
}
```

Simplify `+layout.svelte` mount — replace the added imports with:

```ts
  import Composer from '$lib/components/terminal/Composer.svelte';
  import { composerDraft, setComposerDraft, invokeComposerSubmit } from '$lib/stores/composerBridge';
```

Place the `<Composer />` tag inside the `.app-root` div (after `<main>...</main>`):

```svelte
<div
  class="app-root"
  data-density={$uiPrefs.density}
  style={themeToCssVars($activeTheme.colors)}
>
  <TopBar />
  <main>{@render children()}</main>
  <Composer
    draft={$composerDraft}
    ondraftChange={setComposerDraft}
    onsubmit={invokeComposerSubmit}
  />
</div>
```

- [ ] **Step 3: Wire terminal page to bridge**

In `web/src/routes/(authed)/terminal/+page.svelte`:

Add imports:

```ts
  import {
    composerDraft,
    setComposerDraft,
    registerComposerSubmit,
  } from '$lib/stores/composerBridge';
```

In `onMount`:

```ts
    // Register composer's submit path to use the same sendCommand used by the inline input
    registerComposerSubmit((cmd) => {
      pushCommand(cmd);
      sendCommand(cmd);
    });
```

Import `pushCommand`:

```ts
  import { pushCommand } from '$lib/stores/commandHistoryStore';
```

In `onDestroy`:

```ts
    registerComposerSubmit(null);
```

**Mirror draft between inline input and composer.** The inline textarea's `text` is the canonical state while the composer is closed. When composer opens, `$composerDraft` should seed from `text`; while composer is open, `text` stays frozen (textarea is disabled). When composer closes, `text = $composerDraft`.

Add this effect to `+page.svelte` outside of `onMount` (inside `<script>` top-level):

```ts
  let wasComposerOpen = $state(false);
  $effect(() => {
    const isOpen = $uiPrefs.composerOpen;
    if (isOpen && !wasComposerOpen) {
      // Seed composer draft from injectText pathway using CommandInput's current
      // localStorage state. Read from localStorage directly — CommandInput writes
      // debounced to the same key.
      if (sessionId) {
        const saved = localStorage.getItem(`holomush-draft:${sessionId}`) ?? '';
        setComposerDraft(saved);
      }
    } else if (!isOpen && wasComposerOpen) {
      // On close, route current composer draft back into CommandInput via the
      // injectText prop (existing mechanism from Task 10).
      injectText = $composerDraft;
    }
    wasComposerOpen = isOpen;
  });
```

Add `uiPrefs` import if not already present:

```ts
  import { uiPrefs } from '$lib/stores/uiPrefsStore';
```

- [ ] **Step 4: Typecheck**

Run: `cd web && pnpm check`
Expected: no type errors.

- [ ] **Step 5: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): add Composer (non-modal floating panel) with draft bridge + drag/resize"
```

---

## Task 13: Build `CommandPalette.svelte` on cmdk-sv

**Files:**

- Create: `web/src/lib/components/terminal/CommandPalette.svelte`
- Modify: `web/src/routes/+layout.svelte` (mount palette)
- Install: `cmdk-sv`

**Goal:** `⌘K` palette built on `cmdk-sv` — no hand-rolled focus trap, arrow nav, or filtering. Static item registry of 10 UI actions.

- [ ] **Step 1: Install cmdk-sv**

Run: `cd web && pnpm add cmdk-sv`
Expected: added successfully.

- [ ] **Step 2: Create `CommandPalette.svelte`**

Create `web/src/lib/components/terminal/CommandPalette.svelte`:

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { Command } from 'cmdk-sv';
  import {
    uiPrefs,
    closePalette,
    toggleRail,
    toggleSidebar,
    toggleComposer,
    toggleDensity,
  } from '$lib/stores/uiPrefsStore';
  import { clearLines } from '$lib/stores/terminalStore';
  import {
    themePreferences,
    setTheme,
    setTerminalBlackBackground,
  } from '$lib/stores/themeStore';
  import { clearAuth } from '$lib/stores/authStore';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';

  const client = createClient(WebService, transport);

  interface PaletteItem {
    id: string;
    label: string;
    hint?: string;
    run: () => void | Promise<void>;
  }

  async function signOut() {
    try { await client.webLogout({}); } catch { /* best effort */ }
    clearAuth();
    goto('/');
  }

  const items: PaletteItem[] = [
    { id: 'theme.default-dark',   label: 'Switch theme: Default Dark',   run: () => setTheme('default-dark') },
    { id: 'theme.default-light',  label: 'Switch theme: Default Light',  run: () => setTheme('default-light') },
    { id: 'theme.classic-dark',   label: 'Switch theme: Classic Dark',   run: () => setTheme('classic-dark') },
    { id: 'theme.classic-light',  label: 'Switch theme: Classic Light',  run: () => setTheme('classic-light') },
    { id: 'ui.rail',              label: 'Toggle rail',                  hint: '⌘B',  run: toggleRail },
    { id: 'ui.sidebar',           label: 'Toggle sidebar',               hint: '⌘.',  run: toggleSidebar },
    { id: 'ui.composer',          label: 'Toggle composer',              hint: '⌘⇧E', run: toggleComposer },
    { id: 'ui.density',           label: 'Toggle density (cozy/compact)',               run: toggleDensity },
    { id: 'ui.term-black',        label: 'Toggle black terminal background',            run: () => setTerminalBlackBackground(!$themePreferences.terminalBlackBackground) },
    { id: 'term.clear',           label: 'Clear terminal',               hint: '⌘L',  run: clearLines },
    { id: 'auth.sign-out',        label: 'Sign out',                                   run: signOut },
  ];

  function runAndClose(item: PaletteItem) {
    item.run();
    closePalette();
  }
</script>

<Command.Dialog
  bind:open={$uiPrefs.paletteOpen}
  label="Command palette"
  onOpenChange={(open: boolean) => { if (!open) closePalette(); }}
>
  <Command.Input placeholder="Type a command…" />
  <Command.List>
    <Command.Empty>No matches</Command.Empty>
    {#each items as item (item.id)}
      <Command.Item value={item.label} onSelect={() => runAndClose(item)}>
        <span class="pl-label">{item.label}</span>
        {#if item.hint}<kbd class="pl-hint">{item.hint}</kbd>{/if}
      </Command.Item>
    {/each}
  </Command.List>
</Command.Dialog>

<style>
  :global([data-cmdk-dialog]) {
    position: fixed;
    inset: 0;
    z-index: 200;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding-top: 15vh;
    background: rgba(0,0,0,0.4);
  }
  :global([data-cmdk-root]) {
    width: min(560px, 92vw);
    background: var(--color-card);
    border: 1px solid var(--color-border);
    border-radius: 8px;
    box-shadow: 0 16px 48px rgba(0,0,0,0.5);
    overflow: hidden;
    font-family: inherit;
  }
  :global([data-cmdk-input]) {
    width: 100%;
    padding: 12px 14px;
    background: transparent;
    border: none; outline: none;
    color: var(--color-input-text);
    font-family: inherit;
    font-size: 14px;
    border-bottom: 1px solid var(--color-border);
  }
  :global([data-cmdk-list]) {
    max-height: 320px;
    overflow-y: auto;
    padding: 4px;
  }
  :global([data-cmdk-item]) {
    display: flex; align-items: center; justify-content: space-between;
    gap: 8px;
    padding: 8px 10px;
    border-radius: 4px;
    color: var(--color-input-text);
    font-size: 13px;
    cursor: pointer;
  }
  :global([data-cmdk-item][data-selected="true"]) {
    background: color-mix(in srgb, var(--color-primary) 18%, transparent);
  }
  :global([data-cmdk-empty]) {
    padding: 16px;
    text-align: center;
    color: var(--color-status-text);
    font-size: 12px;
  }
  .pl-hint {
    font-family: inherit; font-size: 10px;
    padding: 1px 5px;
    border: 1px solid var(--color-border); border-radius: 3px;
    color: var(--color-status-text);
  }
</style>
```

- [ ] **Step 3: Mount palette in `+layout.svelte`**

In `web/src/routes/+layout.svelte`:

```ts
  import CommandPalette from '$lib/components/terminal/CommandPalette.svelte';
```

Add the element inside `.app-root` (after `<Composer />`):

```svelte
  <CommandPalette />
```

- [ ] **Step 4: Typecheck**

Run: `cd web && pnpm check`
Expected: no type errors. If `cmdk-sv` types surface prop-name differences (e.g., `onOpenChange` vs `onValueChange`), adjust to match the installed version's types.

- [ ] **Step 5: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): add CommandPalette on cmdk-sv with UI-action registry"
```

---

## Task 14: Global keyboard handler in `+layout.svelte`

**Files:**

- Modify: `web/src/routes/+layout.svelte`
- Modify: `web/src/routes/(authed)/terminal/+page.svelte` (remove the existing `onKeydown` that handled Ctrl+B / Ctrl+L)

**Goal:** Single `window`-level keydown listener with `capture: true`, IME guard, explicit `preventDefault`+`stopPropagation` on every match. Covers `⌘K`, `⌘B`, `⌘.`, `⌘⇧E`, `⌘L`.

- [ ] **Step 1: Add handler to `+layout.svelte`**

In `web/src/routes/+layout.svelte`, add imports:

```ts
  import {
    toggleRail,
    toggleSidebar,
    toggleComposer,
    togglePalette,
  } from '$lib/stores/uiPrefsStore';
  import { clearLines } from '$lib/stores/terminalStore';
```

Add the handler inside the component script (before `onMount`):

```ts
  function handleGlobalKey(e: KeyboardEvent) {
    // IME composition guard — MUST be first. CJK/Japanese/Korean input uses
    // composition events; treating every keystroke as a shortcut would eat
    // in-progress text.
    if (e.isComposing || e.keyCode === 229) return;

    const mod = e.metaKey || e.ctrlKey;
    if (!mod && e.key !== 'Escape') return;

    // Palette
    if (mod && e.key === 'k' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      togglePalette();
      return;
    }
    // Rail
    if (mod && e.key === 'b' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      toggleRail();
      return;
    }
    // Sidebar
    if (mod && e.key === '.' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      toggleSidebar();
      return;
    }
    // Composer
    if (mod && e.shiftKey && (e.key === 'E' || e.key === 'e')) {
      e.preventDefault();
      e.stopPropagation();
      toggleComposer();
      return;
    }
    // Clear terminal
    if (mod && e.key === 'l' && !e.shiftKey) {
      e.preventDefault();
      e.stopPropagation();
      clearLines();
      return;
    }
    // Esc: no-op at this level — palette is handled by cmdk-sv, composer by
    // its own window listener (both installed with capture:true and fire
    // before this handler), and CommandInput's local Esc clears its draft.
  }
```

Update `onMount`:

```ts
  onMount(() => {
    initTelemetry();
    restoreSession();
    hydrateUiPrefs();
    window.addEventListener('keydown', handleGlobalKey, { capture: true });
    return () => window.removeEventListener('keydown', handleGlobalKey, { capture: true });
  });
```

- [ ] **Step 2: Remove old handler from `+page.svelte`**

In `web/src/routes/(authed)/terminal/+page.svelte`:

- Delete the `onKeydown` function (lines 48–57).
- In `onMount`, delete `window.addEventListener('keydown', onKeydown);` (line 62).
- In `onDestroy`, delete `window.removeEventListener('keydown', onKeydown);` (line 80).

Also delete the old `toggleSidebar` import from `$lib/stores/sidebarStore` (it's replaced by the `uiPrefsStore` version already used via the global handler and `TopBar`).

- [ ] **Step 3: Typecheck**

Run: `cd web && pnpm check`
Expected: no type errors; no unused-import warnings.

- [ ] **Step 4: Commit**

Run:

```bash
jj --no-pager commit -m "feat(web): global keyboard handler with IME guard for ⌘K/B/./L + ⌘⇧E"
```

---

## Task 15: Apply `.hm-*` message classes and ensure message rendering still works

**Files:**

- (Spec reference — the `.hm-*` classes existed already in the `@media` section of `app.css` as part of the theme system. Verify that existing renderers already use `--mush-*` tokens and no further work is needed.)
- Modify if needed: `web/src/lib/components/terminal/CommunicationRenderer.svelte`, `MovementRenderer.svelte`, `CommandRenderer.svelte`, `SystemRenderer.svelte`

**Goal:** Existing message renderers already apply theme-token classes (`.speaker`, `.speech`, `.actor`, `.action`, `.ooc-*`, etc. — verified in the Exploration during planning). The spec's `.hm-*` naming is stylistic — the EXISTING class names already reference `--mush-*` tokens correctly, so no rename is required. This task is a no-op verification + optional cleanup.

- [ ] **Step 1: Verify no renderer changes are needed**

Run: `cd web && pnpm vitest run`
Expected: all existing tests pass.

Run: `cd web && pnpm dev` (background)
Open http://localhost:5173/terminal manually; verify message colors render correctly with each theme (spot-check: say, pose, ooc, system, command-error).

Stop the dev server.

- [ ] **Step 2: No commit (nothing to commit)**

If any cleanup emerges from the visual check, commit with a descriptive message. Otherwise skip.

---

## Task 16: Update E2E tests — new scenarios

**Files:**

- Modify: `web/e2e/terminal.spec.ts`

**Goal:** Add E2E coverage for palette (`⌘K`), rail (`⌘B`), composer (`⌘⇧E`), mode chip, timestamps, LIVE separator, IME guard.

- [ ] **Step 1: Add new scenarios**

Append these tests to `web/e2e/terminal.spec.ts` inside the existing `test.describe('Terminal UI', ...)` block (before the closing `});`):

```ts
  test('Cmd+K opens palette, Escape closes it', async ({ page }) => {
    await connectAsGuest(page);
    await page.keyboard.press('ControlOrMeta+k');
    await expect(page.locator('[data-cmdk-dialog]')).toBeVisible({ timeout: 3000 });
    // Type to filter
    await page.keyboard.type('theme');
    await expect(page.locator('[data-cmdk-item]').first()).toContainText(/theme/i);
    await page.keyboard.press('Escape');
    await expect(page.locator('[data-cmdk-dialog]')).toBeHidden();
  });

  test('Cmd+B toggles rail visibility', async ({ page }) => {
    await connectAsGuest(page);
    const rail = page.locator('[data-testid="rail"]');
    await expect(rail).toHaveClass(/(?!is-hidden).*rail/);
    await page.keyboard.press('ControlOrMeta+b');
    await expect(rail).toHaveClass(/is-hidden/);
    await page.keyboard.press('ControlOrMeta+b');
    await expect(rail).not.toHaveClass(/is-hidden/);
  });

  test('Cmd+Shift+E opens composer; text mirrors inline input', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea').first();
    await input.fill('partial pose from inline');
    await page.waitForTimeout(700);  // allow draft debounce
    await page.keyboard.press('ControlOrMeta+Shift+KeyE');
    const composer = page.locator('[role="region"][aria-label="Command composer"]');
    await expect(composer).toBeVisible();
    // Composer textarea should see the draft
    const composerTA = composer.locator('textarea');
    await expect(composerTA).toHaveValue('partial pose from inline');
    // Esc closes composer
    await page.keyboard.press('Escape');
    await expect(composer).toBeHidden();
  });

  test('mode chip appears for say/pose/ooc prefixes', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea').first();
    await input.fill(': smiles');
    await expect(page.locator('.mode-chip')).toContainText(/pose/i);
    await input.fill('say hello');
    await expect(page.locator('.mode-chip')).toContainText(/say/i);
    await input.fill('ooc brb');
    await expect(page.locator('.mode-chip')).toContainText(/ooc/i);
    await input.fill('look');
    await expect(page.locator('.mode-chip')).toHaveCount(0);
  });

  test('timestamps render on terminal lines', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea').first();
    const token = `ts-${Date.now()}`;
    await input.fill(`say ${token}`);
    await input.press('Enter');
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: token }),
    ).toBeVisible({ timeout: 10000 });
    // Each line has a .tstamp in HH:MM form
    const tstamp = page.locator('.line .tstamp').first();
    await expect(tstamp).toBeVisible();
    await expect(tstamp).toHaveText(/^\d{2}:\d{2}$/);
  });

  test('IME composition does not trigger global shortcuts', async ({ page }) => {
    await connectAsGuest(page);
    // Dispatch a synthesized keydown with isComposing=true for the palette shortcut.
    await page.evaluate(() => {
      const ev = new KeyboardEvent('keydown', {
        key: 'k', code: 'KeyK', metaKey: true, ctrlKey: true, isComposing: true,
        bubbles: true, cancelable: true,
      });
      window.dispatchEvent(ev);
    });
    // Palette must not open
    await expect(page.locator('[data-cmdk-dialog]')).toBeHidden();
  });
```

- [ ] **Step 2: Run integration tests**

Run: `task test:int`
Expected: PASS. If any test fails, investigate and fix root cause before proceeding.

- [ ] **Step 3: Commit**

Run:

```bash
jj --no-pager commit -m "test(web): E2E scenarios for palette/rail/composer/mode chip/timestamps/IME"
```

---

## Task 17: Run full pr-prep gate

**Files:** none (verification gate)

- [ ] **Step 1: Run `task pr-prep`**

Run: `task pr-prep`
Expected: PASS — all CI jobs green (lint, format, schema, license, unit, integration, E2E).

If anything fails, fix the root cause and re-run. Do NOT push to a PR branch until green (project rule — see CLAUDE.md feedback memory).

- [ ] **Step 2: PR-description smoke checklist (manual, not a gate)**

When opening the PR, capture:

- Screenshots for each theme × density combination (4 themes × 2 densities = 8 images) on `/terminal`
- Short video/GIF of `⌘K`, `⌘B`, `⌘.`, `⌘⇧E` working
- Confirmation that `command.roundtrip` and `stream.lifecycle` OTEL spans still appear in browser devtools
- Confirmation that `undefined` values for `location.mood`, `presence.lastMode`, `presence.isIdle` render cleanly (the server doesn't populate them yet — `holomush-uhiz`)
- Confirmation that a page reload restores the last rail/sidebar/density/composer state

Attach to the PR body.

---

## Task 18: Open PR and address review

**Files:** none

- [ ] **Step 1: Bookmark + push**

Run:

```bash
jj --no-pager bookmark create terminal-hifi -r @
jj --no-pager git push -b terminal-hifi
```

- [ ] **Step 2: Open PR**

Run:

```bash
gh pr create --title "feat(web): terminal hi-fi revamp — 44px topbar, rail, cards, composer, palette" --body "$(cat <<'EOF'
## Summary

- Delivers the full "Terminal hi-fi" design from the handoff bundle
- Merged 44px topbar with breadcrumb + conn pill + palette hint + sidebar toggle
- New 48px icon rail with Room (active) + DM/Map/Notes (disabled placeholders) + Settings popover
- TerminalView restructured: `.lines` container, per-line HH:MM timestamps, animated LIVE separator, pure-CSS just-arrived flash
- Card-based sidebar (Room / Exits / Presence / Recent)
- Floating non-modal Composer (role="region") with drag + resize + ⌘⏎ submit
- Command palette on cmdk-sv with 11 UI actions
- Global keyboard: ⌘K (palette), ⌘B (rail), ⌘. (sidebar), ⌘⇧E (composer), ⌘L (clear) — all with IME guard + explicit preventDefault
- Three new stores (uiPrefsStore, commandHistoryStore, connectionStore) with post-mount hydration for SSR safety

Spec: docs/superpowers/specs/2026-04-18-terminal-hifi-revamp-design.md

## Test plan

- [x] Unit tests for all new stores
- [x] E2E: palette, rail, composer, mode chip, timestamps, IME guard
- [x] `task pr-prep` green
- [ ] Manual: screenshot matrix (4 themes × 2 densities), see PR description smoke checklist

## Follow-ups

- holomush-uhiz — server-side population of location.mood, presence.lastMode, presence.isIdle
- New bead for ⌘R reverse-i-search history overlay (to be filed)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 3: Invoke review**

Run the review skill (not a bash command):
`/pr-review-toolkit:review-pr`

Wait for findings; address each. Do NOT mark complete until review is clean.

- [ ] **Step 4: File ⌘R follow-up bead**

Run:

```bash
bd create "⌘R reverse-i-search overlay for command history" \
  --description="Terminal hi-fi revamp (PR forthcoming) ships with commandHistoryStore but does NOT include an interactive history search. Design: an inline overlay activated by ⌘R (or Ctrl+R on non-macOS) that lets users filter and jump to a prior command. Lives on commandHistoryStore. Discovered during terminal hi-fi brainstorming 2026-04-18 — deferred to keep the hi-fi PR focused on visual revamp." \
  -t feature -p 3 --json
```

---

## Self-Review Checklist

Before declaring this plan complete, verify:

- [x] **Spec coverage.** Every locked decision (1–9 in the spec) maps to tasks:
  - Decision 1 (Scope C): Tasks 5, 7, 12, 13 deliver visuals + palette + composer + density; tweaks + latency ms not implemented (non-goal).
  - Decision 2 (Chrome A): Task 6 extends TopBar, deletes StatusBar.
  - Decision 3 (Rail): Task 7 builds Rail with DM/Map/Notes disabled.
  - Decision 4 (Server timestamps): Task 1 plumbs `event.timestamp` through `TerminalLine`.
  - Decision 5 (uiPrefsStore new): Task 2.
  - Decision 6 (Composer draft mirror on open/close, submit-and-close): Task 12 implements open-seed + close-restore via the composerBridge.
  - Decision 7 (Palette = UI actions only): Task 13.
  - Decision 8 (commandHistoryStore new): Task 3.
  - Decision 9 (single PR, bottom-up): Plan is one PR, 15 implementation tasks.
- [x] **No placeholders.** Spot-checked — all code blocks contain complete, runnable code. No "TBD", "implement later", "handle edge cases" phrases.
- [x] **Type consistency.** `TerminalLine.timestamp: Date` in Task 1 is read by `TerminalView` in Task 8. `uiPrefs.composerOpen` set in Task 2 read in Tasks 9, 12, 14. `commandHistory` shape in Task 3 consumed in Tasks 9 (via nav helpers), 10 (RecentCommandsCard), 18 (pushCommand from composer bridge).
- [x] **OTEL spans.** Preserved — Task 6 and Task 11 do not modify `hydrateAndStream()`'s span bodies or `sendCommand`'s span usage. Confirmed at Task 17 smoke.
- [x] **SSR safety.** Task 2 keeps localStorage reads out of module init; Task 5 hydrates from a post-mount `$effect` in `+layout.svelte`. No hydration mismatch.
- [x] **E2E selectors.** Task 6 migrates `.status-bar .character`; Task 10 migrates `.sidebar.expanded`; both in the same PR. `.presence-list`, `.terminal-layout`, `[data-testid="event"]`, `button[title="Toggle sidebar"]` all preserved.
- [x] **PR gate.** Task 17 runs full `task pr-prep` before push.

## Execution Handoff

Plan complete and saved to `docs/superpowers/plans/2026-04-18-terminal-hifi-revamp.md`. Two execution options:

1. **Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach?
