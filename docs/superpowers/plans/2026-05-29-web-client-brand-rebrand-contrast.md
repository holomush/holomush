<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Web Client Brand Rebrand + AA Contrast Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-skin the web client's two default themes to the holographic-terminal brand palette (cyan/ink), relocate the brown/warm palettes to `warm-*` themes, and meet WCAG AA 4.5:1 on all four themes — closing holomush-6siys, holomush-1hgk, and holomush-vhsz.

**Architecture:** Theme colors live in `web/src/lib/theme/*.json`, mapped to `--color-*`/`--mush-*` CSS custom properties by `themeToCssVars()` and applied as inline style on `.app-root`. `web/src/app.css` `@theme` carries build-time statics (default-dark). The rebrand is driven from the JSON token tables; a new pure-vitest contrast test enforces AA across all themes. Two coordinated fixes are CSS-mechanism changes in terminal components.

**Tech Stack:** SvelteKit 2 / Svelte 5, Tailwind CSS v4 (`@theme`), vitest, TypeScript.

**Spec:** `docs/superpowers/specs/2026-05-29-web-client-brand-rebrand-contrast-design.md` (design bead holomush-9ektq).

**Working dir:** jj workspace `/Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand`. All `pnpm`/test commands run from `web/`.

---

## File structure

| File | Responsibility | Change |
|------|----------------|--------|
| `web/src/lib/theme/types.ts` | `ThemeColors` contract | add `foreground`, `input`, `cursor`, `scrollback.replayed` keys |
| `web/src/lib/theme/default-dark.json` | brand dark | rewrite → cyan/ink |
| `web/src/lib/theme/default-light.json` | brand light | rewrite → cyan-on-light |
| `web/src/lib/theme/warm-dark.json` | warm dark alternate | new file (relocated brown), replaces `classic-dark.json` |
| `web/src/lib/theme/warm-light.json` | warm light alternate | new file (relocated cream), replaces `classic-light.json` |
| `web/src/lib/stores/themeStore.ts` | theme registry + prefs | rename ids, add `classic-*`→`warm-*` migration |
| `web/src/lib/components/terminal/CommandPalette.svelte` | theme-switch palette commands | rename ids/labels `:44-47` |
| `web/src/app.css` | `@theme` build-time statics | brand-dark values + new tokens |
| `web/src/lib/components/terminal/CommandInput.svelte` | command input + hint bar | caret-color amber; vhsz font-size |
| `web/src/lib/components/terminal/TerminalView.svelte` | scrollback rendering | 1hgk: opacity → `--color-scrollback-replayed` |
| `web/src/lib/components/terminal/EventRenderer.svelte` | per-event renderer | 1hgk: remove `.dimmed` opacity |
| `web/src/lib/stores/themeStore.test.ts` | theme tests | add contrast + brand-guard + migration tests |

**Token-vs-type decision (resolves spec reconciliation note):** `foreground` and `input` are promoted to themed `ThemeColors` keys (light and dark need different foregrounds; a single `@theme` static cannot serve both). `themeToCssVars()` already maps any non-MUSH key to `--color-<key>`, so no store-logic change is needed for them — only the type, the JSONs, and the `@theme` defaults.

---

### Task 1: Promote/add the four new theme token keys to the type contract

**Files:**

- Modify: `web/src/lib/theme/types.ts:18-30`

- [ ] **Step 1: Add the keys to `ThemeColors`**

In `web/src/lib/theme/types.ts`, replace the "Chrome tokens (structural UI)" block (lines 18-30) with:

```typescript
  // Chrome tokens (structural UI)
  background: string;
  foreground: string;
  input: string;
  surface: string;
  border: string;
  cursor: string;
  'input.prompt': string;
  'input.text': string;
  'input.background': string;
  'status.text': string;
  'status.background': string;
  'status.online': string;
  'status.offline': string;
  'sidebar.background': string;
  'scrollback.indicator': string;
  'scrollback.replayed': string;
```

- [ ] **Step 2: Run the type check to verify it now fails (JSONs missing keys)**

Run: `cd web && pnpm check`
Expected: FAIL — the four `*.json` files cast `as Theme` are now missing `foreground`, `input`, `cursor`, `scrollback.replayed`. (Svelte-check surfaces the missing-property errors on the JSON imports.) This failure is expected and is fixed by Tasks 2-6.

