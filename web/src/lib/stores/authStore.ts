// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { writable, derived } from 'svelte/store';
import { trace } from '@opentelemetry/api';

const tracer = trace.getTracer('holomush-web');

export interface CharacterSummary {
  characterId: string;
  name?: string;
}

interface AuthState {
  isPlayerAuthenticated: boolean;
  sessionId: string | null;
  characterName: string | null;
  playerName: string | null;
  playerId: string | null;
  isGuest: boolean;
  characters: CharacterSummary[];
}

const initial: AuthState = {
  isPlayerAuthenticated: false,
  sessionId: null,
  characterName: null,
  playerName: null,
  playerId: null,
  isGuest: false,
  characters: [],
};

export const authState = writable<AuthState>(initial);
export const isAuthenticated = derived(authState, ($s) => $s.isPlayerAuthenticated || !!$s.sessionId);
export const hasCharacter = derived(authState, ($s) => !!$s.sessionId && !!$s.characterName);

export function setPlayerProfile(profile: {
  playerId: string;
  playerName: string;
  isGuest: boolean;
  characters: CharacterSummary[];
}) {
  sessionStorage.removeItem('holomush-player'); // clean up legacy raw-token key
  authState.update((s) => ({
    ...s,
    isPlayerAuthenticated: true,
    playerId: profile.playerId,
    playerName: profile.playerName,
    isGuest: profile.isGuest,
    characters: profile.characters,
  }));
}

export function setCharacterSession(sessionId: string, characterName: string) {
  authState.update((s) => ({ ...s, sessionId, characterName }));
  sessionStorage.setItem('holomush-session', JSON.stringify({ sessionId, characterName }));
}

export function clearAuth() {
  authState.set(initial);
  sessionStorage.removeItem('holomush-session');
}

export function clearCharacterSession() {
  authState.update((s) => ({ ...s, sessionId: null, characterName: null }));
  sessionStorage.removeItem('holomush-session');
}

export function restoreSession(): void {
  const span = tracer.startSpan('session.restore');
  try {
    sessionStorage.removeItem('holomush-player'); // clean up legacy raw-token key
    const saved = sessionStorage.getItem('holomush-session');
    if (saved) {
      try {
        const { sessionId, characterName } = JSON.parse(saved);
        if (sessionId) authState.update((s) => ({ ...s, sessionId, characterName }));
      } catch {
        /* ignore corrupt data */
      }
    }
  } finally {
    span.end();
  }
}
