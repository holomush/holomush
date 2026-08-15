// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount } from 'svelte';
import CharacterTable from './CharacterTable.svelte';
import type { CharacterRow, CharacterSortField } from '$lib/admin/client';

type Props = {
  rows: CharacterRow[];
  sortField?: CharacterSortField;
  descending?: boolean;
  pendingRowId?: string;
  flashRowId?: string;
  loading?: boolean;
  onsort?: (field: CharacterSortField) => void;
  onedit?: (id: string) => void;
  onlifecycle?: (row: CharacterRow) => void;
  onfilterplayer?: (playerId: string) => void;
  now?: Date;
};

function render(props: Props) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(CharacterTable, { target, props });
  return { target, component };
}

/** 2023-11-14T22:13:20Z, in epoch nanoseconds. */
const CREATED = 1_700_000_000_000_000_000n;

const row = (over: Partial<CharacterRow> = {}): CharacterRow => ({
  id: 'c1',
  playerId: 'p1',
  playerUsername: 'ashwood',
  name: 'Miren',
  status: 'active',
  lastActiveAt: 0n,
  createdAt: CREATED,
  version: 3,
  ...over,
});

const rowNames = (target: HTMLElement) =>
  [...target.querySelectorAll('tbody tr')].map((tr) => tr.getAttribute('data-row-id'));

afterEach(() => document.body.replaceChildren());

describe('CharacterTable — headers and sort', () => {
  it('renders exactly five sortable headers and leaves Ver without one', () => {
    const { target, component } = render({ rows: [row()] });
    const heads = [...target.querySelectorAll('thead th')];
    expect(heads.map((h) => h.textContent?.replace(/\s+/g, ' ').trim().replace(/[▲▼]$/, '').trim()))
      .toEqual(['Name', 'Player', 'Status', 'Last active', 'Created', 'Ver']);

    const sortable = heads.filter((h) => h.querySelector('button'));
    expect(sortable).toHaveLength(5);

    // Ver is a concurrency guard, not an ordering, and is not a §11.3 field.
    const ver = heads[5];
    expect(ver.querySelector('button')).toBeNull();
    expect(ver.getAttribute('aria-sort')).toBeNull();
    unmount(component);
  });

  it('reflects the sort props into aria-sort on the sorted header alone', () => {
    const { target, component } = render({ rows: [row()], sortField: 'name', descending: false });
    const heads = [...target.querySelectorAll('thead th')];
    expect(heads.map((h) => h.getAttribute('aria-sort'))).toEqual([
      'ascending',
      'none',
      'none',
      'none',
      'none',
      null,
    ]);
    unmount(component);
  });

  it('reports descending on the sorted header when descending is set', () => {
    const { target, component } = render({
      rows: [row()],
      sortField: 'lastActive',
      descending: true,
    });
    const heads = [...target.querySelectorAll('thead th')];
    expect(heads.map((h) => h.getAttribute('aria-sort'))).toEqual([
      'none',
      'none',
      'none',
      'descending',
      'none',
      null,
    ]);
    unmount(component);
  });

  it('carries a caret glyph on the sorted header, so state is not colour alone', () => {
    const { target, component } = render({ rows: [row()], sortField: 'name', descending: false });
    const heads = [...target.querySelectorAll('thead th')];
    expect(heads[0].textContent).toMatch(/[▲▼]/);
    expect(heads[1].textContent).not.toMatch(/[▲▼]/);
    unmount(component);
  });

  it('reports the clicked field through onsort', () => {
    const seen: CharacterSortField[] = [];
    const { target, component } = render({ rows: [row()], onsort: (f) => seen.push(f) });
    const heads = [...target.querySelectorAll('thead th')];
    (heads[3].querySelector('button') as HTMLButtonElement).click();
    expect(seen).toEqual(['lastActive']);
    unmount(component);
  });
});

describe('CharacterTable — the server order is the rendered order', () => {
  it('renders a deliberately unsorted array index-for-index, ties included', () => {
    // Two rows share a sort key (both never-active) and arrive in an order the
    // client has no business "fixing". The three-clause server ordering —
    // (last_active_at = 0) first, then the key, then normalized_name ASC — has
    // already decided this; a local re-sort would silently break the property a
    // server test proves.
    const rows = [
      row({ id: 'c3', name: 'Zeno', lastActiveAt: 0n }),
      row({ id: 'c1', name: 'Miren', lastActiveAt: 0n }),
      row({ id: 'c2', name: 'Ash', lastActiveAt: CREATED }),
    ];
    const { target, component } = render({ rows });
    expect(rowNames(target)).toEqual(['c3', 'c1', 'c2']);
    unmount(component);
  });
});

