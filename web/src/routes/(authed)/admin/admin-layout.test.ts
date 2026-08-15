// web/src/routes/(authed)/admin/admin-layout.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// The load function under test, not a component. The 404 is a ROUTE decision:
// feeding an empty array to AdminNav proves only that the component renders no
// links, and would stay green if load returned {sections: []} and the browser
// showed an empty admin shell — the exact defect.
//
// The filename is admin-layout.test.ts rather than +layout.test.ts because
// SvelteKit refuses to build when src/routes/ holds a +-prefixed file it does
// not recognise as a route file (#4979). The plain .test.ts suffix routes it to
// the server Vitest project, which is correct: it mounts nothing.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ConnectError, Code } from '@connectrpc/connect';
import { isHttpError } from '@sveltejs/kit';

const listAdminSections = vi.fn();
vi.mock('$lib/admin/client', () => ({ listAdminSections: () => listAdminSections() }));

const { load } = await import('./+layout');

const section = (id: string, status = 'planned') => ({
  id,
  displayName: `Name ${id}`,
  status,
});

async function statusOfThrow(fn: () => unknown): Promise<number> {
  try {
    await fn();
  } catch (e) {
    if (isHttpError(e)) return e.status;
    throw e;
  }
  throw new Error('expected load to throw, but it returned');
}

beforeEach(() => listAdminSections.mockReset());

describe('the admin layout load', () => {
  it('returns the sections a permitted caller received', async () => {
    listAdminSections.mockResolvedValue([section('a', 'available'), section('b')]);
    const data = await load();
    expect(data.sections.map((s) => s.id)).toEqual(['a', 'b']);
    expect(data.loadFailed).toBe(false);
  });

  it('throws 404 for zero sections, so /admin renders the ordinary not-found', async () => {
    listAdminSections.mockResolvedValue([]);
    expect(await statusOfThrow(() => load())).toBe(404);
  });

  it('throws 404 on a denial-class refusal, never a redirect', async () => {
    listAdminSections.mockRejectedValue(new ConnectError('no', Code.PermissionDenied));
    expect(await statusOfThrow(() => load())).toBe(404);
  });

  it('throws 404 on an unenumerated refusal too — the fail-safe default', async () => {
    listAdminSections.mockRejectedValue(new ConnectError('?', Code.Internal));
    expect(await statusOfThrow(() => load())).toBe(404);
  });

  it('returns loadFailed and throws nothing on an infrastructure-class failure', async () => {
    listAdminSections.mockRejectedValue(new ConnectError('down', Code.Unavailable));
    const data = await load();
    expect(data.loadFailed).toBe(true);
    expect(data.sections).toEqual([]);
  });

  it('awaits the section list before returning — no partial nav state', async () => {
    // The mocked promise resolves only AFTER load's own promise is observed.
    // A load that returned early with a placeholder would resolve to an empty
    // or unresolved array here; a bare `rg -n await` cannot see that.
    let release!: (v: unknown) => void;
    listAdminSections.mockReturnValue(new Promise((r) => (release = r)));

    let settled = false;
    const pending = load().then((d) => {
      settled = true;
      return d;
    });

    await Promise.resolve();
    expect(settled).toBe(false);

    release([section('a'), section('b'), section('c')]);
    const data = await pending;
    expect(data.sections).toHaveLength(3);
    expect(data.sections.map((s) => s.id)).toEqual(['a', 'b', 'c']);
  });
});
