// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, get } from 'svelte/store';

export const MAX_HISTORY = 100;

export interface CommandHistoryState {
  entries: string[];
  navIndex: number;  // -1 = not navigating; 0..entries.length-1 = position from newest
}

export const commandHistory = writable<CommandHistoryState>({
  entries: [],
  navIndex: -1,
});

export function pushCommand(cmd: string) {
  const trimmed = cmd.trim();
  if (!trimmed) return;
  commandHistory.update((s) => {
    const entries = [...s.entries];
    if (entries[entries.length - 1] === trimmed) {
      // dedupe consecutive duplicate
      return { entries, navIndex: -1 };
    }
    entries.push(trimmed);
    if (entries.length > MAX_HISTORY) entries.splice(0, entries.length - MAX_HISTORY);
    return { entries, navIndex: -1 };
  });
}

export function seedCommands(entries: string[]) {
  commandHistory.set({
    entries: entries.slice(-MAX_HISTORY),
    navIndex: -1,
  });
}

/**
 * Move one step back in history (toward older entries).
 * Returns the command at the new position, or null if already at oldest.
 */
export function navigatePrev(): string | null {
  const s = get(commandHistory);
  if (s.entries.length === 0) return null;
  const nextIdx = s.navIndex + 1;
  if (nextIdx >= s.entries.length) return null;
  commandHistory.update((prev) => ({ ...prev, navIndex: nextIdx }));
  return s.entries[s.entries.length - 1 - nextIdx];
}

/**
 * Move one step forward (toward newer entries). Returns the command at the
 * new position, empty string at the "past-newest" position (consumer clears
 * the input), or null if not currently navigating.
 */
export function navigateNext(): string | null {
  const s = get(commandHistory);
  if (s.navIndex < 0) return null;
  const nextIdx = s.navIndex - 1;
  if (nextIdx < 0) {
    commandHistory.update((prev) => ({ ...prev, navIndex: -1 }));
    return '';
  }
  commandHistory.update((prev) => ({ ...prev, navIndex: nextIdx }));
  return s.entries[s.entries.length - 1 - nextIdx];
}

export function resetNav() {
  commandHistory.update((s) => ({ ...s, navIndex: -1 }));
}
