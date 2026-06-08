// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { redirect } from '@sveltejs/kit';
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';

export const ssr = false;

// UX guard: redirects guests to /terminal.
// The real enforcement gate is the SceneAccessService facade (INV-SCENE-64);
// this redirect is client-side convenience only.
export const load = async () => {
  const client = createClient(WebService, transport);
  const session = await client.webCheckSession({}); // throws on unauthenticated → (authed) layout redirects to /login
  if (session.isGuest) throw redirect(302, '/terminal');
  return { playerId: session.playerId, characters: session.characters };
};
