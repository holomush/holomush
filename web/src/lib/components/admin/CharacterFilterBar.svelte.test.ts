// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount } from 'svelte';
import CharacterFilterBar from './CharacterFilterBar.svelte';
import { ADMIN_STATUS_FILTERS, type CharacterStatusFilter } from '$lib/admin/client';

type Props = {
  term?: string;
  status?: CharacterStatusFilter;
  onsearch?: (term: string) => void;
  onstatus?: (status: CharacterStatusFilter) => void;
};

function render(props: Props = {}) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(CharacterFilterBar, { target, props });
  return { target, component };
}

function type(input: HTMLInputElement, value: string) {
  input.value = value;
  input.dispatchEvent(new Event('input', { bubbles: true }));
}

beforeEach(() => vi.useFakeTimers());
afterEach(() => {
  vi.useRealTimers();
  document.body.replaceChildren();
});

describe('CharacterFilterBar — the two controls', () => {
  it('names the search input q and carries the contract placeholder', () => {
    const { target, component } = render();
    const input = target.querySelector('input[name="q"]') as HTMLInputElement;
    expect(input).not.toBeNull();
    expect(input.getAttribute('placeholder')).toBe('Search characters and players');
    unmount(component);
  });

  it('carries no minimum-length gate attribute on the search input', () => {
    const { target, component } = render();
    const input = target.querySelector('input[name="q"]') as HTMLInputElement;
    expect(input.getAttribute('minlength')).toBeNull();
    expect(input.getAttribute('pattern')).toBeNull();
    unmount(component);
  });

  it('renders exactly one status control, named status and labelled Status', () => {
    const { target, component } = render();
    // Counted by the spelling shadcn-svelte 1.5.0 actually generated. The
    // bits-ui trigger is a <button aria-haspopup="listbox">, so it exposes no
    // role="combobox"; the hidden form participant is an <input name="status">,
    // not a native <select>.
    expect(target.querySelectorAll('[data-slot="select-trigger"]')).toHaveLength(1);
    expect(target.querySelectorAll('[aria-haspopup="listbox"]')).toHaveLength(1);
    expect(target.querySelectorAll('input[name="status"]')).toHaveLength(1);

    const trigger = target.querySelector('[data-slot="select-trigger"]') as HTMLElement;
    expect(trigger.getAttribute('aria-label')).toBe('Status');
    unmount(component);
  });

  it('offers exactly the four closed lifecycle filter options', () => {
    expect(ADMIN_STATUS_FILTERS.map((o) => o.label)).toEqual([
      'All',
      'Active',
      'Retired',
      'Idle',
    ]);
    expect(ADMIN_STATUS_FILTERS.map((o) => o.value)).toEqual([
      'all',
      'active',
      'retired',
      'idle',
    ]);
  });

  it('renders no sort control and no facet panel of its own', () => {
    // §11.3 names a control whose options are drawn from the field list as THE
    // warning sign, because the field list is the privacy-bearing set. Sorting
    // is click-header only, and there is exactly one control here that opens a
    // list — the status filter.
    const { target, component } = render();
    expect(target.querySelectorAll('[aria-haspopup]')).toHaveLength(1);
    expect(target.innerHTML).not.toMatch(/sort/i);
    unmount(component);
  });
});

describe('CharacterFilterBar — the debounce', () => {
  it('fires no search before 250ms of quiet', () => {
    const seen: string[] = [];
    const { target, component } = render({ onsearch: (t) => seen.push(t) });
    type(target.querySelector('input[name="q"]') as HTMLInputElement, 'mir');

    vi.advanceTimersByTime(249);
    expect(seen).toEqual([]);

    vi.advanceTimersByTime(1);
    expect(seen).toEqual(['mir']);
    unmount(component);
  });

  it('restarts the window on every keystroke and reports the final value once', () => {
    const seen: string[] = [];
    const { target, component } = render({ onsearch: (t) => seen.push(t) });
    const input = target.querySelector('input[name="q"]') as HTMLInputElement;

    type(input, 'm');
    vi.advanceTimersByTime(200);
    type(input, 'mi');
    vi.advanceTimersByTime(200);
    type(input, 'mir');
    vi.advanceTimersByTime(249);
    expect(seen).toEqual([]);

    vi.advanceTimersByTime(1);
    expect(seen).toEqual(['mir']);
    unmount(component);
  });

  it('sends the raw typed string byte for byte', () => {
    // Fullwidth characters, a combining sequence and surrounding whitespace.
    // The client makes NO claim about which strings are equal — not case, not
    // width, not composition. Normalization is the server's single pipeline,
    // and a TypeScript mirror of it would be a second, drifting definition of
    // name equality.
    const raw = '  Ｍｉrén  ';
    const seen: string[] = [];
    const { target, component } = render({ onsearch: (t) => seen.push(t) });
    type(target.querySelector('input[name="q"]') as HTMLInputElement, raw);
    vi.advanceTimersByTime(250);
    expect(seen).toEqual([raw]);
    expect(seen[0]).toBe(raw);
    unmount(component);
  });

  it('reports an emptied box as the empty string rather than swallowing it', () => {
    const seen: string[] = [];
    const { target, component } = render({ term: 'mir', onsearch: (t) => seen.push(t) });
    type(target.querySelector('input[name="q"]') as HTMLInputElement, '');
    vi.advanceTimersByTime(250);
    expect(seen).toEqual(['']);
    unmount(component);
  });
});
