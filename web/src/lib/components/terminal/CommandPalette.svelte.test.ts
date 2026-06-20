// web/src/lib/components/terminal/CommandPalette.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount, tick } from 'svelte';
import CommandPalette from './CommandPalette.svelte';
import { authState } from '$lib/stores/authStore';
import { openPalette, closePalette } from '$lib/stores/uiPrefsStore';

// themeStore reads localStorage/matchMedia at module load — before test-setup's
// beforeEach polyfill runs. Hoist a stub so the module evaluates. Same pattern
// as SectionRail.svelte.test.ts.
vi.hoisted(() => {
  const store = new Map<string, string>();
  globalThis.localStorage = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, String(v)),
    removeItem: (k: string) => void store.delete(k),
    clear: () => store.clear(),
    key: (i: number) => Array.from(store.keys())[i] ?? null,
    get length() { return store.size; },
  } as Storage;
  globalThis.matchMedia ??= ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as typeof globalThis.matchMedia;
  // bits-ui Command observes its list size; jsdom has no ResizeObserver.
  globalThis.ResizeObserver ??= class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof globalThis.ResizeObserver;
  // bits-ui Command scrolls the active item into view; jsdom lacks the method.
  if (!globalThis.Element.prototype.scrollIntoView) {
    globalThis.Element.prototype.scrollIntoView = () => {};
  }
});

// The palette imports goto from $app/navigation; stub it so the module mounts.
vi.mock('$app/navigation', () => ({ goto: vi.fn() }));

function setAuth(isGuest: boolean) {
  // Both a guest and a registered player are authenticated; only isGuest differs.
  authState.set({
    isPlayerAuthenticated: true,
    sessionId: null,
    characterName: null,
    playerName: null,
    playerId: null,
    isGuest,
    characters: [],
  });
}

// The bits-ui dialog portal mounts its content a macrotask after open; flush
// Svelte's microtask queue around it so the rendered items settle.
async function flush() {
  await tick();
  await new Promise((r) => setTimeout(r, 0));
  await tick();
}

afterEach(() => {
  closePalette();
  setAuth(false);
  document.body.replaceChildren();
});

describe('CommandPalette guest gate', () => {
  // The palette mounts once in the persistent ROOT layout, before route load
  // functions resolve the session's guest status — so its go-to entries MUST
  // react to authState rather than snapshot it at construction. Per holomush-5rh.23.
  it('drops the Scenes go-to entry when the session resolves as a guest after mount', async () => {
    setAuth(false); // pre-auth default — the palette mounts with this snapshot
    openPalette(); // open before mount so the portal renders its items
    const target = document.createElement('div');
    document.body.appendChild(target);
    const component = mount(CommandPalette, { target });
    await flush();
    expect(document.body.textContent).toContain('Go to Scenes');
    expect(document.body.textContent).toContain('Go to Room');

    // Auth resolves as a guest AFTER the palette already mounted.
    setAuth(true);
    await flush();
    expect(document.body.textContent).not.toContain('Go to Scenes');
    expect(document.body.textContent).toContain('Go to Room');

    unmount(component);
  });
});
