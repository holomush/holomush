// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, derived } from 'svelte/store';

interface AuthState {
  playerToken: string | null;
  sessionId: string | null;
  characterName: string | null;
  playerName: string | null;
  isGuest: boolean;
}

const initial: AuthState = {
  playerToken: null,
  sessionId: null,
  characterName: null,
  playerName: null,
  isGuest: false,
};

export const authState = writable<AuthState>(initial);
export const isAuthenticated = derived(authState, ($s) => !!$s.playerToken || !!$s.sessionId);
export const hasCharacter = derived(authState, ($s) => !!$s.sessionId && !!$s.characterName);

export function setPlayerAuth(playerToken: string, playerName: string) {
  authState.update((s) => ({ ...s, playerToken, playerName, isGuest: false }));
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
}

export function restoreSession() {
  const saved = sessionStorage.getItem('holomush-session');
  if (saved) {
    try {
      const { sessionId, characterName } = JSON.parse(saved);
      if (sessionId) authState.update((s) => ({ ...s, sessionId, characterName }));
    } catch {
      /* ignore corrupt data */
    }
  }
}
