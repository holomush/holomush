// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * `/characters/new` — the structured six-field creation card (IDENT-01, D-87)
 * and its rejection path.
 *
 * The roster's inline create input is gone. Creation is a LINK to a page of its
 * own, it lands back on the roster rather than in the terminal, and the name it
 * reports is the one the SERVER stored — which for a full-width-Latin
 * submission is not the string that was typed. That inequality is the whole
 * point of the fold assertion below: an echo that read local form state would
 * pass every ASCII fixture and fail only here.
 */

import { test, expect, registerPlayer } from './helpers/fixtures';

/** ASCII printable → its full-width (U+FF01–U+FF5E) counterpart. */
function toFullWidth(s: string): string {
  return [...s].map((c) => String.fromCodePoint(c.codePointAt(0)! + 0xfee0)).join('');
}

/**
 * A name whose NFKC normal form differs from what is typed.
 *
 * `charname.Normalize` runs NFKC first, then strips format runes, then
 * collapses whitespace (internal/charname/pipeline.go), and `Normalized.Display`
 * preserves case — so the stored display name is exactly the ASCII fold. The
 * transform is pure and does no I/O, which makes this a deterministic fixture
 * rather than a guess about server behaviour.
 */
function foldingName(): { typed: string; stored: string } {
  const alpha = 'abcdefghijklmnopqrstuvwxyz';
  const suffix = Array.from({ length: 6 }, () => alpha[Math.floor(Math.random() * 26)]).join('');
  const stored = `Fw${suffix}`;
  return { typed: toFullWidth(stored), stored };
}

const PROFILE = {
  pronouns: 'she/her',
  concept: 'Wandering cartographer',
  species: 'Human',
  age: 'Thirty-one',
  faction: 'The Ninth Survey',
};

/** Every field on the card, read back by its `name` attribute (web/CLAUDE.md). */
const FIELD_NAMES = ['characterName', 'pronouns', 'concept', 'species', 'age', 'faction'] as const;

async function fillAllSix(page: import('@playwright/test').Page, name: string): Promise<void> {
  await page.fill('input[name="characterName"]', name);
  await page.fill('input[name="pronouns"]', PROFILE.pronouns);
  await page.fill('input[name="concept"]', PROFILE.concept);
  await page.fill('input[name="species"]', PROFILE.species);
  await page.fill('input[name="age"]', PROFILE.age);
  await page.fill('input[name="faction"]', PROFILE.faction);
}

test.describe('Character creation — the structured card', () => {
  test('the roster create affordance is a link to /characters/new, not an inline input', async ({
    page,
  }) => {
    await registerPlayer(page, 'crlk');

    // The inline branch is gone (D-87). A spec that still found a name input on
    // the roster would be finding a regression, not a passing test.
    await expect(page.locator('input[name="characterName"]')).toHaveCount(0);

    const create = page.locator('[data-testid="create-character"]');
    await expect(create).toBeVisible({ timeout: 10000 });
    await expect(create).toHaveAttribute('href', '/characters/new');

    await create.click();
    await expect(page).toHaveURL(/\/characters\/new/, { timeout: 10000 });
    await expect(page.getByRole('heading', { name: 'Create a character' })).toBeVisible();
  });

  test('the name rule is stated on first paint, with no interaction', async ({ page }) => {
    await registerPlayer(page, 'crrl');
    await page.locator('[data-testid="create-character"]').click();
    await expect(page).toHaveURL(/\/characters\/new/, { timeout: 10000 });

    // Always shown, because it states the CLASS of rewrite rather than
    // predicting the result for what was typed — predicting would require the
    // client-side mirror of the name pipeline D-88 forbids.
    const rule = page.getByText(/Letters and single spaces/);
    await expect(rule).toBeVisible();
    await expect(rule).toContainText(/are folded, so the name you get may differ/);

    // The rune counter is part of that always-visible affordance.
    await expect(page.getByTestId('name-counter')).toHaveText('0 / 32');
  });

  test('a full-width name is created and the confirmation names the folded ASCII form the server stored', async ({
    page,
  }) => {
    const { typed, stored } = foldingName();
    await registerPlayer(page, 'crfd');

    await page.locator('[data-testid="create-character"]').click();
    await expect(page).toHaveURL(/\/characters\/new/, { timeout: 10000 });
    await fillAllSix(page, typed);
    await page.locator('button[type="submit"]').click();

    // Creation lands on the roster, not the terminal (D-87).
    await expect(page).toHaveURL(/\/characters$/, { timeout: 15000 });

    const confirmation = page.locator('[role="status"]');
    await expect(confirmation.first()).toContainText(stored, { timeout: 15000 });

    // THE LOAD-BEARING HALF. Presence of the ASCII form alone is satisfied by an
    // echo of local form state on any ASCII fixture; only the ABSENCE of the
    // string that was typed proves the confirmation read the server's response.
    await expect(confirmation.first()).not.toContainText(typed);

    // And the card the roster drew carries the same stored string.
    await expect(page.locator('[data-testid="char-name"]', { hasText: stored })).toBeVisible({
      timeout: 10000,
    });
    await expect(page.locator('[data-testid="char-name"]', { hasText: typed })).toHaveCount(0);
  });

  test('a taken name is refused with the authored copy and costs the player none of the six values', async ({
    page,
  }) => {
    const { typed, stored } = foldingName();
    await registerPlayer(page, 'crtk');

    // First create: succeeds.
    await page.locator('[data-testid="create-character"]').click();
    await expect(page).toHaveURL(/\/characters\/new/, { timeout: 10000 });
    await fillAllSix(page, typed);
    await page.locator('button[type="submit"]').click();
    await expect(page.locator('[data-testid="char-name"]', { hasText: stored })).toBeVisible({
      timeout: 15000,
    });

    // Second create: the same name, the same player, the same six values.
    await page.locator('[data-testid="create-character"]').click();
    await expect(page).toHaveURL(/\/characters\/new/, { timeout: 10000 });
    await fillAllSix(page, typed);
    await page.locator('button[type="submit"]').click();

    const alert = page.locator('[role="alert"]');
    await expect(alert.first()).toBeVisible({ timeout: 10000 });
    await expect(alert.first()).toHaveText('That name is taken. Try another.');

    // The refusal names no other character: the pipeline's confusable refusal
    // is deliberately silent about what it collided with, and a helpful
    // addition here would rebuild the enumeration oracle §6.1.2 closes.
    await expect(alert.first()).not.toContainText(stored);
    await expect(alert.first()).not.toContainText(typed);

    // The player stayed put — no navigation to the roster, none to the terminal.
    await expect(page).toHaveURL(/\/characters\/new/);

    // ALL SIX read back individually. A sampled assertion passes while the
    // other five are wiped, and re-typing six fields is the cost the
    // submit-and-report design exists to avoid.
    const expected: Record<(typeof FIELD_NAMES)[number], string> = {
      characterName: typed,
      pronouns: PROFILE.pronouns,
      concept: PROFILE.concept,
      species: PROFILE.species,
      age: PROFILE.age,
      faction: PROFILE.faction,
    };
    for (const field of FIELD_NAMES) {
      await expect(
        page.locator(`input[name="${field}"]`),
        `input[name="${field}"] must still hold its submitted value after a refusal`,
      ).toHaveValue(expected[field]);
    }
  });
});
