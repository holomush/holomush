// web/src/lib/stores/adminNavStore.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { describe, it, expect, beforeEach } from 'vitest';
import { get } from 'svelte/store';
import { adminNavSections, setAdminSections, clearAdminSections } from './adminNavStore';

describe('adminNavStore', () => {
  beforeEach(() => clearAdminSections());

  it('starts empty, so a non-admin route has no Admin group to draw', () => {
    expect(get(adminNavSections)).toEqual([]);
  });

  it('carries the sections it was set with, verbatim and in order', () => {
    setAdminSections([
      { id: 'a', displayName: 'Alpha', status: 'available' },
      { id: 'b', displayName: 'Beta', status: 'planned' },
    ]);
    expect(get(adminNavSections).map((s) => s.id)).toEqual(['a', 'b']);
    expect(get(adminNavSections)[1].status).toBe('planned');
  });

  it('clears back to empty on teardown, so a stale Admin group cannot outlive the route', () => {
    setAdminSections([{ id: 'a', displayName: 'Alpha', status: 'available' }]);
    clearAdminSections();
    expect(get(adminNavSections)).toEqual([]);
  });
});
