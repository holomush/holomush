// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { readFileSync } from 'node:fs';
import { describe, it, expect, vi } from 'vitest';

// themeStore.ts calls loadPreferences() at module load (touches localStorage),
// which runs before the beforeEach polyfill in test-setup.ts. Stub it before
// the import so the module evaluates cleanly.
vi.hoisted(() => {
  const store = new Map<string, string>();
  globalThis.localStorage = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => store.clear(),
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() { return store.size; },
  } as Storage;
  // jsdom does not implement matchMedia; loadPreferences() calls it at module load.
  globalThis.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as typeof globalThis.matchMedia;
});

import { themeToCssVars } from './themeStore';
import type { ThemeColors } from '$lib/theme/types';
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

// Parse #rrggbb into channels so "is it green?" is asserted on the value,
// not pinned to one exact hex.
function rgb(hex: string): { r: number; g: number; b: number } {
  const m = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);
  if (!m) throw new Error(`not a #rrggbb hex: ${hex}`);
  return { r: parseInt(m[1], 16), g: parseInt(m[2], 16), b: parseInt(m[3], 16) };
}

describe('status.online token (holomush-wnilg)', () => {
  for (const [id, theme] of builtInThemes) {
    const colors = theme.colors as Record<string, string>;

    it(`${id} defines a green-dominant status.online color`, () => {
      const online = colors['status.online'];
      expect(online, `${id} must define a status.online color`).toBeDefined();
      const { r, g, b } = rgb(online);
      // Green channel dominates → the dot reads as green, not grey.
      expect(g, `${id} status.online (${online}) green channel must exceed red`).toBeGreaterThan(r);
      expect(g, `${id} status.online (${online}) green channel must exceed blue`).toBeGreaterThan(b);
    });

    it(`${id} decouples status.online from the muted arrive message token`, () => {
      // The original bug coupled the connection dot to --mush-arrive, which
      // every theme defines grey. The status indicator must NOT reuse it.
      expect(colors['status.online']).not.toBe(colors['arrive']);
    });

    it(`${id} exposes status.online as the chrome --color-status-online custom property`, () => {
      // The CSS references var(--color-status-online); themeToCssVars must
      // actually define it (killing the dead var(--x, fallback) footgun).
      const css = themeToCssVars(theme.colors as ThemeColors);
      expect(css).toContain(`--color-status-online: ${colors['status.online']}`);
    });
  }
});

describe('status.offline token (holomush-qs31c)', () => {
  for (const [id, theme] of builtInThemes) {
    const colors = theme.colors as Record<string, string>;

    it(`${id} defines a red-dominant status.offline color`, () => {
      const offline = colors['status.offline'];
      expect(offline, `${id} must define a status.offline color`).toBeDefined();
      const { r, g, b } = rgb(offline);
      // Red channel dominates → the dot reads as red (dead connection), not orange.
      expect(r, `${id} status.offline (${offline}) red channel must exceed green`).toBeGreaterThan(g);
      expect(r, `${id} status.offline (${offline}) red channel must exceed blue`).toBeGreaterThan(b);
    });

    it(`${id} decouples status.offline from the orange system message token`, () => {
      // The original bug coupled the disconnected dot to --mush-system, which
      // every theme defines orange. The status indicator must NOT reuse it.
      expect(colors['status.offline']).not.toBe(colors['system']);
    });

    it(`${id} exposes status.offline as the chrome --color-status-offline custom property`, () => {
      // The CSS references var(--color-status-offline); themeToCssVars must
      // actually define it (killing the dead var(--x, fallback) footgun).
      const css = themeToCssVars(theme.colors as ThemeColors);
      expect(css).toContain(`--color-status-offline: ${colors['status.offline']}`);
    });
  }
});

describe('status dot CSS wiring (holomush-wnilg, holomush-qs31c)', () => {
  // Guard the consuming sites, not just the token table: the original bug
  // lived in the .svelte CSS (var(--mush-arrive, …)). Reverting these lines
  // while keeping the token would otherwise pass every token-table assertion.
  // cwd is the web/ package dir because vitest is launched from there.
  const componentSrc = (rel: string) =>
    readFileSync(`${process.cwd()}/src/lib/components/${rel}`, 'utf8');

  const sites = [
    ['TopBar connected dot', 'TopBar.svelte'],
    ['PresenceList online dot', 'sidebar/PresenceList.svelte'],
  ] as const;

  for (const [label, rel] of sites) {
    it(`${label} consumes --color-status-online`, () => {
      expect(componentSrc(rel)).toContain('var(--color-status-online)');
    });

    it(`${label} no longer reuses the muted --mush-arrive message token`, () => {
      // Match the var() usage, not the bare token name, so a future prose
      // mention of --mush-arrive in a comment can't false-fail the guard.
      expect(componentSrc(rel)).not.toContain('var(--mush-arrive');
    });
  }

  it('TopBar disconnected dot consumes --color-status-offline', () => {
    expect(componentSrc('TopBar.svelte')).toContain('var(--color-status-offline)');
  });

  it('TopBar disconnected dot no longer reuses the orange --mush-system message token', () => {
    expect(componentSrc('TopBar.svelte')).not.toContain('var(--mush-system');
  });
});

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