- [ ] **Step 3: Commit**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "feat(web): add foreground/input/cursor/scrollback.replayed theme tokens (holomush-9ektq)"`

---

### Task 2: Relocate warm themes + rename theme ids + localStorage migration (D3, D9)

**Files:**

- Create: `web/src/lib/theme/warm-dark.json` (content = current `default-dark.json` brown palette + new tokens)
- Create: `web/src/lib/theme/warm-light.json` (content = current `default-light.json` cream palette + new tokens)
- Delete: `web/src/lib/theme/classic-dark.json`, `web/src/lib/theme/classic-light.json`
- Modify: `web/src/lib/stores/themeStore.ts:6-16,28-54`
- Modify: `web/src/lib/components/terminal/CommandPalette.svelte:44-47`
- Modify: `web/src/lib/components/TopBar.svelte:24-35` (duplicate static theme registry)
- Modify: `web/src/lib/stores/themeStore.test.ts:35-45`

- [ ] **Step 1: Create `warm-dark.json`** (brown palette relocated; `status.text`/`muted.foreground` raised to AA `#9a8f7e`; new tokens added)

```json
{
  "name": "warm-dark",
  "colors": {
    "say.speaker": "#4fc3f7",
    "say.speech": "#ffffff",
    "pose.actor": "#81c784",
    "pose.action": "#aaaaaa",
    "system": "#ffb74d",
    "arrive": "#888888",
    "leave": "#888888",
    "command.output": "#e0e0e0",
    "command.error": "#e57373",
    "ooc": "#9575cd",
    "pemit": "#80cbc4",
    "background": "#1a1210",
    "foreground": "#e0d6cf",
    "input": "#3e2a1a",
    "surface": "#1e1612",
    "border": "#3e2a1a",
    "cursor": "#ffb74d",
    "input.prompt": "#ff7043",
    "input.text": "#e0d6cf",
    "input.background": "#16100c",
    "status.text": "#9a8f7e",
    "status.background": "#1e1612",
    "status.online": "#66bb6a",
    "status.offline": "#e57373",
    "sidebar.background": "#1e1612",
    "scrollback.indicator": "#ffb74d",
    "scrollback.replayed": "#9a8f7e",
    "primary": "#ff7043",
    "primary.foreground": "#ffffff",
    "secondary": "#2a1e16",
    "secondary.foreground": "#e0d6cf",
    "muted": "#2a1e16",
    "muted.foreground": "#9a8f7e",
    "accent": "#ffb74d",
    "accent.foreground": "#1a1210",
    "destructive": "#e57373",
    "destructive.foreground": "#ffffff",
    "card": "#1e1612",
    "card.foreground": "#e0d6cf",
    "popover": "#1e1612",
    "popover.foreground": "#e0d6cf",
    "ring": "#ff7043",
    "radius": "0.5rem"
  }
}
```

- [ ] **Step 2: Create `warm-light.json`** (cream palette relocated; `muted.foreground #6b6055` already AA; new tokens added)

```json
{
  "name": "warm-light",
  "colors": {
    "say.speaker": "#0268a8",
    "say.speech": "#1a1a1a",
    "pose.actor": "#2e7d32",
    "pose.action": "#666666",
    "system": "#b34700",
    "arrive": "#616161",
    "leave": "#616161",
    "command.output": "#1a1a1a",
    "command.error": "#c62828",
    "ooc": "#6a1b9a",
    "pemit": "#00695c",
    "background": "#faf5f0",
    "foreground": "#1a1510",
    "input": "#d4c4b0",
    "surface": "#f0ebe5",
    "border": "#d4c4b0",
    "cursor": "#c75c00",
    "input.prompt": "#e64a19",
    "input.text": "#1a1510",
    "input.background": "#fffcf8",
    "status.text": "#6b6055",
    "status.background": "#f0ebe5",
    "status.online": "#2e7d32",
    "status.offline": "#c62828",
    "sidebar.background": "#f5f0ea",
    "scrollback.indicator": "#e65100",
    "scrollback.replayed": "#6b6055",
    "primary": "#e64a19",
    "primary.foreground": "#ffffff",
    "secondary": "#d4c4b0",
    "secondary.foreground": "#1a1510",
    "muted": "#d4c4b0",
    "muted.foreground": "#6b6055",
    "accent": "#f57c00",
    "accent.foreground": "#ffffff",
    "destructive": "#c62828",
    "destructive.foreground": "#ffffff",
    "card": "#f0ebe5",
    "card.foreground": "#1a1510",
    "popover": "#f0ebe5",
    "popover.foreground": "#1a1510",
    "ring": "#e64a19",
    "radius": "0.5rem"
  }
}
```

