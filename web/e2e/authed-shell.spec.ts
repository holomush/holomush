// web/e2e/authed-shell.spec.ts
// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors
import { test, expect, registerAndEnterTerminal } from './helpers/fixtures';

test.describe('unified authed shell', () => {
  test('rail + footer persist across /terminal and /scenes with correct active state', async ({ page }) => {
    await registerAndEnterTerminal(page, 'shl');

    const rail = page.getByTestId('rail').first();
    await expect(rail).toBeVisible();
    await expect(page.getByTestId('shell-footer')).toBeVisible();
    // Room active on the terminal.
    await expect(rail.getByRole('link', { name: 'Room' })).toHaveAttribute('aria-current', 'page');

    // Navigate to Scenes via the rail.
    await rail.getByRole('link', { name: 'Scenes' }).click();
    await expect(page).toHaveURL(/\/scenes/);
    await expect(page.getByTestId('rail').first()).toBeVisible();
    await expect(page.getByTestId('shell-footer')).toBeVisible();
    await expect(page.getByTestId('rail').first().getByRole('link', { name: 'Scenes' }))
      .toHaveAttribute('aria-current', 'page');
  });

  test('command palette navigates between sections', async ({ page }) => {
    await registerAndEnterTerminal(page, 'pal');
    await page.keyboard.press('Meta+k');
    await page.getByPlaceholder('Type a command…').fill('Go to Scenes');
    await page.getByRole('option', { name: 'Go to Scenes' }).click();
    await expect(page).toHaveURL(/\/scenes/);
  });

  test('mobile: hamburger opens the drawer and navigates', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await registerAndEnterTerminal(page, 'mnv');

    // Persistent rail is collapsed on mobile.
    await expect(page.getByRole('button', { name: 'Open navigation' })).toBeVisible();
    await page.getByRole('button', { name: 'Open navigation' }).click();

    // The drawer is a dialog (labelled by its SheetTitle "Navigation").
    const drawer = page.getByRole('dialog', { name: 'Navigation' });
    await expect(drawer).toBeVisible();
    // Click the Scenes link INSIDE the drawer (the persistent rail also has a
    // Scenes link in the DOM at mobile width — width:0/hidden — so scope to the
    // drawer; do NOT assert a global link count).
    await drawer.getByRole('link', { name: 'Scenes' }).click();
    await expect(page).toHaveURL(/\/scenes/);
    await expect(drawer).toHaveCount(0); // drawer unmounted on navigate
  });

  test('terminal hotkey bar renders in the shell footer (no regression)', async ({ page }) => {
    await registerAndEnterTerminal(page, 'ftr');
    const footer = page.getByTestId('shell-footer');
    await expect(footer).toContainText('history');
    await expect(footer).toContainText('composer');
  });
});
