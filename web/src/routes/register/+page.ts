// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { setPlayerProfile, clearAuth } from '$lib/stores/authStore';
import { isStaleSession } from '$lib/util/stale';
import type { PageLoad } from './$types';

export const ssr = false;

export const load: PageLoad = async () => {
  if (typeof window === 'undefined') return { authenticated: false };

  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerProfile({
      playerId: resp.playerId,
      playerName: resp.playerName,
      isGuest: resp.isGuest,
      characters: resp.characters.map((c) => ({ characterId: c.characterId, name: c.characterName })),
    });
    return { authenticated: true, playerName: resp.playerName, characters: resp.characters };
  } catch (e) {
    // See login/+page.ts: only clear on real stale-session signals.
    if (isStaleSession(e)) {
      clearAuth();
    }
    return { authenticated: false };
  }
};