- [ ] **Step 3: Delete the old classic files**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && rm web/src/lib/theme/classic-dark.json web/src/lib/theme/classic-light.json`

- [ ] **Step 4: Update `themeStore.ts` imports, registry, and add migration**

Replace lines 6-16 of `web/src/lib/stores/themeStore.ts`:

```typescript
import defaultDark from '$lib/theme/default-dark.json';
import defaultLight from '$lib/theme/default-light.json';
import warmDark from '$lib/theme/warm-dark.json';
import warmLight from '$lib/theme/warm-light.json';

const themes: Record<string, Theme> = {
  'default-dark': defaultDark as Theme,
  'default-light': defaultLight as Theme,
  'warm-dark': warmDark as Theme,
  'warm-light': warmLight as Theme,
};

// Renamed theme ids (holomush-9ektq D3): classic-* → warm-*.
const RENAMED_THEME_IDS: Record<string, string> = {
  'classic-dark': 'warm-dark',
  'classic-light': 'warm-light',
};
```

Then in `loadPreferences()` (lines 32-41), apply the rename when reading saved prefs. Replace the `if (saved) { ... }` block with:

```typescript
    if (saved) {
      const parsed = JSON.parse(saved) as Partial<ThemePreferences>;
      const requested = parsed.themeId ?? '';
      const migrated = RENAMED_THEME_IDS[requested] ?? requested;
      return {
        themeId: themes[migrated] ? migrated : 'default-dark',
        terminalBlackBackground: parsed.terminalBlackBackground ?? false,
      };
    }
```

And in the legacy-key branch (lines 44-50), map the legacy id too. Replace:

```typescript
  // Migrate from old key
  const legacyTheme = localStorage.getItem('holomush-theme');
  if (legacyTheme) {
    const migrated = RENAMED_THEME_IDS[legacyTheme] ?? legacyTheme;
    if (themes[migrated]) {
      const prefs = { themeId: migrated, terminalBlackBackground: false };
      localStorage.setItem(PREFS_KEY, JSON.stringify(prefs));
      localStorage.removeItem('holomush-theme');
      return prefs;
    }
  }
```

- [ ] **Step 5: Update `CommandPalette.svelte` theme-switch commands**

Replace lines 46-47 of `web/src/lib/components/terminal/CommandPalette.svelte`:

```svelte
    { id: 'theme.warm-dark',      label: 'Switch theme: Warm Dark',      run: () => setTheme('warm-dark') },
    { id: 'theme.warm-light',     label: 'Switch theme: Warm Light',     run: () => setTheme('warm-light') },
```

- [ ] **Step 5b: Update `TopBar.svelte`'s duplicate theme registry** — it statically imports the JSON files deleted in Step 3, so this MUST change or `pnpm check`/`pnpm build` fails with module-not-found. Replace lines 24-35 of `web/src/lib/components/TopBar.svelte`:

```svelte
  import defaultDark from '$lib/theme/default-dark.json';
  import defaultLight from '$lib/theme/default-light.json';
  import warmDark from '$lib/theme/warm-dark.json';
  import warmLight from '$lib/theme/warm-light.json';
  import type { Theme } from '$lib/theme/types';

  const themeData: Record<string, Theme> = {
    'default-dark': defaultDark as Theme,
    'default-light': defaultLight as Theme,
    'warm-dark': warmDark as Theme,
    'warm-light': warmLight as Theme,
  };
```

- [ ] **Step 6: Update the test fixture imports**

Replace lines 37-45 of `web/src/lib/stores/themeStore.test.ts`:

```typescript
import defaultDark from '$lib/theme/default-dark.json';
import defaultLight from '$lib/theme/default-light.json';
import warmDark from '$lib/theme/warm-dark.json';
import warmLight from '$lib/theme/warm-light.json';

