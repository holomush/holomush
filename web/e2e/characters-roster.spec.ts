// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * `/characters` — the sectioned owner roster (IDENT-05, D-96, sketch 008-B).
 *
 * Sectioning, the badge-suppression rule, the collapse chip, the default change,
 * and the one surface the PROFILE-12 not-retroactive statement is allowed to
 * live on.
 *
 * ORDERING IS DELIBERATELY NOT ASSERTED. `charRepo.ListByPlayer` declares no
 * `ORDER BY` (recorded in plan 05-01 and again in 05-07), so PostgreSQL
 * guarantees nothing about the order two consecutive reads return. A spec
 * pinning a stable order here would be asserting an implementation accident of
 * a small heap scan, and would go red the first time the plan changed with no
 * defect behind it. The fix is one `ORDER BY` on the server; until it lands,
 * every assertion below is order-independent.
 */

import pg from 'pg';
import { test, expect, db, registerPlayer, createCharacter } from './helpers/fixtures';

/**
 * There is NO player-facing retirement flow in v0.13 — IDENT-04 defers
 * self-retire and the admin path is Phase 6's — so the only way to observe a
 * not-playable card is to write the lifecycle directly. The suite's precedent
 * for reaching a state the UI cannot is `db.grantAdminRole`, a direct write in
 * the helpers module; this one stays inline in the spec instead, because it is
 * this file's fixture and nothing else needs it.
 */
async function retireCharacter(characterId: string): Promise<void> {
  const url = process.env.E2E_DATABASE_URL;
  if (!url) throw new Error('E2E_DATABASE_URL environment variable is required');
  const client = new pg.Client({ connectionString: url });
  await client.connect();
  try {
    const res = await client.query("UPDATE characters SET status = 'retired' WHERE id = $1", [
      characterId,
    ]);
    if (res.rowCount !== 1) {
      throw new Error(`expected to retire exactly 1 character, updated ${res.rowCount}`);
    }
  } finally {
    await client.end();
  }
}

/**
 * The two authored session words. Neither may appear on a retired card.
 * Case-insensitive so a future `text-transform` on the badge cannot smuggle the
 * vocabulary past the assertion. `Retired` does not match either alternative.
 */
const SESSION_WORDS = /\b(active|offline)\b/i;

/** The PROFILE-12 standing statement, authored once in 05-UI-SPEC. */
const NOT_RETROACTIVE = /Privacy here is not retroactive/;

/** A unique second character name. Letters and a single space only. */
function secondName(base: string): string {
  const alpha = 'abcdefghijklmnopqrstuvwxyz';
  const suffix = Array.from({ length: 6 }, () => alpha[Math.floor(Math.random() * 26)]).join('');
  return `${base} ${suffix}`;
}

