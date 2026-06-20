// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test as base, expect, type Page } from '@playwright/test';
import * as db from './db';

export { db };

/** Extract the session ID from the browser's sessionStorage. */
export async function getClientSessionId(page: Page): Promise<string | null> {
  return page.evaluate(() => {
    const raw = sessionStorage.getItem('holomush-session');
    if (!raw) return null;
    try {
      return JSON.parse(raw).sessionId ?? null;
    } catch {
      return null;
    }
  });
}

/** Extract the character name from the browser's sessionStorage. */
export async function getClientCharacterName(page: Page): Promise<string | null> {
  return page.evaluate(() => {
    const raw = sessionStorage.getItem('holomush-session');
    if (!raw) return null;
    try {
      return JSON.parse(raw).characterName ?? null;
    } catch {
      return null;
    }
  });
}

/**
 * Extended test fixture that captures browser console logs and tears down the
 * DB pool after all tests. Import `test` and `expect` from this module instead
 * of @playwright/test.
 */
export const test = base.extend<{ _consoleCapture: void }>({
  _consoleCapture: [
    async ({ page }, use, testInfo) => {
      const logs: string[] = [];
      page.on('console', (msg) => {
        logs.push(`[${msg.type()}] ${msg.text()}`);
      });
      page.on('pageerror', (err) => {
        logs.push(`[error] ${err.message}`);
      });

      await use();

      if (logs.length > 0) {
        await testInfo.attach('browser-console-logs', {
          body: logs.join('\n'),
          contentType: 'text/plain',
        });
      }
    },
    { auto: true },
  ],
});

// Close the shared pool after all workers finish.
base.afterAll(async () => {
  await db.closePool();
});

export { expect };

/** Generate unique test credentials for registered-player scenarios. */
export function uniqueSceneUser(prefix: string) {
  const suffix = `${Date.now()}_${crypto.randomUUID().slice(0, 4)}`;
  const charSuffix = crypto.randomUUID().replace(/[^a-z]/g, '').slice(0, 6);
  // Character names allow letters and spaces only — strip any non-letter chars
  // from the prefix (e.g. 'a11y' → 'ay') before using it in the name.
  const safePrefix = prefix.replace(/[^a-zA-Z]/g, '');
  const capitalised = safePrefix.charAt(0).toUpperCase() + safePrefix.slice(1);
  return {
    username: `e2e_sc_${prefix}_${suffix}`,
    charName: `Sc${capitalised} ${charSuffix}`,
    password: 'testpass123',
  };
}

/**
 * Register a new player, create a character, and land in the terminal.
 * Returns `{ username, password, charName }` for later re-login if needed.
 * Reuses the same form-fill pattern as auth.spec.ts and character-switcher.spec.ts.
 */
export async function registerAndEnterTerminal(
  page: Page,
  prefix: string,
): Promise<{ username: string; password: string; charName: string }> {
  const { username, charName, password } = uniqueSceneUser(prefix);
  await page.goto('/register');
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', password);
  await page.fill('input[name="confirmPassword"]', password);
  await page.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });
  await page.locator('text=Create New Character').click();
  await page.fill('input[name="characterName"]', charName);
  await page.locator('button[role="checkbox"]').click();
  await page.locator('button:has-text("Create")').click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
  // Wait for the stream to be fully open (STREAM_OPENED → connectionId set →
  // REPLAY_COMPLETE → conn-pill shows "connected"). Without this, sendCommand
  // may carry an empty connectionId, causing `scene focus` to error with
  // "`scene focus` requires a live connection."
  await page
    .locator('[data-testid="conn-pill"][data-status="connected"]')
    .waitFor({ timeout: 15000 });
  return { username, password, charName };
}
