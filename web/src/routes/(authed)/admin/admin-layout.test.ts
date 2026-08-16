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
//
// The stub is a plain mutable holder, NOT a vi.fn. Vitest 4.1.10's mock-result
// tracking reports a throw recorded on a vi.fn as a test failure even when the
// code under test caught and handled it — which is this load's entire job, so
// every refusal case failed while `load` was demonstrably correct.

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { ConnectError, Code } from '@connectrpc/connect';
import { isHttpError } from '@sveltejs/kit';
import type { AdminSectionEntry } from '$lib/stores/adminNavStore';

const stub: { impl: () => Promise<AdminSectionEntry[]> } = { impl: async () => [] };
vi.mock('$lib/admin/client', () => ({ listAdminSections: () => stub.impl() }));

const { load } = await import('./+layout');

const section = (id: string, status = 'planned'): AdminSectionEntry => ({
  id,
  displayName: `Name ${id}`,
  status,
});

const resolveWith = (sections: AdminSectionEntry[]) => {
  stub.impl = async () => sections;
};
const rejectWith = (err: unknown) => {
  stub.impl = async () => {
    throw err;
  };
};

async function statusOfThrow(fn: () => unknown): Promise<number> {
  try {
    await fn();
  } catch (e) {
    if (isHttpError(e)) return e.status;
    throw e;
  }
  throw new Error('expected load to throw, but it returned');
}

beforeEach(() => resolveWith([]));

describe('the admin layout load', () => {
  it('returns the sections a permitted caller received', async () => {
    resolveWith([section('a', 'available'), section('b')]);
    const data = await load();
    expect(data.sections.map((s) => s.id)).toEqual(['a', 'b']);
    expect(data.loadFailed).toBe(false);
  });

  it('throws 404 for zero sections, so /admin renders the ordinary not-found', async () => {
    resolveWith([]);
    expect(await statusOfThrow(() => load())).toBe(404);
  });

  it('throws 404 on a denial-class refusal, never a redirect', async () => {
    rejectWith(new ConnectError('no', Code.PermissionDenied));
    expect(await statusOfThrow(() => load())).toBe(404);
  });

  it('throws 404 on an unenumerated refusal too — the fail-safe default', async () => {
    rejectWith(new ConnectError('?', Code.Internal));
    expect(await statusOfThrow(() => load())).toBe(404);
  });

  it('returns loadFailed and throws nothing on an infrastructure-class failure', async () => {
    rejectWith(new ConnectError('down', Code.Unavailable));
    const data = await load();
    expect(data.loadFailed).toBe(true);
    expect(data.sections).toEqual([]);
  });

  it('returns loadFailed on a transport throw that never reached a server', async () => {
    rejectWith(new Error('network down'));
    const data = await load();
    expect(data.loadFailed).toBe(true);
  });

  it('awaits the section list before returning — no partial nav state', async () => {
    // The stubbed promise resolves only AFTER load's own promise is observed.
    // A load that returned early with a placeholder would resolve to an empty
    // or unresolved array here; a bare `rg -n await` cannot see that.
    let release!: (v: AdminSectionEntry[]) => void;
    stub.impl = () => new Promise<AdminSectionEntry[]>((r) => (release = r));

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
