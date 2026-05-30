// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable } from 'svelte/store';
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

export interface CommandListState {
  names: Set<string>; // canonical command names the character may run
  aliases: Record<string, string>; // alias → canonical name
  incomplete: boolean;
}

const empty: CommandListState = { names: new Set(), aliases: {}, incomplete: false };
export const commandList = writable<CommandListState>(empty);

const client = createClient(WebService, transport);

export function resetCommandList(): void {
  commandList.set({ names: new Set(), aliases: {}, incomplete: false });
}

export function seedCommandList(resp: {
  commands: { name: string }[];
  aliases: Record<string, string>;
  incomplete: boolean;
}): void {
  commandList.set({
    names: new Set(resp.commands.map((c) => c.name)),
    aliases: { ...resp.aliases },
    incomplete: resp.incomplete,
  });
}

// fetchCommandList loads the recognized-command set for a session. Mirrors the
// stale-session guard used by CommandInput's history fetch. Errors degrade to an
// empty list (chip simply won't render — INV-6 graceful degradation).
export async function fetchCommandList(sessionId: string): Promise<void> {
  if (!sessionId) {
    resetCommandList();
    return;
  }
  try {
    const resp = await client.webListCommands({ sessionId });
    seedCommandList({
      commands: (resp.commands ?? []).map((c) => ({ name: c.name })),
      aliases: resp.aliases ?? {},
      incomplete: resp.incomplete ?? false,
    });
  } catch (e) {
    console.warn('[commands] list load failed', e);
    resetCommandList();
  }
}
