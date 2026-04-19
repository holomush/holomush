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
