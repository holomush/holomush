// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { authState, setPlayerProfile, clearAuth } from './authStore';
import { get } from 'svelte/store';

describe('authStore.setPlayerProfile', () => {
  beforeEach(() => {
    sessionStorage.clear();
    clearAuth();
  });

  it('stores playerId, playerName, isGuest, characters', () => {
    setPlayerProfile({
      playerId: '01KQ2Y5ETK5957724MGZ2H2TDB',
      playerName: 'Jasper Iodine',
      isGuest: true,
      characters: [{ characterId: '01KQ', name: 'Jasper Iodine' }],
    });
    const s = get(authState);
    expect(s.playerId).toBe('01KQ2Y5ETK5957724MGZ2H2TDB');
    expect(s.playerName).toBe('Jasper Iodine');
    expect(s.isGuest).toBe(true);
    expect(s.characters).toHaveLength(1);
    expect(s.isPlayerAuthenticated).toBe(true);
  });

  it('clearAuth resets all profile fields', () => {
    setPlayerProfile({
      playerId: '01KQ',
      playerName: 'X',
      isGuest: false,
      characters: [],
    });
    clearAuth();
    const s = get(authState);
    expect(s.playerId).toBeNull();
    expect(s.playerName).toBeNull();
    expect(s.isGuest).toBe(false);
    expect(s.characters).toEqual([]);
    expect(s.isPlayerAuthenticated).toBe(false);
  });
});
