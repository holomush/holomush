// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
//
// Admin-portal E2E helpers, shared by more than one spec.
//
// These four symbols were local to admin-portal.spec.ts until a SECOND spec —
// admin-band-root-font.spec.ts, which runs in its own Playwright project at a
// non-default root font size — needed the same sign-in and the same locators.
// They are moved verbatim rather than reimplemented, so the two specs cannot
// drift into disagreeing about what "signed in as an admin" means.

import { test, expect, db, registerPlayer, createCharacter } from './fixtures';
import type { Page } from '@playwright/test';

/**
 * Register a player, give them a character, grant that character the admin
 * role, and land on /admin/characters.
 *
 * The role reaches the browser as the ADMIN-08 nav hint on the next
 * WebCheckSession, which the (authed) layout load issues — so a navigation
 * after the grant is all that is needed; no terminal round trip.
 */
export async function signInAsAdmin(page: Page, prefix: string) {
  // The prefix stays SHORT and letters-only. `uniqueSceneUser` builds
  // `e2e_sc_{prefix}_{13-digit ms}_{4}` = 26 + prefix characters against a
  // 30-character username limit, so a five-character prefix silently fails
  // registration and the failure surfaces as "still on /register".
  expect(prefix).toMatch(/^[a-z]{2,4}$/);
  const creds = await registerPlayer(page, prefix);
  await createCharacter(page, creds.charName);
  const player = await db.getPlayerByUsername(creds.username);
  expect(player).not.toBeNull();
  const chars = await db.getCharactersByPlayerId(player!.id);
  expect(chars.length).toBeGreaterThan(0);
  await db.grantAdminRole(chars[0].id);
  const admin = { ...creds, characterId: chars[0].id, playerId: player!.id };
  await gotoAdminCharacters(page, admin.charName);
  return admin;
}

/**
 * Land on /admin/characters with the list narrowed to one character.
 *
 * The narrowing is not decoration: the admin list is every character in the
 * database, page size 50, and the E2E database is shared with nineteen other
 * specs. Without it a row this file created can sit on page 3.
 */
export async function gotoAdminCharacters(page: Page, charName: string) {
  await page.goto('/admin/characters');
  await expect(page.getByRole('heading', { name: 'Characters' })).toBeVisible({ timeout: 15000 });
  // The random letters-only tail of the generated name: unique, ASCII, and a
  // substring of the stored normal form whatever the folding turns out to be.
  const term = charName.split(' ').pop() ?? charName;

  // SETTLE THE SEARCH THIS HELPER STARTS — not merely the row it was looking for.
  //
  // `fill` arms CharacterFilterBar's 250ms debounce, and the row assertion below
  // does NOT wait for it: a character this fixture just created is already on
  // page 1 of the INITIAL unfiltered list, so the row can be present before the
  // search has even been issued. Returning with the timer still armed lets that
  // search land inside whatever the caller measures next — holomush-i4986, where
  // it was counted as a list re-read no caller had caused.
  //
  // Awaiting the response is what makes "narrowed to one character" TRUE on
  // return, rather than "a matching row happens to be on screen". Registered
  // before the fill so a fast answer cannot be missed.
  const searched = page.waitForResponse(
    (r) => r.url().includes('/holomush.web.v1.WebService/WebAdminSearchCharacters'),
    { timeout: 15000 },
  );
  await page.locator('input[name="q"]').fill(term);
  const searchResponse = await searched;
  // Fail naming the RPC. A failed search renders the `failure === 'search'` empty
  // state, so without this the row assertion below reports "expected 1, received 0"
  // 15s later — blaming the row for an error this line already has in hand.
  expect(searchResponse.ok(), `search RPC failed: HTTP ${searchResponse.status()}`).toBeTruthy();
  await expect(rowFor(page, charName)).toHaveCount(1, { timeout: 15000 });
}

export const sheet = (page: Page) => page.locator('[data-slot="sheet-content"]');

/** The row for a character, by its rendered name. */
export function rowFor(page: Page, name: string) {
  return page.locator('tr.charrow', { hasText: name });
}

// Re-exported so a spec importing these helpers gets the FIXTURE-extended
// `test` (database lifecycle, per-worker isolation) rather than the bare one
// from @playwright/test — importing the bare one is a silent way to lose the
// fixtures these helpers depend on.
export { test, expect, db, registerPlayer, createCharacter };
