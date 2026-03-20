import { writable } from 'svelte/store';

export interface RoomLocation {
  id: string;
  name: string;
  description: string;
}

export interface RoomExit {
  direction: string;
  name: string;
  locked: boolean;
}

export interface RoomCharacter {
  name: string;
  idle: boolean;
}

export const location = writable<RoomLocation | null>(null);
export const exits = writable<RoomExit[]>([]);
export const presence = writable<RoomCharacter[]>([]);
export const sidebarExpanded = writable<boolean>(false);

export function applyRoomState(metadata: Record<string, unknown>) {
  const loc = metadata.location as RoomLocation | undefined;
  if (loc) location.set(loc);
  const ex = metadata.exits as RoomExit[] | undefined;
  if (ex) exits.set(ex);
  const pr = metadata.present as RoomCharacter[] | undefined;
  if (pr) presence.set(pr);
}

export function addPresence(name: string) {
  presence.update((list) => {
    if (!list.some((c) => c.name === name)) {
      return [...list, { name, idle: false }];
    }
    return list;
  });
}

export function removePresence(name: string) {
  presence.update((list) => list.filter((c) => c.name !== name));
}

export function toggleSidebar() {
  sidebarExpanded.update((v) => !v);
}
