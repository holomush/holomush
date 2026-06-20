// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach } from 'vitest';
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
  openPalette,
  closePalette,
  openComposer,
  closeComposer,
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

  it('open/close convenience helpers set explicit state', () => {
    openComposer();
    expect(get(uiPrefs).composerOpen).toBe(true);
    closeComposer();
    expect(get(uiPrefs).composerOpen).toBe(false);
    openPalette();
    expect(get(uiPrefs).paletteOpen).toBe(true);
    closePalette();
    expect(get(uiPrefs).paletteOpen).toBe(false);
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

  it('does NOT write to localStorage before hydrateUiPrefs runs', () => {
    // Critical SSR-safety guarantee: if the server or pre-hydration client
    // mutates the store (e.g. applying a default), we must not persist —
    // otherwise we would overwrite the user's saved prefs with defaults.
    toggleRail();
    expect(localStorage.getItem('holomush-ui-prefs')).toBeNull();
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
