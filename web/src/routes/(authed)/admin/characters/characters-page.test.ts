// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Named characters-page.test.ts, NOT +page.test.ts (#4979). No .svelte suffix:
// this exercises a load function, mounts nothing, and belongs in the server
// project.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ConnectError, Code } from '@connectrpc/connect';
import type { AdminSectionEntry } from '$lib/stores/adminNavStore';
import type { CharacterPage } from '$lib/admin/client';

/**
 * A plain mutable holder, not a vi.fn: vitest 4.1.10's mock-result tracking
 * fails the test on an error this load correctly catches (#4980).
 */
const impl = vi.hoisted(() => ({
  list: null as unknown as () => Promise<CharacterPage>,
}));

vi.mock('$lib/admin/client', async (importActual) => {
  const actual = await importActual<typeof import('$lib/admin/client')>();
  return { ...actual, listAdminCharacters: () => impl.list() };
});

const { load } = await import('./+page');

const section = (id: string, status = 'available'): AdminSectionEntry => ({
  id,
  displayName: id,
  status,
});

const parentWith = (sections: AdminSectionEntry[]) => async () => ({
  sections,
  loadFailed: false,
});

beforeEach(() => {
  impl.list = async () => ({ rows: [], totalCount: 0n });
});

afterEach(() => vi.restoreAllMocks());

describe('/admin/characters load', () => {
  it('returns the first page when the section is in this caller list', async () => {
    impl.list = async () => ({ rows: [], totalCount: 7n });
    const data = await load({ parent: parentWith([section('characters')]) });
    expect(data).toEqual({ rows: [], totalCount: 7n, loadFailed: false });
  });

  it('throws 404 when characters is absent from this caller list', async () => {
    // The concrete route shadows [section], so without this the resolution the
    // parameterised route performs would simply not happen here.
    await expect(load({ parent: parentWith([section('audit')]) })).rejects.toMatchObject({
      status: 404,
    });
  });

  it('throws 404 for an empty section list', async () => {
    await expect(load({ parent: parentWith([]) })).rejects.toMatchObject({ status: 404 });
  });

  it('throws 404 on a denial rather than rendering a forbidden screen', async () => {
    impl.list = async () => {
      throw new ConnectError('nope', Code.PermissionDenied);
    };
    await expect(load({ parent: parentWith([section('characters')]) })).rejects.toMatchObject({
      status: 404,
    });
  });

  it('resolves an infrastructure failure to the shared retry state, not a 404', async () => {
    impl.list = async () => {
      throw new ConnectError('down', Code.Unavailable);
    };
    const data = await load({ parent: parentWith([section('characters')]) });
    expect(data).toEqual({ rows: [], totalCount: 0n, loadFailed: true });
  });

  it('resolves a deadline the same way as any other outage', async () => {
    impl.list = async () => {
      throw new ConnectError('slow', Code.DeadlineExceeded);
    };
    const data = await load({ parent: parentWith([section('characters')]) });
    expect(data.loadFailed).toBe(true);
  });

  it('defaults an unrecognised refusal to the not-found, never the retry state', async () => {
    // classifyAdminFailure is total and its residue is the denial class: a
    // retry state for a genuine refusal announces that something is there to
    // retry.
    impl.list = async () => {
      throw new ConnectError('nope', Code.ResourceExhausted);
    };
    await expect(load({ parent: parentWith([section('characters')]) })).rejects.toMatchObject({
      status: 404,
    });
  });
});
