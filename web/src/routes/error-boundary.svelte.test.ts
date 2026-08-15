// web/src/routes/error-boundary.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// The `.svelte.test.ts` spelling is load-bearing: web/vite.config.ts routes only
// that glob to the client Vitest project, whose resolve.conditions ['browser'] is
// what makes `mount` work. The `error-boundary` stem is also load-bearing, in the
// other direction: SvelteKit refuses to build when src/routes/ holds a
// `+`-prefixed file it does not recognize as a route file, so the boundary's test
// cannot be named after the boundary.
import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import ErrorBoundary from './+error.svelte';
import { authState } from '$lib/stores/authStore';

// The three cases below are distinguished by their INPUT — the auth store's
// resolution bit and guest bit — not by their output. Cases (a) and (b) are
// asserted to render IDENTICALLY; see the case (b) name for why.

const UNRESOLVED = {
  isPlayerAuthenticated: false,
  sessionId: null,
  characterName: null,
  playerName: null,
  playerId: null,
  isGuest: false,
  characters: [],
  roles: [],
};

const RESOLVED_GUEST = { ...UNRESOLVED, isPlayerAuthenticated: true, isGuest: true };

const RESOLVED_REGISTERED = { ...UNRESOLVED, isPlayerAuthenticated: true, isGuest: false };

function render(state: typeof UNRESOLVED) {
  authState.set(state);
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(ErrorBoundary, { target });
  return { target, component };
}

function destinations(target: HTMLElement) {
  return [...target.querySelectorAll('a.destination')].map((a) => ({
    label: a.textContent?.trim(),
    href: a.getAttribute('href'),
  }));
}

afterEach(() => {
  authState.set(UNRESOLVED);
  document.body.replaceChildren();
});

describe('the root error boundary', () => {
  it('renders the ordinary not-found copy with no reason, path or status', () => {
    const { target, component } = render(RESOLVED_REGISTERED);
    expect(target.textContent).toContain('Not found');
    expect(target.textContent).toContain("We couldn't find that page.");
    unmount(component);
  });

  it('(a) renders Home plus the guest destination set when the auth store is unresolved', () => {
    const { target, component } = render(UNRESOLVED);
    expect(destinations(target)).toEqual([
      { label: 'Home', href: '/' },
      { label: 'Room', href: '/terminal' },
    ]);
    // The registered-player-only section must not appear before the session is
    // restored: showing it would correlate the destination list with a
    // permission the viewer has not been shown to hold.
    expect(target.textContent).not.toContain('Scenes');
    unmount(component);
  });

  it('(a) shows no spinner, no retry control and no failure copy while unresolved', () => {
    const { target, component } = render(UNRESOLVED);
    expect(target.querySelector('[aria-busy="true"]')).toBeNull();
    expect(target.querySelector('button')).toBeNull();
    expect(target.textContent).not.toMatch(/loading|retry|try again|unavailable|couldn't load/i);
    unmount(component);
  });

  it('(b) renders IDENTICALLY for a resolved guest — that identity is the anti-correlation property, so a difference here would be the leak', () => {
    const unresolved = render(UNRESOLVED);
    const unresolvedDestinations = destinations(unresolved.target);
    const unresolvedText = unresolved.target.textContent;
    unmount(unresolved.component);
    unresolved.target.remove();

    const guest = render(RESOLVED_GUEST);
    expect(destinations(guest.target)).toEqual([
      { label: 'Home', href: '/' },
      { label: 'Room', href: '/terminal' },
    ]);
    expect(destinations(guest.target)).toEqual(unresolvedDestinations);
    expect(guest.target.textContent).toEqual(unresolvedText);
    unmount(guest.component);
  });

  it('(c) renders Home plus both workspace sections for a resolved registered player', () => {
    const { target, component } = render(RESOLVED_REGISTERED);
    expect(destinations(target)).toEqual([
      { label: 'Home', href: '/' },
      { label: 'Room', href: '/terminal' },
      { label: 'Scenes', href: '/scenes' },
    ]);
    unmount(component);
  });

  it('never offers /admin as a destination, for any viewer', () => {
    for (const state of [UNRESOLVED, RESOLVED_GUEST, RESOLVED_REGISTERED]) {
      const { target, component } = render(state);
      expect(destinations(target).map((d) => d.href)).not.toContain('/admin');
      expect(target.textContent).not.toContain('Admin');
      unmount(component);
      target.remove();
    }
  });
});
