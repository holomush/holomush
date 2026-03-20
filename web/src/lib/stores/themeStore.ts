import { writable, derived } from 'svelte/store';
import type { Theme, ThemeColors } from '$lib/theme/types';
import darkTheme from '$lib/theme/default-dark.json';
import lightTheme from '$lib/theme/default-light.json';

const themes: Record<string, Theme> = {
  'default-dark': darkTheme as Theme,
  'default-light': lightTheme as Theme,
};

function getInitialTheme(): string {
  if (typeof window === 'undefined') return 'default-dark';
  const saved = localStorage.getItem('holomush-theme');
  if (saved && themes[saved]) return saved;
  return window.matchMedia('(prefers-color-scheme: light)').matches
    ? 'default-light'
    : 'default-dark';
}

export const activeThemeId = writable<string>(getInitialTheme());
export const activeTheme = derived(activeThemeId, ($id) => themes[$id] ?? themes['default-dark']);

export function setTheme(id: string) {
  if (themes[id]) {
    activeThemeId.set(id);
    localStorage.setItem('holomush-theme', id);
  }
}

export function themeToCssVars(colors: ThemeColors): string {
  return Object.entries(colors)
    .map(([key, value]) => `--color-${key.replace(/\./g, '-')}: ${value}`)
    .join('; ');
}
