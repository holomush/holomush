// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect } from 'vitest';
import { createPresenceStore } from './store';

describe('presence store', () => {
  it('seeds from snapshot', () => {
    const s = createPresenceStore();
    s.seed([
      { characterId: 'c1', name: 'alice', state: 'ACTIVE' },
      { characterId: 'c2', name: 'bob',   state: 'ACTIVE' },
    ]);
    expect(s.size()).toBe(2);
    expect(s.has('c1')).toBe(true);
    expect(s.has('c2')).toBe(true);
  });

  it('idempotent add', () => {
    const s = createPresenceStore();
    s.seed([{ characterId: 'c1', name: 'alice', state: 'ACTIVE' }]);
    s.upsert({ characterId: 'c1', name: 'alice', state: 'ACTIVE' });
    s.upsert({ characterId: 'c1', name: 'alice', state: 'ACTIVE' });
    expect(s.size()).toBe(1);
  });

  it('idempotent remove of absent id is a no-op', () => {
    const s = createPresenceStore();
    s.seed([{ characterId: 'c1', name: 'alice', state: 'ACTIVE' }]);
    s.remove('c999');
    expect(s.size()).toBe(1);
    expect(s.has('c1')).toBe(true);
  });

  it('clear() resets the store', () => {
    const s = createPresenceStore();
    s.seed([{ characterId: 'c1', name: 'alice', state: 'ACTIVE' }]);
    s.clear();
    expect(s.size()).toBe(0);
  });

  it('entries() returns array of current state', () => {
    const s = createPresenceStore();
    s.upsert({ characterId: 'c1', name: 'alice', state: 'ACTIVE' });
    s.upsert({ characterId: 'c2', name: 'bob',   state: 'ACTIVE' });
    const list = s.entries();
    expect(list).toHaveLength(2);
    expect(list.map(e => e.characterId).sort()).toEqual(['c1', 'c2']);
  });
});
