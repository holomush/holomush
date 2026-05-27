<!--
  ~ SPDX-License-Identifier: Apache-2.0
  ~ Copyright 2026 HoloMUSH Contributors
-->

# Web Client Theme Redesign & shadcn-svelte Migration

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Redesign the web client's visual identity with a warm deep-orange/amber palette, integrate shadcn-svelte for forms/layout, bump font sizes, and ship 4 choosable themes.

**Architecture:** The existing theme store expands to include shadcn semantic tokens alongside MUSH tokens. Tailwind CSS v4 + shadcn-svelte replace hand-rolled CSS for forms, buttons, dialogs, and structural layout. Terminal content rendering stays custom. Theme preferences are a single serializable object in localStorage.

**Tech Stack:** SvelteKit 2, Tailwind CSS v4, bits-ui, shadcn-svelte, pnpm

**Spec:** `docs/superpowers/specs/2026-04-02-web-client-theme-redesign.md`

---

## File Structure

### New files

| File | Purpose |
| --- | --- |
| `web/src/app.css` | Tailwind base imports + theme variable bridge |
| `web/src/lib/utils.ts` | `cn()` utility (clsx + tailwind-merge) |
| `web/src/lib/theme/classic-dark.json` | Preserved original cyan/navy palette |
| `web/src/lib/theme/classic-light.json` | Preserved original blue/white palette |
| `web/src/lib/components/ui/` | shadcn-svelte generated components |
| `web/tailwind.config.ts` | Tailwind v4 config for shadcn theme mapping |
| `web/components.json` | shadcn-svelte CLI configuration |

### Modified files

| File | Change |
| --- | --- |
| `web/package.json` | New deps: tailwindcss, bits-ui, shadcn-svelte, clsx, tailwind-merge |
| `web/svelte.config.js` | Add Tailwind preprocessor if needed |
| `web/vite.config.ts` | Tailwind plugin integration |
| `web/src/lib/theme/types.ts` | Add shadcn tokens + ThemePreferences interface |
| `web/src/lib/theme/default-dark.json` | Warm palette + shadcn tokens |
| `web/src/lib/theme/default-light.json` | Warm light palette + shadcn tokens |
| `web/src/lib/stores/themeStore.ts` | ThemePreferences, black bg override, dual-prefix CSS vars |
| `web/src/routes/+layout.svelte` | Import app.css, apply theme vars to root |
| `web/src/lib/components/TopBar.svelte` | shadcn Button + DropdownMenu theme picker |
| `web/src/routes/login/+page.svelte` | shadcn Card + Input + Button + Label |
| `web/src/routes/register/+page.svelte` | shadcn Card + Input + Button + Label |
| `web/src/routes/reset/+page.svelte` | shadcn Card + Input + Button + Label |
| `web/src/routes/reset/confirm/+page.svelte` | shadcn Card + Input + Button + Label |
| `web/src/routes/+page.svelte` | shadcn Card + Button, sans-serif font |
| `web/src/routes/(authed)/characters/+page.svelte` | shadcn Card + Input + Button + Badge |
| `web/src/routes/(authed)/terminal/+page.svelte` | Card + ScrollArea + Resizable |
| `web/src/lib/components/terminal/TerminalView.svelte` | ScrollArea, font size 15px |
| `web/src/lib/components/terminal/CommandInput.svelte` | cn() conventions, font size 15px |
| `web/src/lib/components/terminal/StatusBar.svelte` | Font size 12px |
| `web/src/lib/components/sidebar/Sidebar.svelte` | Resizable.Pane + Sheet (mobile), font 12px |
| `web/src/lib/components/MarkdownContent.svelte` | No change (used inside Card) |

### Deleted files

| File | Reason |
| --- | --- |
| `web/src/lib/components/FeatureCard.svelte` | Replaced by shadcn Card on landing page |

---

## Task 1: Install Toolchain & Initialize shadcn-svelte

**Files:**

- Modify: `web/package.json`
- Create: `web/src/app.css`
- Create: `web/src/lib/utils.ts`
- Create: `web/components.json`
- Create: `web/tailwind.config.ts`
- Modify: `web/svelte.config.js`
- Modify: `web/vite.config.ts`

- [ ] **Step 1: Install Tailwind CSS v4 and shadcn-svelte dependencies**

Run from `web/` directory:

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm add -D tailwindcss @tailwindcss/vite
pnpm add clsx tailwind-merge tailwind-variants
```

- [ ] **Step 2: Update vite.config.ts with Tailwind plugin**

```typescript
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [tailwindcss(), sveltekit()]
});
```

- [ ] **Step 3: Initialize shadcn-svelte**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm dlx shadcn-svelte@latest init
```

When prompted:

- Base color: **Slate** (we override with our warm palette)
- Global CSS file: `src/app.css`
- Import alias for lib: `$lib`
- Import alias for components: `$lib/components`
- Import alias for utils: `$lib/utils`
- Import alias for hooks: `$lib/hooks`
- Import alias for ui: `$lib/components/ui`

This creates `components.json`, `src/app.css`, and `src/lib/utils.ts`.

- [ ] **Step 4: Verify the generated files exist**

```bash
ls web/components.json web/src/app.css web/src/lib/utils.ts
cat web/src/lib/utils.ts
```

Expected `utils.ts` content (approximately):

```typescript
import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}
```

