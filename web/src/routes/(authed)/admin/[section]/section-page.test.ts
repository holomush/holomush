// web/src/routes/(authed)/admin/[section]/section-page.test.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// The resolution is a LOAD function, so its 404 is a route decision a test can
// observe. A component that throws during mount renders NOTHING, which makes
// "renders the not-found" unassertable from a component test — the same reason
// the sibling zero-sections case lives in admin-layout.test.ts.
//
// Named section-page.test.ts rather than +page.test.ts: SvelteKit refuses to
// build when src/routes/ holds a +-prefixed file it does not recognise as a
// route file (#4979). The plain .test.ts suffix keeps it in the server project,
// which is right — it mounts nothing.

import { describe, it, expect } from 'vitest';
import { isHttpError } from '@sveltejs/kit';
import type { AdminSectionEntry } from '$lib/stores/adminNavStore';
import { load } from './+page';

const SECTIONS: AdminSectionEntry[] = [
  { id: 'one', displayName: 'Name One', status: 'available' },
  { id: 'two', displayName: 'Name Two', status: 'planned' },
];

const call = (section: string, sections = SECTIONS) =>
  load({
    params: { section },
    parent: async () => ({ sections, loadFailed: false }),
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

describe('the admin section load', () => {
  it('returns the planned entry the parent already resolved', async () => {
    const data = await call('two');
    expect(data.entry.id).toBe('two');
    expect(data.entry.displayName).toBe('Name Two');
    expect(data.entry.status).toBe('planned');
  });

  it('returns an available entry unchanged, for a route that owns its own screen', async () => {
    const data = await call('one');
    expect(data.entry.status).toBe('available');
  });

  // The two cases below are asserted IDENTICALLY on purpose. Nothing about the
  // outcome distinguishes "there is no such section" from "that one is not
  // yours" — one branch covers both, which is the property.
  it('throws 404 for an id that is in no registry at all', async () => {
    expect(await statusOfThrow(() => call('not-a-section'))).toBe(404);
  });

  it('throws 404 for an id simply absent from THIS caller list', async () => {
    expect(await statusOfThrow(() => call('two', [SECTIONS[0]]))).toBe(404);
  });

  it('throws 404 for an empty caller list', async () => {
    expect(await statusOfThrow(() => call('one', []))).toBe(404);
  });
});
