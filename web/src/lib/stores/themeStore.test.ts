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
import classicDark from '$lib/theme/classic-dark.json';
import classicLight from '$lib/theme/classic-light.json';

const builtInThemes = [
  ['default-dark', defaultDark],
  ['default-light', defaultLight],
  ['classic-dark', classicDark],
  ['classic-light', classicLight],
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

describe('status dot CSS wiring (holomush-wnilg)', () => {
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
});