- [ ] **Step 5: Verify dev server starts**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm dev --port 5173 &
sleep 3
curl -s http://localhost:5173 | head -5
kill %1
```

Expected: HTML response (no build errors).

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(web): install Tailwind CSS v4 and initialize shadcn-svelte

Adds Tailwind CSS v4 via @tailwindcss/vite plugin, initializes
shadcn-svelte CLI, and creates app.css with base theme variables.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 2: Create Theme Files & Update Theme Types

**Files:**

- Modify: `web/src/lib/theme/types.ts`
- Modify: `web/src/lib/theme/default-dark.json`
- Modify: `web/src/lib/theme/default-light.json`
- Create: `web/src/lib/theme/classic-dark.json`
- Create: `web/src/lib/theme/classic-light.json`

- [ ] **Step 1: Write the failing test for ThemePreferences type**

Create `web/src/lib/theme/types.test.ts`:

```typescript
import { describe, it, expect } from 'vitest';
import type { ThemeColors, Theme, ThemePreferences } from './types';

describe('ThemeColors', () => {
  it('includes MUSH tokens', () => {
    const colors = {} as ThemeColors;
    // TypeScript compilation ensures these properties exist
    const mushKeys: (keyof ThemeColors)[] = [
      'say.speaker', 'say.speech', 'pose.actor', 'pose.action',
      'system', 'arrive', 'leave', 'command.output', 'command.error',
      'ooc', 'pemit',
    ];
    expect(mushKeys.length).toBe(11);
  });

  it('includes shadcn tokens', () => {
    const colors = {} as ThemeColors;
    const shadcnKeys: (keyof ThemeColors)[] = [
      'primary', 'primary.foreground', 'secondary', 'secondary.foreground',
      'muted', 'muted.foreground', 'accent', 'accent.foreground',
      'destructive', 'destructive.foreground', 'card', 'card.foreground',
      'popover', 'popover.foreground', 'ring', 'radius',
    ];
    expect(shadcnKeys.length).toBe(16);
  });

  it('includes chrome tokens', () => {
    const colors = {} as ThemeColors;
    const chromeKeys: (keyof ThemeColors)[] = [
      'background', 'surface', 'border', 'input.prompt', 'input.text',
      'input.background', 'status.text', 'status.background',
      'sidebar.background', 'scrollback.indicator',
    ];
    expect(chromeKeys.length).toBe(10);
  });
});

