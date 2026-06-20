// web/src/lib/stores/mobileNavStore.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { beforeEach, describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import { mobileNavOpen, openMobileNav, closeMobileNav, toggleMobileNav } from './mobileNavStore';

beforeEach(() => closeMobileNav());

describe('mobileNavStore', () => {
  it('opens, closes, and toggles', () => {
    expect(get(mobileNavOpen)).toBe(false);
    openMobileNav();
    expect(get(mobileNavOpen)).toBe(true);
    toggleMobileNav();
    expect(get(mobileNavOpen)).toBe(false);
    toggleMobileNav();
    expect(get(mobileNavOpen)).toBe(true);
    closeMobileNav();
    expect(get(mobileNavOpen)).toBe(false);
  });
});
