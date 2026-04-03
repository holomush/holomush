// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { get } from 'svelte/store';
import { redirect } from '@sveltejs/kit';
import { createClient } from '@connectrpc/connect';
import { WebService } from '$lib/connect/holomush/web/v1/web_pb';
import { transport } from '$lib/transport';
import { authState, clearAuth, setPlayerAuth, restoreSession } from '$lib/stores/authStore';

export const ssr = false;

export async function load() {
  if (typeof window === 'undefined') return;

  // Restore game session (sessionId/characterName) from sessionStorage.
  restoreSession();

  // If a game session was restored (guest or character), we're already
  // authenticated at the session level — no server round-trip needed.
  if (get(authState).sessionId) return;

  // Validate player auth via cookie — server is the authority.
  const client = createClient(WebService, transport);
  try {
    const resp = await client.webCheckSession({});
    setPlayerAuth(resp.playerName);
  } catch {
    clearAuth();
    redirect(302, '/login');
  }
}