describe('CharacterTable — row actions', () => {
  it('offers exactly two actions per row and no overflow trigger', () => {
    const { target, component } = render({ rows: [row()] });
    const tr = target.querySelector('tbody tr') as HTMLElement;
    const actions = [...tr.querySelectorAll('.rowactions button')];
    expect(actions.map((b) => b.textContent?.trim())).toEqual(['Edit', 'Retire…']);
    unmount(component);
  });

  it('flips the lifecycle action label from the row own status', () => {
    const { target, component } = render({
      rows: [row({ id: 'a', status: 'active' }), row({ id: 'r', status: 'retired' })],
    });
    const labels = [...target.querySelectorAll('tbody tr')].map((tr) =>
      [...tr.querySelectorAll('.rowactions button')].map((b) => b.textContent?.trim()),
    );
    expect(labels).toEqual([
      ['Edit', 'Retire…'],
      ['Edit', 'Un-retire'],
    ]);
    unmount(component);
  });

  it('reports the row through onlifecycle without deciding the transition itself', () => {
    const onlifecycle = vi.fn();
    const { target, component } = render({ rows: [row({ id: 'a', status: 'retired' })], onlifecycle });
    const tr = target.querySelector('tbody tr') as HTMLElement;
    ([...tr.querySelectorAll('.rowactions button')][1] as HTMLButtonElement).click();
    expect(onlifecycle).toHaveBeenCalledWith(expect.objectContaining({ id: 'a', status: 'retired' }));
    unmount(component);
  });
});

