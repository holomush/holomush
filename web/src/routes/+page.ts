// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { clearAuth, setPlayerProfile } from '$lib/stores/authStore';
import { listContent } from '$lib/stores/contentStore';
import type { ContentItem } from '$lib/stores/contentStore';
import { isStaleSession } from '$lib/util/stale';
import type { PageLoad } from './$types';

export const ssr = false;

export const load: PageLoad = async () => {
  // Existing landing content flow — preserved verbatim.
  let items: ContentItem[] = [];
  try {
    items = await listContent('landing.');
  } catch {
    // Content service unavailable — render with fallback defaults.
  }

  const hero = items.find((i) => i.key === 'landing.hero');
  const pitch = items.find((i) => i.key === 'landing.pitch');
  const features: ContentItem[] = items
    .filter((i) => i.key.startsWith('landing.features.'))
    .sort((a, b) => Number(a.metadata.order ?? '99') - Number(b.metadata.order ?? '99'));
  const connectInfo = items.find((i) => i.key === 'landing.connect');
  const baseData = { hero, pitch, features, connectInfo };

  // SSR guard — `ssr = false` above already ensures this runs client-side,
  // but keep the explicit window check for safety in any pre-render path.
  if (typeof window === 'undefined') {
    return { ...baseData, authenticated: false };
  }

  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerProfile({
      playerId: resp.playerId,
      playerName: resp.playerName,
      isGuest: resp.isGuest,
      characters: resp.characters.map((c) => ({
        characterId: c.characterId,
        name: c.characterName,
      })),
    });
    return {
      ...baseData,
      authenticated: true,
      playerName: resp.playerName,
      characters: resp.characters,
    };
  } catch (e) {
    // See login/+page.ts: only clear on real stale-session signals so a
    // transient webCheckSession outage doesn't log a returning user out.
    if (isStaleSession(e)) {
      clearAuth();
    }
    return { ...baseData, authenticated: false };
  }
};
