// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, derived } from 'svelte/store';
import type { Theme, ThemeColors, ThemePreferences } from '$lib/theme/types';
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

const PREFS_KEY = 'holomush-theme-prefs';

// MUSH message tokens use --mush-* prefix.
// Everything else (chrome tokens, shadcn tokens) uses --color-* prefix.
const MUSH_TOKENS = new Set([
  'say.speaker', 'say.speech', 'pose.actor', 'pose.action',
  'system', 'arrive', 'leave', 'command.output', 'command.error',
  'ooc', 'pemit',
]);

function loadPreferences(): ThemePreferences {
  if (typeof window === 'undefined') {
    return { themeId: 'default-dark', terminalBlackBackground: false };
  }
  try {
    const saved = localStorage.getItem(PREFS_KEY);
    if (saved) {
      const parsed = JSON.parse(saved) as Partial<ThemePreferences>;
      const requested = parsed.themeId ?? '';
      const migrated = RENAMED_THEME_IDS[requested] ?? requested;
      return {
        themeId: themes[migrated] ? migrated : 'default-dark',
        terminalBlackBackground: parsed.terminalBlackBackground ?? false,
      };
    }
  } catch { /* corrupt data — fall through */ }

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
      if (MUSH_TOKENS.has(key)) {
        return `--mush-${cssKey}: ${value}`;
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