describe('CharacterTable — the phone-band row target', () => {
  /**
   * The tap target below 768px is a REAL <button> in the Name cell whose
   * ::after overlay spans the row — not a <tr> with a handler. A <tr> cannot
   * become a <button> and a <button> cannot legally contain <td>s, so the naive
   * reading produces invalid markup that svelte-check and every desktop test
   * would pass over in silence.
   *
   * This asserts the COMPONENT'S OWN CONTRACT — onedit fired with the row id —
   * not "the Sheet opened". The Sheet is plan 06.1-04's artifact and does not
   * exist yet; a mock of it would prove nothing.
   */
  it('puts a real button in the Name cell and fires onedit with the row id', () => {
    const onedit = vi.fn();
    const { target, component } = render({ rows: [row({ id: 'c9' })], onedit });
    const nameCell = target.querySelector('tbody tr td') as HTMLElement;
    const btn = nameCell.querySelector('button.rowbtn') as HTMLButtonElement;

    // A real <button type="button"> is precisely what grants Enter/Space
    // activation. jsdom implements no keyboard activation default for buttons,
    // so the element type IS the assertion; .click() is the event a real Enter
    // press produces.
    expect(btn.tagName).toBe('BUTTON');
    expect(btn.getAttribute('type')).toBe('button');

    btn.focus();
    expect(document.activeElement).toBe(btn);
    btn.click();
    expect(onedit).toHaveBeenCalledWith('c9');
    unmount(component);
  });

  it('leaves the row itself handler-free and role-free', () => {
    const { target, component } = render({ rows: [row()] });
    const tr = target.querySelector('tbody tr') as HTMLTableRowElement;
    expect(tr.getAttribute('role')).toBeNull();
    expect(tr.onclick).toBeNull();
    expect(tr.getAttribute('onclick')).toBeNull();
    expect(tr.getAttribute('tabindex')).toBeNull();
    unmount(component);
  });

  it('carries the full row values in the button accessible name', () => {
    const { target, component } = render({
      rows: [row({ name: 'Miren', playerUsername: 'ashwood', status: 'active', lastActiveAt: 0n })],
    });
    const btn = target.querySelector('button.rowbtn') as HTMLElement;
    const label = btn.textContent?.replace(/\s+/g, ' ').trim();
    expect(label).toContain('Miren');
    expect(label).toContain('ashwood');
    expect(label).toContain('active');
    expect(label).toContain('never');
    unmount(component);
  });

  /**
   * THE OVERLAY'S CONTAINING BLOCK IS THE ROW, asserted on the STYLESHEET.
   *
   * A computed-style assertion cannot exist here: the rule lives inside
   * `@media (width < theme(--breakpoint-md))`, both Vitest projects run jsdom
   * (web/vite.config.ts:9) which has no sub-1024px viewport and no layout
   * engine, so getComputedStyle(tr).position reads `static` whether the CSS is
   * right or wrong — it would fail a correct implementation. The repo already
   * stubs matchMedia to `matches: false` for the same reason
   * (SectionRail.svelte.test.ts:21-30). The browser-level span proof is plan
   * 06.1-04's 375px tap on a NON-Name cell, the only environment that can
   * prove it.
   */
  it('declares position: relative on the row, and on no cell, inside the phone media block', () => {
    // Read from the repo path rather than import.meta.url: under Vite the
    // module URL is an http one, not a file one.
    const src = readFileSync(
      resolve(process.cwd(), 'src/lib/components/admin/CharacterTable.svelte'),
      'utf8',
    );
    const start = src.indexOf('@media (width < theme(--breakpoint-md))');
    expect(start).toBeGreaterThan(-1);

    // Take the media block by brace balance from its opening brace.
    let depth = 0;
    let i = src.indexOf('{', start);
    const from = i;
    do {
      if (src[i] === '{') depth++;
      else if (src[i] === '}') depth--;
      i++;
    } while (depth > 0 && i < src.length);
    // Comments are stripped before matching: a rule's explanatory comment is
    // captured by the selector group otherwise, and a comment explaining the
    // prohibition would trip the assertion enforcing it.
    const block = src.slice(from, i).replace(/\/\*[\s\S]*?\*\//g, '');

    const relativeRules = [...block.matchAll(/([^{}]+)\{[^{}]*position:\s*relative/g)].map((m) =>
      m[1].replace(/\s+/g, ' ').trim(),
    );
    expect(relativeRules.length).toBeGreaterThan(0);
    // Every containing block declared in this media query is a ROW selector.
    for (const selector of relativeRules) {
      expect(selector).toMatch(/charrow/);
      expect(selector).not.toMatch(/\btd\b|cell/);
    }
    // And the overlay that depends on it exists.
    expect(block).toMatch(/\.rowbtn::after/);
    expect(block).toMatch(/inset:\s*0/);
  });
});

describe('CharacterTable — pending, loading and vocabulary', () => {
  it('puts only the mutating row in a pending state', () => {
    const { target, component } = render({
      rows: [row({ id: 'a' }), row({ id: 'b' })],
      pendingRowId: 'b',
    });
    const trs = [...target.querySelectorAll('tbody tr')];
    expect(trs.map((tr) => tr.getAttribute('aria-busy'))).toEqual([null, 'true']);
    const disabled = trs.map((tr) =>
      [...tr.querySelectorAll('.rowactions button')].map((b) => (b as HTMLButtonElement).disabled),
    );
    expect(disabled).toEqual([
      [false, false],
      [true, true],
    ]);
    unmount(component);
  });

  it('renders skeleton rows at the full column count inside a labelled status region', () => {
    const { target, component } = render({ rows: [], loading: true });
    const region = target.querySelector('[role="status"]') as HTMLElement;
    expect(region).not.toBeNull();
    expect(region.getAttribute('aria-label')).toBe('Loading characters…');
    const skeletonRows = [...target.querySelectorAll('tbody tr')];
    expect(skeletonRows.length).toBeGreaterThan(0);
    for (const tr of skeletonRows) {
      expect(tr.querySelectorAll('td')).toHaveLength(6);
      expect(tr.querySelectorAll('[data-slot="skeleton"]').length).toBe(6);
    }
    // No spinner, ever.
    expect(target.querySelector('[role="progressbar"]')).toBeNull();
    unmount(component);
  });

  it('drops no session vocabulary into the rendered output', () => {
    const { target, component } = render({
      rows: [row({ status: 'active' }), row({ id: 'x', status: 'retired', lastActiveAt: CREATED })],
    });
    expect(target.innerHTML).not.toMatch(/online|presence|Last seen|Last login/i);
    unmount(component);
  });

  it('renders Last active as coarse text with no absolute timestamp behind it', () => {
    const now = new Date('2026-08-14T12:00:00.000Z');
    const twoHours = BigInt(now.getTime() - 2 * 60 * 60 * 1000) * 1_000_000n;
    const { target, component } = render({
      rows: [row({ lastActiveAt: twoHours }), row({ id: 'n', lastActiveAt: 0n })],
      now,
    });
    const cells = [...target.querySelectorAll('td.cell-lastactive')];
    expect(cells).toHaveLength(2);
    expect(cells[0].textContent?.trim()).toBe('2h ago');
    expect(cells[1].textContent?.trim()).toBe('never');
    for (const cell of cells) {
      expect(cell.getAttribute('title')).toBeNull();
      expect(cell.querySelector('[title]')).toBeNull();
    }
    unmount(component);
  });
});
