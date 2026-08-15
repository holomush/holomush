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
import { ConnectError, Code } from '@connectrpc/connect';
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
  detail: null as unknown as (id: string) => Promise<unknown>,
  update: null as unknown as (args: unknown) => Promise<unknown>,
  retire: null as unknown as (id: string, v: number) => Promise<unknown>,
  unretire: null as unknown as (id: string, v: number) => Promise<unknown>,
  calls: [] as {
    kind: 'list' | 'search' | 'detail' | 'update' | 'retire' | 'unretire';
    term?: string;
    args?: unknown;
    expectedVersion?: number;
  }[],
  /** Every toast fired, in order, with the action label when one is offered. */
  toasts: [] as { message: string; action?: string; duration?: number }[],
  /** The most recent toast's Undo handler, so a test can trigger it. */
  undo: null as null | (() => void),
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
    getAdminCharacter: (id: string) => {
      impl.calls.push({ kind: 'detail' });
      return impl.detail(id);
    },
    updateAdminCharacter: (args: unknown) => {
      impl.calls.push({ kind: 'update', args });
      return impl.update(args);
    },
    retireAdminCharacter: (id: string, v: number) => {
      impl.calls.push({ kind: 'retire', expectedVersion: v });
      return impl.retire(id, v);
    },
    unretireAdminCharacter: (id: string, v: number) => {
      impl.calls.push({ kind: 'unretire', expectedVersion: v });
      return impl.unretire(id, v);
    },
  };
});

/**
 * The toast is replaced by a recorder. It is a RECEIPT and never the sole
 * carrier of an outcome — the row already updated in place — so these cases
 * assert that one fired and what it named, not that anything depended on it.
 */
vi.mock('svelte-sonner', () => ({
  toast: (message: string, opts?: { duration?: number; action?: { label: string; onClick: () => void } }) => {
    impl.toasts.push({ message, action: opts?.action?.label, duration: opts?.duration });
    impl.undo = opts?.action?.onClick ?? null;
    return 1;
  },
}));

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

/** What AdminGetCharacter returns behind the Sheet: prose the row never has. */
const DETAIL = {
  character: { version: 1 },
  description: 'A tall figure in a long grey coat.',
  profile: { 'profile.concept': 'Wandering archivist' },
};

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
  impl.toasts = [];
  impl.undo = null;
  impl.list = async () => ({ rows: [], totalCount: 0n });
  impl.search = async () => ({ rows: [], totalCount: 0n });
  impl.detail = async () => DETAIL;
  impl.update = async () => ({ ...row(0), version: 8 });
  impl.retire = async () => ({ ...row(0), status: 'retired', version: 8 });
  impl.unretire = async () => ({ ...row(0), status: 'active', version: 9 });
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

describe('/admin/characters — request sequencing', () => {
  /**
   * Every interactive re-read calls the same `reload`, and two are easy to
   * have in flight at once: the 250ms debounce bounds a keystroke burst but
   * not the NETWORK, and a sort click or a status change fires with no
   * debounce at all. These hold the deferred settlers so the older read can be
   * made to answer second — the ordering the page has no control over.
   */
  function deferredSearch() {
    const pending: {
      resolve: (page: CharacterPage) => void;
      reject: (reason: unknown) => void;
    }[] = [];
    impl.search = () =>
      new Promise<CharacterPage>((resolve, reject) => {
        pending.push({ resolve, reject });
      });
    return pending;
  }

  it('discards a stale page that resolves after a newer read already answered', async () => {
    const pending = deferredSearch();
    const { target, component } = render({ rows: rows(2), totalCount: 2n, loadFailed: false });

    await typeSearch(target, 'mir'); // read A
    await typeSearch(target, 'miren'); // read B
    expect(pending).toHaveLength(2);

    // B — the read the search box, the term and the pager all describe.
    pending[1].resolve({ rows: [row(9)], totalCount: 1n });
    await settle();
    expect(target.querySelectorAll('tbody tr')).toHaveLength(1);

    // A answers late. Its page describes a term the operator has moved past,
    // so committing it would render one request's rows under another's state.
    pending[0].resolve({ rows: rows(3), totalCount: 3n });
    await settle();
    expect(target.querySelectorAll('tbody tr')).toHaveLength(1);
    expect(text(target)).toContain('Name 9');
    unmount(component);
  });

  it('discards a stale failure rather than flipping a good page into the failure screen', async () => {
    const pending = deferredSearch();
    const { target, component } = render({ rows: rows(2), totalCount: 2n, loadFailed: false });

    await typeSearch(target, 'mir'); // read A
    await typeSearch(target, 'miren'); // read B
    expect(pending).toHaveLength(2);

    pending[1].resolve({ rows: rows(2), totalCount: 2n });
    await settle();
    expect(target.querySelectorAll('tbody tr')).toHaveLength(2);

    pending[0].reject(new Error('boom'));
    await settle();
    // The newer read succeeded; an older refusal is not news about it.
    expect(text(target)).not.toContain("Couldn't run that search. Try again.");
    expect(text(target)).not.toContain("Couldn't load characters. Try again.");
    expect(target.querySelectorAll('tbody tr')).toHaveLength(2);
    unmount(component);
  });

  it('keeps the surface in its loading state until the newest read answers', async () => {
    // `loading` is written by the same unconditional path: the FIRST response
    // to arrive would otherwise clear it while a newer read is outstanding.
    const pending = deferredSearch();
    const { target, component } = render({ rows: rows(2), totalCount: 2n, loadFailed: false });

    await typeSearch(target, 'mir'); // read A
    await typeSearch(target, 'miren'); // read B

    pending[0].resolve({ rows: rows(3), totalCount: 3n }); // A answers first
    await settle();
    expect(target.querySelector('[role="status"]')).not.toBeNull();

    pending[1].resolve({ rows: [row(9)], totalCount: 1n });
    await settle();
    expect(target.querySelector('[role="status"]')).toBeNull();
    expect(target.querySelectorAll('tbody tr')).toHaveLength(1);
    unmount(component);
  });
});

