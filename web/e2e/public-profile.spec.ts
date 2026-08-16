// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * `/c/[id]` — the first application path this app has ever served to a browser
 * carrying no session cookie at all.
 *
 * Every other route lives under `(authed)` and its layout redirects; the only
 * anonymous surface the suite exercised before this file was `/`. The
 * combination PROFILE-01 promises — create while signed in, then READ from a
 * fresh, cookie-less context — had no in-tree precedent, which is why the
 * anonymous half of every test below runs against `browser.newContext()` and
 * never against the fixture's own `page`. Reusing the signed-in page would
 * silently exercise the authenticated path and pass while the anonymous one was
 * broken.
 *
 * WHAT THIS FILE DOES NOT DISCHARGE. It asserts the user-visible journey
 * against the DOM. It does NOT satisfy PORTAL-10 rule 3 (01-SPEC §12.1 rule 3),
 * which requires assertions against MARSHALED RESPONSE BYTES and says in terms
 * that a Playwright DOM assertion does not satisfy it: a field absent from the
 * rendered page may still have been on the wire. That obligation is met by plan
 * 05-05's integration specs
 * (test/integration/access/character_profile_read_test.go and its siblings),
 * which assert over the response message itself. Do not read this file as the
 * privacy proof.
 */

import { test, expect, db, registerPlayer, createCharacter } from './helpers/fixtures';

// Values authored here so the anonymous read has something to show beyond the
// name, and distinctive enough that finding them on the page cannot be an
// accident of chrome or seeded content.
const PRONOUNS = 'they/them';
const DESCRIPTION = 'A traveller in a long coat the colour of wet slate.';

/**
 * A syntactically valid ULID (Crockford base32, 26 chars) that names no
 * character. `Z` is a legal ULID symbol, and a first character of `7` keeps the
 * value inside the 48-bit timestamp range, so this parses and simply misses.
 */
const ABSENT_ULID = '7ZZZZZZZZZZZZZZZZZZZZZZZZZ';

/**
 * Not a ULID at all. The facade answers a malformed id with the SAME code and
 * the SAME message literal as a well-formed one naming no row
 * (internal/grpc/characteraccess_service.go GetCharacterProfile: "that is not a
 * ULID" is still a statement about which characters exist). That parity is the
 * §8.7 property this file compares.
 */
const MALFORMED_ID = 'not-a-ulid';

/** Normalised visible text, so whitespace differences do not fake a divergence. */
async function visibleText(page: import('@playwright/test').Page): Promise<string> {
  const body = await page.locator('body').innerText();
  return body.replace(/\s+/g, ' ').trim();
}

