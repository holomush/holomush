// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';

// Mock the connect client factory so the store's module-scope client is a stub
// we control. createClient is the only runtime export the store uses.
const { webListCommandsMock } = vi.hoisted(() => ({ webListCommandsMock: vi.fn() }));
vi.mock('@connectrpc/connect', () => ({
  createClient: () => ({ webListCommands: webListCommandsMock }),
}));

import {
  commandList,
  seedCommandList,
  resetCommandList,
  fetchCommandList,
} from './commandListStore';

describe('commandListStore', () => {
  beforeEach(() => {
    resetCommandList();
    webListCommandsMock.mockReset();
  });

  it('starts empty', () => {
    const s = get(commandList);
    expect(s.names.size).toBe(0);
    expect(Object.keys(s.aliases)).toHaveLength(0);
  });

  it('seedCommandList stores names as a set and keeps aliases', () => {
    seedCommandList({
      commands: [{ name: 'look' }, { name: 'scene' }],
      aliases: { l: 'look', '"': 'say' },
      incomplete: false,
    });
    const s = get(commandList);
    expect(s.names.has('scene')).toBe(true);
    expect(s.aliases['l']).toBe('look');
    expect(s.incomplete).toBe(false);
  });

  it('fetchCommandList resets to empty for a blank sessionId without calling the RPC', async () => {
    seedCommandList({ commands: [{ name: 'look' }], aliases: { l: 'look' }, incomplete: false });
    await fetchCommandList('');
    expect(get(commandList).names.size).toBe(0);
    expect(webListCommandsMock).not.toHaveBeenCalled();
  });

  it('fetchCommandList seeds the store from a successful response', async () => {
    webListCommandsMock.mockResolvedValueOnce({
      commands: [{ name: 'scene' }],
      aliases: { sc: 'scene' },
      incomplete: true,
    });
    await fetchCommandList('sess-1');
    const s = get(commandList);
    expect(webListCommandsMock).toHaveBeenCalledWith({ sessionId: 'sess-1' });
    expect(s.names.has('scene')).toBe(true);
    expect(s.aliases['sc']).toBe('scene');
    expect(s.incomplete).toBe(true);
  });

  it('fetchCommandList degrades to an empty list when the RPC rejects (INV-6)', async () => {
    seedCommandList({ commands: [{ name: 'look' }], aliases: { l: 'look' }, incomplete: false });
    webListCommandsMock.mockRejectedValueOnce(new Error('permission denied'));
    await fetchCommandList('sess-2');
    const s = get(commandList);
    expect(s.names.size).toBe(0);
    expect(Object.keys(s.aliases)).toHaveLength(0);
    expect(s.incomplete).toBe(false);
  });
});
