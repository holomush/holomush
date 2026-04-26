// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { isRedirect, redirect } from '@sveltejs/kit';
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { clearAuth, setPlayerProfile, restoreSession } from '$lib/stores/authStore';

export const ssr = false;

export async function load() {
  if (typeof window === 'undefined') return;

  restoreSession();

  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerProfile({
      playerId: resp.playerId,
      playerName: resp.playerName,
      isGuest: resp.isGuest,
      characters: resp.characters.map((c) => ({ characterId: c.characterId, name: c.characterName })),
    });
  } catch (e) {
    if (isRedirect(e)) throw e;
    clearAuth();
    redirect(302, '/login');
  }
}
