// web/src/lib/nav/sections.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { describe, expect, it } from 'vitest';
import { SECTIONS, activeSectionId, activeSectionLabel, sectionNavEntries } from './sections';

describe('section registry', () => {
  it('lists Room then Scenes with their routes', () => {
    expect(SECTIONS.map((s) => s.id)).toEqual(['room', 'scenes']);
    expect(SECTIONS.map((s) => s.href)).toEqual(['/terminal', '/scenes']);
  });
});

describe('activeSectionId uses prefix match', () => {
  it('marks Room active on /terminal', () => {
    expect(activeSectionId('/terminal')).toBe('room');
  });
  it('marks Scenes active on /scenes and nested routes', () => {
    expect(activeSectionId('/scenes')).toBe('scenes');
    expect(activeSectionId('/scenes/browse')).toBe('scenes');
    expect(activeSectionId('/scenes/01HZN3XS')).toBe('scenes');
  });
  it('does not false-match a sibling prefix', () => {
    expect(activeSectionId('/scenesfoo')).toBeNull();
  });
  it('returns null for an unregistered route', () => {
    expect(activeSectionId('/characters')).toBeNull();
  });
});

describe('activeSectionLabel', () => {
  it('returns the active section label', () => {
    expect(activeSectionLabel('/scenes/x')).toBe('Scenes');
    expect(activeSectionLabel('/characters')).toBeNull();
  });
});

describe('sectionNavEntries', () => {
  it('derives palette go-to entries from the same registry', () => {
    expect(sectionNavEntries()).toEqual([
      { id: 'nav.room', label: 'Go to Room', href: '/terminal' },
      { id: 'nav.scenes', label: 'Go to Scenes', href: '/scenes' },
    ]);
  });
});
