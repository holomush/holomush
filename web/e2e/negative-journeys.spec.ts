// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

/**
 * Negative-journey E2E tests — auth, scenes, terminal.
 *
 * Every test asserts visible UI feedback (error text, disabled state, denial
 * message) for an invalid or unauthorized action. No zero-assertion tests.
 *
 * Journeys NOT covered here and why:
 *   - reconnect-timeout recovery: overlaps the quarantined reconnect set
 *     (holomush-0jzs) — explicitly out of scope per bead spec.
 *   - scene end by non-owner: requires two registered players in the same
 *     location; the ABAC check happens before the handler and surfaces as
 *     a gRPC permission error that the command dispatcher renders as a
 *     terminal event. Deferred — the "scene end <bogus id>" journey already
 *     covers the error-surfacing path cleanly.
 *   - ABAC denial via /scenes admin UI: the scenes workspace has no
 *     dedicated "admin" surface yet; admin actions go through the terminal.
 *   - switch-to-deleted-character: the character picker lists only live
 *     characters from webListCharacters; deleted characters never appear,
 *     so there is no UI path to click one.
 */

import { test, expect, db } from './helpers/fixtures';
import type { Page } from '@playwright/test';

// ── Shared helpers ────────────────────────────────────────────────────────────

/** Connect as guest and land in the terminal. */
async function connectAsGuest(page: Page) {
  await page.goto('/');
  await page.getByRole('main').getByRole('button', { name: 'Try as Guest' }).click();
  await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
  await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
}

/** Snapshot the current terminal event count before a command. */
async function currentEventCount(page: Page): Promise<number> {
  return page.locator('[data-testid="event"]').count();
}

/**
 * Wait for a terminal event matching `pattern` at or after `sinceIndex`.
 * Mirrors the same helper in scenes.spec.ts.
 */
async function waitForOutputMatching(
  page: Page,
  pattern: RegExp,
  sinceIndex: number,
): Promise<void> {
  const events = page.locator('[data-testid="event"]');
  await expect
    .poll(
      async () => {
        const count = await events.count();
        for (let i = sinceIndex; i < count; i++) {
          const text = (await events.nth(i).textContent()) ?? '';
          if (pattern.test(text)) return true;
        }
        return false;
      },
      { timeout: 10000 },
    )
    .toBe(true);
}

/** Register a fresh player, create a character, and enter the terminal. */
async function registerAndEnterTerminal(
  page: Page,
  prefix: string,
): Promise<{ username: string; password: string; charName: string }> {
  const suffix = `${Date.now()}_${crypto.randomUUID().slice(0, 4)}`;
  const charSuffix = crypto.randomUUID().replace(/[^a-z]/g, '').slice(0, 6);
  const safePrefix = prefix.replace(/[^a-zA-Z]/g, '');
  const username = `e2e_nj_${safePrefix}_${suffix}`;
  const charName = `${safePrefix.charAt(0).toUpperCase() + safePrefix.slice(1)} ${charSuffix}`;
  const password = 'testpass123';

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

  return { username, password, charName };
}

// ── Auth — negative journeys ──────────────────────────────────────────────────

