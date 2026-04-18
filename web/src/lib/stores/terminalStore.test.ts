// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { appendLine, clearLines, lines } from './terminalStore';

describe('terminalStore.appendLine', () => {
  beforeEach(() => {
    // jsdom in this environment exposes `localStorage` as an object without
    // methods; stub the API the store touches so tests focus on behavior.
    vi.stubGlobal('localStorage', {
      getItem: () => null,
      setItem: () => undefined,
      removeItem: () => undefined,
    });
    clearLines();
  });

  it('sets timestamp from numeric millis when provided', () => {
    const ms = 1713456789000;
    appendLine({ type: 'say', characterName: 'A', text: 'hi' }, false, ms);
    const [line] = get(lines);
    expect(line.timestamp).toBeInstanceOf(Date);
    expect(line.timestamp.getTime()).toBe(ms);
  });

  it('falls back to Date.now() when timestamp is 0', () => {
    const before = Date.now();
    appendLine({ type: 'say', characterName: 'A', text: 'hi' }, false, 0);
    const after = Date.now();
    const [line] = get(lines);
    expect(line.timestamp.getTime()).toBeGreaterThanOrEqual(before);
    expect(line.timestamp.getTime()).toBeLessThanOrEqual(after);
  });

  it('falls back to Date.now() when timestamp is omitted', () => {
    const before = Date.now();
    appendLine({ type: 'say', characterName: 'A', text: 'hi' }, false);
    const after = Date.now();
    const [line] = get(lines);
    expect(line.timestamp.getTime()).toBeGreaterThanOrEqual(before);
    expect(line.timestamp.getTime()).toBeLessThanOrEqual(after);
  });
});
