// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect } from '@playwright/test';

test.describe('Landing Page', () => {
  test('shows landing page with link to terminal', async ({ page }) => {
    await page.goto('/');

    await expect(page.getByRole('heading', { name: 'HoloMUSH' })).toBeVisible();
    await expect(page.getByText('A modern MUSH platform')).toBeVisible();
    await expect(page.getByRole('link', { name: 'Enter Terminal' })).toBeVisible();
  });

  test('navigates to terminal from landing page', async ({ page }) => {
    await page.goto('/');
    await page.getByRole('link', { name: 'Enter Terminal' }).click();
    await expect(page).toHaveURL(/\/terminal/);
    await expect(page.getByRole('button', { name: 'Connect as Guest' })).toBeVisible();
  });
});
