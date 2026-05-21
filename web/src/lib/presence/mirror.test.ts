// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, vi } from 'vitest';
import { createPresenceStore } from './store';
import { mirrorMovementPresence } from './mirror';

function makeLegacy() {
  return { add: vi.fn<(name: string) => void>(), remove: vi.fn<(name: string) => void>() };
}

describe('mirrorMovementPresence', () => {
  it('arrive: updates new PresenceStore + legacy add', () => {
    const presence = createPresenceStore();
    const legacy = makeLegacy();
    mirrorMovementPresence(
      { category: 'movement', type: 'arrive', actorId: '01HYXCHARALICE0000000000AA', actor: 'Alice' },
      presence,
      legacy,
    );
    expect(presence.has('01HYXCHARALICE0000000000AA')).toBe(true);
    expect(legacy.add).toHaveBeenCalledExactlyOnceWith('Alice');
    expect(legacy.remove).not.toHaveBeenCalled();
  });

  it('leave: removes from new PresenceStore + legacy remove', () => {
    const presence = createPresenceStore();
    presence.upsert({ characterId: '01HYXCHARALICE0000000000AA', name: 'Alice', state: 'ACTIVE' });
    const legacy = makeLegacy();
    mirrorMovementPresence(
      { category: 'movement', type: 'leave', actorId: '01HYXCHARALICE0000000000AA', actor: 'Alice' },
      presence,
      legacy,
    );
    expect(presence.has('01HYXCHARALICE0000000000AA')).toBe(false);
    expect(legacy.remove).toHaveBeenCalledExactlyOnceWith('Alice');
    expect(legacy.add).not.toHaveBeenCalled();
  });

  it('duplicate arrive for same actorId is idempotent in the new store', () => {
    const presence = createPresenceStore();
    const legacy = makeLegacy();
    const ev = { category: 'movement', type: 'arrive', actorId: '01HYXCHARALICE0000000000AA', actor: 'Alice' };
    mirrorMovementPresence(ev, presence, legacy);
    mirrorMovementPresence(ev, presence, legacy);
    mirrorMovementPresence(ev, presence, legacy);
    expect(presence.size()).toBe(1);
    // Legacy is set-keyed by name and idempotent in its own store; the helper
    // re-calls .add each time, which is the documented contract.
    expect(legacy.add).toHaveBeenCalledTimes(3);
  });

  it('non-movement event is a no-op on both stores', () => {
    const presence = createPresenceStore();
    const legacy = makeLegacy();
    mirrorMovementPresence(
      { category: 'communication', type: 'say', actorId: 'whatever', actor: 'Alice' },
      presence,
      legacy,
    );
    expect(presence.size()).toBe(0);
    expect(legacy.add).not.toHaveBeenCalled();
    expect(legacy.remove).not.toHaveBeenCalled();
  });

  it('missing actorId: skips the new store, still updates legacy by name', () => {
    const presence = createPresenceStore();
    const legacy = makeLegacy();
    mirrorMovementPresence(
      { category: 'movement', type: 'arrive', actor: 'Alice' },
      presence,
      legacy,
    );
    expect(presence.size()).toBe(0);
    expect(legacy.add).toHaveBeenCalledExactlyOnceWith('Alice');
  });

  it('missing actor name: updates new store, skips legacy', () => {
    const presence = createPresenceStore();
    const legacy = makeLegacy();
    mirrorMovementPresence(
      { category: 'movement', type: 'arrive', actorId: '01HYXCHARALICE0000000000AA' },
      presence,
      legacy,
    );
    expect(presence.has('01HYXCHARALICE0000000000AA')).toBe(true);
    // Entry has empty name field — UI can fall back / show placeholder.
    expect(presence.entries()[0].name).toBe('');
    expect(legacy.add).not.toHaveBeenCalled();
  });

  it('legacy parameter is optional (T12 path: legacy store removed)', () => {
    const presence = createPresenceStore();
    expect(() => {
      mirrorMovementPresence(
        { category: 'movement', type: 'arrive', actorId: '01HYXCHARALICE0000000000AA', actor: 'Alice' },
        presence,
      );
    }).not.toThrow();
    expect(presence.has('01HYXCHARALICE0000000000AA')).toBe(true);
  });
});