test.describe('Auth Flows — Negative Journeys', () => {
  // Wrong password: the server returns success=false with errorMessage
  // "invalid username or password". The login page renders this inside
  // [data-testid="login-error"].
  test('login with wrong password shows invalid credentials error', async ({ page }) => {
    // Register a real player first so the username exists.
    const suffix = `${Date.now()}_${crypto.randomUUID().slice(0, 4)}`;
    const username = `e2e_nj_wp_${suffix}`;

    await page.goto('/register');
    await page.fill('input[name="username"]', username);
    await page.fill('input[name="password"]', 'correctpass99');
    await page.fill('input[name="confirmPassword"]', 'correctpass99');
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Log out so we return to an unauthenticated state.
    await page.getByLabel('Log out').click();
    await expect(page).toHaveURL(/\/$/, { timeout: 10000 });

    // Attempt login with the wrong password.
    await page.goto('/login');
    await page.fill('input[name="username"]', username);
    await page.fill('input[name="password"]', 'wrongpassword');
    await page.locator('button[type="submit"]').click();

    // Must stay on /login — no redirect to /characters or /terminal.
    await expect(page).toHaveURL(/\/login/, { timeout: 5000 });

    // Error element must be visible and contain the server's sanitized message.
    const errorEl = page.locator('[data-testid="login-error"]');
    await expect(errorEl).toBeVisible({ timeout: 5000 });
    await expect(errorEl).toContainText(/invalid username or password/i);
  });

  // Non-existent username: same code path — server returns the same sanitized
  // message regardless of whether the username exists (avoids user enumeration).
  test('login with non-existent username shows invalid credentials error', async ({ page }) => {
    await page.goto('/login');
    await page.fill('input[name="username"]', 'user_that_does_not_exist_e2e_nj');
    await page.fill('input[name="password"]', 'anypassword');
    await page.locator('button[type="submit"]').click();

    await expect(page).toHaveURL(/\/login/, { timeout: 5000 });

    const errorEl = page.locator('[data-testid="login-error"]');
    await expect(errorEl).toBeVisible({ timeout: 5000 });
    await expect(errorEl).toContainText(/invalid username or password/i);
  });

  // Submitting login with empty fields: the client-side guard fires before
  // any RPC — the error renders immediately without a round-trip.
  test('login with empty credentials shows required fields error without redirecting', async ({
    page,
  }) => {
    await page.goto('/login');
    // Leave both fields empty and submit.
    await page.locator('button[type="submit"]').click();

    // Must remain on /login.
    await expect(page).toHaveURL(/\/login/, { timeout: 3000 });

    // Client-side validation fires: "Username and password are required."
    const errorEl = page.locator('[data-testid="login-error"]');
    await expect(errorEl).toBeVisible({ timeout: 3000 });
    await expect(errorEl).toContainText(/required/i);
  });

  // Duplicate username registration: the server returns success=false with
  // errorMessage "username is already taken". Rendered in
  // [data-testid="register-error"].
  test('registering a duplicate username shows already taken error', async ({ page }) => {
    const suffix = `${Date.now()}_${crypto.randomUUID().slice(0, 4)}`;
    const username = `e2e_nj_dup_${suffix}`;

    // First registration succeeds.
    await page.goto('/register');
    await page.fill('input[name="username"]', username);
    await page.fill('input[name="password"]', 'testpass123');
    await page.fill('input[name="confirmPassword"]', 'testpass123');
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Log out so we can attempt registration again from a clean state.
    await page.getByLabel('Log out').click();
    await expect(page).toHaveURL(/\/$/, { timeout: 10000 });

    // Second registration with the same username must fail.
    await page.goto('/register');
    await page.fill('input[name="username"]', username);
    await page.fill('input[name="password"]', 'testpass123');
    await page.fill('input[name="confirmPassword"]', 'testpass123');
    await page.locator('button[type="submit"]').click();

    // Must stay on /register.
    await expect(page).toHaveURL(/\/register/, { timeout: 5000 });

    const errorEl = page.locator('[data-testid="register-error"]');
    await expect(errorEl).toBeVisible({ timeout: 5000 });
    await expect(errorEl).toContainText(/already taken/i);
  });
});

// ── Terminal — negative journeys ──────────────────────────────────────────────

