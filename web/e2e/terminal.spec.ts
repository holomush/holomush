// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect } from '@playwright/test';

test.describe('Terminal UI', () => {
  test('connects and displays events', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');
    await expect(page.locator('.terminal-layout')).toBeVisible();
    // Guest characters get random names like "Beryl_Helium"
    await expect(page.locator('.character')).toContainText(/\w+_\w+/);
  });

  test('sends commands and receives output', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');
    const input = page.locator('textarea');
    // Use 'say' — it emits a say event to the stream (unlike 'look' which returns via RPC response)
    await input.fill('say hello world');
    await input.press('Enter');
    await expect(page.locator('[data-testid="event"]').first()).toBeVisible({ timeout: 10000 });
  });

  test('sidebar toggles with Ctrl+B', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');
    await expect(page.locator('.sidebar:not(.expanded)')).toBeVisible();
    await page.keyboard.press('Control+b');
    await expect(page.locator('.sidebar.expanded')).toBeVisible();
    await page.keyboard.press('Control+b');
    await expect(page.locator('.sidebar:not(.expanded)')).toBeVisible();
  });

  test('responsive layout hides sidebar on mobile', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 });
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');
    await expect(page.locator('button[title="Toggle sidebar"]')).toBeVisible();
  });

  test('presence list shows self and other connections', async ({ browser }) => {
    // Two independent browser contexts (separate sessions)
    const context1 = await browser.newContext();
    const context2 = await browser.newContext();
    const page1 = await context1.newPage();
    const page2 = await context2.newPage();


    // Both connect as guests
    await page1.goto('/terminal');
    await page1.click('text=Connect as Guest');
    await expect(page1.locator('.terminal-layout')).toBeVisible();

    // Get first character's name
    const name1 = await page1.locator('.character').textContent();

    await page2.goto('/terminal');
    await page2.click('text=Connect as Guest');
    await expect(page2.locator('.terminal-layout')).toBeVisible();

    // Get second character's name
    const name2 = await page2.locator('.character').textContent();

    // Wait for arrive event to propagate
    await page1.waitForTimeout(1000);

    // Expand sidebars on both pages
    await page1.keyboard.press('Control+b');
    await page2.keyboard.press('Control+b');
    await expect(page1.locator('.sidebar.expanded')).toBeVisible();
    await expect(page2.locator('.sidebar.expanded')).toBeVisible();

    // Page 1 should see BOTH characters in presence list (self + other)
    const presence1 = page1.locator('.presence-list');
    await expect(presence1).toContainText(name1!, { timeout: 5000 });
    await expect(presence1).toContainText(name2!, { timeout: 5000 });

    // Page 2 should also see BOTH characters
    const presence2 = page2.locator('.presence-list');
    await expect(presence2).toContainText(name1!, { timeout: 5000 });
    await expect(presence2).toContainText(name2!, { timeout: 5000 });

    await context1.close();
    await context2.close();
  });

  test('session survives page reload', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');
    await expect(page.locator('.terminal-layout')).toBeVisible();

    // Capture the character name before reload
    const nameBefore = await page.locator('.character').textContent();

    // Reload the page
    await page.reload();

    // Should reconnect automatically (no login screen)
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // Same character name
    const nameAfter = await page.locator('.character').textContent();
    expect(nameAfter).toBe(nameBefore);
  });

  test('disconnect clears session so reload shows login', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');
    await expect(page.locator('.terminal-layout')).toBeVisible();

    // Send quit command to disconnect
    const input = page.locator('textarea');
    await input.fill('quit');
    await input.press('Enter');

    // Should return to login screen
    await expect(page.locator('text=Connect as Guest')).toBeVisible({ timeout: 5000 });

    // Verify sessionStorage was actually cleared
    const session = await page.evaluate(() => sessionStorage.getItem('holomush-session'));
    expect(session).toBeNull();

    // Reload — should still show login (session was cleared)
    await page.reload();
    await expect(page.locator('text=Connect as Guest')).toBeVisible();
  });

  test('command history with up/down arrows', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');
    const input = page.locator('textarea');
    await input.fill('look');
    await input.press('Enter');
    await input.fill('say hello');
    await input.press('Enter');
    await input.press('ArrowUp');
    await expect(input).toHaveValue('say hello');
    await input.press('ArrowUp');
    await expect(input).toHaveValue('look');
  });

  test('reconnect receives live events after replay', async ({ page }) => {
    await page.goto('/terminal');
    await page.click('text=Connect as Guest');
    await expect(page.locator('.terminal-layout')).toBeVisible();

    // Reload — session persists, stream reconnects
    await page.reload();
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // Send a command with a unique token so we can distinguish it from replayed events
    const token = `live-${Date.now()}`;
    const input = page.locator('textarea');
    await input.fill(`say ${token}`);
    await input.press('Enter');
    await expect(
      page.locator('[data-testid="event"]').filter({ hasText: token })
    ).toBeVisible({ timeout: 10000 });
  });

  // TODO: command history across reconnect requires a core RPC for
  // GetCommandHistory — the gateway has no session store (gateway boundary
  // invariant). Tracked by bead. The within-session history test above
  // covers the client-side arrow key behavior.
});
