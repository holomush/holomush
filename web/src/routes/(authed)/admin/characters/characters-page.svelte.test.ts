// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Named characters-page.svelte.test.ts, NOT +page.svelte.test.ts: SvelteKit
// reserves the entire `+` prefix under src/routes/ and refuses to build when it
// meets a +-prefixed file whose stem is not a route file (#4979). The
// .svelte.test.ts SUFFIX is load-bearing and kept — it is what routes the file
// to the client Vitest project, whose resolve.conditions: ['browser'] makes
// `mount` work (web/vite.config.ts:24-32).

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { mount, unmount, flushSync } from 'svelte';
import type { CharacterPage, CharacterRow } from '$lib/admin/client';

/**
 * The two RPC wrappers are replaced with PLAIN MUTABLE HOLDERS rather than
 * vi.fn stubs. vitest 4.1.10's mock-result tracking fails a test on an error
 * the code under test correctly caught — the symptom is indistinguishable from
 * "the error escaped the try/catch" and the stack points at the error's
 * construction site (#4980). These loads catch by design.
 */
const impl = vi.hoisted(() => ({
  list: null as unknown as (q: unknown) => Promise<CharacterPage>,
  search: null as unknown as (q: unknown, term: string) => Promise<CharacterPage>,
  calls: [] as { kind: 'list' | 'search'; term?: string }[],
}));

vi.mock('$lib/admin/client', async (importActual) => {
  const actual = await importActual<typeof import('$lib/admin/client')>();
  return {
    ...actual,
    listAdminCharacters: (q: unknown) => {
      impl.calls.push({ kind: 'list' });
      return impl.list(q);
    },
    searchAdminCharacters: (q: unknown, term: string) => {
      impl.calls.push({ kind: 'search', term });
      return impl.search(q, term);
    },
  };
});

const CharactersPage = (await import('./+page.svelte')).default;

const CREATED = 1_700_000_000_000_000_000n;

const row = (i: number): CharacterRow => ({
  id: `c${i}`,
  playerId: `p${i}`,
  playerUsername: `player${i}`,
  name: `Name ${i}`,
  status: 'active',
  lastActiveAt: 0n,
  createdAt: CREATED,
  version: 1,
});

const rows = (n: number) => Array.from({ length: n }, (_, i) => row(i));

function render(data: { rows: CharacterRow[]; totalCount: bigint; loadFailed: boolean }) {
  const target = document.createElement('div');
  document.body.appendChild(target);
  const component = mount(CharactersPage, { target, props: { data } });
  return { target, component };
}

const text = (target: HTMLElement) => target.textContent?.replace(/\s+/g, ' ') ?? '';

const buttonsIn = (el: Element | null) =>
  el ? [...el.querySelectorAll('button, a')].map((b) => b.textContent?.trim()) : [];

const typeSearch = async (target: HTMLElement, value: string) => {
  const input = target.querySelector('input[name="q"]') as HTMLInputElement;
  input.value = value;
  input.dispatchEvent(new Event('input', { bubbles: true }));
  await vi.advanceTimersByTimeAsync(250);
  flushSync();
  await Promise.resolve();
  flushSync();
};

beforeEach(() => {
  vi.useFakeTimers();
  impl.calls = [];
  impl.list = async () => ({ rows: [], totalCount: 0n });
  impl.search = async () => ({ rows: [], totalCount: 0n });
});

afterEach(() => {
  vi.useRealTimers();
  document.body.replaceChildren();
});

