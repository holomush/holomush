// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db } from './helpers/fixtures';
import type { Page } from '@playwright/test';

/**
 * Generate a short unique suffix that fits within username length limits (max 30 chars).
 * Format: timestamp last 8 digits + 4 random alpha chars = 13 chars of suffix.
 */
function shortSuffix(): string {
  const ts = String(Date.now()).slice(-8);
  const rand = crypto.randomUUID().slice(0, 4);
  return `${ts}_${rand}`;
}

/**
 * Register a fresh player via the UI, create a character, and enter terminal.
 * Returns { username, charName }. The page is left on the terminal.
 */
async function registerAndEnterTerminal(
  page: Page,
  prefix: string,
  charPrefix: string,
): Promise<{ username: string; charName: string }> {
  const username = `${prefix}${shortSuffix()}`;
  // Character names only allow letters — filter UUID to alpha chars only.
  const charSuffix = crypto.randomUUID().replace(/[^a-z]/g, '').slice(0, 6);
  const charName = `${charPrefix} ${charSuffix}`;

  await page.goto('/register');
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', 'testpass123');
  await page.fill('input[name="confirmPassword"]', 'testpass123');
  await page.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

  const createBtn = page.locator('text=Create New Character');
  await expect(createBtn).toBeVisible({ timeout: 10000 });
  await createBtn.click();
  await page.fill('input[name="characterName"]', charName);
  await page.locator('label.checkbox-label input[type="checkbox"]').check();
  await page.locator('button:has-text("Create")').click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

  return { username, charName };
}

test.describe('Admin Commands', () => {
  test('admin resets player password (generated)', async ({ browser }) => {
    // ── Phase 1: Register target player ──
    const targetCtx = await browser.newContext();
    const targetPage = await targetCtx.newPage();
    const target = await registerAndEnterTerminal(targetPage, 'e2etgt', 'Tgt');

    // Quit the target session — registered user goes to character picker
    const targetInput = targetPage.locator('textarea');
    await targetInput.fill('quit');
    await targetInput.press('Enter');
    await expect(targetPage).toHaveURL(/\/characters/, { timeout: 10000 });
    await targetCtx.close();

    // ── Phase 2: Register admin player and grant admin role via DB ──
    const adminCtx = await browser.newContext();
    const adminPage = await adminCtx.newPage();
    const admin = await registerAndEnterTerminal(adminPage, 'e2eadm', 'Adm');

    // Look up admin's character and grant admin role
    const adminPlayer = await db.getPlayerByUsername(admin.username);
    expect(adminPlayer).not.toBeNull();
    const adminChars = await db.getCharactersByPlayerId(adminPlayer!.id);
    expect(adminChars.length).toBe(1);
    await db.grantAdminRole(adminChars[0].id);

    // Quit and re-enter so the session picks up the admin role
    const adminInput = adminPage.locator('textarea');
    await adminInput.fill('quit');
    await adminInput.press('Enter');
    await expect(adminPage).toHaveURL(/\/characters/, { timeout: 10000 });

    // Select the existing character to re-enter with the admin role
    await expect(adminPage.locator('text=' + admin.charName)).toBeVisible({ timeout: 10000 });
    await adminPage.locator('text=' + admin.charName).click();
    await expect(adminPage).toHaveURL(/\/terminal/, { timeout: 15000 });
    await expect(adminPage.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // ── Phase 3: Reset target password ──
    // Get target's password hash before reset
    const targetPlayer = await db.getPlayerByUsername(target.username);
    expect(targetPlayer).not.toBeNull();
    const hashBefore = await db.getPlayerPasswordHash(targetPlayer!.id);
    expect(hashBefore).toBeTruthy();

    // Send resetpassword command
    const termInput = adminPage.locator('textarea');
    await termInput.fill(`resetpassword ${target.username}`);
    await termInput.press('Enter');

    // Wait for the output event containing the new password
    const eventLocator = adminPage.locator('[data-testid="event"]').filter({
      hasText: /password/i,
    });
    await expect(eventLocator.first()).toBeVisible({ timeout: 10000 });

    // DB: password hash changed
    await expect(async () => {
      const hashAfter = await db.getPlayerPasswordHash(targetPlayer!.id);
      expect(hashAfter).toBeTruthy();
      expect(hashAfter).not.toBe(hashBefore);
    }).toPass({ timeout: 5000 });

    await adminCtx.close();
  });

  test('non-admin denied resetpassword', async ({ browser }) => {
    const ctx = await browser.newContext();
    const page = await ctx.newPage();

    // Register a fresh non-admin player (default role is 'player', not 'admin')
    await registerAndEnterTerminal(page, 'e2ereg', 'Reg');

    // Try resetpassword — should be denied
    const input = page.locator('textarea');
    await input.fill('resetpassword someone');
    await input.press('Enter');

    // Wait for denial message
    const eventLocator = page.locator('[data-testid="event"]').filter({
      hasText: /permission|denied|don.?t have|not allowed|unauthorized/i,
    });
    await expect(eventLocator.first()).toBeVisible({ timeout: 10000 });

    await ctx.close();
  });
});
