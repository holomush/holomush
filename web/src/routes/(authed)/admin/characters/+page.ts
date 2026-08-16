// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { error, isRedirect } from '@sveltejs/kit';
import { listAdminCharacters, type CharacterRow } from '$lib/admin/client';
import { classifyAdminFailure } from '$lib/connect/errors';
import type { AdminSectionEntry } from '$lib/stores/adminNavStore';

export const ssr = false;

/**
 * The first page, resolved before the surface renders.
 *
 * WHY THIS LOAD EXISTS AT ALL. This concrete route SHADOWS [section], so the
 * resolution the parameterised route performs — is this id in the array the
 * parent already awaited? — simply would not happen here otherwise. And a
 * refusal has to resolve to a ROUTE decision: throwing from a component during
 * mount renders nothing, and navigating instead would make a denial a
 * distinguishable outcome, which is the leak the whole surface is built to
 * close.
 *
 * Three outcomes, and no fourth. They are the same three the admin layout
 * already draws, reached through the same total classifier:
 *
 *  - the section is absent from this caller's list → error(404), the ordinary
 *    not-found, identical to a mistyped URL;
 *  - a denial-class refusal from the list call → error(404), likewise. Never a
 *    redirect and never a "forbidden" screen;
 *  - an infrastructure-class failure → one shared retry state, identical for
 *    every viewer, naming nothing about the caller or the call.
 *
 * This load is NOT the access control. The core-side check is, and it denies
 * independently of anything decided here; what this owns is which screen a
 * refusal resolves to.
 *
 * Interactive re-reads — a header sort, a status filter, a search, a page turn
 * — run from the component, where their failures resolve into the page's own
 * two authored error strings rather than into a route decision.
 */
export async function load({
  parent,
}: {
  parent: () => Promise<{ sections: AdminSectionEntry[]; loadFailed: boolean }>;
}): Promise<{ rows: CharacterRow[]; totalCount: bigint; loadFailed: boolean }> {
  const { sections } = await parent();
  if (!sections.some((s) => s.id === 'characters')) error(404);

  try {
    const page = await listAdminCharacters({
      sortField: 'name',
      descending: false,
      status: 'all',
      playerId: '',
      page: 1,
    });
    return { rows: page.rows, totalCount: page.totalCount, loadFailed: false };
  } catch (e) {
    if (isRedirect(e)) throw e;
    if (classifyAdminFailure(e) === 'infrastructure') {
      return { rows: [], totalCount: 0n, loadFailed: true };
    }
    error(404);
  }
}
