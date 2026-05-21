// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { SvelteMap } from 'svelte/reactivity';

export type PresenceState = 'UNSPECIFIED' | 'ACTIVE' | 'DETACHED' | 'INACTIVE';

export interface PresenceEntry {
  characterId: string;
  name: string;
  state: PresenceState;
}

export interface PresenceStore {
  seed(entries: PresenceEntry[]): void;
  upsert(entry: PresenceEntry): void;
  remove(characterId: string): void;
  clear(): void;
  has(characterId: string): boolean;
  size(): number;
  entries(): PresenceEntry[];
  readonly map: SvelteMap<string, PresenceEntry>;
}

export function createPresenceStore(): PresenceStore {
  const m = new SvelteMap<string, PresenceEntry>();
  return {
    seed(entries) {
      m.clear();
      for (const e of entries) m.set(e.characterId, e);
    },
    upsert(entry) {
      m.set(entry.characterId, entry);
    },
    remove(characterId) {
      m.delete(characterId);
    },
    clear() {
      m.clear();
    },
    has(characterId) {
      return m.has(characterId);
    },
    size() {
      return m.size;
    },
    entries() {
      return Array.from(m.values());
    },
    map: m,
  };
}

/**
 * Module-level singleton presence store consumed by the terminal sidebar.
 * Tests should use `createPresenceStore()` for isolation; production code
 * imports this singleton.
 */
export const presenceStore: PresenceStore = createPresenceStore();
