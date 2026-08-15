// web/src/routes/(authed)/admin/[section]/section-page.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Client project — the .svelte.test.ts suffix is what routes it there
// (web/vite.config.ts), and the absent + prefix is what lets SvelteKit build
// with it in place (#4979).

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import SectionPage from './+page.svelte';
import type { AdminSectionEntry } from '$lib/stores/adminNavStore';

const COPY = 'Registered and gated. No handler yet.';

function render(entry: AdminSectionEntry) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(SectionPage, { target, props: { data: { entry } } });
  return { target, component };
}

const flat = (t: HTMLElement) => t.textContent?.replace(/\s+/g, ' ').trim() ?? '';

afterEach(() => document.body.replaceChildren());

describe('the planned-section state', () => {
  it('renders the display name and the contract line, byte for byte', () => {
    const { target, component } = render({
      id: 'two',
      displayName: 'Name Two',
      status: 'planned',
    });
    expect(flat(target)).toContain('Name Two');
    expect(flat(target)).toContain(COPY);
    unmount(component);
  });

  it('renders the server-supplied name and nothing invented beside it', () => {
    const { target, component } = render({
      id: 'x',
      displayName: 'Somewhere Else',
      status: 'planned',
    });
    expect(flat(target)).toBe(`Somewhere Else ${COPY}`);
    unmount(component);
  });

  it('offers no action at all — no button and no link', () => {
    const { target, component } = render({ id: 'x', displayName: 'X', status: 'planned' });
    // Asserted on RENDERED ROLES, not on source text: an aliased import would
    // slip past a grep of the component file. @testing-library/svelte is not a
    // dependency here, so the roles are read off the DOM directly — the same
    // property by the same definition (an <a> is only a link when it has href).
    expect(target.querySelectorAll('button, [role="button"]')).toHaveLength(0);
    expect(target.querySelectorAll('a[href], [role="link"]')).toHaveLength(0);
    unmount(component);
  });

  it('does not render the planned line for an available entry', () => {
    const { target, component } = render({
      id: 'one',
      displayName: 'Name One',
      status: 'available',
    });
    expect(flat(target)).not.toContain(COPY);
    unmount(component);
  });

  /**
   * The negative above is satisfied by rendering NOTHING, which is what the
   * template did. The load has already resolved the entry and answered 200, so
   * an `available` id with no concrete route drew the admin frame, the
   * breadcrumb and a completely empty content column — no copy, no error, no
   * not-found. Today `characters` is the only available row and its own
   * concrete route shadows this one, so the branch is unreachable; it becomes
   * reachable the moment a second section flips, and a blank screen with no
   * console error and green tests is the hardest kind of failure to attribute.
   */
  it('renders an authored line for an available entry rather than an empty column', () => {
    const { target, component } = render({
      id: 'one',
      displayName: 'Name One',
      status: 'available',
    });
    const rendered = flat(target);
    expect(rendered).toContain('Name One');
    expect(rendered).toContain('This section is not available here.');
    expect(rendered).not.toContain(COPY);
    expect(target.querySelector('[data-section-state="unhandled"]')).not.toBeNull();
    unmount(component);
  });

  it('offers no action in the unhandled state either', () => {
    // There is nowhere to send the operator: an available entry arriving here
    // is a routing bug, not a destination.
    const { target, component } = render({ id: 'one', displayName: 'X', status: 'available' });
    expect(target.querySelectorAll('button, [role="button"]')).toHaveLength(0);
    expect(target.querySelectorAll('a[href], [role="link"]')).toHaveLength(0);
    unmount(component);
  });

  it('wraps a long name rather than truncating it', () => {
    const long = 'Ackermann Bureau of Contingent Provisioning Oversight';
    expect(long.length).toBeGreaterThan(40);
    const { target, component } = render({ id: 'x', displayName: long, status: 'planned' });
    const heading = target.querySelector('[data-slot="empty-title"]') as HTMLElement;
    expect(heading.textContent?.trim()).toBe(long);
    // A truncated section name in this position is a worse failure than a
    // two-line one: the name is the only thing telling the operator where
    // they are.
    expect(heading.className).not.toMatch(/\btruncate\b/);
    expect(heading.className).not.toMatch(/text-ellipsis|overflow-hidden|line-clamp/);
    expect(heading.style.textOverflow).toBe('');
    unmount(component);
  });
});
