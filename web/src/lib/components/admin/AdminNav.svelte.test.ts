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

  it('renders the server-supplied display name and no refusal code', () => {
    const { target, component } = render({
      sections: [{ id: 'x', displayName: 'Something Long', status: 'planned' }],
    });
    expect(target.textContent).toContain('Something Long');
    expect(target.innerHTML).not.toMatch(/DENY_/);
    unmount(component);
  });
});
