// web/src/lib/stores/footerBridge.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { describe, expect, it } from 'vitest';
import { get } from 'svelte/store';
import type { Snippet } from 'svelte';
import { footerContent, setFooter, clearFooter } from './footerBridge';

describe('footerBridge', () => {
  it('stores and clears a footer snippet', () => {
    const fake = (() => {}) as unknown as Snippet;
    clearFooter();
    expect(get(footerContent)).toBeNull();
    setFooter(fake);
    expect(get(footerContent)).toBe(fake);
    clearFooter();
    expect(get(footerContent)).toBeNull();
  });
});