const builtInThemes = [
  ['default-dark', defaultDark],
  ['default-light', defaultLight],
  ['warm-dark', warmDark],
  ['warm-light', warmLight],
] as const;
```

- [ ] **Step 7: Add a migration test**

Append to `web/src/lib/stores/themeStore.test.ts` (before the final closing):

```typescript
describe('classic-* → warm-* id migration (holomush-9ektq D9)', () => {
  it('maps a saved classic-dark pref to warm-dark', async () => {
    localStorage.setItem('holomush-theme-prefs', JSON.stringify({ themeId: 'classic-dark', terminalBlackBackground: false }));
    vi.resetModules();
    const mod = await import('./themeStore');
    let id = '';
    mod.themePreferences.subscribe((p) => { id = p.themeId; })();
    expect(id).toBe('warm-dark');
  });
});
```

- [ ] **Step 8: Run the migration test**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts -t "migration"`
Expected: PASS (the new describe block). Other suites in the file may still fail on missing tokens until Tasks 4-6 — that's expected.

- [ ] **Step 9: Commit**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "feat(web): relocate classic→warm themes + id migration (holomush-9ektq)"`

---

### Task 3: Add the WCAG contrast + brand-guard test harness (INV-1, INV-2, INV-3, INV-6)

**Files:**

- Modify: `web/src/lib/stores/themeStore.test.ts` (append new describe blocks + helpers)

- [ ] **Step 1: Add the luminance + contrast helpers** (append near the `rgb()` helper, after line 53)

```typescript
function relLuminance(hex: string): number {
  const { r, g, b } = rgb(hex);
  const lin = (c: number) => {
    const s = c / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

function contrastRatio(fg: string, bg: string): number {
  const a = relLuminance(fg);
  const b = relLuminance(bg);
  const hi = Math.max(a, b);
  const lo = Math.min(a, b);
  return (hi + 0.05) / (lo + 0.05);
}

// Chrome text tokens that render type and must clear AA against a background.
const CHROME_TEXT_TOKENS = [
  'foreground', 'status.text', 'muted.foreground', 'scrollback.replayed',
  'card.foreground', 'popover.foreground', 'secondary.foreground',
] as const;

// All 11 message tokens render against the theme background.
const MESSAGE_TOKENS = [
  'say.speaker', 'say.speech', 'pose.actor', 'pose.action', 'system',
  'arrive', 'leave', 'command.output', 'command.error', 'ooc', 'pemit',
] as const;

const AA = 4.5;
```

- [ ] **Step 2: Add the contrast test (INV-2, INV-3)**

```typescript
describe('WCAG AA contrast (holomush-6siys, holomush-9ektq INV-2/INV-3)', () => {
  for (const [id, theme] of builtInThemes) {
    const c = theme.colors as Record<string, string>;
    for (const tok of CHROME_TEXT_TOKENS) {
      it(`${id}: chrome ${tok} ≥ ${AA}:1 on background`, () => {
        expect(contrastRatio(c[tok], c.background)).toBeGreaterThanOrEqual(AA);
      });
    }
    for (const tok of MESSAGE_TOKENS) {
      it(`${id}: message ${tok} ≥ ${AA}:1 on background`, () => {
        expect(contrastRatio(c[tok], c.background)).toBeGreaterThanOrEqual(AA);
      });
    }
  }
});
```

- [ ] **Step 3: Add the brand/amber guard (INV-1) and token-completeness (INV-6)**

```typescript
describe('brand alignment (holomush-9ektq INV-1)', () => {
  const brandDefaults = [['default-dark', defaultDark], ['default-light', defaultLight]] as const;
  for (const [id, theme] of brandDefaults) {
    const c = theme.colors as Record<string, string>;
    for (const tok of ['primary', 'accent', 'ring', 'scrollback.indicator'] as const) {
      it(`${id}: ${tok} is cyan-dominant (blue ≥ red), never amber`, () => {
        const { r, b } = rgb(c[tok]);
        expect(b, `${tok}=${c[tok]} must be cyan-family`).toBeGreaterThanOrEqual(r);
      });
    }
    it(`${id}: amber appears only on cursor`, () => {
      // amber = red-dominant warm hue; the cursor is allowed to be amber, nothing else.
      const amberish = (hex: string) => { const { r, g, b } = rgb(hex); return r > b && g > b && r > 150; };
      for (const [k, v] of Object.entries(c)) {
        if (k === 'cursor' || k === 'radius') continue;
        expect(amberish(v), `${k}=${v} must not be amber`).toBe(false);
      }
    });
  }
});

describe('new tokens exposed as --color-* (holomush-9ektq INV-6)', () => {
  for (const [id, theme] of builtInThemes) {
    const css = themeToCssVars(theme.colors as ThemeColors);
    for (const cssKey of ['--color-cursor', '--color-scrollback-replayed', '--color-foreground', '--color-input']) {
      it(`${id}: themeToCssVars emits ${cssKey}`, () => {
        expect(css).toContain(`${cssKey}: `);
      });
    }
  }
});
```

- [ ] **Step 4: Run the new suites to verify they FAIL on current themes**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts -t "contrast"`
Expected: FAIL — `default-dark`/`default-light` still hold brown values (e.g. `status.text` brown fails AA on the not-yet-rebranded background), and `default-dark.json` doesn't yet have the brand cyan accents. This is the TDD red state; Tasks 4-6 turn it green.

- [ ] **Step 5: Commit**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "test(web): WCAG AA contrast + brand-guard harness for themes (holomush-9ektq)"`

---

### Task 4: Rewrite `default-dark.json` to the brand palette

**Files:**

- Modify: `web/src/lib/theme/default-dark.json` (full rewrite)

- [ ] **Step 1: Replace the entire file**

```json
{
  "name": "default-dark",
  "colors": {
    "say.speaker": "#43ebff",
    "say.speech": "#f2f5f8",
    "pose.actor": "#69ddb9",
    "pose.action": "#99a7b4",
    "system": "#7acaeb",
    "arrive": "#758a96",
    "leave": "#758a96",
    "command.output": "#dbe3e9",
    "command.error": "#fc7f7f",
    "ooc": "#98b0f6",
    "pemit": "#65ddd3",
    "background": "#0b0c0e",
    "foreground": "#e8edf2",
    "input": "#1d2a33",
    "surface": "#101418",
    "border": "#1d2a33",
    "cursor": "#ffb300",
    "input.prompt": "#3dd6f7",
    "input.text": "#e8edf2",
    "input.background": "#07080a",
    "status.text": "#9aa7b2",
    "status.background": "#101418",
    "status.online": "#7fd98f",
    "status.offline": "#fc7f7f",
    "sidebar.background": "#101418",
    "scrollback.indicator": "#3dd6f7",
    "scrollback.replayed": "#8b98a4",
    "primary": "#3dd6f7",
    "primary.foreground": "#04222e",
    "secondary": "#18202a",
    "secondary.foreground": "#e8edf2",
    "muted": "#1a2128",
    "muted.foreground": "#9aa7b2",
    "accent": "#3dd6f7",
    "accent.foreground": "#04222e",
    "destructive": "#fc7f7f",
    "destructive.foreground": "#04222e",
    "card": "#101418",
    "card.foreground": "#e8edf2",
    "popover": "#101418",
    "popover.foreground": "#e8edf2",
    "ring": "#3dd6f7",
    "radius": "0.5rem"
  }
}
```

- [ ] **Step 2: Run the default-dark contrast + brand suites**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts -t "default-dark"`
Expected: PASS for all `default-dark:` contrast, brand-alignment, amber-only-cursor, and token-exposure cases.

- [ ] **Step 3: Commit**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "feat(web): rebrand default-dark to holographic cyan/ink (holomush-9ektq, holomush-6siys)"`

---

### Task 5: Rewrite `default-light.json` to the brand palette

**Files:**

- Modify: `web/src/lib/theme/default-light.json` (full rewrite)

- [ ] **Step 1: Replace the entire file**

```json
{
  "name": "default-light",
  "colors": {
    "say.speaker": "#0277bd",
    "say.speech": "#1a2026",
    "pose.actor": "#1b7a5e",
    "pose.action": "#5a6b76",
    "system": "#1565c0",
    "arrive": "#62717c",
    "leave": "#62717c",
    "command.output": "#1a2026",
    "command.error": "#c62828",
    "ooc": "#5e35b1",
    "pemit": "#00796b",
    "background": "#f6f8fa",
    "foreground": "#1a2026",
    "input": "#d3dde4",
    "surface": "#eef2f5",
    "border": "#d3dde4",
    "cursor": "#e09000",
    "input.prompt": "#1565c0",
    "input.text": "#1a2026",
    "input.background": "#ffffff",
    "status.text": "#5a6b76",
    "status.background": "#eef2f5",
    "status.online": "#2e7d32",
    "status.offline": "#c62828",
    "sidebar.background": "#eef2f5",
    "scrollback.indicator": "#1565c0",
    "scrollback.replayed": "#62717c",
    "primary": "#1565c0",
    "primary.foreground": "#ffffff",
    "secondary": "#d3dde4",
    "secondary.foreground": "#1a2026",
    "muted": "#d3dde4",
    "muted.foreground": "#5a6b76",
    "accent": "#1565c0",
    "accent.foreground": "#ffffff",
    "destructive": "#c62828",
    "destructive.foreground": "#ffffff",
    "card": "#eef2f5",
    "card.foreground": "#1a2026",
    "popover": "#eef2f5",
    "popover.foreground": "#1a2026",
    "ring": "#1565c0",
    "radius": "0.5rem"
  }
}
```

- [ ] **Step 2: Run the default-light suites**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts -t "default-light"`
Expected: PASS. If any `scrollback.replayed`/`muted.foreground` case fails (light-mode dim text reduces contrast), darken the offending token one step (e.g. `#62717c`→`#5a6770`) and re-run until ≥4.5:1.

- [ ] **Step 3: Commit**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "feat(web): rebrand default-light to cyan-on-light (holomush-9ektq)"`

---

### Task 6: Run the full theme test suite (all four green — INV-7)

**Files:** none (verification task; warm themes were authored AA-compliant in Task 2)

- [ ] **Step 1: Run the entire theme test file**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts`
Expected: PASS — all four themes clear contrast (INV-2/3/7), brand guards hold (INV-1), tokens exposed (INV-6), migration works (D9). If any contrast case fails, darken the offending token until ≥4.5:1: for chrome, `status.text`/`muted.foreground`/`scrollback.replayed` (warm-dark muted is `#9a8f7e`; bump toward `#a89c8a`); for messages, the dimmest hues — note `warm-light say.speaker` was pre-darkened `#0277bd`→`#0268a8` (4.43→5.46:1) in Task 2 for exactly this reason, so any *other* warm-light message token that fails gets the same one-step darkening treatment.

- [ ] **Step 2: Commit (only if a warm token was adjusted; otherwise skip)**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "fix(web): tune warm-theme muted tokens to AA (holomush-9ektq INV-7)"`

---

### Task 7: Update `app.css` `@theme` statics + wire the amber caret (D7, D8, INV-1)

**Files:**

- Modify: `web/src/app.css:9-48`
- Modify: `web/src/lib/components/terminal/CommandInput.svelte:214-222`

- [ ] **Step 1: Replace the `@theme` block** in `web/src/app.css` (lines 9-48) with brand-dark statics + the new tokens:

```css
@theme {
  --color-primary: #3dd6f7;
  --color-primary-foreground: #04222e;
  --color-secondary: #18202a;
  --color-secondary-foreground: #e8edf2;
  --color-muted: #1a2128;
  --color-muted-foreground: #9aa7b2;
  --color-accent: #3dd6f7;
  --color-accent-foreground: #04222e;
  --color-destructive: #fc7f7f;
  --color-destructive-foreground: #04222e;
  --color-card: #101418;
  --color-card-foreground: #e8edf2;
  --color-popover: #101418;
  --color-popover-foreground: #e8edf2;
  --color-border: #1d2a33;
  --color-input: #1d2a33;
  --color-ring: #3dd6f7;
  --color-background: #0b0c0e;
  --color-foreground: #e8edf2;
  --color-sidebar: #101418;
  --color-sidebar-foreground: #e8edf2;
  --color-sidebar-primary: #3dd6f7;
  --color-sidebar-primary-foreground: #04222e;
  --color-sidebar-accent: #18202a;
  --color-sidebar-accent-foreground: #e8edf2;
  --color-sidebar-border: #1d2a33;
  --color-sidebar-ring: #3dd6f7;
  --color-input-text: #e8edf2;
  --color-input-background: #07080a;
  --color-input-prompt: #3dd6f7;
  --color-cursor: #ffb300;
  --color-surface: #101418;
  --color-status-text: #9aa7b2;
  --color-status-background: #101418;
  --color-status-online: #7fd98f;
  --color-status-offline: #fc7f7f;
  --color-sidebar-background: #101418;
  --color-scrollback-indicator: #3dd6f7;
  --color-scrollback-replayed: #8b98a4;
  --radius: 0.5rem;
}
```

- [ ] **Step 2: Wire the caret to amber** — in `web/src/lib/components/terminal/CommandInput.svelte`, add `caret-color` to the `textarea` rule (lines 214-222):

```css
  textarea {
    flex: 1;
    background: transparent;
    border: none; outline: none;
    color: var(--color-input-text);
    caret-color: var(--color-cursor);
    font-family: inherit; font-size: inherit;
    resize: none; line-height: 20px;
    overflow-y: auto;
  }
```

- [ ] **Step 3: Verify build compiles**

Run: `cd web && pnpm check`
Expected: PASS (no type errors; all theme JSONs now carry the four new keys).

- [ ] **Step 4: Commit**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "feat(web): brand @theme statics + amber caret-color (holomush-9ektq)"`

---

### Task 8: holomush-1hgk — replace scrollback compounding opacity with a colour token (INV-4)

**Files:**

- Modify: `web/src/lib/components/terminal/EventRenderer.svelte:27,44`
- Modify: `web/src/lib/components/terminal/TerminalView.svelte:53-55,74,109`
- Modify: `web/src/lib/stores/themeStore.test.ts` (consuming-site guard)

- [ ] **Step 1: Write the consuming-site guard test first**

Append to `web/src/lib/stores/themeStore.test.ts` inside a new describe:

```typescript
describe('scrollback de-emphasis uses colour, not opacity (holomush-1hgk INV-4)', () => {
  const src = (rel: string) => readFileSync(`${process.cwd()}/src/lib/components/terminal/${rel}`, 'utf8');
  it('TerminalView replay lines use --color-scrollback-replayed', () => {
    expect(src('TerminalView.svelte')).toContain('var(--color-scrollback-replayed)');
  });
  it('TerminalView no longer dims replay lines with opacity', () => {
    expect(src('TerminalView.svelte')).not.toMatch(/\.line\.replay\s*\{[^}]*opacity/);
  });
  it('EventRenderer no longer has a .dimmed opacity rule', () => {
    expect(src('EventRenderer.svelte')).not.toMatch(/\.dimmed\s*\{[^}]*opacity/);
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts -t "scrollback de-emphasis"`
Expected: FAIL (current `.line.replay { opacity: 0.45 }` and `.dimmed { opacity: 0.5 }` still present).

- [ ] **Step 3: Remove the inner dim in `EventRenderer.svelte`** — delete the `dimmed` prop usage and the opacity rule. Replace line 27 (`<div class:dimmed>`) with:

```svelte
<div>
```

Delete line 44 entirely:

```css
  .dimmed { opacity: 0.5; }
```

Remove the now-unused `dimmed` prop from the `Props` type (line 21) and the `let { ... dimmed = false }` destructure (line 24) — change line 24 to:

```typescript
  let { event }: Props = $props();
```

and delete the `dimmed?: boolean;` line from `Props`.

- [ ] **Step 4: Update `TerminalView.svelte`** — stop passing the removed `dimmed` prop at BOTH call sites and switch the replay line to a colour. Replace line 55 (`<EventRenderer event={line.event} dimmed={true} />`) with:

```svelte
            <EventRenderer event={line.event} />
```

And replace line 74 (`<EventRenderer event={line.event} dimmed={false} />`, the live-line call) with:

```svelte
            <EventRenderer event={line.event} />
```

(Both are required — `dimmed` no longer exists on `EventRenderer` after Step 3, so a stale `dimmed={false}` would be an unknown-prop error under `pnpm check`.)

Replace line 109 (`.line.replay { opacity: 0.45; }`) with:

```css
  .line.replay { color: var(--color-scrollback-replayed); }
  .line.replay :global(*) { color: var(--color-scrollback-replayed); }
```

(The `:global(*)` override ensures replayed message text takes the dim colour instead of its per-message `--mush-*` hue — the intended monochrome-dim scrollback treatment per spec D6.)

- [ ] **Step 5: Run the guard test + visual smoke**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts -t "scrollback de-emphasis"`
Expected: PASS.

Then manual: `cd web && pnpm dev`, connect as guest, scroll back — replayed lines render in the dim slate colour, comfortably readable on both default themes (no near-invisible text).

- [ ] **Step 6: Commit**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "fix(web): scrollback de-emphasis via colour token, drop compounding opacity (holomush-1hgk)"`

---

### Task 9: holomush-vhsz — enlarge the shortcut hint bar (INV-5)

**Files:**

- Modify: `web/src/lib/components/terminal/CommandInput.svelte:233-247`
- Modify: `web/src/lib/stores/themeStore.test.ts` (consuming-site guard)

- [ ] **Step 1: Write the guard test first**

Append a describe to `web/src/lib/stores/themeStore.test.ts`:

```typescript
describe('shortcut hint bar legibility (holomush-vhsz INV-5)', () => {
  const src = readFileSync(`${process.cwd()}/src/lib/components/terminal/CommandInput.svelte`, 'utf8');
  it('.cmd-hints font-size ≥ 14px', () => {
    const m = /\.cmd-hints\s*\{[^}]*font-size:\s*(\d+)px/.exec(src);
    expect(m && Number(m[1])).toBeGreaterThanOrEqual(14);
  });
  it('.cmd-hints kbd font-size ≥ 13px', () => {
    const m = /\.cmd-hints kbd\s*\{[^}]*font-size:\s*(\d+)px/.exec(src);
    expect(m && Number(m[1])).toBeGreaterThanOrEqual(13);
  });
});
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts -t "hint bar legibility"`
Expected: FAIL (current 10px / 9px).

- [ ] **Step 3: Bump the sizes** — replace `.cmd-hints` and `.cmd-hints kbd` (lines 233-247) of `CommandInput.svelte`:

```css
  .cmd-hints {
    padding: 3px 12px;
    font-size: 14px;
    color: var(--color-status-text);
    background: var(--color-background);
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    align-items: center;
  }
  .cmd-hints kbd {
    font-family: inherit; padding: 1px 5px;
    border: 1px solid var(--color-border); border-radius: 3px;
    font-size: 13px;
  }
```

- [ ] **Step 4: Run the guard test**

Run: `cd web && pnpm vitest run src/lib/stores/themeStore.test.ts -t "hint bar legibility"`
Expected: PASS.

- [ ] **Step 5: Commit**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "fix(web): enlarge shortcut hint bar to 14/13px (holomush-vhsz)"`

---

### Task 10: Full verification

**Files:** none

- [ ] **Step 1: Type check**

Run: `cd web && pnpm check`
Expected: PASS, zero errors.

- [ ] **Step 2: Full unit suite**

Run: `cd web && pnpm vitest run`
Expected: PASS — all theme contrast/brand/migration/consuming-site tests green.

- [ ] **Step 3: Visual smoke across all four themes**

Run: `cd web && pnpm dev`. In the browser: open the command palette (`⌘K`), switch through Default Dark / Default Light / Warm Dark / Warm Light. Confirm per theme: footer hint row legible; amber caret in the input; cyan accents (default themes) / amber accents (warm themes); scrollback lines readable. Confirm a saved `classic-dark` pref (set `localStorage['holomush-theme-prefs'] = '{"themeId":"classic-dark"}'` then reload) lands on Warm Dark.

- [ ] **Step 4: Lint/format** (per repo task runner)

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && task fmt && task lint`
Expected: PASS (or fix surfaced issues).

- [ ] **Step 5: Final commit (if fmt/lint changed anything)**

Run: `cd /Volumes/Code/github.com/holomush/.worktrees/web-brand-rebrand && jj commit -m "chore(web): fmt/lint after theme rebrand (holomush-9ektq)"`

---

## Coverage check (plan ↔ spec)

| Spec item | Task |
|-----------|------|
| D1 default-dark in-place brand | Task 4 |
| D2 default-light brand | Task 5 |
| D3 classic→warm relocation/rename | Task 2 |
| D4 +10% cyan message palette | Tasks 4, 5 (values) |
| D5 AA 4.5:1 target | Task 3 (test) |
| D6 scrollback colour token (option C) | Task 8 |
| D7 dark/light cursor amber | Tasks 4, 5, 7 |
| D8 prompt cyan vs amber caret | Task 7 |
| D9 localStorage migration | Task 2 |
| INV-1 brand/amber guard | Task 3 |
| INV-2/3 AA contrast | Task 3 |
| INV-4 no-opacity scrollback | Task 8 |
| INV-5 hint legibility | Task 9 |
| INV-6 token completeness | Tasks 1, 3 |
| INV-7 warm aesthetic preserved | Tasks 2, 6 |

## Out of scope (per spec)

Game/sandbox warm-theme adoption; operator custom themes (holomush-nmr8); `terminalBlackBackground` semantics; non-theme component restructuring.

<!-- adr-capture: sha256=63b1ae52b335c618; session=brainstorm-9ektq; ts=2026-05-29T00:00:00Z; adrs=holomush-lcr84,holomush-poxub -->
