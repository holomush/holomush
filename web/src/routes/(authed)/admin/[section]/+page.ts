// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { error } from '@sveltejs/kit';
import type { AdminSectionEntry } from '$lib/stores/adminNavStore';

export const ssr = false;

/**
 * Resolves the URL's section against the array the parent layout already
 * awaited — the server's own answer, decided by the same rule that let this
 * caller through the layout at all. Reading it here is therefore not a
 * client-side authorization decision; it is reading the decision.
 *
 * It deliberately does NOT call the per-section read. That call's answer for a
 * section with no handler is an error, and an error carries no response body:
 * the display name is simply not on it. The name IS on the list the parent
 * resolved. The per-section read keeps no browser caller — that is intended,
 * stated in its own wire doc comment, and exercised at the wire level. Do not
 * wire it up here to make it feel used, and do not delete it as dead.
 *
 * The resolution lives in a LOAD function rather than the component so its 404
 * is a route decision a test can observe. A component that throws during mount
 * renders nothing, which makes "renders the not-found" unassertable.
 *
 * Two outcomes:
 *
 *  - no matching entry → error(404). A mistyped URL, an id no server knows,
 *    and one this caller simply did not receive all take this ONE branch, so
 *    nothing here distinguishes them. Never a redirect.
 *  - a matching entry → return it; the page renders from it.
 */
export async function load({
  params,
  parent,
}: {
  params: { section: string };
  parent: () => Promise<{ sections: AdminSectionEntry[]; loadFailed: boolean }>;
}): Promise<{ entry: AdminSectionEntry }> {
  const { sections } = await parent();
  const entry = sections.find((s) => s.id === params.section);
  if (!entry) error(404);
  return { entry };
}
