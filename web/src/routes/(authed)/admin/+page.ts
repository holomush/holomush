// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { redirect } from '@sveltejs/kit';
import type { LayoutData } from './$types';

export const ssr = false;

/**
 * /admin has no screen of its own; it resolves to the first section the caller
 * may use.
 *
 * THIS IS THE ONE PLACE IN THE ADMIN TREE THAT MAY REDIRECT, and the
 * distinction is the whole point. A refusal must never redirect: a redirected
 * refusal is a distinguishable outcome and tells the caller something is here.
 * A canonical-index resolution for a caller who has ALREADY passed the gate
 * tells them nothing they did not already have — the list it resolves against
 * is the server's own answer, awaited one level up. No layout and no [section]
 * route redirects; both throw error(404) instead.
 *
 * When the parent could not reach the server at all it returns loadFailed and
 * an empty list. There is nothing to resolve to, so this returns and the
 * layout's shared retry state renders in place.
 */
export async function load({ parent }: { parent: () => Promise<LayoutData> }) {
  const { sections, loadFailed } = await parent();
  if (loadFailed || sections.length === 0) return {};
  redirect(303, `/admin/${sections[0].id}`);
}
