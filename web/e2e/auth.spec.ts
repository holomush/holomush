// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect } from '@playwright/test';

test.describe('Auth Flows', () => {
  test('landing page shows login and register links', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('text=Login')).toBeVisible();
    await expect(page.locator('text=Register')).toBeVisible();
  });

  test('top bar shows login/register when anonymous', async ({ page }) => {
    await page.goto('/');
    const topBar = page.locator('[data-testid="top-bar"]');
    await expect(topBar.locator('text=Login')).toBeVisible();
    await expect(topBar.locator('text=Register')).toBeVisible();
  });

  test('login page renders with form fields', async ({ page }) => {
    await page.goto('/login');
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
    await expect(page.locator('text=Sign In')).toBeVisible();
    await expect(page.locator('text=Try as Guest')).toBeVisible();
  });

  test('register page renders with form fields', async ({ page }) => {
    await page.goto('/register');
    await expect(page.locator('input[name="username"]')).toBeVisible();
    await expect(page.locator('input[name="password"]')).toBeVisible();
    await expect(page.locator('input[name="confirmPassword"]')).toBeVisible();
  });

  test('guest login from landing page enters terminal', async ({ page }) => {
    await page.goto('/');
    await page.click('text=Try as Guest');
    await expect(page).toHaveURL(/\/terminal/);
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
  });

  test('guest login from login page enters terminal', async ({ page }) => {
    await page.goto('/login');
    await page.click('text=Try as Guest');
    await expect(page).toHaveURL(/\/terminal/);
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
  });

  test('login with invalid credentials shows error', async ({ page }) => {
    await page.goto('/login');
    await page.fill('input[name="username"]', 'nonexistent');
    await page.fill('input[name="password"]', 'wrongpassword');
    await page.click('text=Sign In');
    await expect(page.locator('[data-testid="error-message"]')).toBeVisible({ timeout: 5000 });
  });

  test('register with mismatched passwords shows error', async ({ page }) => {
    await page.goto('/register');
    await page.fill('input[name="username"]', 'testuser');
    await page.fill('input[name="password"]', 'password123');
    await page.fill('input[name="confirmPassword"]', 'different123');
    await page.click('text=Create Account');
    await expect(page.locator('text=Passwords do not match')).toBeVisible();
  });

  test('register with short password shows error', async ({ page }) => {
    await page.goto('/register');
    await page.fill('input[name="username"]', 'testuser');
    await page.fill('input[name="password"]', 'short');
    await page.fill('input[name="confirmPassword"]', 'short');
    await page.click('text=Create Account');
    await expect(page.locator('text=at least 8')).toBeVisible();
  });

  test('unauthenticated access to /terminal redirects to /login', async ({ page }) => {
    await page.goto('/terminal');
    await expect(page).toHaveURL(/\/login/);
  });

  test('unauthenticated access to /characters redirects to /login', async ({ page }) => {
    await page.goto('/characters');
    await expect(page).toHaveURL(/\/login/);
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
    await page.click('text=Create Account');

    // Should redirect to character select (new player has no characters)
    await expect(page).toHaveURL(/\/characters/, { timeout: 10000 });

    // Create a character
    await page.click('text=Create New Character');
    await page.fill('input[name="characterName"]', 'TestHero');
    await page.click('text=Create');

    // Should auto-enter terminal
    await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
  });
});
