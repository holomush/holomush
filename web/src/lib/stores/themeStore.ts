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
