// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect, db, getClientSessionId } from './helpers/fixtures';

test.describe('Auth Flows', () => {
  test('landing page shows login and register links', async ({ page }) => {
    await page.goto('/');
    const main = page.getByRole('main');
    await expect(main.getByRole('link', { name: 'Login' })).toBeVisible();
    await expect(main.getByRole('link', { name: 'Register' })).toBeVisible();
    await expect(main.getByRole('button', { name: 'Try as Guest' })).toBeVisible();
  });

  test('login page renders with form fields', async ({ page }) => {
    await page.goto('/login');
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });

  test('register page renders with form fields', async ({ page }) => {
    await page.goto('/register');
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
  });

  test('guest login from landing page enters terminal', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('main').getByRole('button', { name: 'Try as Guest' }).click();
    await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // DB: session exists with valid location
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();
    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();
    expect(session!.is_guest).toBe(true);
    expect(session!.status).toBe('active');
    expect(db.isValidLocationId(session!.location_id)).toBe(true);

    // DB: location matches the starting location
    const startLoc = await db.getStartingLocation();
    expect(startLoc).not.toBeNull();
    expect(session!.location_id).toBe(startLoc!.id);
  });

  test('register with mismatched passwords shows error', async ({ page }) => {
    await page.goto('/register');
    await page.fill('input[name="username"]', 'testuser');
    await page.fill('input[name="password"]', 'password123');
    await page.fill('input[name="confirmPassword"]', 'different123');
    // Try to submit — client-side validation should catch this
    await page.locator('button[type="submit"]').click();
    await expect(page.locator('text=Passwords do not match')).toBeVisible({ timeout: 5000 });
  });

  test('register with short password shows error', async ({ page }) => {
    await page.goto('/register');
    await page.fill('input[name="username"]', 'testuser');
    await page.fill('input[name="password"]', 'short');
    await page.fill('input[name="confirmPassword"]', 'short');
    await page.locator('button[type="submit"]').click();
    await expect(page.locator('text=at least 8')).toBeVisible({ timeout: 5000 });
  });

  test('unauthenticated access to /terminal redirects to /login', async ({ page }) => {
    await page.goto('/terminal');
    // Auth guard should redirect — either to /login or show landing
    await expect(page).not.toHaveURL(/\/terminal/);
  });

  test('unauthenticated access to /characters redirects to /login', async ({ page }) => {
    await page.goto('/characters');
    await expect(page).not.toHaveURL(/\/characters/);
  });

  test('password reset page renders', async ({ page }) => {
    await page.goto('/reset');
    await expect(page.locator('input[name="email"]')).toBeVisible();
  });
});

test.describe('Auth Flows — Registered User Login', () => {
  test('register → logout → login → terminal with DB validation', async ({ page }) => {
    const suffix = `${Date.now()}_${Math.random().toString(36).slice(2, 6)}`;
    const testUser = `e2e_login_${suffix}`;
    const alpha = 'abcdefghijklmnopqrstuvwxyz';
    const charSuffix = Array.from({ length: 6 }, () => alpha[Math.floor(Math.random() * 26)]).join('');
    const testChar = `Loginhero ${charSuffix}`;

    // ── Phase 1: Register and create character ──
    await page.goto('/register');
    await page.fill('input[name="username"]', testUser);
    await page.fill('input[name="password"]', 'testpass123');
    await page.fill('input[name="confirmPassword"]', 'testpass123');
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    const createBtn = page.locator('text=Create New Character');
    await expect(createBtn).toBeVisible({ timeout: 10000 });
    await createBtn.click();
    await page.fill('input[name="characterName"]', testChar);
    await page.locator('label.checkbox-label input[type="checkbox"]').check();
    await page.locator('button:has-text("Create")').click();
    await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // ── Phase 2: Quit → character picker → logout ──
    // Quit returns to character picker (player stays authenticated)
    const input = page.locator('textarea');
    await input.fill('quit');
    await input.press('Enter');
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Click logout button in TopBar to fully log out
    await page.getByLabel('Log out').click();
    await expect(page).toHaveURL(/\/$/, { timeout: 10000 });

    // ── Phase 3: Login with credentials ──
    await page.goto('/login');
    await page.fill('input[name="username"]', testUser);
    await page.fill('input[name="password"]', 'testpass123');
    await page.locator('button[type="submit"]').click();

    // With a single character, login auto-selects and goes straight to terminal
    await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // ── DB validations ──
    const player = await db.getPlayerByUsername(testUser);
    expect(player).not.toBeNull();

    // DB: player_session exists after login (persistent session)
    const sessions = await db.getPlayerSessions(player!.id);
    expect(sessions.length).toBeGreaterThanOrEqual(1);

    // DB: game session created, active, non-guest
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();
    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();
    expect(session!.status).toBe('active');
    expect(session!.is_guest).toBe(false);
    expect(db.isValidLocationId(session!.location_id)).toBe(true);

    // DB: session location matches character location
    const chars = await db.getCharactersByPlayerId(player!.id);
    expect(chars.length).toBe(1);
    expect(chars[0].location_id).toBe(session!.location_id);
  });
});

