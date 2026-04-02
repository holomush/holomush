// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, derived } from 'svelte/store';
import { trace } from '@opentelemetry/api';

const tracer = trace.getTracer('holomush-web');

interface AuthState {
  playerSessionToken: string | null;
  sessionId: string | null;
  characterName: string | null;
  playerName: string | null;
  isGuest: boolean;
}

const initial: AuthState = {
  playerSessionToken: null,
  sessionId: null,
  characterName: null,
  playerName: null,
  isGuest: false,
};

export const authState = writable<AuthState>(initial);
export const isAuthenticated = derived(authState, ($s) => !!$s.playerSessionToken || !!$s.sessionId);
export const hasCharacter = derived(authState, ($s) => !!$s.sessionId && !!$s.characterName);

export function setPlayerAuth(playerSessionToken: string, playerName: string) {
  authState.update((s) => ({ ...s, playerSessionToken, playerName, isGuest: false }));
  sessionStorage.setItem('holomush-player', JSON.stringify({ playerSessionToken, playerName }));
}

export function setCharacterSession(sessionId: string, characterName: string) {
  authState.update((s) => ({ ...s, sessionId, characterName }));
  sessionStorage.setItem('holomush-session', JSON.stringify({ sessionId, characterName }));
}

export function setGuestSession(sessionId: string, characterName: string) {
  authState.update((s) => ({
    ...s,
    sessionId,
    characterName,
    isGuest: true,
    playerName: characterName,
  }));
  sessionStorage.setItem('holomush-session', JSON.stringify({ sessionId, characterName }));
}

export function clearAuth() {
  authState.set(initial);
  sessionStorage.removeItem('holomush-session');
  sessionStorage.removeItem('holomush-player');
}

export function clearCharacterSession() {
  authState.update((s) => ({ ...s, sessionId: null, characterName: null }));
  sessionStorage.removeItem('holomush-session');
}

export function restoreSession(): void {
  const span = tracer.startSpan('session.restore');
  try {
    const saved = sessionStorage.getItem('holomush-session');
    if (saved) {
      try {
        const { sessionId, characterName } = JSON.parse(saved);
        if (sessionId) authState.update((s) => ({ ...s, sessionId, characterName }));
      } catch {
        /* ignore corrupt data */
      }
    }
    const playerSaved = sessionStorage.getItem('holomush-player');
    if (playerSaved) {
      try {
        const { playerSessionToken, playerName } = JSON.parse(playerSaved);
        if (playerSessionToken) authState.update((s) => ({ ...s, playerSessionToken, playerName }));
      } catch {
        /* ignore corrupt data */
      }
    }
  } finally {
    span.end();
  }
}
