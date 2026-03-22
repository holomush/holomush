// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect } from '@playwright/test';

test.describe('Auth Flows', () => {
  test('landing page shows login and register links', async ({ page }) => {
    await page.goto('/');
    await expect(page.getByRole('link', { name: 'Login' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Register' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Try as Guest' })).toBeVisible();
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
    await page.getByRole('button', { name: 'Try as Guest' }).click();
    await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
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

test.describe('Auth Flows — Full Registration Flow', () => {
  // These tests require the full Docker stack (task dev)
  const testUser = `e2e_${Date.now()}`;

  test('register → character select → create character → terminal', async ({ page }) => {
    // Register
    await page.goto('/register');
    await page.fill('input[name="username"]', testUser);
    await page.fill('input[name="password"]', 'testpass123');
    await page.fill('input[name="confirmPassword"]', 'testpass123');
    await page.locator('button[type="submit"]').click();

    // Should redirect to character select (new player has no characters)
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Create a character
    const createBtn = page.locator('text=Create New Character');
    if (await createBtn.isVisible()) {
      await createBtn.click();
      await page.fill('input[name="characterName"]', 'TestHero');
      await page.locator('button:has-text("Create")').click();
    }

    // Should auto-enter terminal
    await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
  });
});
