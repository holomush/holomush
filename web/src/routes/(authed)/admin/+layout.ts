// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { error, isRedirect } from '@sveltejs/kit';
import { listAdminSections } from '$lib/admin/client';
import { classifyAdminFailure } from '$lib/connect/errors';
import type { AdminSectionEntry } from '$lib/stores/adminNavStore';

export const ssr = false;

/**
 * Resolves the admin section list before the shell renders. This load is NOT
 * the access control — the core-side check is, and it denies independently of
 * anything decided here. What this function owns is which screen a refusal
 * resolves to.
 *
 * Three outcomes, and no fourth:
 *
 *  - a list with members → render it;
 *  - a denial-class refusal → error(404), so the ordinary root boundary draws
 *    the same not-found a mistyped URL draws. Never a redirect: a redirected
 *    refusal is a distinguishable outcome, which is the leak;
 *  - an infrastructure-class failure → one shared retry state, identical for
 *    every viewer, naming nothing about the caller.
 *
 * The empty-list branch is core-UNREACHABLE in v0.13 and is written anyway. The
 * seeded rule is resource-type scoped, so the usable set is all-or-nothing; and
 * a caller who may use none of these sections is refused outright rather than
 * handed an empty 200. "Empty" and "refused" are therefore distinct server
 * answers today and only the second is reachable. It resolves to error(404)
 * regardless, so the two are one screen — an empty-nav state would be a third
 * rendering of "nothing here" and would distinguish the two callers.
 *
 * The list is AWAITED rather than streamed. A nav that renders empty and then
 * fills in shows one frame indistinguishable from having nothing to show, and
 * that frame is the leak. The route-level waiting affordance is the app shell.
 */
export async function load(): Promise<{ sections: AdminSectionEntry[]; loadFailed: boolean }> {
  let sections: AdminSectionEntry[];
  try {
    sections = await listAdminSections();
  } catch (e) {
    if (isRedirect(e)) throw e;
    if (classifyAdminFailure(e) === 'infrastructure') {
      return { sections: [], loadFailed: true };
    }
    error(404);
  }

  if (sections.length === 0) error(404);

  return { sections, loadFailed: false };
}
