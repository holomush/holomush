// web/src/lib/components/shell/SectionRail.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount } from 'svelte';
import SectionRail from './SectionRail.svelte';

// themeStore calls localStorage.getItem and matchMedia at module load — before
// the test-setup.ts beforeEach polyfill runs. Hoist a stub so the module
// evaluates cleanly. Same pattern as src/lib/stores/themeStore.test.ts.
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
});

function render(props: { pathname: string; variant?: 'rail' | 'drawer'; isGuest?: boolean }) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(SectionRail, { target, props });
  return { target, component };
}

afterEach(() => document.body.replaceChildren());

describe('SectionRail', () => {
  it('renders one link per registry section, in order, to its href', () => {
    const { target, component } = render({ pathname: '/terminal' });
    const hrefs = [...target.querySelectorAll('a.rail-btn')].map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual(['/terminal', '/scenes']);
    unmount(component);
  });

  it('marks the active section from the pathname (prefix match)', () => {
    const { target, component } = render({ pathname: '/scenes/01HZN' });
    const active = target.querySelector('a.rail-btn.is-active');
    expect(active?.getAttribute('href')).toBe('/scenes');
    expect(active?.getAttribute('aria-current')).toBe('page');
    unmount(component);
  });

  it('shows text labels in the drawer variant', () => {
    const { target, component } = render({ pathname: '/terminal', variant: 'drawer' });
    expect(target.textContent).toContain('Room');
    expect(target.textContent).toContain('Scenes');
    unmount(component);
  });

  it('hides the Scenes link for a guest session', () => {
    const { target, component } = render({ pathname: '/terminal', isGuest: true });
    const hrefs = [...target.querySelectorAll('a.rail-btn')].map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual(['/terminal']);
    unmount(component);
  });
});
