// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 HoloMUSH Contributors

import { test, expect } from './helpers/fixtures';

// Repro from spec §1.1: with a single browser context (shared cookie jar
// across tabs, just like a real browser), tab 1 enters the terminal as a
// guest, tab 2 then opens "/" — historically this would either re-create
// guest state and break tab 1's session, or surface the guest CTA on tab
// 2 even though tab 1 is already authenticated.
//
// After the multi-tab session isolation work, tab 2 must land on the
// authenticated landing branch (Continue button visible, Try as Guest
// hidden), and tab 1 must remain able to send commands without errors.
test('multi-tab guest creation no longer breaks tab 1', async ({ browser }) => {
  // Single browser context = shared cookie jar across tabs.
  const ctx = await browser.newContext();
  try {
    // Tab 1: guest login → terminal.
    const tab1 = await ctx.newPage();
    await tab1.goto('/');
    await tab1.getByTestId('guest-button').click();
    await expect(tab1).toHaveURL(/\/terminal$/, { timeout: 15_000 });
    await expect(tab1.locator('.terminal-layout')).toBeVisible({ timeout: 15_000 });

    // Tab 2: opens "/" in the same context — should see the authenticated
    // landing branch, NOT the guest CTA.
    const tab2 = await ctx.newPage();
    await tab2.goto('/');
    await expect(tab2.getByTestId('continue-button')).toBeVisible({ timeout: 15_000 });
    await expect(tab2.getByTestId('guest-button')).not.toBeVisible();

    // Tab 1 still able to send. Mirrors the terminal.spec.ts pattern: type
    // a `say` with a unique token into the textarea, press Enter, and assert
    // the matching event appears in the stream.
    await tab1.bringToFront();
    const token = `tab1-${Date.now()}`;
    const input = tab1.locator('textarea');
    await input.fill(`say ${token}`);
    await input.press('Enter');
    await expect(
      tab1.locator('[data-testid="event"]').filter({ hasText: token }),
    ).toBeVisible({ timeout: 10_000 });
  } finally {
    // Always close the extra context — without try/finally, an assertion
    // failure mid-test would leak the context to subsequent specs.
    await ctx.close();
  }
});
