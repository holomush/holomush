// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect } from './helpers/fixtures';
import type { Browser, BrowserContext, Page } from '@playwright/test';

/**
 * Generate a short unique suffix that fits within username length limits (max 30 chars).
 */
function shortSuffix(): string {
  const ts = String(Date.now()).slice(-8);
  const rand = crypto.randomUUID().slice(0, 4);
  return `${ts}_${rand}`;
}

/**
 * Register a fresh player via the UI, create a character, and enter terminal.
 * Returns { username, password, charName }. The page is left on the terminal.
 */
async function registerAndEnterTerminal(
  page: Page,
  prefix: string,
  charPrefix: string,
): Promise<{ username: string; password: string; charName: string }> {
  const username = `${prefix}${shortSuffix()}`;
  const charSuffix = crypto.randomUUID().replace(/[^a-z]/g, '').slice(0, 6);
  const charName = `${charPrefix} ${charSuffix}`;
  const password = 'testpass123';

  await page.goto('/register');
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', password);
  await page.fill('input[name="confirmPassword"]', password);
  await page.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

  const createBtn = page.locator('text=Create New Character');
  await expect(createBtn).toBeVisible({ timeout: 10000 });
  await createBtn.click();
  await page.fill('input[name="characterName"]', charName);
  await page.locator('button[role="checkbox"]').click();
  await page.locator('button:has-text("Create")').click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

  return { username, password, charName };
}

/**
 * Log an existing player in by navigating to /login and submitting credentials.
 * Assumes the player has at least one character, so login auto-selects and
 * redirects to /terminal.
 */
async function loginExistingPlayer(page: Page, username: string, password: string): Promise<void> {
  await page.goto('/login');
  await page.fill('input[name="username"]', username);
  await page.fill('input[name="password"]', password);
  await page.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
}

/**
 * Open a fresh browser context + page and log the existing player in.
 */
async function loginIntoNewContext(
  browser: Browser,
  username: string,
  password: string,
): Promise<{ ctx: BrowserContext; page: Page }> {
  const ctx = await browser.newContext();
  const page = await ctx.newPage();
  await loginExistingPlayer(page, username, password);
  return { ctx, page };
}

test.describe('Session Security (bd-urbq)', () => {
  test('revoking other sessions terminates them while keeping caller active', async ({
    browser,
  }) => {
    // Register a player in context A, which lands them on the terminal with
    // one active PlayerSession.
    const ctxA = await browser.newContext();
    const pageA = await ctxA.newPage();
    const { username, password } = await registerAndEnterTerminal(pageA, 'e2esrvk', 'Revo');

    // Log the same player into a second context (B). Each context has its own
    // cookie jar, so this creates a second PlayerSession for the same player.
    const { ctx: ctxB, page: pageB } = await loginIntoNewContext(browser, username, password);

    // Precondition: both sessions should be live on /terminal.
    await expect(pageA).toHaveURL(/\/terminal/);
    await expect(pageB).toHaveURL(/\/terminal/);

    // From context A, invoke WebRevokeOtherPlayerSessions via a direct POST
    // to the ConnectRPC endpoint. The page's cookie jar carries the session
    // cookie automatically via credentials: "include".
    const result = await pageA.evaluate(async () => {
      const response = await fetch('/holomush.web.v1.WebService/WebRevokeOtherPlayerSessions', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
        body: JSON.stringify({}),
      });
      const text = await response.text();
      let body: Record<string, unknown> = {};
      try {
        body = JSON.parse(text) as Record<string, unknown>;
      } catch {
        // non-JSON body on error; leave empty
      }
      return { ok: response.ok, status: response.status, body };
    });

    expect(result.ok).toBe(true);
    expect(result.body.success).toBe(true);
    // Connect-JSON encodes proto int32 as a JS number; coerce for safety.
    expect(Number(result.body.revokedCount ?? 0)).toBeGreaterThanOrEqual(1);

    // Context B's PlayerSession has been revoked. The next navigation triggers
    // the authed layout's webCheckSession, which fails and redirects to /login.
    await pageB.goto('/terminal');
    await expect(pageB).toHaveURL(/\/login|\/$/, { timeout: 20000 });

    // Context A is still authenticated — terminal remains reachable.
    await pageA.goto('/terminal');
    await expect(pageA).toHaveURL(/\/terminal/, { timeout: 10000 });
    await expect(pageA.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    await ctxA.close();
    await ctxB.close();
  });

  test('11th login evicts the oldest session', async ({ browser }) => {
    // DefaultMaxPlayerSessionsPerPlayer is 10; the 11th login must evict
    // the oldest PlayerSession via the session cap enforcement path.
    const cap = 10;

    // First login: register a fresh player (context 0). This creates the
    // "oldest" PlayerSession.
    const ctx0 = await browser.newContext();
    const page0 = await ctx0.newPage();
    const { username, password } = await registerAndEnterTerminal(page0, 'e2ecap', 'Cap');

    // Open (cap - 1) additional concurrent logins, keeping the session at
    // exactly cap. No eviction should happen yet.
    const extras: { ctx: BrowserContext; page: Page }[] = [];
    for (let i = 1; i < cap; i++) {
      extras.push(await loginIntoNewContext(browser, username, password));
    }

    // Everyone is still authenticated.
    await page0.goto('/terminal');
    await expect(page0).toHaveURL(/\/terminal/, { timeout: 10000 });

    // The (cap + 1)-th login triggers eviction of the oldest session, which
    // is the one in ctx0.
    const { ctx: ctxNewest, page: pageNewest } = await loginIntoNewContext(
      browser,
      username,
      password,
    );

    // Context 0's PlayerSession has been evicted. Re-navigating triggers the
    // authed layout's webCheckSession, which fails and redirects to /login.
    await page0.goto('/terminal');
    await expect(page0).toHaveURL(/\/login|\/$/, { timeout: 20000 });

    // The newest context is still authenticated.
    await pageNewest.goto('/terminal');
    await expect(pageNewest).toHaveURL(/\/terminal/, { timeout: 10000 });
    await expect(pageNewest.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // Cleanup.
    await ctx0.close();
    for (const { ctx } of extras) {
      await ctx.close();
    }
    await ctxNewest.close();
  });
});