describe('/admin/characters — the four list states', () => {
  it('renders the zero-characters state with no action at all', () => {
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: false });
    expect(text(target)).toContain('No characters yet.');
    expect(text(target)).toContain('Characters appear here once players create them.');
    const empty = target.querySelector('[data-empty-state="zero"]');
    expect(empty).not.toBeNull();
    // An admin cannot create a character for a player, so a CTA here would be
    // an invented promise.
    expect(buttonsIn(empty)).toEqual([]);
    unmount(component);
  });

  it('renders the no-results state with a Clear filters action', async () => {
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: false });
    await typeSearch(target, 'zzz');

    expect(text(target)).toContain('No characters match those filters.');
    expect(text(target)).toContain('Try a different search or clear the status filter.');
    const empty = target.querySelector('[data-empty-state="no-results"]');
    expect(empty).not.toBeNull();
    expect(buttonsIn(empty)).toEqual(['Clear filters']);
    unmount(component);
  });

  it('discriminates the two empty states on whether a filter is applied, not the count', async () => {
    // Both have zero rows. Collapsing them would fail one of these.
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: false });
    expect(text(target)).toContain('No characters yet.');
    expect(text(target)).not.toContain('No characters match those filters.');

    await typeSearch(target, 'zzz');
    expect(text(target)).toContain('No characters match those filters.');
    expect(text(target)).not.toContain('No characters yet.');
    unmount(component);
  });

  it('makes no existence claim in the no-results copy', async () => {
    // #4972 is open: AdminSearchCharacters binds a charname-normalized term
    // against the verbatim players.username column, so a non-ASCII username
    // silently returns an empty 200. This state is currently reachable BY
    // DEFECT as well as by data, and the client cannot tell the two apart — so
    // it must not upgrade the render into a claim it has no grounds for.
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: false });
    await typeSearch(target, 'ﾐﾚﾝ');
    const rendered = text(target);
    expect(rendered).toContain('No characters match those filters.');
    expect(rendered).not.toMatch(/No such character|no such name|No results found|no matches exist/i);
    unmount(component);
  });

  it('renders the load-failure state with a Try again control', () => {
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: true });
    expect(text(target)).toContain("Couldn't load characters. Try again.");
    expect(buttonsIn(target.querySelector('[data-failure="load"]'))).toEqual(['Try again']);
    unmount(component);
  });

  it('renders the search-failure state with its own copy', async () => {
    const { target, component } = render({ rows: rows(2), totalCount: 2n, loadFailed: false });
    impl.search = async () => {
      throw new Error('boom');
    };
    await typeSearch(target, 'mir');

    const rendered = text(target);
    expect(rendered).toContain("Couldn't run that search. Try again.");
    expect(rendered).not.toContain("Couldn't load characters. Try again.");
    unmount(component);
  });

  it('renders no server-supplied string on any failure path', async () => {
    const { target, component } = render({ rows: rows(2), totalCount: 2n, loadFailed: false });
    impl.search = async () => {
      throw new Error('DENY_ADMIN_SECTION: pg_trgm index missing on characters.normalized_name');
    };
    await typeSearch(target, 'mir');

    const rendered = text(target);
    expect(rendered).not.toContain('DENY_');
    expect(rendered).not.toContain('pg_trgm');
    expect(rendered).not.toContain('boom');
    unmount(component);
  });

  it('clears the filters back to the unfiltered page', async () => {
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: false });
    await typeSearch(target, 'zzz');
    impl.list = async () => ({ rows: rows(3), totalCount: 3n });

    const clear = [...target.querySelectorAll('button')].find(
      (b) => b.textContent?.trim() === 'Clear filters',
    ) as HTMLButtonElement;
    clear.click();
    await vi.advanceTimersByTimeAsync(0);
    flushSync();
    await Promise.resolve();
    flushSync();

    expect(target.querySelectorAll('tbody tr')).toHaveLength(3);
    expect((target.querySelector('input[name="q"]') as HTMLInputElement).value).toBe('');
    unmount(component);
  });
});

describe('/admin/characters — pagination boundaries', () => {
  const pager = (target: HTMLElement) => target.querySelector('[aria-label="pagination"]');

  it('renders no pagination control at all for zero rows', () => {
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: false });
    expect(pager(target)).toBeNull();
    unmount(component);
  });

  it('renders no pagination control for exactly one full page', () => {
    // 50 is the last row of one page. A single-page pager is chrome with no
    // function.
    const { target, component } = render({ rows: rows(50), totalCount: 50n, loadFailed: false });
    expect(pager(target)).toBeNull();
    unmount(component);
  });

  it('renders the pagination control at one row past a full page', () => {
    const { target, component } = render({ rows: rows(50), totalCount: 51n, loadFailed: false });
    expect(pager(target)).not.toBeNull();
    expect(text(target)).toContain('1–50 of 51');
    unmount(component);
  });

  it('clamps the last page end to the total rather than to a full page width', async () => {
    const { target, component } = render({ rows: rows(50), totalCount: 51n, loadFailed: false });
    impl.list = async () => ({ rows: rows(1), totalCount: 51n });

    const next = target.querySelector('[aria-label="Go to next page"]') as HTMLButtonElement;
    next.click();
    await vi.advanceTimersByTimeAsync(0);
    flushSync();
    await Promise.resolve();
    flushSync();

    expect(text(target)).toContain('51–51 of 51');
    unmount(component);
  });

  it('renders the server total verbatim, never a figure derived from the page', () => {
    // The page holds 50 rows; the total is the server's own COUNT over the
    // filter and disagrees with the page width by design.
    const { target, component } = render({ rows: rows(50), totalCount: 412n, loadFailed: false });
    expect(text(target)).toContain('1–50 of 412');
    unmount(component);
  });
});

describe('/admin/characters — the search wire', () => {
  it('sends the raw typed string to the search RPC', async () => {
    const raw = '  Ｍｉrén  ';
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: false });
    await typeSearch(target, raw);
    const searches = impl.calls.filter((c) => c.kind === 'search');
    expect(searches).toHaveLength(1);
    expect(searches[0].term).toBe(raw);
    unmount(component);
  });

  it('returns to the list RPC when the box is emptied', async () => {
    const { target, component } = render({ rows: [], totalCount: 0n, loadFailed: false });
    await typeSearch(target, 'mir');
    impl.calls = [];
    await typeSearch(target, '');
    expect(impl.calls.map((c) => c.kind)).toEqual(['list']);
    unmount(component);
  });
});
