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
});
