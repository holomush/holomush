// web/src/lib/components/shell/SectionRail.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount } from 'svelte';
import SectionRail from './SectionRail.svelte';
import { setAdminSections, clearAdminSections } from '$lib/stores/adminNavStore';

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

function render(props: {
  pathname: string;
  variant?: 'rail' | 'drawer';
  isGuest?: boolean;
  roles?: string[];
}) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(SectionRail, { target, props });
  return { target, component };
}

const adminLinks = (target: HTMLElement) =>
  [...target.querySelectorAll('a.rail-btn')].filter((a) => a.getAttribute('href')?.startsWith('/admin'));

afterEach(() => {
  clearAdminSections();
  document.body.replaceChildren();
});

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

// The Admin entry is a NAV HINT drawn from the roles the session response
// carried, and from nothing else. Hiding it is a courtesy: a viewer who forges
// a role gets an entry leading to a route whose every RPC denies them.
describe('SectionRail — the Admin entry', () => {
  it('renders exactly one /admin link when roles contain admin', () => {
    const { target, component } = render({ pathname: '/terminal', roles: ['admin'] });
    const hrefs = adminLinks(target).map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual(['/admin']);
    unmount(component);
  });

  it('renders zero /admin links for a viewer with no roles', () => {
    const { target, component } = render({ pathname: '/terminal', roles: [] });
    expect(adminLinks(target)).toHaveLength(0);
    unmount(component);
  });

  it('renders zero /admin links for a viewer holding some other role', () => {
    const { target, component } = render({ pathname: '/terminal', roles: ['moderator'] });
    expect(adminLinks(target)).toHaveLength(0);
    unmount(component);
  });

  it('announces the Admin entry the same way its shipped siblings do', () => {
    const { target, component } = render({ pathname: '/terminal', roles: ['admin'] });
    const link = adminLinks(target)[0];
    expect(link.getAttribute('aria-label')).toBe('Admin');
    expect(link.getAttribute('title')).toBe('Admin');
    unmount(component);
  });
});

// The <768px drawer holds both nav levels in the ONE shipped Sheet. The admin
// group arrives through adminNavStore, which this component reads itself.
describe('SectionRail — the drawer groups', () => {
  it('renders both group labels and one link per store member when the store is populated', () => {
    setAdminSections([
      { id: 'characters', displayName: 'Characters', status: 'available' },
      { id: 'x', displayName: 'Ecks', status: 'planned' },
      { id: 'y', displayName: 'Why', status: 'planned' },
    ]);
    const { target, component } = render({ pathname: '/admin/characters', variant: 'drawer', roles: ['admin'] });
    const labels = [...target.querySelectorAll('.rail-group-label')].map((n) => n.textContent?.trim());
    expect(labels).toEqual(['Workspace', 'Admin']);
    expect(target.querySelectorAll('a.rail-admin-item')).toHaveLength(3);
    unmount(component);
  });

  it('renders no Admin label and zero admin items when the store is empty', () => {
    clearAdminSections();
    const { target, component } = render({ pathname: '/terminal', variant: 'drawer', roles: ['admin'] });
    const labels = [...target.querySelectorAll('.rail-group-label')].map((n) => n.textContent?.trim());
    expect(labels).toEqual(['Workspace']);
    expect(target.querySelectorAll('a.rail-admin-item')).toHaveLength(0);
    unmount(component);
  });

  it('renders neither group label in the persistent rail variant', () => {
    setAdminSections([{ id: 'characters', displayName: 'Characters', status: 'available' }]);
    const { target, component } = render({ pathname: '/admin/characters', variant: 'rail', roles: ['admin'] });
    expect(target.querySelectorAll('.rail-group-label')).toHaveLength(0);
    unmount(component);
  });

  it('closes the drawer through the shipped onnavigate callback when an admin item is clicked', () => {
    setAdminSections([{ id: 'characters', displayName: 'Characters', status: 'available' }]);
    let closed = 0;
    const target = document.createElement('div');
    document.body.appendChild(target);
    const component = mount(SectionRail, {
      target,
      props: { pathname: '/admin', variant: 'drawer', roles: ['admin'], onnavigate: () => (closed += 1) },
    });
    (target.querySelector('a.rail-admin-item') as HTMLAnchorElement).click();
    expect(closed).toBe(1);
    unmount(component);
  });
});