test.describe('Public profile — the logged-out read', () => {
  test('a browser carrying no session cookie reads a character name, pronouns and description at /c/[id]', async ({
    page,
    browser,
  }) => {
    // ── Signed in: create the character and author the two fields ──
    const { charName } = await registerPlayer(page, 'pubp');
    await createCharacter(page, charName);

    // The /c/<id> URL is keyed on the character ID, never the name (§9.2).
    const row = await db.getCharacterByName(charName);
    expect(row, `expected a characters row for ${charName}`).not.toBeNull();
    const characterId = row!.id;

    await page.goto(`/characters/${characterId}`);
    await page.fill('input[name="pronouns"]', PRONOUNS);
    await page.getByRole('button', { name: 'Save identity' }).click();
    await expect(page.getByText('Saved.').first()).toBeVisible({ timeout: 10000 });

    await page.fill('textarea[name="description"]', DESCRIPTION);
    await page.getByRole('button', { name: 'Save in-world description' }).click();
    await expect(page.getByText('Saved.').nth(1)).toBeVisible({ timeout: 10000 });

    // ── Signed OUT: a brand-new context, with no storage state of any kind ──
    const anon = await browser.newContext();
    try {
      const anonPage = await anon.newPage();

      // Belt and braces: prove the context really carries no cookie before the
      // read, so a regression that leaked one would fail HERE rather than
      // silently turn this into an authenticated test.
      expect(await anon.cookies(), 'the anonymous context must carry no cookie').toHaveLength(0);

      await anonPage.goto(`/c/${characterId}`);

      await expect(anonPage.getByRole('heading', { name: charName })).toBeVisible({
        timeout: 15000,
      });
      await expect(anonPage.getByText(PRONOUNS)).toBeVisible();
      await expect(anonPage.getByTestId('description')).toHaveText(DESCRIPTION);

      // The unconditional invitation 007-C permits: the root layout's TopBar
      // renders Login / Register for every anonymous viewer on every page
      // (D-85). Its presence here is what makes a profile-LOCAL sign-in notice
      // unnecessary — and a profile-local one would vary with the page it sat
      // on, which is exactly what would make it an oracle for which profiles
      // are populated.
      await expect(anonPage.getByRole('link', { name: 'Login' })).toBeVisible();
      await expect(anonPage.getByRole('link', { name: 'Register' })).toBeVisible();
    } finally {
      await anon.close();
    }
  });

  test('two opaque /c/[id] failures render identical pages that name neither the identifier nor a reason', async ({
    browser,
  }) => {
    const anon = await browser.newContext();
    try {
      const anonPage = await anon.newPage();
      expect(await anon.cookies(), 'the anonymous context must carry no cookie').toHaveLength(0);

      await anonPage.goto(`/c/${ABSENT_ULID}`);
      await expect(anonPage.getByRole('heading', { name: 'Not found' })).toBeVisible({
        timeout: 15000,
      });
      const absentText = await visibleText(anonPage);

      await anonPage.goto(`/c/${MALFORMED_ID}`);
      await expect(anonPage.getByRole('heading', { name: 'Not found' })).toBeVisible({
        timeout: 15000,
      });
      const malformedText = await visibleText(anonPage);

      // CAPTURED-vs-CAPTURED, never captured-vs-hardcoded. A hardcoded expected
      // string passes while the two pages diverge from each other, which is the
      // only thing §8.7 actually forbids.
      expect(malformedText).toBe(absentText);

      // It echoes neither identifier: echoing the string back confirms it was
      // routed, and the malformed one would additionally confirm that "not a
      // ULID" is a distinguishable outcome.
      expect(absentText).not.toContain(ABSENT_ULID);
      expect(malformedText).not.toContain(MALFORMED_ID);

      // And no reason — §9.6 collapses "no such character" and "below this
      // viewer's floor" into one code and one message on purpose, so any word
      // that would let a reader tell them apart is a leak.
      for (const reason of [
        /permission/i,
        /denied/i,
        /forbidden/i,
        /unauthor/i,
        /private/i,
        /hidden/i,
        /retired/i,
        /sign in/i,
        /log in/i,
        /invalid/i,
        /malformed/i,
        /ulid/i,
      ]) {
        expect(absentText, `not-found copy must name no reason (${reason})`).not.toMatch(reason);
      }
    } finally {
      await anon.close();
    }
  });

  test('the not-found page is distinguishable from a real profile, so the parity check is not vacuous', async ({
    page,
    browser,
  }) => {
    // Without this, the previous test would still pass if EVERY /c/<id> render
    // — including a populated one — collapsed to the same page. The property
    // that matters is "the two FAILURES agree", not "everything agrees".
    const { charName } = await registerPlayer(page, 'pubc');
    await createCharacter(page, charName);
    const row = await db.getCharacterByName(charName);
    expect(row).not.toBeNull();

    const anon = await browser.newContext();
    try {
      const anonPage = await anon.newPage();

      await anonPage.goto(`/c/${row!.id}`);
      await expect(anonPage.getByRole('heading', { name: charName })).toBeVisible({
        timeout: 15000,
      });
      const found = await visibleText(anonPage);

      await anonPage.goto(`/c/${ABSENT_ULID}`);
      await expect(anonPage.getByRole('heading', { name: 'Not found' })).toBeVisible({
        timeout: 15000,
      });
      const notFound = await visibleText(anonPage);

      expect(found).not.toBe(notFound);
      expect(found).toContain(charName);
      expect(notFound).not.toContain(charName);
    } finally {
      await anon.close();
    }
  });
});