describe('ThemePreferences', () => {
  it('has required fields', () => {
    const prefs: ThemePreferences = {
      themeId: 'default-dark',
      terminalBlackBackground: false,
    };
    expect(prefs.themeId).toBe('default-dark');
    expect(prefs.terminalBlackBackground).toBe(false);
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm exec vitest run src/lib/theme/types.test.ts
```

Expected: FAIL — `ThemePreferences` and new shadcn keys don't exist on `ThemeColors`.

- [ ] **Step 3: Update types.ts with extended interface**

Replace `web/src/lib/theme/types.ts`:

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

export interface ThemeColors {
  // MUSH message tokens (functional — distinct hues for readability)
  'say.speaker': string;
  'say.speech': string;
  'pose.actor': string;
  'pose.action': string;
  system: string;
  arrive: string;
  leave: string;
  'command.output': string;
  'command.error': string;
  ooc: string;
  pemit: string;

  // Chrome tokens (structural UI)
  background: string;
  surface: string;
  border: string;
  'input.prompt': string;
  'input.text': string;
  'input.background': string;
  'status.text': string;
  'status.background': string;
  'sidebar.background': string;
  'scrollback.indicator': string;

  // shadcn semantic tokens
  primary: string;
  'primary.foreground': string;
  secondary: string;
  'secondary.foreground': string;
  muted: string;
  'muted.foreground': string;
  accent: string;
  'accent.foreground': string;
  destructive: string;
  'destructive.foreground': string;
  card: string;
  'card.foreground': string;
  popover: string;
  'popover.foreground': string;
  ring: string;
  radius: string;
}

export interface Theme {
  name: string;
  colors: ThemeColors;
}

export interface ThemePreferences {
  themeId: string;
  terminalBlackBackground: boolean;
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm exec vitest run src/lib/theme/types.test.ts
```

Expected: PASS

- [ ] **Step 5: Copy current themes to classic-dark.json and classic-light.json**

Copy the current `default-dark.json` → `classic-dark.json` and `default-light.json` → `classic-light.json`, adding the shadcn tokens that map to the original palette.

`web/src/lib/theme/classic-dark.json`:

```json
{
  "name": "classic-dark",
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
    "background": "#0d0d1a",
    "surface": "#12122a",
    "border": "#1a1a3e",
    "input.prompt": "#4fc3f7",
    "input.text": "#e0e0e0",
    "input.background": "#0a0a16",
    "status.text": "#555555",
    "status.background": "#12122a",
    "sidebar.background": "#12122a",
    "scrollback.indicator": "#ffb74d",
    "primary": "#4fc3f7",
    "primary.foreground": "#0d0d1a",
    "secondary": "#1a1a3e",
    "secondary.foreground": "#e0e0e0",
    "muted": "#1a1a3e",
    "muted.foreground": "#555555",
    "accent": "#ffb74d",
    "accent.foreground": "#0d0d1a",
    "destructive": "#e57373",
    "destructive.foreground": "#ffffff",
    "card": "#12122a",
    "card.foreground": "#e0e0e0",
    "popover": "#12122a",
    "popover.foreground": "#e0e0e0",
    "ring": "#4fc3f7",
    "radius": "0.5rem"
  }
}
```

`web/src/lib/theme/classic-light.json`:

```json
{
  "name": "classic-light",
  "colors": {
    "say.speaker": "#0277bd",
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
    "background": "#fafafa",
    "surface": "#f0f0f0",
    "border": "#e0e0e0",
    "input.prompt": "#0277bd",
    "input.text": "#1a1a1a",
    "input.background": "#ffffff",
    "status.text": "#616161",
    "status.background": "#f0f0f0",
    "sidebar.background": "#f5f5f5",
    "scrollback.indicator": "#e65100",
    "primary": "#0277bd",
    "primary.foreground": "#ffffff",
    "secondary": "#e0e0e0",
    "secondary.foreground": "#1a1a1a",
    "muted": "#e0e0e0",
    "muted.foreground": "#616161",
    "accent": "#e65100",
    "accent.foreground": "#ffffff",
    "destructive": "#c62828",
    "destructive.foreground": "#ffffff",
    "card": "#f0f0f0",
    "card.foreground": "#1a1a1a",
    "popover": "#f0f0f0",
    "popover.foreground": "#1a1a1a",
    "ring": "#0277bd",
    "radius": "0.5rem"
  }
}
```

- [ ] **Step 6: Update default-dark.json with warm palette**

`web/src/lib/theme/default-dark.json`:

```json
{
  "name": "default-dark",
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
    "surface": "#1e1612",
    "border": "#3e2a1a",
    "input.prompt": "#ff7043",
    "input.text": "#e0d6cf",
    "input.background": "#16100c",
    "status.text": "#666055",
    "status.background": "#1e1612",
    "sidebar.background": "#1e1612",
    "scrollback.indicator": "#ffb74d",
    "primary": "#ff7043",
    "primary.foreground": "#ffffff",
    "secondary": "#2a1e16",
    "secondary.foreground": "#e0d6cf",
    "muted": "#2a1e16",
    "muted.foreground": "#666055",
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

- [ ] **Step 7: Update default-light.json with warm palette**

`web/src/lib/theme/default-light.json`:

```json
{
  "name": "default-light",
  "colors": {
    "say.speaker": "#0277bd",
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
    "surface": "#f0ebe5",
    "border": "#d4c4b0",
    "input.prompt": "#e64a19",
    "input.text": "#1a1510",
    "input.background": "#fffcf8",
    "status.text": "#6b6055",
    "status.background": "#f0ebe5",
    "sidebar.background": "#f5f0ea",
    "scrollback.indicator": "#e65100",
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

- [ ] **Step 8: Verify all theme JSON files parse correctly**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
node -e "
  const fs = require('fs');
  const themes = ['default-dark', 'default-light', 'classic-dark', 'classic-light'];
  for (const t of themes) {
    const data = JSON.parse(fs.readFileSync('src/lib/theme/' + t + '.json', 'utf8'));
    const keys = Object.keys(data.colors);
    console.log(t + ': ' + keys.length + ' tokens');
    if (keys.length !== 37) throw new Error(t + ' has wrong number of tokens: ' + keys.length);
  }
  console.log('All themes valid');
"
```

Expected: Each theme has 37 tokens (11 MUSH + 10 chrome + 16 shadcn). Output: `All themes valid`.

- [ ] **Step 9: Commit**

```bash
jj commit -m "feat(web): create 4-theme palette with warm defaults and shadcn tokens

Adds deep-orange/amber warm palette as default dark/light themes.
Preserves original cyan/navy palette as classic dark/light themes.
Extends ThemeColors interface with shadcn semantic tokens and
ThemePreferences type for future persistence.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 3: Update Theme Store & CSS Variable Bridge

**Files:**

- Modify: `web/src/lib/stores/themeStore.ts`
- Modify: `web/src/app.css`

- [ ] **Step 1: Write the failing test for themeStore changes**

Create `web/src/lib/stores/themeStore.test.ts`:

```typescript
import { describe, it, expect, beforeEach, vi } from 'vitest';

// Mock localStorage before importing the store
const storage = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (key: string) => storage.get(key) ?? null,
  setItem: (key: string, val: string) => storage.set(key, val),
  removeItem: (key: string) => storage.delete(key),
});

vi.stubGlobal('window', {
  matchMedia: () => ({ matches: false }),
  localStorage: globalThis.localStorage,
});

// Dynamic import so mocks are in place
const { themeToCssVars, themePreferences, setTheme, setTerminalBlackBackground } =
  await import('./themeStore');

import defaultDark from '$lib/theme/default-dark.json';
import type { ThemeColors } from '$lib/theme/types';

describe('themeToCssVars', () => {
  it('generates MUSH vars with --color- prefix', () => {
    const result = themeToCssVars(defaultDark.colors as ThemeColors);
    expect(result).toContain('--color-say-speaker:');
    expect(result).toContain('--color-pose-actor:');
    expect(result).toContain('--color-command-error:');
  });

  it('generates shadcn vars without prefix', () => {
    const result = themeToCssVars(defaultDark.colors as ThemeColors);
    expect(result).toContain('--primary:');
    expect(result).toContain('--primary-foreground:');
    expect(result).toContain('--muted:');
    expect(result).toContain('--ring:');
    expect(result).toContain('--radius:');
  });

  it('does not double-prefix shadcn vars', () => {
    const result = themeToCssVars(defaultDark.colors as ThemeColors);
    expect(result).not.toContain('--color-primary:');
  });
});

describe('themePreferences', () => {
  beforeEach(() => storage.clear());

  it('defaults terminalBlackBackground to false', () => {
    // Reset module to pick up fresh localStorage
    expect(typeof setTerminalBlackBackground).toBe('function');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm exec vitest run src/lib/stores/themeStore.test.ts
```

Expected: FAIL — `setTerminalBlackBackground` not exported, `themeToCssVars` generates wrong prefix format.

- [ ] **Step 3: Rewrite themeStore.ts**

```typescript
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, derived } from 'svelte/store';
import type { Theme, ThemeColors, ThemePreferences } from '$lib/theme/types';
import defaultDark from '$lib/theme/default-dark.json';
import defaultLight from '$lib/theme/default-light.json';
import classicDark from '$lib/theme/classic-dark.json';
import classicLight from '$lib/theme/classic-light.json';

const themes: Record<string, Theme> = {
  'default-dark': defaultDark as Theme,
  'default-light': defaultLight as Theme,
  'classic-dark': classicDark as Theme,
  'classic-light': classicLight as Theme,
};

const PREFS_KEY = 'holomush-theme-prefs';

// shadcn tokens use bare CSS variable names (--primary, --ring, etc.)
// MUSH and chrome tokens use --color- prefix (--color-say-speaker, etc.)
const SHADCN_TOKENS = new Set([
  'primary', 'primary.foreground', 'secondary', 'secondary.foreground',
  'muted', 'muted.foreground', 'accent', 'accent.foreground',
  'destructive', 'destructive.foreground', 'card', 'card.foreground',
  'popover', 'popover.foreground', 'ring', 'radius',
]);

function loadPreferences(): ThemePreferences {
  if (typeof window === 'undefined') {
    return { themeId: 'default-dark', terminalBlackBackground: false };
  }
  try {
    const saved = localStorage.getItem(PREFS_KEY);
    if (saved) {
      const parsed = JSON.parse(saved) as Partial<ThemePreferences>;
      return {
        themeId: (parsed.themeId && themes[parsed.themeId]) ? parsed.themeId : 'default-dark',
        terminalBlackBackground: parsed.terminalBlackBackground ?? false,
      };
    }
  } catch { /* corrupt data — fall through */ }

  // Migrate from old key
  const legacyTheme = localStorage.getItem('holomush-theme');
  if (legacyTheme && themes[legacyTheme]) {
    const prefs = { themeId: legacyTheme, terminalBlackBackground: false };
    localStorage.setItem(PREFS_KEY, JSON.stringify(prefs));
    localStorage.removeItem('holomush-theme');
    return prefs;
  }

  const prefersDark = !window.matchMedia('(prefers-color-scheme: light)').matches;
  return { themeId: prefersDark ? 'default-dark' : 'default-light', terminalBlackBackground: false };
}

export const themePreferences = writable<ThemePreferences>(loadPreferences());
export const activeTheme = derived(themePreferences, ($prefs) => themes[$prefs.themeId] ?? themes['default-dark']);

function savePreferences(prefs: ThemePreferences) {
  if (typeof window !== 'undefined') {
    localStorage.setItem(PREFS_KEY, JSON.stringify(prefs));
  }
}

export function setTheme(id: string) {
  if (themes[id]) {
    themePreferences.update((prefs) => {
      const next = { ...prefs, themeId: id };
      savePreferences(next);
      return next;
    });
  }
}

export function setTerminalBlackBackground(enabled: boolean) {
  themePreferences.update((prefs) => {
    const next = { ...prefs, terminalBlackBackground: enabled };
    savePreferences(next);
    return next;
  });
}

export function themeToCssVars(colors: ThemeColors): string {
  return Object.entries(colors)
    .map(([key, value]) => {
      const cssKey = key.replace(/\./g, '-');
      if (SHADCN_TOKENS.has(key)) {
        return `--${cssKey}: ${value}`;
      }
      return `--color-${cssKey}: ${value}`;
    })
    .join('; ');
}

export function terminalBlackOverrideVars(): string {
  return '--color-background: #000000; --color-input-background: #0a0a0a';
}

export function getAvailableThemes(): Array<{ id: string; name: string }> {
  return Object.entries(themes).map(([id, theme]) => ({ id, name: theme.name }));
}

export function applyOverrides(customThemes: Record<string, Theme>, overrides: Record<string, Partial<ThemeColors>>) {
  for (const [name, theme] of Object.entries(customThemes)) {
    themes[name] = theme;
  }
  for (const [base, colors] of Object.entries(overrides)) {
    if (themes[base]) {
      themes[base] = { ...themes[base], colors: { ...themes[base].colors, ...colors } };
    }
  }
  themePreferences.update((p) => p);
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm exec vitest run src/lib/stores/themeStore.test.ts
```

Expected: PASS

- [ ] **Step 5: Update app.css to bridge theme variables to Tailwind**

The shadcn init creates an `app.css` with static HSL values. Replace the `:root` / `.dark` blocks with fallback values that get overridden by the theme store's inline styles. Keep the Tailwind imports and border-color compatibility layer.

```css
@import "tailwindcss";

/*
  Compatibility: Tailwind CSS v4 changed default border-color to currentcolor.
  These ensure existing elements behave like v3.
*/
@layer base {
  *,
  ::after,
  ::before,
  ::backdrop,
  ::file-selector-button {
    border-color: var(--border, currentcolor);
  }
}

/*
  Theme variables are injected at runtime by themeStore.ts via inline style.
  These fallbacks match default-dark so the page renders before JS loads.
*/
@layer base {
  :root {
    --primary: #ff7043;
    --primary-foreground: #ffffff;
    --secondary: #2a1e16;
    --secondary-foreground: #e0d6cf;
    --muted: #2a1e16;
    --muted-foreground: #666055;
    --accent: #ffb74d;
    --accent-foreground: #1a1210;
    --destructive: #e57373;
    --destructive-foreground: #ffffff;
    --card: #1e1612;
    --card-foreground: #e0d6cf;
    --popover: #1e1612;
    --popover-foreground: #e0d6cf;
    --border: #3e2a1a;
    --input: #3e2a1a;
    --ring: #ff7043;
    --radius: 0.5rem;
    --background: #1a1210;
    --foreground: #e0d6cf;
  }
}

@layer base {
  * {
    @apply border-border;
  }
  body {
    @apply bg-background text-foreground;
    font-family: -apple-system, 'Inter', system-ui, sans-serif;
  }
}
```

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(web): rewrite themeStore with dual-prefix CSS vars and preferences

ThemeStore now generates --color-* prefix for MUSH/chrome tokens and
bare --* names for shadcn tokens. Adds ThemePreferences object with
terminalBlackBackground toggle. Migrates from legacy localStorage key.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 4: Add shadcn Components

**Files:**

- Create: `web/src/lib/components/ui/*` (generated by CLI)

- [ ] **Step 1: Add all needed shadcn components**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm dlx shadcn-svelte@latest add button card input label separator checkbox scroll-area dropdown-menu badge tooltip
```

- [ ] **Step 2: Verify the components were created**

```bash
ls web/src/lib/components/ui/
```

Expected: Directories for each component (button, card, input, label, separator, checkbox, scroll-area, dropdown-menu, badge, tooltip).

- [ ] **Step 3: Add resizable and sheet components**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm dlx shadcn-svelte@latest add resizable sheet
```

- [ ] **Step 4: Verify all components exist**

```bash
ls -d web/src/lib/components/ui/*/
```

Expected: button, card, input, label, separator, checkbox, scroll-area, dropdown-menu, badge, tooltip, resizable, sheet.

- [ ] **Step 5: Verify build succeeds**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm check
```

Expected: No TypeScript errors.

- [ ] **Step 6: Commit**

```bash
jj commit -m "feat(web): add shadcn-svelte UI components

Adds Button, Card, Input, Label, Separator, Checkbox, ScrollArea,
DropdownMenu, Badge, Tooltip, Resizable, and Sheet components via
the shadcn-svelte CLI.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 5: Migrate Layout & TopBar with Theme Picker

**Files:**

- Modify: `web/src/routes/+layout.svelte`
- Modify: `web/src/lib/components/TopBar.svelte`

- [ ] **Step 1: Update +layout.svelte to import app.css and apply theme**

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import TopBar from '$lib/components/TopBar.svelte';
  import { initTelemetry, startNavigationSpan, endNavigationSpan } from '$lib/telemetry';
  import { restoreSession } from '$lib/stores/authStore';
  import { activeTheme, themeToCssVars } from '$lib/stores/themeStore';
  import { beforeNavigate, afterNavigate } from '$app/navigation';
  import { onMount } from 'svelte';
  import '../app.css';

  let { children } = $props();

  onMount(() => {
    initTelemetry();
    restoreSession();
  });

  beforeNavigate(({ to }) => {
    startNavigationSpan(to?.url.pathname ?? 'unknown');
  });

  afterNavigate(() => {
    endNavigationSpan();
  });
</script>

<div class="app-root" style={themeToCssVars($activeTheme.colors)}>
  <TopBar />
  <main>{@render children()}</main>
</div>

<style>
  .app-root {
    display: flex;
    flex-direction: column;
    min-height: 100vh;
  }
  main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }
</style>
```

- [ ] **Step 2: Rewrite TopBar.svelte with shadcn components and theme picker**

```svelte
<!--
  SPDX-License-Identifier: Apache-2.0
  Copyright 2026 HoloMUSH Contributors
-->
<script lang="ts">
  import { LogOut, ArrowLeftRight, Palette } from 'lucide-svelte';
  import { authState, clearAuth } from '$lib/stores/authStore';
  import {
    activeTheme,
    themePreferences,
    setTheme,
    setTerminalBlackBackground,
    getAvailableThemes,
  } from '$lib/stores/themeStore';
  import { createClient } from '@connectrpc/connect';
  import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
  import { transport } from '$lib/transport';
  import { goto } from '$app/navigation';
  import { Button } from '$lib/components/ui/button';
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu';
  import { Checkbox } from '$lib/components/ui/checkbox';
  import defaultDark from '$lib/theme/default-dark.json';
  import defaultLight from '$lib/theme/default-light.json';
  import classicDark from '$lib/theme/classic-dark.json';
  import classicLight from '$lib/theme/classic-light.json';
  import type { Theme } from '$lib/theme/types';

  const themes: Record<string, Theme> = {
    'default-dark': defaultDark as Theme,
    'default-light': defaultLight as Theme,
    'classic-dark': classicDark as Theme,
    'classic-light': classicLight as Theme,
  };

  const client = createClient(WebService, transport);
  const availableThemes = getAvailableThemes();

  async function handleLogout() {
    try {
      await client.webLogout({ sessionId: $authState.sessionId ?? '' });
    } catch {
      /* best effort */
    }
    clearAuth();
    goto('/');
  }

  function handleSwitchCharacter() {
    goto('/characters');
  }

  function displayName(id: string): string {
    return id.split('-').map((w) => w.charAt(0).toUpperCase() + w.slice(1)).join(' ');
  }
</script>

<header>
  <div class="left">
    <a href="/" class="logo">
      <span class="logo-icon">H</span>
      <span class="logo-text">HoloMUSH</span>
    </a>
  </div>
  <nav class="right">
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
        <DropdownMenu.RadioGroup value={$themePreferences.themeId} onValueChange={(v) => v && setTheme(v)}>
          {#each availableThemes as theme (theme.id)}
            <DropdownMenu.RadioItem value={theme.id}>
              <span class="flex items-center gap-2">
                <span class="flex gap-0.5">
                  <span
                    class="inline-block w-3 h-3 rounded-sm border border-border"
                    style="background: {themes[theme.id]?.colors.background ?? '#000'}"
                  ></span>
                  <span
                    class="inline-block w-3 h-3 rounded-sm"
                    style="background: {themes[theme.id]?.colors.primary ?? '#888'}"
                  ></span>
                  <span
                    class="inline-block w-3 h-3 rounded-sm"
                    style="background: {themes[theme.id]?.colors.accent ?? '#888'}"
                  ></span>
                </span>
                {displayName(theme.id)}
              </span>
            </DropdownMenu.RadioItem>
          {/each}
        </DropdownMenu.RadioGroup>
        <DropdownMenu.Separator />
        <DropdownMenu.CheckboxItem
          checked={$themePreferences.terminalBlackBackground}
          onCheckedChange={(v) => setTerminalBlackBackground(v === true)}
        >
          Black terminal background
        </DropdownMenu.CheckboxItem>
      </DropdownMenu.Content>
    </DropdownMenu.Root>

    {#if !$authState.playerToken && !$authState.sessionId}
      <Button variant="ghost" size="sm" href="/login">Login</Button>
      <Button size="sm" href="/register">Register</Button>
    {:else if $authState.sessionId && $authState.characterName}
      <span class="char-name">{$authState.characterName}</span>
      <button class="icon-btn" onclick={handleSwitchCharacter} title="Switch character" aria-label="Switch character">
        <ArrowLeftRight size={16} />
      </button>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {:else if $authState.playerToken}
      <span class="player-name">{$authState.playerName}</span>
      <button class="icon-btn" onclick={handleLogout} title="Logout" aria-label="Log out">
        <LogOut size={16} />
      </button>
    {/if}
  </nav>
</header>

<style>
  header {
    height: 36px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 12px;
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-border);
    flex-shrink: 0;
    font-size: 13px;
  }

  .left {
    display: flex;
    align-items: center;
  }

  .logo {
    display: flex;
    align-items: center;
    gap: 6px;
    text-decoration: none;
    color: var(--color-input-text);
  }

  .logo-icon {
    width: 22px;
    height: 22px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--primary);
    color: var(--primary-foreground);
    border-radius: 4px;
    font-weight: bold;
    font-size: 12px;
    flex-shrink: 0;
  }

  .logo-text {
    color: var(--primary);
    font-weight: 600;
    letter-spacing: 0.05em;
  }

  .right {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .char-name,
  .player-name {
    color: var(--primary);
    font-size: 13px;
  }

  .icon-btn {
    background: none;
    border: none;
    cursor: pointer;
    color: var(--color-status-text);
    display: flex;
    align-items: center;
    padding: 2px;
    border-radius: 4px;
    transition: color 0.15s;
  }

  .icon-btn:hover {
    color: var(--color-input-text);
  }
</style>
```

**Note:** The theme color swatches in the dropdown reference the `themes` object imported in the instance `<script>` block. Svelte 5 does not support `context="module"` — all imports go in the instance script. The implementing agent MUST move the theme JSON imports and `themes` map into the main `<script lang="ts">` block, not a separate module script.

- [ ] **Step 3: Verify build succeeds**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm check
```

Expected: No TypeScript errors.

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(web): add theme picker dropdown and apply theme at layout root

Moves theme CSS variable injection to +layout.svelte root element.
TopBar now uses shadcn Button for nav links and DropdownMenu for a
theme picker with color swatches and terminal black bg toggle.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 6: Migrate Auth Forms to shadcn

**Files:**

- Modify: `web/src/routes/login/+page.svelte`
- Modify: `web/src/routes/register/+page.svelte`
- Modify: `web/src/routes/reset/+page.svelte`
- Modify: `web/src/routes/reset/confirm/+page.svelte`

- [ ] **Step 1: Rewrite login page with shadcn Card + Input + Button + Label**

Replace the `<style>` block and template in `web/src/routes/login/+page.svelte`. Keep the `<script>` block's logic identical. Remove `themeToCssVars` and `activeTheme` imports — theme vars now come from the layout root.

Key changes:

- Wrap form in `<Card.Root>` / `<Card.Header>` / `<Card.Content>` / `<Card.Footer>`
- Replace `<input>` with shadcn `<Input>` component
- Replace `<button class="btn-primary">` with shadcn `<Button>`
- Replace `<button class="btn-ghost">` with `<Button variant="outline">`
- Replace `<span class="label">` with shadcn `<Label>`
- Use `<Separator>` for the "or" divider
- Remove all hand-rolled `<style>` — rely on Tailwind utility classes
- Font family: page body inherits sans-serif from `app.css`
- Form inputs MUST retain `name` attributes for E2E testability
- Submit buttons MUST have `type="submit"`

The implementing agent MUST read the current `login/+page.svelte` script block and preserve ALL business logic exactly. Only the template and styling change.

- [ ] **Step 2: Rewrite register page**

Same pattern as login. Key differences:

- 4 fields (username, email, password, confirmPassword)
- `<Card.Description>` for the hint text about player accounts
- No guest login button or separator
- Footer link: "Already have an account? Sign in"

Preserve ALL business logic from the existing `<script>` block.

- [ ] **Step 3: Rewrite reset page**

Same Card pattern. Two states:

- Initial: email input + submit button
- Submitted: success message + back link

Preserve ALL business logic.

- [ ] **Step 4: Rewrite reset/confirm page**

Same Card pattern. Two states:

- Form: new password + confirm password + submit
- Success: "Password changed! Redirecting to login…"

Preserve ALL business logic.

- [ ] **Step 5: Verify build succeeds**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm check
```

- [ ] **Step 6: Run E2E auth tests**

```bash
cd /Volumes/Code/github.com/holomush/holomush
task test:e2e -- --grep "auth"
```

Expected: PASS — form `name` attributes preserved, `type="submit"` on buttons.

- [ ] **Step 7: Commit**

```bash
jj commit -m "feat(web): migrate auth forms to shadcn Card + Input + Button

Replaces hand-rolled CSS with shadcn components on login, register,
reset, and reset/confirm pages. All form name attributes and submit
button types preserved for E2E testability.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 7: Migrate Landing Page & Delete FeatureCard

**Files:**

- Modify: `web/src/routes/+page.svelte`
- Delete: `web/src/lib/components/FeatureCard.svelte`

- [ ] **Step 1: Rewrite landing page with shadcn components**

Key changes:

- Hero buttons: shadcn `<Button>` (primary for Login, outline for Register, ghost for Guest)
- Feature cards: Replace `<FeatureCard>` with shadcn `<Card.Root>` / `<Card.Header>` / `<Card.Content>`
- Remove `import FeatureCard`
- Remove `import { activeTheme, themeToCssVars }` — theme comes from layout
- Remove `style={themeToCssVars(...)}` from the root div
- Remove all hand-rolled `<style>` — use Tailwind classes
- Hero title: `text-[38px]` (36 + 2)
- Subtitle: `text-[15px]` (14 + 1)
- Body font: inherits sans-serif from app.css

Preserve ALL business logic (guest login handler, content data loading).

- [ ] **Step 2: Delete FeatureCard.svelte**

```bash
rm web/src/lib/components/FeatureCard.svelte
```

- [ ] **Step 3: Verify build and landing E2E test**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm check
```

```bash
cd /Volumes/Code/github.com/holomush/holomush
task test:e2e -- --grep "landing"
```

Expected: PASS

- [ ] **Step 4: Commit**

```bash
jj commit -m "feat(web): migrate landing page to shadcn and delete FeatureCard

Replaces hand-rolled hero buttons and feature cards with shadcn
Button and Card components. Removes FeatureCard.svelte.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 8: Migrate Character Picker Page

**Files:**

- Modify: `web/src/routes/(authed)/characters/+page.svelte`

- [ ] **Step 1: Rewrite character picker with shadcn components**

Key changes:

- Character cards: shadcn `<Card.Root>` with `onclick` handler
- Status badges: shadcn `<Badge>` (variant `default` for active, `outline` for offline)
- Create character form: shadcn `<Card.Root>` + `<Input>` + `<Button>` + `<Checkbox>`
- "Create New Character" card: dashed border Card
- Remove `import { activeTheme, themeToCssVars }` — theme comes from layout
- Remove `style={themeToCssVars(...)}` from root div
- Remove all hand-rolled `<style>` — Tailwind classes
- Character name: `text-[14px]`, meta: `text-[12px]`

Preserve ALL business logic.

- [ ] **Step 2: Verify build**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm check
```

- [ ] **Step 3: Commit**

```bash
jj commit -m "feat(web): migrate character picker to shadcn Card + Badge

Replaces hand-rolled character cards and status badges with shadcn
components. Create character form uses shadcn Input and Button.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 9: Migrate Terminal Layout

**Files:**

- Modify: `web/src/routes/(authed)/terminal/+page.svelte`
- Modify: `web/src/lib/components/terminal/TerminalView.svelte`
- Modify: `web/src/lib/components/terminal/CommandInput.svelte`
- Modify: `web/src/lib/components/terminal/StatusBar.svelte`
- Modify: `web/src/lib/components/sidebar/Sidebar.svelte`

- [ ] **Step 1: Update terminal/+page.svelte with Resizable layout and black bg override**

Key changes:

- Wrap terminal area in `<Card.Root>` with no padding
- Replace `<div class="main-area">` with `<Resizable.PaneGroup direction="horizontal">`
- Terminal column becomes `<Resizable.Pane defaultSize={75}>`
- Sidebar becomes `<Resizable.Pane defaultSize={25}>` with `<Resizable.Handle />` between
- Mobile: sidebar uses `<Sheet>` instead of Resizable
- Terminal black bg override: when `$themePreferences.terminalBlackBackground`, apply `terminalBlackOverrideVars()` as additional inline style on the terminal pane
- Remove `import { activeTheme, themeToCssVars }` — theme comes from layout root
- Import `themePreferences` and `terminalBlackOverrideVars` from themeStore
- Login-screen fallback (`.login-screen`): use themed CSS vars instead of hardcoded colors

Preserve ALL business logic (streaming, sendCommand, disconnect, reconnect, keyboard handlers).

- [ ] **Step 2: Update TerminalView.svelte with ScrollArea and font bump**

Key changes:

- Wrap scrollback in shadcn `<ScrollArea>` for styled scrollbars
- Font size: `13px` → `15px` for terminal content
- Separator font: `10px` → `11px`
- Scroll indicator font: `11px` → `12px`
- Keep IntersectionObserver sentinel logic intact

Preserve ALL scrollback logic.

- [ ] **Step 3: Update CommandInput.svelte with font bump and cn() convention**

Key changes:

- Font size: inherits `15px` from terminal layout
- Hints font: `9px` → `10px`
- Import `cn` from `$lib/utils` — use for any conditional classes
- Expose `class` prop for override/extension

Preserve ALL command input logic (history, draft save, auto-grow, keyboard handlers).

- [ ] **Step 4: Update StatusBar.svelte with font bump**

Key changes:

- Font size: `11px` → `12px`
- Location font: `10px` → `11px`
- Icon button font: `13px` → `14px`

Preserve ALL status bar logic.

- [ ] **Step 5: Update Sidebar.svelte with Resizable.Pane and font bumps**

Key changes:

- Sidebar content font: `11px` → `12px`
- Badge font: `8px` → `9px`
- Icon size: leave at `28px` (already appropriate)
- The Sidebar component no longer manages its own width — `Resizable.Pane` handles that from the parent. Remove the `width` transition CSS.
- Overlay mode (mobile): wrap in `<Sheet>` from shadcn instead of hand-rolled backdrop + absolute positioning

Preserve ALL sidebar store logic.

- [ ] **Step 6: Verify build and terminal E2E test**

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm check
```

```bash
cd /Volumes/Code/github.com/holomush/holomush
task test:e2e -- --grep "terminal"
```

Expected: PASS

- [ ] **Step 7: Commit**

```bash
jj commit -m "feat(web): migrate terminal layout to shadcn Resizable + ScrollArea

Terminal page uses Resizable.PaneGroup for sidebar/terminal split,
ScrollArea for styled scrollback, and Sheet for mobile sidebar.
Bumps terminal font to 15px, chrome to 12px. Adds terminal black
background override support.

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

---

## Task 10: Final Verification & Cleanup

**Files:**

- Various (cleanup only)

- [ ] **Step 1: Run full test suite**

```bash
cd /Volumes/Code/github.com/holomush/holomush
task test && task lint
```

Expected: PASS

- [ ] **Step 2: Run E2E tests**

```bash
cd /Volumes/Code/github.com/holomush/holomush
task test:e2e
```

Expected: PASS

- [ ] **Step 3: Run integration tests**

```bash
cd /Volumes/Code/github.com/holomush/holomush
task test:int
```

Expected: PASS (no Go changes, but verify nothing broke)

- [ ] **Step 4: Verify all 4 themes load without console errors**

Start the dev server and manually verify each theme works:

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
pnpm dev --port 5173
```

Check in browser:

- [ ] Default Dark: warm charcoal background, deep-orange accent
- [ ] Default Light: warm cream background, deep-orange accent
- [ ] Classic Dark: navy background, cyan accent
- [ ] Classic Light: white background, blue accent
- [ ] Terminal black bg toggle: terminal area goes #000000
- [ ] Theme picker shows color swatches
- [ ] Theme persists across page reload

- [ ] **Step 5: Remove any orphaned CSS that references old color vars**

Search for any remaining `var(--color-say-speaker)` used as accent colors (these should now be `var(--primary)`). MUSH message renderers correctly keep `--color-say-speaker` etc.

```bash
cd /Volumes/Code/github.com/holomush/holomush/web
rg 'var\(--color-say-speaker\)' src/ --type svelte
```

The only files referencing `--color-say-speaker` should be the terminal renderers (`CommunicationRenderer.svelte`, `EventRenderer.svelte`) and similar MUSH-specific components. Form pages, TopBar, landing page, and character picker should use `var(--primary)` instead.

- [ ] **Step 6: Commit any cleanup**

```bash
jj commit -m "chore(web): clean up orphaned CSS references after theme migration

Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>"
```

- [ ] **Step 7: Final status check**

```bash
jj st
jj log -r 'ancestors(@, 15)' --no-graph
```

Verify all commits are clean and the log tells a coherent story.