test.describe('Auth Flows — Logout', () => {
  test('registered user logout clears session from DB', async ({ page }) => {
    const suffix = `${Date.now()}_${Math.random().toString(36).slice(2, 6)}`;
    const testUser = `e2e_logout_${suffix}`;
    const alpha = 'abcdefghijklmnopqrstuvwxyz';
    const charSuffix = Array.from({ length: 6 }, () => alpha[Math.floor(Math.random() * 26)]).join('');
    const testChar = `Logouthero ${charSuffix}`;

    // Register and enter terminal
    await page.goto('/register');
    await page.fill('input[name="username"]', testUser);
    await page.fill('input[name="password"]', 'testpass123');
    await page.fill('input[name="confirmPassword"]', 'testpass123');
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    const createBtn = page.locator('text=Create New Character');
    await expect(createBtn).toBeVisible({ timeout: 10000 });
    await createBtn.click();
    await page.fill('input[name="characterName"]', testChar);
    await page.locator('label.checkbox-label input[type="checkbox"]').check();
    await page.locator('button:has-text("Create")').click();
    await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();

    // Verify session exists in DB before logout
    const sessionBefore = await db.getSessionById(sessionId!);
    expect(sessionBefore).not.toBeNull();
    expect(sessionBefore!.status).toBe('active');

    // Quit returns to character picker (player still authenticated)
    const input = page.locator('textarea');
    await input.fill('quit');
    await input.press('Enter');
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Verify character sessionStorage cleared but player session persists
    const storedSession = await page.evaluate(() => sessionStorage.getItem('holomush-session'));
    expect(storedSession).toBeNull();
    const storedPlayer = await page.evaluate(() => sessionStorage.getItem('holomush-player'));
    expect(storedPlayer).not.toBeNull();

    // DB: game session deleted after quit
    await expect(async () => {
      const dbSession = await db.getSessionById(sessionId!);
      expect(dbSession).toBeNull();
    }).toPass({ timeout: 5000 });

    // Now fully log out via TopBar
    await page.getByLabel('Log out').click();
    await expect(page).toHaveURL(/\/$/, { timeout: 10000 });

    // Verify all sessionStorage cleared
    const storedPlayerAfter = await page.evaluate(() => sessionStorage.getItem('holomush-player'));
    expect(storedPlayerAfter).toBeNull();

    // DB: player session deleted after logout
    const player = await db.getPlayerByUsername(testUser);
    expect(player).not.toBeNull();
    await expect(async () => {
      const playerSessions = await db.getPlayerSessions(player!.id);
      expect(playerSessions.length).toBe(0);
    }).toPass({ timeout: 5000 });
  });
});

test.describe('Auth Flows — Full Registration Flow', () => {
  test('register → character select → create character → terminal', async ({ page }) => {
    const suffix = `${Date.now()}_${Math.random().toString(36).slice(2, 6)}`;
    const testUser = `e2e_${suffix}`;
    // Character names allow letters and spaces only — use alpha suffix
    const alpha = 'abcdefghijklmnopqrstuvwxyz';
    const charSuffix = Array.from({ length: 6 }, () => alpha[Math.floor(Math.random() * 26)]).join('');
    const testChar = `Testhero ${charSuffix}`;
    // Register
    await page.goto('/register');
    await page.fill('input[name="username"]', testUser);
    await page.fill('input[name="password"]', 'testpass123');
    await page.fill('input[name="confirmPassword"]', 'testpass123');
    await page.locator('button[type="submit"]').click();

    // Should redirect to character select (new player has no characters)
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Create a character with auto-enter
    const createBtn = page.locator('text=Create New Character');
    await expect(createBtn).toBeVisible({ timeout: 10000 });
    await createBtn.click();
    await page.fill('input[name="characterName"]', testChar);
    await page.locator('label.checkbox-label input[type="checkbox"]').check();
    await page.locator('button:has-text("Create")').click();

    // Should auto-enter terminal
    await expect(page).toHaveURL(/\/terminal/, { timeout: 15000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });

    // ── DB validations (all after UI flow completes) ──

    // DB: player exists
    const player = await db.getPlayerByUsername(testUser);
    expect(player).not.toBeNull();

    // DB: session exists with valid location
    const sessionId = await getClientSessionId(page);
    expect(sessionId).toBeTruthy();
    const session = await db.getSessionById(sessionId!);
    expect(session).not.toBeNull();
    expect(session!.status).toBe('active');
    expect(session!.is_guest).toBe(false);
    expect(db.isValidLocationId(session!.location_id)).toBe(true);

    // DB: character exists, matches testChar, and location matches session
    const chars = await db.getCharactersByPlayerId(player!.id);
    expect(chars.length).toBe(1);
    expect(chars[0].name.toLowerCase()).toBe(testChar.toLowerCase());
    expect(chars[0].location_id).toBe(session!.location_id);
  });
});
