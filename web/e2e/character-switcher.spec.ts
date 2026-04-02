// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db } from './helpers/fixtures';

/** Generate unique test credentials. */
function uniqueUser(prefix: string) {
  const suffix = `${Date.now()}_${crypto.randomUUID().slice(0, 4)}`;
  const charSuffix = crypto.randomUUID().replace(/[^a-z]/g, '').slice(0, 6);
  return {
    username: `e2e_${prefix}_${suffix}`,
    charName: `${prefix.charAt(0).toUpperCase() + prefix.slice(1)} ${charSuffix}`,
  };
}

/** Register a player, create a character, and enter the terminal. */
async function registerAndEnterGame(
  page: import('@playwright/test').Page,
  username: string,
  charName: string,
) {
  await page.goto('/register');
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', 'testpass123');
  await page.fill('input[name="confirmPassword"]', 'testpass123');
  await page.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

  await page.locator('text=Create New Character').click();
  await page.fill('input[name="characterName"]', charName);
  await page.locator('label.checkbox-label input[type="checkbox"]').check();
  await page.locator('button:has-text("Create")').click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
}

test.describe('Character Switcher', () => {
  test('character switcher shows existing character after entering game', async ({ page }) => {
    const { username, charName } = uniqueUser('switch');
    await registerAndEnterGame(page, username, charName);

    // Click character switcher button in TopBar
    await page.getByLabel('Switch character').click();
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // The existing character should appear in the picker grid (not the TopBar)
    await expect(page.locator('.grid .char-name', { hasText: charName })).toBeVisible({ timeout: 10000 });
  });

  test('quit character lands on character picker with player still authenticated', async ({
    page,
  }) => {
    const { username, charName } = uniqueUser('quit');
    await registerAndEnterGame(page, username, charName);

    // Type quit command
    const input = page.locator('textarea');
    await input.fill('quit');
    await input.press('Enter');

    // Should navigate to /characters, not / or /login
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Player is still authenticated — character picker title visible
    await expect(page.locator('text=Choose Your Character')).toBeVisible({ timeout: 10000 });

    // Player session token still in sessionStorage
    const storedPlayer = await page.evaluate(() => sessionStorage.getItem('holomush-player'));
    expect(storedPlayer).not.toBeNull();
  });

  test('logout button navigates to home and clears session', async ({ page }) => {
    const { username, charName } = uniqueUser('logout');
    await registerAndEnterGame(page, username, charName);

    // Click logout button in TopBar
    await page.getByLabel('Log out').click();

    // Should navigate to / (match full URL ending with just /)
    await expect(page).toHaveURL(/\/$/, { timeout: 10000 });

    // All session storage cleared
    const storedPlayer = await page.evaluate(() => sessionStorage.getItem('holomush-player'));
    expect(storedPlayer).toBeNull();
    const storedSession = await page.evaluate(() => sessionStorage.getItem('holomush-session'));
    expect(storedSession).toBeNull();

    // Navigating to /characters should redirect away (no longer authenticated)
    await page.goto('/characters');
    await expect(page).not.toHaveURL(/\/characters/);
  });

  test('can re-enter game after quit via character picker', async ({ page }) => {
    const { username, charName } = uniqueUser('reenter');
    await registerAndEnterGame(page, username, charName);

    // Quit to character picker
    const input = page.locator('textarea');
    await input.fill('quit');
    await input.press('Enter');
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Select the existing character to re-enter
    const charCard = page.locator('.char-name', { hasText: charName });
    await expect(charCard).toBeVisible({ timeout: 10000 });
    await charCard.click();
    await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // DB: new game session is active
    const player = await db.getPlayerByUsername(username);
    expect(player).not.toBeNull();
    const chars = await db.getCharactersByPlayerId(player!.id);
    expect(chars.length).toBe(1);
    const session = await db.getSessionByCharacterId(chars[0].id);
    expect(session).not.toBeNull();
    expect(session!.status).toBe('active');
  });
});
