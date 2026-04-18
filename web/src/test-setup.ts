// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

// Shared vitest setup: polyfill localStorage for jsdom.
// Node 25 + vitest 4 + jsdom 29 exposes `localStorage` without functioning
// getItem/setItem methods. This polyfill provides an in-memory backing
// store so code paths that touch localStorage work in tests.

import { beforeEach, vi } from 'vitest';

beforeEach(() => {
  const store = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => store.clear(),
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() { return store.size; },
  });
});