/**
 * D-110's binding sequence, driven through the real Sheet and the real confirm.
 *
 * The success and the conflict paths differ in exactly three places — whether
 * the Sheet closes, whether the typed text survives, and whether a receipt
 * fires — and each of those is asserted on both sides here rather than on the
 * side where it happens to be convenient.
 */
const sheet = () => document.body.querySelector('[data-slot="sheet-content"]') as HTMLElement | null;
const confirm = () =>
  document.body.querySelector('[data-slot="alert-dialog-content"]') as HTMLElement | null;

async function settle() {
  for (let i = 0; i < 6; i++) {
    await Promise.resolve();
    flushSync();
  }
}

function clickRowAction(target: HTMLElement, rowId: string, label: string) {
  const tr = target.querySelector(`[data-row-id="${rowId}"]`) as HTMLElement;
  const btn = [...tr.querySelectorAll('button')].find((b) => b.textContent?.trim() === label);
  if (!btn) throw new Error(`no row action ${label}`);
  (btn as HTMLButtonElement).click();
  flushSync();
}

function sheetField(name: string): HTMLInputElement | HTMLTextAreaElement {
  const el = sheet()!.querySelector(`[name="${name}"]`);
  if (!el) throw new Error(`no sheet control named ${name}`);
  return el as HTMLInputElement | HTMLTextAreaElement;
}

function typeInSheet(name: string, value: string) {
  const el = sheetField(name);
  el.value = value;
  el.dispatchEvent(new Event('input', { bubbles: true }));
  flushSync();
}