test.describe('Terminal — Negative Journeys', () => {
  // An unrecognised command (not registered by any plugin) is dispatched to
  // the core command engine which returns an error event. The terminal must
  // render that event — this verifies the error path from server to UI.
  test('unknown command shows error output in terminal', async ({ page }) => {
    await connectAsGuest(page);
    const input = page.locator('textarea');

    const before = await currentEventCount(page);
    // Use a token that cannot possibly match a real command name.
    await input.fill('xyzzy_not_a_real_command_e2e_nj');
    await input.press('Enter');

    // Terminal must render at least one new event containing an error indicator.
    // The core engine typically returns "Unknown command" or similar.
    await waitForOutputMatching(page, /unknown|not found|invalid|error/i, before);

    // The command input must be empty and editable after the response — not stuck.
    await expect(input).toHaveValue('');
    await expect(input).toBeEditable();
  });

  // A non-existent (but well-formed ULID) scene ID produces a client-visible
  // error event. Scene commands are registered-player-only (guests are denied
  // at Layer-1 per holomush-5rh.23), so this runs as a registered player; the
  // command reaches the store, which returns a not-found error surfaced as
  // "Failed to end scene: ...".
  test('scene end with bogus id shows error event in terminal', async ({ page }) => {
    await registerAndEnterTerminal(page, 'nse');
    const input = page.locator('textarea');

    const before = await currentEventCount(page);
    await input.fill('scene end 00000000000000000000000000');
    await input.press('Enter');

    // Must receive a failure message — not silently succeed.
    await waitForOutputMatching(page, /Failed to end scene|not found|error/i, before);
  });
});

// ── Scenes workspace — negative journeys ─────────────────────────────────────

test.describe('Scenes — Negative Journeys', () => {
  // A player who already owns a character with name X cannot create a second
  // character with the same name on the same account. The server returns
  // success=false with "character name is already taken". The character picker
  // renders this inline as `p.text-destructive` without navigating away.
  //
  // We use one player with two creation attempts — simpler and less prone to
  // cookie-state races than the two-player variant.
  test('creating a second character with the same name shows already taken error', async ({
    page,
  }) => {
    const suffix = `${Date.now()}_${crypto.randomUUID().slice(0, 4)}`;
    const username = `e2e_nj_cn_${suffix}`;
    // Character names: letters and spaces only.
    const alpha = 'abcdefghijklmnopqrstuvwxyz';
    const charSuffix = Array.from({ length: 6 }, () => alpha[Math.floor(Math.random() * 26)]).join('');
    const takenName = `Njchar ${charSuffix}`;

    // Register and land on character picker (no characters yet).
    await page.goto('/register');
    await page.fill('input[name="username"]', username);
    await page.fill('input[name="password"]', 'testpass123');
    await page.fill('input[name="confirmPassword"]', 'testpass123');
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // First creation: succeed without entering game (leave "Enter game immediately" unticked).
    await page.locator('text=Create New Character').click();
    await page.fill('input[name="characterName"]', takenName);
    // Do NOT tick autoDefault — stay on /characters after creation.
    await page.locator('button:has-text("Create")').click();
    // The character appears in the list — creation succeeded.
    await expect(page.locator('[data-testid="char-name"]', { hasText: takenName })).toBeVisible({
      timeout: 10000,
    });

    // Second creation attempt: same name, same player.
    await page.locator('text=Create New Character').click();
    await page.fill('input[name="characterName"]', takenName);
    await page.locator('button:has-text("Create")').click();

    // Must stay on /characters — no redirect to /terminal.
    await expect(page).toHaveURL(/\/characters/, { timeout: 5000 });

    // Inline createError must be visible and contain the server's sanitized message.
    const errorEl = page.locator('p.text-destructive');
    await expect(errorEl.first()).toBeVisible({ timeout: 5000 });
    await expect(errorEl.first()).toContainText(/already taken/i);
  });

  // A guest player navigating directly to /scenes/browse must be redirected
  // to /terminal (the scenes guest guard). The scenes workspace must not render.
  // This is a different assertion from S6 (which tests /scenes root) — here
  // we test a deeper path (/scenes/browse) to verify the guard covers sub-routes.
  test('guest player navigating to /scenes/browse is redirected to /terminal', async ({ page }) => {
    await connectAsGuest(page);

    await page.goto('/scenes/browse');
    await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });

    // Browse board must NOT render — guard fired before the page loaded.
    await expect(page.getByRole('list', { name: 'Scene list' })).not.toBeVisible();
  });
});
