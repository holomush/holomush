// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable } from 'svelte/store';

export type Density = 'cozy' | 'compact';
export interface UiPrefs {
  railHidden: boolean;
  sidebarHidden: boolean;
  sidebarWidthPx: number;
  density: Density;
  composerOpen: boolean;
  composerPos: { x: number; y: number };
  composerSize: { w: number; h: number };
  paletteOpen: boolean;
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
    // Narrow validation: density is an enum, reject anything else.
    const density: Density = parsed.density === 'compact' ? 'compact' : 'cozy';
    uiPrefs.update((current) => ({
      ...current,
      ...parsed,
      density,
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
