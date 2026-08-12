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
 * Register a new player and land on the roster with no characters yet.
 * Split out of `registerAndEnterTerminal` so a spec that wants to drive the
 * creation card itself does not have to re-type the registration form.
 */
export async function registerPlayer(
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
  return { username, password, charName };
}

/**
 * Create a character from the roster and return to the roster.
 *
 * THIS IS THE ONE PLACE THE CREATION JOURNEY IS SPELLED OUT. Six specs used to
 * carry their own copy of it, which is why deleting the roster's inline form in
 * plan 05-03 broke eight files at once. Callers that need the terminal chain
 * `enterGameAs` onto this rather than re-deriving the sequence.
 *
 * The affordance is a LINK to /characters/new (D-87), not an inline input, and
 * creation lands back on /characters rather than in the terminal (D-87, 05-06):
 * entering the game is now a separate, explicit act.
 */
export async function createCharacter(page: Page, charName: string): Promise<void> {
  await page.locator('[data-testid="create-character"]').click();
  await expect(page).toHaveURL(/\/characters\/new/, { timeout: 10000 });
  await page.fill('input[name="characterName"]', charName);
  await page.locator('button[type="submit"]').click();
  // The roster is the landing surface, and the new card appearing on it is the
  // observable that creation succeeded.
  await expect(page.locator('[data-testid="char-name"]', { hasText: charName })).toBeVisible({
    timeout: 15000,
  });
}

/**
 * Select an existing character from the roster and wait for a live terminal.
 * The whole playable card is the click target (008-B), so the card — not the
 * name span — is what is clicked.
 */
export async function enterGameAs(page: Page, charName: string): Promise<void> {
  await page.locator('[data-testid="roster-card"]', { hasText: charName }).click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
  // Wait for the stream to be fully open (STREAM_OPENED → connectionId set →
  // REPLAY_COMPLETE → conn-pill shows "connected"). Without this, sendCommand
  // may carry an empty connectionId, causing `scene focus` to error with
  // "`scene focus` requires a live connection."
  await page
    .locator('[data-testid="conn-pill"][data-status="connected"]')
    .waitFor({ timeout: 15000 });
}

/**
 * Register a new player, create a character, and land in the terminal.
 * Returns `{ username, password, charName }` for later re-login if needed.
 */
export async function registerAndEnterTerminal(
  page: Page,
  prefix: string,
): Promise<{ username: string; password: string; charName: string }> {
  const creds = await registerPlayer(page, prefix);
  await createCharacter(page, creds.charName);
  await enterGameAs(page, creds.charName);
  return creds;
}