test.describe('Owner roster — sections, badges and the default', () => {
  test('a retired character sits in Not playable, wears the Retired badge and no session word', async ({
    page,
  }) => {
    const { charName } = await registerPlayer(page, 'rsrt');
    const keeper = charName;
    const retiree = secondName('Ret');

    await createCharacter(page, keeper);
    await createCharacter(page, retiree);

    const retiredRow = await db.getCharacterByName(retiree);
    expect(retiredRow, `expected a characters row for ${retiree}`).not.toBeNull();
    await retireCharacter(retiredRow!.id);

    await page.reload();
    await expect(page.locator('[data-testid="char-name"]', { hasText: keeper })).toBeVisible({
      timeout: 15000,
    });

    // ── Sectioning ──
    const headings = page.locator('h2');
    await expect(headings.filter({ hasText: 'Playable' }).first()).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Not playable' })).toBeVisible();

    // Document order: Playable first. `Not playable` contains the substring
    // `playable`, so the two are separated by an EXACT match rather than by a
    // substring filter — a `hasText: 'Playable'` filter matches both.
    //
    // Lower-cased first: the section headings carry `text-transform: uppercase`
    // and `innerText` reports rendered text, so these arrive as `PLAYABLE` and
    // `NOT PLAYABLE`. The accessible name used above is the DOM text and is
    // NOT transformed, which is why the two assertions spell the same heading
    // differently.
    const headingTexts = (await headings.allInnerTexts()).map((t) => t.trim().toLowerCase());
    const playableAt = headingTexts.indexOf('playable');
    const notPlayableAt = headingTexts.indexOf('not playable');
    expect(playableAt).toBeGreaterThanOrEqual(0);
    expect(notPlayableAt).toBeGreaterThan(playableAt);

    // The create affordance is inside the playable grid, not the not-playable one.
    const notPlayableGrid = page.getByTestId('not-playable-grid');
    await expect(notPlayableGrid.locator('[data-testid="create-character"]')).toHaveCount(0);
    await expect(page.locator('[data-testid="create-character"]')).toHaveCount(1);

    // The retired character's card is in the second section; the keeper is not.
    const retiredCard = notPlayableGrid.locator('[data-testid="roster-card"]', {
      hasText: retiree,
    });
    await expect(retiredCard).toHaveCount(1);
    await expect(
      notPlayableGrid.locator('[data-testid="roster-card"]', { hasText: keeper }),
    ).toHaveCount(0);

    // ── The suppression rule (D-96) ──
    // `Retired · Offline` is meaningless, and the two vocabularies collide on
    // the token `active`. The ABSENCE is the load-bearing half: asserting only
    // that the Retired badge is present passes with a session word sitting
    // beside it.
    await expect(retiredCard.getByTestId('lifecycle-badge')).toHaveText('Retired');
    await expect(retiredCard.getByTestId('session-badge')).toHaveCount(0);
    const retiredText = await retiredCard.innerText();
    expect(retiredText, 'a retired card must carry no session word').not.toMatch(SESSION_WORDS);

    // A retired character can never wear the once-per-roster Default marker.
    await expect(retiredCard.getByTestId('default-badge')).toHaveCount(0);
  });

  test('the Not playable chip starts expanded and collapsing REMOVES its grid from the markup', async ({
    page,
  }) => {
    const { charName } = await registerPlayer(page, 'rsch');
    const retiree = secondName('Chip');

    await createCharacter(page, charName);
    await createCharacter(page, retiree);
    const retiredRow = await db.getCharacterByName(retiree);
    expect(retiredRow).not.toBeNull();
    await retireCharacter(retiredRow!.id);

    await page.reload();
    const chip = page.getByTestId('not-playable-toggle');
    await expect(chip).toBeVisible({ timeout: 15000 });

    // Expanded on first paint: these are the player's own characters, and a
    // default that hides them presumes they are clutter.
    await expect(chip).toHaveAttribute('aria-expanded', 'true');
    await expect(chip).toHaveAttribute('aria-controls', 'roster-not-playable');
    // The label carries the count and the action, and singular for one.
    await expect(chip).toHaveText('Hide 1 character');
    await expect(page.getByTestId('not-playable-grid')).toHaveCount(1);

    await chip.click();

    await expect(chip).toHaveAttribute('aria-expanded', 'false');
    await expect(chip).toHaveText('Show 1 character');
    // REMOVED, not hidden. A display:none grid is still in the accessibility
    // tree, which would make aria-expanded="false" a claim about content that
    // can still be announced. toHaveCount(0) is the assertion a class-based
    // check cannot make.
    await expect(page.getByTestId('not-playable-grid')).toHaveCount(0);

    await chip.click();
    await expect(chip).toHaveAttribute('aria-expanded', 'true');
    await expect(page.getByTestId('not-playable-grid')).toHaveCount(1);
  });

  test('setting a different character as default moves the badge and announces it without a reload', async ({
    page,
  }) => {
    const { charName } = await registerPlayer(page, 'rsdf');
    const second = secondName('Def');

    await createCharacter(page, charName);
    await createCharacter(page, second);

    await expect(page.locator('[data-testid="char-name"]', { hasText: second })).toBeVisible({
      timeout: 15000,
    });

    // At most one Default marker, whatever the session reports.
    const badges = page.getByTestId('default-badge');
    expect(await badges.count()).toBeLessThanOrEqual(1);

    // Pick a card that is NOT currently the default — `Make default` renders
    // only on a playable card that is not already the default, so its presence
    // is the selector.
    const target = page
      .locator('[data-testid="roster-card"]')
      .filter({ has: page.locator('[data-testid="make-default"]') })
      .first();
    const targetName = (await target.getByTestId('char-name').innerText()).trim();

    await target.getByTestId('make-default').click();

    // No page.reload() anywhere between the click and these assertions: the
    // marker moves in the live document, from the roster the write returned.
    await expect(
      page.locator('[data-testid="roster-card"]', { hasText: targetName }).getByTestId('default-badge'),
    ).toBeVisible({ timeout: 15000 });
    await expect(page.getByTestId('default-badge')).toHaveCount(1);

    // The authored confirmation names that character, in the status region —
    // a succeeded write is never announced as an alert.
    await expect(page.locator('[role="status"]').first()).toContainText(
      `${targetName} is now your default character.`,
    );

    // A `Make default` control no longer renders on the card that now holds it.
    await expect(
      page
        .locator('[data-testid="roster-card"]', { hasText: targetName })
        .getByTestId('make-default'),
    ).toHaveCount(0);
  });

  test('the not-retroactive statement is on /characters/[id] and on no other surface', async ({
    page,
    browser,
  }) => {
    const { charName } = await registerPlayer(page, 'rsnt');
    await createCharacter(page, charName);

    const row = await db.getCharacterByName(charName);
    expect(row).not.toBeNull();
    const characterId = row!.id;

    // PRESENT on the authoring surface — the one place a player is making a
    // decision the statement is about.
    await page.goto(`/characters/${characterId}`);
    await expect(page.getByRole('heading', { name: charName })).toBeVisible({ timeout: 15000 });
    await expect(page.getByText(NOT_RETROACTIVE)).toBeVisible();

    // ABSENT from the roster. D-91 scopes it to the authoring surface; a copy
    // here would be a warning where nobody is deciding anything.
    await page.goto('/characters');
    await expect(page.locator('[data-testid="char-name"]', { hasText: charName })).toBeVisible({
      timeout: 15000,
    });
    expect(await page.locator('body').innerText()).not.toMatch(NOT_RETROACTIVE);

    // ABSENT from the public profile, read the way a visitor reads it: with no
    // session cookie at all.
    const anon = await browser.newContext();
    try {
      const anonPage = await anon.newPage();
      await anonPage.goto(`/c/${characterId}`);
      await expect(anonPage.getByRole('heading', { name: charName })).toBeVisible({
        timeout: 15000,
      });
      expect(await anonPage.locator('body').innerText()).not.toMatch(NOT_RETROACTIVE);
    } finally {
      await anon.close();
    }
  });
});
