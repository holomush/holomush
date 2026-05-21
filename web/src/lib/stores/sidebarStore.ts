// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable } from 'svelte/store';
import { presenceStore } from '$lib/presence/store';

export interface RoomLocation {
  id: string;
  name: string;
  description: string;
  mood?: string;  // optional; populated by server when holomush-uhiz lands
}

export interface RoomExit {
  direction: string;
  name: string;
  locked: boolean;
}

export interface RoomCharacter {
  name: string;
  idle: boolean;
  lastMode?: 'say' | 'pose' | 'ooc' | 'sys';  // optional; holomush-uhiz
  isIdle?: boolean;  // optional; holomush-uhiz — distinct from presence.idle which is per-char timeout
}

export const location = writable<RoomLocation | null>(null);
export const exits = writable<RoomExit[]>([]);
// The `presence` writable is kept for any consumers that still read it
// (TopBar, RoomInfo, ExitList do not; only the now-migrated PresenceList did).
// It is no longer the authoritative source for the sidebar UI.
export const presence = writable<RoomCharacter[]>([]);

export function applyLocationState(metadata: Record<string, unknown>) {
  const loc = metadata.location as RoomLocation | undefined;
  if (loc) location.set(loc);
  const ex = metadata.exits as RoomExit[] | undefined;
  if (ex) exits.set(ex);
  const pr = metadata.present as Array<{ character_id?: string; name?: string; idle?: boolean }> | undefined;
  if (pr) {
    // Keep the legacy writable populated for any remaining consumer.
    presence.set(pr.map((c) => ({ name: c.name ?? '', idle: c.idle ?? false })));
    // Seed the new PresenceStore from the location_state snapshot.
    // Prefer the server-emitted character_id (ULID); fall back to name only if
    // it is absent — that's the forward-compat path for events from older
    // servers. New servers MUST populate character_id (see holomush-e4qo).
    presenceStore.seed(
      pr
        .filter((c) => c.name)
        .map((c) => ({
          characterId: c.character_id ?? (c.name as string),
          name: c.name as string,
          state: 'ACTIVE' as const,
        })),
    );
  }
}