describe('/admin/characters — D-110: the success path', () => {
  it('closes the Sheet, updates the row from the response, and fires one receipt', async () => {
    impl.list = async () => ({ rows: rows(2), totalCount: 2n });
    impl.update = async () => ({ ...row(0), name: 'Name 0', version: 8 });
    const { target, component } = render({ rows: rows(2), totalCount: 2n, loadFailed: false });

    clickRowAction(target, 'c0', 'Edit');
    await settle();
    expect(sheet()).not.toBeNull();

    typeInSheet('concept', 'Archivist, retired');
    const save = sheet()!.querySelector('[data-testid="sheet-save"]') as HTMLButtonElement;
    expect(save.disabled).toBe(false);
    expect(save.textContent?.trim()).toBe('Save changes');

    const before = impl.calls.filter((c) => c.kind === 'list' || c.kind === 'search').length;
    (sheet()!.querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();

    // 3 — the Sheet closed.
    expect(sheet()).toBeNull();
    // 4 — the row's rendered version came from the RESPONSE.
    const tr = target.querySelector('[data-row-id="c0"]') as HTMLElement;
    expect(tr.textContent).toContain('8');
    // …with no list request between the mutation and the row update.
    const after = impl.calls.filter((c) => c.kind === 'list' || c.kind === 'search').length;
    expect(after - before).toBe(0);
    // 5 — one receipt, naming the RPC and the version transition.
    expect(impl.toasts).toHaveLength(1);
    expect(impl.toasts[0].message).toContain('AdminUpdateCharacter');
    expect(impl.toasts[0].message).toContain('update_mask: 1 paths');
    expect(impl.toasts[0].message).toContain('v1 → v8');
    expect(impl.toasts[0].duration).toBe(6000);

    unmount(component);
  });

  it('marks the submit busy without swapping its label, before the Sheet closes', async () => {
    let release!: (v: unknown) => void;
    impl.update = () =>
      new Promise((res) => {
        release = res;
      });
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Edit');
    await settle();
    typeInSheet('concept', 'x');
    (sheet()!.querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();

    // 1 — label kept, disabled, aria-busy. 2 — the row is the pending one.
    const save = sheet()!.querySelector('[data-testid="sheet-save"]') as HTMLButtonElement;
    expect(save.textContent?.trim()).toBe('Save changes');
    expect(save.getAttribute('aria-busy')).toBe('true');
    expect(save.disabled).toBe(true);
    expect(
      (target.querySelector('[data-row-id="c0"]') as HTMLElement).getAttribute('aria-busy'),
    ).toBe('true');
    expect(impl.toasts).toHaveLength(0);

    release({ ...row(0), version: 8 });
    await settle();
    expect(sheet()).toBeNull();
    unmount(component);
  });
});

describe('/admin/characters — D-110: the Aborted path', () => {
  it('keeps the Sheet open with its typed text, names both versions, and fires no receipt', async () => {
    let detailCalls = 0;
    impl.detail = async () => {
      detailCalls += 1;
      return detailCalls === 1 ? DETAIL : { ...DETAIL, character: { version: 9 } };
    };
    impl.update = async () => {
      throw new ConnectError('stale', Code.Aborted);
    };
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Edit');
    await settle();
    typeInSheet('concept', 'Still mine');
    (sheet()!.querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();

    // 3 — the Sheet stayed open. 4 — the typed text survived.
    expect(sheet()).not.toBeNull();
    expect(sheetField('concept').value).toBe('Still mine');
    // 5 — the conflict alert, with both numbers, and NO receipt.
    const alert = sheet()!.querySelector('[role="alert"]') as HTMLElement;
    expect(alert.textContent).toContain('version 1');
    expect(alert.textContent).toContain('9');
    expect(impl.toasts).toEqual([]);
    // The row is no longer pending.
    expect(
      (target.querySelector('[data-row-id="c0"]') as HTMLElement).getAttribute('aria-busy'),
    ).toBeNull();
    unmount(component);
  });
});

describe('/admin/characters — a response the page cannot confirm', () => {
  /**
   * All three mutation wrappers are typed `Promise<CharacterRow | undefined>`,
   * and `applyRow` already guards for exactly that. The receipts did not: a
   * body-less success printed `AdminRetireCharacter · undefined · v1 →
   * vundefined` beside a row that had not changed — a receipt asserting a
   * mutation the page could not confirm.
   */
  it('writes no receipt for a body-less edit response', async () => {
    impl.update = async () => undefined;
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Edit');
    await settle();
    typeInSheet('concept', 'Archivist');
    (sheet()!.querySelector('form') as HTMLFormElement).requestSubmit();
    await settle();

    for (const t of impl.toasts) expect(t.message).not.toContain('undefined');
    expect(impl.toasts).toEqual([]);
    // The row is untouched, so nothing on screen claims otherwise either.
    const tr = target.querySelector('[data-row-id="c0"]') as HTMLElement;
    expect(tr.textContent).toContain('1');
    expect(tr.getAttribute('aria-busy')).toBeNull();
    unmount(component);
  });

  it('writes no receipt for a body-less lifecycle response', async () => {
    impl.retire = async () => undefined;
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Retire…');
    await settle();
    (confirm()!.querySelector('[data-testid="lifecycle-confirm"]') as HTMLButtonElement).click();
    await settle();

    for (const t of impl.toasts) expect(t.message).not.toContain('undefined');
    expect(impl.toasts).toEqual([]);
    const tr = target.querySelector('[data-row-id="c0"]') as HTMLElement;
    expect(tr.textContent).toContain('active');
    unmount(component);
  });
});

describe('/admin/characters — the lifecycle transitions', () => {
  it('routes the row action to the confirm and sends no RPC until it is confirmed', async () => {
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Retire…');
    await settle();
    expect(confirm()).not.toBeNull();
    expect(impl.calls.filter((c) => c.kind === 'retire')).toEqual([]);
    unmount(component);
  });

  it('routes the Sheet picker to the same confirm', async () => {
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Edit');
    await settle();
    const picker = sheet()!.querySelector('select[name="lifecycle"]') as HTMLSelectElement;
    picker.value = 'retired';
    picker.dispatchEvent(new Event('change', { bubbles: true }));
    flushSync();
    await settle();
    expect(confirm()).not.toBeNull();
    expect(impl.calls.filter((c) => c.kind === 'retire')).toEqual([]);
    unmount(component);
  });

  it('sends AdminRetireCharacter on confirm and offers an Undo receipt', async () => {
    impl.retire = async () => ({ ...row(0), status: 'retired', version: 8 });
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Retire…');
    await settle();
    (confirm()!.querySelector('[data-testid="lifecycle-confirm"]') as HTMLButtonElement).click();
    await settle();

    expect(impl.calls.filter((c) => c.kind === 'retire')).toEqual([
      { kind: 'retire', expectedVersion: 1 },
    ]);
    expect(confirm()).toBeNull();
    const tr = target.querySelector('[data-row-id="c0"]') as HTMLElement;
    expect(tr.textContent).toContain('retired');
    expect(impl.toasts).toHaveLength(1);
    expect(impl.toasts[0].message).toContain('AdminRetireCharacter');
    expect(impl.toasts[0].message).toContain('v1 → v8');
    expect(impl.toasts[0].action).toBe('Undo');
    unmount(component);
  });

  it('sends AdminUnretireCharacter from Undo, at the NEW version, through the same path', async () => {
    impl.retire = async () => ({ ...row(0), status: 'retired', version: 8 });
    impl.unretire = async () => ({ ...row(0), status: 'active', version: 9 });
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Retire…');
    await settle();
    (confirm()!.querySelector('[data-testid="lifecycle-confirm"]') as HTMLButtonElement).click();
    await settle();

    expect(impl.undo).not.toBeNull();
    impl.undo!();
    await settle();

    // The un-retire RPC, never a status value, at the version the retire
    // response returned.
    expect(impl.calls.filter((c) => c.kind === 'unretire')).toEqual([
      { kind: 'unretire', expectedVersion: 8 },
    ]);
    const tr = target.querySelector('[data-row-id="c0"]') as HTMLElement;
    expect(tr.textContent).toContain('active');
    unmount(component);
  });

  it('renders an authored failure receipt when the Undo itself is refused', async () => {
    // The toast action is the OTHER caller of applyLifecycle, and unlike the
    // confirm dialog it has no wrapper that catches. A refused un-retire — an
    // Aborted is the likeliest, since `Undo` is composed against a version a
    // second operator may already have moved — dismisses the toast on click and
    // leaves the row reading `retired`, so without a surface of its own the
    // operator is told an operation succeeded that did not.
    impl.retire = async () => ({ ...row(0), status: 'retired', version: 8 });
    impl.unretire = async () => {
      throw new ConnectError('stale', Code.Aborted);
    };
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Retire…');
    await settle();
    (confirm()!.querySelector('[data-testid="lifecycle-confirm"]') as HTMLButtonElement).click();
    await settle();

    expect(impl.undo).not.toBeNull();
    impl.undo!();
    await settle();

    // The row did NOT move: the un-retire was refused.
    const tr = target.querySelector('[data-row-id="c0"]') as HTMLElement;
    expect(tr.textContent).toContain('retired');
    expect(tr.getAttribute('aria-busy')).toBeNull();

    // …and the refusal reached the operator, in this page's own words.
    expect(impl.toasts).toHaveLength(2);
    expect(impl.toasts[1].message).toBe("Couldn't undo that. The character is still retired.");
    // Authored, not relayed: no ConnectError text and no `[code]` prefix.
    expect(impl.toasts[1].message).not.toContain('stale');
    expect(impl.toasts[1].message).not.toMatch(/\[[a-z_]+\]/);
    unmount(component);
  });

  it('offers no Undo on the un-retire receipt', async () => {
    impl.unretire = async () => ({ ...row(0), status: 'active', version: 8 });
    const { target, component } = render({
      rows: [{ ...row(0), status: 'retired' }],
      totalCount: 1n,
      loadFailed: false,
    });
    clickRowAction(target, 'c0', 'Un-retire');
    await settle();
    (confirm()!.querySelector('[data-testid="lifecycle-confirm"]') as HTMLButtonElement).click();
    await settle();
    expect(impl.toasts).toHaveLength(1);
    expect(impl.toasts[0].message).toContain('AdminUnretireCharacter');
    expect(impl.toasts[0].action).toBeUndefined();
    unmount(component);
  });

  it('keeps the confirm open on a lifecycle failure and fires no receipt', async () => {
    impl.retire = async () => {
      throw new ConnectError('boom', Code.Internal);
    };
    const { target, component } = render({ rows: rows(1), totalCount: 1n, loadFailed: false });
    clickRowAction(target, 'c0', 'Retire…');
    await settle();
    (confirm()!.querySelector('[data-testid="lifecycle-confirm"]') as HTMLButtonElement).click();
    await settle();
    expect(confirm()).not.toBeNull();
    expect(confirm()!.textContent).toContain("Couldn't change this character's lifecycle. Try again.");
    expect(impl.toasts).toEqual([]);
    unmount(component);
  });
});
