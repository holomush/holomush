// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { afterEach, describe, expect, it, vi } from 'vitest';
import { flushSync } from 'svelte';
import { mediaQuery, isDesktop, DESKTOP_MEDIA_QUERY } from './mediaQuery.svelte';

interface FakeMql {
  matches: boolean;
  media: string;
  addEventListener: (t: string, cb: (e: MediaQueryListEvent) => void) => void;
  removeEventListener: (t: string, cb: (e: MediaQueryListEvent) => void) => void;
  emit: (matches: boolean) => void;
}

/** Install a controllable window.matchMedia and record the queries it sees. */
function installMatchMedia(initial: boolean): { mql: FakeMql; queries: string[] } {
  const queries: string[] = [];
  let listeners: ((e: MediaQueryListEvent) => void)[] = [];
  const mql: FakeMql = {
    matches: initial,
    media: '',
    addEventListener: (_t, cb) => void listeners.push(cb),
    removeEventListener: (_t, cb) => void (listeners = listeners.filter((l) => l !== cb)),
    emit: (matches) => {
      mql.matches = matches;
      listeners.forEach((l) => l({ matches } as MediaQueryListEvent));
    },
  };
  vi.stubGlobal('matchMedia', (q: string) => {
    queries.push(q);
    mql.media = q;
    return mql;
  });
  return { mql, queries };
}

afterEach(() => vi.unstubAllGlobals());

describe('mediaQuery', () => {
  it('reflects the initial match state synchronously', () => {
    installMatchMedia(true);
    const cleanup = $effect.root(() => {
      const mq = mediaQuery('(min-width: 768px)');
      expect(mq.current).toBe(true);
    });
    cleanup();
  });

  it('updates reactively when the media query changes', () => {
    const { mql } = installMatchMedia(false);
    const cleanup = $effect.root(() => {
      const mq = mediaQuery('(min-width: 768px)');
      flushSync();
      expect(mq.current).toBe(false);
      mql.emit(true);
      flushSync();
      expect(mq.current).toBe(true);
    });
    cleanup();
  });

  it('removes its change listener on teardown', () => {
    const { mql } = installMatchMedia(true);
    let removed = false;
    const orig = mql.removeEventListener;
    mql.removeEventListener = (t, cb) => {
      removed = true;
      orig(t, cb);
    };
    const cleanup = $effect.root(() => {
      mediaQuery('(min-width: 768px)');
      flushSync();
    });
    cleanup();
    expect(removed).toBe(true);
  });

  it('isDesktop tracks the Tailwind md breakpoint', () => {
    const { queries } = installMatchMedia(true);
    const cleanup = $effect.root(() => {
      const d = isDesktop();
      expect(d.current).toBe(true);
    });
    cleanup();
    expect(DESKTOP_MEDIA_QUERY).toBe('(min-width: 768px)');
    expect(queries).toContain(DESKTOP_MEDIA_QUERY);
  });
});
