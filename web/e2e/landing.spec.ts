// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect } from '@playwright/test';

test.describe('Landing Page', () => {
  test('shows landing page with auth links', async ({ page }) => {
    await page.goto('/');

    await expect(page.getByRole('heading', { name: 'HoloMUSH' })).toBeVisible();
    await expect(page.getByText('A modern MUSH platform')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Login' })).toBeVisible();
    await expect(page.getByRole('link', { name: 'Register' })).toBeVisible();
    await expect(page.getByRole('button', { name: 'Try as Guest' })).toBeVisible();
  });

  test('navigates to login from landing page', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: 'Login' }).click();
    await expect(page).toHaveURL(/\/login/);
  });

  test('guest login from landing enters terminal', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('button', { name: 'Try as Guest' }).click();
    await expect(page).toHaveURL(/\/terminal/, { timeout: 10000 });
    await expect(page.locator('.terminal-layout')).toBeVisible({ timeout: 10000 });
  });
});
