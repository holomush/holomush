// web/src/lib/components/admin/AdminNav.svelte.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, describe, expect, it } from 'vitest';
import { mount, unmount } from 'svelte';
import AdminNav from './AdminNav.svelte';
import type { AdminSectionEntry } from '$lib/stores/adminNavStore';

function render(props: { sections: AdminSectionEntry[]; activeId?: string }) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(AdminNav, { target, props });
  return { target, component };
}

const entry = (id: string, status = 'planned'): AdminSectionEntry => ({
  id,
  displayName: `Name ${id}`,
  status,
});

afterEach(() => document.body.replaceChildren());

describe('AdminNav', () => {
  it('renders one entry per array member for a seven-element response', () => {
    const sections = ['a', 'b', 'c', 'd', 'e', 'f', 'g'].map((id) => entry(id));
    const { target, component } = render({ sections });
    // Driven from the input length, not a fixed number: a hard-coded list
    // would satisfy a literal count and fail this.
    expect(target.querySelectorAll('a.navitem')).toHaveLength(sections.length);
    unmount(component);
  });

  it('renders one entry per array member for a one-element response', () => {
    const sections = [entry('solo', 'available')];
    const { target, component } = render({ sections });
    expect(target.querySelectorAll('a.navitem')).toHaveLength(sections.length);
    unmount(component);
  });

  it('renders zero entries for an empty response, drawing nothing of its own', () => {
    const { target, component } = render({ sections: [] });
    expect(target.querySelectorAll('a.navitem')).toHaveLength(0);
    unmount(component);
  });

  it('draws the planned badge from the status field and not from the id', () => {
    const { target, component } = render({
      sections: [entry('one', 'planned'), entry('two', 'available')],
    });
    const items = [...target.querySelectorAll('a.navitem')];
    expect(items[0].querySelector('.badge-planned')?.textContent?.trim()).toBe('planned');
    expect(items[1].querySelector('.badge-planned')).toBeNull();
    unmount(component);
  });

  it('links each entry to its own id and marks the active one', () => {
    const { target, component } = render({
      sections: [entry('one'), entry('two')],
      activeId: 'two',
    });
    const hrefs = [...target.querySelectorAll('a.navitem')].map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual(['/admin/one', '/admin/two']);
    const active = target.querySelector('a.navitem.is-active');
    expect(active?.getAttribute('href')).toBe('/admin/two');
    expect(active?.getAttribute('aria-current')).toBe('page');
    unmount(component);
  });

  // Asserted as an exhaustive equality rather than a negative match on a
  // refusal-code substring: naming that token here would put it in the very
  // tree the scoped absence-grep scans, and equality is the stronger property
  // anyway — ANY extra token the component invented would fail it, not just
  // the one shape a negative regex happened to anticipate.
  it('renders the monogram, the server-supplied name and the badge — and nothing else', () => {
    const { target, component } = render({
      sections: [{ id: 'x', displayName: 'Something Long', status: 'planned' }],
    });
    const item = target.querySelector('a.navitem') as HTMLElement;
    expect(item.textContent?.replace(/\s+/g, ' ').trim()).toBe('S Something Long planned');
    unmount(component);
  });

  it('renders an available entry as the monogram and the name alone', () => {
    const { target, component } = render({
      sections: [{ id: 'x', displayName: 'Something Long', status: 'available' }],
    });
    const item = target.querySelector('a.navitem') as HTMLElement;
    expect(item.textContent?.replace(/\s+/g, ' ').trim()).toBe('S Something Long');
    unmount(component);
  });
});
